package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"
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

// ClientWrite reports what a client save actually did: the rows it wrote (or
// deleted) and the inbounds whose live user table has to be refreshed. Both are
// []uint and mean different things, so they travel named rather than as two
// bare return values a call site could quietly swap.
type ClientWrite struct {
	Ids        []uint
	InboundIds []uint
}

func (s *ClientService) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) (*ClientWrite, error) {
	var err error
	var inboundIds []uint
	var savedIds []uint

	switch act {
	case "new", "edit":
		var client model.Client
		err = json.Unmarshal(data, &client)
		if err != nil {
			return nil, err
		}
		// Before the rename check below, and not exempted for a cluster push:
		// a nameless client breaks the inbound's user table wherever it came
		// from, and the master never pushes one (the name is the map key).
		if err = normalizeClientName(&client); err != nil {
			return nil, err
		}
		if err = validateResetSchedule(&client); err != nil {
			return nil, err
		}
		if act == "new" {
			defaultClientJSONFields(&client)
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
		if err = s.alignNextReset(tx, &client, act == "new", time.Now().Unix(), panelLocation()); err != nil {
			return nil, err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return nil, err
		}
		// After the write: on a create the id is assigned by the insert.
		savedIds = []uint{client.Id}
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
			if err = normalizeClientName(client); err != nil {
				return nil, err
			}
			if err = validateResetSchedule(client); err != nil {
				return nil, err
			}
			defaultClientJSONFields(client)
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
		loc := panelLocation()
		for _, client := range clients {
			if err = setConfigIdentity(client); err != nil {
				return nil, err
			}
			client.CreatedAt = now
			// isNew, so this reads no rows -- safe to call per client.
			if err = s.alignNextReset(tx, client, true, now, loc); err != nil {
				return nil, err
			}
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
		savedIds = clientIds(clients)
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
			// Trim first: an untrimmed copy of the stored name would otherwise
			// read as a rename and be checked against the table for nothing.
			if err = normalizeClientName(client); err != nil {
				return nil, err
			}
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
		savedIds = clientIds(clients)
	case "resetpolicy":
		// The reset schedule for a selection of clients, in one statement.
		//
		// Deliberately not folded into editbulk: that path submits the client
		// list projection, which cannot carry these columns, so
		// findInboundsChanges restores all of them from the stored row -- the
		// fields this action exists to change are exactly the ones editbulk
		// cannot. It also writes one row at a time and regenerates links, which
		// a schedule change has no reason to touch.
		var policy struct {
			Ids             []uint `json:"ids"`
			AutoReset       bool   `json:"autoReset"`
			ResetDays       int    `json:"resetDays"`
			ResetDayOfMonth int    `json:"resetDayOfMonth"`
		}
		if err = json.Unmarshal(data, &policy); err != nil {
			return nil, err
		}
		if len(policy.Ids) == 0 {
			err = common.NewError("no clients selected")
			return nil, err
		}
		if policy.ResetDayOfMonth < 0 || policy.ResetDayOfMonth > 31 {
			err = common.NewErrorf("reset day of month out of range: %d", policy.ResetDayOfMonth)
			return nil, err
		}
		if policy.ResetDays < 0 {
			err = common.NewErrorf("reset days cannot be negative: %d", policy.ResetDays)
			return nil, err
		}
		// Refused rather than stored, unlike a single client save.
		//
		// ResetClients deliberately leaves a periodless row alone so a
		// misconfiguration stays visible instead of being quietly repaired, and
		// the per-client save keeps writing whatever it is handed for the same
		// reason. This action has no such history to honour: its only purpose is
		// to set a schedule, and storing one that ResetClients will skip means
		// answering a bulk request with a success toast and changing nothing.
		if policy.AutoReset && policy.ResetDays == 0 && policy.ResetDayOfMonth == 0 {
			err = common.NewError("auto reset needs a period: set reset days or a day of the month")
			return nil, err
		}
		// One boundary for the whole selection: same schedule, same instant.
		now := time.Now().Unix()
		next := int64(0)
		if policy.AutoReset && (policy.ResetDays > 0 || policy.ResetDayOfMonth > 0) {
			next = nextResetAt(&model.Client{
				ResetDays:       policy.ResetDays,
				ResetDayOfMonth: policy.ResetDayOfMonth,
			}, now, panelLocation())
		}
		cols := map[string]interface{}{
			"auto_reset":         policy.AutoReset,
			"reset_day_of_month": policy.ResetDayOfMonth,
			// A delayed start has no boundary until its first bytes arrive,
			// and ResetClients only scans rows with delay_start = false --
			// so writing one here would put a date in the drawer that
			// nothing ever acts on.
			"next_reset": gorm.Expr("CASE WHEN delay_start THEN 0 ELSE ? END", next),
		}
		// reset_days is only this action's to set while auto reset is on. On a
		// delay-start row the same column is the plan length -- ResetClients'
		// first branch reads it to compute Expiry -- and that branch needs it
		// above zero. Writing the incoming 0 over it (which is what switching
		// auto reset off sends) would leave every delayed client in the
		// selection matching no branch at all: delay_start never cleared,
		// Expiry never set, so the client never expires. reset_day_of_month
		// above is safe to clear either way, since that branch does not read it.
		if policy.AutoReset {
			cols["reset_days"] = policy.ResetDays
		}
		res := tx.Model(model.Client{}).Where("id IN ?", policy.Ids).UpdateColumns(cols)
		if res.Error != nil {
			err = res.Error
			return nil, err
		}
		// Zero rows means every id was wrong -- answering "saved" to that is how
		// an integrator keeps a broken call. Not compared against len(Ids): a
		// repeated id would match once and make an exact check fail on a request
		// that did exactly what was asked.
		if res.RowsAffected == 0 {
			err = common.NewError("no matching clients")
			return nil, err
		}
		// No InboundIds: a schedule change moves no user in or out of an
		// inbound's table, so there is nothing for the core to reload.
		savedIds = policy.Ids
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
		savedIds = ids
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
		savedIds = []uint{id}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}

	return &ClientWrite{Ids: savedIds, InboundIds: inboundIds}, nil
}

func clientIds(clients []*model.Client) []uint {
	ids := make([]uint, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.Id)
	}
	return ids
}

// normalizeClientName trims the name in place and rejects an empty one.
//
// The name stopped being merely a label when core/protocol switched its user
// tables from list positions to names (upstream #1231): it is now the key the
// running inbound identifies a session by. An empty one makes every nameless
// client on an inbound the same user -- trojan's UpdateUsers rejects the
// repeated identity outright and fails the whole update, vmess silently keeps
// only the last, vless mis-attributes flow. Untrimmed is the same problem one
// step removed: "alice" and "alice " are two keys here but one name to the
// operator reading the traffic table.
//
// Trimming also has to happen before ensureNameAvailable, or a padded copy of
// an existing name walks straight past the duplicate check.
func normalizeClientName(client *model.Client) error {
	client.Name = strings.TrimSpace(client.Name)
	if client.Name == "" {
		return common.NewError("client name must not be empty")
	}
	return nil
}

// defaultClientJSONFields fills the json.RawMessage columns a create may omit.
//
// Both are unmarshalled unconditionally on the create path -- Links by
// updateLinksWithFixedInbounds, Inbounds right after it -- and json.Unmarshal on
// a nil RawMessage fails with "unexpected end of JSON input", an error naming
// neither the field nor the request. The panel never hits it because its own
// forms always send both; an API caller creating a client has nothing to put in
// links and no reason to guess it is mandatory.
//
// Create only, deliberately. On an edit these carry state nothing else can
// recover: an absent inbounds would read as "remove from every inbound", and an
// absent links would drop the external entries the payload is the only source of
// (node-owned ones re-attach from the stored row, user-authored ones do not). An
// explicit empty array is a different thing and still means what it says -- it
// is two bytes, so it never reaches this.
func defaultClientJSONFields(client *model.Client) {
	if len(client.Links) == 0 {
		client.Links = json.RawMessage("[]")
	}
	if len(client.Inbounds) == 0 {
		client.Inbounds = json.RawMessage("[]")
	}
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
	err := tx.Model(model.Client{}).Select("created_at", "online_at", "up", "down", "total_up", "total_down", "tg_id").
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
	// The panel's client form has no field for the Telegram binding -- it is
	// set through the bot -- so without this every save from the panel would
	// silently unbind whoever was watching their own usage.
	if !payloadHas["tgId"] {
		client.TgId = existing.TgId
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

	// Read before the transaction opens: a monthly reset boundary is a local
	// midnight, so ResetClients needs the panel's timezone.
	loc := panelLocation()

	// This job runs every minute whether or not there is anything to deplete or
	// reset, so notifying unconditionally is not free: the hub answers with a
	// full config push and the SPA swaps its whole config object for the new
	// one, throwing away whatever the operator had typed into the rules/DNS/
	// settings pages but not saved yet. Both write paths below report exactly
	// when they wrote something -- follow that condition instead of announcing
	// an idle round.
	// Collected inside the transaction, published after it commits: an alert
	// about a client that a rollback left enabled would simply be false.
	var depleted []model.Client
	var expiring []expiringClient

	// Set by whichever branch below actually wrote. Both used to call
	// MarkLastUpdate from inside the transaction, which is the one thing this
	// must not do: the mark is what invalidates the config cache, and a reader
	// racing the commit sees the pre-commit snapshot rather than blocking (WAL),
	// so it would cache the pre-change rows under the post-change key and serve
	// them for the whole TTL -- outliving the push that follows, which lands
	// on that same entry and is dropped by the SPA as not newer.
	var marked bool
	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			// Only now is the write visible on the hub's own connection.
			if marked {
				SetLastUpdate(dt)
			}
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				logger.Error("Error checkpointing WAL: ", err1.Error())
			}
			publishClientEvents(depleted, expiring)
		} else {
			tx.Rollback()
		}
	}()

	// Reset clients
	inboundIds, enableChanged, marked, err = s.ResetClients(tx, dt, loc)
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
		depleted = append(depleted, client)
		var userInbounds []uint
		json.Unmarshal(client.Inbounds, &userInbounds)
		// Find changed inbounds
		inboundIds = common.UnionUintArray(inboundIds, userInbounds)
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "DepleteJob",
			Key:      "clients",
			Action:   "disable",
			Obj:      changeObjName(client.Name),
		})
	}

	// Warning about a client that has already run out is an obituary -- by then
	// it is disconnected and the operator hears about it from the customer.
	// This is the half that is actually actionable, and it reads the same rows
	// one step earlier.
	expiring, err = s.findExpiringClients(tx, dt)
	if err != nil {
		return nil, nil, err
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
		marked = true
	}

	return inboundIds, enableChanged, nil
}

