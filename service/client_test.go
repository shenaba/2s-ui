package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// One connection, or each pooled connection gets its own empty :memory: db.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}, &model.Node{}, &model.Tls{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// A node push must never be blocked by a name the node happens to use locally:
// runReconcile aborts the WHOLE round on a failed push, so one collision would
// stop that node syncing entirely — worse than the duplicate the check prevents.
func TestSaveNameCheckExemptsClusterPush(t *testing.T) {
	db := newTestDB(t)
	var svc ClientService

	local := model.Client{
		Name: "X", Group: "user",
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("seed local client: %v", err)
	}

	t.Run("normal create still rejects a duplicate", func(t *testing.T) {
		data := json.RawMessage(`{"name":"X","group":"user","inbounds":[],"links":[],"config":{}}`)
		if _, err := svc.Save(db, "new", data, "example.com"); err == nil {
			t.Fatal("duplicate name accepted on a normal create")
		}
	})

	t.Run("cluster push goes through", func(t *testing.T) {
		data := json.RawMessage(`{"name":"X","group":"@cluster","inbounds":[],"links":[],"config":{}}`)
		if _, err := svc.Save(db, "new", data, "example.com"); err != nil {
			t.Fatalf("cluster push rejected by the name check: %v", err)
		}
		var n int64
		db.Model(model.Client{}).Where("name = ?", "X").Count(&n)
		if n != 2 {
			t.Errorf("expected the pushed @cluster client alongside the local one, got %d rows", n)
		}
	})
}

// editbulk validates only the names that actually change: the SPA submits the
// list projection back unchanged, so the ordinary path must not trip the check,
// while an apiv2 caller can rename through here.
func TestSaveEditbulkRenames(t *testing.T) {
	db := newTestDB(t)
	var svc ClientService

	seed := func(name string) uint {
		c := model.Client{
			Name: name, Group: "user",
			Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
		}
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
		return c.Id
	}
	idA, idB := seed("a"), seed("b")

	row := func(id uint, name string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "group": "user",
			"inbounds": []uint{}, "links": []map[string]string{}, "config": map[string]any{},
		}
	}
	editbulk := func(rows ...map[string]any) error {
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		_, err = svc.Save(db, "editbulk", data, "example.com")
		return err
	}

	t.Run("rename onto an existing name is rejected", func(t *testing.T) {
		if err := editbulk(row(idA, "b")); err == nil {
			t.Error("rename onto a name another row holds was accepted")
		}
	})
	// Neither row is committed yet, so the table cannot show this collision.
	t.Run("two rows renamed to the same new name are rejected", func(t *testing.T) {
		if err := editbulk(row(idA, "same"), row(idB, "same")); err == nil {
			t.Error("batch-internal duplicate was accepted")
		}
	})
	t.Run("unchanged names pass", func(t *testing.T) {
		if err := editbulk(row(idA, "a"), row(idB, "b")); err != nil {
			t.Errorf("the SPA's ordinary submit was rejected: %v", err)
		}
	})

	// Nothing above may have renamed anything.
	var names []string
	if err := db.Model(model.Client{}).Order("id").Pluck("name", &names).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("names changed by a rejected batch: %v", names)
	}
}

// limit_ip rides in the client list projection, which is what editbulk submits
// back. Two mistakes zero it here: leaving it out of clientListColumns (the SPA
// never receives it, so the round-trip writes 0), or adding it to
// findInboundsChanges' fillOmitted list (the old value would overwrite whatever
// the request carried, making bulk edits of the limit impossible).
func TestSaveEditbulkKeepsLimitIp(t *testing.T) {
	db := newTestDB(t)
	var svc ClientService

	seeded := model.Client{
		Name: "a", Group: "user", LimitIp: 3,
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}
	if err := db.Create(&seeded).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Exactly what the SPA sends back: the list projection, unchanged.
	var projected []model.Client
	if err := db.Model(model.Client{}).Select(clientListColumns).Scan(&projected).Error; err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if len(projected) != 1 || projected[0].LimitIp != 3 {
		t.Fatalf("clientListColumns does not carry limit_ip: %+v", projected)
	}

	data, err := json.Marshal(projected)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := svc.Save(db, "editbulk", data, "example.com"); err != nil {
		t.Fatalf("editbulk: %v", err)
	}

	var after model.Client
	if err := db.Model(model.Client{}).Where("id = ?", seeded.Id).First(&after).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.LimitIp != 3 {
		t.Errorf("LimitIp = %d after a bulk edit that did not touch it, want 3", after.LimitIp)
	}

	t.Run("a new value goes through", func(t *testing.T) {
		projected[0].LimitIp = 5
		data, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := svc.Save(db, "editbulk", data, "example.com"); err != nil {
			t.Fatalf("editbulk: %v", err)
		}
		var after model.Client
		if err := db.Model(model.Client{}).Where("id = ?", seeded.Id).First(&after).Error; err != nil {
			t.Fatalf("read back: %v", err)
		}
		if after.LimitIp != 5 {
			t.Errorf("LimitIp = %d, want the submitted 5", after.LimitIp)
		}
	})
}

