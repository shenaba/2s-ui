package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

type ClientService struct{}

func (s *ClientService) Get(id string) (*[]model.Client, error) {
	if id == "" {
		return s.GetAll()
	}
	return s.getById(id)
}

// getById returns the whole row on purpose — do NOT narrow it to
// clientListColumns. The drawer edits from this shape, and the counters it
// omits have no omitempty, so a projection here would send zeros the client
// would then echo back as an intentional-looking value.
func (s *ClientService) getById(id string) (*[]model.Client, error) {
	db := database.GetDB()
	var client []model.Client
	err := db.Model(model.Client{}).Where("id in ?", strings.Split(id, ",")).Scan(&client).Error
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (s *ClientService) GetAll() (*[]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select(clientListColumns).
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	return &clients, nil
}

// clientListColumns deliberately omits config and links: both are large and
// config carries every protocol's credentials. This projection feeds the client
// list, the websocket full payload and every save response, so anything added
// here is paid on all three. Consumers needing the full row fetch it by id.
const clientListColumns = "`id`, `enable`, `name`, `desc`, `group`, `remark`, `inbounds`, `up`, `down`, `volume`, `expiry`, `created_at`, `online_at`, `limit_ip`"

// GetAllWithConfig adds config for the cluster reconcile diff only: clientDiffers
// compares it, and an absent key reads as "always different", which would re-push
// every client every round. Reached solely by the node-facing apiv2 clients read.
func (s *ClientService) GetAllWithConfig() (*[]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select(clientListColumns + ", `config`").
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	return &clients, nil
}

func (s *ClientService) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, error) {
	var err error
	var inboundIds []uint

	switch act {
	case "new", "edit":
		var client model.Client
		err = json.Unmarshal(data, &client)
		if err != nil {
			return nil, err
		}
		if act == "edit" {
			// Only a name actually changing is validated, so a duplicate that
			// predates this check stays editable on its other fields — same
			// stance as the node-name bracket rule.
			var oldName string
			if err = tx.Model(model.Client{}).Select("name").
				Where("id = ?", client.Id).Scan(&oldName).Error; err != nil {
				return nil, err
			}
			if oldName != client.Name {
				if err = s.ensureNameAvailable(tx, client.Name, client.Id); err != nil {
					return nil, err
				}
			}
		} else if client.Group != clusterGroup {
			// A cluster push is exempt: runReconcile aborts the WHOLE round on a
			// failed push, so one name a node happens to use locally would stop
			// that node syncing entirely — a worse failure than the duplicate
			// this check exists to prevent. A same-group duplicate cannot reach
			// here anyway: actualClusterClients would have found it and the
			// master would be pushing an edit instead of a new.
			if err = s.ensureNameAvailable(tx, client.Name, 0); err != nil {
				return nil, err
			}
		}
		if err = setConfigIdentity(&client); err != nil {
			return nil, err
		}
		err = s.updateLinksWithFixedInbounds(tx, []*model.Client{&client}, hostname)
		if err != nil {
			return nil, err
		}
		if act == "edit" {
			// Find changed inbounds
			inboundIds, err = s.findInboundsChanges(tx, &client, false)
			if err != nil {
				return nil, err
			}
			if err = s.preserveServerManagedFields(tx, &client, payloadFields(data)); err != nil {
				return nil, err
			}
		} else {
			client.CreatedAt = time.Now().Unix()
			err = json.Unmarshal(client.Inbounds, &inboundIds)
			if err != nil {
				return nil, err
			}
		}
		err = tx.Save(&client).Error
		if err != nil {
			return nil, err
		}
	case "addbulk":
		var clients []*model.Client
		err = json.Unmarshal(data, &clients)
		if err != nil {
			return nil, err
		}
		now := time.Now().Unix()
		// Name check for the whole batch in one query rather than per client —
		// this path imports hundreds at a time. `seen` covers duplicates within
		// the batch, which the table cannot show: none of them are committed yet.
		names := make([]string, 0, len(clients))
		seen := make(map[string]bool, len(clients))
		for _, client := range clients {
			if seen[client.Name] {
				return nil, common.NewErrorf("duplicate client name in this batch: %q", client.Name)
			}
			seen[client.Name] = true
			names = append(names, client.Name)
		}
		var taken []string
		if err = tx.Model(model.Client{}).Where("name in ?", names).
			Pluck("name", &taken).Error; err != nil {
			return nil, err
		}
		if len(taken) > 0 {
			return nil, common.NewErrorf("client name %q already exists", taken[0])
		}
		for _, client := range clients {
			if err = setConfigIdentity(client); err != nil {
				return nil, err
			}
			client.CreatedAt = now
			var ids []uint
			if err = json.Unmarshal(client.Inbounds, &ids); err != nil {
				return nil, err
			}
			inboundIds = common.UnionUintArray(inboundIds, ids)
		}
		err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
		if err != nil {
			return nil, err
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, err
		}
	case "editbulk":
		var clients []*model.Client
		err = json.Unmarshal(data, &clients)
		if err != nil {
			return nil, err
		}
		// Renames are validated as a batch — one query for the stored names,
		// then a conflict check only for the ones actually changing. The SPA
		// never renames through this path (it submits the list projection
		// unchanged), so the common case costs a single extra query.
		ids := make([]uint, 0, len(clients))
		for _, client := range clients {
			if client.Id != 0 {
				ids = append(ids, client.Id)
			}
		}
		oldNameById := make(map[uint]string, len(ids))
		if len(ids) > 0 {
			var stored []struct {
				Id   uint
				Name string
			}
			if err = tx.Model(model.Client{}).Select("id", "name").
				Where("id in ?", ids).Scan(&stored).Error; err != nil {
				return nil, err
			}
			for _, row := range stored {
				oldNameById[row.Id] = row.Name
			}
		}
		renamedTo := make(map[string]bool)
		for _, client := range clients {
			if oldNameById[client.Id] == client.Name {
				continue
			}
			// Two rows renamed to the same new name would both pass the DB
			// check — neither is committed yet.
			if renamedTo[client.Name] {
				return nil, common.NewErrorf("duplicate client name in this batch: %q", client.Name)
			}
			renamedTo[client.Name] = true
			if err = s.ensureNameAvailable(tx, client.Name, client.Id); err != nil {
				return nil, err
			}
		}
		for _, client := range clients {
			changedInboundIds, err := s.findInboundsChanges(tx, client, true)
			if err != nil {
				return nil, err
			}
			if err = setConfigIdentity(client); err != nil {
				return nil, err
			}
			// nil: the bulk payload is the list projection, so none of the
			// counters it carries are authoritative — take them all from the row.
			if err = s.preserveServerManagedFields(tx, client, nil); err != nil {
				return nil, err
			}
			if len(changedInboundIds) > 0 {
				inboundIds = common.UnionUintArray(inboundIds, changedInboundIds)
			}
		}
		if len(inboundIds) > 0 {
			err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
			if err != nil {
				return nil, err
			}
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, err
		}
	case "delbulk":
		var ids []uint
		err = json.Unmarshal(data, &ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			var client model.Client
			err = tx.Where("id = ?", id).First(&client).Error
			if err != nil {
				return nil, err
			}
			var clientInbounds []uint
			err = json.Unmarshal(client.Inbounds, &clientInbounds)
			if err != nil {
				return nil, err
			}
			inboundIds = common.UnionUintArray(inboundIds, clientInbounds)
		}
		err = tx.Where("id in ?", ids).Delete(model.Client{}).Error
		if err != nil {
			return nil, err
		}
	case "del":
		var id uint
		err = json.Unmarshal(data, &id)
		if err != nil {
			return nil, err
		}
		var client model.Client
		err = tx.Where("id = ?", id).First(&client).Error
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(client.Inbounds, &inboundIds)
		if err != nil {
			return nil, err
		}
		err = tx.Where("id = ?", id).Delete(model.Client{}).Error
		if err != nil {
			return nil, err
		}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}

	return inboundIds, nil
}

