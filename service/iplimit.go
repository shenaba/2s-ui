package service

import (
	"net/netip"
	"slices"
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

	// How long an address that has stopped being visible keeps the seniority it
	// earned, so a redial does not come back as a stranger. Three consecutive
	// scans at the cronjob's @every 10s: enough that a gap has to survive several
	// rounds, not just land unluckily between two, and enough slack for a client
	// that backs off before retrying.
	//
	// Not ipIdleGrace, though the two look interchangeable. That one decides when
	// a still-connected address stops counting; this one decides how long a gone
	// address can return and outrank whatever arrived while it was away. At 90s a
	// phone that lost signal for over a minute would displace -- and, since
	// eviction bans, lock out for five minutes -- a laptop that had been working
	// the whole time. Sizing this from the scan interval instead bounds that
	// window to the span a redial actually needs, and bounds the retained record
	// with it: an address enters only by being admitted, so a client's record
	// holds at most (this / scan interval + 1) x its limit entries.
	ipHoldMemory = 30 * time.Second
)

// ipHold is one source IP's standing with a client. `at` is what ranks it
// against the others; `lastSeen` is only there to decide when to forget it.
// Both are monotonic nanoseconds in the owning tracker's basis.
type ipHold struct {
	at       int64
	lastSeen int64
}

type ipBanKey struct {
	user string
	ip   netip.Addr
}

// ipBan is one refused (client, IP) pair. The generation is the tracker the
// eviction was decided on, which is what lets sweep tell "the slot this ban
// protects has genuinely freed up" from "the core restarted and disconnected
// everyone" -- the latter reads identically from occupancy alone, and forgiving
// on it is exactly what keeping this list outside Box exists to prevent.
type ipBan struct {
	until      int64 // unix seconds when the ban lifts
	generation uint64
}

// banScope is what one enforcement scan observed, so sweep can drop the bans
// that are no longer doing any work.
type banScope struct {
	generation uint64
	// client -> is it still at or over its cap? A client missing from the map
	// is no longer limited at all: limit cleared, disabled, or deleted.
	capped map[string]bool
}

// ipLimiter holds everything the feature remembers between scans. It is
// package-level, like corePtr and onlineResources, and that is the point: Box
// and its ConnTracker are rebuilt on every core Start, so state kept there
// would hand every evicted IP a fresh five-minute reprieve each time the core
// restarted.
type ipLimiter struct {
	mu   sync.RWMutex
	bans map[ipBanKey]ipBan

	// client -> IP -> what that IP is owed. Seniority is remembered rather than
	// re-derived from live connections: an IP whose oldest connection just
	// ended would otherwise look newly arrived and lose its slot to a newcomer.
	holders map[string]map[netip.Addr]ipHold

	// Which tracker instance the recorded seniorities were measured against.
	// Comparing monotonic readings instead would not be reliable: a core that
	// ran briefly before crashing leaves a small one behind, and its
	// replacement passes that within seconds while still being a fresh epoch.
	lastGeneration uint64
}

var ipLimits = &ipLimiter{
	bans:    make(map[ipBanKey]ipBan),
	holders: make(map[string]map[netip.Addr]ipHold),
}

// allow is the core.ConnGate: it is consulted on every routed connection, so it
// stays a single map lookup and holds only a read lock.
func (l *ipLimiter) allow(user string, source netip.Addr) bool {
	l.mu.RLock()
	ban, banned := l.bans[ipBanKey{user: user, ip: source}]
	l.mu.RUnlock()
	if !banned {
		return true
	}
	// Expiry is checked here as well as in sweep so a ban never outlives its TTL
	// just because no scan has run since.
	return time.Now().Unix() >= ban.until
}

func (l *ipLimiter) ban(user string, ips []netip.Addr, nowUnix int64, generation uint64) {
	if len(ips) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ip := range ips {
		l.bans[ipBanKey{user: user, ip: ip}] = ipBan{
			until:      nowUnix + int64(ipBanTTL.Seconds()),
			generation: generation,
		}
	}
	l.evictBansLocked(nowUnix)
}

func (l *ipLimiter) sweep(nowUnix int64, scope *banScope) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(nowUnix, scope)
}

