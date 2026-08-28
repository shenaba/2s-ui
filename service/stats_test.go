package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// TopTags answers "which tags moved the most", which no other query does --
// GetStats is per-tag over time. Both directions are summed, because a report
// ranking uploads separately from downloads ranks nothing anyone asked about.
func TestTopTags(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "toptags.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()

	now := time.Now().Unix()
	rows := []model.Stats{
		{Resource: "inbound", Tag: "busy", DateTime: now - 60, Direction: true, Traffic: 300},
		{Resource: "inbound", Tag: "busy", DateTime: now - 60, Direction: false, Traffic: 200},
		{Resource: "inbound", Tag: "quiet", DateTime: now - 60, Direction: true, Traffic: 10},
		// Outside the window.
		{Resource: "inbound", Tag: "yesterday", DateTime: now - 90000, Direction: true, Traffic: 9999},
		// A different resource: a client tag must not be ranked among inbounds.
		{Resource: "user", Tag: "alice", DateTime: now - 60, Direction: true, Traffic: 500},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed %s/%s: %v", row.Resource, row.Tag, err)
		}
	}

	var stats StatsService
	got, err := stats.TopTags("inbound", now-3600, 10)
	if err != nil {
		t.Fatalf("TopTags: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (busy, quiet): %+v", len(got), got)
	}
	if got[0].Tag != "busy" || got[0].Traffic != 500 {
		t.Errorf("busiest inbound is %+v, want busy with both directions summed to 500", got[0])
	}
	if got[1].Tag != "quiet" {
		t.Errorf("second row is %+v, want quiet", got[1])
	}

	users, err := stats.TopTags("user", now-3600, 10)
	if err != nil {
		t.Fatalf("TopTags(user): %v", err)
	}
	if len(users) != 1 || users[0].Tag != "alice" {
		t.Errorf("client ranking is %+v, want just alice", users)
	}
}
