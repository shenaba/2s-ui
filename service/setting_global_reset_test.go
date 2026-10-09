package service

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func newSettingDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "setting.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	// Seed the defaults, as the panel does on its first read.
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
}

func saveSettings(t *testing.T, body string) error {
	t.Helper()
	db := database.GetDB()
	tx := db.Begin()
	err := (&SettingService{}).Save(tx, []byte(body))
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func globalResetLast(t *testing.T) int64 {
	t.Helper()
	v, err := (&SettingService{}).GetGlobalResetLast()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A bad spec used to be stored and only logged at the next panel start, after
// which the reset silently never ran.
func TestSaveRejectsInvalidGlobalReset(t *testing.T) {
	newSettingDB(t)
	if err := saveSettings(t, `{"globalReset":"not a cron"}`); err == nil {
		t.Fatal("an invalid cron spec was accepted")
	}
	for _, ok := range []string{"", "off", "@monthly", "0 0 1 * *", "0 0 0 1 * *"} {
		if err := saveSettings(t, `{"globalReset":"`+ok+`"}`); err != nil {
			t.Errorf("spec %q rejected: %v", ok, err)
		}
	}
}

// globalResetLast holds the next boundary of the schedule that armed it.
// Changing the schedule -- or the zone it is read in -- must clear it, or a
// monthly-to-daily switch does nothing for up to a month. Saving the form with
// the schedule unchanged must not, or every unrelated settings save would push
// the reset back.
func TestSaveDisarmsGlobalResetOnChange(t *testing.T) {
	newSettingDB(t)
	svc := &SettingService{}
	if err := saveSettings(t, `{"globalReset":"@monthly"}`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetGlobalResetLast(1234); err != nil {
		t.Fatal(err)
	}

	if err := saveSettings(t, `{"globalReset":"@monthly","subShowInfo":"true"}`); err != nil {
		t.Fatal(err)
	}
	if got := globalResetLast(t); got != 1234 {
		t.Errorf("unchanged schedule: want the armed boundary kept, got %d", got)
	}

	if err := saveSettings(t, `{"globalReset":"@daily"}`); err != nil {
		t.Fatal(err)
	}
	if got := globalResetLast(t); got != 0 {
		t.Errorf("changed schedule: want the boundary cleared, got %d", got)
	}

	if err := svc.SetGlobalResetLast(1234); err != nil {
		t.Fatal(err)
	}
	var zone model.Setting
	database.GetDB().Where("key = ?", "timeLocation").First(&zone)
	other := "Asia/Shanghai"
	if zone.Value == other {
		other = "Europe/Berlin"
	}
	if err := saveSettings(t, `{"timeLocation":"`+other+`"}`); err != nil {
		t.Fatal(err)
	}
	if got := globalResetLast(t); got != 0 {
		t.Errorf("changed zone: want the boundary cleared, got %d", got)
	}
}