// sweepLocked drops expired bans and, when a scan reported what it saw, the
// ones enforcement no longer needs.
//
// A ban only exists to stop an evicted address redialling straight back into a
// full slot, so holding it once the slot frees punishes exactly the network
// switch this feature is most likely to hit: the client whose old session died
// is under its cap and still locked out for the rest of the TTL. Two ways a ban
// stops doing work, and they release on different terms:
//
//   - the client is no longer limited (limit cleared, disabled, deleted). There
//     is no slot left to protect, so this releases whatever tracker the ban was
//     placed against.
//   - the client is still limited but currently under its cap. This releases
//     only when the ban was placed against the tracker that just reported that:
//     after a core restart every client reads as under its cap because everyone
//     was disconnected, and those bans have to survive to their TTL or the
//     restart forgives them -- which is what this list lives outside Box to
//     avoid.
//
// A nil scope means no scan happened, i.e. expiry only.
func (l *ipLimiter) sweepLocked(nowUnix int64, scope *banScope) {
	for key, ban := range l.bans {
		if nowUnix >= ban.until {
			delete(l.bans, key)
			continue
		}
		if scope == nil {
			continue
		}
		stillCapped, limited := scope.capped[key.user]
		if !limited || (!stillCapped && ban.generation == scope.generation) {
			delete(l.bans, key)
		}
	}
}

