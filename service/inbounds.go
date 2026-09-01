package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

type InboundService struct {
	ClientService
}

func (s *InboundService) Get(ids string) (*[]map[string]interface{}, error) {
	if ids == "" {
		return s.GetAll()
	}
	return s.getById(ids)
}

func (s *InboundService) getById(ids string) (*[]map[string]interface{}, error) {
	var inbound []model.Inbound
	var result []map[string]interface{}
	db := database.GetDB()
	err := db.Model(model.Inbound{}).Where("id in ?", strings.Split(ids, ",")).Scan(&inbound).Error
	if err != nil {
		return nil, err
	}
	for _, inb := range inbound {
		inbData, err := inb.MarshalFull()
		if err != nil {
			return nil, err
		}
		result = append(result, *inbData)
	}
	return &result, nil
}

func (s *InboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	inbounds := []model.Inbound{}
	err := db.Model(model.Inbound{}).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, inbound := range inbounds {
		var shadowtls_version uint
		ss_managed := false
		inbData := map[string]interface{}{
			"id":     inbound.Id,
			"type":   inbound.Type,
			"tag":    inbound.Tag,
			"tls_id": inbound.TlsId,
		}
		if inbound.NodeId != nil {
			inbData["node_id"] = *inbound.NodeId
		}
		if inbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(inbound.Options, &restFields); err != nil {
				return nil, err
			}
			inbData["listen"] = restFields["listen"]
			inbData["listen_port"] = restFields["listen_port"]
			if inbound.Type == "shadowtls" {
				json.Unmarshal(restFields["version"], &shadowtls_version)
			}
			if inbound.Type == "shadowsocks" {
				json.Unmarshal(restFields["managed"], &ss_managed)
			}
		}
		if s.hasUser(inbound.Type) &&
			!(inbound.Type == "shadowtls" && shadowtls_version < 3) &&
			!(inbound.Type == "shadowsocks" && ss_managed) {
			users := []string{}
			err = db.Raw("SELECT clients.name FROM clients, json_each(clients.inbounds) as je WHERE je.value = ?", inbound.Id).Scan(&users).Error
			if err != nil {
				return nil, err
			}
			inbData["users"] = users
		}

		data = append(data, inbData)
	}
	return &data, nil
}

