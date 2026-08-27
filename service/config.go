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

type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log"`
	Dns          json.RawMessage   `json:"dns"`
	Ntp          json.RawMessage   `json:"ntp"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Services     []json.RawMessage `json:"services"`
	Endpoints    []json.RawMessage `json:"endpoints"`
	Route        json.RawMessage   `json:"route"`
	Experimental json.RawMessage   `json:"experimental"`
}

func NewConfigService(c *core.Core) *ConfigService {
	corePtr = c
	// The gate is read per connection rather than captured by each tracker, so
	// installing it here does not have to be ordered against the first StartCore.
	core.SetConnGate(ipLimits.allow)
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

func (s *ConfigService) StartCore() error {
	if corePtr.IsRunning() {
		return nil
	}
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	if time.Since(lastStartFailTime) < startCooldown {
		logger.Info("start core cooldown ", startCooldown/time.Second, " seconds")
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
	err = corePtr.Start(*rawConfig)
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
	err := s.StopCore()
	if err != nil {
		return err
	}
	return s.StartCore()
}

func (s *ConfigService) restartCoreWithConfig(config json.RawMessage) error {
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
	if err := corePtr.Start(*rawConfig); err != nil {
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	logger.Info("sing-box restarted with new config")
	return nil
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
	if corePtr == nil || !corePtr.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return core.CheckOutbound(corePtr.GetCtx(), tag, link)
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) ([]string, error) {
	var err error
	var objs []string = []string{obj}

	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			// Only now is the write visible on the hub's own connection.
			NotifyConfigChanged()
			// Try to start core if it is not running
			if !corePtr.IsRunning() {
				s.StartCore()
			}
		} else {
			tx.Rollback()
		}
	}()

	switch obj {
	case "clients":
		var inboundIds []uint
		inboundIds, err = s.ClientService.Save(tx, act, data, hostname)
		if err == nil && len(inboundIds) > 0 {
			objs = append(objs, "inbounds")
			err = s.InboundService.UpdateInboundsUsers(tx, inboundIds)
			if err != nil {
				return nil, common.NewErrorf("failed to update users for inbounds: %v", err)
			}
		}
	case "tls":
		err = s.TlsService.Save(tx, act, data, hostname)
		objs = append(objs, "clients", "inbounds")
	case "inbounds":
		err = s.InboundService.Save(tx, act, data, initUsers, hostname)
		objs = append(objs, "clients")
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
		go func() { _ = s.restartCoreWithConfig(configData) }()
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

	dt := time.Now().Unix()
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

	MarkLastUpdate(dt)

	return objs, nil
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
