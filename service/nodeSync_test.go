package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"

	"github.com/op/go-logging"
)

func TestIsNodeOwnedRemark(t *testing.T) {
	nodes := []string{"eu", "eu-2", "us west"}
	tests := []struct {
		name   string
		remark string
		want   bool
	}{
		{"exact node link", "[eu] vless-in", true},
		{"node name with a space", "[us west] tcp", true},
		// The "] " terminator is what keeps one node's prefix from swallowing
		// another whose name starts with it; node names may not contain
		// brackets precisely so this holds.
		{"longer node name is not a prefix match", "[eu-2] vless-in", true},
		{"user link that only looks the part", "[backup] vless-in", false},
		{"bare tag", "vless-in", false},
		{"empty", "", false},
		{"prefix without the separator", "[eu]vless-in", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNodeOwnedRemark(tc.remark, nodes); got != tc.want {
				t.Errorf("isNodeOwnedRemark(%q) = %v, want %v", tc.remark, got, tc.want)
			}
		})
	}
}

// Regression guard: an inbound delete used to strip anything shaped like
// "[...] <tag>", which caught user-authored links too.
func TestIsNodeLinkFor(t *testing.T) {
	nodes := []string{"eu", "us"}
	tests := []struct {
		name   string
		remark string
		tag    string
		want   bool
	}{
		{"node link for this tag", "[eu] vless-in", "vless-in", true},
		{"node link for another tag", "[eu] trojan-in", "vless-in", false},
		{"unknown node", "[backup] vless-in", "vless-in", false},
		{"bare tag is not a node link", "vless-in", "vless-in", false},
		{"tag is a suffix of a longer one", "[eu] my-vless-in", "vless-in", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNodeLinkFor(tc.remark, tc.tag, nodes); got != tc.want {
				t.Errorf("isNodeLinkFor(%q, %q) = %v, want %v", tc.remark, tc.tag, got, tc.want)
			}
		})
	}
}