// expiringClient is one client close to a limit, carrying whichever margin
// tripped. Only the relevant one is set.
type expiringClient struct {
	Name      string
	DaysLeft  int
	BytesLeft int64
	// Telegram chat to warn directly, or 0 for a client with no binding.
	TgId int64
}

// findExpiringClients selects the clients approaching either limit, using the
// thresholds from the notification settings. Both at zero disables the warning
// entirely and skips the query.
func (s *ClientService) findExpiringClients(tx *gorm.DB, dt int64) ([]expiringClient, error) {
	var settingService SettingService
	th := settingService.GetNotifyThresholds()

	// Computed once and used twice -- in the query that selects the rows and in
	// the loop below that decides which margin to report -- so the two cannot
	// drift into disagreeing about which threshold actually tripped.
	expireBefore := dt + int64(th.ExpireDays)*86400

	var conds []string
	// Anything already over a limit belongs to the depletion pass above;
	// without these two guards a client that expired days ago but still has
	// traffic left would be reported as "expiring soon" every day forever.
	args := []any{dt}
	if th.ExpireDays > 0 {
		conds = append(conds, "(expiry > 0 AND expiry < ?)")
		args = append(args, expireBefore)
	}
	if th.VolumeBytes > 0 {
		conds = append(conds, "(volume > 0 AND up+down > volume - ?)")
		args = append(args, th.VolumeBytes)
	}
	if len(conds) == 0 {
		return nil, nil
	}

	where := "enable = true AND (expiry = 0 OR expiry > ?) AND (volume = 0 OR up+down < volume) AND (" +
		strings.Join(conds, " OR ") + ")"

	var rows []model.Client
	if err := tx.Model(model.Client{}).Where(where, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]expiringClient, 0, len(rows))
	for _, c := range rows {
		e := expiringClient{Name: c.Name, TgId: c.TgId}
		left := c.Volume - c.Up - c.Down
		// Only the margin that actually selected the row is filled in, because
		// the renderer prefers DaysLeft when both are set. A client picked up
		// by the volume threshold can still have months left on its expiry, and
		// filling that in regardless reported "expires in 30 days" to a client
		// whose real problem was 4 GiB of traffic left -- naming the wrong
		// reason, on the customer's own Telegram rather than only the
		// operator's.
		if th.ExpireDays > 0 && c.Expiry > dt && c.Expiry < expireBefore {
			// Rounded up, so a client with six hours left reads "1 day" rather
			// than "0 days", which would look like a bug.
			e.DaysLeft = int((c.Expiry - dt + 86399) / 86400)
		}
		// left > 0 and left < VolumeBytes are the Go spellings of the two SQL
		// conditions above: "not over the limit yet" and "inside the warning
		// margin".
		if th.VolumeBytes > 0 && c.Volume > 0 && left > 0 && left < th.VolumeBytes {
			e.BytesLeft = left
		}
		out = append(out, e)
	}
	return out, nil
}

