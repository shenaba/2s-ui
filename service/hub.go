package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"

	"github.com/coder/websocket"
)

// The websocket hub pushes to the panel SPA what it used to poll over HTTP:
//
//	topic "load"   — the api/load payload: full snapshot on subscribe (lu-gated),
//	                 onlines after every stats flush, nodesStatus after every
//	                 node probe, and the full config payload when it changes.
//	topic "status" — the api/status sample, from a 2s ticker that only runs
//	                 while at least one client subscribes.
//	topic "stats"  — the api/stats rows for one (resource, tag, period),
//	                 refreshed after every stats flush (when the data can
//	                 actually change).
//
// The hub is a package-level singleton (same pattern as corePtr) because the
// api package depends on service, not the other way around: cron jobs and
// ConfigService notify it directly, api.WsHandler hands accepted connections
// to HubServe.

const (
	hubSendBuffer   = 64
	hubReadLimit    = 8192
	hubPingInterval = 30 * time.Second
	hubWriteTimeout = 10 * time.Second
	statusTickEvery = 2 * time.Second
	// Save and node sync bump LastUpdate several times in a row; coalesce the
	// burst into one full-payload build.
	configDebounce = 200 * time.Millisecond
	// Floor between two database-backed subscribes on one connection. Real UI
	// actions (open a modal, switch a period, toggle a tile) are seconds apart;
	// anything faster is a loop and gets its answer from the periodic push.
	minQueryInterval = 250 * time.Millisecond
)

var (
	hubMu sync.RWMutex
	hub   *Hub
)

type wsClientMsg struct {
	Op     string          `json:"op"`
	Topic  string          `json:"topic"`
	Params json.RawMessage `json:"params"`
}

type wsEnvelope struct {
	Topic string      `json:"topic"`
	Data  interface{} `json:"data"`
}

type statsSubKey struct {
	Resource string `json:"resource"`
	Tag      string `json:"tag"`
	Period   string `json:"period"`
}

type hubClient struct {
	conn     *websocket.Conn
	hostname string
	send     chan []byte
	closed   chan struct{}
	closeOne sync.Once
	// When the session that authorised this handshake stops being valid. Zero
	// means no cap. Immutable after construction, so it needs no lock.
	deadline time.Time

	// mu guards the subscription state below. Lock order is hub.mu before
	// client.mu; nothing takes hub.mu while holding client.mu.
	mu       sync.Mutex
	subLoad  bool
	statusR  []string     // nil = not subscribed to "status"
	statsKey *statsSubKey // nil = not subscribed to "stats"
	// Rate limiter for subscribes that hit the database, keyed by topic. Read
	// limits bound message size, not rate, so without this a looping client
	// could drive unbounded query load from one session. Per topic rather than
	// per connection so a reconnect, which re-sends every subscription at once,
	// does not throttle its own second topic.
	lastQuery map[string]time.Time
}

type Hub struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	clients map[*hubClient]struct{}
	stopped bool // set under mu before wg.Wait, so no Add can race the Wait

	configDirty chan struct{} // buffered(1), non-blocking producers

	statusMu   sync.Mutex
	statusStop chan struct{} // non-nil while statusLoop runs

	// Last pushed nodesStatus size, letting us skip empty→empty pushes while
	// still sending one final empty map when the last node is deleted.
	nodesMu    sync.Mutex
	lastNodesN int
}

// StartHub creates the singleton. Called from app.Start before the web server
// accepts connections and before the cron jobs first fire.
func StartHub() {
	hubMu.Lock()
	defer hubMu.Unlock()
	if hub != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		ctx:         ctx,
		cancel:      cancel,
		clients:     make(map[*hubClient]struct{}),
		configDirty: make(chan struct{}, 1),
	}
	h.wg.Add(1)
	go h.configNotifier()
	hub = h
}

