package migration

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// versionBefore reports whether dot-separated numeric version a sorts before
// b. The gates below used plain string compares, which break once a segment
// reaches two digits ("1.5.10" < "1.5.7" lexically). Missing or non-numeric
// segments count as 0, so "1.5" == "1.5.0" and "" sorts first.
func versionBefore(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}

// MigrateDb applies the one-off data repairs older releases left pending and
// stamps the database with the running version. Callers must run it before the
// panel opens the database: the repairs below assume the pre-AutoMigrate
// schema, exactly as `sui migrate` has always seen it.
//
// It reports failures instead of killing the process, because the panel now
// calls it on every start-up: a data repair that cannot be applied must not
// keep the panel from booting. Panics become errors for the same reason — the
// gates below parse whatever version string the database happens to hold.
// Progress goes to stdout because `sui migrate` is a CLI command whose entire
// output this is.
func MigrateDb() error {
	return migrateDb(os.Stdout)
}

// MigrateDbQuietly is MigrateDb with the progress lines discarded. The panel
// calls it on every start-up, where the answer is almost always "already up to
// date"; printing that would add three lines of untimestamped, unlevelled text
// to stdout on every restart, on a channel SUI_LOG_LEVEL cannot turn down.
// Discarded rather than routed to logger: only app.Init initialises logger, so
// the same call from `sui migrate` would dereference a nil handle.
func MigrateDbQuietly() error {
	return migrateDb(io.Discard)
}

func migrateDb(out io.Writer) (err error) {
	// void running on first install
	path := config.GetDBPath()
	if _, statErr := os.Stat(path); statErr != nil {
		fmt.Fprintln(out, "Database not found")
		return nil
	}

	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}()

	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	// Registered after the close above so it runs first (defers are LIFO): the
	// transaction must be settled while the connection is still open. Recovery
	// lives here rather than in its own defer so a panic cannot reach a commit
	// with err still nil and write a half-applied migration.
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			err = fmt.Errorf("migration panicked: %v", r)
			return
		}
		if err != nil {
			tx.Rollback()
			return
		}
		err = tx.Commit().Error
	}()

	currentVersion := config.GetVersion()
	dbVersion := ""
	tx.Raw("SELECT value FROM settings WHERE key = ?", "version").Find(&dbVersion)
	fmt.Fprintln(out, "Current version:", currentVersion, "\nDatabase version:", dbVersion)

	if currentVersion == dbVersion {
		fmt.Fprintln(out, "Database is up to date, no need to migrate")
		return nil
	}

	fmt.Fprintln(out, "Start migrating database...")

	// Before 1.2
	if dbVersion == "" {
		if err = to1_1(tx); err != nil {
			return fmt.Errorf("migration to 1.1 failed: %w", err)
		}
		if err = to1_2(tx); err != nil {
			return fmt.Errorf("migration to 1.2 failed: %w", err)
		}
		dbVersion = "1.2"
	}

	// Before 1.3
	if strings.HasPrefix(dbVersion, "1.2") {
		if err = to1_3(tx); err != nil {
			return fmt.Errorf("migration to 1.3 failed: %w", err)
		}
	}

	// 2s-ui version line: both upstream migrations below first ship with
	// 2s-ui 1.5.4, and our users' dbVersion follows 2s-ui releases (1.4.2 ..
	// 1.5.3), NOT upstream's. Gate on 1.5.4, not the upstream version numbers,
	// or every existing install would skip them. Both are idempotent.

	// Back-fill self-signed TLS public-key pins and rewrite OutJson
	if versionBefore(dbVersion, "1.5.4") {
		if err = to1_5_1(tx); err != nil {
			return fmt.Errorf("migration to 1.5.1 failed: %w", err)
		}
	}

	// Hash any plaintext admin passwords
	if versionBefore(dbVersion, "1.5.4") {
		if err = to1_5_2(tx); err != nil {
			return fmt.Errorf("migration to 1.5.2 failed: %w", err)
		}
	}

	// Strip server-only TLS fields leaked into client-facing JSON (#51)
	if versionBefore(dbVersion, "1.5.7") {
		if err = to1_5_7(tx); err != nil {
			return fmt.Errorf("migration to 1.5.7 failed: %w", err)
		}
	}

	// Set version
	if err = tx.Exec("UPDATE settings SET value = ? WHERE key = ?", currentVersion, "version").Error; err != nil {
		return fmt.Errorf("update version failed: %w", err)
	}
	fmt.Fprintln(out, "Migration done!")
	return nil
}
