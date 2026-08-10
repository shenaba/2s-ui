package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shenaba/2s-ui/database/model"

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
			data: `{"name":"x","enable":true,"config":{},"inbounds":[1],"links":[],"volume":0,"expiry":0,"group":"@cluster","desc":""}`,
			want: map[string]bool{
				"name": true, "enable": true, "config": true, "inbounds": true,
				"links": true, "volume": true, "expiry": true, "group": true, "desc": true,
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
