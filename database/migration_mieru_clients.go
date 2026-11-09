package database

import (
	"encoding/json"
	"log"

	"github.com/shenaba/2s-ui/database/jsonsql"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

// backfillMieruClientConfig gives every client a mieru credential block.
//
// A client's config holds one block per protocol, and a listener's user list is
// built from the block of its own type: a client with none is left out of a
// mieru inbound and cannot connect, with nothing saying why. Clients created
// before mieru existed have no block, and the client drawer only ever renames
// the blocks a config already has, so re-saving them never adds one.
//
// The password is the client's trojan one where there is one -- the same
// shared password NewClientConfig gives a new client for trojan, anytls and
// mieru alike -- so a client keeps one secret across those protocols.
//
// It is not a one-shot migration with a settings flag: anything that writes a
// client row without a mieru block (a restored backup, an import, the API)
// would otherwise stay broken. The WHERE clause keeps a start with nothing to
// do to one query.
func backfillMieruClientConfig() error {
	return backfillMieruClients(db)
}

func backfillMieruClients(tx *gorm.DB) error {
	var clients []model.Client
	err := tx.Model(model.Client{}).
		Where(jsonsql.Extract("config", "$.mieru") + " IS NULL").
		Find(&clients).Error
	if err != nil {
		return err
	}
	filled := 0
	for _, client := range clients {
		configs := map[string]json.RawMessage{}
		if len(client.Config) > 0 {
			if err := json.Unmarshal(client.Config, &configs); err != nil {
				// Not this migration's to repair: the row is unreadable for
				// every protocol, and stopping the panel over it would be worse.
				log.Printf("clients: config of client %q is not a JSON object, mieru credentials not added: %v", client.Name, err)
				continue
			}
		}
		block, err := json.Marshal(map[string]string{
			"name":     client.Name,
			"password": sharedPassword(configs),
		})
		if err != nil {
			return err
		}
		configs["mieru"] = block
		config, err := json.MarshalIndent(configs, "", "  ")
		if err != nil {
			return err
		}
		if err = tx.Model(model.Client{}).Where("id = ?", client.Id).
			UpdateColumn("config", config).Error; err != nil {
			return err
		}
		filled++
	}
	if filled > 0 {
		log.Printf("clients: added mieru credentials to %d client(s)", filled)
	}
	return nil
}

// sharedPassword is the password a client already uses for the protocols that
// share one, or a fresh one when it has none of them.
func sharedPassword(configs map[string]json.RawMessage) string {
	for _, key := range []string{"trojan", "anytls"} {
		var block struct {
			Password string `json:"password"`
		}
		if json.Unmarshal(configs[key], &block) == nil && block.Password != "" {
			return block.Password
		}
	}
	return common.Random(10)
}
