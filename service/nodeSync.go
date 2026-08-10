package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"

	"gorm.io/gorm"
)

// NodeSyncService owns everything that writes TO a node: adopting its inbounds
// as read-only replicas, and pushing/reconciling the master's clients onto it.
// Reconcile is the single write channel — first push, offline catch-up, drift
// repair and the manual button all funnel through it.
type NodeSyncService struct {
	NodeService
}

const (
	// clusterGroup marks clients this master pushed to a node. Reconciliation
	// is scoped to this group so it never touches a node's own local users and
	// needs no tombstones for deletes.
	clusterGroup = "@cluster"

	nodePushTimeout = 15 * time.Second // node hot-restarts inbounds on save
	reconcileBackoff = 30 * time.Second
)

// per-node single-flight + backoff so overlapping triggers (heartbeat, fanout,
// manual button) don't stampede the same node.
var (
	reconcileMu   sync.Mutex
	reconcileBusy = map[uint]bool{}
	reconcileLast = map[uint]time.Time{}
	// dirtyGen increments on every dirty-mark, BEFORE the DB write. A finished
	// reconcile clears the dirty flag only if the generation it claimed is
	// still current, so an edit landing mid-run keeps its flag and the next
	// heartbeat reruns the (by then cheap) diff instead of losing the edit.
	dirtyGen uint64
)

// nodeLinkPrefix is the single definition of the remark convention marking a
// client link as owned by a node: refreshNodeLinks stamps it, client saves use
// it to tell system links from user links, node renames rewrite it and inbound
// deletes strip it. Keep every one of those going through here.
func nodeLinkPrefix(nodeName string) string { return "[" + nodeName + "] " }

// isNodeOwnedRemark reports whether a remark was emitted for any known node.
func isNodeOwnedRemark(remark string, nodeNames []string) bool {
	for _, n := range nodeNames {
		if strings.HasPrefix(remark, nodeLinkPrefix(n)) {
			return true
		}
	}
	return false
}

// isNodeLinkFor reports whether a remark is the node-owned link for this exact
// tag. Matching the whole "[<node>] <tag>" rather than prefix-plus-suffix keeps
// a user-authored external link that merely looks the part — remark "[backup]
// vless-in" on an unrelated entry — out of the blast radius when that tag's
// inbound is deleted.
func isNodeLinkFor(remark, tag string, nodeNames []string) bool {
	for _, n := range nodeNames {
		if remark == nodeLinkPrefix(n)+tag {
			return true
		}
	}
	return false
}

// refreshLinksMu serializes the read-modify-write of client.Links across nodes.
// ReconcileDirtyOnline fans out one goroutine per dirty node and any client/inbound
// save marks every node dirty, so two nodes' refreshNodeLinks can hit the SAME
// client concurrently — each folding in its own "[node] " links. Without this the
// later writer clobbers the earlier node's links (last-write-wins). It guards only
// the local link merge, never the per-node HTTP push.
var refreshLinksMu sync.Mutex

// ---------- remote calls ----------