// ensureNameAvailable rejects a duplicate client name. The name is the cluster's
// identity key — expectedClients and actualClusterClients both map by it — so two
// clients sharing one means only ever syncing whichever the map iteration kept,
// silently and forever. There is no unique index to lean on (the column predates
// the cluster feature and existing installs may already hold duplicates), and the
// only guard until now was the SPA's own check, which apiv2 callers bypass. Same
// loud-failure stance as the adoption tag-collision check. excludeId skips the row
// being edited; pass 0 for a create.
func (s *ClientService) ensureNameAvailable(tx *gorm.DB, name string, excludeId uint) error {
	q := tx.Model(model.Client{}).Where("name = ?", name)
	if excludeId != 0 {
		q = q.Where("id <> ?", excludeId)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewErrorf("client name %q already exists", name)
	}
	return nil
}

// preserveServerManagedFields restores columns the server owns from the stored
// row. createdAt/onlineAt always; the traffic counters only when the request did
// not carry them, which is what separates the two kinds of caller: a master's
// node push omits them (they would unmarshal as zero and wipe the node's
// totals), while the SPA sends them — and its per-client "Reset" button works by
// sending zeroed ones, so overwriting those would silently undo the reset.
// payloadHas nil means "carried nothing", i.e. preserve every counter.
func (s *ClientService) preserveServerManagedFields(tx *gorm.DB, client *model.Client, payloadHas map[string]bool) error {
	var existing model.Client
	err := tx.Model(model.Client{}).Select("created_at", "online_at", "up", "down", "total_up", "total_down").
		Where("id = ?", client.Id).First(&existing).Error
	if err != nil {
		// ErrRecordNotFound included, deliberately: failing open here would write
		// the payload's zeroed counters and destroy the node's traffic history,
		// and an edit whose row vanished has nothing to preserve onto anyway.
		// Both callers load the row via findInboundsChanges first, so in practice
		// this only fires on a concurrent delete.
		return err
	}
	client.CreatedAt = existing.CreatedAt
	client.OnlineAt = existing.OnlineAt
	if !payloadHas["up"] {
		client.Up = existing.Up
	}
	if !payloadHas["down"] {
		client.Down = existing.Down
	}
	if !payloadHas["totalUp"] {
		client.TotalUp = existing.TotalUp
	}
	if !payloadHas["totalDown"] {
		client.TotalDown = existing.TotalDown
	}
	return nil
}

