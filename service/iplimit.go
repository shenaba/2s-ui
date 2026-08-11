package service

import (
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
)

const (
	// How long an evicted IP stays refused. Without this the scan would drop a
	// connection only for the client to redial instantly and be dropped again
	// next round -- the loop 3x-ui hands to fail2ban, kept in-process here.
	ipBanTTL = 5 * time.Minute

	// An IP whose newest connection has moved no bytes for this long is ignored:
	// neither counted against the limit nor evicted. UDP sessions linger in the
	// tracker until sing-box's NAT times them out, so without this a phone
	// moving from Wi-Fi to cellular would keep the dead session's slot and get
	// its live one evicted and banned.
	//
	// Note what "moved no bytes" actually measures: reads and writes on the
	// tunnel socket, not whether the user is doing something. A client that
	// buffers far ahead of what it consumes fills its receive window, TCP flow
	// control freezes the tunnel, and no bytes cross for as long as it takes to
	// drain -- measured at over four minutes against a rate-limited download.
	// Such a connection reads as idle and gives up its slot. That errs towards
	// letting someone through, which is the safe direction here, but it is why
	// this is minutes rather than seconds.
	ipIdleGrace = 90 * time.Second

	// Stolen credentials plus rotating source IPs can grow the ban map without
	// bound; evict the soonest-expiring entries rather than tracking every IP
	// an attacker cares to try.
	ipBanMaxSize = 1024
)

type ipBanKey struct {
	user string
	ip   netip.Addr
}

// ipLimiter holds everything the feature remembers between scans. It is
// package-level, like corePtr and onlineResources, and that is the point: Box
// and its ConnTracker are rebuilt on every core Start, so state kept there
// would hand every evicted IP a fresh five-minute reprieve each time the core
// restarted.
type ipLimiter struct {
	mu   sync.RWMutex
	bans map[ipBanKey]int64 // -> unix seconds when the ban lifts

	// client -> IP -> when it was first admitted, in the tracker's monotonic
	// basis. Seniority is remembered rather than re-derived from live
	// connections: an IP whose oldest connection just ended would otherwise
	// look newly arrived and lose its slot to a newcomer.
	holders map[string]map[netip.Addr]int64

	// Which tracker instance the recorded seniorities were measured against.
	// Comparing monotonic readings instead would not be reliable: a core that
	// ran briefly before crashing leaves a small one behind, and its
	// replacement passes that within seconds while still being a fresh epoch.
	lastGeneration uint64
}

var ipLimits = &ipLimiter{
	bans:    make(map[ipBanKey]int64),
	holders: make(map[string]map[netip.Addr]int64),
}

// allow is the core.ConnGate: it is consulted on every routed connection, so it
// stays a single map lookup and holds only a read lock.
func (l *ipLimiter) allow(user string, source netip.Addr) bool {
	l.mu.RLock()
	until, banned := l.bans[ipBanKey{user: user, ip: source}]
	l.mu.RUnlock()
	if !banned {
		return true
	}
	// Expiry is checked here as well as in sweep so a ban never outlives its TTL
	// just because no scan has run since.
	return time.Now().Unix() >= until
}

func (l *ipLimiter) ban(user string, ips []netip.Addr, nowUnix int64) {
	if len(ips) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ip := range ips {
		l.bans[ipBanKey{user: user, ip: ip}] = nowUnix + int64(ipBanTTL.Seconds())
	}
	l.evictBansLocked(nowUnix)
}

func (l *ipLimiter) sweep(nowUnix int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(nowUnix)
}

func (l *ipLimiter) sweepLocked(nowUnix int64) {
	for key, until := range l.bans {
		if nowUnix >= until {
			delete(l.bans, key)
		}
	}
}

// evictBansLocked keeps the ban map bounded, dropping expired entries first and
// then the soonest to expire.
func (l *ipLimiter) evictBansLocked(nowUnix int64) {
	l.sweepLocked(nowUnix)
	if len(l.bans) <= ipBanMaxSize {
		return
	}
	type banEntry struct {
		key   ipBanKey
		until int64
	}
	entries := make([]banEntry, 0, len(l.bans))
	for key, until := range l.bans {
		entries = append(entries, banEntry{key: key, until: until})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].until < entries[j].until })
	for _, entry := range entries[:len(l.bans)-ipBanMaxSize] {
		delete(l.bans, entry.key)
	}
}

