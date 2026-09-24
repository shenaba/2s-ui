package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
)

// A node keeps the master's bans briefly if a scan is missed, then falls back
// to its own per-node cap rather than holding an obsolete cluster decision.
const clusterBanLease = 30 * time.Second

type clusterIPHold struct {
	firstSeen time.Time
	lastSeen  time.Time
}

var clusterIPs = struct {
	mu        sync.RWMutex
	holders   map[ipBanKey]clusterIPHold
	bans      map[ipBanKey]time.Time
	counts    map[string]int
	list      map[string][]OnlineIP
	active    bool
	updatedAt time.Time
}{
	holders: map[ipBanKey]clusterIPHold{},
	bans:    map[ipBanKey]time.Time{},
}

// ClusterIPSnapshot is one local panel's active, limited client IP identities.
// It walks the tracker once, so a master can fetch a whole node in one request.
func ClusterIPSnapshot() (map[string][]string, error) {
	// A node can also have its own clients. Only the clients pushed by a master
	// may be counted or evicted by that master's cluster decision.
	var rows []struct {
		Name    string
		LimitIp int
	}
	if err := database.GetDB().Model(model.Client{}).
		Select("`name`, `limit_ip`").
		Where("limit_ip > 0 AND enable = ? AND `group` = ?", true, clusterGroup).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	limits := make(map[string]int, len(rows))
	for _, row := range rows {
		limits[row.Name] = row.LimitIp
	}
	return clusterIPSnapshot(limits), nil
}

func clusterIPSnapshot(limits map[string]int) map[string][]string {
	out := map[string][]string{}
	if len(limits) == 0 {
		return out
	}
	tracker := liveConnTracker()
	if tracker == nil {
		return out
	}
	active, now := tracker.UserIPs(func(user string) bool {
		_, limited := limits[user]
		return limited
	})
	for user, ips := range active {
		for addr, window := range ips {
			if core.IPLimitActive() && now-window.LastSeen > int64(ipIdleGrace) {
				continue
			}
			out[user] = append(out[user], addr.String())
		}
		sort.Strings(out[user])
	}
	return out
}

func normalizeClusterIP(raw string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, err
	}
	addr = addr.Unmap()
	if addr.Is6() {
		addr = netip.PrefixFrom(addr, core.IPv6IdentityPrefixBits).Masked().Addr()
	}
	return addr, nil
}

// ApplyClusterBans replaces the leased deny set and drops existing connections
// for the same (client, IP) pairs. The normal local limiter remains in force.
func ApplyClusterBans(raw map[string][]string) error {
	until := time.Now().Add(clusterBanLease).Unix()
	_, managed, err := loadIPLimits()
	if err != nil {
		return fmt.Errorf("read cluster-managed clients: %w", err)
	}
	bans := make(map[ipBanKey]int64)
	drop := make(map[string]map[netip.Addr]struct{})
	for user, ips := range raw {
		if user == "" {
			return fmt.Errorf("cluster IP ban has an empty client name")
		}
		for _, ip := range ips {
			addr, err := normalizeClusterIP(ip)
			if err != nil {
				return fmt.Errorf("cluster IP ban for %s: %w", user, err)
			}
			key := ipBanKey{user: user, ip: addr}
			if len(bans) >= ipBanMaxSize && bans[key] == 0 {
				return fmt.Errorf("too many cluster IP bans")
			}
			bans[key] = until
			if drop[user] == nil {
				drop[user] = map[netip.Addr]struct{}{}
			}
			drop[user][addr] = struct{}{}
		}
	}
	ipLimits.mu.Lock()
	ipLimits.clusterBans = bans
	ipLimits.clusterManaged = managed
	ipLimits.clusterLeaseUntil = until
	ipLimits.mu.Unlock()
	if tracker := liveConnTracker(); tracker != nil && len(drop) > 0 {
		tracker.CloseConnByUserIPs(drop)
	}
	return nil
}

