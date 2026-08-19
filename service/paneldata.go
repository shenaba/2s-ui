package service

import (
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// PanelDataService assembles the api/load payloads. It exists so the HTTP
// handler and the websocket hub share one implementation; all embedded
// services are stateless, so the zero value is ready to use.
type PanelDataService struct {
	SettingService
	ClientService
	TlsService
	InboundService
	OutboundService
	EndpointService
	ServicesService
	NodeService
	StatsService
	ServerService
}

// OnlinesPayload is the per-flush live data: which tags are online, plus the
// newest core log line when sing-box is down (the UI surfaces it as a toast).
// Callers that run on the StatsJob goroutine right after SaveStats get a value
// snapshot of onlineResources before anything crosses a goroutine boundary.
func (s *PanelDataService) OnlinesPayload() (map[string]interface{}, error) {
	data := make(map[string]interface{})
	onlines, err := s.StatsService.GetOnlines()

	// Ask the core directly rather than via GetSingboxInfo: that one opens with
	// a stop-the-world runtime.ReadMemStats, and sing-box runs in-process, so
	// on the 10s push path the pause would hit the data plane for one boolean.
	if corePtr == nil || !corePtr.IsRunning() {
		logs := s.ServerService.GetLogs("1", "debug")
		if len(logs) > 0 {
			data["lastLog"] = logs[0]
		}
	}

	if err != nil {
		return nil, err
	}
	data["onlines"] = onlines
	// Always sent, even empty. The frontend treats a missing key as "unchanged",
	// so omitting it once nobody is over their limit would leave the last
	// non-empty counts on screen forever.
	data["ipCounts"] = GetIPCounts()
	// Client up/down is rewritten by every stats flush, which does not mark
	// LastUpdate -- so it rides the live payload rather than waiting for a
	// config push that may never come on a panel nobody is editing. Sending it
	// here rather than only in the config half is what keeps the traffic
	// columns, quota bars and per-client totals moving; FullPayload overwrites
	// the key with the identical list out of its config half.
	//
	// Versioned off the same counter the config half uses, and allocated BEFORE
	// the read for the same reason: the two payload kinds are built on
	// different goroutines, so a list read here can still be enqueued after a
	// full payload built later, and applying it would put pre-change rows back
	// on screen. One shared counter gives both kinds a total order, so the
	// client can always tell which list was read last.
	clientsSeq := configSeq.Add(1)
	clients, err := s.ClientService.GetAll()
	if err != nil {
		return nil, err
	}
	data["clients"] = clients
	data["clientsSeq"] = clientsSeq
	return data, nil
}

// LivePayload is OnlinesPayload plus live node status — api/load's response
// when nothing changed since the client's lu.
func (s *PanelDataService) LivePayload() (map[string]interface{}, error) {
	data, err := s.OnlinesPayload()
	if err != nil {
		return nil, err
	}
	s.attachNodesStatus(data)
	return data, nil
}

// The config half of a full payload costs ~10 queries and is rebuilt far more
// often than it changes: once per reconnect, once per api/load that arrives
// without an lu, and once per distinct hostname on every config push. Caching
// it against the change timestamp collapses a burst of those into one build.
//
// Invalidation is the timestamp, not the clock: every write path bumps
// LastUpdate, so a change is visible on the next call. The TTL is only a
// backstop -- if some future write forgets to bump, staleness is bounded by it
// instead of being permanent. Deliberately a single entry: hostname varies only
// with how the panel is reached, and an unbounded map would be attacker-growable
// through the Host header wherever DomainValidator is not pinning it.
const configCacheTTL = 2 * time.Second

var configCache struct {
	mu       sync.Mutex
	valid    bool
	hostname string
	luKey    int64  // the LastUpdate this entry was built against
	stamp    int64  // the lu served alongside it
	seq      uint64 // the config version served alongside it
	builtAt  time.Time
	data     map[string]interface{}
}

// configSeq versions every config payload so a client can tell which of two it
// read later, which is what lets the hub add a subscriber to the broadcast set
// BEFORE building its snapshot: a push that lands mid-build carries a higher
// version and the older snapshot is then discarded rather than applied over it.
//
// Seeded from the wall clock so it keeps rising across restarts. A counter
// starting at zero would make every payload after a restart look older than
// what open tabs had already applied, and they would ignore all of them.
var configSeq atomic.Uint64

func init() {
	configSeq.Store(uint64(time.Now().UnixMilli()))
}

// configCacheUsable is the whole staleness decision, kept pure so it can be
// verified without a database. Serving a stale config is the one way this cache
// can break the panel, so every reason to rebuild is spelled out here.
func configCacheUsable(valid bool, cachedHost, host string, cachedLu, curLu int64, age time.Duration) bool {
	if !valid {
		return false
	}
	if cachedHost != host {
		// subURI is derived from the hostname, so an entry built for one is
		// wrong for another.
		return false
	}
	if cachedLu != curLu {
		return false // a write landed
	}
	return age >= 0 && age < configCacheTTL
}

// configHalf returns the cacheable part of a full payload plus the lu stamp and
// config version that belong with it. The returned map is shared and must not
// be mutated -- callers copy out of it.
func (s *PanelDataService) configHalf(hostname string) (map[string]interface{}, int64, uint64, error) {
	cur := lastUpdate.Load()
	configCache.mu.Lock()
	defer configCache.mu.Unlock()
	if configCacheUsable(configCache.valid, configCache.hostname, hostname,
		configCache.luKey, cur, time.Since(configCache.builtAt)) {
		return configCache.data, configCache.stamp, configCache.seq, nil
	}

	// Stamped BEFORE the reads: a change committing during the build is then
	// strictly newer than the stamp, so the next gate still reports it. It has
	// to come from the server because lu is compared against the server's own
	// change timestamp -- a client deriving it from its own clock either misses
	// changes (clock ahead) or refetches the whole config on every reconnect
	// (clock behind). A cache hit reuses the older stamp on purpose: it means
	// nothing changed since, so the earlier value is the conservative one.
	stamp := time.Now().Unix()
	// Allocated before the reads for the same reason as the stamp: it must
	// order this payload against one built from a later read.
	seq := configSeq.Add(1)
	data := make(map[string]interface{}, 11)
	config, err := s.SettingService.GetConfig()
	if err != nil {
		return nil, 0, 0, err
	}
	clients, err := s.ClientService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	tlsConfigs, err := s.TlsService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	inbounds, err := s.InboundService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	outbounds, err := s.OutboundService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	endpoints, err := s.EndpointService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	services, err := s.ServicesService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	subURI, err := s.SettingService.GetFinalSubURI(hostname)
	if err != nil {
		return nil, 0, 0, err
	}
	trafficAge, err := s.SettingService.GetTrafficAge()
	if err != nil {
		return nil, 0, 0, err
	}
	nodes, err := s.NodeService.GetAll()
	if err != nil {
		return nil, 0, 0, err
	}
	data["config"] = json.RawMessage(config)
	data["clients"] = clients
	data["tls"] = tlsConfigs
	data["inbounds"] = inbounds
	data["outbounds"] = outbounds
	data["endpoints"] = endpoints
	data["services"] = services
	data["nodes"] = nodes
	data["subURI"] = subURI
	data["enableTraffic"] = trafficAge > 0
	data["os"] = runtime.GOOS

	configCache.valid = true
	configCache.hostname = hostname
	configCache.luKey = cur
	configCache.stamp = stamp
	configCache.seq = seq
	configCache.builtAt = time.Now()
	configCache.data = data
	return data, stamp, seq, nil
}

// FullPayload is LivePayload plus the whole panel config — api/load's response
// when the lu gate opens. hostname feeds the subscription-URI fallback.
func (s *PanelDataService) FullPayload(hostname string) (map[string]interface{}, error) {
	cfg, stamp, seq, err := s.configHalf(hostname)
	if err != nil {
		return nil, err
	}
	// The live half is never cached — onlines and the core's last log move on
	// their own schedule, not the config's.
	data, err := s.OnlinesPayload()
	if err != nil {
		return nil, err
	}
	for k, v := range cfg {
		data[k] = v
	}
	data["lu"] = stamp
	data["cseq"] = seq
	// The spread above replaced the live half's client list with the config
	// half's, so the version has to follow it down -- keeping the live one
	// would claim a newer read than the rows actually carry and make the next
	// live push look stale.
	data["clientsSeq"] = seq
	s.attachNodesStatus(data)
	return data, nil
}

// attachNodesStatus rides live node status outside the lu gate (it changes
// every heartbeat); the key is omitted when empty so zero-node setups pay
// nothing.
func (s *PanelDataService) attachNodesStatus(data map[string]interface{}) {
	nodesStatus := s.NodeService.GetStatuses()
	if len(nodesStatus) > 0 {
		data["nodesStatus"] = nodesStatus
	}
}
