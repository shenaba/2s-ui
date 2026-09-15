package service

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func resetPending() {
	pendingMu.Lock()
	pendingStats = nil
	pendingMu.Unlock()
}

func pendingLen() int {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return len(pendingStats)
}

// GetStats is destructive, so a flush whose transaction fails has already taken
// the bytes out of the core and nothing else remembers them. The rows owed to
// the database are carried to the next cycle instead.
func TestPendingStatsCarryAndClear(t *testing.T) {
	resetPending()
	t.Cleanup(resetPending)

	if got := takePending(); got != nil {
		t.Errorf("a fresh buffer should hand back nothing, got %d rows", len(got))
	}

	batch := []model.Stats{
		{Resource: "user", Tag: "alice", Direction: true, Traffic: 100},
		{Resource: "user", Tag: "alice", Direction: false, Traffic: 200},
	}
	holdPending(batch)
	if pendingLen() != 2 {
		t.Fatalf("held %d rows, want 2", pendingLen())
	}

	got := takePending()
	if len(got) != 2 {
		t.Errorf("took %d rows, want 2", len(got))
	}
	if pendingLen() != 0 {
		t.Error("taking the buffer must clear it, or the next cycle writes the rows twice")
	}
}

// A panel whose database has been refusing writes for hours should lose the
// oldest accounting rather than grow until it is killed for it.
func TestPendingStatsAreBounded(t *testing.T) {
	resetPending()
	t.Cleanup(resetPending)

	over := make([]model.Stats, maxPendingStats+1000)
	for i := range over {
		over[i] = model.Stats{Resource: "user", Tag: "t", Traffic: int64(i)}
	}
	holdPending(over)

	if got := pendingLen(); got != maxPendingStats {
		t.Fatalf("held %d rows, want the cap of %d", got, maxPendingStats)
	}
	// The oldest go, not the newest: the newest are the ones the counters and
	// the table are furthest behind on.
	held := takePending()
	if held[0].Traffic != 1000 {
		t.Errorf("first retained row has traffic %d, want the oldest 1000 dropped", held[0].Traffic)
	}
	if held[len(held)-1].Traffic != int64(maxPendingStats+1000-1) {
		t.Errorf("last retained row has traffic %d, want the newest kept", held[len(held)-1].Traffic)
	}
}

// The retention purge filters on date_time alone, which the composite bucket
// index cannot serve from its third column -- so it needs one of its own.
func TestStatsHasADateTimeIndex(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "stats.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	var names []string
	if err := database.GetDB().Raw(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'stats'").
		Scan(&names).Error; err != nil {
		t.Fatalf("read indexes: %v", err)
	}
	for _, n := range names {
		if n == "idx_stats_date_time" {
			return
		}
	}
	t.Errorf("stats indexes = %v, want one on date_time alone", names)
}

// The purge deletes in bounded chunks so the write lock is released in between,
// and it has to keep going until everything past the window is gone.
func TestDelOldStatsRemovesEverythingPastTheWindow(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "stats.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	db := database.GetDB()
	old := int64(1000)       // far outside any retention window
	recent := int64(1 << 40) // far inside it
	// More than one chunk, so the loop has to run at least twice.
	rows := make([]model.Stats, 0, delOldStatsChunk+50)
	for i := 0; i < delOldStatsChunk+50; i++ {
		rows = append(rows, model.Stats{
			Resource: "user", Tag: "t", Direction: i%2 == 0, DateTime: old + int64(i),
		})
	}
	rows = append(rows, model.Stats{Resource: "user", Tag: "keep", DateTime: recent})
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := (&StatsService{}).DelOldStats(1); err != nil {
		t.Fatalf("DelOldStats: %v", err)
	}

	var left []model.Stats
	if err := db.Find(&left).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("%d rows left, want only the recent one", len(left))
	}
	if left[0].DateTime != recent {
		t.Errorf("the surviving row has date_time %d, want %d", left[0].DateTime, recent)
	}
}
