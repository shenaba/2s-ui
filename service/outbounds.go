package service

import (
	"encoding/json"
	"os"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

type OutboundService struct{}

func (o *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	outbounds := []*model.Outbound{}
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, outbound := range outbounds {
		outData := map[string]interface{}{
			"id":   outbound.Id,
			"type": outbound.Type,
			"tag":  outbound.Tag,
		}
		if outbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(outbound.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				outData[k] = v
			}
		}
		data = append(data, outData)
	}
	return &data, nil
}

// probeSkippedTypes are the outbound types the reachability probe leaves alone.
// direct, block and dns never dial the remote a URL test would measure, and
// selector/urltest are groups -- probing one reports the health of whichever
// member it currently points at, filed under the group's tag.
var probeSkippedTypes = map[string]bool{
	"direct": true, "block": true, "dns": true, "selector": true, "urltest": true,
}

// ProbeTargets lists the outbound tags worth reachability-testing.
func (o *OutboundService) ProbeTargets() ([]string, error) {
	var rows []model.Outbound
	err := database.GetDB().Model(model.Outbound{}).Select("type", "tag").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Tag == "" || probeSkippedTypes[row.Type] {
			continue
		}
		tags = append(tags, row.Tag)
	}
	return tags, nil
}

func (o *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var outboundsJson []json.RawMessage
	var outbounds []*model.Outbound
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		outboundJson, err := outbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		outboundsJson = append(outboundsJson, outboundJson)
	}
	return outboundsJson, nil
}

// Save writes an outbound and applies it to the running core. restartCore
// reports an edit the core cannot take in place -- of an outbound a selector or
// urltest uses, which resolved it at start -- and that the caller applies by
// restarting the core once the change is committed.
func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) (restartCore bool, err error) {
	switch act {
	case "new", "edit":
		var outbound model.Outbound
		err = outbound.UnmarshalJSON(data)
		if err != nil {
			return false, err
		}

		err = tagFreeAcross(tx, model.Outbound{}, "outbound", outbound.Tag, outbound.Id)
		if err != nil {
			return false, err
		}
		if act == "edit" {
			// A rename takes the old tag out of the config; see refuseRuleNamedTag.
			var oldTags []string
			err = tx.Model(model.Outbound{}).Where("id = ?", outbound.Id).Pluck("tag", &oldTags).Error
			if err != nil {
				return false, err
			}
			if len(oldTags) > 0 && oldTags[0] != outbound.Tag {
				if err = refuseRuleNamedTag(tx, "outbound", oldTags[0]); err != nil {
					return false, err
				}
			}
		}

		if corePtr.IsRunning() {
			configData, err := outbound.MarshalJSON()
			if err != nil {
				return false, err
			}
			var old model.Outbound
			if act == "edit" {
				err = tx.Model(model.Outbound{}).Where("id = ?", outbound.Id).Find(&old).Error
				if err != nil {
					return false, err
				}
			}
			oldTag := old.Tag
			switch {
			case act == "edit" && oldTag == outbound.Tag && sameConfig(old.MarshalJSON, configData) && corePtr.HasOutbound(oldTag):
				// Saved as it was: nothing for the core to do, and for a
				// group's member the restart below would drop every
				// connection on the panel for nothing. Only while the core
				// does hold it: saving one unchanged is also how an operator
				// puts back one the core lost.
			case act == "edit" && oldTag == outbound.Tag && len(corePtr.OutboundDependents(oldTag)) > 0:
				// Replacing it would leave the groups on the closed instance,
				// and refusing would make the edit impossible without
				// deleting every group first. Vetted now so that what the
				// core would refuse at start -- a cycle, a missing detour --
				// is still refused before the commit, not left to take the
				// whole core down on the restart.
				err = corePtr.ValidateOutbound(configData)
				if err != nil {
					return false, err
				}
				restartCore = true
			case act == "edit" && oldTag != outbound.Tag:
				// A rename starts the new one before removing the old, so a
				// failure on either side leaves the core as it was. An
				// outbound holds no port, so the two can overlap.
				err = corePtr.AddOutbound(configData)
				if err != nil {
					return false, err
				}
				err = corePtr.RemoveOutbound(oldTag)
				if err != nil && err != os.ErrInvalid {
					_ = corePtr.RemoveOutbound(outbound.Tag)
					return false, err
				}
			default:
				// One that keeps its tag is replaced in place (see
				// InboundService.Save).
				err = corePtr.AddOutbound(configData)
				if err != nil {
					return false, err
				}
			}
		}

		err = tx.Save(&outbound).Error
		if err != nil {
			return false, err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return false, err
		}
		if err = refuseRuleNamedTag(tx, "outbound", tag); err != nil {
			return false, err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveOutbound(tag)
			if err != nil && err != os.ErrInvalid {
				return false, err
			}
		}
		err = tx.Where("tag = ?", tag).Delete(model.Outbound{}).Error
		if err != nil {
			return false, err
		}
	default:
		return false, common.NewErrorf("unknown action: %s", act)
	}
	return restartCore, nil
}
