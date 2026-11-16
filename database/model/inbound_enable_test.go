package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func inboundTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func storedEnable(t *testing.T, db *gorm.DB, tag string) bool {
	t.Helper()
	var enable bool
	if err := db.Raw("SELECT enable FROM inbounds WHERE tag = ?", tag).Scan(&enable).Error; err != nil {
		t.Fatalf("read %s: %v", tag, err)
	}
	return enable
}

// GORM leaves a zero value out of the INSERT for a column with a default, so a
// disabled inbound used to be stored enabled -- through Create and through the
// Save the inbound service creates with alike.
func TestInboundCreateKeepsDisabled(t *testing.T) {
	db := inboundTestDB(t)
	if err := db.AutoMigrate(&Inbound{}); err != nil {
		t.Fatal(err)
	}
	off := &Inbound{Type: "vless", Tag: "off", Enable: false, EnableSet: true}
	if err := db.Create(off).Error; err != nil {
		t.Fatal(err)
	}
	saved := &Inbound{Type: "vless", Tag: "saved-off", Enable: false, EnableSet: true}
	if err := db.Save(saved).Error; err != nil {
		t.Fatal(err)
	}
	on := &Inbound{Type: "vless", Tag: "on", Enable: true}
	if err := db.Create(on).Error; err != nil {
		t.Fatal(err)
	}
	// A literal that never mentions enable is an enabled inbound, not a
	// disabled one by zero value.
	unsaid := &Inbound{Type: "vless", Tag: "unsaid"}
	if err := db.Create(unsaid).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		row  *Inbound
		want bool
	}{{off, false}, {saved, false}, {on, true}, {unsaid, true}} {
		if got := storedEnable(t, db, tc.row.Tag); got != tc.want {
			t.Errorf("%s stored enable=%v, want %v", tc.row.Tag, got, tc.want)
		}
		if tc.row.Enable != tc.want {
			t.Errorf("%s struct enable=%v after create, want %v", tc.row.Tag, tc.row.Enable, tc.want)
		}
	}

	// An update writes false like any other value.
	on.Enable = false
	if err := db.Save(on).Error; err != nil {
		t.Fatal(err)
	}
	if storedEnable(t, db, "on") {
		t.Error("Save of an existing row did not store enable=false")
	}
}

// Rows from before the column read as enabled once it is added.
func TestInboundEnableColumnDefaultsOn(t *testing.T) {
	db := inboundTestDB(t)
	if err := db.Exec(`CREATE TABLE inbounds (id integer PRIMARY KEY AUTOINCREMENT, type text, tag text UNIQUE,
		tls_id integer, node_id integer, addrs blob, out_json blob, options blob)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO inbounds (type, tag) VALUES ('vless', 'old')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Inbound{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var row Inbound
	if err := db.Where("tag = ?", "old").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !row.Enable {
		t.Error("an inbound from before the column reads as disabled")
	}
}

func TestInboundEnableJSON(t *testing.T) {
	var in Inbound
	if err := in.UnmarshalJSON([]byte(`{"type":"vless","tag":"a","listen_port":1}`)); err != nil {
		t.Fatal(err)
	}
	if !in.Enable || in.EnableSet {
		t.Errorf("absent: Enable=%v EnableSet=%v, want true/false", in.Enable, in.EnableSet)
	}
	if err := in.UnmarshalJSON([]byte(`{"type":"vless","tag":"a","enable":false}`)); err != nil {
		t.Fatal(err)
	}
	if in.Enable || !in.EnableSet {
		t.Errorf("false: Enable=%v EnableSet=%v, want false/true", in.Enable, in.EnableSet)
	}
	if strings.Contains(string(in.Options), "enable") {
		t.Errorf("enable left in Options, where the core would reject it: %s", in.Options)
	}
	// The shapes scripts send are taken as meant.
	for raw, want := range map[string]bool{`"false"`: false, `"TRUE"`: true, `0`: false, `1`: true, `" true "`: true} {
		if err := in.UnmarshalJSON([]byte(`{"type":"vless","tag":"a","enable":` + raw + `}`)); err != nil {
			t.Errorf("enable %s refused: %v", raw, err)
			continue
		}
		if in.Enable != want || !in.EnableSet {
			t.Errorf("enable %s: Enable=%v EnableSet=%v, want %v/true", raw, in.Enable, in.EnableSet, want)
		}
	}
	// Anything that says neither is refused rather than guessed at.
	for _, raw := range []string{`"yes"`, `2`, `[]`, `{}`} {
		if err := in.UnmarshalJSON([]byte(`{"type":"vless","tag":"a","enable":` + raw + `}`)); err == nil {
			t.Errorf("enable %s was accepted", raw)
		}
	}
	if err := in.UnmarshalJSON([]byte(`{"type":"vless","tag":"a","enable":false}`)); err != nil {
		t.Fatal(err)
	}

	in.Enable = false
	singBox, err := in.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(singBox), "enable") {
		t.Errorf("the sing-box shape carries enable: %s", singBox)
	}
	full, err := in.MarshalFull()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(full)
	if (*full)["enable"] != false {
		t.Errorf("the panel shape lost enable: %s", raw)
	}
}
