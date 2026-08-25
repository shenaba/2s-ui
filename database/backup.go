package database

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/shenaba/2s-ui/cmd/migration"
	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func GetDb(exclude string) ([]byte, error) {
	exclude_changes, exclude_stats := false, false
	for _, table := range strings.Split(exclude, ",") {
		if table == "changes" {
			exclude_changes = true
		} else if table == "stats" {
			exclude_stats = true
		}
	}

	// A unique scratch path rather than a timestamped one: two exports entering
	// the same second would otherwise share it, and whichever finished first
	// would delete the file out from under the other.
	scratch, err := os.CreateTemp(config.GetDBFolderPath(), config.GetName()+"_*.db")
	if err != nil {
		return nil, err
	}
	dbPath := scratch.Name()
	// Only the name was wanted; SQLite opens the path itself, and on Windows it
	// cannot while this handle is still on it.
	scratch.Close()
	// Registered here rather than after the open below: CreateTemp has already
	// made the file, so a failing open would return above the registration and
	// leave it behind. Still registered before the close for the reason it
	// always was -- defers are LIFO, so this one runs last, and Windows refuses
	// to delete a file that is still open.
	defer os.Remove(dbPath)

	backupDb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if sqlDB, e := backupDb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}()

	err = backupDb.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Endpoint{},
		&model.User{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
		&model.Cert{},
	)
	if err != nil {
		return nil, err
	}

	var settings []model.Setting
	var tls []model.Tls
	var certs []model.Cert
	var inbound []model.Inbound
	var outbound []model.Outbound
	var endpoint []model.Endpoint
	var users []model.User
	var clients []model.Client
	var stats []model.Stats
	var changes []model.Changes

	// Perform scans and handle errors
	if err := db.Model(&model.Setting{}).Scan(&settings).Error; err != nil {
		return nil, err
	} else if len(settings) > 0 {
		if err := backupDb.Save(settings).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Tls{}).Scan(&tls).Error; err != nil {
		return nil, err
	} else if len(tls) > 0 {
		if err := backupDb.Save(tls).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Inbound{}).Scan(&inbound).Error; err != nil {
		return nil, err
	} else if len(inbound) > 0 {
		if err := backupDb.Save(inbound).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Outbound{}).Scan(&outbound).Error; err != nil {
		return nil, err
	} else if len(outbound) > 0 {
		if err := backupDb.Save(outbound).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Endpoint{}).Scan(&endpoint).Error; err != nil {
		return nil, err
	} else if len(endpoint) > 0 {
		if err := backupDb.Save(endpoint).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.User{}).Scan(&users).Error; err != nil {
		return nil, err
	} else if len(users) > 0 {
		if err := backupDb.Save(users).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Client{}).Scan(&clients).Error; err != nil {
		return nil, err
	} else if len(clients) > 0 {
		if err := backupDb.Save(clients).Error; err != nil {
			return nil, err
		}
	}
	// 手动登记的证书记录也要带走,漏了它们会在恢复后从证书页静默消失
	if err := db.Model(&model.Cert{}).Scan(&certs).Error; err != nil {
		return nil, err
	} else if len(certs) > 0 {
		if err := backupDb.Save(certs).Error; err != nil {
			return nil, err
		}
	}

	if !exclude_stats {
		if err := db.Model(&model.Stats{}).Scan(&stats).Error; err != nil {
			return nil, err
		}
		if len(stats) > 0 {
			if err := backupDb.Save(stats).Error; err != nil {
				return nil, err
			}
		}
	}
	if !exclude_changes {
		if err := db.Model(&model.Changes{}).Scan(&changes).Error; err != nil {
			return nil, err
		}
		if len(changes) > 0 {
			if err := backupDb.Save(changes).Error; err != nil {
				return nil, err
			}
		}
	}

	// Update WAL
	err = backupDb.Exec("PRAGMA wal_checkpoint;").Error
	if err != nil {
		return nil, err
	}

	bdb, _ := backupDb.DB()
	bdb.Close()

	// Open the file for reading
	file, err := os.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read the file contents
	fileContents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return fileContents, nil
}