// expectedClients is the single place that decides what a node receives, and
// nothing else asserts on its output: a field dropped from that map would ship
// silently, leaving the node to enforce a limit it was never told about. That
// makes it worth pinning the payload here rather than only in a live cluster.
func TestExpectedClientsCarriesLimitIp(t *testing.T) {
	// expectedClients reads the package-level handle rather than taking one, so
	// the DB has to be installed globally. CloseDBForTest puts it back on the way
	// out: leaving a closed pool behind the global would fail whatever ran next
	// with "sql: database is closed", from a handle it never set up, and today
	// nothing catches that but the alphabetical order of the test files.
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	// Registered after TempDir's own cleanup, so LIFO runs it first: Windows
	// refuses to delete the file while the pool still holds it open.
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	const nodeId = uint(7)
	node := nodeId
	replica := model.Inbound{
		Type: "vless", Tag: "vless-replica", NodeId: &node,
		Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`), Options: json.RawMessage(`{}`),
	}
	if err := db.Create(&replica).Error; err != nil {
		t.Fatalf("seed replica inbound: %v", err)
	}
	inbounds, err := json.Marshal([]uint{replica.Id})
	if err != nil {
		t.Fatalf("marshal inbounds: %v", err)
	}
	seed := model.Client{
		Name: "capped", Enable: true, LimitIp: 3, Volume: 100 << 30, Expiry: 1786000000,
		Inbounds: inbounds, Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	var svc NodeSyncService
	expected, err := svc.expectedClients(nodeId, map[string]uint{"vless-replica": 42})
	if err != nil {
		t.Fatalf("expectedClients: %v", err)
	}
	got, ok := expected["capped"]
	if !ok {
		t.Fatalf("client missing from the push payload: %v", expected)
	}

	if limitIp, _ := util.AsInt64(got["limitIp"]); limitIp != 3 {
		t.Errorf("limitIp = %v, want the master's 3 copied verbatim", got["limitIp"])
	}
	// The contrast that makes the above meaningful: a quota is additive across
	// nodes so it is deliberately zeroed, an IP cap is not so it is replicated.
	if volume, _ := util.AsInt64(got["volume"]); volume != 0 {
		t.Errorf("volume = %v, want 0 — quota stays the master's job", got["volume"])
	}
	if expiry, _ := util.AsInt64(got["expiry"]); expiry != 1786000000 {
		t.Errorf("expiry = %v, want it copied", got["expiry"])
	}
}

// The node answers with a marshalled model.Client and the master reads it back
// as a nodeClientState. The two json tags have to agree: a mismatch leaves
// LimitIp nil forever, which clientDiffers reads as "cannot compare" — so the
// limit would silently never sync, with nothing failing anywhere.
func TestNodeClientStateReadsLimitIpBackFromAClient(t *testing.T) {
	raw, err := json.Marshal(model.Client{Name: "x", LimitIp: 4})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var state nodeClientState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.LimitIp == nil {
		t.Fatalf("LimitIp came back nil from %s — the json tags disagree", raw)
	}
	if *state.LimitIp != 4 {
		t.Errorf("LimitIp = %d, want 4", *state.LimitIp)
	}

	// A node too old to have the column omits the key entirely, which must stay
	// distinguishable from a real zero.
	var old nodeClientState
	if err := json.Unmarshal([]byte(`{"name":"x"}`), &old); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if old.LimitIp != nil {
		t.Errorf("LimitIp = %v for a payload without the key, want nil", *old.LimitIp)
	}
}

func TestClientDiffers(t *testing.T) {
	cfg := json.RawMessage(`{"vless":{"uuid":"a"}}`)
	cfgReordered := json.RawMessage(`{"vless":{"uuid":"a"} }`)
	cfgOther := json.RawMessage(`{"vless":{"uuid":"b"}}`)
	inbounds := json.RawMessage(`[1,2]`)

	want := func(mods map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{
			"enable":   true,
			"expiry":   int64(100),
			"config":   cfg,
			"inbounds": inbounds,
		}
		for k, v := range mods {
			m[k] = v
		}
		return m
	}
	cur := nodeClientState{Enable: true, Expiry: 100, Config: cfg, Inbounds: inbounds}

	t.Run("identical", func(t *testing.T) {
		if clientDiffers(want(nil), cur) {
			t.Error("identical state reported as differing")
		}
	})
	t.Run("config compared structurally, not byte-wise", func(t *testing.T) {
		c := cur
		c.Config = cfgReordered
		if clientDiffers(want(nil), c) {
			t.Error("whitespace-only config difference reported as differing")
		}
	})
	t.Run("real config change", func(t *testing.T) {
		c := cur
		c.Config = cfgOther
		if !clientDiffers(want(nil), c) {
			t.Error("changed config not detected")
		}
	})
	t.Run("enable and expiry", func(t *testing.T) {
		if !clientDiffers(want(map[string]interface{}{"enable": false}), cur) {
			t.Error("changed enable not detected")
		}
		if !clientDiffers(want(map[string]interface{}{"expiry": int64(200)}), cur) {
			t.Error("changed expiry not detected")
		}
	})
	t.Run("inbounds", func(t *testing.T) {
		c := cur
		c.Inbounds = json.RawMessage(`[1]`)
		if !clientDiffers(want(nil), c) {
			t.Error("changed inbounds not detected")
		}
	})

	// The reason the both-sides-present guard exists: an older node's clients
	// projection has no config column, and treating that as "differs" re-pushed
	// every client every round, each push appending a credential-bearing
	// changes row that nothing prunes.
	//
	// The same assertion pins the accepted cost of that guard: a node-side
	// config someone CLEARED is indistinguishable from an old node's missing
	// column, so the safety net will not repair it either.
	t.Run("absent node config is not a difference", func(t *testing.T) {
		c := cur
		c.Config = nil
		if clientDiffers(want(nil), c) {
			t.Error("node without a config column reported as differing")
		}
		c.Config = json.RawMessage("")
		if clientDiffers(want(nil), c) {
			t.Error("node with an empty config reported as differing")
		}
	})
	t.Run("absent master config is not a difference", func(t *testing.T) {
		if clientDiffers(want(map[string]interface{}{"config": json.RawMessage(nil)}), cur) {
			t.Error("configless master client reported as differing")
		}
	})
	// Other fields must still be compared when config cannot be.
	t.Run("other fields still compared without config", func(t *testing.T) {
		c := cur
		c.Config = nil
		c.Enable = false
		if !clientDiffers(want(nil), c) {
			t.Error("enable change missed when config was absent")
		}
	})

	t.Run("limitIp change is detected", func(t *testing.T) {
		two := 2
		c := cur
		c.LimitIp = &two
		if !clientDiffers(want(map[string]interface{}{"limitIp": 3}), c) {
			t.Error("changed IP limit not detected")
		}
		if clientDiffers(want(map[string]interface{}{"limitIp": 2}), c) {
			t.Error("matching IP limit reported as differing")
		}
	})

	// Same guard as config: a node predating the column omits it, and reading
	// that as 0 would re-push every limited client on every round forever.
	t.Run("absent node limitIp is not a difference", func(t *testing.T) {
		if clientDiffers(want(map[string]interface{}{"limitIp": 2}), cur) {
			t.Error("node without a limit_ip column reported as differing")
		}
	})
}

// Adoption stores a snapshot of the node's inbound and, until issue #196,
// nothing ever refreshed it: an edit made on the node itself -- the listen port,
// most visibly -- reached neither this panel's inbounds list nor the "[node] "
// links it regenerates into the subscription, so the master went on handing out
// a dead port. What matters about refreshReplicas is that it rewrites the row
// from the node, keeps the columns adoption deliberately owns, leaves a replica
// the node no longer lists alone, and writes nothing when nothing moved.
func TestRefreshReplicasPullsNodeSideEdits(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	// Same global-handle dance as TestExpectedClientsCarriesLimitIp above, and
	// for the same reason: refreshReplicas reaches for database.GetDB() itself.
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "replicas.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	const nodeId = uint(3)
	nid := nodeId
	// The snapshot as adoption left it. Both halves carry the old port: Options
	// is what the inbounds list reads, OutJson is what link generation reads.
	adopted := model.Inbound{
		Type: "vless", Tag: "vless-node", NodeId: &nid,
		Options: json.RawMessage(`{"listen_port":443}`),
		OutJson: json.RawMessage(`{"server":"node.example","server_port":443}`),
		Addrs:   json.RawMessage(`[]`),
	}
	if err := db.Create(&adopted).Error; err != nil {
		t.Fatalf("seed replica: %v", err)
	}
	// A replica the node no longer lists — renamed there, or deleted. Clients
	// point their inbounds array at this row, so it must survive untouched.
	orphan := model.Inbound{
		Type: "trojan", Tag: "gone-from-node", NodeId: &nid,
		Options: json.RawMessage(`{"listen_port":8080}`),
	}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatalf("seed orphan replica: %v", err)
	}

	var queried []string
	node := &model.Node{Id: nodeId, Name: "tokyo", Enable: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/apiv2/inbounds" {
			t.Errorf("unexpected node request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queried = append(queried, r.URL.Query().Get("id"))
		// The node's panel shape (MarshalFull), with the port now moved and a
		// tls_id of its own that adoption drops on this side.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"inbounds":[
			{"id":9,"type":"vless","tag":"vless-node","tls_id":4,"addrs":[],
			 "out_json":{"server":"node.example","server_port":8443},
			 "listen":"::","listen_port":8443}]}}`))
	}))
	defer srv.Close()
	node.BaseUrl = srv.URL

	var svc NodeSyncService
	// "gone-from-node" is absent from the map on purpose — that is what a rename
	// or delete on the node side looks like from here.
	tagToId := map[string]uint{"vless-node": 9}
	// The return value is what tells runReconcile to regenerate the links right
	// away instead of waiting for the refreshNodeLinks past the client push,
	// which every push error returns before reaching. Getting it wrong strands
	// the panel showing the new port while the subscription serves the old one.
	if !svc.refreshReplicas(node, srv.Client(), tagToId) {
		t.Fatal("refreshReplicas reported no change after the node moved the port")
	}

	if len(queried) != 1 || queried[0] != "9" {
		t.Fatalf("node was asked for id=%v, want exactly the one adopted tag", queried)
	}

	var got model.Inbound
	if err := db.Model(model.Inbound{}).Where("id = ?", adopted.Id).Find(&got).Error; err != nil {
		t.Fatalf("reload replica: %v", err)
	}
	var opts map[string]interface{}
	if err := json.Unmarshal(got.Options, &opts); err != nil {
		t.Fatalf("unmarshal refreshed options: %v", err)
	}
	if port, _ := util.AsInt64(opts["listen_port"]); port != 8443 {
		t.Errorf("options listen_port = %v, want the node's 8443", opts["listen_port"])
	}
	// Panel-only keys must stay out of Options — the same contract adoption has.
	for _, k := range []string{"id", "tls_id", "out_json", "addrs", "type", "tag"} {
		if _, leaked := opts[k]; leaked {
			t.Errorf("refreshed options leaked the panel-only key %q", k)
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(got.OutJson, &out); err != nil {
		t.Fatalf("unmarshal refreshed out_json: %v", err)
	}
	if port, _ := util.AsInt64(out["server_port"]); port != 8443 {
		t.Errorf("out_json server_port = %v, want the node's 8443", out["server_port"])
	}
	// The columns this panel owns, not the node: TLS terminates on the node so
	// adoption drops its tls_id, and node_id is what marks the row a replica.
	if got.TlsId != 0 {
		t.Errorf("tls_id = %d, want the node's own left behind", got.TlsId)
	}
	if got.NodeId == nil || *got.NodeId != nodeId {
		t.Errorf("node_id = %v, want it preserved as %d", got.NodeId, nodeId)
	}

	// The half users actually feel: the subscription link now names the new port.
	links := genNodeReplicaLinks(&got, &model.Client{
		Name: "u1", Config: json.RawMessage(`{"vless":{"uuid":"uuid-1"}}`),
	})
	if len(links) != 1 || !strings.Contains(links[0], ":8443") {
		t.Errorf("regenerated links = %v, want one naming port 8443", links)
	}

	var stillThere model.Inbound
	if err := db.Model(model.Inbound{}).Where("id = ?", orphan.Id).Find(&stillThere).Error; err != nil {
		t.Fatalf("reload orphan replica: %v", err)
	}
	if stillThere.Tag != "gone-from-node" || !jsonEqual(stillThere.Options, json.RawMessage(`{"listen_port":8080}`)) {
		t.Errorf("replica absent from the node was modified: %+v", stillThere)
	}

	// One audit row for the one row that moved.
	var changes int64
	if err := db.Model(model.Changes{}).Count(&changes).Error; err != nil {
		t.Fatalf("count changes: %v", err)
	}
	if changes != 1 {
		t.Fatalf("changes rows = %d after the first refresh, want 1", changes)
	}

	// Idempotence is load-bearing, not tidiness: this runs on the 5s heartbeat
	// and the hourly sweep, and every write costs an unpruned changes row plus a
	// LastUpdate bump that repaints every open panel.
	if svc.refreshReplicas(node, srv.Client(), tagToId) {
		t.Error("refreshReplicas reported a change on an unchanged round")
	}
	if err := db.Model(model.Changes{}).Count(&changes).Error; err != nil {
		t.Fatalf("count changes: %v", err)
	}
	if changes != 1 {
		t.Errorf("changes rows = %d after an unchanged refresh, want the first 1", changes)
	}
}