// beginScan returns the seniority record to plan against, discarding it
// wholesale when it was measured against a different tracker: the core restarted
// in between, and the stored timestamps mean nothing against the new epoch.
//
// The result is a copy. The caller reads it after the lock is gone, and a
// second concurrent caller -- or a commit that ever becomes an in-place update
// rather than a whole-map swap -- would otherwise be a concurrent map access,
// which the Go runtime turns into an unrecoverable fatal error rather than a
// catchable panic.
func (l *ipLimiter) beginScan(generation uint64) map[string]map[netip.Addr]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if generation != l.lastGeneration {
		l.holders = make(map[string]map[netip.Addr]int64)
		l.lastGeneration = generation
	}
	held := make(map[string]map[netip.Addr]int64, len(l.holders))
	for name, ips := range l.holders {
		heldIPs := make(map[netip.Addr]int64, len(ips))
		for addr, at := range ips {
			heldIPs[addr] = at
		}
		held[name] = heldIPs
	}
	return held
}

// commit replaces the seniority record. Clients absent from admitted (limit
// removed, client deleted, nobody connected) drop out, which is what keeps the
// map from growing forever.
func (l *ipLimiter) commit(admitted map[string]map[netip.Addr]int64) {
	l.mu.Lock()
	l.holders = admitted
	l.mu.Unlock()
}

// planIPLimit decides which of one client's source IPs keep their slot and which
// lose every connection, first-come-first-served.
//
// Pure by design: the tracker snapshot, the clock and the previous admissions
// all arrive as arguments, so the whole "who gets kicked" rule is verifiable
// without sing-box, a database, or a real second passing -- the same stance
// service/acme_test.go takes toward nginx.
//
//	held      IP -> admittedAt from the previous scan.
//	active    what the tracker currently reports for this client.
//	limit     0 or less means unlimited; nothing is ever dropped.
//	now, idleGrace   monotonic nanoseconds in the tracker's basis.
func planIPLimit(
	held map[netip.Addr]int64,
	active map[netip.Addr]core.IPWindow,
	limit int,
	now int64,
	idleGrace int64,
) (map[netip.Addr]int64, []netip.Addr) {
	if limit <= 0 || len(active) == 0 {
		return nil, nil
	}

	type candidate struct {
		addr netip.Addr
		at   int64
	}
	candidates := make([]candidate, 0, len(active))
	for addr, window := range active {
		if now-window.LastSeen > idleGrace {
			continue
		}
		at, seen := held[addr]
		if !seen {
			at = window.FirstSeen
		}
		candidates = append(candidates, candidate{addr: addr, at: at})
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Ties break on the address so two IPs sharing a timestamp cannot swap
	// places every round on map iteration order alone.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at != candidates[j].at {
			return candidates[i].at < candidates[j].at
		}
		return candidates[i].addr.Compare(candidates[j].addr) < 0
	})

	admitted := make(map[netip.Addr]int64, min(len(candidates), limit))
	var drop []netip.Addr
	for i, c := range candidates {
		if i < limit {
			admitted[c.addr] = c.at
			continue
		}
		drop = append(drop, c.addr)
	}
	return admitted, drop
}

// ---------- snapshot published to the panel ----------

var (
	ipCountMu     sync.RWMutex
	ipCountsValue = map[string]int{}
)

// setIPCounts publishes a freshly built snapshot in one guarded swap, mirroring
// setOnlines. Only clients with at least one admitted IP are included: the UI
// falls back to 0, so listing every limited client would cost a full map on
// every push to say nothing.
func setIPCounts(counts map[string]int) {
	if counts == nil {
		counts = map[string]int{}
	}
	ipCountMu.Lock()
	ipCountsValue = counts
	ipCountMu.Unlock()
}

func GetIPCounts() map[string]int {
	ipCountMu.RLock()
	defer ipCountMu.RUnlock()
	out := make(map[string]int, len(ipCountsValue))
	for name, n := range ipCountsValue {
		out[name] = n
	}
	return out
}

