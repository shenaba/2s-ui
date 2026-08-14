package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
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

	if asInt64(got["limitIp"]) != 3 {
		t.Errorf("limitIp = %v, want the master's 3 copied verbatim", got["limitIp"])
	}
	// The contrast that makes the above meaningful: a quota is additive across
	// nodes so it is deliberately zeroed, an IP cap is not so it is replicated.
	if asInt64(got["volume"]) != 0 {
		t.Errorf("volume = %v, want 0 — quota stays the master's job", got["volume"])
	}
	if asInt64(got["expiry"]) != 1786000000 {
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
