package service

import (
	"net/netip"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/core"
)

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return a
}

// live builds an IP window that is active as of now.
func live(firstSeen int64) core.IPWindow {
	return core.IPWindow{FirstSeen: firstSeen, LastSeen: firstSeen}
}

func dropSet(drop []netip.Addr) map[netip.Addr]struct{} {
	set := make(map[netip.Addr]struct{}, len(drop))
	for _, a := range drop {
		set[a] = struct{}{}
	}
	return set
}

func TestPlanIPLimit(t *testing.T) {
	ip1 := addr(t, "1.1.1.1")
	ip2 := addr(t, "2.2.2.2")
	ip3 := addr(t, "3.3.3.3")
	grace := int64(90 * time.Second)
	// Every window below is stamped relative to this reading, so "now" is late
	// enough that a deliberately stale LastSeen can sit outside the grace.
	now := int64(10 * time.Minute)

	fresh := func(firstSeen time.Duration) core.IPWindow {
		return core.IPWindow{FirstSeen: int64(firstSeen), LastSeen: now}
	}

	t.Run("unlimited never drops", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{ip1: fresh(0), ip2: fresh(1), ip3: fresh(2)}
		for _, limit := range []int{0, -1} {
			admitted, drop := planIPLimit(nil, active, limit, now, grace)
			if len(drop) != 0 {
				t.Errorf("limit %d dropped %v", limit, drop)
			}
			if len(admitted) != 0 {
				t.Errorf("limit %d admitted %v, expected the caller to skip counting", limit, admitted)
			}
		}
	})

	t.Run("under and at the limit", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{ip1: fresh(0), ip2: fresh(1)}
		for _, limit := range []int{2, 5} {
			admitted, drop := planIPLimit(nil, active, limit, now, grace)
			if len(drop) != 0 {
				t.Errorf("limit %d dropped %v while not over", limit, drop)
			}
			if len(admitted) != 2 {
				t.Errorf("limit %d admitted %d IPs, want 2", limit, len(admitted))
			}
		}
	})

	t.Run("newest loses, oldest keeps its slot", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{
			ip1: fresh(1 * time.Minute), // oldest
			ip2: fresh(2 * time.Minute),
			ip3: fresh(3 * time.Minute), // newest
		}
		admitted, drop := planIPLimit(nil, active, 2, now, grace)
		if len(drop) != 1 || drop[0] != ip3 {
			t.Fatalf("dropped %v, want only the newest (%v)", drop, ip3)
		}
		if _, ok := admitted[ip1]; !ok {
			t.Error("oldest IP lost its slot")
		}
		if _, ok := admitted[ip2]; !ok {
			t.Error("second-oldest IP lost its slot")
		}
	})

	// The whole reason lastActive exists: a QUIC session left behind by a
	// network switch must not hold the slot and get the live address evicted.
	t.Run("idle IP is neither counted nor dropped", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{
			ip1: {FirstSeen: 0, LastSeen: now - grace - 1}, // the corpse
			ip2: fresh(5 * time.Minute),                    // the live one
		}
		admitted, drop := planIPLimit(nil, active, 1, now, grace)
		if len(drop) != 0 {
			t.Errorf("dropped %v, want nothing: the only live IP fits the limit", drop)
		}
		if _, ok := admitted[ip1]; ok {
			t.Error("idle IP counted against the limit")
		}
		if _, ok := admitted[ip2]; !ok {
			t.Error("live IP not admitted while an idle one occupied the slot")
		}
	})

	// Seniority is remembered, not re-derived: an IP whose oldest connection
	// just ended reports a newer FirstSeen, and must not lose to a newcomer.
	t.Run("held IP keeps its seniority against a newcomer", func(t *testing.T) {
		held := map[netip.Addr]ipHold{ip1: {at: int64(1 * time.Minute)}}
		active := map[netip.Addr]core.IPWindow{
			ip1: fresh(8 * time.Minute), // reconnected, looks new
			ip2: fresh(5 * time.Minute), // genuinely newer than ip1's admission
		}
		admitted, drop := planIPLimit(held, active, 1, now, grace)
		if len(drop) != 1 || drop[0] != ip2 {
			t.Fatalf("dropped %v, want the newcomer (%v)", drop, ip2)
		}
		if admitted[ip1].at != int64(1*time.Minute) {
			t.Errorf("admitted[ip1].at = %d, want the original admission time preserved", admitted[ip1].at)
		}
		if admitted[ip1].lastSeen != now {
			t.Errorf("admitted[ip1].lastSeen = %d, want this scan's %d", admitted[ip1].lastSeen, now)
		}
	})

	t.Run("ties break on the address, deterministically", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{ip1: fresh(time.Minute), ip2: fresh(time.Minute)}
		first, _ := planIPLimit(nil, active, 1, now, grace)
		// Re-running must pick the same winner; map iteration order alone would
		// otherwise make the two swap places every round.
		for i := 0; i < 20; i++ {
			again, _ := planIPLimit(nil, active, 1, now, grace)
			if len(again) != 1 {
				t.Fatalf("admitted %d IPs, want 1", len(again))
			}
			for a := range first {
				if _, ok := again[a]; !ok {
					t.Fatalf("run %d picked a different winner", i)
				}
			}
		}
		if _, ok := first[ip1]; !ok {
			t.Errorf("expected the lower address to win the tie, got %v", first)
		}
	})

	// planIPLimit reports occupancy only; what survives into the next scan is
	// rememberHolders' call, and it is the one bounding the record.
	t.Run("only active IPs are admitted", func(t *testing.T) {
		held := map[netip.Addr]ipHold{ip1: {}, ip2: {}, ip3: {}}
		active := map[netip.Addr]core.IPWindow{ip1: fresh(time.Minute)}
		admitted, _ := planIPLimit(held, active, 5, now, grace)
		if len(admitted) != 1 {
			t.Fatalf("admitted %v, want only the still-active IP", admitted)
		}
		if _, ok := admitted[ip1]; !ok {
			t.Error("active IP missing from the admissions")
		}
	})

	t.Run("no active IPs", func(t *testing.T) {
		admitted, drop := planIPLimit(map[netip.Addr]ipHold{ip1: {}}, nil, 2, now, grace)
		if len(admitted) != 0 || len(drop) != 0 {
			t.Errorf("admitted %v drop %v, want both empty", admitted, drop)
		}
	})

	t.Run("every IP over the limit is dropped", func(t *testing.T) {
		active := map[netip.Addr]core.IPWindow{
			ip1: fresh(1 * time.Minute),
			ip2: fresh(2 * time.Minute),
			ip3: fresh(3 * time.Minute),
		}
		admitted, drop := planIPLimit(nil, active, 1, now, grace)
		if len(admitted) != 1 {
			t.Fatalf("admitted %d IPs, want 1", len(admitted))
		}
		set := dropSet(drop)
		if len(set) != 2 {
			t.Fatalf("dropped %v, want the two losers", drop)
		}
		if _, ok := set[ip1]; ok {
			t.Error("the oldest IP was dropped")
		}
	})
}

