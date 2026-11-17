package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// AddInboundsToClients is the one path that binds clients to new inbounds, for
// an inbound created here and for replicas adopted from a node (#287).
func TestAddInboundsToClients(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "assign.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })
	db := database.GetDB()
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	nodeId := uint(1)
	local := model.Inbound{Type: "vless", Tag: "vl", Options: json.RawMessage(`{"listen_port": 443}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	replica := model.Inbound{Type: "vless", Tag: "node-vl", NodeId: &nodeId, Options: json.RawMessage(`{"listen_port": 8443}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	for _, ib := range []*model.Inbound{&local, &replica} {
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
	}

	cfg := json.RawMessage(`{"vless": {"name": "x", "uuid": "8b5e6d3c-3f3e-4c1a-9b1e-2b8a4f6f0a11"}}`)
	nodeLink := `[{"remark":"[n1] node-vl","type":"external","uri":"vless://node"}]`
	fresh := model.Client{Enable: true, Name: "fresh", Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(nodeLink)}
	holder := model.Client{Enable: true, Name: "holder", Config: cfg, Inbounds: mustJSONRaw(t, []uint{replica.Id}), Links: json.RawMessage(`[]`)}
	other := model.Client{Enable: true, Name: "other", Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	for _, c := range []*model.Client{&fresh, &holder, &other} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	tx := db.Begin()
	defer tx.Rollback()
	// 999999 matches no row and is skipped, as the initUsers path always did.
	if err := (&ClientService{}).AddInboundsToClients(tx, []uint{fresh.Id, holder.Id, 999999}, []uint{local.Id, replica.Id}, "example.com"); err != nil {
		t.Fatal(err)
	}

	load := func(id uint) (ids []uint, links []map[string]string) {
		t.Helper()
		var c model.Client
		if err := tx.First(&c, id).Error; err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.Inbounds, &ids); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.Links, &links); err != nil {
			t.Fatal(err)
		}
		return ids, links
	}

	ids, links := load(fresh.Id)
	if want := []uint{local.Id, replica.Id}; !reflect.DeepEqual(ids, want) {
		t.Errorf("fresh: inbounds %v, want %v", ids, want)
	}
	var sawLocal, sawNode bool
	for _, l := range links {
		switch l["remark"] {
		case "vl":
			sawLocal = l["type"] == "local" && strings.HasPrefix(l["uri"], "vless://")
		case "[n1] node-vl":
			sawNode = l["type"] == "external"
		case "node-vl":
			// A local link for a replica would carry this panel's hostname.
			t.Errorf("fresh: replica got a local link: %v", l)
		}
	}
	if !sawLocal {
		t.Errorf("fresh: no local link for the local inbound: %v", links)
	}
	if !sawNode {
		t.Errorf("fresh: the node's own link was dropped: %v", links)
	}

	// Already holding the replica: added once, not twice.
	if ids, _ := load(holder.Id); !reflect.DeepEqual(ids, []uint{replica.Id, local.Id}) {
		t.Errorf("holder: inbounds %v, want [%d %d]", ids, replica.Id, local.Id)
	}
	if ids, links := load(other.Id); len(ids) != 0 || len(links) != 0 {
		t.Errorf("other: untouched client changed: %v %v", ids, links)
	}
}

func TestUpdateClientsOnInboundAddRejectsBadIds(t *testing.T) {
	if err := (&ClientService{}).UpdateClientsOnInboundAdd(nil, "1,x", 1, ""); err == nil {
		t.Fatal("a malformed initUsers id was accepted")
	}
}

func TestInboundTakesUsers(t *testing.T) {
	for _, tc := range []struct {
		typ, options string
		want         bool
	}{
		{"vless", `{}`, true},
		{"mixed", `{}`, true},
		{"tun", `{}`, false},
		{"direct", `{}`, false},
		{"shadowtls", `{"version": 2}`, false},
		{"shadowtls", `{"version": 3}`, true},
		{"shadowtls", `not json`, false},
	} {
		ib := &model.Inbound{Type: tc.typ, Options: json.RawMessage(tc.options)}
		if got := inboundTakesUsers(ib); got != tc.want {
			t.Errorf("%s %s: got %v, want %v", tc.typ, tc.options, got, tc.want)
		}
	}
}

func mustJSONRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
