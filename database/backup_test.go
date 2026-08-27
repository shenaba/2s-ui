package database

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ImportDB closes the connection pool before it starts renaming files, and the
// package-level handle outlives that close. A failure path that returns without
// reopening therefore leaves GetDB serving a *gorm.DB whose pool is shut: every
// later query fails with "sql: database is closed" and the panel stays up but
// unusable until someone restarts it by hand.
//
// The import below fails in the migration, which is also what pins the temp
// file being closed before the renames: Windows cannot rename an open file, so
// without that close this fails at "Error moving db file" instead and never
// reaches the migration at all.
//
// It deliberately stops at a failing import. The success path ends in
// SendSighup, which on Windows kills the current process — it would take the
// test binary down with it.
func TestImportDBKeepsPoolUsableWhenImportFails(t *testing.T) {
	// ImportDB reports several of its recovery decisions through logger, whose
	// handle is nil until something initialises it -- app.Init is the only
	// caller that does, so a test reaching one of those paths panics instead of
	// failing.
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)

	if err := InitDB(config.GetDBPath()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer CloseDBForTest()

	// Marker row, so a passing pool check cannot be satisfied by the import.
	if err := GetDB().Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "test_marker", "original").Error; err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	// A valid SQLite file the migration cannot apply: the version gates it into
	// to1_5_7, but inbounds has no `addrs` column for it to rewrite.
	badPath := filepath.Join(dir, "unmigratable.db")
	bad, err := gorm.Open(sqlite.Open(badPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open backup fixture: %v", err)
	}
	for _, stmt := range []string{
		"CREATE TABLE settings (id INTEGER PRIMARY KEY AUTOINCREMENT, key TEXT UNIQUE, value TEXT)",
		"INSERT INTO settings (key, value) VALUES ('version', '1.5.6-alpha.1')",
		"CREATE TABLE tls (id INTEGER PRIMARY KEY AUTOINCREMENT, server BLOB, client BLOB)",
		"CREATE TABLE inbounds (id INTEGER PRIMARY KEY AUTOINCREMENT, tls_id INTEGER DEFAULT 0, out_json BLOB)",
		"CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT, password TEXT)",
	} {
		if err := bad.Exec(stmt).Error; err != nil {
			t.Fatalf("seed backup fixture (%s): %v", stmt, err)
		}
	}
	if sqlDB, e := bad.DB(); e == nil {
		sqlDB.Close()
	}

	f, err := os.Open(badPath)
	if err != nil {
		t.Fatalf("open backup fixture: %v", err)
	}
	defer f.Close()

	err = ImportDB(f)
	if err == nil {
		t.Fatal("expected ImportDB to reject an unmigratable backup")
	}
	if !strings.Contains(err.Error(), "Error migrating db") {
		t.Fatalf("expected the import to fail in the migration, got: %v", err)
	}

	var count int64
	if err := GetDB().Model(&model.Setting{}).Count(&count).Error; err != nil {
		t.Fatalf("connection pool unusable after a failed import: %v", err)
	}

	var marker string
	if err := GetDB().Raw("SELECT value FROM settings WHERE key = ?", "test_marker").Scan(&marker).Error; err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if marker != "original" {
		t.Fatalf("expected the original database back, marker = %q", marker)
	}
}

// loginAttemptsTable is the one table GetDb deliberately leaves out; see the
// comment on its AutoMigrate list.
const loginAttemptsTable = "login_attempts"

