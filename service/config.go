package service

import (
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/network"
	"github.com/shenaba/2s-ui/service/notify"
	"github.com/shenaba/2s-ui/util"
	"github.com/shenaba/2s-ui/util/common"
)

var (
	// Written by gin handlers (Save), the DepleteJob and NodesJob cron
	// goroutines, and read by every api/load handler plus the websocket hub's
	// read pump — a plain int64 here is a data race the detector flags.
	// Unexported so the atomics cannot be bypassed; nothing outside this
	// package touches them. lastUpdateSeq counts the marks instead of stamping
	// them, so a caller can tell whether its own run marked anything --
	// comparing lastUpdate cannot, since two writers landing in the same second
	// store the same unix value.
	lastUpdate          atomic.Int64
	lastUpdateSeq       atomic.Int64
	corePtr             *core.Core
	startCoreMu         sync.Mutex
	startCoreInProgress bool
	lastStartFailTime   time.Time
	startCooldown       = 15 * time.Second
)

type ConfigService struct {
	ClientService
	TlsService
	SettingService
	InboundService
	OutboundService
	ServicesService
	EndpointService
	NodeService
}

// SingBoxConfig is the shape GetConfig decodes the stored base config into
// before filling in the objects held in the database. Every top-level sing-box
// key has to be listed here: anything missing is silently dropped on the way
// through, even when the operator wrote it by hand.
type SingBoxConfig struct {
	Schema string          `json:"$schema,omitempty"`
	Log    json.RawMessage `json:"log"`
	Dns    json.RawMessage `json:"dns"`
	Ntp    json.RawMessage `json:"ntp"`
	// Global certificate store settings, and the shared certificate providers
	// referenced by tag from a TLS config's certificate_provider. Providers are
	// edited alongside the TLS configs but stored in the base config.
	Certificate          json.RawMessage   `json:"certificate,omitempty"`
	CertificateProviders []json.RawMessage `json:"certificate_providers,omitempty"`
	// Named HTTP clients, referenced by remote rule-sets and by
	// route.default_http_client.
	HTTPClients       []json.RawMessage `json:"http_clients,omitempty"`
	NetworkNamespaces []json.RawMessage `json:"network_namespaces,omitempty"`
	Inbounds          []json.RawMessage `json:"inbounds"`
	Outbounds         []json.RawMessage `json:"outbounds"`
	Services          []json.RawMessage `json:"services"`
	Endpoints         []json.RawMessage `json:"endpoints"`
	Route             json.RawMessage   `json:"route"`
	Experimental      json.RawMessage   `json:"experimental"`
}

func NewConfigService(c *core.Core) *ConfigService {
	corePtr = c
	// The gate is read per connection rather than captured by each tracker, so
	// installing it here does not have to be ordered against the first StartCore.
	core.SetConnGate(ipLimits.allow)
	// Read once here rather than per start: this runs after the database is
	// open and before anything can start the core.
	loadMaintenance()
	return &ConfigService{}
}

func (s *ConfigService) GetConfig(data string) (*[]byte, error) {
	var err error
	if len(data) == 0 {
		data, err = s.SettingService.GetConfig()
		if err != nil {
			return nil, err
		}
	}
	singboxConfig := SingBoxConfig{}
	err = json.Unmarshal([]byte(data), &singboxConfig)
	if err != nil {
		return nil, err
	}

	singboxConfig.Inbounds, err = s.InboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Outbounds, err = s.OutboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Services, err = s.ServicesService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Endpoints, err = s.EndpointService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	if err = ensureDefaultHTTPClient(&singboxConfig); err != nil {
		return nil, err
	}
	rawConfig, err := json.MarshalIndent(singboxConfig, "", "  ")
	if err != nil {
		return nil, err
	}
	return &rawConfig, nil
}

// defaultHTTPClientTag names the HTTP client remote rule-sets download over
// when the operator has not set one up.
const defaultHTTPClientTag = "default"

