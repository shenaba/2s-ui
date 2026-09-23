package database

import (
	"log"

	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

const migratedKeyPlanDays = "migratedPlanDays"

// migratePlanDays moves the plan length of delay-start clients out of
// reset_days and into its own column.
//
// Until now reset_days meant two things depending on the row: the plan length
// on a client with delay_start on and auto_reset off (ResetClients derives
// Expiry from it), and the reset period everywhere else. The panel's own reset
// schedule mode was later allowed to leave reset_days at zero, so every path
// that cleared a period could silently clear a plan length instead.
//
// Only rows in that one combination carry a plan length. A delay-start client
// that also auto-resets takes its expiry from the Expiry column directly, so
// its reset_days is a period and stays where it is.
//
// Run after AutoMigrate, not from cmd/migration: those repairs assume the
// pre-AutoMigrate schema, where plan_days does not exist yet.
func migratePlanDays() error {
	var flag model.Setting
	err := db.Where("key = ?", migratedKeyPlanDays).First(&flag).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(model.Client{}).
			Where("delay_start = ? AND auto_reset = ? AND reset_days > 0", true, false).
			UpdateColumn("plan_days", gorm.Expr("reset_days"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Printf("clients: moved the plan length of %d delay-start client(s) into plan_days", res.RowsAffected)
		}
		// reset_days is deliberately left as it was. Zeroing it would be a
		// second, destructive write for no gain: ResetClients no longer reads
		// it on these rows, and leaving the old value costs nothing while
		// making the migration re-runnable if its flag were ever lost.
		return tx.Create(&model.Setting{Key: migratedKeyPlanDays, Value: "true"}).Error
	})
}
