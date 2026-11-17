package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
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
	// Pushed here by a master this panel is a node of: never bound by choice.
	upstream := model.Client{Enable: true, Name: "up", Group: clusterGroup, Config: cfg, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	for _, c := range []*model.Client{&fresh, &holder, &other, &upstream} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	tx := db.Begin()
	defer tx.Rollback()
	// 999999 matches no row and is skipped, as the initUsers path always did.
	bound, err := (&ClientService{}).AddInboundsToClients(tx, []uint{fresh.Id, holder.Id, upstream.Id, 999999}, []uint{local.Id, replica.Id}, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint{fresh.Id, holder.Id}; !reflect.DeepEqual(bound, want) {
		t.Errorf("bound %v, want %v", bound, want)
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
	if ids, links := load(upstream.Id); len(ids) != 0 || len(links) != 0 {
		t.Errorf("@cluster client was bound: %v %v", ids, links)
	}
}

func TestUpdateClientsOnInboundAddRejectsBadIds(t *testing.T) {
	if err := (&ClientService{}).UpdateClientsOnInboundAdd(nil, "1,x", 1, ""); err == nil {
		t.Fatal("a malformed initUsers id was accepted")
	}
}

func TestTakesUsers(t *testing.T) {
	for _, tc := range []struct {
		typ     string
		version int
		managed bool
		want    bool
	}{
		{"vless", 0, false, true},
		{"mixed", 0, false, true},
		{"tun", 0, false, false},
		{"direct", 0, false, false},
		{"shadowtls", 2, false, false},
		{"shadowtls", 3, false, true},
		{"shadowsocks", 0, false, true},
		{"shadowsocks", 0, true, false},
	} {
		if got := takesUsers(tc.typ, tc.version, tc.managed); got != tc.want {
			t.Errorf("%s v%d managed=%v: got %v, want %v", tc.typ, tc.version, tc.managed, got, tc.want)
		}
	}
}

// A new local inbound's core user list is built from the same clients the rows
// are bound to: a @cluster client named in initUsers is in neither.
func TestInitUsersSkipsClusterClients(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "init.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })
	db := database.GetDB()
	mine := model.Client{Enable: true, Name: "mine", Config: json.RawMessage(`{"vless":{"name":"mine","uuid":"8b5e6d3c-3f3e-4c1a-9b1e-2b8a4f6f0a11"}}`), Inbounds: json.RawMessage(`[]`)}
	up := model.Client{Enable: true, Name: "up", Group: clusterGroup, Config: json.RawMessage(`{"vless":{"name":"up","uuid":"0d2b1d0e-6a0f-4a52-9a8e-3f9c1d2e4b5a"}}`), Inbounds: json.RawMessage(`[]`)}
	for _, c := range []*model.Client{&mine, &up} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	out, err := (&InboundService{}).initUsers(db, []byte(`{"type":"vless","tag":"v"}`), strconv.FormatUint(uint64(mine.Id), 10)+","+strconv.FormatUint(uint64(up.Id), 10), "vless")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Users) != 1 || got.Users[0].Name != "mine" {
		t.Errorf("core users = %+v, want only mine", got.Users)
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