// evictBansLocked keeps the ban map bounded, dropping expired entries first and
// then the soonest to expire.
func (l *ipLimiter) evictBansLocked(nowUnix int64) {
	l.sweepLocked(nowUnix, nil)
	if len(l.bans) <= ipBanMaxSize {
		return
	}
	type banEntry struct {
		key   ipBanKey
		until int64
	}
	entries := make([]banEntry, 0, len(l.bans))
	for key, ban := range l.bans {
		entries = append(entries, banEntry{key: key, until: ban.until})
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
func (l *ipLimiter) beginScan(generation uint64) map[string]map[netip.Addr]ipHold {
	l.mu.Lock()
	defer l.mu.Unlock()
	if generation != l.lastGeneration {
		l.holders = make(map[string]map[netip.Addr]ipHold)
		l.lastGeneration = generation
	}
	held := make(map[string]map[netip.Addr]ipHold, len(l.holders))
	for name, ips := range l.holders {
		heldIPs := make(map[netip.Addr]ipHold, len(ips))
		for addr, hold := range ips {
			heldIPs[addr] = hold
		}
		held[name] = heldIPs
	}
	return held
}

// commit replaces the seniority record. Clients absent from records (limit
// removed, client deleted, gone long enough to be forgotten) drop out, which is
// what keeps the map from growing forever.
func (l *ipLimiter) commit(records map[string]map[netip.Addr]ipHold) {
	l.mu.Lock()
	l.holders = records
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
//	held      IP -> what it was owed as of the previous scan.
//	active    what the tracker currently reports for this client.
//	limit     0 or less means unlimited; nothing is ever dropped.
//	now, idleGrace   monotonic nanoseconds in the tracker's basis.
//
// The returned map is only the addresses holding a slot right now, which is
// what the panel counts. What gets remembered for the next scan is
// rememberHolders' job.
func planIPLimit(
	held map[netip.Addr]ipHold,
	active map[netip.Addr]core.IPWindow,
	limit int,
	now int64,
	idleGrace int64,
) (map[netip.Addr]ipHold, []netip.Addr) {
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
		at := window.FirstSeen
		if hold, seen := held[addr]; seen {
			at = hold.at
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

	admitted := make(map[netip.Addr]ipHold, min(len(candidates), limit))
	var drop []netip.Addr
	for i, c := range candidates {
		if i < limit {
			admitted[c.addr] = ipHold{at: c.at, lastSeen: now}
			continue
		}
		drop = append(drop, c.addr)
	}
	return admitted, drop
}

// rememberHolders builds the record to carry into the next scan: everything
// admitted now, plus the addresses that have gone quiet recently enough to
// still be owed their place.
//
// Occupancy alone cannot tell "this address left" from "this address is
// redialling", and the gap only has to span one 10s scan to look like the
// former. Ranking on what the tracker reports in that instant would hand a
// client's long-standing address a brand-new arrival time, so the next scan
// ranks it below anything that connected during the gap and evicts -- and bans
// -- the address that had held the slot for hours. Keeping the record for
// ipHoldMemory covers the gap in both shapes it comes in: the client with
// nothing else connected, and the client whose other addresses stayed up the
// whole time.
//
// dropped is excluded on purpose. Those addresses just lost the ranking and are
// being banned; letting them keep the seniority they lost with would hand the
// slot straight back when the ban lifts.
func rememberHolders(
	held map[netip.Addr]ipHold,
	admitted map[netip.Addr]ipHold,
	dropped []netip.Addr,
	now int64,
	memory int64,
) map[netip.Addr]ipHold {
	record := make(map[netip.Addr]ipHold, len(admitted)+len(held))
	for addr, hold := range admitted {
		record[addr] = hold
	}
	for addr, hold := range held {
		if _, live := record[addr]; live {
			continue
		}
		if now-hold.lastSeen > memory {
			continue
		}
		if slices.Contains(dropped, addr) {
			continue
		}
		record[addr] = hold
	}
	if len(record) == 0 {
		return nil
	}
	return record
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
	nowUnix := time.Now().Unix()
	if len(limits) == 0 {
		setIPCounts(nil)
		ipLimits.commit(nil)
		// Nobody is limited, so no ban is protecting anything: an empty capped
		// map releases every one of them, whichever tracker they were placed on.
		ipLimits.sweep(nowUnix, &banScope{capped: map[string]bool{}})
		return
	}

	tracker := liveConnTracker()
	if tracker == nil {
		// Nothing is connected, so no counts and no seniority -- but bans stay,
		// they are wall-clock and outliving a core restart is the whole point.
		// Expiry only: with no scan there is nothing to say whose cap freed up.
		setIPCounts(nil)
		ipLimits.commit(nil)
		ipLimits.sweep(nowUnix, nil)
		return
	}

	generation := tracker.Generation()
	active, now := tracker.UserIPs(func(user string) bool {
		_, limited := limits[user]
		return limited
	})
	held := ipLimits.beginScan(generation)
	idleGrace := int64(ipIdleGrace)

	counts := make(map[string]int, len(limits))
	records := make(map[string]map[netip.Addr]ipHold, len(limits))
	drop := make(map[string]map[netip.Addr]struct{})
	capped := make(map[string]bool, len(limits))
	holdMemory := int64(ipHoldMemory)

	for name, limit := range limits {
		admitted, dropped := planIPLimit(held[name], active[name], limit, now, idleGrace)
		if len(admitted) > 0 {
			counts[name] = len(admitted)
		}
		if record := rememberHolders(held[name], admitted, dropped, now, holdMemory); record != nil {
			records[name] = record
		}
		// At the cap with no room to give away, or over it and being trimmed:
		// either way this client's bans are still holding a slot for someone.
		// Counted from the live admissions, not the record -- a remembered
		// address is not occupying anything.
		capped[name] = len(admitted) >= limit || len(dropped) > 0
		if len(dropped) == 0 {
			continue
		}
		set := make(map[netip.Addr]struct{}, len(dropped))
		for _, addr := range dropped {
			set[addr] = struct{}{}
		}
		drop[name] = set
		ipLimits.ban(name, dropped, nowUnix, generation)
		logger.Info("ip limit: client ", name, " exceeded its ", limit,
			"-IP limit, dropping ", len(dropped), " address(es) for ", ipBanTTL)
	}

	ipLimits.commit(records)
	setIPCounts(counts)
	if len(drop) > 0 {
		tracker.CloseConnByUserIPs(drop)
	}
	ipLimits.sweep(nowUnix, &banScope{generation: generation, capped: capped})
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
// return nil even while IsRunning reports true, so every caller goes through
// here rather than chaining off it.
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
	// Nil when no client has an IP limit: lastActive is only stamped while one
	// does, so answering from it there would report connection age as inactivity.
	// Unknown is the honest reading, and the UI renders no badge for it.
	Idle *bool `json:"idle"`
}

// identityLabel spells a normalized address the way it is actually counted. A
// v6 source was masked to a prefix at track time, and printing the masked
// address bare shows an admin one no client ever used -- and hides that the row
// stands for a whole /64 while a v4 row stands for one address.
func identityLabel(addr netip.Addr) string {
	if !addr.Is6() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, core.IPv6IdentityPrefixBits).String()
}

// OnlineIPsOf lists one client's live source IPs, oldest first. A stopped core
// means "nobody is connected", not an error.
func OnlineIPsOf(name string) []OnlineIP {
	tracker := liveConnTracker()
	if tracker == nil || name == "" {
		return []OnlineIP{}
	}
	active, now := tracker.UserIPs(func(user string) bool { return user == name })
	windows := active[name]
	if len(windows) == 0 {
		return []OnlineIP{}
	}

	wallNow := time.Now()
	idleGrace := int64(ipIdleGrace)
	idleKnown := core.IPLimitActive()
	out := make([]OnlineIP, 0, len(windows))
	for addr, window := range windows {
		var idle *bool
		if idleKnown {
			stale := now-window.LastSeen > idleGrace
			idle = &stale
		}
		out = append(out, OnlineIP{
			IP:    identityLabel(addr),
			Since: wallNow.Add(-time.Duration(now - window.FirstSeen)).Unix(),
			Idle:  idle,
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