// ---------- the cron entry ----------

// EnforceIPLimits scans the live connection table, evicts whatever exceeds each
// client's cap and republishes the per-client counts. A deployment that uses no
// limits pays one indexed query per run and nothing else.
func EnforceIPLimits() {
	limits, err := loadIPLimits()
	if err != nil {
		logger.Warning("ip limit: read limits:", err)
		return
	}
	// Gates the per-packet timestamping in core; see core.SetIPLimitActive.
	core.SetIPLimitActive(len(limits) > 0)
	if len(limits) == 0 {
		setIPCounts(nil)
		ipLimits.commit(nil)
		return
	}

	tracker := liveConnTracker()
	if tracker == nil {
		// Nothing is connected, so no counts and no seniority -- but bans stay,
		// they are wall-clock and outliving a core restart is the whole point.
		setIPCounts(nil)
		ipLimits.commit(nil)
		return
	}

	active, now := tracker.UserIPs()
	held := ipLimits.beginScan(tracker.Generation())
	idleGrace := int64(ipIdleGrace)

	counts := make(map[string]int, len(limits))
	admittedAll := make(map[string]map[netip.Addr]int64, len(limits))
	drop := make(map[string]map[netip.Addr]struct{})
	nowUnix := time.Now().Unix()

	for name, limit := range limits {
		admitted, dropped := planIPLimit(held[name], active[name], limit, now, idleGrace)
		if len(admitted) > 0 {
			admittedAll[name] = admitted
			counts[name] = len(admitted)
		}
		if len(dropped) == 0 {
			continue
		}
		set := make(map[netip.Addr]struct{}, len(dropped))
		for _, addr := range dropped {
			set[addr] = struct{}{}
		}
		drop[name] = set
		ipLimits.ban(name, dropped, nowUnix)
		logger.Info("ip limit: client ", name, " exceeded its ", limit,
			"-IP limit, dropping ", len(dropped), " address(es) for ", ipBanTTL)
	}

	ipLimits.commit(admittedAll)
	setIPCounts(counts)
	if len(drop) > 0 {
		tracker.CloseConnByUserIPs(drop)
	}
	ipLimits.sweep(nowUnix)
}

func loadIPLimits() (map[string]int, error) {
	var rows []struct {
		Name    string
		LimitIp int
	}
	err := database.GetDB().Model(model.Client{}).
		Select("`name`, `limit_ip`").
		Where("limit_ip > 0 AND enable = ?", true).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	limits := make(map[string]int, len(rows))
	for _, row := range rows {
		limits[row.Name] = row.LimitIp
	}
	return limits, nil
}

// liveConnTracker returns the running core's tracker, or nil. GetInstance can
// return nil even while IsRunning reports true, which the older callers in
// inbounds.go do not check for.
func liveConnTracker() *core.ConnTracker {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	box := corePtr.GetInstance()
	if box == nil {
		return nil
	}
	return box.ConnTracker()
}

// ---------- read model for the UI ----------

type OnlineIP struct {
	IP    string `json:"ip"`
	Since int64  `json:"since"` // unix seconds, converted from the monotonic basis
	Idle  bool   `json:"idle"`
}

// OnlineIPsOf lists one client's live source IPs, oldest first. A stopped core
// means "nobody is connected", not an error.
func OnlineIPsOf(name string) []OnlineIP {
	tracker := liveConnTracker()
	if tracker == nil || name == "" {
		return []OnlineIP{}
	}
	active, now := tracker.UserIPs()
	windows := active[name]
	if len(windows) == 0 {
		return []OnlineIP{}
	}

	wallNow := time.Now()
	idleGrace := int64(ipIdleGrace)
	out := make([]OnlineIP, 0, len(windows))
	for addr, window := range windows {
		out = append(out, OnlineIP{
			IP:    addr.String(),
			Since: wallNow.Add(-time.Duration(now - window.FirstSeen)).Unix(),
			Idle:  now-window.LastSeen > idleGrace,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Since != out[j].Since {
			return out[i].Since < out[j].Since
		}
		return out[i].IP < out[j].IP
	})
	return out
}
