package util

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// socks, http and mixed are three of the twelve types in InboundTypeWithLink, so
// the panel writes links for them -- and GetOutbound could not read any of them
// back. sub/linkService.go drops a link it cannot parse without a word, so a node
// replica of one of those inbounds was served in the plain-link subscription and
// silently missing from the JSON and Clash ones.
func TestGetOutboundReadsTheProxyLinksThePanelWrites(t *testing.T) {
	tests := []struct {
		name string
		link string
		want map[string]interface{}
	}{
		{
			name: "socks5 with credentials",
			link: "socks5://alice:hunter2@e.com:1080#node-socks",
			want: map[string]interface{}{
				"type": "socks", "tag": "node-socks", "server": "e.com",
				"server_port": 1080, "version": "5",
				"username": "alice", "password": "hunter2",
			},
		},
		{
			name: "socks5 with no authentication",
			link: "socks5://e.com:1080#node",
			want: map[string]interface{}{
				"type": "socks", "tag": "node", "server": "e.com",
				"server_port": 1080, "version": "5",
			},
		},
		{
			name: "http",
			link: "http://alice:hunter2@e.com:8080#node-http",
			want: map[string]interface{}{
				"type": "http", "tag": "node-http", "server": "e.com",
				"server_port": 8080, "username": "alice", "password": "hunter2",
			},
		},
		{
			// httpLink picks the scheme from each address's own tls.enabled, so
			// the scheme is the only thing carrying it.
			name: "https carries the TLS flag",
			link: "https://e.com:8443#node-http",
			want: map[string]interface{}{
				"type": "http", "tag": "node-http", "server": "e.com",
				"server_port": 8443,
				"tls":         map[string]interface{}{"enabled": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, tag, err := GetOutbound(tt.link, 0)
			if err != nil {
				t.Fatalf("GetOutbound(%q): %v", tt.link, err)
			}
			if tag != tt.want["tag"] {
				t.Errorf("tag = %q, want %v", tag, tt.want["tag"])
			}
			for key, want := range tt.want {
				// The one nested value is checked by hand: maps are not
				// comparable, and == on two of them panics rather than failing.
				if key == "tls" {
					tls, _ := (*got)["tls"].(map[string]interface{})
					if tls == nil || tls["enabled"] != true {
						t.Errorf("tls = %#v, want enabled", (*got)["tls"])
					}
					continue
				}
				if (*got)[key] != want {
					t.Errorf("%s = %#v, want %#v", key, (*got)[key], want)
				}
			}
			if _, wantTLS := tt.want["tls"]; !wantTLS {
				if _, present := (*got)["tls"]; present {
					t.Errorf("a plain http link produced a TLS outbound: %#v", *got)
				}
			}
			// Absent credentials must stay absent: sing-box reads the fields
			// being there as "authenticate", and empty is not the same as none.
			if _, ok := tt.want["username"]; !ok {
				if _, present := (*got)["username"]; present {
					t.Errorf("username is present for a link with no userinfo: %#v", *got)
				}
			}
		})
	}
}

// http:// and https:// are also what an ordinary web address looks like, and a
// text subscription body is handed to GetOutbound a line at a time. Without a
// rule that tells the two apart, every URL in one would become a proxy.
func TestGetOutboundRefusesAnOrdinaryWebAddress(t *testing.T) {
	for _, link := range []string{
		"https://example.com",
		"https://example.com/sub.txt",
		"http://example.com/",
		"https://example.com:8443/api/v1/sub",
		"https://example.com:0",
		"https://example.com:99999",
		"socks5://example.com",
	} {
		t.Run(link, func(t *testing.T) {
			if _, _, err := GetOutbound(link, 0); err == nil {
				t.Errorf("GetOutbound(%q) was accepted as a proxy link", link)
			}
		})
	}
}

// The panel has to be able to read back what it writes: these links go into a
// node replica's client entry and come out of GetExternalOutbounds.
func TestProxyLinksRoundTripThroughGetOutbound(t *testing.T) {
	tests := []struct {
		inboundType string
		config      string
		wantTypes   []string
	}{
		{"socks", `{"socks":{"username":"alice","password":"hunter2"}}`, []string{"socks"}},
		{"http", `{"http":{"username":"alice","password":"hunter2"}}`, []string{"http"}},
		{"mixed", `{"socks":{"username":"a","password":"b"},"http":{"username":"a","password":"b"}}`,
			[]string{"socks", "http"}},
		// No credentials configured: the link carries no userinfo at all.
		{"socks", `{"socks":{}}`, []string{"socks"}},
	}

	for _, tt := range tests {
		t.Run(tt.inboundType+" "+tt.config, func(t *testing.T) {
			links := LinkGenerator(
				json.RawMessage(tt.config),
				&model.Inbound{
					Type:    tt.inboundType,
					Tag:     "in",
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(`{"listen_port":1080}`),
				},
				"example.com", "alice")
			if len(links) != len(tt.wantTypes) {
				t.Fatalf("got %d links %v, want %d", len(links), links, len(tt.wantTypes))
			}
			for i, link := range links {
				ob, tag, err := GetOutbound(link, 0)
				if err != nil {
					t.Fatalf("the panel cannot parse its own link %q: %v", link, err)
				}
				if (*ob)["type"] != tt.wantTypes[i] {
					t.Errorf("link %q parsed to type %v, want %s", link, (*ob)["type"], tt.wantTypes[i])
				}
				if tag == "" {
					t.Errorf("link %q parsed to an empty tag, which the subscription drops", link)
				}
			}
		})
	}
}