// EnforceClusterIPLimits coordinates one global cap per master client. Local
// node limits still protect each node if the master cannot reach it.
func EnforceClusterIPLimits() {
	limits, managed, err := loadIPLimits()
	if err != nil {
		logger.Warning("cluster IP limit: read limits: ", err)
		deactivateClusterIPSnapshot()
		return
	}
	// Clients received from another master belong to that master's policy.
	for name := range managed {
		delete(limits, name)
	}
	var nodes []model.Node
	if err := database.GetDB().Where("enable = ?", true).Find(&nodes).Error; err != nil {
		logger.Warning("cluster IP limit: read nodes: ", err)
		deactivateClusterIPSnapshot()
		return
	}
	if len(nodes) == 0 || len(limits) == 0 {
		clusterIPs.mu.Lock()
		clusterIPs.holders = map[ipBanKey]clusterIPHold{}
		clusterIPs.bans = map[ipBanKey]time.Time{}
		clusterIPs.counts = nil
		clusterIPs.list = nil
		clusterIPs.active = false
		clusterIPs.updatedAt = time.Time{}
		clusterIPs.mu.Unlock()
		return
	}

	all := clusterIPSnapshot(limits)
	status := (&NodeService{}).GetStatuses()
	for _, node := range nodes {
		if status[node.Id].State != "online" {
			deactivateClusterIPSnapshot()
			return
		}
	}
	snapshots := make([]map[string][]string, len(nodes))
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	sem := make(chan struct{}, nodeProbeParallel)
	for i := range nodes {
		if status[nodes[i].Id].State != "online" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			node := &nodes[i]
			client := nodeHTTPClient(node)
			obj, err := (&NodeService{}).nodeGet(node, client, "clusterIps", nil)
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = json.Unmarshal(obj, &snapshots[i])
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			logger.Warning("cluster IP limit: read ", nodes[i].Name, ": ", err)
			deactivateClusterIPSnapshot()
			return
		}
	}
	for _, snapshot := range snapshots {
		for user, ips := range snapshot {
			if limits[user] > 0 {
				all[user] = append(all[user], ips...)
			}
		}
	}

	bans, err := planClusterIPLimits(all, limits, time.Now())
	if err != nil {
		logger.Warning("cluster IP limit: plan: ", err)
		deactivateClusterIPSnapshot()
		return
	}
	if err := ApplyClusterBans(bans); err != nil {
		logger.Warning("cluster IP limit: apply local bans: ", err)
		deactivateClusterIPSnapshot()
		return
	}
	data, err := json.Marshal(bans)
	if err != nil {
		logger.Warning("cluster IP limit: encode bans: ", err)
		deactivateClusterIPSnapshot()
		return
	}
	wg = sync.WaitGroup{}
	sem = make(chan struct{}, nodeProbeParallel)
	for i := range nodes {
		if status[nodes[i].Id].State != "online" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			form := url.Values{"data": {string(data)}}
			_, errs[i] = (&NodeSyncService{}).nodePost(&nodes[i], nodeHTTPClient(&nodes[i]), "clusterBans", form)
		}(i)
	}
	wg.Wait()
	failed := false
	for i, err := range errs {
		if err != nil {
			logger.Warning("cluster IP limit: apply bans to ", nodes[i].Name, ": ", err)
			failed = true
		}
	}
	if failed {
		deactivateClusterIPSnapshot()
	}
}

func deactivateClusterIPSnapshot() {
	clusterIPs.mu.Lock()
	clusterIPs.active = false
	clusterIPs.updatedAt = time.Time{}
	clusterIPs.counts = nil
	clusterIPs.list = nil
	clusterIPs.mu.Unlock()
}

