package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
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

// The armed boundary carries the schedule it was armed for, and both are
// written together -- also on a panel whose rows were never seeded, where
// they only exist as defaults until the first write.
func TestArmGlobalReset(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "arm.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })
	svc := &SettingService{}

	next, armedFor, err := svc.GetGlobalResetArmed()
	if err != nil || next != 0 || armedFor != "" {
		t.Fatalf("unseeded: want 0 and no schedule, got %d %q %v", next, armedFor, err)
	}
	want := GlobalResetArmedFor("@daily", time.UTC)
	for _, at := range []int64{1234, 5678} {
		if err := svc.ArmGlobalReset(database.GetDB(), at, want); err != nil {
			t.Fatal(err)
		}
		next, armedFor, err = svc.GetGlobalResetArmed()
		if err != nil || next != at || armedFor != want {
			t.Errorf("want %d %q, got %d %q %v", at, want, next, armedFor, err)
		}
	}
	if GlobalResetArmedFor("@daily", time.UTC) == GlobalResetArmedFor("@daily", time.FixedZone("x", 3600)) {
		t.Error("a zone change must read as a different schedule")
	}
}

// Both halves are bookkeeping: a settings save carrying either one is refused,
// or a form posting back a stale boundary could re-arm an old schedule.
func TestSaveRefusesGlobalResetBookkeeping(t *testing.T) {
	newSettingDB(t)
	for _, key := range []string{"globalResetLast", "globalResetArmed"} {
		if err := saveSettings(t, `{"`+key+`":"1"}`); err == nil {
			t.Errorf("%s was writable through the settings endpoint", key)
		}
	}
}