// publishClientEvents reports one depletion pass.
//
// Every event carries the Telegram bindings of the clients it is about, so the
// notifier can warn each of them as well as the operator. Whole rows come in
// rather than names because that binding is the one field a name cannot carry.
func publishClientEvents(depleted []model.Client, expiring []expiringClient) {
	if len(depleted) == 0 && len(expiring) == 0 {
		return
	}
	// Asked once for the pass rather than left to Publish, which reads the
	// settings per event. The expiry half is one event per client sitting near
	// a limit, every minute -- on a panel with these alerts off that was a
	// settings scan per client to decide nothing.
	var settingService SettingService
	if !settingService.NotifyWants(notify.ClientDepleted, notify.ClientExpiring) {
		return
	}

	if len(depleted) > 0 {
		// One event for the whole pass: disabling fifty clients at once is one
		// thing that happened, not fifty. A disabled client is not selected by
		// the next pass, so there is nothing to repeat.
		//
		// The clients themselves are still told individually -- "you have run
		// out" is not a batch statement -- which is what Targets carries.
		names := make([]string, 0, len(depleted))
		var targets []notify.ClientTarget
		for _, c := range depleted {
			names = append(names, c.Name)
			if c.TgId != 0 {
				targets = append(targets, notify.ClientTarget{Name: c.Name, TgId: c.TgId})
			}
		}
		notify.Publish(notify.Event{
			Kind:    notify.ClientDepleted,
			Subject: "batch",
			Data:    &notify.ClientData{Names: names, Targets: targets},
		})
	}
	// Warnings stay per client, because the useful part of the message is how
	// much a *particular* client has left, which a combined list cannot say.
	// The suppressor keys its 24h cooldown on the name, so a client sitting
	// near its limit is reported once a day rather than once a minute -- and
	// the client's own copy inherits that same cooldown, since both are the
	// same event.
	for _, c := range expiring {
		data := &notify.ClientData{DaysLeft: c.DaysLeft, BytesLeft: c.BytesLeft}
		if c.TgId != 0 {
			data.Targets = []notify.ClientTarget{{
				Name: c.Name, TgId: c.TgId, DaysLeft: c.DaysLeft, BytesLeft: c.BytesLeft,
			}}
		}
		notify.Publish(notify.Event{
			Kind:    notify.ClientExpiring,
			Subject: c.Name,
			Data:    data,
		})
	}
}