func planClusterIPLimits(snapshots map[string][]string, limits map[string]int, now time.Time) (map[string][]string, error) {
	active := make(map[ipBanKey]bool)
	for user, ips := range snapshots {
		if limits[user] <= 0 {
			continue
		}
		for _, ip := range ips {
			addr, err := normalizeClusterIP(ip)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", user, err)
			}
			active[ipBanKey{user: user, ip: addr}] = true
		}
	}
	clusterIPs.mu.Lock()
	defer clusterIPs.mu.Unlock()
	for key, hold := range clusterIPs.holders {
		if !active[key] && now.Sub(hold.lastSeen) > ipHoldMemory {
			delete(clusterIPs.holders, key)
		}
	}
	for key := range active {
		hold := clusterIPs.holders[key]
		if hold.firstSeen.IsZero() {
			hold.firstSeen = now
		}
		hold.lastSeen = now
		clusterIPs.holders[key] = hold
	}
	for key, until := range clusterIPs.bans {
		if limits[key.user] <= 0 || !until.After(now) {
			delete(clusterIPs.bans, key)
		}
	}

	counts := make(map[string]int, len(limits))
	lists := make(map[string][]OnlineIP, len(limits))
	for user, limit := range limits {
		var candidates []ipBanKey
		for key := range active {
			if key.user == user {
				candidates = append(candidates, key)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := clusterIPs.holders[candidates[i]], clusterIPs.holders[candidates[j]]
			if !a.firstSeen.Equal(b.firstSeen) {
				return a.firstSeen.Before(b.firstSeen)
			}
			return candidates[i].ip.Compare(candidates[j].ip) < 0
		})
		available := 0
		for _, key := range candidates {
			if _, banned := clusterIPs.bans[key]; !banned {
				available++
			}
		}
		if available < limit {
			for key := range clusterIPs.bans {
				if key.user == user {
					delete(clusterIPs.bans, key)
				}
			}
		}
		for _, key := range candidates {
			if _, banned := clusterIPs.bans[key]; banned {
				delete(clusterIPs.holders, key)
				continue
			}
			if counts[user] >= limit {
				if _, banned := clusterIPs.bans[key]; !banned {
					clusterIPs.bans[key] = now.Add(ipBanTTL)
				}
				delete(clusterIPs.holders, key)
				continue
			}
			counts[user]++
			// Node snapshots contain IP identities, not their connection start
			// times. Leave Since unknown instead of reporting the scan time.
			lists[user] = append(lists[user], OnlineIP{IP: identityLabel(key.ip)})
		}
	}
	if len(clusterIPs.bans) > ipBanMaxSize {
		keys := make([]ipBanKey, 0, len(clusterIPs.bans))
		for key := range clusterIPs.bans {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := clusterIPs.bans[keys[i]], clusterIPs.bans[keys[j]]
			if !a.Equal(b) {
				return a.Before(b)
			}
			if keys[i].user != keys[j].user {
				return keys[i].user < keys[j].user
			}
			return keys[i].ip.Compare(keys[j].ip) < 0
		})
		for _, key := range keys[:len(keys)-ipBanMaxSize] {
			delete(clusterIPs.bans, key)
		}
	}
	bans := make(map[string][]string)
	for key := range clusterIPs.bans {
		bans[key.user] = append(bans[key.user], key.ip.String())
	}
	for user := range bans {
		sort.Strings(bans[user])
	}
	clusterIPs.counts = counts
	clusterIPs.list = lists
	clusterIPs.active = true
	clusterIPs.updatedAt = now
	return bans, nil
}

func clusterIPCountSnapshot() (map[string]int, bool) {
	clusterIPs.mu.RLock()
	defer clusterIPs.mu.RUnlock()
	if !clusterIPs.active || time.Since(clusterIPs.updatedAt) > clusterBanLease {
		return nil, false
	}
	out := make(map[string]int, len(clusterIPs.counts))
	for name, count := range clusterIPs.counts {
		if count > 0 {
			out[name] = count
		}
	}
	return out, true
}

func ClusterIPActive() bool {
	clusterIPs.mu.RLock()
	defer clusterIPs.mu.RUnlock()
	return clusterIPs.active && time.Since(clusterIPs.updatedAt) <= clusterBanLease
}

func ClusterOnlineIPsOf(name string) ([]OnlineIP, bool) {
	clusterIPs.mu.RLock()
	defer clusterIPs.mu.RUnlock()
	if !clusterIPs.active || time.Since(clusterIPs.updatedAt) > clusterBanLease {
		return nil, false
	}
	if _, limited := clusterIPs.counts[name]; !limited {
		return nil, false
	}
	out := append([]OnlineIP{}, clusterIPs.list[name]...)
	return out, true
}
