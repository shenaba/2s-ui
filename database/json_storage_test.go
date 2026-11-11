package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// A client row written by the sqlite3 shell stores its JSON as TEXT, which
// database/sql cannot scan into a json.RawMessage. Such a row used to stop the
// panel at start, valid JSON or not: the mieru backfill loads every client
// without a mieru block, and its scan failure failed InitDB.
func TestInitDBStartsWithClientRowsStoredAsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("init db: %v", err)
	}
	// SQL string literals are TEXT, as a hand edit's would be.
	for _, row := range []struct{ name, config, inbounds string }{
		{"text-valid", `{"trojan":{"name":"text-valid","password":"shared-1"}}`, `[]`},
		{"text-broken", `{"trojan":`, `oops`},
	} {
		if err := db.Exec("INSERT INTO clients (name, enable, config, inbounds, links) VALUES (?, 1, ?, ?, '[]')",
			row.name, row.config, row.inbounds).Error; err != nil {
			t.Fatalf("seed %s: %v", row.name, err)
		}
	}
	var before []model.Client
	if err := db.Find(&before).Error; err == nil {
		t.Fatal("scanning a TEXT row worked without normalizing; this test no longer shows the failure")
	}
	if err := CloseDBForTest(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if err := InitDB(path); err != nil {
		t.Fatalf("init db with TEXT client rows: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	var types []string
	if err := db.Raw("SELECT DISTINCT typeof(config) || '/' || typeof(inbounds) || '/' || typeof(links) FROM clients").
		Scan(&types).Error; err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 || types[0] != "blob/blob/blob" {
		t.Fatalf("storage classes after start: %v, want blob/blob/blob", types)
	}

	var clients []model.Client
	if err := db.Order("id").Find(&clients).Error; err != nil {
		t.Fatalf("load clients: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("got %d clients, want 2", len(clients))
	}
	// The valid row is served like any other: the backfill reached it.
	var configs map[string]map[string]string
	if err := json.Unmarshal(clients[0].Config, &configs); err != nil {
		t.Fatalf("valid row config: %v", err)
	}
	if got := configs["mieru"]["password"]; got != "shared-1" {
		t.Fatalf("valid row mieru password %q, want the shared one", got)
	}
	// The broken one keeps its bytes; repairing it is not this code's job.
	if string(clients[1].Config) != `{"trojan":` || string(clients[1].Inbounds) != `oops` {
		t.Fatalf("broken row changed: config %q inbounds %q", clients[1].Config, clients[1].Inbounds)
	}
}