// nodePost sends an x-www-form-urlencoded apiv2 request (apiv2 save reads
// c.Request.FormValue, not a JSON body) and unwraps the {success,msg,obj} envelope.
func (s *NodeSyncService) nodePost(n *model.Node, client *http.Client, action string, form url.Values) (json.RawMessage, error) {
	u := nodeApiURL(n, action)
	req, err := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Token", n.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpStatusError(resp.StatusCode)
	}
	var msg struct {
		Success bool            `json:"success"`
		Msg     string          `json:"msg"`
		Obj     json.RawMessage `json:"obj"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, nodeMaxResponseSize)).Decode(&msg); err != nil {
		return nil, common.NewError("unexpected response from node")
	}
	if !msg.Success {
		if msg.Msg == "" {
			msg.Msg = "node rejected the request"
		}
		return nil, common.NewError(msg.Msg)
	}
	return msg.Obj, nil
}

// pushClient runs one clients save (new|edit|del) against a node.
// The form carries no "sync" field, so the receiving panel does not immediately
// fan the change out to its own nodes — there is nothing to fan out (see
// ApiService.Save for why a pushed client can never enter an outgoing set), and
// paying for a reconcile sweep per pushed client would be pure waste.
func (s *NodeSyncService) pushClient(n *model.Node, client *http.Client, act string, payload interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("object", "clients")
	form.Set("action", act)
	form.Set("data", string(data))
	return s.nodePost(n, client, "save", form)
}

// ---------- inbound adoption ----------

type remoteInbound struct {
	Id      uint   `json:"id"`
	Type    string `json:"type"`
	Tag     string `json:"tag"`
	Adopted bool   `json:"adopted"`
}

// nodeClient builds a short-lived HTTP client honouring the node's TLS mode,
// with the longer push timeout.
func nodePushClient(n *model.Node) *http.Client {
	c := buildNodeHTTPClient(n)
	c.Timeout = nodePushTimeout
	return c
}

// FetchNodeInbounds lists a node's inbounds, flagging which tags this panel has
// already adopted as replicas.
func (s *NodeSyncService) FetchNodeInbounds(nodeId uint) ([]remoteInbound, error) {
	node, err := s.getNodeById(nodeId)
	if err != nil {
		return nil, err
	}
	obj, err := s.nodeGet(node, nodePushClient(node), "inbounds", nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Inbounds []struct {
			Id   uint   `json:"id"`
			Type string `json:"type"`
			Tag  string `json:"tag"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(obj, &payload); err != nil {
		return nil, common.NewError("unexpected inbounds payload from node")
	}
	adopted, err := s.adoptedTags(nodeId)
	if err != nil {
		return nil, err
	}
	out := make([]remoteInbound, 0, len(payload.Inbounds))
	for _, ib := range payload.Inbounds {
		out = append(out, remoteInbound{
			Id: ib.Id, Type: ib.Type, Tag: ib.Tag,
			Adopted: adopted[ib.Tag],
		})
	}
	return out, nil
}

func (s *NodeSyncService) adoptedTags(nodeId uint) (map[string]bool, error) {
	var tags []string
	err := database.GetDB().Model(model.Inbound{}).Where("node_id = ?", nodeId).Pluck("tag", &tags).Error
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(tags))
	for _, t := range tags {
		m[t] = true
	}
	return m, nil
}