// StopHub closes every live connection and waits for the hub's goroutines.
// http.Server.Shutdown ignores hijacked connections, so without this every
// RestartApp (SIGHUP, api/restartApp) would leak one goroutine pair and one
// socket per open tab. Called from app.Stop after webServer.Stop — the
// listener is already closed, so no new handshake can arrive mid-teardown.
func StopHub() {
	hubMu.Lock()
	h := hub
	hub = nil
	hubMu.Unlock()
	if h == nil {
		return
	}
	h.cancel()

	h.mu.Lock()
	h.stopped = true
	clients := make([]*hubClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[*hubClient]struct{})
	h.mu.Unlock()

	// GoingAway makes the browser start its reconnect loop immediately. Close
	// performs the closing handshake, so run them in parallel with a small
	// budget and hard-close the stragglers.
	done := make(chan struct{})
	go func() {
		var cw sync.WaitGroup
		for _, c := range clients {
			cw.Add(1)
			go func(c *hubClient) {
				defer cw.Done()
				c.closeOne.Do(func() { close(c.closed) })
				_ = c.conn.Close(websocket.StatusGoingAway, "panel restarting")
			}(c)
		}
		cw.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		for _, c := range clients {
			_ = c.conn.CloseNow()
		}
		<-done
	}
	h.wg.Wait()
}

func getHub() *Hub {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hub
}

// DropAllClients closes every live connection but leaves the hub running.
// Authentication is handshake-only, so after the admin credentials change an
// already-open socket would keep streaming the full config — client UUIDs,
// passwords, subURI — to a tab that authenticated with the old ones. Browsers
// reconnect within a second; a session that is still valid simply re-auths.
func DropAllClients() {
	h := getHub()
	if h == nil {
		return
	}
	// Collect first: dropClient takes h.mu itself.
	h.mu.Lock()
	clients := make([]*hubClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		h.dropClient(c)
	}
}

// HubServe registers an accepted connection and blocks reading it until it
// drops. Runs on the (hijacked) HTTP handler goroutine.
func HubServe(conn *websocket.Conn, hostname string, deadline time.Time) {
	h := getHub()
	if h == nil {
		_ = conn.Close(websocket.StatusGoingAway, "hub not running")
		return
	}
	c := &hubClient{
		conn:     conn,
		hostname: hostname,
		send:     make(chan []byte, hubSendBuffer),
		closed:   make(chan struct{}),
		deadline: deadline,
	}
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "panel restarting")
		return
	}
	h.clients[c] = struct{}{}
	h.wg.Add(1)
	h.mu.Unlock()

	go h.writePump(c)
	h.readPump(c)
}

func (h *Hub) readPump(c *hubClient) {
	defer h.dropClient(c)
	c.conn.SetReadLimit(hubReadLimit)
	for {
		_, data, err := c.conn.Read(h.ctx)
		if err != nil {
			return
		}
		h.handleClientMsg(c, data)
	}
}

// writePump is the connection's only writer. Keeping a continuous Read in
// flight on the readPump side is what services the pong replies Ping waits on.
func (h *Hub) writePump(c *hubClient) {
	defer h.wg.Done()
	ticker := time.NewTicker(hubPingInterval)
	defer ticker.Stop()
	for {
		select {
		case msg := <-c.send:
			ctx, cancel := context.WithTimeout(h.ctx, hubWriteTimeout)
			err := c.conn.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				h.dropClient(c)
				return
			}
		case <-ticker.C:
			// The ping tick doubles as the session check: auth only happens at
			// the handshake, so this is the only thing that ends a socket whose
			// session has since expired. Granularity is one ping interval,
			// which is fine for a bound measured in hours.
			if !c.deadline.IsZero() && time.Now().After(c.deadline) {
				logger.Debug("ws: session expired, dropping client")
				h.dropClient(c)
				return
			}
			ctx, cancel := context.WithTimeout(h.ctx, hubWriteTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				h.dropClient(c)
				return
			}
		case <-c.closed:
			return
		case <-h.ctx.Done():
			return
		}
	}
}

// dropClient is idempotent: readPump, writePump and a full send buffer all
// funnel here. Never call it while holding h.mu.
func (h *Hub) dropClient(c *hubClient) {
	h.mu.Lock()
	_, wasRegistered := h.clients[c]
	delete(h.clients, c)
	h.mu.Unlock()
	c.closeOne.Do(func() { close(c.closed) })
	_ = c.conn.CloseNow()
	if wasRegistered {
		h.ensureStatusLoop()
	}
}

func (h *Hub) handleClientMsg(c *hubClient, data []byte) {
	var msg wsClientMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Debug("ws: bad client message: ", err)
		return
	}
	switch msg.Op {
	case "subscribe":
		h.subscribe(c, msg.Topic, msg.Params)
	case "unsubscribe":
		h.unsubscribe(c, msg.Topic)
	default:
		logger.Debug("ws: unknown op: ", msg.Op)
	}
}