// A scan cannot tell a departed address from a redialling one, and the gap only
// has to span a single 10s round to look like the former. Forgetting on that
// hands a long-standing address a brand-new arrival time, so the next round
// ranks it below whatever connected during the gap and evicts -- and bans -- the
// wrong one.
func TestRememberHolders(t *testing.T) {
	ip1 := addr(t, "1.1.1.1")
	ip2 := addr(t, "2.2.2.2")
	ip3 := addr(t, "3.3.3.3")
	now := int64(10 * time.Minute)
	// The shipped value, so the boundary cases below exercise what actually runs.
	memory := int64(ipHoldMemory)

	// This window is also one in which an address that left can come back and
	// outrank one that stayed, and eviction bans for five minutes -- so it has to
	// stay near the redial it exists for. Sizing it from ipIdleGrace once made it
	// 90s: long enough for a phone that lost signal to evict, and lock out, a
	// laptop that had been working the whole time.
	t.Run("the memory spans a redial, not an absence", func(t *testing.T) {
		const scanInterval = 10 * time.Second // cronjob's @every 10s
		if ipHoldMemory <= scanInterval {
			t.Errorf("ipHoldMemory = %s, too short to survive a gap spanning one scan", ipHoldMemory)
		}
		if ipHoldMemory >= ipIdleGrace {
			t.Errorf("ipHoldMemory = %s, want it well under ipIdleGrace (%s)", ipHoldMemory, ipIdleGrace)
		}
	})

	t.Run("an address that blipped keeps its place", func(t *testing.T) {
		// ip1 held a slot for hours and is missing from this round's admissions,
		// but ip2 stayed up throughout -- so occupancy alone says nothing is wrong.
		held := map[netip.Addr]ipHold{
			ip1: {at: int64(time.Minute), lastSeen: now - int64(10*time.Second)},
			ip2: {at: int64(2 * time.Minute), lastSeen: now - int64(10*time.Second)},
		}
		admitted := map[netip.Addr]ipHold{ip2: {at: int64(2 * time.Minute), lastSeen: now}}

		record := rememberHolders(held, admitted, nil, now, memory)
		if got, ok := record[ip1]; !ok || got.at != int64(time.Minute) {
			t.Errorf("record[ip1] = %+v (present=%v), want its original admission time", got, ok)
		}
		if record[ip2].lastSeen != now {
			t.Errorf("record[ip2].lastSeen = %d, want this scan's %d", record[ip2].lastSeen, now)
		}
	})

	t.Run("a client with nothing connected keeps its record", func(t *testing.T) {
		held := map[netip.Addr]ipHold{ip1: {at: 5, lastSeen: now - int64(time.Second)}}
		record := rememberHolders(held, nil, nil, now, memory)
		if len(record) != 1 || record[ip1].at != 5 {
			t.Errorf("record = %v, want ip1's seniority carried through the gap", record)
		}
	})

	// Without this the record grows for the lifetime of the process, one entry
	// per address the client has ever used.
	t.Run("an address gone longer than the memory is forgotten", func(t *testing.T) {
		held := map[netip.Addr]ipHold{
			ip1: {at: 5, lastSeen: now - memory},     // exactly at the edge, kept
			ip2: {at: 5, lastSeen: now - memory - 1}, // one past it, forgotten
		}
		record := rememberHolders(held, nil, nil, now, memory)
		if _, ok := record[ip1]; !ok {
			t.Error("an address inside the memory window was forgotten")
		}
		if _, ok := record[ip2]; ok {
			t.Error("an address past the memory window was kept")
		}
	})

	// It just lost the ranking and is being banned; giving it back the seniority
	// it lost with would hand the slot straight over when the ban lifts.
	t.Run("a dropped address does not keep its seniority", func(t *testing.T) {
		held := map[netip.Addr]ipHold{ip3: {at: 5, lastSeen: now}}
		admitted := map[netip.Addr]ipHold{ip1: {at: 1, lastSeen: now}}
		record := rememberHolders(held, admitted, []netip.Addr{ip3}, now, memory)
		if _, ok := record[ip3]; ok {
			t.Error("the evicted address kept its place in the queue")
		}
	})

	t.Run("nothing to remember returns nil", func(t *testing.T) {
		if record := rememberHolders(nil, nil, nil, now, memory); record != nil {
			t.Errorf("record = %v, want nil so the client drops out of the map", record)
		}
	})
}

