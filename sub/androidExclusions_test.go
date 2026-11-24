package sub

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/service"
	"gorm.io/gorm"
)

func TestAndroidExclusionsGeneratedSubscription(t *testing.T) {
	setupSubDB(t)
	seedNodeReplicaClient(t, "android-exclusions-test")
	service.InvalidateAndroidExclusions()
	t.Cleanup(service.InvalidateAndroidExclusions)
	settings := &service.SettingService{}
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	raw := `{"inbounds":[{"type":"tun","tag":"tun-in","address":["172.19.0.1/30"],"auto_route":true,"exclude_package":["com.legacy"]}],"rules":[{"package_name":["com.block"],"action":"route","outbound":"direct"}]}`
	save := func(enabled bool) {
		t.Helper()
		options, _ := json.Marshal(map[string]any{"enabled": enabled, "manual": "com.block,com.auto,com.auto", "sources": []string{}, "interval": 0})
		payload, _ := json.Marshal(map[string]string{"subJsonExt": raw, "androidExcludeOptions": string(options)})
		if err := database.GetDB().Transaction(func(tx *gorm.DB) error { return settings.Save(tx, payload) }); err != nil {
			t.Fatal(err)
		}
		service.InvalidateAndroidExclusions()
	}
	check := func(want []string, packageName, packageRegex string) {
		t.Helper()
		got, _, err := (&JsonService{}).GetJson("android-exclusions-test", "json")
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Inbounds []struct {
				Exclude []string `json:"exclude_package"`
			} `json:"inbounds"`
			Route struct {
				Rules []struct {
					PackageName  []string `json:"package_name"`
					PackageRegex string   `json:"package_name_regex"`
					Outbound     string   `json:"outbound"`
				} `json:"rules"`
			} `json:"route"`
		}
		if err = json.Unmarshal([]byte(*got), &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Inbounds) != 1 || !reflect.DeepEqual(cfg.Inbounds[0].Exclude, want) {
			t.Fatalf("unexpected inbounds: %+v", cfg.Inbounds)
		}
		if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Outbound != "direct" {
			t.Fatalf("missing route rule: %+v", cfg.Route.Rules)
		}
		rule := cfg.Route.Rules[0]
		if packageName != "" && !reflect.DeepEqual(rule.PackageName, []string{packageName}) {
			t.Fatal(rule)
		}
		if packageRegex != "" && rule.PackageRegex != packageRegex {
			t.Fatal(rule)
		}
	}
	save(true)
	check([]string{"com.auto", "com.legacy"}, "com.block", "")
	original := raw
	raw = `{"inbounds":[{"type":"tun","tag":"tun-in","address":["172.19.0.1/30"],"auto_route":true,"exclude_package":["com.legacy"]}],"rules":[{"package_name_regex":"[","outbound":"direct"}]}`
	save(true)
	check(nil, "", "[")
	raw = original
	save(true)
	check([]string{"com.auto", "com.legacy"}, "com.block", "")
	save(false)
	check([]string{"com.legacy"}, "com.block", "")
}
