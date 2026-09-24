package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/op/go-logging"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
)

func resetClusterIPState(t *testing.T) {
	t.Helper()
	clusterIPs.mu.Lock()
	previousHolders, previousBans := clusterIPs.holders, clusterIPs.bans
	previousCounts, previousList, previousActive := clusterIPs.counts, clusterIPs.list, clusterIPs.active
	previousUpdatedAt := clusterIPs.updatedAt
	clusterIPs.holders = map[ipBanKey]clusterIPHold{}
	clusterIPs.bans = map[ipBanKey]time.Time{}
	clusterIPs.counts = nil
	clusterIPs.list = nil
	clusterIPs.active = false
	clusterIPs.updatedAt = time.Time{}
	clusterIPs.mu.Unlock()
	ipLimits.mu.Lock()
	previousNodeBans := ipLimits.clusterBans
	previousManaged, previousLease := ipLimits.clusterManaged, ipLimits.clusterLeaseUntil
	ipLimits.clusterBans = nil
	ipLimits.clusterManaged = nil
	ipLimits.clusterLeaseUntil = 0
	ipLimits.mu.Unlock()
	t.Cleanup(func() {
		clusterIPs.mu.Lock()
		clusterIPs.holders, clusterIPs.bans = previousHolders, previousBans
		clusterIPs.counts, clusterIPs.list, clusterIPs.active = previousCounts, previousList, previousActive
		clusterIPs.updatedAt = previousUpdatedAt
		clusterIPs.mu.Unlock()
		ipLimits.mu.Lock()
		ipLimits.clusterBans = previousNodeBans
		ipLimits.clusterManaged, ipLimits.clusterLeaseUntil = previousManaged, previousLease
		ipLimits.mu.Unlock()
	})
}

