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
	lastUpdate    atomic.Int64
	lastUpdateSeq atomic.Int64
	corePtr       *core.Core
	// startMu's write side serializes everything that stops or starts the
	// core, and guards lastStartFailTime. The watchdog's StartCore only tries
	// it -- a start already under way is all it wanted -- but a restart waits
	// for it. It used to be a flag every path skipped on, so a restart for a
	// change committed while the watchdog was starting the core gave up, and
	// the core stayed on the config that start had read before the commit; and
	// RestartCore and StopCore did not take it at all, so a restart button
	// pressed during another restart could stop the core that one had just
	// started.
	//
	// Its read side is held by every write that also changes the running core
	// (HoldCore): ConfigService.Save for the whole save, and the jobs that
	// change users outside it. Without that, a write landing mid-restart found
	// the core "not running", skipped the hot change, and -- committed after
	// the restart had read its config -- was missing from the new core until
	// the next restart: a client added then could not connect, and one
	// depleted then kept its access.
	startMu sync.RWMutex
	// restartQueued is set while a requested restart waits for startMu, and
	// queuedRestarts counts the ones that ran; see requestRestart.
	restartQueued  atomic.Bool
	queuedRestarts atomic.Int64
	// coreRetired is set while the panel is not holding the core: from
	// app.Stop until app.Start takes it back, and across the exec of an
	// in-place update. See RetireCore and ReclaimCore.
	coreRetired       atomic.Bool
	lastStartFailTime time.Time
	startCooldown     = 15 * time.Second
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
	rawConfig, err := json.MarshalIndent(singboxConfig, "", "  ")
	if err != nil {
		return nil, err
	}
	return &rawConfig, nil
}

// validateBaseConfig refuses a base config the core could not parse (see
// Core.ValidateConfig for what that does not cover). The save restarts the core
// after the commit, and a config sing-box cannot parse left it down -- retried
// by the watchdog every few seconds -- until someone found the field: a
// memory_limit of "512mb512mb", a gc_percent of 80.5.
//
// Only the base is judged, shaped the way GetConfig shapes it (keys
// SingBoxConfig does not list are dropped there too) and with the tables left
// out. Parsing checks each field on its own, so the tables add nothing for the
// base -- but one stored row the core cannot read would refuse every base save,
// including the log level an operator raises to find out why the core is down.
//
// Anything but a JSON object is refused first: an empty body used to be stored
// as null, a restart with no log, dns or route.
func (s *ConfigService) validateBaseConfig(data json.RawMessage) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return common.NewErrorf("invalid config: the base config must be a JSON object")
	}
	var base SingBoxConfig
	if err := json.Unmarshal(data, &base); err != nil {
		return common.NewErrorf("invalid config: %v", err)
	}
	// Replaced from the tables on every start.
	base.Inbounds, base.Outbounds, base.Services, base.Endpoints = nil, nil, nil, nil
	rawConfig, err := json.Marshal(base)
	if err != nil {
		return err
	}
	if err = corePtr.ValidateConfig(rawConfig); err != nil {
		return common.NewErrorf("invalid config: %v", err)
	}
	return nil
}

// checkRuleSetDownloads refuses a base config that leaves a remote rule-set on
// either download form sing-box 1.14 deprecated and 1.16 removes: the legacy
// download_detour, or no client at all with no default client declared, which
// falls back to the implicit client riding the default outbound.
//
// The panel used to cover the second case when generating the config, by
// declaring a client on route.final. That only reproduced the deprecated
// behaviour under another name, and not when route.final was unset. Stored
// configs were made explicit once (database.migrateExplicitRuleSetHTTPClients);
// this keeps every later save, the API's included, from putting either back.
//
// A config the panel cannot read is not judged here: validateBaseConfig, which
// Save runs right after this, refuses it.
func checkRuleSetDownloads(data json.RawMessage) error {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	if tags := util.LegacyDownloadDetourRuleSets(root); len(tags) > 0 {
		return common.NewErrorf("rule-set %s: download_detour is deprecated since sing-box 1.14, use http_client instead", quoteTags(tags))
	}
	if tags := util.ImplicitHTTPClientRuleSets(root); len(tags) > 0 {
		return common.NewErrorf("rule-set %s: no http_client and no default HTTP client to fall back on; sing-box 1.14 deprecated the implicit one, so choose how it downloads", quoteTags(tags))
	}
	return nil
}

func quoteTags(tags []string) string {
	quoted := ""
	for i, tag := range tags {
		if i > 0 {
			quoted += ", "
		}
		quoted += strconv.Quote(tag)
	}
	return quoted
}