// changeObjName encodes a client name for the changes log.
//
// Built by concatenation, a name holding a quote or a backslash produced
// invalid JSON in the Obj column -- and cmd/migration/1_1.go already exists to
// repair the previous generation of exactly this bug.
func changeObjName(name string) json.RawMessage {
	encoded, err := json.Marshal(name)
	if err != nil {
		// json.Marshal of a string cannot fail, but never write broken JSON.
		return json.RawMessage(`""`)
	}
	return json.RawMessage(encoded)
}

// daysInMonth returns the length of the given month. Day zero of the next month
// is the last day of this one, and Date normalises a 13th month into January of
// the next year, so December needs no special case.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// nextMonthlyReset returns the first local midnight on day-of-month `dom` that
// falls strictly after `from`, clamped to the last day of months too short to
// hold it.
//
// The clamp is applied fresh each time, against `dom` rather than against the
// previous result: clamping 31 to February's 28th and then anchoring on that
// would make every later boundary the 28th. Recomputed this way, a client on the
// 31st resets on Feb 28 and then again on Mar 31.
func nextMonthlyReset(from time.Time, dom int) time.Time {
	year, month := from.Year(), from.Month()
	// Twelve would do; the extra turn is for a `dom` beyond every month's
	// length, which cannot happen through the API but would loop forever.
	for i := 0; i < 13; i++ {
		day := dom
		if last := daysInMonth(year, month); day > last {
			day = last
		}
		t := time.Date(year, month, day, 0, 0, 0, 0, from.Location())
		if t.After(from) {
			return t
		}
		month++
		if month > time.December {
			month = time.January
			year++
		}
	}
	return from
}