// AdoptInbounds pulls the full panel-shape inbound for each selected tag and
// stores it as a local replica row (node_id set). tag collisions fail loudly —
// the tag is the reconciliation key, so we never silently rename.
func (s *NodeSyncService) AdoptInbounds(nodeId uint, tags []string, actor string) error {
	if len(tags) == 0 {
		return nil
	}
	node, err := s.getNodeById(nodeId)
	if err != nil {
		return err
	}
	client := nodePushClient(node)

	wanted := map[string]bool{}
	for _, t := range tags {
		wanted[t] = true
	}

	// The inbounds LIST projection (InboundService.GetAll) drops out_json/addrs,
	// so a plain "inbounds" GET can't seed the replica's node-side link snapshot
	// (subscription aggregation later reads out_json). Resolve the wanted tags to
	// node inbound ids from the list, then re-fetch those by id — getById returns
	// the MarshalFull shape, which carries out_json and addrs. Both endpoints are
	// stock apiv2, so the node needs no change.
	listObj, err := s.nodeGet(node, client, "inbounds", nil)
	if err != nil {
		return err
	}
	var list struct {
		Inbounds []struct {
			Id  uint   `json:"id"`
			Tag string `json:"tag"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(listObj, &list); err != nil {
		return common.NewError("unexpected inbounds payload from node")
	}
	var ids []string
	for _, ib := range list.Inbounds {
		if wanted[ib.Tag] {
			ids = append(ids, strconv.FormatUint(uint64(ib.Id), 10))
		}
	}
	if len(ids) == 0 {
		return common.NewError("no matching inbounds found on the node")
	}

	fullObj, err := s.nodeGet(node, client, "inbounds", url.Values{"id": {strings.Join(ids, ",")}})
	if err != nil {
		return err
	}
	var payload struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if err := json.Unmarshal(fullObj, &payload); err != nil {
		return common.NewError("unexpected inbounds payload from node")
	}

	db := database.GetDB()
	err = db.Transaction(func(tx *gorm.DB) error {
		dt := time.Now().Unix()
		adopted := 0
		for _, raw := range payload.Inbounds {
			var meta struct {
				Tag string `json:"tag"`
			}
			if err := json.Unmarshal(raw, &meta); err != nil {
				return err
			}
			if !wanted[meta.Tag] {
				continue
			}
			// tag must be globally unique (DB constraint + reconciliation key).
			var count int64
			if err := tx.Model(model.Inbound{}).Where("tag = ?", meta.Tag).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return common.NewErrorf("tag %q already exists here — rename it on the node first", meta.Tag)
			}
			replica, err := buildReplicaInbound(raw, nodeId)
			if err != nil {
				return err
			}
			if err := tx.Create(replica).Error; err != nil {
				return err
			}
			adopted++
		}
		if adopted == 0 {
			return common.NewError("no matching inbounds found on the node")
		}
		if err := tx.Create(&model.Changes{
			DateTime: dt, Actor: actor, Key: "inbounds", Action: "adopt",
			Obj: json.RawMessage(mustJSON(map[string]interface{}{"nodeId": nodeId, "tags": tags})),
		}).Error; err != nil {
			return err
		}
		// Mark for sync inside the tx: if the caller's immediate push is
		// skipped (busy) or fails, the heartbeat converges the node anyway.
		bumpDirtyGen()
		if err := tx.Model(model.Node{}).Where("id = ?", nodeId).Update("dirty", true).Error; err != nil {
			return err
		}
		MarkLastUpdate(time.Now().Unix())
		return nil
	})
	if err == nil {
		// Only now is the write visible on the hub's own connection.
		NotifyConfigChanged()
	}
	return err
}

// buildReplicaInbound turns a node's panel-shape inbound into a local replica:
// keep type/tag/addrs/out_json (links already point at the node), drop tls_id
// (TLS terminates on the node), strip panel-only keys from Options.
func buildReplicaInbound(raw json.RawMessage, nodeId uint) (*model.Inbound, error) {
	var full map[string]interface{}
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, err
	}
	inb := &model.Inbound{
		Type:   asString(full["type"]),
		Tag:    asString(full["tag"]),
		NodeId: &nodeId,
	}
	if addrs, ok := full["addrs"]; ok && addrs != nil {
		inb.Addrs, _ = json.MarshalIndent(addrs, "", "  ")
	}
	if outJson, ok := full["out_json"]; ok && outJson != nil {
		inb.OutJson, _ = json.MarshalIndent(outJson, "", "  ")
	}
	for _, k := range []string{"id", "tls_id", "tls", "addrs", "out_json", "users", "node_id", "type", "tag"} {
		delete(full, k)
	}
	options, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return nil, err
	}
	inb.Options = options
	return inb, nil
}

// ---------- reconciliation ----------

type nodeClientState struct {
	Id       uint            `json:"id"`
	Name     string          `json:"name"`
	Enable   bool            `json:"enable"`
	Config   json.RawMessage `json:"config"`
	Inbounds json.RawMessage `json:"inbounds"`
	Expiry   int64           `json:"expiry"`
	Group    string          `json:"group"`
	Links    json.RawMessage `json:"links"`
	Up       int64           `json:"up"`
	Down     int64           `json:"down"`
}

// Reconcile makes a node's @cluster clients match the master's expectation:
// clients that reference any of this node's replica inbounds. Together with
// ReconcileNow it is the ONLY path that writes clients to a node.
//
// This is the background entry: single-flight per node plus a 30s backoff so
// heartbeat/fanout triggers don't stampede. A skip returns nil — the dirty
// flag stays set and the next heartbeat retries.
func (s *NodeSyncService) Reconcile(nodeId uint) error {
	gen, ok := s.claimReconcile(nodeId, false)
	if !ok {
		return nil
	}
	defer s.releaseReconcile(nodeId)
	return s.runReconcile(nodeId, gen)
}

// ReconcileNow is the interactive entry (manual sync button, post-adoption
// push): it skips the backoff — the user asked, so sync — and reports a busy
// overlap as an error instead of silently doing nothing, so the UI never
// toasts success for a no-op.
func (s *NodeSyncService) ReconcileNow(nodeId uint) error {
	gen, ok := s.claimReconcile(nodeId, true)
	if !ok {
		return common.NewError("a sync for this node is already running — try again shortly")
	}
	defer s.releaseReconcile(nodeId)
	return s.runReconcile(nodeId, gen)
}

func (s *NodeSyncService) runReconcile(nodeId uint, startGen uint64) error {
	node, err := s.getNodeById(nodeId)
	if err != nil {
		return err
	}
	// Background callers pre-filter on enable=true in SQL; only the manual
	// button / post-adoption push can land here disabled — tell them.
	if !node.Enable {
		return common.NewError("node is disabled — enable it before syncing")
	}
	client := nodePushClient(node)

	// tag -> node-local inbound id
	tagToId, err := s.nodeInboundTagMap(node, client)
	if err != nil {
		return err
	}

	expected, err := s.expectedClients(nodeId, tagToId)
	if err != nil {
		return err
	}
	actual, err := s.actualClusterClients(node, client)
	if err != nil {
		return err
	}

	// new / edit
	for name, want := range expected {
		cur, exists := actual[name]
		if !exists {
			if _, err := s.pushClient(node, client, "new", want); err != nil {
				logger.Warning("reconcile: push new ", name, " to node ", node.Name, ": ", err)
				return err
			}
		} else if clientDiffers(want, cur) {
			want["id"] = cur.Id // node-local id for the edit
			if _, err := s.pushClient(node, client, "edit", want); err != nil {
				logger.Warning("reconcile: push edit ", name, " to node ", node.Name, ": ", err)
				return err
			}
		}
	}
	// del: on the node, in @cluster, but no longer expected
	for name, cur := range actual {
		if _, ok := expected[name]; !ok {
			if _, err := s.pushClient(node, client, "del", cur.Id); err != nil {
				logger.Warning("reconcile: push del ", name, " to node ", node.Name, ": ", err)
				return err
			}
		}
	}

	// Fold this node's routes into the master subscription. The node never hands
	// its own links back (its apiv2 clients projection omits the column), so we
	// regenerate them locally from each replica inbound's out_json snapshot.
	s.refreshNodeLinks(node)

	now := time.Now().Unix()
	db := database.GetDB()
	// An edit that landed while this run was reading state bumped the
	// generation: leave dirty set (the heartbeat reruns the diff) instead of
	// wiping a mark whose change we never pushed.
	if !s.dirtyUnchangedSince(startGen) {
		return db.Model(model.Node{}).Where("id = ?", nodeId).Update("last_sync", now).Error
	}
	res := db.Model(model.Node{}).Where("id = ? AND dirty = ?", nodeId, true).
		Updates(map[string]interface{}{"dirty": false, "last_sync": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		// The nodes list hides behind the lu gate; without this bump a cleared
		// badge lingers in every open UI until some unrelated change.
		SetLastUpdate(now)
		return nil
	}
	return db.Model(model.Node{}).Where("id = ?", nodeId).Update("last_sync", now).Error
}

// expectedClients builds the desired @cluster client set for a node: every
// master client that references at least one of the node's replica inbounds,
// shaped as a node-local client (Volume=0, Expiry copied, Group=@cluster,
// Inbounds mapped tag->node id, Links omitted so the node generates them).
func (s *NodeSyncService) expectedClients(nodeId uint, tagToId map[string]uint) (map[string]map[string]interface{}, error) {
	db := database.GetDB()

	// replica inbound id -> tag, for this node
	var replicas []model.Inbound
	if err := db.Model(model.Inbound{}).Where("node_id = ?", nodeId).Find(&replicas).Error; err != nil {
		return nil, err
	}
	replicaTagById := map[uint]string{}
	replicaIds := map[uint]bool{}
	for _, r := range replicas {
		replicaTagById[r.Id] = r.Tag
		replicaIds[r.Id] = true
	}
	if len(replicaIds) == 0 {
		return map[string]map[string]interface{}{}, nil
	}

	var clients []model.Client
	if err := db.Model(model.Client{}).Find(&clients).Error; err != nil {
		return nil, err
	}

	expected := map[string]map[string]interface{}{}
	for i := range clients {
		c := &clients[i]
		var ids []uint
		if err := json.Unmarshal(c.Inbounds, &ids); err != nil {
			continue
		}
		var nodeLocalIds []uint
		for _, id := range ids {
			if tag, ok := replicaTagById[id]; ok {
				if localId, ok := tagToId[tag]; ok {
					nodeLocalIds = append(nodeLocalIds, localId)
				}
			}
		}
		if len(nodeLocalIds) == 0 {
			continue
		}
		inboundsJSON, _ := json.Marshal(nodeLocalIds)
		expected[c.Name] = map[string]interface{}{
			"name":     c.Name,
			"enable":   c.Enable,
			"config":   c.Config,
			"inbounds": json.RawMessage(inboundsJSON),
			// links must be present (even empty): the node's link-refresh path
			// unmarshals it and a nil RawMessage is "unexpected end of JSON input".
			// The node regenerates the actual links from the request Host.
			"links":  json.RawMessage("[]"),
			"volume": 0,        // quota is the master's job only
			"expiry": c.Expiry, // absolute; node self-expires consistently
			"group":  clusterGroup,
			"desc":   c.Desc,
		}
	}
	return expected, nil
}

func (s *NodeSyncService) actualClusterClients(node *model.Node, client *http.Client) (map[string]nodeClientState, error) {
	// full=1 asks the node to include config, which clientDiffers needs; without
	// it every client would read as changed and be re-pushed every round.
	obj, err := s.nodeGet(node, client, "clients", url.Values{"full": {"1"}})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Clients []nodeClientState `json:"clients"`
	}
	if err := json.Unmarshal(obj, &payload); err != nil {
		return nil, common.NewError("unexpected clients payload from node")
	}
	out := map[string]nodeClientState{}
	for _, c := range payload.Clients {
		if c.Group == clusterGroup {
			out[c.Name] = c
		}
	}
	return out, nil
}

func (s *NodeSyncService) nodeInboundTagMap(node *model.Node, client *http.Client) (map[string]uint, error) {
	obj, err := s.nodeGet(node, client, "inbounds", nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Inbounds []struct {
			Id  uint   `json:"id"`
			Tag string `json:"tag"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(obj, &payload); err != nil {
		return nil, common.NewError("unexpected inbounds payload from node")
	}
	m := make(map[string]uint, len(payload.Inbounds))
	for _, ib := range payload.Inbounds {
		m[ib.Tag] = ib.Id
	}
	return m, nil
}

// clientDiffers compares the master's desired client against the node's current
// one on the fields we own. Config is compared structurally to avoid whitespace noise.
func clientDiffers(want map[string]interface{}, cur nodeClientState) bool {
	if asBool(want["enable"]) != cur.Enable {
		return true
	}
	if asInt64(want["expiry"]) != cur.Expiry {
		return true
	}
	// Compare config only when both sides actually carry one. jsonEqual fails on
	// a nil operand, so an absent config would report EVERY client as changed on
	// EVERY round — which is exactly what an older node yields, since its clients
	// projection has no config column, and what a client saved without a config
	// yields on either end. The cost of a false "differs" is a re-push per client
	// per round, each writing a credential-bearing changes row that nothing
	// prunes, so treat absence as "cannot compare", not as "differs".
	//
	// The price of that choice: a node-side config that was CLEARED (someone
	// edited an @cluster client in the node's own UI) reads as "cannot compare"
	// too, so the safety net will not repair it — this diff self-heals a changed
	// config, not a deleted one. Comparing whenever either side has a config
	// would cover it, at the cost of re-pushing every client every round against
	// any node too old to return the column.
	wantConfig, _ := want["config"].(json.RawMessage)
	if len(wantConfig) > 0 && len(cur.Config) > 0 && !jsonEqual(wantConfig, cur.Config) {
		return true
	}
	if !jsonEqual(want["inbounds"], cur.Inbounds) {
		return true
	}
	return false
}

// genNodeReplicaLinks builds the share links for one client on one replica
// inbound entirely from local state — no round-trip to the node. The replica's
// out_json (the node-side server/port/TLS snapshot captured at adoption) supplies
// the transport, and the client's config supplies the per-user credentials.
//
// It mirrors util.LinkGenerator but feeds the TLS snapshot in as a per-address
// block instead of resolving a local Tls record: the replica carries no tls_id
// (TLS terminates on the node, so adoption drops it), and with tls_id==0
// LinkGenerator passes addr["tls"] straight through to the per-protocol builders
// — reproducing exactly the reality/tls params the node itself would emit.
func genNodeReplicaLinks(replica *model.Inbound, c *model.Client) []string {
	if len(replica.OutJson) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(replica.OutJson, &out); err != nil || out == nil {
		return nil
	}
	server, _ := out["server"].(string)
	if server == "" {
		return nil
	}

	base := map[string]interface{}{
		"server":      server,
		"server_port": out["server_port"],
	}
	// enabled must be a real bool: the per-protocol builders do tls["enabled"].(bool)
	// unguarded, which would panic on a missing/!bool value.
	if tls, ok := out["tls"].(map[string]interface{}); ok {
		if _, isBool := tls["enabled"].(bool); isBool {
			base["tls"] = tls
		}
	}

	// Honour the replica's address book when the node has one, backfilling the
	// snapshot's server/port/tls onto entries that don't override them.
	var book []map[string]interface{}
	if len(replica.Addrs) > 0 {
		_ = json.Unmarshal(replica.Addrs, &book)
	}
	var addrs []map[string]interface{}
	for _, a := range book {
		if _, ok := a["server"]; !ok {
			a["server"] = base["server"]
		}
		if _, ok := a["server_port"]; !ok {
			a["server_port"] = base["server_port"]
		}
		if _, ok := a["tls"]; !ok {
			if tls, ok := base["tls"]; ok {
				a["tls"] = tls
			}
		}
		addrs = append(addrs, a)
	}
	if len(addrs) == 0 {
		addrs = []map[string]interface{}{base}
	}

	synthetic := *replica
	synthetic.TlsId = 0
	synthetic.Tls = nil
	synthetic.Addrs, _ = json.Marshal(addrs)

	return safeLinkGenerator(c.Config, &synthetic, server, c.Remark)
}

// safeLinkGenerator wraps util.LinkGenerator, which has unguarded type
// assertions over the inbound/tls maps. Here the data originates from the node
// (its out_json snapshot), so a malformed snapshot — a compromised or
// version-skewed node — could panic. This runs inside a background reconcile
// goroutine with no recover of its own, so a panic would take the whole process
// down and then crash-loop. Contain it: a bad snapshot yields no link.
func safeLinkGenerator(config json.RawMessage, i *model.Inbound, server, remark string) (links []string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warning("reconcile: link generation panicked for ", i.Tag, ": ", r)
			links = nil
		}
	}()
	return util.LinkGenerator(config, i, server, remark)
}

