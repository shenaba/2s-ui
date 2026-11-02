package core

import (
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

// A sample reads the user counters in place; the stats flush empties them on
// its own schedule. Bytes a flush took between two samples still belong to the
// second one, and the flush still gets every byte it would have without the
// sampler.
func TestSampleUsersCountsAcrossADrain(t *testing.T) {
	c := NewStatsTracker()
	read, write := c.getReadCounters("", "", "alice")
	start := time.Now()

	read[0].Add(500)
	// The first sample measures from the tracker's creation, when every
	// counter was zero.
	if moved, _, _ := c.SampleUsers(start, 0); moved["alice"] != (UserBytes{Up: 500}) {
		t.Errorf("first sample = %v, want alice {Up:500}", moved)
	}

	read[0].Add(1000)
	write[0].Add(4000)
	stats := *c.GetStats()
	var drained int64
	for _, s := range stats {
		drained += s.Traffic
	}
	if drained != 5500 {
		t.Errorf("flush drained %d bytes, want 5500: the sampler must not take any from it", drained)
	}
	read[0].Add(10)

	moved, window, _ := c.SampleUsers(start.Add(2*time.Second), 0)
	if window != 2*time.Second {
		t.Errorf("window = %v, want 2s", window)
	}
	if got := moved["alice"]; got != (UserBytes{Up: 1010, Down: 4000}) {
		t.Errorf("alice moved %+v, want {Up:1010 Down:4000}: what the flush took plus what came after it", got)
	}

	// Nothing since: the client is left out rather than reported at zero, and
	// the drain booked for the previous sample is not counted twice.
	moved, _, _ = c.SampleUsers(start.Add(4*time.Second), 0)
	if len(moved) != 0 {
		t.Errorf("idle sample = %v, want empty", moved)
	}
}

// Reset is what a closed box runs; the next sample measures from the Reset,
// not from a sample taken before it, and carries none of the old counts.
func TestSampleUsersAfterReset(t *testing.T) {
	c := NewStatsTracker()
	read, _ := c.getReadCounters("", "", "alice")
	read[0].Add(100)
	c.SampleUsers(time.Now(), 0)
	read[0].Add(100)
	c.Reset()
	moved, window, _ := c.SampleUsers(time.Now(), 0)
	if len(moved) != 0 {
		t.Errorf("first sample after Reset = %v, want empty", moved)
	}
	if window < 0 || window > time.Minute {
		t.Errorf("first sample after Reset: window %v, want it measured from the Reset", window)
	}
}

// A window shorter than the minimum is left open: nothing moves, and its bytes
// are still there for the sample that does close it.
func TestSampleUsersLeavesAShortWindowOpen(t *testing.T) {
	c := NewStatsTracker()
	read, _ := c.getReadCounters("", "", "alice")
	start := time.Now()
	c.SampleUsers(start, 0)

	read[0].Add(300)
	if moved, window, ok := c.SampleUsers(start.Add(10*time.Millisecond), time.Second); ok || moved != nil {
		t.Errorf("10ms sample with a 1s minimum = %v over %v (ok=%v), want it refused", moved, window, ok)
	}
	moved, window, ok := c.SampleUsers(start.Add(2*time.Second), time.Second)
	if !ok || window != 2*time.Second || moved["alice"] != (UserBytes{Up: 300}) {
		t.Errorf("next sample = %v over %v (ok=%v), want alice {Up:300} over 2s", moved, window, ok)
	}
}

func TestExtractTarget(t *testing.T) {
	if got := ExtractTarget("GitHub.com:443", M.ParseSocksaddr("1.2.3.4:443")); got != "github.com" {
		t.Errorf("ExtractTarget with domain: got %s, want github.com", got)
	}
	if got := ExtractTarget("", M.ParseSocksaddrHostPort("API.Google.COM", 443)); got != "api.google.com" {
		t.Errorf("ExtractTarget with dest fqdn: got %s, want api.google.com", got)
	}
	if got := ExtractTarget("", M.ParseSocksaddr("1.1.1.1:80")); got != "1.1.1.1" {
		t.Errorf("ExtractTarget with dest addr: got %s, want 1.1.1.1", got)
	}
}

func TestTargetStatsDrainsCorrectly(t *testing.T) {
	c := NewStatsTracker()
	read, write := c.getReadCounters("inbound-1", "direct", "alice", "example.com")
	if len(read) < 2 || len(write) < 2 {
		t.Fatalf("expected at least 2 counters (inbound/user/target), got read:%d write:%d", len(read), len(write))
	}
	// Target counters are appended
	targetRead := read[len(read)-1]
	targetWrite := write[len(write)-1]

	targetRead.Add(500)
	targetWrite.Add(1500)

	stats := *c.GetTargetStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 target stat, got %d", len(stats))
	}
	item := stats[0]
	if item.User != "alice" || item.Target != "example.com" || item.Up != 500 || item.Down != 1500 {
		t.Errorf("unexpected target stat: %+v", item)
	}

	// Next drain should be empty since counters were swapped
	stats2 := *c.GetTargetStats()
	if len(stats2) != 0 {
		t.Errorf("expected 0 target stats on next drain, got %d", len(stats2))
	}
}
