package database

import (
	"log"
	"strings"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

const migratedKeyResetSchedule = "migratedResetSchedule"

// migrateResetSchedule brings rows written by main into the shape the reset
// schedule has now: auto reset owns it, and a client without auto reset has no
// period and no delayed start.
//
// main let a delay-start client without auto reset carry a plan length in
// reset_days: its first connection set Expiry to that many days later. That
// feature is gone. Clients that already connected keep the Expiry it gave them;
// the ones still waiting lose it and will not expire unless an expiry is set,
// so their names are logged for an operator who relied on it.
//
// Every row without auto reset then has delay_start and the period cleared.
// Delay start without auto reset would hold a clock that does not exist. A
// leftover period is a field the drawer does not show that still fails
// validation on an unrelated edit -- main never cleared a plan length after
// first use, and 99999 was a common way to write "lifetime".
//
// Run after AutoMigrate, not from cmd/migration: those repairs assume the
// pre-AutoMigrate schema, where reset_day_of_month does not exist yet.
func migrateResetSchedule() error {
	var flag model.Setting
	err := db.Where("key = ?", migratedKeyResetSchedule).First(&flag).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var waiting []string
		if err := tx.Model(model.Client{}).
			Where("delay_start = ? AND auto_reset = ? AND reset_days > 0", true, false).
			Pluck("name", &waiting).Error; err != nil {
			return err
		}
		if len(waiting) > 0 {
			log.Printf("clients: \"expire N days after the first connection\" is no longer supported; "+
				"%d client(s) had not connected yet and will not expire unless an expiry is set: %s",
				len(waiting), strings.Join(waiting, ", "))
		}
		if err := tx.Model(model.Client{}).
			Where("auto_reset = ? AND (delay_start = ? OR reset_days <> 0 OR reset_day_of_month <> 0)", false, true).
			UpdateColumns(map[string]interface{}{"delay_start": false, "reset_days": 0, "reset_day_of_month": 0}).
			Error; err != nil {
			return err
		}
		return tx.Create(&model.Setting{Key: migratedKeyResetSchedule, Value: "true"}).Error
	})
}
