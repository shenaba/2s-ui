package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func TestTargetStatsQueries(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "target_stats.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	now := time.Now().Unix()
	db := database.GetDB()

	// Seed test data
	// alice: github.com (up: 100, down: 900), google.com (up: 50, down: 150)
	// bob: github.com (up: 200, down: 800), twitter.com (up: 300, down: 300)
	testRows := []model.TargetStats{
		{DateTime: now - 1800, User: "alice", Target: "github.com", Up: 100, Down: 900},
		{DateTime: now - 1800, User: "alice", Target: "google.com", Up: 50, Down: 150},
		{DateTime: now - 1800, User: "bob", Target: "github.com", Up: 200, Down: 800},
		{DateTime: now - 1800, User: "bob", Target: "twitter.com", Up: 300, Down: 300},
		// Stale data older than 1 day
		{DateTime: now - 90000, User: "alice", Target: "old.site.com", Up: 500, Down: 500},
	}
	if err := db.Create(&testRows).Error; err != nil {
		t.Fatalf("create test rows: %v", err)
	}

	svc := &StatsService{}

	// 1. Global stats (past 1 day)
	res, err := svc.GetTargetStats(TargetStatsQuery{
		Period:    "day",
		SortField: "total",
		SortOrder: "desc",
	})
	if err != nil {
		t.Fatalf("GetTargetStats global: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("global total sites: got %d, want 3", res.Total)
	}
	// Verify #1 is github.com (alice 1000 + bob 1000 = 2000)
	if len(res.Items) == 0 || res.Items[0].Target != "github.com" {
		t.Errorf("first item: got %+v, want github.com", res.Items)
	}
	if res.Items[0].Up != 300 || res.Items[0].Down != 1700 || res.Items[0].Total != 2000 {
		t.Errorf("github.com aggregated: got %+v, want up:300 down:1700 total:2000", res.Items[0])
	}
	if res.Summary.Total != 2800 {
		t.Errorf("summary total: got %d, want 2800", res.Summary.Total)
	}

	// 2. Filter by user alice (past 1 day)
	resAlice, err := svc.GetTargetStats(TargetStatsQuery{
		User:      "alice",
		Period:    "day",
		SortField: "down",
		SortOrder: "desc",
	})
	if err != nil {
		t.Fatalf("GetTargetStats alice: %v", err)
	}
	if resAlice.Total != 2 {
		t.Errorf("alice total sites: got %d, want 2", resAlice.Total)
	}
	if resAlice.Items[0].Target != "github.com" || resAlice.Items[0].Down != 900 {
		t.Errorf("alice first item: got %+v, want github.com down:900", resAlice.Items[0])
	}

	// 3. Search filter "goog"
	resSearch, err := svc.GetTargetStats(TargetStatsQuery{
		Period: "day",
		Search: "goog",
	})
	if err != nil {
		t.Fatalf("GetTargetStats search: %v", err)
	}
	if resSearch.Total != 1 || resSearch.Items[0].Target != "google.com" {
		t.Errorf("search result: got %+v, want 1 item google.com", resSearch)
	}

	// 4. Period filter "month" (includes record from 90,000s ago)
	resMonth, err := svc.GetTargetStats(TargetStatsQuery{
		Period: "month",
	})
	if err != nil {
		t.Fatalf("GetTargetStats month: %v", err)
	}
	if resMonth.Total != 4 {
		t.Errorf("month total sites: got %d, want 4", resMonth.Total)
	}
}
