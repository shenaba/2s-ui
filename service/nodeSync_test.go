package service

import (
	"encoding/json"
	"testing"
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
}