// refreshNodeLinks re-derives the "[node] " external links for every master
// client from this node's replica inbounds and folds them into client.Links,
// replacing any previous entries under the same "[node] " prefix (idempotent).
// A client that no longer references the node has its stale prefix links stripped.
// Writes (and the resulting Changes row / LastUpdate bump) happen only when a
// client's links actually change, so the heartbeat/hourly reconcile doesn't churn.
// The existing subscription output then serves the external entries with no extra work.
func (s *NodeSyncService) refreshNodeLinks(node *model.Node) {
	refreshLinksMu.Lock()
	defer refreshLinksMu.Unlock()

	db := database.GetDB()

	var replicas []model.Inbound
	if err := db.Model(model.Inbound{}).Where("node_id = ?", node.Id).Find(&replicas).Error; err != nil {
		logger.Warning("reconcile: load replicas for link refresh: ", err)
		return
	}
	replicaById := make(map[uint]*model.Inbound, len(replicas))
	for i := range replicas {
		replicaById[replicas[i].Id] = &replicas[i]
	}

	var clients []model.Client
	if err := db.Model(model.Client{}).Find(&clients).Error; err != nil {
		logger.Warning("reconcile: load clients for link refresh: ", err)
		return
	}

	prefix := nodeLinkPrefix(node.Name)
	touched := false

	for i := range clients {
		c := &clients[i]

		var ids []uint
		_ = json.Unmarshal(c.Inbounds, &ids)
		var desired []map[string]string
		for _, id := range ids {
			rep, ok := replicaById[id]
			if !ok {
				continue
			}
			for _, uri := range genNodeReplicaLinks(rep, c) {
				desired = append(desired, map[string]string{
					"remark": prefix + rep.Tag,
					"type":   "external",
					"uri":    uri,
				})
			}
		}

		var existing []map[string]string
		_ = json.Unmarshal(c.Links, &existing)
		hadPrefix := false
		kept := make([]map[string]string, 0, len(existing))
		for _, l := range existing {
			// Only our own external entries: a LOCAL inbound whose tag happens to
			// start with this node's prefix generates a link with the same remark,
			// and stripping it here would drop it from the subscription until an
			// unrelated client save regenerated it.
			if l["type"] != "local" && strings.HasPrefix(l["remark"], prefix) {
				hadPrefix = true
				continue
			}
			kept = append(kept, l)
		}
		// Nothing to add and nothing to strip: leave clients unrelated to this
		// node untouched (and don't rewrite a null Links into "[]").
		if len(desired) == 0 && !hadPrefix {
			continue
		}

		merged := append(kept, desired...)
		newLinks, err := json.MarshalIndent(merged, "", "  ")
		if err != nil {
			continue
		}
		if jsonEqual(c.Links, newLinks) {
			continue // node links unchanged
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(model.Client{}).Where("id = ?", c.Id).Update("links", newLinks).Error; err != nil {
				return err
			}
			return tx.Create(&model.Changes{
				DateTime: time.Now().Unix(), Actor: "NodeSync", Key: "clients", Action: "edit",
				Obj: json.RawMessage(mustJSON(map[string]interface{}{"name": c.Name, "node": node.Name})),
			}).Error
		}); err != nil {
			logger.Warning("reconcile: refresh links for ", c.Name, ": ", err)
			continue
		}
		touched = true
	}

	if touched {
		SetLastUpdate(time.Now().Unix())
	}
}

