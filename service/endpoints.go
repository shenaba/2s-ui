package service

import (
	"encoding/json"
	"os"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

type EndpointService struct {
	WarpService
}

func (o *EndpointService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	endpoints := []*model.Endpoint{}
	err := db.Model(model.Endpoint{}).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, endpoint := range endpoints {
		epData := map[string]interface{}{
			"id":     endpoint.Id,
			"type":   endpoint.Type,
			"tag":    endpoint.Tag,
			"tls_id": endpoint.TlsId,
			"ext":    endpoint.Ext,
		}
		if endpoint.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(endpoint.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				epData[k] = v
			}
		}
		data = append(data, epData)
	}
	return &data, nil
}

func (o *EndpointService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var endpointsJson []json.RawMessage
	var endpoints []*model.Endpoint
	err := db.Model(model.Endpoint{}).Preload("Tls").Find(&endpoints).Error
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		endpointJson, err := endpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		endpointsJson = append(endpointsJson, endpointJson)
	}
	return endpointsJson, nil
}

// Save writes an endpoint and applies it to the running core. restartCore is
// as in OutboundService.Save.
func (s *EndpointService) Save(tx *gorm.DB, act string, data json.RawMessage) (restartCore bool, err error) {
	switch act {
	case "new", "edit":
		var endpoint model.Endpoint
		err = endpoint.UnmarshalJSON(data)
		if err != nil {
			return false, err
		}
		if endpoint.TlsId > 0 {
			err = tx.Model(model.Tls{}).Where("id = ?", endpoint.TlsId).Find(&endpoint.Tls).Error
			if err != nil {
				return false, err
			}
		}

		// Before the WARP calls below: registering one creates a device with
		// Cloudflare, which a refusal here would leave behind.
		err = tagFreeAcross(tx, model.Endpoint{}, "endpoint", endpoint.Tag, endpoint.Id)
		if err != nil {
			return false, err
		}

		if endpoint.Type == "warp" {
			if act == "new" {
				err = s.WarpService.RegisterWarp(&endpoint)
				if err != nil {
					return false, err
				}
			} else {
				var old_license string
				err = tx.Model(model.Endpoint{}).Select("json_extract(CAST(ext AS TEXT), '$.license_key')").Where("id = ?", endpoint.Id).Find(&old_license).Error
				if err != nil {
					return false, err
				}
				err = s.WarpService.SetWarpLicense(old_license, &endpoint)
				if err != nil {
					return false, err
				}
			}
		}

		if corePtr.IsRunning() {
			configData, err := endpoint.MarshalJSON()
			if err != nil {
				return false, err
			}
			var old model.Endpoint
			if act == "edit" {
				err = tx.Model(model.Endpoint{}).Preload("Tls").Where("id = ?", endpoint.Id).Find(&old).Error
				if err != nil {
					return false, err
				}
			}
			oldTag := old.Tag
			if act == "edit" && oldTag == endpoint.Tag && sameConfig(old.MarshalJSON, configData) && corePtr.HasEndpoint(oldTag) {
				// Saved as it was, or with only panel-side fields changed:
				// as in OutboundService.Save, nothing for the core to do.
			} else if act == "edit" && oldTag == endpoint.Tag && len(corePtr.EndpointDependents(oldTag)) > 0 {
				// As in OutboundService.Save: applied by a restart after the
				// commit, vetted now. A listen port another socket holds
				// still only shows on the restart.
				err = corePtr.ValidateEndpoint(configData)
				if err != nil {
					return false, err
				}
				restartCore = true
			} else if act == "edit" && oldTag != endpoint.Tag {
				// See InboundService.Save.
				err = corePtr.RenameEndpoint(oldTag, configData)
				if err != nil {
					return false, err
				}
			} else {
				err = corePtr.AddEndpoint(configData)
				if err != nil {
					return false, err
				}
			}
		}

		err = tx.Save(&endpoint).Error
		if err != nil {
			return false, err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return false, err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveEndpoint(tag)
			if err != nil && err != os.ErrInvalid {
				return false, err
			}
		}
		err = tx.Where("tag = ?", tag).Delete(model.Endpoint{}).Error
		if err != nil {
			return false, err
		}
	default:
		return false, common.NewErrorf("unknown action: %s", act)
	}
	return restartCore, nil
}

// RestartEndpoints reloads the given endpoints in a running core, so a change
// to the TLS config they reference takes effect without restarting the panel.
//
// Each is replaced in place, so a certificate that does not load leaves the
// old one running (core/managers.go). One that a selector or urltest uses
// cannot be: they resolved it at start and would keep the closed instance, and
// refusing would refuse the certificate change with it. For those restartCore
// asks the caller to restart the core once the change is committed -- rare
// enough (a certificate on an endpoint in a group) to be worth the dropped
// connections over a group silently dialing a closed endpoint.
func (s *EndpointService) RestartEndpoints(tx *gorm.DB, ids []uint) (restartCore bool, err error) {
	if !corePtr.IsRunning() {
		return false, nil
	}
	var endpoints []*model.Endpoint
	err = tx.Model(model.Endpoint{}).Preload("Tls").Where("id in ?", ids).Find(&endpoints).Error
	if err != nil {
		return false, err
	}
	for _, endpoint := range endpoints {
		if len(corePtr.EndpointDependents(endpoint.Tag)) > 0 {
			restartCore = true
			continue
		}
		endpointConfig, err := endpoint.MarshalJSON()
		if err != nil {
			return false, err
		}
		err = corePtr.AddEndpoint(endpointConfig)
		if err != nil {
			return false, err
		}
	}
	return restartCore, nil
}
