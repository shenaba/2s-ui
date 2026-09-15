package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"

	"github.com/op/go-logging"
)

// initLogger runs once for the whole package rather than per test, because
// these tests start a real core: InitLogger replaces the logger globals without
// synchronisation, and a box logs from goroutines of its own that outlive the
// test that started it.
var initLogger sync.Once

// maintenanceEnv stands up a panel with an empty database and a real core, and
// puts every piece of package state it touches back afterwards.
func maintenanceEnv(t *testing.T) *ConfigService {
	t.Helper()
	initLogger.Do(func() { logger.InitLogger(logging.CRITICAL) })
	if err := database.InitDB(filepath.Join(t.TempDir(), "maintenance.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	previous := corePtr
	s := NewConfigService(core.NewCore())
	t.Cleanup(func() {
		if corePtr != nil {
			_ = corePtr.Stop()
		}
		corePtr = previous
		maintenanceMode.Store(false)
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return s
}

// The switch only works if it holds at the one place the core is started: eight
// paths lead there, and the five-second watchdog walks one of them whether or
// not anybody is looking.
func TestMaintenanceGatesEveryPathThatStartsTheCore(t *testing.T) {
	s := maintenanceEnv(t)
	// A base config that cannot be assembled, so every path that gets as far
	// as reading one reports an error. That is what makes "never reached the
	// core" observable here: startBox undoes a start that raced the switch, so
	// a test that only checked the core ended up down would pass with no gate
	// at all -- and the panel would still assemble a config and build a box
	// every five seconds for as long as maintenance lasted.
	const unusable = "not json"
	if err := s.SettingService.SetConfig(unusable); err != nil {
		t.Fatalf("seed an unusable config: %v", err)
	}
	if err := s.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}

	// What checkCoreJob calls every five seconds. Nothing to report: the core
	// is down because it was asked to be.
	if err := s.StartCore(); err != nil {
		t.Errorf("StartCore under maintenance = %v, want nil: it has to return before it reads the config", err)
	}
	if s.CoreRunning() {
		t.Error("StartCore started the core while it was out of service")
	}

	// A config save restarts the core with the new config. The config is
	// stored either way; it takes effect when maintenance ends.
	if err := s.restartCoreWithConfig(json.RawMessage(unusable)); err != nil {
		t.Errorf("restartCoreWithConfig under maintenance = %v, want nil: same, it has to return first", err)
	}
	if s.CoreRunning() {
		t.Error("a config save started the core while it was out of service")
	}

	// RestartCore is somebody pressing a button, so this one answers instead
	// of going quiet.
	err := s.RestartCore()
	if err == nil {
		t.Fatal("RestartCore under maintenance reported success")
	}
	if !strings.Contains(err.Error(), "maintenance") {
		t.Errorf("RestartCore error = %q, want it to say why", err)
	}
	if s.CoreRunning() {
		t.Error("RestartCore started the core while it was out of service")
	}
}

// Maintenance has to outlive the process, or a panel that reboots in the middle
// of it quietly puts users back online.
func TestMaintenanceSurvivesARestart(t *testing.T) {
	s := maintenanceEnv(t)

	if s.InMaintenance() {
		t.Fatal("a fresh panel starts in service")
	}
	if err := s.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}

	// What the next process does: nothing in memory, the row is all it has.
	// Through NewConfigService rather than loadMaintenance, so the test covers
	// the wiring app.Init actually uses.
	maintenanceMode.Store(false)
	s = NewConfigService(core.NewCore())
	if !s.InMaintenance() {
		t.Error("maintenance did not survive a restart")
	}

	if err := s.SetMaintenance(false); err != nil {
		t.Fatalf("SetMaintenance(false): %v", err)
	}
	maintenanceMode.Store(true)
	s = NewConfigService(core.NewCore())
	if s.InMaintenance() {
		t.Error("switching maintenance off did not survive a restart")
	}
}

// The settings form and the switch would otherwise disagree -- the flag set
// with the core still serving clients, or cleared with the core still down.
func TestMaintenanceIsNotASettingsField(t *testing.T) {
	s := maintenanceEnv(t)
	if err := s.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}

	all, err := s.SettingService.GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	if _, ok := (*all)[maintenanceKey]; ok {
		t.Error("the settings form is handed the maintenance flag, so it will send it back")
	}

	tx := database.GetDB().Begin()
	defer tx.Rollback()
	if err := s.SettingService.Save(tx, json.RawMessage(`{"maintenance":"false"}`)); err == nil {
		t.Error("the settings endpoint wrote the maintenance flag without touching the core")
	}

	if !s.InMaintenance() {
		t.Error("a rejected settings save changed the switch anyway")
	}
}

// The gates are read before the config is assembled, which reads most of the
// database. A switch thrown inside that window issues a stop that finds nothing
// running, and without this the core comes up seconds later and stays up.
func TestStartBoxUndoesAStartThatRacedTheSwitch(t *testing.T) {
	maintenanceEnv(t)
	config := []byte(`{}`)

	up, err := startBox(config)
	if err != nil {
		t.Fatalf("startBox on an in-service panel: %v", err)
	}
	if !up || !corePtr.IsRunning() {
		t.Fatalf("startBox reported up=%v, running=%v; want both true", up, corePtr.IsRunning())
	}
	if err := corePtr.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The switch lands after the gate and before the core is up.
	maintenanceMode.Store(true)
	up, err = startBox(config)
	if err != nil {
		t.Fatalf("startBox racing the switch = %v, want no error: the operator got what they asked for", err)
	}
	if up {
		t.Error("startBox reported the core up after maintenance took it down")
	}
	if corePtr.IsRunning() {
		t.Error("a start that raced the switch left the core running with maintenance on")
	}
}

// The scheduled report and the bot's /status both read this line. "stopped" on
// its own is what a crash looks like, and it is the one thing the report must
// not say about a core the operator took down on purpose.
func TestStatusDigestSaysWhyTheCoreIsDown(t *testing.T) {
	s := maintenanceEnv(t)
	coreLine := func() string {
		lines := strings.Split(StatusDigest("en"), "\n")
		return lines[len(lines)-1]
	}

	if want := "Core " + notify.Label("en", "digest.stopped"); coreLine() != want {
		t.Errorf("core line = %q, want %q for a core that is simply down", coreLine(), want)
	}

	if err := s.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}
	if want := "Core " + notify.Label("en", "digest.maintenance"); coreLine() != want {
		t.Errorf("core line = %q, want %q", coreLine(), want)
	}
}

// running:false alone cannot tell a core that was stopped on purpose from one
// that crashed, and the panel has to draw them differently.
func TestMaintenanceIsReportedInTheStatusPayload(t *testing.T) {
	s := maintenanceEnv(t)
	var server ServerService

	if got := server.GetSingboxInfo()["maintenance"]; got != false {
		t.Errorf("maintenance = %v on an in-service panel, want false", got)
	}
	if err := s.SetMaintenance(true); err != nil {
		t.Fatalf("SetMaintenance(true): %v", err)
	}
	if got := server.GetSingboxInfo()["maintenance"]; got != true {
		t.Errorf("maintenance = %v after the switch, want true", got)
	}
}
