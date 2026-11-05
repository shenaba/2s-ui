package cronjob

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// The jobs log through the package-level logger, which only exists once the
// panel has initialised it.
var initCronLogger sync.Once

// The cleanup reads trafficAge on every run instead of being given it when the
// cron entry is registered: 0 keeps no history at all, so there is nothing to
// age out -- and it must never be passed down as "delete everything older than
// now". Turning retention on without a restart has to start the cleanup too,
// which is why the job is registered unconditionally.
func TestDelStatsJobFollowsTrafficAge(t *testing.T) {
	initCronLogger.Do(func() { logger.InitLogger(logging.CRITICAL) })
	if err := database.InitDB(filepath.Join(t.TempDir(), "del_stats.db")); err != nil {
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
		DateTime: time.Now().AddDate(0, 0, -100).Unix(),
		User:     "alice",
		Target:   "old.example.com",
		Up:       1,
		Down:     2,
	}).Error; err != nil {
		t.Fatalf("seed target row: %v", err)
	}

	count := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(model.TargetStats{}).Count(&n).Error; err != nil {
			t.Fatalf("count target stats: %v", err)
		}
		return n
	}

	NewDelStatsJob().Run()
	if got := count(); got != 1 {
		t.Fatalf("trafficAge=0 deleted rows: %d left", got)
	}

	if err := db.Model(model.Setting{}).Where("key = ?", "trafficAge").Update("value", "30").Error; err != nil {
		t.Fatalf("raise trafficAge: %v", err)
	}
	NewDelStatsJob().Run()
	if got := count(); got != 0 {
		t.Fatalf("trafficAge=30 kept a 100-day-old row: %d left", got)
	}
}
