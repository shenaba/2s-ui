package database

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

func TestMigrateRemovedOptions(t *testing.T) {
	openHysteriaTestDB(t)
	if err := db.Create(&model.Inbound{
		Type: "tun", Tag: "tun-in",
		Options: json.RawMessage(`{"address": ["172.19.0.1/30"], "mtu": 9000, "endpoint_independent_nat": false}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Tls{
		Name:   "site",
		Server: json.RawMessage(`{"enabled": true}`),
		Client: json.RawMessage(`{
			"enabled": true,
			"ech": {"enabled": true, "pq_signature_schemes_enabled": false, "dynamic_record_sizing_disabled": false, "config_path": "/e.pem"}
		}`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateRemovedOptions(); err != nil {
		t.Fatal(err)
	}

	options, _ := inboundOptions(t, 1)
	if _, ok := options["endpoint_independent_nat"]; ok {
		t.Errorf("the removed tun option must go, got %v", options)
	}
	if options["mtu"] != float64(9000) {
		t.Errorf("the rest of the options must survive, got %v", options)
	}

	var stored model.Tls
	if err := db.Where("id = ?", 1).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	var client map[string]any
	if err := json.Unmarshal(stored.Client, &client); err != nil {
		t.Fatal(err)
	}
	ech, ok := client["ech"].(map[string]any)
	if !ok {
		t.Fatalf("the ech block must survive, got %v", client)
	}
	for _, removed := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"} {
		if _, present := ech[removed]; present {
			t.Errorf("%q must go, got %v", removed, ech)
		}
	}
	if ech["enabled"] != true || ech["config_path"] != "/e.pem" {
		t.Errorf("the settings that still apply must survive, got %v", ech)
	}
}

// Objects carrying none of them must not be rewritten.
func TestMigrateRemovedOptionsLeavesCleanObjects(t *testing.T) {
	openHysteriaTestDB(t)
	if err := db.Create(&model.Inbound{
		Type: "tun", Tag: "tun-in",
		Options: json.RawMessage(`{"address":["172.19.0.1/30"],"mtu":9000}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateRemovedOptions(); err != nil {
		t.Fatal(err)
	}

	var stored model.Inbound
	if err := db.Where("id = ?", 1).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if string(stored.Options) != `{"address":["172.19.0.1/30"],"mtu":9000}` {
		t.Errorf("a clean object must be left byte for byte, got %s", stored.Options)
	}
}

// The first ECH cleanup cleared only tls.client. The same options on tls.server
// make sing-box refuse the inbound, and the panel had copied them into the
// stored out_json too -- including on rows with no tls_id, such as node
// replicas.
func TestMigrateRemovedServerECH(t *testing.T) {
	openHysteriaTestDB(t)
	if err := db.Create(&model.Tls{
		Name:   "site",
		Server: json.RawMessage(`{"enabled":true,"ech":{"enabled":true,"key":["k"],"pq_signature_schemes_enabled":true,"dynamic_record_sizing_disabled":false}}`),
		Client: json.RawMessage(`{"enabled":true}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	replica := uint(7)
	for _, in := range []model.Inbound{
		{Type: "trojan", Tag: "local", TlsId: 1, Options: json.RawMessage(`{}`),
			OutJson: json.RawMessage(`{"type":"trojan","tls":{"enabled":true,"ech":{"enabled":true,"pq_signature_schemes_enabled":true}}}`)},
		{Type: "trojan", Tag: "replica", NodeId: &replica, Options: json.RawMessage(`{}`),
			OutJson: json.RawMessage(`{"type":"trojan","tls":{"enabled":true,"ech":{"enabled":true,"dynamic_record_sizing_disabled":true}}}`)},
		{Type: "vless", Tag: "clean", Options: json.RawMessage(`{}`),
			OutJson: json.RawMessage(`{"type":"vless","tls":{"enabled":true}}`)},
	} {
		if err := db.Create(&in).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateRemovedServerECH(); err != nil {
		t.Fatal(err)
	}

	var stored model.Tls
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	json.Unmarshal(stored.Server, &server)
	ech, _ := server["ech"].(map[string]any)
	if ech["enabled"] != true || ech["key"] == nil {
		t.Errorf("the ech settings that still apply must survive, got %v", ech)
	}
	for _, removed := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"} {
		if _, present := ech[removed]; present {
			t.Errorf("server: %q must go, got %v", removed, ech)
		}
	}

	for _, tag := range []string{"local", "replica"} {
		var in model.Inbound
		if err := db.Where("tag = ?", tag).First(&in).Error; err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.Unmarshal(in.OutJson, &out)
		tls, _ := out["tls"].(map[string]any)
		ech, _ := tls["ech"].(map[string]any)
		if ech["enabled"] != true || out["type"] != "trojan" {
			t.Errorf("%s: the rest of out_json must survive, got %v", tag, out)
		}
		if len(ech) != 1 {
			t.Errorf("%s: legacy ECH options must go, got %v", tag, ech)
		}
	}

	var clean model.Inbound
	db.Where("tag = ?", "clean").First(&clean)
	if string(clean.OutJson) != `{"type":"vless","tls":{"enabled":true}}` {
		t.Errorf("a clean out_json must not be rewritten, got %s", clean.OutJson)
	}

	// One-shot: the flag row stops a second run.
	var flag model.Setting
	if err := db.Where("key = ?", migratedKeyRemovedServerECH).First(&flag).Error; err != nil {
		t.Errorf("the migration must mark itself done: %v", err)
	}
}