func (s *InboundService) FromIds(ids []uint) ([]*model.Inbound, error) {
	db := database.GetDB()
	inbounds := []*model.Inbound{}
	err := db.Model(model.Inbound{}).Where("id in ?", ids).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) Save(tx *gorm.DB, act string, data json.RawMessage, initUserIds string, hostname string) error {
	var err error

	switch act {
	case "new", "edit":
		var inbound model.Inbound
		err = inbound.UnmarshalJSON(data)
		if err != nil {
			return err
		}
		// Node replicas are read-only on this panel (edit them on their node;
		// reconciliation refreshes the copy). Rejecting here also prevents an
		// edit without node_id from silently flipping a replica into a local
		// inbound, which would feed a foreign port to the local core.
		if inbound.NodeId != nil {
			return common.NewError("inbound belongs to a node: manage it on the node panel")
		}
		if inbound.TlsId > 0 {
			err = tx.Model(model.Tls{}).Where("id = ?", inbound.TlsId).Find(&inbound.Tls).Error
			if err != nil {
				return err
			}
		}
		var oldTag string
		if act == "edit" {
			var old model.Inbound
			err = tx.Model(model.Inbound{}).Select("tag", "node_id").Where("id = ?", inbound.Id).Find(&old).Error
			if err != nil {
				return err
			}
			if old.NodeId != nil {
				return common.NewError("inbound belongs to a node: manage it on the node panel")
			}
			oldTag = old.Tag
		}

		// Node replicas only ever touch the DB: never the local core (their
		// ports live on the node) and never FillOutJson (their OutJson is the
		// node-side link snapshot, host = the node).
		if inbound.NodeId == nil && corePtr.IsRunning() {
			if act == "edit" {
				err = corePtr.RemoveInbound(oldTag)
				if err != nil && err != os.ErrInvalid {
					return err
				}
			}

			inboundConfig, err := inbound.MarshalJSON()
			if err != nil {
				return err
			}

			if act == "edit" {
				inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
			} else {
				inboundConfig, err = s.initUsers(tx, inboundConfig, initUserIds, inbound.Type)
			}
			if err != nil {
				return err
			}

			err = corePtr.AddInbound(inboundConfig)
			if err != nil {
				return err
			}
		}

		if inbound.NodeId == nil {
			err = util.FillOutJson(&inbound, hostname)
			if err != nil {
				return err
			}
		}

		err = tx.Save(&inbound).Error
		if err != nil {
			return err
		}
		switch act {
		case "new":
			err = s.ClientService.UpdateClientsOnInboundAdd(tx, initUserIds, inbound.Id, hostname)
		case "edit":
			err = s.ClientService.UpdateLinksByInboundChange(tx, &[]model.Inbound{inbound}, hostname, oldTag)
		}
		if err != nil {
			return err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		var old model.Inbound
		err = tx.Model(model.Inbound{}).Select("id", "node_id").Where("tag = ?", tag).Scan(&old).Error
		if err != nil {
			return err
		}
		// Deleting a replica (de-adopt) is DB-only; the inbound keeps running
		// on its node and reconciliation strips the pushed users afterwards.
		if old.NodeId == nil && corePtr.IsRunning() {
			err = corePtr.RemoveInbound(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}
		id := old.Id
		err = s.ClientService.UpdateClientsOnInboundDelete(tx, id, tag)
		if err != nil {
			return err
		}
		err = tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error
		if err != nil {
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
	return nil
}

func (s *InboundService) UpdateOutJsons(tx *gorm.DB, inboundIds []uint, hostname string) error {
	var inbounds []model.Inbound
	// A replica's OutJson is the node-side link snapshot (host = the node);
	// rewriting it with this panel's hostname would point subscriptions here.
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIds).Where("node_id IS NULL").Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = util.FillOutJson(&inbound, hostname)
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("tag = ?", inbound.Tag).Update("out_json", inbound.OutJson).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *InboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var inboundsJson []json.RawMessage
	var inbounds []*model.Inbound
	// node_id IS NULL: replicas of remote inbounds must never reach the local
	// core — their ports live on other machines and binding them here would
	// wedge the core in its 15s restart loop.
	err := db.Model(model.Inbound{}).Preload("Tls").Where("node_id IS NULL").Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		inboundJson, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		inboundJson, err = s.addUsers(db, inboundJson, inbound.Id, inbound.Type)
		if err != nil {
			return nil, err
		}
		inboundsJson = append(inboundsJson, inboundJson)
	}
	return inboundsJson, nil
}

func (s *InboundService) hasUser(inboundType string) bool {
	switch inboundType {
	case "mixed", "socks", "http", "shadowsocks", "snell", "vmess", "trojan", "naive", "hysteria", "shadowtls", "tuic", "hysteria2", "vless", "anytls":
		return true
	}
	return false
}

// fetchUsers collects the per-client config for one inbound type. condition is
// a WHERE fragment and args are its placeholders -- callers must not build
// values into the fragment, since one of them comes from a request.
//
// inboundType is still interpolated, and safely: it is one of the literals in
// hasUser or one of the two ShadowsocksClientConfigKey returns, never anything
// from a request. It has to be, since it names a JSON path rather than a value.
func (s *InboundService) fetchUsers(db *gorm.DB, inboundType string, condition string, inbound map[string]interface{}, args ...any) ([]json.RawMessage, error) {
	if inboundType == "shadowtls" {
		version, _ := inbound["version"].(float64)
		if int(version) < 3 {
			return nil, nil
		}
	}
	if inboundType == "shadowsocks" {
		method, _ := inbound["method"].(string)
		inboundType = util.ShadowsocksClientConfigKey(method)
	}

	var users []string

	// IS NOT NULL is load-bearing, not tidiness: json_extract returns NULL for a
	// client whose config predates the protocol, and Scan into []string fails
	// the whole query on it ("converting NULL to string is unsupported") rather
	// than skipping the row. Every existing client is one of those the moment a
	// protocol is added to hasUser, so without this the first inbound of a new
	// type silently comes up with no users at all.
	err := db.Raw(
		fmt.Sprintf(`SELECT json_extract(clients.config, "$.%s")
		FROM clients WHERE enable = true AND %s
		AND json_extract(clients.config, "$.%s") IS NOT NULL`,
			inboundType, condition, inboundType), args...).Scan(&users).Error
	if err != nil {
		return nil, err
	}
	stripVision := false
	if inboundType == "vless" {
		// flow only applies to raw TCP+TLS: an empty transport type is still
		// TCP, so keep the flow there (upstream #1156).
		transportType := ""
		if tr, ok := inbound["transport"].(map[string]interface{}); ok {
			transportType, _ = tr["type"].(string)
		}
		stripVision = inbound["tls"] == nil || transportType != ""
	}

	var usersJson []json.RawMessage
	for _, user := range users {
		// The query already drops a missing key; this catches a config that
		// stores the key as an empty string. An empty RawMessage is not valid
		// JSON and would fail the whole inbound's marshal.
		if user == "" {
			continue
		}
		if stripVision {
			user = strings.ReplaceAll(user, "xtls-rprx-vision", "")
		}
		usersJson = append(usersJson, json.RawMessage(user))
	}
	return usersJson, nil
}

func (s *InboundService) addUsers(db *gorm.DB, inboundJson []byte, inboundId uint, inboundType string) ([]byte, error) {
	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	err := json.Unmarshal(inboundJson, &inbound)
	if err != nil {
		return nil, err
	}

	const condition = "? IN (SELECT json_each.value FROM json_each(clients.inbounds))"
	inbound["users"], err = s.fetchUsers(db, inboundType, condition, inbound, inboundId)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

// parseClientIds turns the comma-separated `initUsers` request field into ids
// the query can be given as placeholders.
//
// It used to be split on commas and joined straight back -- a round trip that
// changed nothing -- and the result was formatted into the WHERE clause, so the
// field reached SQLite verbatim. An empty field is normal and means "no users
// yet"; anything else that is not a number is a caller that has gone wrong, and
// is reported rather than dropped.
func parseClientIds(raw string) ([]uint, error) {
	var ids []uint
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, common.NewErrorf("invalid client id %q", part)
		}
		ids = append(ids, uint(id))
	}
	return ids, nil
}

func (s *InboundService) initUsers(db *gorm.DB, inboundJson []byte, clientIds string, inboundType string) ([]byte, error) {
	ClientIds, err := parseClientIds(clientIds)
	if err != nil {
		return nil, err
	}
	if len(ClientIds) == 0 {
		return inboundJson, nil
	}

	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	if err = json.Unmarshal(inboundJson, &inbound); err != nil {
		return nil, err
	}

	inbound["users"], err = s.fetchUsers(db, inboundType, "id IN ?", inbound, ClientIds)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s *InboundService) enabledClientNames(tx *gorm.DB, inboundId uint) (map[string]struct{}, error) {
	var names []string
	err := tx.Raw(
		"SELECT clients.name FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ? AND clients.enable = true",
		inboundId).Scan(&names).Error
	if err != nil {
		return nil, err
	}
	keep := make(map[string]struct{}, len(names))
	for _, name := range names {
		keep[name] = struct{}{}
	}
	return keep, nil
}

func (s *InboundService) UpdateInboundsUsers(tx *gorm.DB, ids []uint) error {
	if !corePtr.IsRunning() {
		return nil
	}
	var inbounds []*model.Inbound
	// Same filter as RestartInbounds: a client edit passes in the union of its
	// inbound ids, which may include node replicas. Those are not in the local
	// core, so the fallback below would AddInbound them here and bind the remote
	// node's port locally.
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Where("node_id IS NULL").Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return err
		}
		inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
		if err != nil {
			return err
		}

		// An in-place update that errors leaves the inbound running with its old
		// user table, so a removed user would stay connected and keep
		// authenticating. Fall through to the restart rather than returning:
		// dropping every connection on this inbound is the safe failure.
		handled, err := corePtr.UpdateInboundUsers(inboundConfig)
		if err != nil {
			logger.Warning("in-place user update failed for inbound ", inbound.Tag, ", restarting it: ", err)
			handled = false
		}
		if handled {
			// Disconnect only users no longer enabled on this inbound.
			//
			// Matching is by user name, so rotating a client's UUID or password
			// does NOT drop its live connection -- the name is unchanged, so it
			// stays in `keep` and the old credential keeps working until the
			// connection ends on its own. Tracked separately; see issue.
			keep, err := s.enabledClientNames(tx, inbound.Id)
			if err != nil {
				return err
			}
			closed := 0
			if tracker := liveConnTracker(); tracker != nil {
				closed = tracker.CloseConnByInboundUsers(inbound.Tag, keep)
			}
			logger.Debug("updated users of inbound ", inbound.Tag, " in place, closed ", closed, " stale connections")
			continue
		}

		// Fallback: full restart for protocols without in-place user updates
		err = corePtr.RemoveInbound(inbound.Tag)
		if err != nil && err != os.ErrInvalid {
			return err
		}
		if tracker := liveConnTracker(); tracker != nil {
			tracker.CloseConnByInbound(inbound.Tag)
		}
		err = corePtr.AddInbound(inboundConfig)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *InboundService) RestartInbounds(tx *gorm.DB, ids []uint) error {
	if !corePtr.IsRunning() {
		return nil
	}
	var inbounds []*model.Inbound
	// Client edits pass in the union of their inbound ids, which may include
	// node replicas — those must not be hot-plugged into the local core.
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Where("node_id IS NULL").Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = corePtr.RemoveInbound(inbound.Tag)
		if err != nil && err != os.ErrInvalid {
			return err
		}
		// Close all existing connections
		if tracker := liveConnTracker(); tracker != nil {
			tracker.CloseConnByInbound(inbound.Tag)
		}

		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return err
		}
		inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
		if err != nil {
			return err
		}
		err = corePtr.AddInbound(inboundConfig)
		if err != nil {
			return err
		}
	}
	return nil
}