// nextResetAt is when the client's counters are due to be cleared next, in
// whichever of the two modes it is configured for.
func nextResetAt(c *model.Client, dt int64, loc *time.Location) int64 {
	if c.ResetDayOfMonth > 0 {
		return nextMonthlyReset(time.Unix(dt, 0).In(loc), c.ResetDayOfMonth).Unix()
	}
	return dt + (int64(c.ResetDays) * 86400)
}

// validateResetSchedule rejects a day of the month the panel cannot represent.
//
// Out of range is not dangerous in itself -- nextMonthlyReset clamps per month,
// so 200 silently means "month end" and nothing loops -- but the drawer's input
// carries max=31 and would show the stored number unchanged, so the panel and
// the row disagree about what was configured. The bulk action refuses the same
// range; without this the two write paths disagreed about a legal day.
//
// Deliberately narrower than the bulk action's checks: a period of zero with
// auto reset on stays writable here, because ResetClients leaves such a row
// alone on purpose and older rows already carry that combination.
func validateResetSchedule(c *model.Client) error {
	if c.ResetDayOfMonth < 0 || c.ResetDayOfMonth > 31 {
		return common.NewErrorf("reset day of month out of range: %d", c.ResetDayOfMonth)
	}
	return nil
}

// panelLocation is the configured timezone, or the machine's own if the setting
// cannot be read. Only the monthly mode consults it, and a timezone lookup is
// not a reason to fail the write it is part of.
//
// The nil check is not defensive padding: the setting is read through the global
// handle, which unit tests driving this service with their own gorm.DB never
// set, and gorm dereferences a nil *DB rather than returning an error -- so
// without it a save in such a test panics instead of failing.
func panelLocation() *time.Location {
	if database.GetDB() == nil {
		return time.Local
	}
	var settingService SettingService
	loc, err := settingService.GetTimeLocation()
	if err != nil {
		logger.Warning("get time location failed, falling back to local: ", err)
		return time.Local
	}
	return loc
}

