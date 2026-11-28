package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// A base config sing-box cannot parse is refused before it is written. The save
// restarts the core after the commit, and one it could not parse left the core
// down -- retried every few seconds by the watchdog -- with the bad value
// already stored.
func TestABaseConfigTheCoreCannotParseIsRefused(t *testing.T) {
	s := maintenanceEnv(t)
	const stored = `{"log":{"disabled":true}}`
	if err := s.SettingService.SetConfig(stored); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	for name, config := range map[string]string{
		// GetConfig reads an empty string as "the stored config", which used to
		// validate the old one and then store this as null.
		"empty":            ``,
		"null":             `null`,
		"array":            `[]`,
		"memory limit":     `{"log":{"disabled":true},"experimental":{"debug":{"memory_limit":"512mb512mb"}}}`,
		"fractional gc":    `{"log":{"disabled":true},"experimental":{"debug":{"gc_percent":80.5}}}`,
		"legacy oom":       `{"log":{"disabled":true},"experimental":{"debug":{"oom_killer":true}}}`,
		"unknown log knob": `{"log":{"disabled":true,"levle":"info"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := save(t, s, "config", "set", config)
			if err == nil || !strings.Contains(err.Error(), "invalid config") {
				t.Fatalf("want the save refused as an invalid config, got %v", err)
			}
			got, err := s.SettingService.GetConfig()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(strings.Fields(got), "") != stored {
				t.Fatalf("the refused config was written: %s", got)
			}
		})
	}
}

// A stored row the core cannot read keeps it down, but must not also refuse
// every base save: raising the log level is how an operator finds that row.
// Saves made while the core is down are never parsed, so such a row can exist.
func TestABadRowDoesNotBlockTheBaseConfig(t *testing.T) {
	s := maintenanceEnv(t)
	bad := model.Outbound{Type: "direct", Tag: "broken", Options: json.RawMessage(`{"bogus":true}`)}
	if err := database.GetDB().Create(&bad).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	whole, err := s.GetConfig(`{"log":{"level":"debug"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if corePtr.ValidateConfig(*whole) == nil {
		t.Fatal("the seeded row was meant to be unreadable, so the test proves nothing")
	}
	if err := s.validateBaseConfig([]byte(`{"log":{"level":"debug"}}`)); err != nil {
		t.Fatalf("a base config was refused for a row it does not contain: %v", err)
	}
}

func TestABaseConfigTheCoreCanParsePassesValidation(t *testing.T) {
	s := maintenanceEnv(t)
	config := `{"log":{"level":"info"},"experimental":{"debug":{"listen":"127.0.0.1:6060","memory_limit":"512mb","gc_percent":80}}}`
	if err := s.validateBaseConfig([]byte(config)); err != nil {
		t.Fatalf("a valid config was refused: %v", err)
	}
}

// Since sing-box 1.14.6 a route rule naming an outbound the core lacks fails
// the whole restart, so the save that would lead to one is refused. The tables
// are what the base names outbounds from: one in the outbounds table and one in
// the endpoints table both count.
func TestABaseConfigWhoseRuleNamesAMissingOutboundIsRefused(t *testing.T) {
	s := maintenanceEnv(t)
	db := database.GetDB()
	if err := db.Create(&model.Outbound{Type: "direct", Tag: "proxy", Options: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatalf("seed outbound: %v", err)
	}
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: "wg", Options: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}
	ok := `{"log":{"disabled":true},"route":{"rules":[{"domain":["a"],"outbound":"proxy"},{"domain":["b"],"outbound":"wg"}]}}`
	if err := checkBaseRuleActions(db, []byte(ok)); err != nil {
		t.Fatalf("rules naming a stored outbound and endpoint were refused: %v", err)
	}
	const stored = `{"log":{"disabled":true}}`
	if err := s.SettingService.SetConfig(stored); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	missing := `{"log":{"disabled":true},"route":{"rules":[{"domain":["a"],"outbound":"proxy"},{"domain":["b"],"outbound":"gone"}]}}`
	err := save(t, s, "config", "set", missing)
	if err == nil || !strings.Contains(err.Error(), "route rule #2: outbound not found: gone") {
		t.Fatalf("want the save of a rule naming a missing outbound refused, got %v", err)
	}
	if got, _ := s.SettingService.GetConfig(); strings.Join(strings.Fields(got), "") != stored {
		t.Fatalf("the refused config was written: %s", got)
	}
}

// A running core refuses to drop an outbound a route rule names; with the core
// down nothing did, and the delete or rename committed a config whose next
// start sing-box 1.14.6 refuses. The service layer refuses it either way.
func TestARuleNamedOutboundCannotBeDeletedOrRenamedWithTheCoreDown(t *testing.T) {
	s := maintenanceEnv(t)
	if corePtr.IsRunning() {
		t.Fatal("this test is about a core that is not running")
	}
	db := database.GetDB()
	ruled := model.Outbound{Type: "direct", Tag: "ruled", Options: json.RawMessage(`{}`)}
	free := model.Outbound{Type: "direct", Tag: "free", Options: json.RawMessage(`{}`)}
	for _, it := range []*model.Outbound{&ruled, &free} {
		if err := db.Create(it).Error; err != nil {
			t.Fatalf("seed outbound: %v", err)
		}
	}
	// Stored as the panel stores it, with keys GetConfig drops.
	if err := s.SettingService.SetConfig(`{"log":{"disabled":true},"inbounds":[],"route":{"rules":[{"domain":["a"],"action":"reject"},{"domain":["b"],"outbound":"ruled"}]}}`); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	err := save(t, s, "outbounds", "del", `"ruled"`)
	if err == nil || !strings.Contains(err.Error(), "named by route rule #2") {
		t.Fatalf("deleting an outbound a rule names must be refused, got %v", err)
	}
	renamed := fmt.Sprintf(`{"id":%d,"type":"direct","tag":"renamed"}`, ruled.Id)
	if err = save(t, s, "outbounds", "edit", renamed); err == nil || !strings.Contains(err.Error(), "named by route rule #2") {
		t.Fatalf("renaming an outbound a rule names must be refused, got %v", err)
	}
	var tags []string
	db.Model(model.Outbound{}).Where("id = ?", ruled.Id).Pluck("tag", &tags)
	if len(tags) != 1 || tags[0] != "ruled" {
		t.Fatalf("a refused delete or rename must leave the row, got %v", tags)
	}
	edited := fmt.Sprintf(`{"id":%d,"type":"direct","tag":"ruled","connect_timeout":"5s"}`, ruled.Id)
	if err = save(t, s, "outbounds", "edit", edited); err != nil {
		t.Fatalf("an edit that keeps the tag must still go through: %v", err)
	}
	if err = save(t, s, "outbounds", "del", `"free"`); err != nil {
		t.Fatalf("deleting an outbound nothing names: %v", err)
	}
	// Each committed save above tries to start the core; a failed start would
	// put every later test in the package in the start cooldown.
	if !lastStartFailTime.IsZero() && time.Since(lastStartFailTime) < startCooldown {
		t.Fatal("a save in this test failed to start the core, which leaves the start cooldown armed for the tests after it")
	}
}
