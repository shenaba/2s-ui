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

func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	var err error

	switch act {
	case "new", "edit":
		var outbound model.Outbound
		err = outbound.UnmarshalJSON(data)
		if err != nil {
			return err
		}

		if corePtr.IsRunning() {
			configData, err := outbound.MarshalJSON()
			if err != nil {
				return err
			}
			if act == "edit" {
				var oldTag string
				err = tx.Model(model.Outbound{}).Select("tag").Where("id = ?", outbound.Id).Find(&oldTag).Error
				if err != nil {
					return err
				}
				err = corePtr.RemoveOutbound(oldTag)
				if err != nil && err != os.ErrInvalid {
					return err
				}
			}
			err = corePtr.AddOutbound(configData)
			if err != nil {
				return err
			}
		}

		err = tx.Save(&outbound).Error
		if err != nil {
			return err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveOutbound(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}
		err = tx.Where("tag = ?", tag).Delete(model.Outbound{}).Error
		if err != nil {
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
	return nil
}