// ensureDefaultHTTPClient declares an HTTP client for remote rule-sets that
// name none, so sing-box 1.14 stops reporting the implicit fallback as
// deprecated. It is added to the generated config rather than to the stored
// one, so it also covers rule-sets added later.
//
// The declared client has to reproduce the fallback, not merely replace it.
// sing-box's implicit client is built with DefaultOutbound set, which dials
// through the default outbound; that field has no JSON name (option/http.go),
// so the only way to say the same thing in a config is a detour naming the
// default outbound explicitly. Declaring a bare {"tag": "default"} instead
// leaves DefaultOutbound false, and dialer.NewWithOptions then falls through to
// a plain system dialer -- rule-set downloads would silently stop going through
// the operator's egress.
//
// Which outbound that is can only be named when route.final is set. Without it
// sing-box picks the first outbound it created, which depends on creation order
// the panel does not model, so the client is not declared at all and the
// deprecation line stays -- a log line beats a wrong dial path.
//
// An operator who named a default themselves is left alone; one who only
// declared clients still gets a default, since otherwise the rule-sets that
// name none keep falling back.
func ensureDefaultHTTPClient(config *SingBoxConfig) error {
	if len(config.Route) == 0 {
		return nil
	}
	var route map[string]json.RawMessage
	if err := json.Unmarshal(config.Route, &route); err != nil {
		// A route section the panel cannot read is passed through untouched.
		return nil
	}
	if raw, ok := route["default_http_client"]; ok && !isEmptyRawJSON(raw) {
		return nil
	}
	if !hasImplicitHTTPClientRuleSet(route["rule_set"]) {
		return nil
	}
	var finalTag string
	if raw, ok := route["final"]; ok {
		if err := json.Unmarshal(raw, &finalTag); err != nil {
			return nil
		}
	}
	if finalTag == "" {
		return nil
	}
	// Endpoints count: sing-box resolves a detour through
	// outbound.Manager.Outbound, which falls back to the endpoint manager, and
	// route.final may name a wireguard or tailscale endpoint just as well as an
	// outbound. Searching only outbounds would skip declaring the client for a
	// perfectly valid config.
	finalOutbound, found := outboundByTag(config.Outbounds, finalTag)
	if !found {
		finalOutbound, found = outboundByTag(config.Endpoints, finalTag)
	}
	if !found {
		// route.final names nothing this config defines; sing-box will refuse
		// it on its own terms, and guessing here would only add a second
		// broken reference.
		return nil
	}

	tagName := unusedHTTPClientTag(config.HTTPClients)
	fields := map[string]string{"tag": tagName}
	// A detour to a plain direct outbound is what sing-box rejects, and it is
	// also what no detour already means, so it is left out.
	if !util.IsPlainDirectOutbound(finalOutbound) {
		fields["detour"] = finalTag
	}
	client, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	tag, err := json.Marshal(tagName)
	if err != nil {
		return err
	}
	route["default_http_client"] = tag
	encodedRoute, err := json.Marshal(route)
	if err != nil {
		return err
	}
	config.HTTPClients = append(config.HTTPClients, client)
	config.Route = encodedRoute
	return nil
}

// outboundByTag finds a generated outbound or endpoint by its tag. Both are
// rendered in the same sing-box shape (type, tag, then the options), so one
// lookup serves both lists.
func outboundByTag(outbounds []json.RawMessage, tag string) (map[string]interface{}, bool) {
	for _, raw := range outbounds {
		var outbound map[string]interface{}
		if err := json.Unmarshal(raw, &outbound); err != nil {
			continue
		}
		if outboundTag, _ := outbound["tag"].(string); outboundTag == tag {
			return outbound, true
		}
	}
	return nil, false
}

