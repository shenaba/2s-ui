package service

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
)

func openInstallIdDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "install.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
}

// The install id seeds the acme renewal minute, which has to stay put across
// restarts. The default is random per process, so the stored row, not the
// default, has to be what every later start reads.
func TestInstallIdIsStoredAndStable(t *testing.T) {
	openInstallIdDB(t)
	var s SettingService

	first, err := s.GetInstallId()
	if err != nil || first == "" {
		t.Fatalf("GetInstallId() = %q, %v", first, err)
	}
	// A later start draws a different default; the stored id must win.
	saved := defaultValueMap["installId"]
	defaultValueMap["installId"] = "another-process-default"
	t.Cleanup(func() { defaultValueMap["installId"] = saved })

	again, err := s.GetInstallId()
	if err != nil || again != first {
		t.Errorf("after a restart GetInstallId() = %q, %v; want the stored %q", again, err, first)
	}
}

// A default that could not be stored is this process's random value, not the
// install's: handing it back would move the renewal minute on every start.
func TestInstallIdReportsAFailedSave(t *testing.T) {
	openInstallIdDB(t)
	db := database.GetDB()
	if err := db.Exec(`CREATE TRIGGER refuse_settings BEFORE INSERT ON settings
		BEGIN SELECT RAISE(ABORT, 'read-only'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	var s SettingService
	if id, err := s.GetInstallId(); err == nil {
		t.Errorf("GetInstallId() = %q with the save refused, want an error", id)
	}
}

// installId is bookkeeping, not a form field: it must not reach the settings
// page, where the form would post it back.
func TestInstallIdStaysOffTheSettingsEndpoint(t *testing.T) {
	openInstallIdDB(t)
	var s SettingService
	all, err := s.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*all)["installId"]; ok {
		t.Error("GetAllSetting exposes installId")
	}
}
