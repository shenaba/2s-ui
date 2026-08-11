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
		held := map[netip.Addr]int64{ip1: int64(1 * time.Minute)}
		active := map[netip.Addr]core.IPWindow{
			ip1: fresh(8 * time.Minute), // reconnected, looks new
			ip2: fresh(5 * time.Minute), // genuinely newer than ip1's admission
		}
		admitted, drop := planIPLimit(held, active, 1, now, grace)
		if len(drop) != 1 || drop[0] != ip2 {
			t.Fatalf("dropped %v, want the newcomer (%v)", drop, ip2)
		}
		if admitted[ip1] != int64(1*time.Minute) {
			t.Errorf("admitted[ip1] = %d, want the original admission time preserved", admitted[ip1])
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

	// Without this the record grows for the lifetime of the process.
	t.Run("held entries that are no longer active are dropped", func(t *testing.T) {
		held := map[netip.Addr]int64{ip1: 0, ip2: 0, ip3: 0}
		active := map[netip.Addr]core.IPWindow{ip1: fresh(time.Minute)}
		admitted, _ := planIPLimit(held, active, 5, now, grace)
		if len(admitted) != 1 {
			t.Fatalf("admitted %v, want only the still-active IP", admitted)
		}
		if _, ok := admitted[ip1]; !ok {
			t.Error("active IP missing from the new record")
		}
	})

	t.Run("no active IPs", func(t *testing.T) {
		admitted, drop := planIPLimit(map[netip.Addr]int64{ip1: 0}, nil, 2, now, grace)
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

func TestIPBansExpire(t *testing.T) {
	ip := addr(t, "1.1.1.1")
	other := addr(t, "2.2.2.2")
	limiter := &ipLimiter{
		bans:    make(map[ipBanKey]int64),
		holders: make(map[string]map[netip.Addr]int64),
	}
	// Wall-clock seconds; allow reads through the real-clock allow() below.
	now := time.Now().Unix()

	limiter.ban("alice", []netip.Addr{ip}, now)
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
	limiter.sweep(now + int64(ipBanTTL.Seconds()) + 1)
	if len(limiter.bans) != 0 {
		t.Errorf("sweep left %d expired ban(s)", len(limiter.bans))
	}
	if !limiter.allow("alice", ip) {
		t.Error("an expired ban still refused the IP")
	}
}

// A core restart rebuilds the tracker and restarts its clock, so seniority
// recorded against the previous one no longer shares a basis with the readings
// it gets compared to.
func TestBeginScanDiscardsHoldersFromAnotherTracker(t *testing.T) {
	ip := addr(t, "1.1.1.1")
	newLimiter := func() *ipLimiter {
		return &ipLimiter{
			bans:           make(map[ipBanKey]int64),
			holders:        map[string]map[netip.Addr]int64{"alice": {ip: 500}},
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
		held["alice"][ip] = 999
		delete(held, "alice")

		limiter.mu.RLock()
		defer limiter.mu.RUnlock()
		if at := limiter.holders["alice"][ip]; at != 500 {
			t.Errorf("mutating the returned record reached the limiter: got %d, want 500", at)
		}
	})
}