// allowQuery rate-limits the subscribes that answer straight out of the
// database, so a client that loops subscribe frames cannot saturate the
// connection pool. The subscription itself is always recorded; only the
// immediate answer is skipped.
func (c *hubClient) allowQuery(topic string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastQuery == nil {
		c.lastQuery = make(map[string]time.Time, 2)
	}
	if last, ok := c.lastQuery[topic]; ok && now.Sub(last) < minQueryInterval {
		return false
	}
	c.lastQuery[topic] = now
	return true
}

// subscribe (re)binds one topic; re-subscribing just replaces the params —
// that is how the UI switches the status resource list or the stats period.
func (h *Hub) subscribe(c *hubClient, topic string, params json.RawMessage) {
	switch topic {
	case "load":
		var p struct {
			Lu int64 `json:"lu"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		// Join the broadcast set BEFORE building the snapshot, so a config
		// change committing during the build reaches this client too. That
		// ordering used to be unsafe -- the push would be queued ahead of the
		// older snapshot and the client would apply new-then-old -- which is
		// what the config version now prevents: the snapshot carries the lower
		// cseq and the client discards it.
		c.mu.Lock()
		c.subLoad = true
		c.mu.Unlock()
		if c.allowQuery("load", time.Now()) {
			h.sendLoadSnapshot(c, p.Lu)
		}
	case "status":
		var p struct {
			R []string `json:"r"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		if len(p.R) == 0 {
			p.R = []string{"net", "sbd"}
		}
		c.mu.Lock()
		c.statusR = p.R
		c.mu.Unlock()
		h.ensureStatusLoop()
		// Answer immediately like the other topics do: the ticker's first
		// sample is a full interval away, and resubscribe() is the frontend's
		// way of applying new params (resource-tile toggle) right now.
		h.enqueue(c, statusEnvelope((&ServerService{}).GetStatus(strings.Join(p.R, ","))))
	case "stats":
		var p statsSubKey
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		if p.Resource == "" || p.Tag == "" {
			logger.Debug("ws: stats subscribe without resource/tag")
			return
		}
		if p.Period == "" {
			p.Period = "hour"
		}
		c.mu.Lock()
		c.statsKey = &p
		c.mu.Unlock()
		if c.allowQuery("stats", time.Now()) {
			h.sendStats(c, p)
		}
	default:
		logger.Debug("ws: unknown topic: ", topic)
	}
}

func (h *Hub) unsubscribe(c *hubClient, topic string) {
	switch topic {
	case "load":
		c.mu.Lock()
		c.subLoad = false
		c.mu.Unlock()
	case "status":
		c.mu.Lock()
		c.statusR = nil
		c.mu.Unlock()
		h.ensureStatusLoop()
	case "stats":
		c.mu.Lock()
		c.statsKey = nil
		c.mu.Unlock()
	}
}

// sendLoadSnapshot answers a load subscribe with exactly api/load semantics:
// full payload when the lu gate opens (or no lu), live-only otherwise. This is
// what makes reconnects cheap — an unchanged config costs one small message.
func (h *Hub) sendLoadSnapshot(c *hubClient, lu int64) {
	var cs ConfigService
	luStr := ""
	if lu > 0 {
		luStr = strconv.FormatInt(lu, 10)
	}
	isUpdated, err := cs.CheckChanges(luStr)
	if err != nil {
		logger.Warning("ws: check changes failed: ", err)
		isUpdated = true
	}
	var pd PanelDataService
	var data map[string]interface{}
	if isUpdated {
		data, err = pd.FullPayload(c.hostname)
	} else {
		data, err = pd.LivePayload()
	}
	if err != nil {
		logger.Warning("ws: build load snapshot failed: ", err)
		return
	}
	seedNodesStatus(data)
	h.enqueue(c, hubEnvelope("load", data))
}

// seedNodesStatus makes a full payload state "no nodes" explicitly. The
// assembler omits the key when empty, but the client reads a missing key as
// "unchanged", so without this a payload that follows the last node's deletion
// leaves its badge on screen.
func seedNodesStatus(data map[string]interface{}) {
	if _, ok := data["nodesStatus"]; !ok {
		data["nodesStatus"] = map[uint]NodeStatus{}
	}
}

func (h *Hub) sendStats(c *hubClient, key statsSubKey) {
	var ss StatsService
	rows, err := ss.GetStats(key.Resource, key.Tag, key.Period)
	if err != nil {
		// Still answer, with no rows: the client renders its empty state.
		// Staying silent leaves the modal showing a blank chart forever,
		// since this topic only refreshes at the 10s flush boundary.
		logger.Warning("ws: stats query failed: ", err)
		rows = nil
	}
	h.enqueue(c, statsEnvelope(key, rows))
}

// enqueue never blocks: a client whose buffer is full is dropped instead —
// the browser reconnects and resyncs via the lu gate. Never call while
// holding h.mu (dropClient locks it).
func (h *Hub) enqueue(c *hubClient, msg []byte) {
	if msg == nil {
		return
	}
	select {
	case c.send <- msg:
	default:
		logger.Debug("ws: dropping slow client")
		h.dropClient(c)
	}
}

func hubEnvelope(topic string, data interface{}) []byte {
	b, err := json.Marshal(wsEnvelope{Topic: topic, Data: data})
	if err != nil {
		logger.Warning("ws: marshal failed: ", err)
		return nil
	}
	return b
}

func statsEnvelope(key statsSubKey, rows interface{}) []byte {
	// Echo the key so the client can discard pushes that raced a period/tag
	// switch.
	return hubEnvelope("stats", map[string]interface{}{
		"resource": key.Resource,
		"tag":      key.Tag,
		"period":   key.Period,
		"stats":    rows,
	})
}

// statusEnvelope stamps the sample with the server time so clients can derive
// rates over the real interval instead of assuming a fixed tick.
func statusEnvelope(res *map[string]interface{}) []byte {
	(*res)["t"] = time.Now().UnixMilli()
	return hubEnvelope("status", res)
}

// mergeResources flattens the subscribers' resource lists into one de-duplicated
// request, preserving first-seen order so the sampled set is deterministic.
func mergeResources(lists [][]string) []string {
	seen := map[string]bool{}
	var union []string
	for _, list := range lists {
		for _, r := range list {
			if !seen[r] {
				seen[r] = true
				union = append(union, r)
			}
		}
	}
	return union
}

// shouldPushNodes reports whether a node-status snapshot is worth sending.
// Empty→empty is skipped so zero-node panels pay nothing, but the first empty
// after a non-empty must go out or the last deleted node keeps its badge.
func shouldPushNodes(n, last int) bool {
	return n != 0 || last != 0
}

// broadcastLoad fans one pre-marshaled message out to every load subscriber.
func (h *Hub) broadcastLoad(msg []byte) {
	if msg == nil {
		return
	}
	for _, c := range h.loadSubscribers() {
		h.enqueue(c, msg)
	}
}

func (h *Hub) loadSubscribers() []*hubClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	var subs []*hubClient
	for c := range h.clients {
		c.mu.Lock()
		if c.subLoad {
			subs = append(subs, c)
		}
		c.mu.Unlock()
	}
	return subs
}

