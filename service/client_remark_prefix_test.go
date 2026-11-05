package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func TestSubRemarkPrefixRebuildsLinks(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	// Setting rows are seeded the first time the settings page loads them;
	// Save only updates rows that exist.
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Type: "vless", Tag: "vl", Options: json.RawMessage(`{"listen_port": 443}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "c1",
		Remark:   "alice",
		Config:   json.RawMessage(`{"vless": {"name": "c1", "uuid": "8b5e6d3c-3f3e-4c1a-9b1e-2b8a4f6f0a11"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf("[%d]", inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	links := func(prefix string) string {
		t.Helper()
		tx := db.Begin()
		defer tx.Commit()
		if err := (&SettingService{}).Save(tx, json.RawMessage(`{"subRemarkPrefix": "`+prefix+`"}`)); err != nil {
			t.Fatal(err)
		}
		inbounds := []model.Inbound{inbound}
		if err := (&ClientService{}).UpdateLinksByInboundChange(tx, &inbounds, "example.com", ""); err != nil {
			t.Fatal(err)
		}
		var stored model.Client
		if err := tx.First(&stored, client.Id).Error; err != nil {
			t.Fatal(err)
		}
		return string(stored.Links)
	}

	if got := links("false"); strings.Contains(got, "alice") || !strings.Contains(got, "vless://") {
		t.Errorf("prefix off: links still carry the remark or are missing: %s", got)
	}
	if got := links("true"); !strings.Contains(got, "alice-vl") {
		t.Errorf("prefix on: links lack the remark: %s", got)
	}
}

// Flipping subRemarkPrefix rebuilds every client's links. A node replica must
// not get a local link out of that: it would carry this panel's hostname
// instead of the node's address.
func TestRebuildLocalLinksSkipsNodeReplicas(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	nodeId := uint(7)
	local := model.Inbound{Type: "vless", Tag: "local-vl", Options: json.RawMessage(`{"listen_port": 443}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	replica := model.Inbound{Type: "vless", Tag: "node-vl", NodeId: &nodeId, Options: json.RawMessage(`{"listen_port": 8443}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	for _, in := range []*model.Inbound{&local, &replica} {
		if err := db.Create(in).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{
		Enable:   true,
		Name:     "c1",
		Config:   json.RawMessage(`{"vless": {"name": "c1", "uuid": "8b5e6d3c-3f3e-4c1a-9b1e-2b8a4f6f0a11"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf("[%d, %d]", local.Id, replica.Id)),
		Links:    json.RawMessage(`[{"remark": "node-vl", "type": "external", "uri": "vless://x@node.example.com:8443#node-vl"}]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	if err := (&ClientService{}).RebuildLocalLinks(db, "panel.example.com"); err != nil {
		t.Fatal(err)
	}
	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var links []map[string]string
	if err := json.Unmarshal(stored.Links, &links); err != nil {
		t.Fatal(err)
	}
	var sawLocal, sawExternal bool
	for _, l := range links {
		switch {
		case l["type"] == "local" && l["remark"] == "local-vl":
			sawLocal = true
		case l["type"] == "local":
			t.Errorf("replica got a local link: %v", l)
		case l["type"] == "external" && l["remark"] == "node-vl":
			sawExternal = true
		}
	}
	if !sawLocal || !sawExternal {
		t.Errorf("want the local inbound's link and the node's external link kept, got %s", stored.Links)
	}
}
