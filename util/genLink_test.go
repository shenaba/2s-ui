package util

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

func TestNaiveLinkSchemesFollowNetwork(t *testing.T) {
	tests := []struct {
		network string
		want    []string
	}{
		// The plain naive+ scheme has to match the listener's network, or the
		// link is dead. The legacy http2:// form goes out either way, for
		// clients that parse nothing else -- on a udp-only inbound that one is
		// dead too, and kept anyway rather than diverging from upstream.
		{"tcp", []string{"http2://", "naive+https://"}},
		{"udp", []string{"http2://", "naive+quic://"}},

		// Unset means sing-box listens on both.
		{"", []string{"http2://", "naive+https://", "naive+quic://"}},
	}

	addrs := []map[string]interface{}{{
		"server":      "example.com",
		"server_port": float64(443),
		"remark":      "naive-in",
	}}

	for _, tt := range tests {
		name := tt.network
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			inbound := map[string]interface{}{}
			if tt.network != "" {
				inbound["network"] = tt.network
			}
			links := naiveLink(map[string]interface{}{"username": "u", "password": "p"}, inbound, addrs)

			if len(links) != len(tt.want) {
				t.Fatalf("got %d links %v, want %d", len(links), links, len(tt.want))
			}
			for i, prefix := range tt.want {
				if !strings.HasPrefix(links[i], prefix) {
					t.Errorf("link %d = %q, want prefix %q", i, links[i], prefix)
				}
			}
		})
	}
}

func TestNaiveLinkEscapesUserinfo(t *testing.T) {
	links := naiveLink(
		// A space must survive as %20: '+' would read back as a literal plus.
		map[string]interface{}{"username": "a b", "password": "p@ss/word"},
		map[string]interface{}{"network": "tcp"},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "naive-in",
		}},
	)

	const want = "naive+https://a%20b:p%40ss%2Fword@example.com:443"
	if len(links) != 2 || !strings.HasPrefix(links[1], want) {
		t.Errorf("got %v, want a link starting with %q", links, want)
	}
}

func TestNaiveLinkRemarksDistinguishTransport(t *testing.T) {
	links := naiveLink(
		map[string]interface{}{"username": "u", "password": "p"},
		map[string]interface{}{},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "naive-in",
		}},
	)

	// Three links for one address, so the plain ones say which transport they
	// are. The legacy link keeps the bare remark: renaming it would read as a
	// different node to a client that already has it.
	want := []string{"#naive-in", "#naive-in-h2", "#naive-in-h3"}
	if len(links) != len(want) {
		t.Fatalf("got %d links %v, want %d", len(links), links, len(want))
	}
	for i, fragment := range want {
		if !strings.HasSuffix(links[i], fragment) {
			t.Errorf("link %d = %q, want suffix %q", i, links[i], fragment)
		}
	}
}

// An IPv6 hostname reaches LinkGenerator bracketed (api.normalizeHost does
// that) or bare (an address row holds whatever the operator typed). Both have
// to come out the same way: bracketed inside a URI authority, bare in the
// vmess "add" field, which is JSON rather than a URI (#1220).
func TestLinkGeneratorIPv6Hostname(t *testing.T) {
	newInbound := func(inboundType, addrs string) *model.Inbound {
		return &model.Inbound{
			Type:    inboundType,
			Tag:     "v6-in",
			Addrs:   json.RawMessage(addrs),
			Options: json.RawMessage(`{"listen_port":443}`),
		}
	}

	t.Run("vless brackets the authority", func(t *testing.T) {
		for _, hostname := range []string{"[2001:db8::1]", "2001:db8::1"} {
			links := LinkGenerator(
				json.RawMessage(`{"vless":{"uuid":"uuid-1"}}`),
				newInbound("vless", "null"), hostname, "")

			const want = "vless://uuid-1@[2001:db8::1]:443"
			if len(links) != 1 || !strings.HasPrefix(links[0], want) {
				t.Errorf("hostname %q gave %v, want a link starting with %q", hostname, links, want)
			}
		}
	})

	t.Run("vmess add stays bare", func(t *testing.T) {
		links := LinkGenerator(
			json.RawMessage(`{"vmess":{"uuid":"uuid-1"}}`),
			newInbound("vmess", "null"), "[2001:db8::1]", "")

		if len(links) != 1 {
			t.Fatalf("got %d links %v, want 1", len(links), links)
		}
		raw, err := B64StrToByte(strings.TrimPrefix(links[0], "vmess://"))
		if err != nil {
			t.Fatalf("decoding %q: %v", links[0], err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("unmarshalling %q: %v", raw, err)
		}
		if got := obj["add"]; got != "2001:db8::1" {
			t.Errorf(`add = %q, want "2001:db8::1"`, got)
		}
	})

	t.Run("address rows are normalised too", func(t *testing.T) {
		addrs := `[{"server":"[2001:db8::2]","server_port":8443,"remark":"-alt"}]`
		links := LinkGenerator(
			json.RawMessage(`{"trojan":{"password":"pw"}}`),
			newInbound("trojan", addrs), "example.com", "")

		const want = "trojan://pw@[2001:db8::2]:8443"
		if len(links) != 1 || !strings.HasPrefix(links[0], want) {
			t.Errorf("got %v, want a link starting with %q", links, want)
		}
	})
}
