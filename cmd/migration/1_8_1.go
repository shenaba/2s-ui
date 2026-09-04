package migration

import (
	"encoding/json"

	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

// to1_8_1 gives the clients that already existed the snell credentials 1.8.0
// only handed to new ones.
//
// snell arrived in 1.8.0 with an entry in inboundTypesWithUsers, but nothing
// back-filled the clients already in the database. A client whose config has no
// snell block is dropped by InboundService.fetchUsers -- "$.snell" extracts to
// NULL -- so a snell listener built out of pre-1.8.0 clients comes up with an
// empty user list, which sing-box serves as a single-user, psk-only listener
// rather than refusing. That listener never tags a connection with a user, so
// the panel has nothing to attribute traffic to (issue #143).
//
// Only snell is back-filled: every other protocol in inboundTypesWithUsers
// predates the clients here. Idempotent, and a client that already has the
// block is left alone -- re-running this must never rotate a key in use.
func to1_8_1(tx *gorm.DB) error {
	type clientRow struct {
		Id     uint
		Name   string
		Config []byte
	}
	var clients []clientRow
	if err := tx.Raw("SELECT id, name, config FROM clients").Scan(&clients).Error; err != nil {
		return err
	}
	for _, row := range clients {
		if len(row.Config) == 0 {
			continue
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(row.Config, &config); err != nil {
			continue
		}
		if _, exists := config["snell"]; exists {
			continue
		}
		// The name is what sing-box reports as the connection's user, and that
		// is the only thing tying traffic back to this row -- it has to be the
		// client's own name, as NewClientConfig writes it.
		snell, err := json.Marshal(map[string]string{
			"name":    row.Name,
			"userkey": common.Random(32),
		})
		if err != nil {
			return err
		}
		config["snell"] = snell
		newConfig, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if err := tx.Exec("UPDATE clients SET config = ? WHERE id = ?", newConfig, row.Id).Error; err != nil {
			return err
		}
	}
	return nil
}