func newTestLimiter() *ipLimiter {
	return &ipLimiter{
		bans:    make(map[ipBanKey]ipBan),
		holders: make(map[string]map[netip.Addr]ipHold),
	}
}

func TestIPBansExpire(t *testing.T) {
	ip := addr(t, "1.1.1.1")
	other := addr(t, "2.2.2.2")
	limiter := newTestLimiter()
	// Wall-clock seconds; allow reads through the real-clock allow() below.
	now := time.Now().Unix()

	limiter.ban("alice", []netip.Addr{ip}, now, 1)
	if limiter.allow("alice", ip) {
		t.Error("a just-banned IP was allowed")
	}
	if !limiter.allow("alice", other) {
		t.Error("an unbanned IP of the same client was refused")
	}
	if !limiter.allow("bob", ip) {
		t.Error("the ban leaked to a different client")
	}

	// Bans are keyed to wall clock so they survive a core restart; expiring one
	// means moving the clock past the TTL, which sweep takes as a parameter.
	limiter.sweep(now+int64(ipBanTTL.Seconds())+1, nil)
	if len(limiter.bans) != 0 {
		t.Errorf("sweep left %d expired ban(s)", len(limiter.bans))
	}
	if !limiter.allow("alice", ip) {
		t.Error("an expired ban still refused the IP")
	}
}