// unusedHTTPClientTag names the added client without colliding with one the
// operator declared.
func unusedHTTPClientTag(clients []json.RawMessage) string {
	taken := make(map[string]struct{}, len(clients))
	for _, client := range clients {
		var fields struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(client, &fields); err == nil && fields.Tag != "" {
			taken[fields.Tag] = struct{}{}
		}
	}
	tag := defaultHTTPClientTag
	for i := 2; ; i++ {
		if _, exists := taken[tag]; !exists {
			return tag
		}
		tag = defaultHTTPClientTag + "-" + strconv.Itoa(i)
	}
}

// hasImplicitHTTPClientRuleSet reports whether any remote rule-set would fall
// back to the implicit default HTTP client.
func hasImplicitHTTPClientRuleSet(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var ruleSets []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &ruleSets); err != nil {
		return false
	}
	for _, ruleSet := range ruleSets {
		var ruleSetType string
		if err := json.Unmarshal(ruleSet["type"], &ruleSetType); err != nil || ruleSetType != "remote" {
			continue
		}
		if httpClient, ok := ruleSet["http_client"]; !ok || isEmptyRawJSON(httpClient) {
			return true
		}
	}
	return false
}

func isEmptyRawJSON(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", `""`, "{}", "[]":
		return true
	}
	return false
}

func (s *ConfigService) StartCore() error {
	// First, so the five-second watchdog stops at this line while the core is
	// out of service.
	if maintenanceMode.Load() {
		return nil
	}
	if corePtr.IsRunning() {
		return nil
	}
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	if remaining := startCooldown - time.Since(lastStartFailTime); remaining > 0 {
		// Not startCooldown/time.Second: dividing a Duration by a Duration
		// yields a Duration, so that printed "15ns" for a 15s cooldown. And
		// checkCoreJob lands here every 5s, so the useful number is what is
		// left rather than the constant.
		logger.Info("start core in cooldown, retrying in ", remaining.Truncate(time.Second))
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	logger.Info("starting core")
	rawConfig, err := s.GetConfig("")
	if err != nil {
		// A config that cannot even be assembled fails the same way as one the
		// core rejects, and looks identical from outside the panel.
		notify.Publish(notify.Event{Kind: notify.CoreCrash, Data: &notify.CoreData{Err: err.Error()}})
		return err
	}
	up, err := startBox(*rawConfig)
	if err != nil {
		startCoreMu.Lock()
		lastStartFailTime = time.Now()
		startCoreMu.Unlock()
		logger.Error("start sing-box err:", err.Error())
		// checkCoreJob retries every 5s, so this is published on every attempt
		// for as long as the core stays down. The suppressor only lets the
		// first one through, and only lets the next one through after a
		// recovery has been reported in between.
		notify.Publish(notify.Event{Kind: notify.CoreCrash, Data: &notify.CoreData{Err: err.Error()}})
		return err
	}
	if !up {
		return nil
	}
	logger.Info("sing-box started")
	// Reached only on an actual start: the guard at the top of this function
	// returns early when the core is already running, so a healthy panel does
	// not publish this every 5s.
	notify.Publish(notify.Event{Kind: notify.CoreUp})
	return nil
}

// CoreRunning reports whether the embedded sing-box is up. corePtr is package
// state, so callers outside service (the scheduled report) need this.
func (s *ConfigService) CoreRunning() bool {
	return corePtr != nil && corePtr.IsRunning()
}

func (s *ConfigService) RestartCore() error {
	// Reported rather than ignored, unlike StartCore above: every caller of
	// this one is somebody asking for a restart -- the panel button, the bot,
	// the scheduled reset -- and one that quietly left the core down would
	// read as the button being broken.
	if maintenanceMode.Load() {
		return common.NewError("the core is stopped for maintenance")
	}
	err := s.StopCore()
	if err != nil {
		return err
	}
	return s.StartCore()
}

func (s *ConfigService) restartCoreWithConfig(config json.RawMessage) error {
	// The config is saved either way; it takes effect when the core is
	// started again.
	if maintenanceMode.Load() {
		return nil
	}
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	if corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			logger.Error("restart sing-box err (stop):", err.Error())
			return err
		}
	}
	rawConfig, err := s.GetConfig(string(config))
	if err != nil {
		logger.Error("restart sing-box err (get config):", err.Error())
		return err
	}
	up, err := startBox(*rawConfig)
	if err != nil {
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	if !up {
		return nil
	}
	logger.Info("sing-box restarted with new config")
	return nil
}