// ensureStatusLoop starts the 2s sampler on the 0→1 status-subscriber
// transition and stops it on 1→0, so an idle panel does no gopsutil work.
func (h *Hub) ensureStatusLoop() {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()
	want := false
	h.mu.Lock()
	if !h.stopped {
		for c := range h.clients {
			c.mu.Lock()
			if c.statusR != nil {
				want = true
			}
			c.mu.Unlock()
			if want {
				break
			}
		}
	}
	if want && h.statusStop == nil {
		stop := make(chan struct{})
		h.statusStop = stop
		h.wg.Add(1)
		go h.statusLoop(stop)
	} else if !want && h.statusStop != nil {
		close(h.statusStop)
		h.statusStop = nil
	}
	h.mu.Unlock()
}

func (h *Hub) statusLoop(stop chan struct{}) {
	defer h.wg.Done()
	ticker := time.NewTicker(statusTickEvery)
	defer ticker.Stop()
	var server ServerService
	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			var subs []*hubClient
			var lists [][]string
			for c := range h.clients {
				c.mu.Lock()
				if c.statusR != nil {
					subs = append(subs, c)
					lists = append(lists, c.statusR)
				}
				c.mu.Unlock()
			}
			h.mu.Unlock()
			if len(subs) == 0 {
				continue
			}
			// One sample per tick regardless of client count: everyone gets
			// the union of the requested resources (extra keys are harmless,
			// the UI merges by key).
			res := server.GetStatus(strings.Join(mergeResources(lists), ","))
			msg := statusEnvelope(res)
			for _, c := range subs {
				h.enqueue(c, msg)
			}
		case <-stop:
			return
		case <-h.ctx.Done():
			return
		}
	}
}