// ---------- dirty tracking / triggers ----------

func (s *NodeSyncService) MarkAllDirty() {
	bumpDirtyGen()
	// dirty = false predicate: this runs on the request path of every fanout
	// save, so skip rewriting rows that are already flagged.
	if err := database.GetDB().Model(model.Node{}).Where("enable = ? AND dirty = ?", true, false).Update("dirty", true).Error; err != nil {
		logger.Warning("nodes: mark all dirty: ", err)
	}
}

// ReconcileDirtyOnline reconciles every enabled node that is online and dirty.
// Called by the heartbeat so offline-period edits converge once a node returns.
func (s *NodeSyncService) ReconcileDirtyOnline() {
	var nodes []model.Node
	// Select("id"): only the id is used, and a full Find would hydrate every
	// node's Baselines blob and Token on each trigger.
	if err := database.GetDB().Model(model.Node{}).Select("id").Where("enable = ? AND dirty = ?", true, true).Find(&nodes).Error; err != nil {
		return
	}
	statuses := s.GetStatuses()
	for i := range nodes {
		st, ok := statuses[nodes[i].Id]
		if !ok || st.State != "online" {
			continue
		}
		id := nodes[i].Id
		go func() {
			if err := s.Reconcile(id); err != nil {
				logger.Warning("nodes: background reconcile failed: ", err)
			}
		}()
	}
}

