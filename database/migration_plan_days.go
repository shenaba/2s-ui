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
// on a client with delay_start on and auto_reset off (ResetClients derived
// Expiry from it), and the reset period everywhere else.
//
// Only rows in that one combination carry a plan length, and only they are
// moved. A delay-start client with auto reset took its expiry from the Expiry
// column and never from reset_days, so it has no plan length to move -- and
// must not be given one: the first-use step now applies a plan length whenever
// one is set, so copying its period across would make a client that has never
// had a time limit start expiring. plan_days = 0 is what "no plan length"
// means, and that is exactly what those rows are.
//
// plan_days = 0 in the condition keeps a re-run from overwriting a value set
// since, should the flag ever be lost.
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
			Where("delay_start = ? AND auto_reset = ? AND reset_days > 0 AND plan_days = 0", true, false).
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
