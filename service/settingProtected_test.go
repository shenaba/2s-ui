package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// The settings endpoint is a plain POST api/save with object "settings", open
// to any authenticated caller and to every apiv2 token holder. It stripped
// these keys on the way out but accepted them on the way in, so `secret` could
// be re-keyed (logging every operator out) and `config` replaced without the
// core restart the real path performs.
func TestSettingSaveRefusesProtectedKeys(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "setting.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	settings := SettingService{}
	db := database.GetDB()

	// Seed the rows and remember what the protected ones hold.
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	before := map[string]string{}
	for key := range protectedSettings {
		var row model.Setting
		if err := db.Model(model.Setting{}).Where("key = ?", key).First(&row).Error; err == nil {
			before[key] = row.Value
		}
	}

	for key := range protectedSettings {
		payload, _ := json.Marshal(map[string]string{key: "hijacked"})
		err := settings.Save(db, payload)
		if err == nil {
			t.Errorf("%q was accepted by the settings endpoint", key)
			continue
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the error for %q does not name it: %v", key, err)
		}
	}

	for key, want := range before {
		var row model.Setting
		if err := db.Model(model.Setting{}).Where("key = ?", key).First(&row).Error; err != nil {
			t.Errorf("read %q back: %v", key, err)
			continue
		}
		if row.Value != want {
			t.Errorf("%q changed to %q despite the refusal", key, row.Value)
		}
	}

	// One protected key in an otherwise ordinary payload rejects the whole
	// request rather than writing the rest of it.
	payload, _ := json.Marshal(map[string]string{"webDomain": "example.com", "secret": "hijacked"})
	if err := settings.Save(db, payload); err == nil {
		t.Fatal("a mixed payload carrying a protected key was accepted")
	}
	var domain model.Setting
	if err := db.Model(model.Setting{}).Where("key = ?", "webDomain").First(&domain).Error; err == nil {
		if domain.Value == "example.com" {
			t.Error("the rest of a refused payload must not be written")
		}
	}
}

// GetAllSetting must keep stripping them, or the settings form would hand them
// straight back and every save would now fail.
func TestGetAllSettingStripsProtectedKeys(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "setting.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	all, err := (&SettingService{}).GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	for key := range protectedSettings {
		if _, leaked := (*all)[key]; leaked {
			t.Errorf("%q must not reach the settings form", key)
		}
	}
}