// alignNextReset recomputes the next boundary when the schedule that produces
// it has changed, so a new day of the month takes effect now rather than one
// period late.
//
// It deliberately does not fire on a changed ResetDays. The panel shifts
// NextReset by the difference itself, which is how an operator moves a single
// period without restarting it, and recomputing here would overrule that
// silently. A changed day of the month has no such shift to preserve -- the
// calendar it lands on is the whole point -- and switching auto-reset on has
// nothing to preserve at all.
//
// Zeroing rather than leaving the old boundary when auto-reset is off is not
// cosmetic: ResetClients scans for `next_reset < now`, so a stale PAST boundary
// would clear a client's counters on the first tick after it is switched back
// on, months later.
func (s *ClientService) alignNextReset(tx *gorm.DB, client *model.Client, isNew bool, dt int64, loc *time.Location) error {
	if !client.AutoReset || client.DelayStart {
		// A delayed start has no boundary yet by design: ResetClients sets the
		// first one when the client's first bytes arrive.
		client.NextReset = 0
		return nil
	}
	if client.ResetDays <= 0 && client.ResetDayOfMonth <= 0 {
		// Misconfigured either way, and ResetClients skips such a row on
		// purpose. Leave whatever came in rather than inventing a boundary.
		return nil
	}
	if isNew {
		client.NextReset = nextResetAt(client, dt, loc)
		return nil
	}
	var old model.Client
	if err := tx.Model(model.Client{}).Select("auto_reset", "reset_day_of_month").
		Where("id = ?", client.Id).First(&old).Error; err != nil {
		return err
	}
	if !old.AutoReset || old.ResetDayOfMonth != client.ResetDayOfMonth {
		client.NextReset = nextResetAt(client, dt, loc)
	}
	return nil
}

// ResetClients applies the per-client periodic reset. It returns the affected
// local inbound ids (to hot-restart) and the names it re-enabled, which the
// caller has to fan out to nodes for the same reason DepleteJob fans out a
// disable: the node keeps rejecting a paid-up user until it hears otherwise.
//
// loc is the panel's configured timezone, which only the monthly mode reads: a
// day-of-month boundary is a local midnight, while a plain N-day period is
// arithmetic on an absolute timestamp and has no calendar to align to.
func (s *ClientService) ResetClients(tx *gorm.DB, dt int64, loc *time.Location) ([]uint, []string, bool, error) {
	var err error
	var resetClients, allClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	var reenabled []string
	// A period has to be one of the two: reset_days above zero, or a day of the
	// month to land on. With neither, the date each block computes collapses to
	// dt itself -- the first would set Expiry to right now (a client dead the
	// instant it sends a byte) and the third would set NextReset to dt, which
	// matches again on the very next tick, clearing the counters every minute so
	// the volume quota is never reached. A row like that is misconfigured either
	// way; leaving it untouched keeps it visible rather than quietly breaking it.
	// The panel forms cannot produce one (the toggle writes 1 and the input has
	// min=1), but apiv2 and older rows can.
	//
	// The first block is the odd one out: it writes Expiry, which is how long
	// the plan runs from first use, not when the counters clear -- so a day of
	// the month means nothing to it and reset_days stays its only period.
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND reset_days > 0 AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, nil, false, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      changeObjName(client.Name),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set delay start with periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = true AND (reset_days > 0 OR reset_day_of_month > 0) AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, nil, false, err
	}
	for _, client := range resetClients {
		client.NextReset = nextResetAt(client, dt, loc)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      changeObjName(client.Name),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set periodic reset
	err = tx.Model(model.Client{}).
		Where("delay_start = false AND auto_reset = true AND (reset_days > 0 OR reset_day_of_month > 0) AND next_reset < ?", dt).Find(&resetClients).Error
	if err != nil {
		return nil, nil, false, err
	}
	for _, client := range resetClients {
		client.NextReset = nextResetAt(client, dt, loc)
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
			return nil, nil, false, err
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, nil, false, err
		}
	}
	// Reported on any write, not only on the ones that log a Changes row: the
	// periodic-reset branch rewrites counters and can flip Enable back on
	// without recording a change, and DepleteClients gates both its mark and
	// its hub notify on this. Without it a re-enabled client stays greyed out
	// in every open panel while sing-box is already letting it back in.
	//
	// Reported rather than marked here: a mark invalidates the config cache,
	// and the caller's transaction is still open, so marking now would let a
	// reader racing that commit cache the pre-change rows under the
	// post-change key.
	wrote := len(allClients) > 0 || len(changes) > 0
	return inboundIds, reenabled, wrote, nil
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
		client.ResetDayOfMonth = oldClient.ResetDayOfMonth
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
