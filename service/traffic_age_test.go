package service

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

	"github.com/sagernet/sing-box/adapter"
)

// trafficAge is the switch for traffic history, and it has to act on a running
// panel: 0 empties target_stats and stops the core from counting, and turning
// it back on resumes, all without a restart.
//
// The value used to be captured when the stats job was registered, so a saved 0
// left the core counting and the next flush wrote the rows the settings save had
// just deleted.
func TestTrafficAgeZeroStopsTargetStatsWithoutRestart(t *testing.T) {
	runningPanel(t)
	// The settings form only carries rows that exist and Save only updates
	// them; GetAllSetting is what seeds the defaults on the first page load.
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	takePending()
	takePendingTargets()
	t.Cleanup(func() {
		takePending()
		takePendingTargets()
	})

	saveTrafficAge := func(value string) {
		t.Helper()
		data, err := json.Marshal(map[string]string{"trafficAge": value})
		if err != nil {
			t.Fatalf("marshal trafficAge=%s: %v", value, err)
		}
		tx := database.GetDB().Begin()
		if err = (&SettingService{}).Save(tx, data); err != nil {
			tx.Rollback()
			t.Fatalf("save trafficAge=%s: %v", value, err)
		}
		if err = tx.Commit().Error; err != nil {
			t.Fatalf("commit trafficAge=%s: %v", value, err)
		}
	}

	countTargets := func() int64 {
		t.Helper()
		var n int64
		if err := database.GetDB().Model(model.TargetStats{}).Count(&n).Error; err != nil {
			t.Fatalf("count target stats: %v", err)
		}
		return n
	}

	// One connection, attributed to the target it reaches. The counter is added
	// after the underlying write returns, hence the short wait before a flush.
	visit := func(domain string) {
		t.Helper()
		box := corePtr.GetInstance()
		near, far := net.Pipe()
		counted := box.StatsTracker().RoutedConnection(
			context.Background(), near,
			adapter.InboundContext{User: "alice", Domain: domain},
			nil, box.Outbound().Default())
		go func() { _, _ = io.Copy(io.Discard, far) }()
		if _, err := counted.Write(make([]byte, 500)); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = counted.Close()
		time.Sleep(50 * time.Millisecond)
	}

	flush := func(when string) {
		t.Helper()
		if err := (&StatsService{}).SaveStatsForCurrentSettings(10); err != nil {
			t.Fatalf("flush (%s): %v", when, err)
		}
	}

	// History on: the visit is recorded.
	visit("example.com")
	flush("history on")
	if got := countTargets(); got == 0 {
		t.Fatal("history on: the visit was not recorded")
	}

	// Saving 0 empties target_stats in the same transaction as the setting.
	saveTrafficAge("0")
	if got := countTargets(); got != 0 {
		t.Fatalf("setting trafficAge=0 left %d target rows behind", got)
	}

	// A visit counted between the save and the next flush must be dropped, and
	// that flush is what has to stop the core from counting.
	visit("late.example.com")
	flush("history off")
	if got := countTargets(); got != 0 {
		t.Fatalf("a flush after trafficAge=0 wrote %d target rows", got)
	}
	if corePtr.GetInstance().StatsTracker().IsTrafficEnabled() {
		t.Error("the core is still counting target stats with trafficAge=0")
	}

	// The way back works without a restart too.
	saveTrafficAge("30")
	flush("history back on")
	if !corePtr.GetInstance().StatsTracker().IsTrafficEnabled() {
		t.Fatal("the core did not resume counting after trafficAge went back up")
	}
	visit("back.example.com")
	flush("history back on")
	if got := countTargets(); got == 0 {
		t.Error("history back on: the visit was not recorded")
	}
}

// A panel that was already at trafficAge=0 before it upgraded must start empty.
// The previous build captured the switch when the stats job was registered and
// kept writing target sites after the setting said otherwise; those rows are
// still served by api/targetStats and copied into every backup until something
// clears them, and re-saving an untouched setting is not something an operator
// can be expected to know.
func TestPurgeTrafficHistoryIfDisabled(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "purge_history.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()
	if err := db.Create(&model.Setting{Key: "trafficAge", Value: "0"}).Error; err != nil {
		t.Fatalf("seed trafficAge: %v", err)
	}
	if err := db.Create(&model.TargetStats{
		DateTime: time.Now().Unix(), User: "alice", Target: "left.example.com", Up: 1,
	}).Error; err != nil {
		t.Fatalf("seed target row: %v", err)
	}

	if err := (&StatsService{}).PurgeTrafficHistoryIfDisabled(); err != nil {
		t.Fatalf("purge: %v", err)
	}
	count := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(model.TargetStats{}).Count(&n).Error; err != nil {
			t.Fatalf("count target stats: %v", err)
		}
		return n
	}
	if got := count(); got != 0 {
		t.Fatalf("trafficAge=0 kept %d rows across a restart", got)
	}

	// With history on, nothing is touched.
	if err := db.Model(model.Setting{}).Where("key = ?", "trafficAge").Update("value", "30").Error; err != nil {
		t.Fatalf("raise trafficAge: %v", err)
	}
	if err := db.Create(&model.TargetStats{
		DateTime: time.Now().Unix(), User: "alice", Target: "kept.example.com", Up: 1,
	}).Error; err != nil {
		t.Fatalf("seed target row: %v", err)
	}
	if err := (&StatsService{}).PurgeTrafficHistoryIfDisabled(); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("trafficAge=30 deleted rows: %d left", got)
	}
}
