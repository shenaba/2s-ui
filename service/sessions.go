package service

import (
	"encoding/json"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// SessionsResult is a live-connections listing. Errors names the nodes that
// could not be asked, so a partial list is never mistaken for a complete one.
type SessionsResult struct {
	Sessions []core.SessionInfo `json:"sessions"`
	Errors   map[string]string  `json:"errors,omitempty"`
}

// LocalSessions lists this panel's own live routed connections, narrowed to
// one user, inbound or outbound (an empty tag means all of them). Newest first,
// so a connection that was just opened is at the top.
func LocalSessions(resource string, tag string) ([]core.SessionInfo, error) {
	var want func(info *core.ConnectionInfo) bool
	if tag != "" {
		switch resource {
		case "user":
			want = func(info *core.ConnectionInfo) bool { return info.User == tag }
		case "inbound":
			want = func(info *core.ConnectionInfo) bool { return info.Inbound == tag }
		case "outbound":
			want = func(info *core.ConnectionInfo) bool { return info.Outbound == tag }
		default:
			return nil, common.NewError("unknown resource: ", resource)
		}
	}
	tracker := liveConnTracker()
	if tracker == nil {
		return []core.SessionInfo{}, nil
	}
	sessions := tracker.Sessions(want)
	sortSessions(sessions)
	return sessions, nil
}

func sortSessions(sessions []core.SessionInfo) {
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt > sessions[j].CreatedAt
	})
}

// ClusterUserSessions is LocalSessions for one client, plus the connections it
// holds on any node it is currently online on.
//
// Only a user is merged, never an inbound or outbound: those tags name objects
// on this panel, and a node's inbound of the same tag is a different listener.
//
// The nodes asked are the ones whose last probe listed the client as online.
// That is what made the panel show it online in the first place, so a node it
// is not on costs no request -- and a client online on no node costs none.
func ClusterUserSessions(user string) (SessionsResult, error) {
	local, err := LocalSessions("user", user)
	if err != nil {
		return SessionsResult{}, err
	}
	result := SessionsResult{Sessions: local}
	if user == "" {
		return result, nil
	}

	nodes, err := nodesWithOnlineUser(user)
	if err != nil {
		return SessionsResult{}, err
	}
	if len(nodes) == 0 {
		return result, nil
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	var ns NodeService
	q := url.Values{"resource": {"user"}, "tag": {user}}
	for _, n := range nodes {
		wg.Add(1)
		go func(n *model.Node) {
			defer wg.Done()
			sessions, err := fetchNodeSessions(&ns, n, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if result.Errors == nil {
					result.Errors = map[string]string{}
				}
				result.Errors[n.Name] = err.Error()
				return
			}
			for i := range sessions {
				sessions[i].Node = n.Name
			}
			result.Sessions = append(result.Sessions, sessions...)
		}(n)
	}
	wg.Wait()
	sortSessions(result.Sessions)
	return result, nil
}

// fetchNodeSessions asks one node for its list. The pooled client already
// carries nodeProbeTimeout, so a node slow to answer costs its own rows, not
// the whole modal.
func fetchNodeSessions(ns *NodeService, n *model.Node, q url.Values) ([]core.SessionInfo, error) {
	raw, err := ns.nodeGet(n, nodeHTTPClient(n), "sessions", q)
	if err != nil {
		return nil, err
	}
	var listed SessionsResult
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, common.NewError("unexpected response from node")
	}
	return listed.Sessions, nil
}

// nodesWithOnlineUser returns the enabled nodes whose fresh probe lists user.
func nodesWithOnlineUser(user string) ([]*model.Node, error) {
	now := time.Now().Unix()
	var ids []uint
	nodeStatusMu.RLock()
	for id, status := range nodeStatuses {
		if status.State != "online" || status.onlineCheckedAt == 0 ||
			now-status.onlineCheckedAt > int64(nodeOnlineTTL.Seconds()) {
			continue
		}
		for _, name := range status.onlineUsers {
			if name == user {
				ids = append(ids, id)
				break
			}
		}
	}
	nodeStatusMu.RUnlock()
	if len(ids) == 0 {
		return nil, nil
	}
	var nodes []*model.Node
	err := database.GetDB().Model(model.Node{}).Where("id IN ? AND enable = ?", ids, true).Find(&nodes).Error
	return nodes, err
}

// DisconnectResult adds the nodes that could not be told to the local result.
type DisconnectResult struct {
	core.DisconnectResult
	Errors map[string]string `json:"errors,omitempty"`
}

// DisconnectLocalUser drops one user's live connections on this panel only.
func DisconnectLocalUser(user string) (core.DisconnectResult, error) {
	if user == "" {
		return core.DisconnectResult{}, common.NewError("empty user name")
	}
	if corePtr == nil {
		return core.DisconnectResult{}, common.NewError("sing-box is not running")
	}
	return corePtr.DisconnectUser(user)
}

// DisconnectClusterUser drops a user's live connections here and on every node
// it is online on. The user stays enabled everywhere, so it can reconnect at
// once -- this is a kick, not a ban.
//
// A core that is not running here is not an error when nodes are involved:
// the client can perfectly well be online on a node only.
func DisconnectClusterUser(user string) (DisconnectResult, error) {
	if user == "" {
		return DisconnectResult{}, common.NewError("empty user name")
	}
	var result DisconnectResult
	localErr := error(nil)
	if corePtr != nil && corePtr.IsRunning() {
		local, err := corePtr.DisconnectUser(user)
		result.DisconnectResult = local
		localErr = err
	}

	nodes, err := nodesWithOnlineUser(user)
	if err != nil {
		return result, err
	}
	if len(nodes) == 0 {
		return result, localErr
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	var ss NodeSyncService
	form := url.Values{"u": {user}}
	for _, n := range nodes {
		wg.Add(1)
		go func(n *model.Node) {
			defer wg.Done()
			raw, err := ss.nodePost(n, nodeHTTPClient(n), "closeSessions", form)
			var remote core.DisconnectResult
			if err == nil {
				err = json.Unmarshal(raw, &remote)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if result.Errors == nil {
					result.Errors = map[string]string{}
				}
				result.Errors[n.Name] = err.Error()
				return
			}
			result.Connections += remote.Connections
			result.Sessions += remote.Sessions
			result.Unclosable += remote.Unclosable
		}(n)
	}
	wg.Wait()
	if localErr != nil {
		logger.Debug("disconnect ", user, ": local core: ", localErr)
	}
	return result, nil
}
