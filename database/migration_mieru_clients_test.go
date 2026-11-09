package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database/jsonsql"
	"github.com/shenaba/2s-ui/database/model"
)

// A client from before mieru has no mieru block, and without one it is left
// out of every mieru inbound's user list. The backfill gives it one, reusing
// the shared password where the client has it, and leaves every other block
// -- and any client that already has a mieru block -- as it was.
func TestBackfillMieruClients(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "mieru.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	seed := func(name string, config string) {
		t.Helper()
		c := &model.Client{Name: name, Config: []byte(config), Inbounds: []byte(`[]`), Links: []byte(`[]`)}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("old-trojan", `{"trojan":{"name":"old-trojan","password":"shared-1"},"vless":{"name":"old-trojan","uuid":"u"}}`)
	seed("old-anytls", `{"anytls":{"name":"old-anytls","password":"shared-2"}}`)
	seed("old-bare", `{"vmess":{"name":"old-bare","uuid":"u"}}`)
	seed("current", `{"mieru":{"name":"current","password":"keep-me"},"trojan":{"name":"current","password":"other"}}`)

	// Twice: the second run must find nothing left to do and change nothing.
	for run := 0; run < 2; run++ {
		if err := backfillMieruClientConfig(); err != nil {
			t.Fatalf("backfill run %d: %v", run, err)
		}
	}

	mieruOf := func(name string) (map[string]json.RawMessage, map[string]string) {
		t.Helper()
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		var configs map[string]json.RawMessage
		if err := json.Unmarshal(c.Config, &configs); err != nil {
			t.Fatalf("%s config: %v", name, err)
		}
		var block map[string]string
		if err := json.Unmarshal(configs["mieru"], &block); err != nil {
			t.Fatalf("%s has no readable mieru block: %v", name, err)
		}
		return configs, block
	}

	configs, block := mieruOf("old-trojan")
	if block["name"] != "old-trojan" || block["password"] != "shared-1" {
		t.Errorf("old-trojan: mieru block %v, want its name and trojan password", block)
	}
	if _, ok := configs["vless"]; !ok {
		t.Error("old-trojan lost its vless block")
	}
	if _, block = mieruOf("old-anytls"); block["password"] != "shared-2" {
		t.Errorf("old-anytls: password %q, want the anytls one", block["password"])
	}
	if _, block = mieruOf("old-bare"); len(block["password"]) != 10 || block["name"] != "old-bare" {
		t.Errorf("old-bare: mieru block %v, want its name and a fresh password", block)
	}
	if _, block = mieruOf("current"); block["password"] != "keep-me" {
		t.Errorf("current: existing mieru block was rewritten to %v", block)
	}

	// The listener's user query must now find every client -- the point of
	// the exercise, and the same lookup the backfill itself filters on.
	var withMieru int64
	if err := db.Model(model.Client{}).Where(jsonsql.Extract("config", "$.mieru") + " IS NOT NULL").Count(&withMieru).Error; err != nil {
		t.Fatal(err)
	}
	if withMieru != 4 {
		t.Errorf("%d clients have a mieru block, want 4", withMieru)
	}
}