// ReconcileAllOnline reconciles all online nodes regardless of dirty flag —
// the hourly safety net that repairs silent node-side drift.
func (s *NodeSyncService) ReconcileAllOnline() {
	var nodes []model.Node
	if err := database.GetDB().Model(model.Node{}).Select("id").Where("enable = ?", true).Find(&nodes).Error; err != nil {
		return
	}
	statuses := s.GetStatuses()
	for i := range nodes {
		st, ok := statuses[nodes[i].Id]
		if !ok || st.State != "online" {
			continue
		}
		if err := s.Reconcile(nodes[i].Id); err != nil {
			logger.Warning("nodes: safety-net reconcile failed: ", err)
		}
	}
}

// ---------- traffic collection ----------

type trafficBaseline struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// CollectTraffic pulls each online node's @cluster client counters and folds
// the delta since the last collection into the master's per-client totals.
// The node's clients.up/down are cumulative; a per-(node,client) baseline turns
// them into deltas, resetting the baseline when the node's counter drops (reset).
func (s *NodeSyncService) CollectTraffic() {
	var nodes []model.Node
	if err := database.GetDB().Model(model.Node{}).Where("enable = ?", true).Find(&nodes).Error; err != nil {
		return
	}
	statuses := s.GetStatuses()
	for i := range nodes {
		node := &nodes[i]
		st, ok := statuses[node.Id]
		if !ok || st.State != "online" {
			continue
		}
		if err := s.collectNodeTraffic(node); err != nil {
			logger.Warning("nodes: collect traffic from ", node.Name, ": ", err)
		}
	}
}

