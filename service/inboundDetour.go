package service

import (
	"encoding/json"
	"strings"

	"github.com/shenaba/2s-ui/database/jsonsql"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

// An inbound's client config can name another inbound as its detour (#255):
// the subscription then has the client dial through that inbound's client
// config, the way a shadowsocks route rides a ShadowTLS one. The reference is
// the inbound tag, since that is what the operator picks and what the
// subscription resolves. Nothing in the core sees it, so everything that keeps
// it honest lives here: a rename carries over, a delete is refused, and a
// target with no client config to ride is refused.

// clientConfigHead is the part of a stored client config the detour checks
// read, decoded once so the two cannot disagree about what parses. Anything
// that is not an object, or a field that is not a string, reads as "".
//
// An empty Type means no client config at all: FillOutJson empties a
// ShadowTLS v1/v2 one down to its tls block, and the listeners with no client
// half keep whatever was stored. Neither can be dialled, ridden or not.
type clientConfigHead struct {
	Type   string `json:"type"`
	Detour string `json:"detour"`
}

func readClientConfig(outJson json.RawMessage) clientConfigHead {
	var head clientConfigHead
	// A field of the wrong type fails only that field; the error is the
	// "reads as empty" this wants.
	_ = json.Unmarshal(outJson, &head)
	return head
}

// detourColumn is the detour a stored client config names, as SQL. One row
// whose out_json is not JSON -- restored from an old backup, written by hand --
// would make a bare json_extract fail the whole query, and with it every
// delete and rename; json_valid reads such a row as naming nothing instead.
// jsonsql reads the column as text, whatever the driver stored.
var detourColumn = "CASE WHEN " + jsonsql.Valid("out_json") +
	" THEN " + jsonsql.Extract("out_json", "$.detour") + " END"

// clientDetourRiders lists the local inbounds other than id whose client
// config rides tag. Left alone, a rider of a renamed or deleted inbound would
// quietly fall back to dialling itself directly, which in the ShadowTLS setup
// is a loopback-only port.
func clientDetourRiders(tx *gorm.DB, id uint, tag string) ([]string, error) {
	var riders []string
	err := tx.Model(model.Inbound{}).
		Where("id <> ? AND node_id IS NULL AND "+detourColumn+" = ?", id, tag).
		Order("id").Pluck("tag", &riders).Error
	return riders, err
}

// renameClientDetours carries a rename of inbound id over to the client
// configs that ride it, the way a rename already carries over to client links.
func renameClientDetours(tx *gorm.DB, id uint, oldTag, newTag string) error {
	var riders []model.Inbound
	err := tx.Model(model.Inbound{}).Select("id", "out_json").
		Where("id <> ? AND node_id IS NULL AND "+detourColumn+" = ?", id, oldTag).
		Find(&riders).Error
	if err != nil {
		return err
	}
	for _, rider := range riders {
		var outJson map[string]interface{}
		if err := json.Unmarshal(rider.OutJson, &outJson); err != nil {
			return err
		}
		outJson["detour"] = newTag
		raw, err := json.MarshalIndent(outJson, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("id = ?", rider.Id).Update("out_json", json.RawMessage(raw)).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// checkClientDetour refuses an inbound save that would leave a detour no
// subscription can honour. Both checks need only the database, so they run
// before the core is touched.
//
// tag is the name riders know the inbound by: its stored tag on an edit, which
// a rename has not carried over to them yet.
func checkClientDetour(tx *gorm.DB, inbound *model.Inbound, tag string) error {
	// What it rides. A tag no local inbound carries is left alone: it may name
	// an outbound the subscription carries under exactly that tag, an
	// external link's, and the subscription drops it if it names nothing.
	head := readClientConfig(inbound.OutJson)
	if detour := head.Detour; detour != "" {
		if detour == inbound.Tag {
			return common.NewErrorf("inbound %s: a client config cannot be its own detour", inbound.Tag)
		}
		var target model.Inbound
		err := tx.Model(model.Inbound{}).Select("id", "type", "out_json").
			Where("tag = ? AND node_id IS NULL AND id <> ?", detour, inbound.Id).Limit(1).Find(&target).Error
		if err != nil {
			return err
		}
		if target.Id != 0 && readClientConfig(target.OutJson).Type == "" {
			return common.NewErrorf("inbound %s: detour %s has no client config to ride (ShadowTLS before v3 has none)",
				inbound.Tag, detour)
		}
	}
	// What rides it: the same reason, from the other side -- a ShadowTLS
	// moved back to v1/v2 under its riders.
	if head.Type == "" && tag != "" {
		riders, err := clientDetourRiders(tx, inbound.Id, tag)
		if err != nil {
			return err
		}
		if len(riders) > 0 {
			return common.NewErrorf("inbound %s is the detour of the client config of %s, and would no longer have one to ride",
				tag, strings.Join(riders, ", "))
		}
	}
	return nil
}