// configNotifier turns LastUpdate bumps into debounced full-payload pushes.
// The DB reads happen here, after the debounce, so they see committed state.
func (h *Hub) configNotifier() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.configDirty:
			timer := time.NewTimer(configDebounce)
			select {
			case <-h.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Deliberately no drain here: a notification that arrives during
			// the debounce or during the build below re-arms configDirty and
			// earns its own push. Draining would collapse a late commit into a
			// build that could not yet see it, and with no HTTP poll left
			// nothing would ever repair that.
			h.pushFullPayloads()
		}
	}
}

// pushFullPayloads builds the config payload once per distinct hostname
// (subURI depends on it) and fans out.
func (h *Hub) pushFullPayloads() {
	byHost := map[string][]*hubClient{}
	for _, c := range h.loadSubscribers() {
		byHost[c.hostname] = append(byHost[c.hostname], c)
	}
	if len(byHost) == 0 {
		return
	}
	var pd PanelDataService
	for host, subs := range byHost {
		data, err := pd.FullPayload(host)
		if err != nil {
			logger.Warning("ws: build config push failed: ", err)
			continue
		}
		seedNodesStatus(data)
		msg := hubEnvelope("load", data)
		for _, c := range subs {
			h.enqueue(c, msg)
		}
	}
}

// NotifyConfigChanged wakes the hub after a LastUpdate bump. Non-blocking and
// nil-safe: cron jobs may still fire while the hub is shutting down.
func NotifyConfigChanged() {
	h := getHub()
	if h == nil {
		return
	}
	select {
	case h.configDirty <- struct{}{}:
	default:
	}
}

// HubAfterStatsFlush runs on the StatsJob goroutine right after SaveStats.
// The onlines payload is built and marshaled here, before crossing any
// goroutine boundary, so the pre-existing unsynchronized onlineResources
// access is not widened. It also refreshes stats subscriptions — the stats
// table only changes at this flush.
func HubAfterStatsFlush() {
	h := getHub()
	if h == nil {
		return
	}
	// Check for subscribers before building: with nobody watching, assembling
	// and marshalling this payload every 10s is pure waste.
	if subs := h.loadSubscribers(); len(subs) > 0 {
		var pd PanelDataService
		data, err := pd.OnlinesPayload()
		if err != nil {
			logger.Warning("ws: build onlines push failed: ", err)
		} else {
			msg := hubEnvelope("load", data)
			for _, c := range subs {
				h.enqueue(c, msg)
			}
		}
	}
	h.pushStatsRefresh()
}

func (h *Hub) pushStatsRefresh() {
	h.mu.Lock()
	byKey := map[statsSubKey][]*hubClient{}
	for c := range h.clients {
		c.mu.Lock()
		if c.statsKey != nil {
			byKey[*c.statsKey] = append(byKey[*c.statsKey], c)
		}
		c.mu.Unlock()
	}
	h.mu.Unlock()
	if len(byKey) == 0 {
		return
	}
	var ss StatsService
	for key, subs := range byKey {
		rows, err := ss.GetStats(key.Resource, key.Tag, key.Period)
		if err != nil {
			logger.Warning("ws: stats query failed: ", err)
			continue
		}
		msg := statsEnvelope(key, rows)
		for _, c := range subs {
			h.enqueue(c, msg)
		}
	}
}

// HubPushNodesStatus runs on the NodesJob goroutine after RefreshAll.
func HubPushNodesStatus() {
	h := getHub()
	if h == nil {
		return
	}
	var ns NodeService
	statuses := ns.GetStatuses()
	// Guarded: cron.Stop does not wait for in-flight jobs, so an old NodesJob
	// can still be running against this hub when the restarted cron's new
	// instance (a different mutex) fires.
	h.nodesMu.Lock()
	push := shouldPushNodes(len(statuses), h.lastNodesN)
	h.lastNodesN = len(statuses)
	h.nodesMu.Unlock()
	if !push {
		return
	}
	h.broadcastLoad(hubEnvelope("load", map[string]interface{}{
		"nodesStatus": statuses,
	}))
}