func (s *NodeSyncService) collectNodeTraffic(node *model.Node) error {
	client := nodePushClient(node)
	current, err := s.actualClusterClients(node, client)
	if err != nil {
		return err
	}

	// existing baseline
	baseline := map[string]trafficBaseline{}
	if node.Baselines != nil {
		json.Unmarshal(node.Baselines, &baseline)
	}

	// master client names (only fold traffic for clients we actually own)
	masterNames := map[string]bool{}
	var names []string
	db := database.GetDB()
	db.Model(model.Client{}).Pluck("name", &names)
	for _, n := range names {
		masterNames[n] = true
	}

	newBaseline := map[string]trafficBaseline{}
	type delta struct{ up, down int64 }
	deltas := map[string]delta{}
	for name, cur := range current {
		curUp := clientUp(cur)
		curDown := clientDown(cur)
		newBaseline[name] = trafficBaseline{Up: curUp, Down: curDown}
		if !masterNames[name] {
			continue
		}
		base := baseline[name]
		du := curUp - base.Up
		if du < 0 { // node counter reset — rebase
			du = curUp
		}
		dd := curDown - base.Down
		if dd < 0 {
			dd = curDown
		}
		if du > 0 || dd > 0 {
			deltas[name] = delta{du, dd}
		}
	}

	baselineJSON, err := json.Marshal(newBaseline)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for name, d := range deltas {
			if err := tx.Model(model.Client{}).Where("name = ?", name).
				Updates(map[string]interface{}{
					"up":   gorm.Expr("up + ?", d.up),
					"down": gorm.Expr("down + ?", d.down),
				}).Error; err != nil {
				return err
			}
		}
		return tx.Model(model.Node{}).Where("id = ?", node.Id).Update("baselines", baselineJSON).Error
	})
}

