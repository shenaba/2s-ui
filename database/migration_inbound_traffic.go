package database

import (
	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

const migratedKeyInboundTraffic = "migratedInboundTraffic"

// migrateInboundTraffic seeds the inbounds' up/down counters (#289) from the
// stats table, once.
//
// The inbounds list used to show a sum over that table, so seeding from it
// keeps the figure an operator already sees: an inbound does not read as
// having moved nothing on the first start after the upgrade. It is only as
// complete as the table was -- traffic history off, or past its retention, is
// not there to count -- which is exactly what the list showed before.
//
// Run after AutoMigrate, which is what adds the columns. Local inbounds only:
// a replica's traffic is never counted here.
func migrateInboundTraffic() error {
	var flag model.Setting
	err := db.Where("key = ?", migratedKeyInboundTraffic).First(&flag).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Exec(`UPDATE inbounds SET
			up = COALESCE((SELECT SUM(traffic) FROM stats
				WHERE stats.resource = 'inbound' AND stats.tag = inbounds.tag AND stats.direction = 1), 0),
			down = COALESCE((SELECT SUM(traffic) FROM stats
				WHERE stats.resource = 'inbound' AND stats.tag = inbounds.tag AND stats.direction = 0), 0)
			WHERE node_id IS NULL`).Error
		if err != nil {
			return err
		}
		return tx.Create(&model.Setting{Key: migratedKeyInboundTraffic, Value: "true"}).Error
	})
}
