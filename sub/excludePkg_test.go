package sub

import (
	"encoding/json"
	"reflect"
	"testing"
)

func mergeInto(t *testing.T, raw string, ids []string) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	mergeExcludePkgs(cfg, ids)
	return cfg
}

func tunExcludes(cfg map[string]interface{}, i int) interface{} {
	return cfg["inbounds"].([]interface{})[i].(map[string]interface{})["exclude_package"]
}

func TestMergeExcludePkgsKeepsManualListVerbatim(t *testing.T) {
	// The manual list keeps its order, its duplicate, and even an entry that
	// is no package ID; a rule naming a manual entry does not remove it.
	cfg := mergeInto(t, `{
		"inbounds": [{"type": "tun", "exclude_package": ["z.manual", "not an id", "z.manual", "com.ruled"]}],
		"route": {"rules": [{"package_name": "com.ruled", "outbound": "proxy"}]}
	}`, []string{"com.remote", "z.manual"})
	want := []interface{}{"z.manual", "not an id", "z.manual", "com.ruled", "com.remote"}
	if got := tunExcludes(cfg, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("exclude_package = %v, want %v", got, want)
	}
}

func TestMergeExcludePkgsFiltersRuledRemoteIDs(t *testing.T) {
	cfg := mergeInto(t, `{
		"inbounds": [{"type": "tun"}, {"type": "mixed"}],
		"route": {
			"rules": [
				{"type": "logical", "mode": "or", "rules": [{"package_name": ["com.nested"]}]},
				{"package_name_regex": "^org\\.bank\\.", "outbound": "proxy"},
				{"package_name_regex": "(", "outbound": "proxy"}
			],
			"rule_set": [{"type": "inline", "tag": "x", "rules": [{"package_name": "com.inline"}]}]
		},
		"dns": {"rules": [{"package_name": "com.dns"}]}
	}`, []string{"com.dns", "com.free", "com.inline", "com.nested", "org.bank.app"})
	if got := tunExcludes(cfg, 0); !reflect.DeepEqual(got, []interface{}{"com.free"}) {
		t.Fatalf("exclude_package = %v", got)
	}
	if _, has := cfg["inbounds"].([]interface{})[1].(map[string]interface{})["exclude_package"]; has {
		t.Fatal("a non-TUN inbound must be left alone")
	}
}

func TestMergeExcludePkgsSkipsIncludePackage(t *testing.T) {
	cfg := mergeInto(t, `{"inbounds": [
		{"type": "tun", "include_package": "com.only"},
		{"type": "tun", "exclude_package": "com.single"}
	]}`, []string{"com.remote"})
	if got := tunExcludes(cfg, 0); got != nil {
		t.Fatalf("a TUN with include_package must get nothing, got %v", got)
	}
	if got := tunExcludes(cfg, 1); !reflect.DeepEqual(got, []interface{}{"com.single", "com.remote"}) {
		t.Fatalf("a single-string exclude_package must become a list: %v", got)
	}
}

func TestMergeExcludePkgsNoIDsIsANoop(t *testing.T) {
	cfg := mergeInto(t, `{"inbounds": [{"type": "tun", "exclude_package": "com.single"}]}`, nil)
	if got := tunExcludes(cfg, 0); got != "com.single" {
		t.Fatalf("with no remote IDs nothing changes, got %v", got)
	}
}
