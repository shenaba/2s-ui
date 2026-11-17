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

// The inbounds list shows one traffic figure per inbound. It cannot be the sum
// of that inbound's clients: a client's up/down is its total across every
// inbound it belongs to, so two inbounds sharing one busy client reported the
// same number. This is the figure the core actually measured per inbound,
// folded into the row's own counters by the flush -- the same ones the
// inbound's cap is judged against (#289), whether or not traffic history is
// kept.
func TestInboundTrafficAccumulatesOnTheRow(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)
	resetPending()
	t.Cleanup(resetPending)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "inboundtraffic.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()

	nodeId := uint(3)
	for _, inb := range []*model.Inbound{
		{Type: "mixed", Tag: "busy", Up: 500, Down: 1, Options: []byte(`{}`)},
		{Type: "mixed", Tag: "quiet", Options: []byte(`{}`)},
		{Type: "mixed", Tag: "remote", NodeId: &nodeId, Options: []byte(`{}`)},
	} {
		if err := db.Create(inb).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", inb.Tag, err)
		}
	}

	// What a stopped core leaves for the next flush. With no core running,
	// SaveStats commits exactly this.
	carryPending([]model.Stats{
		{Resource: "inbound", Tag: "busy", Direction: true, Traffic: 7},
		{Resource: "inbound", Tag: "busy", Direction: false, Traffic: 3},
		{Resource: "inbound", Tag: "quiet", Direction: false, Traffic: 10},
		// A replica's tag is counted on its node, never here.
		{Resource: "inbound", Tag: "remote", Direction: true, Traffic: 40},
		// An endpoint's traffic arrives as an inbound's; there is no row.
		{Resource: "inbound", Tag: "wg-endpoint", Direction: true, Traffic: 40},
		// A client's traffic under the same tag is not the inbound's.
		{Resource: "user", Tag: "busy", Direction: true, Traffic: 900},
	})
	var stats StatsService
	// Traffic history off: the counters must move all the same.
	if err := stats.SaveStats(false, 60); err != nil {
		t.Fatalf("save stats: %v", err)
	}

	want := map[string][2]int64{"busy": {507, 4}, "quiet": {0, 10}, "remote": {0, 0}}
	var rows []model.Inbound
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, inb := range rows {
		if w := want[inb.Tag]; inb.Up != w[0] || inb.Down != w[1] {
			t.Errorf("%s: up/down = %d/%d, want %d/%d", inb.Tag, inb.Up, inb.Down, w[0], w[1])
		}
	}
	var history int64
	db.Model(model.Stats{}).Count(&history)
	if history != 0 {
		t.Errorf("history off wrote %d stats rows", history)
	}

	got := stats.GetInboundTraffic()
	wantTotals := map[string]int64{"busy": 511, "quiet": 10}
	if len(got) != len(wantTotals) {
		t.Fatalf("totals %v, want %v (replicas left out)", got, wantTotals)
	}
	for tag, traffic := range wantTotals {
		if got[tag] != traffic {
			t.Errorf("total %q = %d, want %d", tag, got[tag], traffic)
		}
	}
}