// payloadFields reports which keys the request object actually carried, so an
// omitted server-managed value is preserved rather than written as zero.
func payloadFields(data json.RawMessage) map[string]bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	fields := make(map[string]bool, len(raw))
	for k := range raw {
		fields[k] = true
	}
	return fields
}

func (s *ClientService) updateLinksWithFixedInbounds(tx *gorm.DB, clients []*model.Client, hostname string) error {
	clientInboundIds := make([][]uint, len(clients))
	var allIds []uint
	for i, client := range clients {
		var ids []uint
		if err := json.Unmarshal(client.Inbounds, &ids); err != nil {
			return err
		}
		clientInboundIds[i] = ids
		allIds = common.UnionUintArray(allIds, ids)
	}

	// Zero inbounds means removing local links only.
	// node_id IS NULL: local links for node replicas would carry THIS panel's
	// hostname — their real links come back from the node via reconciliation
	// as type "external" (which the non-local pass below preserves).
	var inbounds []model.Inbound
	if len(allIds) > 0 {
		err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ? and type in ? and node_id IS NULL", allIds, util.InboundTypeWithLink).Find(&inbounds).Error
		if err != nil {
			return err
		}
	}
	inboundById := make(map[uint]*model.Inbound, len(inbounds))
	for i := range inbounds {
		inboundById[inbounds[i].Id] = &inbounds[i]
	}

	// Node links are system-owned (refreshNodeLinks rewrites them), so for an
	// existing client they come from the stored row, not the request payload: a
	// tab loaded before the last reconcile would otherwise strip them all, and
	// nothing repairs that until the next reconcile touches this node.
	var nodeNames []string
	if err := tx.Model(model.Node{}).Pluck("name", &nodeNames).Error; err != nil {
		return err
	}

	// Replica inbounds the payload still references, by tag. A node link is
	// re-attached only while its inbound is still bound to the client:
	// otherwise revoking access would leave a working link in the subscription
	// until refreshNodeLinks next runs, which needs that node to be online — so
	// revoking a user from an offline node would not take effect at all.
	replicaTagById := map[uint]string{}
	if len(allIds) > 0 {
		var replicas []model.Inbound
		if err := tx.Model(model.Inbound{}).Select("id", "tag").
			Where("id in ? and node_id IS NOT NULL", allIds).Find(&replicas).Error; err != nil {
			return err
		}
		for _, r := range replicas {
			replicaTagById[r.Id] = r.Tag
		}
	}

	// Stored links for the existing clients, in one query rather than one per
	// client — editbulk submits the whole selection through here. An id missing
	// from the map is a row that vanished concurrently: nothing to re-attach.
	storedLinksById := map[uint]json.RawMessage{}
	if len(nodeNames) > 0 {
		var ids []uint
		for _, client := range clients {
			if client.Id != 0 {
				ids = append(ids, client.Id)
			}
		}
		if len(ids) > 0 {
			var rows []struct {
				Id    uint
				Links json.RawMessage
			}
			if err := tx.Model(model.Client{}).Select("id", "links").
				Where("id in ?", ids).Scan(&rows).Error; err != nil {
				// Failing open would persist the list with every node link
				// stripped — the exact loss this re-attach exists to prevent.
				return err
			}
			for _, r := range rows {
				storedLinksById[r.Id] = r.Links
			}
		}
	}

	for index, client := range clients {
		var clientLinks []map[string]string
		if err := json.Unmarshal(client.Links, &clientLinks); err != nil {
			return err
		}

		newClientLinks := []map[string]string{}
		for _, id := range clientInboundIds[index] {
			inbound, ok := inboundById[id]
			if !ok {
				continue
			}
			newLinks := util.LinkGenerator(client.Config, inbound, hostname, client.Remark)
			for _, newLink := range newLinks {
				newClientLinks = append(newClientLinks, map[string]string{
					"remark": inbound.Tag,
					"type":   "local",
					"uri":    newLink,
				})
			}
		}

		// A client being created has no stored row to re-attach from, so its
		// payload links are kept verbatim — filtering them would drop a
		// user-authored link whose remark merely looks node-owned.
		reattach := client.Id != 0 && len(nodeNames) > 0

		// Add non local links (node-owned ones re-attach from the DB below)
		for _, clientLink := range clientLinks {
			if clientLink["type"] == "local" {
				continue
			}
			if reattach && isNodeOwnedRemark(clientLink["remark"], nodeNames) {
				continue
			}
			newClientLinks = append(newClientLinks, clientLink)
		}
		if stored := storedLinksById[client.Id]; reattach && len(stored) > 0 {
			var storedLinks []map[string]string
			if err := json.Unmarshal(stored, &storedLinks); err != nil {
				return err
			}
			keptTags := map[string]bool{}
			for _, id := range clientInboundIds[index] {
				if tag, ok := replicaTagById[id]; ok {
					keptTags[tag] = true
				}
			}
			for _, l := range storedLinks {
				// type must be checked too: a local inbound tagged like a
				// node prefix is regenerated above, and re-appending the
				// stored copy would duplicate it once per save, forever.
				if l["type"] == "local" || !isNodeOwnedRemark(l["remark"], nodeNames) {
					continue
				}
				sep := strings.Index(l["remark"], "] ")
				if sep < 0 || !keptTags[l["remark"][sep+2:]] {
					continue // inbound no longer bound to this client
				}
				newClientLinks = append(newClientLinks, l)
			}
		}

		links, err := json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		clients[index].Links = links
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundAdd(tx *gorm.DB, initIds string, inboundId uint, hostname string) error {
	clientIds := strings.Split(initIds, ",")
	var clients []model.Client
	err := tx.Model(model.Client{}).Where("id in ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	var inbound model.Inbound
	err = tx.Model(model.Inbound{}).Preload("Tls").Where("id = ?", inboundId).Find(&inbound).Error
	if err != nil {
		return err
	}
	for _, client := range clients {
		// Add inbounds
		var clientInbounds []uint
		json.Unmarshal(client.Inbounds, &clientInbounds)
		clientInbounds = append(clientInbounds, inboundId)
		client.Inbounds, err = json.MarshalIndent(clientInbounds, "", "  ")
		if err != nil {
			return err
		}
		// Add links
		var clientLinks, newClientLinks []map[string]string
		json.Unmarshal(client.Links, &clientLinks)
		newLinks := util.LinkGenerator(client.Config, &inbound, hostname, client.Remark)
		for _, newLink := range newLinks {
			newClientLinks = append(newClientLinks, map[string]string{
				"remark": inbound.Tag,
				"type":   "local",
				"uri":    newLink,
			})
		}
		for _, clientLink := range clientLinks {
			if clientLink["remark"] != inbound.Tag {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}

		client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	var clientIds []uint
	err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", id).Scan(&clientIds).Error
	if err != nil {
		return err
	}
	if len(clientIds) == 0 {
		return nil
	}
	var clients []model.Client
	err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	// Needed to recognise this inbound's node-owned links below; hoisted out of
	// the loop since it does not vary per client.
	var nodeNames []string
	if err = tx.Model(model.Node{}).Pluck("name", &nodeNames).Error; err != nil {
		return err
	}
	for _, client := range clients {
		// Delete inbounds
		var clientInbounds, newClientInbounds []uint
		json.Unmarshal(client.Inbounds, &clientInbounds)
		for _, clientInbound := range clientInbounds {
			if clientInbound != id {
				newClientInbounds = append(newClientInbounds, clientInbound)
			}
		}
		client.Inbounds, err = json.MarshalIndent(newClientInbounds, "", "  ")
		if err != nil {
			return err
		}
		// Delete links. A node link for the same inbound carries the tag behind
		// a "[<node>] " prefix, so an exact match alone would leave it behind
		// for good: nothing else strips links for an inbound that is gone.
		var clientLinks, newClientLinks []map[string]string
		json.Unmarshal(client.Links, &clientLinks)
		for _, clientLink := range clientLinks {
			remark := clientLink["remark"]
			if remark == tag || isNodeLinkFor(remark, tag, nodeNames) {
				continue
			}
			newClientLinks = append(newClientLinks, clientLink)
		}
		client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	var err error
	for _, inbound := range *inbounds {
		var clientIds []uint
		err = tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", inbound.Id).Scan(&clientIds).Error
		if err != nil {
			return err
		}
		if len(clientIds) == 0 {
			continue
		}
		var clients []model.Client
		err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
		if err != nil {
			return err
		}
		for _, client := range clients {
			var clientLinks, newClientLinks []map[string]string
			json.Unmarshal(client.Links, &clientLinks)
			newLinks := util.LinkGenerator(client.Config, &inbound, hostname, client.Remark)
			for _, newLink := range newLinks {
				newClientLinks = append(newClientLinks, map[string]string{
					"remark": inbound.Tag,
					"type":   "local",
					"uri":    newLink,
				})
			}
			for _, clientLink := range clientLinks {
				if clientLink["type"] != "local" || (clientLink["remark"] != inbound.Tag && clientLink["remark"] != oldTag) {
					newClientLinks = append(newClientLinks, clientLink)
				}
			}

			client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
			if err != nil {
				return err
			}
			err = tx.Save(&client).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// DepleteClients disables clients over quota or past expiry and returns both
// the affected local inbound ids (to hot-restart) and the names whose enable
// state changed, so the caller can fan those out to nodes. That second list
// covers BOTH directions: the depletion disable below and the periodic reset's
// re-enable inside ResetClients — a round that only re-enables still has to
// reach the nodes, or they keep rejecting a paid-up user. With cluster totals
// folded into up/down, the quota check is already a whole-cluster judgement.
func (s *ClientService) DepleteClients() ([]uint, []string, error) {
	var err error
	var clients []model.Client
	var changes []model.Changes
	var enableChanged []string
	var inboundIds []uint

	dt := time.Now().Unix()
	db := database.GetDB()

	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			// Only now is the write visible on the hub's own connection.
			NotifyConfigChanged()
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				logger.Error("Error checkpointing WAL: ", err1.Error())
			}
		} else {
			tx.Rollback()
		}
	}()

	// Reset clients
	inboundIds, enableChanged, err = s.ResetClients(tx, dt)
	if err != nil {
		return nil, nil, err
	}

	// Deplete clients
	err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Scan(&clients).Error
	if err != nil {
		return nil, nil, err
	}

	for _, client := range clients {
		logger.Debug("Client ", client.Name, " is going to be disabled")
		enableChanged = append(enableChanged, client.Name)
		var userInbounds []uint
		json.Unmarshal(client.Inbounds, &userInbounds)
		// Find changed inbounds
		inboundIds = common.UnionUintArray(inboundIds, userInbounds)
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "DepleteJob",
			Key:      "clients",
			Action:   "disable",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Update("enable", false).Error
		if err != nil {
			return nil, nil, err
		}
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, nil, err
		}
		MarkLastUpdate(dt)
	}

	return inboundIds, enableChanged, nil
}

// ResetClients applies the per-client periodic reset. It returns the affected
// local inbound ids (to hot-restart) and the names it re-enabled, which the
// caller has to fan out to nodes for the same reason DepleteJob fans out a
// disable: the node keeps rejecting a paid-up user until it hears otherwise.
func (s *ClientService) ResetClients(tx *gorm.DB, dt int64) ([]uint, []string, error) {
	var err error
	var resetClients, allClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	var reenabled []string
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, nil, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set delay start with periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = true AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set periodic reset
	err = tx.Model(model.Client{}).
		Where("delay_start = false AND auto_reset = true AND next_reset < ?", dt).Find(&resetClients).Error
	if err != nil {
		return nil, nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.TotalUp += client.Up
		client.TotalDown += client.Down
		client.Up = 0
		client.Down = 0
		if !client.Enable {
			client.Enable = true
			reenabled = append(reenabled, client.Name)
			var clientInboundIds []uint
			json.Unmarshal(client.Inbounds, &clientInboundIds)
			inboundIds = common.UnionUintArray(inboundIds, clientInboundIds)
		}
	}
	allClients = append(allClients, resetClients...)

	// Save clients
	if len(allClients) > 0 {
		err = tx.Save(allClients).Error
		if err != nil {
			return nil, nil, err
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, nil, err
		}
		MarkLastUpdate(dt)
	}
	return inboundIds, reenabled, nil
}

// ResetAllClientsTraffic zeroes up/down for every client (accumulating into the
// total counters) and re-enables all of them, in a single bulk update. Used by
// the global periodic traffic reset; the caller restarts the core afterwards so
// re-enabled clients take effect.
func (s *ClientService) ResetAllClientsTraffic() error {
	db := database.GetDB()
	dt := time.Now().Unix()

	result := db.Model(model.Client{}).
		Where("(up + down) > 0 OR enable = false").
		UpdateColumns(map[string]interface{}{
			"total_up":   gorm.Expr("total_up + up"),
			"total_down": gorm.Expr("total_down + down"),
			"up":         0,
			"down":       0,
			"enable":     true,
		})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		if err := db.Create(&model.Changes{
			DateTime: dt,
			Actor:    "ResetTrafficJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"all\""),
		}).Error; err != nil {
			return err
		}
		SetLastUpdate(dt)
	}

	return nil
}

func setConfigIdentity(client *model.Client) error {
	if client.Name == "" || len(client.Config) < 2 {
		return nil
	}
	var configs map[string]map[string]interface{}
	if err := json.Unmarshal(client.Config, &configs); err != nil {
		return err
	}
	for _, cfg := range configs {
		if _, ok := cfg["name"]; ok {
			cfg["name"] = client.Name
		} else if _, ok := cfg["username"]; ok {
			cfg["username"] = client.Name
		}
	}
	newConfig, err := json.Marshal(configs)
	if err != nil {
		return err
	}
	client.Config = newConfig
	return nil
}

func (s *ClientService) findInboundsChanges(tx *gorm.DB, client *model.Client, fillOmitted bool) ([]uint, error) {
	var err error
	var oldClient model.Client
	var oldInboundIds, newInboundIds []uint
	err = tx.Model(model.Client{}).Where("id = ?", client.Id).First(&oldClient).Error
	if err != nil {
		return nil, err
	}
	if fillOmitted {
		// The bulk path submits the client list projection, which cannot carry
		// these columns; the payload's zero values would silently wipe them —
		// switching off periodic auto-reset and zeroing next_reset, so
		// ResetClientsTraffic's "next_reset < now" scan never matches again.
		client.Links = oldClient.Links
		client.Config = oldClient.Config
		client.AutoReset = oldClient.AutoReset
		client.ResetDays = oldClient.ResetDays
		client.NextReset = oldClient.NextReset
		client.DelayStart = oldClient.DelayStart
	}
	err = json.Unmarshal(oldClient.Inbounds, &oldInboundIds)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(client.Inbounds, &newInboundIds)
	if err != nil {
		return nil, err
	}

	// Check client.Config changes
	if !bytes.Equal(oldClient.Config, client.Config) ||
		oldClient.Name != client.Name ||
		oldClient.Enable != client.Enable {
		return common.UnionUintArray(oldInboundIds, newInboundIds), nil
	}

	// Check client.Inbounds changes
	diffInbounds := common.DiffUintArray(oldInboundIds, newInboundIds)

	return diffInbounds, nil
}