// A ban outliving the situation that caused it locks out exactly the client the
// limit is not aimed at: the one that moved networks and is now under its cap.
func TestSweepReleasesBansThatNoLongerHoldASlot(t *testing.T) {
	ip := addr(t, "1.1.1.1")
	now := time.Now().Unix()
	const gen = uint64(3)

	banned := func() *ipLimiter {
		l := newTestLimiter()
		l.ban("alice", []netip.Addr{ip}, now, gen)
		return l
	}

	t.Run("still at the cap keeps it", func(t *testing.T) {
		l := banned()
		l.sweep(now, &banScope{generation: gen, capped: map[string]bool{"alice": true}})
		if l.allow("alice", ip) {
			t.Error("the ban was lifted while the client still fills its cap")
		}
	})

	t.Run("back under the cap releases it", func(t *testing.T) {
		l := banned()
		l.sweep(now, &banScope{generation: gen, capped: map[string]bool{"alice": false}})
		if !l.allow("alice", ip) {
			t.Error("a client under its cap is still locked out")
		}
	})

	// Raising or clearing the limit, disabling the client, deleting it: all of
	// them drop the name from the scan, and none leaves a slot worth protecting.
	t.Run("no longer limited releases it whatever the generation", func(t *testing.T) {
		l := banned()
		l.sweep(now, &banScope{generation: gen + 1, capped: map[string]bool{}})
		if !l.allow("alice", ip) {
			t.Error("clearing the limit did not lift the ban")
		}
	})

	// The reason bans live outside Box: a restart disconnects everyone, so every
	// client reads as under its cap and occupancy alone cannot tell the two apart.
	t.Run("a core restart does not count as room", func(t *testing.T) {
		l := banned()
		l.sweep(now, &banScope{generation: gen + 1, capped: map[string]bool{"alice": false}})
		if l.allow("alice", ip) {
			t.Error("a core restart forgave the ban")
		}
	})
}

// A core restart rebuilds the tracker and restarts its clock, so seniority
// recorded against the previous one no longer shares a basis with the readings
// it gets compared to.
func TestBeginScanDiscardsHoldersFromAnotherTracker(t *testing.T) {
	ip := addr(t, "1.1.1.1")
	newLimiter := func() *ipLimiter {
		return &ipLimiter{
			bans:           make(map[ipBanKey]ipBan),
			holders:        map[string]map[netip.Addr]ipHold{"alice": {ip: {at: 500}}},
			lastGeneration: 7,
		}
	}

	t.Run("same tracker keeps the record", func(t *testing.T) {
		if held := newLimiter().beginScan(7); len(held["alice"]) != 1 {
			t.Error("the record was discarded while the tracker had not changed")
		}
	})

	// The decisive case for using generations over clock readings: a core that
	// ran five seconds before crashing leaves a five-second reading behind, and
	// its replacement passes that within seconds while being a fresh epoch. A
	// "did the clock go backwards" test would not fire here.
	t.Run("a new tracker discards it even when its clock reads higher", func(t *testing.T) {
		if held := newLimiter().beginScan(8); len(held) != 0 {
			t.Errorf("seniority survived a tracker swap: %v", held)
		}
	})

	// The caller reads this after the lock is released; handing out the live map
	// would make a second caller a concurrent map access, which is fatal in Go
	// rather than a catchable panic.
	t.Run("the returned record is a copy", func(t *testing.T) {
		limiter := newLimiter()
		held := limiter.beginScan(7)
		held["alice"][ip] = ipHold{at: 999}
		delete(held, "alice")

		limiter.mu.RLock()
		defer limiter.mu.RUnlock()
		if hold := limiter.holders["alice"][ip]; hold.at != 500 {
			t.Errorf("mutating the returned record reached the limiter: got %d, want 500", hold.at)
		}
	})
}