// GetDb names the tables it copies twice over -- once in AutoMigrate, once in
// the scan/Save pairs -- and a table missing from either list is dropped from
// the backup with no error anywhere. Three were missing this way (services,
// tokens, nodes), which only showed up when someone restored a backup and found
// their cluster gone.
//
// Rather than pin the three, this seeds every table the live schema has and
// compares row counts against the backup, so the next table added to db.go and
// forgotten here fails the same way.
func TestGetDbBacksUpEveryTable(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)

	if err := InitDB(config.GetDBPath()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer CloseDBForTest()

	seedEveryTable(t)

	raw, err := GetDb("")
	if err != nil {
		t.Fatalf("GetDb: %v", err)
	}
	backupPath := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(backupPath, raw, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	backupDb, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() {
		if sqlDB, e := backupDb.DB(); e == nil {
			sqlDB.Close()
		}
	}()

	// The live schema is the authority on what a complete backup contains --
	// not a list repeated here, which would rot exactly like the two in GetDb.
	var liveTables []string
	if err := GetDB().Raw(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'",
	).Scan(&liveTables).Error; err != nil {
		t.Fatalf("list live tables: %v", err)
	}
	if len(liveTables) < 10 {
		t.Fatalf("expected the live schema to have every table, got %d: %v", len(liveTables), liveTables)
	}

	for _, table := range liveTables {
		if table == loginAttemptsTable {
			continue
		}
		var live int64
		if err := GetDB().Raw("SELECT COUNT(*) FROM " + table).Scan(&live).Error; err != nil {
			t.Fatalf("count %s in the live db: %v", table, err)
		}
		// A table nothing seeded would pass 0 == 0 without proving anything.
		if live == 0 {
			t.Fatalf("table %q has no seeded rows, so this test cannot tell whether GetDb copies it -- add it to seedEveryTable", table)
		}

		var backed int64
		if err := backupDb.Raw("SELECT COUNT(*) FROM " + table).Scan(&backed).Error; err != nil {
			t.Errorf("table %q is missing from the backup: %v", table, err)
			continue
		}
		if backed != live {
			t.Errorf("table %q: backup has %d rows, live db has %d", table, backed, live)
		}
	}

	// The exemption is deliberate, so assert it rather than leaving it implied.
	// Asked as HasTable rather than a COUNT so a correct run does not log a
	// "no such table" error that reads like a failure.
	if backupDb.Migrator().HasTable(loginAttemptsTable) {
		t.Errorf("%s was copied into the backup; restoring stale bans is not wanted", loginAttemptsTable)
	}
}

// seedEveryTable puts at least one row in every table InitDB does not already
// populate. That is only users and outbounds -- the settings defaults are
// written by SettingService.GetAllSetting, which app.Init calls after InitDB,
// so the table is still empty here.
func seedEveryTable(t *testing.T) {
	t.Helper()
	now := time.Now().Unix()
	obj := json.RawMessage(`{}`)
	arr := json.RawMessage(`[]`)

	rows := []any{
		&model.Setting{Key: "webPort", Value: "2095"},
		&model.Tls{Name: "tls-1", Server: obj, Client: obj},
		&model.Inbound{Type: "vless", Tag: "in-1", Addrs: arr, OutJson: obj, Options: obj},
		&model.Service{Type: "derp", Tag: "svc-1", Options: obj},
		&model.Endpoint{Type: "wireguard", Tag: "ep-1", Options: obj, Ext: obj},
		&model.Tokens{Desc: "token-1", Token: "t0ken", UserId: 1},
		&model.Stats{DateTime: now, Resource: "user", Tag: "client-1", Direction: true, Traffic: 1024},
		&model.Client{Name: "client-1", Enable: true, Config: obj, Inbounds: arr, Links: arr, CreatedAt: now},
		&model.Changes{DateTime: now, Actor: "test", Key: "clients", Action: "new", Obj: json.RawMessage(`"client-1"`)},
		&model.Node{Name: "node-1", BaseUrl: "https://node.example", WebPath: "/app/", Token: "n0de", Baselines: obj},
		&model.Cert{Domain: "example.com", CertFile: "/etc/cert.pem", KeyFile: "/etc/key.pem"},
		&model.LoginAttempt{Scope: "ip", Key: "1.2.3.4", Failures: 1, WindowStart: now},
	}
	for _, row := range rows {
		if err := GetDB().Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
}