func TestClusterIPPlannerCountsDistinctIPsAndReleasesVacantSlot(t *testing.T) {
	resetClusterIPState(t)
	now := time.Now()
	limits := map[string]int{"alice": 1}
	bans, err := planClusterIPLimits(map[string][]string{
		"alice": {"1.1.1.1", "2.2.2.2", "1.1.1.1"},
	}, limits, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bans["alice"], []string{"2.2.2.2"}) {
		t.Fatalf("bans = %v, want only 2.2.2.2", bans)
	}
	if got := clusterIPs.counts["alice"]; got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	if got := clusterIPs.list["alice"]; len(got) != 1 || got[0].IP != "1.1.1.1" {
		t.Fatalf("admitted = %v, want 1.1.1.1", got)
	}
	// A dropped address stays banned while the admitted address holds the slot.
	bans, err = planClusterIPLimits(map[string][]string{"alice": {"1.1.1.1"}}, limits, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(bans["alice"]) != 1 {
		t.Fatalf("ban lost while at cap: %v", bans)
	}
	// Once the slot is free, the ban must release so a phone can move networks.
	bans, err = planClusterIPLimits(nil, limits, now.Add(20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(bans) != 0 || clusterIPs.counts["alice"] != 0 {
		t.Fatalf("empty cluster kept bans or count: bans=%v counts=%v", bans, clusterIPs.counts)
	}
	// Two addresses in one IPv6 /64 are one identity across the cluster.
	bans, err = planClusterIPLimits(map[string][]string{
		"alice": {"2001:db8:1::1", "2001:db8:1::2"},
	}, limits, now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(bans) != 0 || clusterIPs.counts["alice"] != 1 {
		t.Fatalf("IPv6 identity counted twice: bans=%v counts=%v", bans, clusterIPs.counts)
	}
}

func TestApplyClusterBansValidatesBeforeReplacingGate(t *testing.T) {
	resetClusterIPState(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "cluster-bans.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	ip := netip.MustParseAddr("2.2.2.2")
	if err := ApplyClusterBans(map[string][]string{"alice": {ip.String()}}); err != nil {
		t.Fatal(err)
	}
	if ipLimits.allow("alice", ip) || !ipLimits.allow("bob", ip) {
		t.Fatal("cluster ban did not target only alice's IP")
	}
	if err := ApplyClusterBans(map[string][]string{"alice": {"invalid"}}); err == nil {
		t.Fatal("invalid IP was accepted")
	}
	if ipLimits.allow("alice", ip) {
		t.Fatal("invalid replacement erased the previous ban")
	}
	if err := ApplyClusterBans(nil); err != nil {
		t.Fatal(err)
	}
	if !ipLimits.allow("alice", ip) {
		t.Fatal("cleared cluster ban still rejects the IP")
	}
}

func TestClusterLeaseOverridesOnlyManagedClientsLocalBans(t *testing.T) {
	resetClusterIPState(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "cluster-lease.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	for _, client := range []model.Client{
		{Name: "managed", Group: clusterGroup, Enable: true, LimitIp: 1},
		{Name: "local", Enable: true, LimitIp: 1},
	} {
		if err := database.GetDB().Create(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	localBanIP := netip.MustParseAddr("1.1.1.1")
	clusterBanIP := netip.MustParseAddr("2.2.2.2")
	ipLimits.mu.Lock()
	previousBans := ipLimits.bans
	ipLimits.bans = map[ipBanKey]ipBan{
		{user: "managed", ip: localBanIP}: {until: time.Now().Add(time.Minute).Unix()},
		{user: "local", ip: localBanIP}:   {until: time.Now().Add(time.Minute).Unix()},
	}
	ipLimits.mu.Unlock()
	t.Cleanup(func() {
		ipLimits.mu.Lock()
		ipLimits.bans = previousBans
		ipLimits.mu.Unlock()
	})
	if err := ApplyClusterBans(map[string][]string{"managed": {clusterBanIP.String()}}); err != nil {
		t.Fatal(err)
	}
	if !ipLimits.allow("managed", localBanIP) || ipLimits.allow("managed", clusterBanIP) {
		t.Fatal("managed client did not follow the cluster decision")
	}
	if ipLimits.allow("local", localBanIP) {
		t.Fatal("cluster lease bypassed a local client's ban")
	}
	ipLimits.mu.Lock()
	ipLimits.clusterLeaseUntil = time.Now().Add(-time.Second).Unix()
	ipLimits.mu.Unlock()
	if ipLimits.allow("managed", localBanIP) {
		t.Fatal("managed client did not fall back to its local ban after lease expiry")
	}
}

func TestClusterIPLimitPushesOneGlobalDecisionToEveryNode(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)
	resetClusterIPState(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "cluster-ips.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()
	if err := db.Create(&model.Client{Name: "alice", Enable: true, LimitIp: 1}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	type fakeNode struct {
		server *httptest.Server
		mu     sync.Mutex
		bans   map[string][]string
	}
	makeNode := func(name, ip string) (model.Node, *fakeNode) {
		t.Helper()
		fake := &fakeNode{}
		fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Token") != "test-token" {
				t.Errorf("%s: token missing", name)
			}
			switch r.URL.Path {
			case "/app/apiv2/clusterIps":
				w.Write([]byte(`{"success":true,"obj":{"alice":["` + ip + `"]}}`))
			case "/app/apiv2/clusterBans":
				var bans map[string][]string
				if err := json.Unmarshal([]byte(r.FormValue("data")), &bans); err != nil {
					t.Errorf("%s: decode bans: %v", name, err)
				}
				fake.mu.Lock()
				fake.bans = bans
				fake.mu.Unlock()
				w.Write([]byte(`{"success":true,"obj":null}`))
			default:
				t.Errorf("%s: unexpected request %s", name, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(fake.server.Close)
		node := model.Node{Name: name, BaseUrl: fake.server.URL, WebPath: "/app/", Token: "test-token", Enable: true}
		if err := db.Create(&node).Error; err != nil {
			t.Fatalf("seed node %s: %v", name, err)
		}
		t.Cleanup(func() { invalidateNodeClient(node.Id) })
		return node, fake
	}
	one, first := makeNode("one", "1.1.1.1")
	two, second := makeNode("two", "2.2.2.2")
	nodeStatusMu.Lock()
	previousStatuses := nodeStatuses
	nodeStatuses = map[uint]NodeStatus{
		one.Id: {State: "online"}, two.Id: {State: "online"},
	}
	nodeStatusMu.Unlock()
	t.Cleanup(func() {
		nodeStatusMu.Lock()
		nodeStatuses = previousStatuses
		nodeStatusMu.Unlock()
	})

	EnforceClusterIPLimits()
	if !ClusterIPActive() || GetIPCounts()["alice"] != 1 {
		t.Fatalf("cluster count not published: active=%v counts=%v", ClusterIPActive(), GetIPCounts())
	}
	for _, fake := range []*fakeNode{first, second} {
		fake.mu.Lock()
		got := fake.bans["alice"]
		fake.mu.Unlock()
		if !reflect.DeepEqual(got, []string{"2.2.2.2"}) {
			t.Errorf("node bans = %v, want 2.2.2.2", got)
		}
	}
	nodeStatusMu.Lock()
	nodeStatuses[two.Id] = NodeStatus{State: "offline"}
	nodeStatusMu.Unlock()
	EnforceClusterIPLimits()
	if ClusterIPActive() {
		t.Fatal("cluster count stayed active with an unavailable node")
	}
}