// StartCore starts the core if it is down, waiting for whatever holds startMu:
// a restart that is under way, or a save. Waiting matters since saves hold its
// read side -- trying instead let a save in progress turn "maintenance off"
// into a success that started nothing.
func (s *ConfigService) StartCore() error {
	return s.startCore(false)
}

// TryStartCore is StartCore for the five-second watchdog, which gives up when
// startMu is held: a start already under way is all it wanted, and a save will
// be over by its next tick.
func (s *ConfigService) TryStartCore() error {
	return s.startCore(true)
}

func (s *ConfigService) startCore(try bool) error {
	// First, so the five-second watchdog stops at this line while the core is
	// out of service.
	if maintenanceMode.Load() {
		return nil
	}
	if corePtr.IsRunning() {
		return nil
	}
	if try {
		if !startMu.TryLock() {
			return nil
		}
	} else {
		startMu.Lock()
	}
	defer startMu.Unlock()
	// A restart may have brought it up between the check above and the lock.
	if corePtr.IsRunning() {
		return nil
	}
	if remaining := startCooldown - time.Since(lastStartFailTime); remaining > 0 {
		// Not startCooldown/time.Second: dividing a Duration by a Duration
		// yields a Duration, so that printed "15ns" for a 15s cooldown. And
		// checkCoreJob lands here every 5s, so the useful number is what is
		// left rather than the constant.
		logger.Info("start core in cooldown, retrying in ", remaining.Truncate(time.Second))
		return nil
	}
	return s.startLocked(true)
}

// startLocked starts the core from the stored config. The caller holds
// startMu. A failure is recorded for the watchdog's cooldown and published;
// announce publishes CoreUp on success.
func (s *ConfigService) startLocked(announce bool) error {
	// Every start comes through here holding startMu, and RetireCore sets the
	// flag before it takes startMu to stop the core: a start that got in first
	// is stopped by it, and one that comes after stops here.
	if coreRetired.Load() {
		logger.Info("not starting core: the panel is done with it")
		return nil
	}
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
		lastStartFailTime = time.Now()
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
	if announce {
		notify.Publish(notify.Event{Kind: notify.CoreUp})
	}
	return nil
}

// stopLocked stops the core, first leaving what it has counted since the last
// stats flush for the next one (carryOverStats). The caller holds startMu.
//
// The core is down even when this returns an error: Core.Stop drops the box
// before closing it, and the error is what some component's cleanup reported.
// So a restart logs it and starts anyway -- returning there left the core down
// until the watchdog came round.
func stopLocked() error {
	carryOverStats()
	if err := corePtr.Stop(); err != nil {
		return err
	}
	logger.Info("sing-box stopped")
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
	startMu.Lock()
	defer startMu.Unlock()
	// Same reason, and under the lock because that is where RetireCore's stop
	// is ordered: startLocked refuses a retired core by returning nil, which
	// here turned a restart that started nothing into a success (#251).
	if coreRetired.Load() {
		return common.NewError("the panel is restarting or shutting down: the core comes back with it")
	}
	if err := stopLocked(); err != nil {
		logger.Warning("restart sing-box (stop): ", err)
	}
	return s.startLocked(true)
}

// HoldCore runs fn holding off any restart: none begins while fn runs, and fn
// does not begin while one is under way. For a write that is applied to the
// running core as well -- see startMu. fn must not start or stop the core.
func HoldCore(fn func()) {
	startMu.RLock()
	defer startMu.RUnlock()
	fn()
}

// ResetTraffic runs reset -- a global traffic reset, which re-enables depleted
// clients and returns the inbounds they are on -- and brings the running core
// up to date with it. In place where it can be, so the reset does not drop
// every connection on the panel; and under HoldCore, so a restart cannot come
// between the two and leave the re-enabled clients out of the new core.
//
// UpdateInboundsUsers stops at the first inbound it cannot update, leaving the
// rest on their old user lists: the re-enabled clients would stay rejected
// with nothing to retry it, because the reset is already committed. A full
// restart is the fallback that cannot leave part of the core stale -- and if
// that fails too, the core is down, which the watchdog does recover.
//
// resetErr is reset's own, and means nothing changed; coreErr is a core the
// fallback restart could not bring up.
func (s *ConfigService) ResetTraffic(reset func() ([]uint, error)) (resetErr error, coreErr error) {
	var updateErr error
	HoldCore(func() {
		var inboundIds []uint
		inboundIds, resetErr = reset()
		if resetErr == nil && len(inboundIds) > 0 {
			updateErr = s.InboundService.UpdateInboundsUsers(database.GetDB(), inboundIds)
		}
	})
	if resetErr != nil || updateErr == nil {
		return resetErr, nil
	}
	logger.Warning("in-place user update after the traffic reset failed, restarting the core: ", updateErr)
	return nil, s.RestartCore()
}

