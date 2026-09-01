package database

import (
	"encoding/json"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func initUser() error {
	var count int64
	err := db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
}

func OpenDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, 01740)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	// _cache_size=-200 caps each connection's page cache at ~200 KiB
	// (default is ~2 MiB), reducing memory amplification if a connection
	// escapes the pool.
	//
	// _txlock=immediate makes every transaction take the write lock up front.
	// Without it a deferred transaction that reads and then writes — which is
	// what every read-modify-write here is, client.Links being the hottest —
	// gets SQLITE_BUSY when another connection committed in between, and
	// _busy_timeout does NOT cover that case: the busy handler is not invoked
	// for a stale-snapshot conflict, so the transaction just fails. Taking the
	// lock at BEGIN turns that into an ordinary lock wait, which _busy_timeout
	// does cover. Every transaction in this codebase writes, so nothing pays
	// for a write lock it does not need.
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL&_cache_size=-200&_txlock=immediate"
	db, err = gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	if config.IsDebug() {
		db = db.Debug()
	}
	return nil
}

func InitDB(dbPath string) error {
	err := OpenDB(dbPath)
	if err != nil {
		return err
	}

	// Default Outbounds
	if !db.Migrator().HasTable(&model.Outbound{}) {
		db.Migrator().CreateTable(&model.Outbound{})
		defaultOutbound := []model.Outbound{
			{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		}
		db.Create(&defaultOutbound)
	}

	if err = dedupStats(); err != nil {
		return err
	}

	err = db.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Service{},
		&model.Endpoint{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
		&model.Node{},
		&model.Cert{},
		&model.LoginAttempt{},
	)
	if err != nil {
		return err
	}
	err = initUser()
	if err != nil {
		return err
	}

	// The sing-box 1.14 option migrations. Each is one-shot and marks itself
	// done with its own settings row, so they cost one indexed lookup per start
	// after the first. Order matters only for the first two: migrateSingBox114
	// rewrites an inline tls.acme block into a certificate_provider, which
	// migrateCertificateProviders then hoists into the shared list.
	err = migrateSingBox114()
	if err != nil {
		return err
	}
	err = migrateCertificateProviders()
	if err != nil {
		return err
	}
	err = repairRuleSetHTTPClients()
	if err != nil {
		return err
	}
	err = migrateHysteriaQUICFields()
	if err != nil {
		return err
	}
	err = migrateRemovedOptions()
	if err != nil {
		return err
	}
	// The two deprecations that cannot be migrated without changing how names
	// resolve are reported for the operator instead; see migrateSingBox114.
	reportSingBox114Manual()

	return nil
}

// dedupStats merges traffic for duplicate groups of (resource, tag, date_time, direction)
func dedupStats() error {
	if !db.Migrator().HasTable(&model.Stats{}) {
		return nil
	}

	var dupGroups int64
	err := db.Raw("SELECT COUNT(*) FROM (SELECT 1 FROM stats GROUP BY resource, tag, date_time, direction HAVING COUNT(*) > 1)").Scan(&dupGroups).Error
	if err != nil {
		return err
	}
	if dupGroups == 0 {
		return nil
	}
	log.Printf("stats: collapsing %d duplicate group(s) before adding unique index", dupGroups)

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TEMP TABLE stats_dedup AS
			SELECT MIN(id) AS id, resource, tag, date_time, direction, SUM(traffic) AS traffic
			FROM stats GROUP BY resource, tag, date_time, direction`).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM stats").Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO stats (id, resource, tag, date_time, direction, traffic)
			SELECT id, resource, tag, date_time, direction, traffic FROM stats_dedup`).Error; err != nil {
			return err
		}
		return tx.Exec("DROP TABLE stats_dedup").Error
	})
}

func GetDB() *gorm.DB {
	return db
}

// CloseDBForTest closes the pool and puts the package back to its pre-InitDB
// state. A test that installs a global DB has to call it: closing without
// clearing the handle would leave GetDB serving a live *gorm.DB wrapped around
// a dead pool, which fails as "sql: database is closed" from somewhere that
// never opened one.
//
// Named for tests because it must not become a shutdown hook. Clearing the
// handle makes GetDB return nil, which no caller checks; APP.Stop runs
// cronJob.Stop first but that does not wait for in-flight jobs, and the
// scheduler is built without cron.Recover, so the first GetDB().Model(...) in a
// still-running job would panic the process instead of logging. Shutdown leaves
// the pool open and lets the process exit take it down.
func CloseDBForTest() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	db = nil
	return sqlDB.Close()
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