// startBox starts the core, and undoes it when maintenance was switched on
// while the config was being assembled -- the stop that switch issued found
// nothing running, and the gates above are all it would otherwise pass.
// Assembling a config reads most of the database, so on a busy panel that
// window is wide enough for the five-second watchdog to sit in.
//
// up is false when the start was undone that way. It is not an error -- the
// operator got what they asked for -- but the callers must not go on to log
// a start or publish CoreUp for a core that is down.
func startBox(rawConfig []byte) (up bool, err error) {
	if err = corePtr.Start(rawConfig); err != nil {
		return false, err
	}
	if maintenanceMode.Load() {
		logger.Warning("maintenance switched on mid-start: stopping the core again")
		if stopErr := corePtr.Stop(); stopErr != nil {
			logger.Error("stop core after a mid-start maintenance switch:", stopErr.Error())
		}
		return false, nil
	}
	return true, nil
}

func (s *ConfigService) StopCore() error {
	err := corePtr.Stop()
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
	return nil
}

// TestAcme attempts to obtain a certificate for the domain right now, so the UI
// can verify ACME actually works (domain resolves, port 80 reachable, etc.)
// BEFORE the user commits the setting. On success the certificate is cached, so
// the subsequent panel restart serves HTTPS without another challenge.
func (s *ConfigService) TestAcme(domain, email string) error {
	if domain == "" {
		return common.NewError("domain is required for ACME")
	}
	_, err := network.ACMETLSConfig(domain, email, config.GetCertFolderPath())
	return err
}

func (s *ConfigService) CheckOutbound(tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	if corePtr == nil {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	// The running check moved inside: asking here and dereferencing there is
	// the pattern that hands out a box a restart has already replaced.
	return corePtr.CheckOutbound(tag, link)
}

// SaveResult describes what a save wrote -- never what a caller should read
// next. The panel used to get a list of tables to refresh back from here, which
// is how a write ended up answering with the whole client list: a view concern
// decided a service return value. Refreshing is api/load's job now.
//
// Ids covers the primary object only. A tls edit also rewrites client links and
// an inbound edit touches clients, but naming every knock-on row would make this
// a change feed, and nothing needs one -- the panel reloads, and an integrator
// saving a certificate is not waiting to hear which links moved.
type SaveResult struct {
	Object string `json:"object"`
	Action string `json:"action"`
	// Rows written, or removed for a delete. Only clients reports these today;
	// the other services would each need their own id plumbing.
	Ids []uint `json:"ids,omitempty"`
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) (*SaveResult, error) {
	var err error
	var savedIds []uint
	// Set once the change row is in; the deferred commit is what publishes it.
	var dt int64
	// Set when the base config changed. The restart waits for the commit:
	// launched from inside the transaction, a later failure -- the changes
	// row, or anything after it -- rolls the write back while the core is
	// already running a config that was never saved.
	var restartWith json.RawMessage

	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			// Marks the change and wakes the hub, and both halves have to
			// happen after the commit. The hub reads on its own pooled
			// connection, so notifying earlier publishes a pre-commit view.
			// The mark is also what invalidates the config cache, and the DB
			// runs in WAL -- a reader racing the commit sees the old snapshot
			// rather than blocking -- so marking earlier let such a reader
			// cache the pre-change config under the post-change key and serve
			// it for the whole TTL, outliving the push that would repair it
			// (same entry, same cseq, so the SPA drops it as not newer).
			SetLastUpdate(dt)
			if restartWith != nil {
				go func() { _ = s.restartCoreWithConfig(restartWith) }()
			} else if !corePtr.IsRunning() {
				// Try to start core if it is not running
				s.StartCore()
			}
		} else {
			tx.Rollback()
		}
	}()

	switch obj {
	case "clients":
		var write *ClientWrite
		write, err = s.ClientService.Save(tx, act, data, hostname)
		if err == nil {
			savedIds = write.Ids
			if len(write.InboundIds) > 0 {
				err = s.InboundService.UpdateInboundsUsers(tx, write.InboundIds)
				if err != nil {
					return nil, common.NewErrorf("failed to update users for inbounds: %v", err)
				}
			}
		}
	case "tls":
		err = s.TlsService.Save(tx, act, data, hostname)
	case "inbounds":
		err = s.InboundService.Save(tx, act, data, initUsers, hostname)
	case "outbounds":
		err = s.OutboundService.Save(tx, act, data)
	case "services":
		err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		err = s.EndpointService.Save(tx, act, data)
	case "config":
		err = s.SettingService.SaveConfig(tx, data)
		if err != nil {
			return nil, err
		}
		configData := make(json.RawMessage, len(data))
		copy(configData, data)
		restartWith = configData
	case "settings":
		err = s.SettingService.Save(tx, data)
	case "nodes":
		// Nodes are a panel-local concept: no corePtr involvement, so saving
		// one never disturbs the running sing-box.
		err = s.NodeService.Save(tx, act, data)
		if err == nil {
			data = redactNodeToken(data)
		}
	default:
		// Assign the named err: the deferred closure keys off it, and a fresh
		// return value would leave it nil — committing the (empty) txn, waking
		// the hub, and even starting a stopped core for a failed request.
		err = common.NewError("unknown object: ", obj)
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	dt = time.Now().Unix()
	err = tx.Create(&model.Changes{
		DateTime: dt,
		Actor:    loginUser,
		Key:      obj,
		Action:   act,
		Obj:      data,
	}).Error
	if err != nil {
		return nil, err
	}

	return &SaveResult{Object: obj, Action: act, Ids: savedIds}, nil
}

