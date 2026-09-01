package database

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// openTestDB gives each test its own on-disk database, since OpenDB stores the
// handle in the package-level db used by the migration.
//
// The pool has to be closed explicitly, and not only to release the handle:
// t.TempDir's cleanup fails on Windows while the file is still open, which
// turns every test in here red for a reason that has nothing to do with the
// migration under test.
func openTestDB(t *testing.T) {
	t.Helper()
	if err := OpenDB(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close test db: %v", err)
		}
	})
	// Outbound is here because the rule-set migrations resolve a download
	// detour against the outbound it names before deciding to drop it.
	if err := db.AutoMigrate(&model.Setting{}, &model.Tls{}, &model.Outbound{}); err != nil {
		t.Fatal(err)
	}
}

func readConfig(t *testing.T) map[string]any {
	t.Helper()
	var setting model.Setting
	if err := db.Where("key = ?", "config").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(setting.Value), &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func section(t *testing.T, root map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := root[key].(map[string]any)
	if !ok {
		t.Fatalf("missing %q section in %v", key, root)
	}
	return value
}

func TestMigrateSingBox114Config(t *testing.T) {
	openTestDB(t)
	legacy := `{
		"log": {"level": "info"},
		"dns": {"servers": [], "rules": [], "independent_cache": true, "cache_capacity": 4096},
		"experimental": {"cache_file": {"enabled": true, "store_rdrc": true}},
		"route": {
			"rules": [{"action": "sniff"}],
			"rule_set": [
				{"type": "remote", "tag": "geoip-cn", "url": "https://example.com/a.srs", "download_detour": "direct"},
				{"type": "remote", "tag": "geosite-ads", "url": "https://example.com/b.srs"},
				{"type": "remote", "tag": "geosite-ir", "url": "https://example.com/c.srs", "download_detour": "proxy"}
			]
		}
	}`
	if err := db.Create(&model.Setting{Key: "config", Value: legacy}).Error; err != nil {
		t.Fatal(err)
	}
	// The detour named "direct" is dropped because this outbound carries no
	// options, not because of what it is called.
	if err := db.Create(&model.Outbound{
		Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}
	root := readConfig(t)

	dns := section(t, root, "dns")
	if _, ok := dns["independent_cache"]; ok {
		t.Error("independent_cache should have been dropped")
	}
	if dns["cache_capacity"] != float64(4096) {
		t.Errorf("unrelated dns options must be preserved, got %v", dns["cache_capacity"])
	}
	if root["log"] == nil {
		t.Error("unrelated top-level sections must be preserved")
	}

	cacheFile := section(t, section(t, root, "experimental"), "cache_file")
	if _, ok := cacheFile["store_rdrc"]; ok {
		t.Error("store_rdrc should have been renamed")
	}
	if cacheFile["store_dns"] != true {
		t.Errorf("store_rdrc should become store_dns, got %v", cacheFile["store_dns"])
	}

	ruleSets, ok := section(t, root, "route")["rule_set"].([]any)
	if !ok || len(ruleSets) != 3 {
		t.Fatalf("expected 3 rule sets, got %v", ruleSets)
	}
	// A direct detour is what sing-box does anyway, and it cannot be expressed
	// as an http_client detour, so it just goes away.
	direct, _ := ruleSets[0].(map[string]any)
	if _, ok = direct["download_detour"]; ok {
		t.Error("download_detour should have been replaced")
	}
	if _, ok = direct["http_client"]; ok {
		t.Errorf("a direct detour must not become an http_client, got %v", direct)
	}
	if untouched, _ := ruleSets[1].(map[string]any); untouched["http_client"] != nil {
		t.Error("rule sets without download_detour must be left alone")
	}
	proxied, _ := ruleSets[2].(map[string]any)
	if _, ok = proxied["download_detour"]; ok {
		t.Error("download_detour should have been replaced")
	}
	httpClient, ok := proxied["http_client"].(map[string]any)
	if !ok {
		t.Fatalf("expected http_client, got %v", proxied)
	}
	if httpClient["detour"] != "proxy" {
		t.Errorf("unexpected http_client: %v", httpClient)
	}
	// disable_empty_direct_check has no JSON field; emitting it makes the whole
	// config unparseable.
	if _, ok = httpClient["disable_empty_direct_check"]; ok {
		t.Errorf("disable_empty_direct_check is not a real option, got %v", httpClient)
	}
}

func TestMigrateSingBox114Tls(t *testing.T) {
	openTestDB(t)
	server := `{"enabled": true, "server_name": "example.com", "acme": {"domain": ["example.com"], "email": "a@example.com", "dns01_challenge": {"provider": "cloudflare", "api_token": "tok"}}}`
	if err := db.Create(&model.Tls{Name: "cert", Server: json.RawMessage(server)}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}

	var stored model.Tls
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stored.Server, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["acme"]; ok {
		t.Error("inline acme should have been removed")
	}
	if decoded["server_name"] != "example.com" {
		t.Error("unrelated tls options must be preserved")
	}
	provider, ok := decoded["certificate_provider"].(map[string]any)
	if !ok {
		t.Fatalf("expected certificate_provider, got %v", decoded)
	}
	if provider["type"] != "acme" {
		t.Errorf("expected type acme, got %v", provider["type"])
	}
	if provider["email"] != "a@example.com" {
		t.Errorf("acme fields must carry over, got %v", provider)
	}
	if _, ok = provider["dns01_challenge"].(map[string]any); !ok {
		t.Errorf("nested acme options must carry over, got %v", provider)
	}
}

// TestMigrateSingBox114RunsOnce guards that a config edited back to a legacy
// shape after the migration is not silently rewritten again.
func TestMigrateSingBox114RunsOnce(t *testing.T) {
	openTestDB(t)
	if err := db.Create(&model.Setting{Key: "config", Value: `{"dns": {"independent_cache": true}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Setting{}).Where("key = ?", "config").
		Update("value", `{"dns": {"independent_cache": true}}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}
	if _, ok := section(t, readConfig(t), "dns")["independent_cache"]; !ok {
		t.Error("migration should not run a second time")
	}
}

// TestMigrateSingBox114Idempotent guards the common case: a config that has
// nothing to migrate must come through byte-identical.
func TestMigrateSingBox114Idempotent(t *testing.T) {
	openTestDB(t)
	clean := `{"log":{"level":"info"},"dns":{"servers":[],"rules":[]},"experimental":{}}`
	if err := db.Create(&model.Setting{Key: "config", Value: clean}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}
	var setting model.Setting
	if err := db.Where("key = ?", "config").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.Value != clean {
		t.Errorf("config with nothing to migrate was rewritten:\n got %s\nwant %s", setting.Value, clean)
	}
}

// A direct outbound that carries dialer options is a real routing choice, so
// the detour to it has to survive. Only an optionless one is the no-op sing-box
// refuses a detour to.
func TestMigrateSingBox114KeepsConfiguredDirectDetour(t *testing.T) {
	openTestDB(t)
	legacy := `{"route": {"rule_set": [
		{"type": "remote", "tag": "a", "url": "https://e.com/a.srs", "download_detour": "direct"},
		{"type": "remote", "tag": "b", "url": "https://e.com/b.srs", "download_detour": "direct-plain"}
	]}}`
	if err := db.Create(&model.Setting{Key: "config", Value: legacy}).Error; err != nil {
		t.Fatal(err)
	}
	for _, outbound := range []model.Outbound{
		{Type: "direct", Tag: "direct", Options: json.RawMessage(`{"bind_interface":"eth1"}`)},
		{Type: "direct", Tag: "direct-plain", Options: json.RawMessage(`{}`)},
	} {
		if err := db.Create(&outbound).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateSingBox114(); err != nil {
		t.Fatal(err)
	}

	ruleSets, ok := section(t, readConfig(t), "route")["rule_set"].([]any)
	if !ok || len(ruleSets) != 2 {
		t.Fatalf("expected 2 rule sets, got %v", ruleSets)
	}
	configured, _ := ruleSets[0].(map[string]any)
	httpClient, isObject := configured["http_client"].(map[string]any)
	if !isObject {
		t.Fatalf("a direct outbound with dialer options must keep its detour, got %v", configured)
	}
	if httpClient["detour"] != "direct" {
		t.Errorf("unexpected http_client: %v", httpClient)
	}
	plain, _ := ruleSets[1].(map[string]any)
	if _, hasClient := plain["http_client"]; hasClient {
		t.Errorf("an optionless direct outbound is a no-op detour, got %v", plain)
	}
}

// A database read that fails must stop the migration and, through InitDB, the
// panel: it should say it cannot read its own tables rather than come up having
// quietly skipped an upgrade step. This is also what upstream s-ui does, and
// what every other read in these five migrations does.
//
// The scenario is not hypothetical -- a row whose options column had been
// written as TEXT rather than BLOB crash-looped a real install nine times, and
// the log line above is what said why. Normal panel writes are always BLOB;
// TEXT only comes from editing the database by hand or importing one.
func TestMigrateSingBox114FailsOnUnreadableOutbounds(t *testing.T) {
	openTestDB(t)
	legacy := `{"dns": {"independent_cache": true},
		"route": {"rule_set": [
			{"type": "remote", "tag": "a", "url": "https://e.com/a.srs", "download_detour": "direct"}
		]}}`
	if err := db.Create(&model.Setting{Key: "config", Value: legacy}).Error; err != nil {
		t.Fatal(err)
	}
	// json.RawMessage has no Scanner, so a TEXT value fails the row scan.
	if err := db.Exec(
		`INSERT INTO outbounds (type, tag, options) VALUES ('direct','direct',?)`, "{}",
	).Error; err != nil {
		t.Fatal(err)
	}

	err := migrateSingBox114()
	if err == nil {
		t.Fatal("an unreadable outbounds table must fail the migration, not be skipped")
	}
	if !strings.Contains(err.Error(), "unsupported Scan") {
		t.Errorf("expected the scan failure to be reported as-is, got %v", err)
	}

	// Nothing was written: the migration runs in a transaction, so a failure
	// leaves the config exactly as it was rather than half-migrated.
	if _, ok := section(t, readConfig(t), "dns")["independent_cache"]; !ok {
		t.Error("a failed migration must not leave a partly-rewritten config")
	}
	var flag model.Setting
	if err := db.Where("key = ?", migratedKeySingBox114).First(&flag).Error; err == nil {
		t.Error("a failed migration must not mark itself done")
	}
}
