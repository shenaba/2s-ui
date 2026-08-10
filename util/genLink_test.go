package util

import (
	"strings"
	"testing"
)

func TestNaiveLinkSchemesFollowNetwork(t *testing.T) {
	tests := []struct {
		network string
		want    []string
	}{
		// A listener bound to one network can only be reached over the matching
		// scheme, so offering the other one would hand out a dead link.
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
