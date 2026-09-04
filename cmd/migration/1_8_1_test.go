package migration

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// One connection, or each pooled connection gets its own empty :memory: db.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Client{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func clientConfig(t *testing.T, db *gorm.DB, name string) map[string]json.RawMessage {
	t.Helper()
	// A string, not []byte: gorm reads a []byte destination as a slice of rows.
	var raw string
	if err := db.Raw("SELECT config FROM clients WHERE name = ?", name).Scan(&raw).Error; err != nil {
		t.Fatalf("read config for %q: %v", name, err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("config for %q is not an object: %v (%s)", name, err, raw)
	}
	return config
}

// A client that predates snell is invisible to a snell listener until it has a
// key of its own, and the name has to be the client's -- it is what sing-box
// reports as the connection's user, and the only thing tying traffic back.
func TestTo1_8_1BackfillsSnell(t *testing.T) {
	db := newMigrationDB(t)
	if err := db.Create(&model.Client{
		Name: "alice", Group: "user",
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		Config:   json.RawMessage(`{"vless":{"name":"alice","uuid":"u"}}`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	if err := to1_8_1(db); err != nil {
		t.Fatalf("to1_8_1: %v", err)
	}

	config := clientConfig(t, db, "alice")
	var snell struct {
		Name    string `json:"name"`
		UserKey string `json:"userkey"`
	}
	if err := json.Unmarshal(config["snell"], &snell); err != nil {
		t.Fatalf("no snell block added: %v", err)
	}
	if snell.Name != "alice" {
		t.Errorf("snell name = %q, want the client's own name", snell.Name)
	}
	if len(snell.UserKey) != 32 {
		t.Errorf("userkey = %q, want 32 characters", snell.UserKey)
	}
	if _, kept := config["vless"]; !kept {
		t.Errorf("the other protocols were dropped: %v", config)
	}
}

// The panel runs migrations on every start-up, so a second pass must not hand
// out a new key: every client already connecting with the old one would break.
func TestTo1_8_1KeepsAnExistingKey(t *testing.T) {
	db := newMigrationDB(t)
	if err := db.Create(&model.Client{
		Name: "bob", Group: "user",
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		Config:   json.RawMessage(`{"snell":{"name":"bob","userkey":"keep-me"}}`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	if err := to1_8_1(db); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if err := to1_8_1(db); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	config := clientConfig(t, db, "bob")
	var snell struct {
		UserKey string `json:"userkey"`
	}
	if err := json.Unmarshal(config["snell"], &snell); err != nil {
		t.Fatalf("snell block lost: %v", err)
	}
	if snell.UserKey != "keep-me" {
		t.Errorf("userkey = %q, want the stored one untouched", snell.UserKey)
	}
}

// A config that is not an object at all is a row this migration cannot repair;
// it must be stepped over rather than failing the whole start-up.
func TestTo1_8_1SkipsUnreadableConfig(t *testing.T) {
	db := newMigrationDB(t)
	if err := db.Create(&model.Client{
		Name: "broken", Group: "user",
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		Config:   json.RawMessage(`not json`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	if err := to1_8_1(db); err != nil {
		t.Fatalf("to1_8_1: %v", err)
	}

	var raw string
	if err := db.Raw("SELECT config FROM clients WHERE name = ?", "broken").Scan(&raw).Error; err != nil {
		t.Fatalf("read config: %v", err)
	}
	if raw != "not json" {
		t.Errorf("config = %q, want it left as it was", raw)
	}
}

// A config stored as the JSON literal `null` unmarshals into a nil map without
// an error, and writing to that map would panic -- which migrateDb converts
// into a rolled-back transaction, so one such row would keep every client on
// the panel from being back-filled, on every start-up.
func TestTo1_8_1HandlesNullConfig(t *testing.T) {
	db := newMigrationDB(t)
	if err := db.Create(&model.Client{
		Name: "nully", Group: "user",
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		Config:   json.RawMessage(`null`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	if err := to1_8_1(db); err != nil {
		t.Fatalf("to1_8_1: %v", err)
	}

	config := clientConfig(t, db, "nully")
	var snell struct {
		Name    string `json:"name"`
		UserKey string `json:"userkey"`
	}
	if err := json.Unmarshal(config["snell"], &snell); err != nil {
		t.Fatalf("no snell block added: %v", err)
	}
	if snell.Name != "nully" || len(snell.UserKey) != 32 {
		t.Errorf("snell block = %+v, want the client's name and a 32-character key", snell)
	}
}
