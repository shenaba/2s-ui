package migration

import (
	"testing"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service/notify"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// The migration spells the kind out instead of importing it; a rename on either
// side would silently append a kind nothing publishes.
func TestCertFailedKindMatchesNotify(t *testing.T) {
	if certFailedKind != string(notify.CertRenewFailed) {
		t.Fatalf("certFailedKind = %q, notify.CertRenewFailed = %q", certFailedKind, notify.CertRenewFailed)
	}
}

func TestAppendNotifyKind(t *testing.T) {
	tests := []struct {
		name, in, want string
		changed        bool
	}{
		{"appended", "node.down,core.crash", "node.down,core.crash,cert.failed", true},
		{"spaces and empty entries folded", " node.down, ,core.crash ", "node.down,core.crash,cert.failed", true},
		{"already there", "node.down,cert.failed", "node.down,cert.failed", false},
		// Every event switched off is a choice; a new one must not appear in it.
		{"empty list left alone", "", "", false},
		{"blank list left alone", " , ", " , ", false},
	}
	for _, tt := range tests {
		got, changed := appendNotifyKind(tt.in, certFailedKind)
		if got != tt.want || changed != tt.changed {
			t.Errorf("%s: appendNotifyKind(%q) = %q, %v; want %q, %v", tt.name, tt.in, got, changed, tt.want, tt.changed)
		}
	}
}

func TestTo2_1_1(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// One connection, or each pooled connection gets its own empty :memory: db.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	read := func(key string) string {
		t.Helper()
		var v string
		if err := db.Raw("SELECT value FROM settings WHERE key = ?", key).Scan(&v).Error; err != nil {
			t.Fatal(err)
		}
		return v
	}

	// An install that never saved notifyEvents has no row: the default covers it,
	// and the migration must not create one.
	if err := to2_1_1(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&model.Setting{}).Where("key = ?", "notifyEvents").Count(&n)
	if n != 0 {
		t.Fatalf("created %d notifyEvents rows on an install that had none", n)
	}

	db.Create(&model.Setting{Key: "notifyEvents", Value: "node.down,login.banned"})
	db.Create(&model.Setting{Key: "notifyLang", Value: "en"})
	for i := 0; i < 2; i++ { // the second run proves it idempotent
		if err := to2_1_1(db); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := read("notifyEvents"), "node.down,login.banned,cert.failed"; got != want {
		t.Errorf("notifyEvents = %q, want %q", got, want)
	}
	if got := read("notifyLang"); got != "en" {
		t.Errorf("an unrelated setting changed: notifyLang = %q", got)
	}
}