// nodeClientState only carries name/enable/config/inbounds/expiry/group/links;
// up/down come from a second projection of the same clients GET payload.
func clientUp(c nodeClientState) int64   { return c.Up }
func clientDown(c nodeClientState) int64 { return c.Down }

// ---------- helpers ----------

func (s *NodeSyncService) getNodeById(id uint) (*model.Node, error) {
	var node model.Node
	if err := database.GetDB().Model(model.Node{}).Where("id = ?", id).First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

// claimReconcile takes the per-node single-flight slot and snapshots the dirty
// generation the run reconciles against. force (interactive callers) skips the
// backoff but never the busy check.
func (s *NodeSyncService) claimReconcile(nodeId uint, force bool) (uint64, bool) {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	if reconcileBusy[nodeId] {
		return 0, false
	}
	if !force {
		if last, ok := reconcileLast[nodeId]; ok && time.Since(last) < reconcileBackoff {
			return 0, false
		}
	}
	reconcileBusy[nodeId] = true
	return dirtyGen, true
}

// bumpDirtyGen must run BEFORE the corresponding dirty=true DB write: a
// reconcile that claimed earlier then sees a newer generation and refuses to
// clear a flag whose change it never read.
func bumpDirtyGen() {
	reconcileMu.Lock()
	dirtyGen++
	reconcileMu.Unlock()
}

func (s *NodeSyncService) dirtyUnchangedSince(gen uint64) bool {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	return dirtyGen == gen
}

func (s *NodeSyncService) releaseReconcile(nodeId uint) {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	reconcileBusy[nodeId] = false
	reconcileLast[nodeId] = time.Now()
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func asBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func asInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

// jsonEqual compares two JSON values structurally (ignoring key order / whitespace).
func jsonEqual(a interface{}, b json.RawMessage) bool {
	var ar json.RawMessage
	switch v := a.(type) {
	case json.RawMessage:
		ar = v
	case []byte:
		ar = v
	default:
		m, err := json.Marshal(a)
		if err != nil {
			return false
		}
		ar = m
	}
	var av, bv interface{}
	if err := json.Unmarshal(ar, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	am, _ := json.Marshal(canonical(av))
	bm, _ := json.Marshal(canonical(bv))
	return string(am) == string(bm)
}

// canonical recursively normalises maps so Marshal is order-stable.
func canonical(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = canonical(val)
		}
		return out
	case []interface{}:
		for i := range t {
			t[i] = canonical(t[i])
		}
		return t
	}
	return v
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