// requestRestart restarts the core, off the caller's goroutine, for a change
// already committed. A request arriving while an earlier one still waits for
// startMu is folded into it: that one reads the config only once it holds the
// lock, so it carries the later change too. Editing five members of a group in
// a row costs one restart, not five back to back.
func (s *ConfigService) requestRestart() {
	if restartQueued.Swap(true) {
		return
	}
	go func() { _ = s.restartQueuedCore() }()
}

func (s *ConfigService) restartQueuedCore() error {
	// The config is saved either way; it takes effect when the core is
	// started again.
	if maintenanceMode.Load() {
		restartQueued.Store(false)
		return nil
	}
	startMu.Lock()
	defer startMu.Unlock()
	// From here on a request needs a restart of its own: this one may already
	// have read the config by the time that request's change commits.
	restartQueued.Store(false)
	queuedRestarts.Add(1)
	if err := stopLocked(); err != nil {
		logger.Warning("restart sing-box (stop): ", err)
	}
	return s.startLocked(false)
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

// RetireCore stops the core and keeps it down until ReclaimCore: for app.Stop,
// and for the exec of an in-place update.
//
// StopCore alone was not enough. A restart queued by a save just before
// (requestRestart), or a watchdog tick already under way -- cron.Stop does not
// wait for one -- could take startMu after the stop and start the core again.
// On a shutdown that left it running into the process exit, and before an
// exec it carried the core's auto-route rules into the new process, which is
// what stopping it first is for: those rules are not tied to a descriptor, so
// neither the exit nor the exec reclaims them.
func (s *ConfigService) RetireCore() error {
	coreRetired.Store(true)
	return s.StopCore()
}

// ReclaimCore undoes RetireCore: from here on the core may be started again.
// It starts nothing itself: under maintenance the core has to stay down -- and
// that is also why it cannot wait for app.Start's own StartCore, since
// switching maintenance off later goes through StartCore, which starts nothing
// while the core is retired.
//
// app.Start calls it first, before anything that can fail or start the core,
// because a SIGHUP restarts the panel as app.Stop then app.Start on the same
// Core: Init, and with it NewConfigService, never runs again. In 1.9.1 nothing
// else took it back (#251), so every panel restart -- api/restartApp, which
// the settings page calls after any save that needs it, the restart button,
// a database import -- left the core down for the life of the process, with
// the watchdog logging every five seconds that it would not start it.
func (s *ConfigService) ReclaimCore() {
	coreRetired.Store(false)
}

func (s *ConfigService) StopCore() error {
	startMu.Lock()
	defer startMu.Unlock()
	return stopLocked()
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
	// Set when the base config changed, or a change reached an outbound or
	// endpoint a group depends on, which cannot be swapped in place. The
	// restart waits for the commit: launched from inside the transaction, a
	// later failure -- the changes row, or anything after it -- rolls the
	// write back while the core is already running a config that was never
	// saved.
	var restartCore bool

	// Judged before the transaction: neither needs it, and the write lock BEGIN
	// takes would hold the stats flush and every other save behind them.
	if obj == "config" {
		if err = checkRuleSetDownloads(data); err != nil {
			return nil, err
		}
		if err = s.validateBaseConfig(data); err != nil {
			return nil, err
		}
	}

	// See HoldCore. Released before the restart below, which needs the write
	// side, and before StartCore, which only tries it.
	startMu.RLock()
	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err != nil {
			tx.Rollback()
			startMu.RUnlock()
			// The core may have taken the inbound and been put back.
			if obj == "inbounds" {
				ApplyProxyProtocol()
			}
			return
		}
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
		startMu.RUnlock()
		if obj == "settings" || obj == "inbounds" {
			ApplyProxyProtocol()
		}
		if restartCore {
			s.requestRestart()
		} else if !corePtr.IsRunning() {
			// Try to start core if it is not running
			s.StartCore()
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
		restartCore, err = s.TlsService.Save(tx, act, data, hostname)
	case "inbounds":
		err = s.InboundService.Save(tx, act, data, initUsers, hostname)
	case "outbounds":
		restartCore, err = s.OutboundService.Save(tx, act, data)
	case "services":
		err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		restartCore, err = s.EndpointService.Save(tx, act, data)
	case "config":
		// Checked above, ahead of the transaction.
		err = s.SettingService.SaveConfig(tx, data)
		if err != nil {
			return nil, err
		}
		restartCore = true
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