// SetLastUpdate records a config-change timestamp and wakes the websocket
// hub's debounced full-payload push. CheckChanges' lazy seeding below must NOT
// go through it — that is a cache warm-up after a restart, not a change.
//
// Only call this OUTSIDE a write transaction. The hub reads the DB on its own
// pooled connection, so notifying before the commit lands publishes pre-commit
// state, and since the client stamps its own lastLoad from that push, even a
// reconnect's lu gate then reports "unchanged" — the stale config sticks.
// Inside a transaction use MarkLastUpdate and call NotifyConfigChanged after
// the commit.
func SetLastUpdate(dt int64) {
	MarkLastUpdate(dt)
	NotifyConfigChanged()
}

// MarkLastUpdate advances the change timestamp without waking the hub.
func MarkLastUpdate(dt int64) {
	lastUpdate.Store(dt)
	lastUpdateSeq.Add(1)
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	intLu, err := strconv.ParseInt(lu, 10, 64)
	if err != nil {
		return false, err
	}
	// One load, then decide: reading the var twice let the gate branch on one
	// value and answer with another.
	cur := lastUpdate.Load()
	if cur == 0 {
		db := database.GetDB()
		var count int64
		err := db.Model(model.Changes{}).Where("date_time > ?", intLu).Count(&count).Error
		if err == nil {
			// Cache warm-up after a restart, not a change — deliberately not
			// SetLastUpdate, which would wake the hub for news that isn't news.
			lastUpdate.Store(time.Now().Unix())
		}
		return count > 0, err
	}
	return cur > intLu, nil
}

func (s *ConfigService) GetChanges(actor string, chngKey string, count string) []model.Changes {
	c, _ := strconv.Atoi(count)
	db := database.GetDB()
	tx := db.Model(model.Changes{}).Where("`id` > 0")
	if len(actor) > 0 {
		tx = tx.Where("`actor` = ?", actor)
	}
	if len(chngKey) > 0 {
		tx = tx.Where("`key` = ?", chngKey)
	}
	var chngs []model.Changes
	err := tx.Order("`id` desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}
