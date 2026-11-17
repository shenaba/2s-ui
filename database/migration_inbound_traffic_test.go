package database

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// The inbounds list used to show a sum over the stats table. The counters that
// replace it (#289) start from that sum, per direction, so the figure does not
// drop to zero on upgrade -- and a replica, never counted here, is left alone.
func TestMigrateInboundTraffic(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "inboundtraffic.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if err := db.Where("key = ?", migratedKeyInboundTraffic).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("clear flag: %v", err)
	}

	nodeId := uint(7)
	for _, inb := range []*model.Inbound{
		{Type: "mixed", Tag: "busy", Options: []byte(`{}`)},
		{Type: "mixed", Tag: "idle", Options: []byte(`{}`)},
		{Type: "mixed", Tag: "replica", NodeId: &nodeId, Options: []byte(`{}`)},
	} {
		if err := db.Create(inb).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", inb.Tag, err)
		}
	}
	for _, row := range []model.Stats{
		{Resource: "inbound", Tag: "busy", DateTime: 100, Direction: true, Traffic: 300},
		{Resource: "inbound", Tag: "busy", DateTime: 200, Direction: true, Traffic: 1},
		{Resource: "inbound", Tag: "busy", DateTime: 100, Direction: false, Traffic: 200},
		// Another resource under the same tag is not the inbound's.
		{Resource: "user", Tag: "busy", DateTime: 100, Direction: true, Traffic: 9000},
		{Resource: "inbound", Tag: "replica", DateTime: 100, Direction: true, Traffic: 50},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed stats: %v", err)
		}
	}

	if err := migrateInboundTraffic(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	want := map[string][2]int64{"busy": {301, 200}, "idle": {0, 0}, "replica": {0, 0}}
	var got []model.Inbound
	if err := db.Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	for _, inb := range got {
		if w := want[inb.Tag]; inb.Up != w[0] || inb.Down != w[1] {
			t.Errorf("%s: up/down = %d/%d, want %d/%d", inb.Tag, inb.Up, inb.Down, w[0], w[1])
		}
		if !inb.Enable {
			t.Errorf("%s: an inbound written without the column must read as enabled", inb.Tag)
		}
	}

	// Once: a second run must not re-seed over counters the flush has moved.
	if err := db.Model(model.Inbound{}).Where("tag = ?", "busy").Update("up", 5).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateInboundTraffic(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var busy model.Inbound
	db.Where("tag = ?", "busy").First(&busy)
	if busy.Up != 5 {
		t.Errorf("second run rewrote up to %d", busy.Up)
	}
}