// payloadFields is what separates a master's node push (omits the counters, so
// they must be preserved) from the SPA's per-client Reset (sends zeroed ones,
// which must be written). Getting it wrong either wipes a node's traffic
// history or silently undoes a reset.
func TestPayloadFields(t *testing.T) {
	tests := []struct {
		name string
		data string
		want map[string]bool
	}{
		{
			name: "node push omits the counters",
			// Mirrors expectedClients' payload field for field; keep the two in
			// step, since this doubles as the record of what a push carries.
			data: `{"name":"x","enable":true,"config":{},"inbounds":[1],"links":[],"volume":0,"expiry":0,"group":"@cluster","desc":"","limitIp":0}`,
			want: map[string]bool{
				"name": true, "enable": true, "config": true, "inbounds": true,
				"links": true, "volume": true, "expiry": true, "group": true,
				"desc": true, "limitIp": true,
			},
		},
		{
			name: "reset carries all four",
			data: `{"id":3,"name":"x","up":0,"down":0,"totalUp":50,"totalDown":60}`,
			want: map[string]bool{
				"id": true, "name": true, "up": true, "down": true,
				"totalUp": true, "totalDown": true,
			},
		},
		{
			name: "explicit null still counts as carried",
			data: `{"up":null}`,
			want: map[string]bool{"up": true},
		},
		{
			name: "empty object carries nothing",
			data: `{}`,
			want: map[string]bool{},
		},
		{
			name: "malformed payload reports nothing rather than guessing",
			data: `[1,2,3]`,
			want: nil,
		},
		{
			name: "garbage reports nothing",
			data: `not json`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := payloadFields(json.RawMessage(tc.data))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("payloadFields(%s) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// The DepleteJob fires every minute whether or not there is anything to do, and
// its notify makes the hub push a full config payload -- which the SPA applies
// by replacing its whole config object, taking any edit the operator has open
// but unsaved on the rules/DNS/settings pages with it. The notify is gated on
// this round having marked a change, so an idle round has to mark nothing.
func TestDepleteClientsMarksOnlyRealChanges(t *testing.T) {
	// DepleteClients reads the package-level handle rather than taking one, so
	// the DB has to be installed globally. CloseDBForTest puts it back on the
	// way out, and is registered after TempDir's own cleanup so LIFO runs it
	// first: Windows refuses to delete the file while the pool holds it open.
	// The deplete path logs each client it disables, and package logger panics
	// on a nil handle -- nothing installs one in a test binary.
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()
	var svc ClientService

	// Inside its quota, no expiry, no periodic reset due: nothing to report.
	healthy := model.Client{
		Name: "healthy", Enable: true, Group: "user", Up: 1, Down: 1,
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}
	if err := db.Create(&healthy).Error; err != nil {
		t.Fatalf("seed healthy client: %v", err)
	}

	before := lastUpdateSeq.Load()
	if _, _, err := svc.DepleteClients(); err != nil {
		t.Fatalf("idle round: %v", err)
	}
	if got := lastUpdateSeq.Load(); got != before {
		t.Fatalf("an idle round marked %d change(s); the hub would push a full config and discard unsaved edits", got-before)
	}

	// Over quota: now the round has real news and must say so, or the panel
	// never hears that the client was cut off.
	over := model.Client{
		Name: "over", Enable: true, Group: "user", Up: 10, Down: 10, Volume: 5,
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}
	if err := db.Create(&over).Error; err != nil {
		t.Fatalf("seed over-quota client: %v", err)
	}
	if _, _, err := svc.DepleteClients(); err != nil {
		t.Fatalf("depleting round: %v", err)
	}
	if lastUpdateSeq.Load() == before {
		t.Error("depleting a client marked nothing, so no push would ever report it")
	}
	var stillEnabled bool
	if err := db.Model(model.Client{}).Where("name = ?", "over").Select("enable").Scan(&stillEnabled).Error; err != nil {
		t.Fatalf("read back client: %v", err)
	}
	if stillEnabled {
		t.Error("over-quota client was not disabled")
	}
}
