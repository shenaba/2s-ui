package service

import (
	"encoding/json"
	"runtime"
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

// FullPayload is LivePayload plus the whole panel config — api/load's response
// when the lu gate opens. hostname feeds the subscription-URI fallback.
func (s *PanelDataService) FullPayload(hostname string) (map[string]interface{}, error) {
	// The client's next lu, stamped BEFORE the reads below: a change committing
	// during the build is then strictly newer than the stamp, so the next gate
	// still reports it. It has to come from here because lu is compared against
	// the server's own change timestamp — a client deriving it from its own
	// clock either misses changes (clock ahead) or refetches the whole config on
	// every reconnect (clock behind).
	stamp := time.Now().Unix()
	data, err := s.OnlinesPayload()
	if err != nil {
		return nil, err
	}
	config, err := s.SettingService.GetConfig()
	if err != nil {
		return nil, err
	}
	clients, err := s.ClientService.GetAll()
	if err != nil {
		return nil, err
	}
	tlsConfigs, err := s.TlsService.GetAll()
	if err != nil {
		return nil, err
	}
	inbounds, err := s.InboundService.GetAll()
	if err != nil {
		return nil, err
	}
	outbounds, err := s.OutboundService.GetAll()
	if err != nil {
		return nil, err
	}
	endpoints, err := s.EndpointService.GetAll()
	if err != nil {
		return nil, err
	}
	services, err := s.ServicesService.GetAll()
	if err != nil {
		return nil, err
	}
	subURI, err := s.SettingService.GetFinalSubURI(hostname)
	if err != nil {
		return nil, err
	}
	trafficAge, err := s.SettingService.GetTrafficAge()
	if err != nil {
		return nil, err
	}
	nodes, err := s.NodeService.GetAll()
	if err != nil {
		return nil, err
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
	data["lu"] = stamp
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
