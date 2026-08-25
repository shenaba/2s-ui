package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