func ImportDB(file multipart.File) error {
	// Check if the file is a SQLite database
	isValidDb, err := IsSQLiteDB(file)
	if err != nil {
		return common.NewErrorf("Error checking db file format: %v", err)
	}
	if !isValidDb {
		return common.NewError("Invalid db file format")
	}

	// Reset the file reader to the beginning
	_, err = file.Seek(0, 0)
	if err != nil {
		return common.NewErrorf("Error resetting file reader: %v", err)
	}

	// Save the file as temporary file
	tempPath := fmt.Sprintf("%s.temp", config.GetDBPath())
	// Remove the existing fallback file (if any) before creating one
	_, err = os.Stat(tempPath)
	if err == nil {
		errRemove := os.Remove(tempPath)
		if errRemove != nil {
			return common.NewErrorf("Error removing existing temporary db file: %v", errRemove)
		}
	}
	// Create the temporary file
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return common.NewErrorf("Error creating temporary db file: %v", err)
	}
	defer tempFile.Close()

	// Remove temp file before returning
	defer os.Remove(tempPath)

	// Close old DB
	old_db, _ := db.DB()
	old_db.Close()

	// The package-level handle outlives that Close, so any path returning from
	// here on without reopening leaves GetDB serving a *gorm.DB whose pool is
	// shut: every later query fails with "sql: database is closed" and the
	// panel stays up but unusable until someone restarts it by hand. Reopen on
	// the way out unless the success path below already did.
	dbReopened := false
	// Whether config.GetDBPath() still holds the panel's own database. It stops
	// doing so the moment the upload is moved into place, and only a successful
	// rollback puts it back -- reopening in between would bring the panel up on
	// the uploaded data, whose users table is not the operator's.
	dbPathIsOriginal := true
	defer func() {
		if dbReopened {
			return
		}
		if !dbPathIsOriginal {
			logger.Error("import left the uploaded database in place and could not roll back, not reopening")
			return
		}
		// Only reopen a database that is still there. InitDB creates the file
		// when it is missing and initUser then seeds admin/admin, so on the path
		// that leaves nothing behind -- the move below failing and the rollback
		// failing with it -- reopening would swap the operator's data for an
		// empty panel anyone can log into. Their database is kept at
		// fallbackPath there; putting it back is not this defer's job.
		if _, statErr := os.Stat(config.GetDBPath()); statErr != nil {
			logger.Error("import left no database in place, not reopening: ", statErr)
			return
		}
		if err := InitDB(config.GetDBPath()); err != nil {
			logger.Warning("import aborted and reopening the database failed: ", err)
		}
	}()

	// Save uploaded file to temporary file
	_, err = io.Copy(tempFile, file)
	if err != nil {
		return common.NewErrorf("Error saving db: %v", err)
	}
	// Close before the renames below rather than leaving it to the deferred
	// Close, which only runs once this function returns: Windows refuses to
	// rename a file that is still open, so every import on that platform used
	// to die at "Error moving db file". The defer stays as a safety net for
	// the early returns above.
	if err = tempFile.Close(); err != nil {
		return common.NewErrorf("Error saving db: %v", err)
	}

	// Check if we can init db or not
	newDb, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		return common.NewErrorf("Error checking db: %v", err)
	}
	newDb_db, _ := newDb.DB()
	if newDb_db != nil {
		newDb_db.Close()
	}

	// Backup the current database for fallback
	fallbackPath := fmt.Sprintf("%s.backup", config.GetDBPath())
	// An existing one here is not stale scratch. An earlier import that could
	// not roll back deliberately leaves the operator's database at this path and
	// names it in the error it returns, and retrying the import is the natural
	// reaction to that failure -- so deleting it, which is what this used to do,
	// would destroy the last copy on exactly the path that was trying to save
	// it. Archive it under a dated name instead and say where it went.
	if _, statErr := os.Stat(fallbackPath); statErr == nil {
		archived := fallbackPath + "." + time.Now().Format("20060102-150405")
		if errRename := os.Rename(fallbackPath, archived); errRename != nil {
			return common.NewErrorf("Error archiving the database an earlier failed import left at %s: %v", fallbackPath, errRename)
		}
		logger.Warning("a database preserved by an earlier failed import was archived at ", archived)
	}
	// Move the current database to the fallback location
	err = os.Rename(config.GetDBPath(), fallbackPath)
	if err != nil {
		return common.NewErrorf("Error backing up temporary db file: %v", err)
	}

	// Removed only once the import has actually taken. On the two paths below
	// where restoring it failed, this file is the operator's last copy of their
	// data, and deleting it here would turn a failed rename into an
	// unrecoverable one.
	importDone := false
	defer func() {
		if importDone {
			os.Remove(fallbackPath)
		}
	}()

	// Move temp to DB path
	err = os.Rename(tempPath, config.GetDBPath())
	if err != nil {
		errRename := os.Rename(fallbackPath, config.GetDBPath())
		if errRename != nil {
			return common.NewErrorf("Error moving db file and restoring fallback: %v -- the original database is kept at %s", errRename, fallbackPath)
		}
		return common.NewErrorf("Error moving db file: %v", err)
	}
	dbPathIsOriginal = false

	// Migrate DB. A failed migration leaves the imported file half-repaired,
	// so restore the fallback instead of booting on it.
	err = migration.MigrateDb()
	if err == nil {
		err = InitDB(config.GetDBPath())
	}
	if err != nil {
		// InitDB opens the pool before it can fail in AutoMigrate or initUser,
		// and the rollback below renames over that very path -- which Windows
		// refuses while this process still holds it open, making the rollback
		// impossible rather than merely unlikely. Release it first.
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		errRename := os.Rename(fallbackPath, config.GetDBPath())
		if errRename != nil {
			return common.NewErrorf("Error migrating db and restoring fallback: %v -- the original database is kept at %s", errRename, fallbackPath)
		}
		dbPathIsOriginal = true
		return common.NewErrorf("Error migrating db: %v", err)
	}
	dbReopened = true
	importDone = true

	// Restart app
	err = SendSighup()
	if err != nil {
		return common.NewErrorf("Error restarting app: %v", err)
	}

	return nil
}

func IsSQLiteDB(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	_, err := file.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}

func SendSighup() error {
	// Get the current process
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}

	// Send SIGHUP to the current process
	go func() {
		time.Sleep(3 * time.Second)
		if runtime.GOOS == "windows" {
			err = process.Kill()
		} else {
			err = process.Signal(syscall.SIGHUP)
		}
		if err != nil {
			logger.Error("send signal SIGHUP failed:", err)
		}
	}()
	return nil
}
