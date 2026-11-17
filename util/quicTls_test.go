package util

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// A TLS template with a uTLS fingerprint, shared by TCP and QUIC inbounds. The
// fingerprint belongs on the TCP ones only: sing-box refuses uTLS on a QUIC
// outbound ("unsupported usage for uTLS"), so a tuic or hysteria client handed
// one cannot connect at all.
func utlsTemplate() *model.Tls {
	return &model.Tls{
		Server: json.RawMessage(`{"enabled":true,"server_name":"example.com","alpn":["h3"]}`),
		Client: json.RawMessage(`{"utls":{"enabled":true,"fingerprint":"chrome"}}`),
	}
}

func TestFillOutJsonDropsUtlsOnQuic(t *testing.T) {
	tests := []struct {
		inboundType string
		options     string
		wantUtls    bool
	}{
		{"tuic", `{"listen_port":443}`, false},
		{"hysteria2", `{"listen_port":443}`, false},
		{"hysteria", `{"listen_port":443,"up_mbps":10,"down_mbps":10}`, false},
		{"vless", `{"listen_port":443}`, true},
		{"trojan", `{"listen_port":443}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.inboundType, func(t *testing.T) {
			inbound := &model.Inbound{
				Type: tt.inboundType, Tag: "in",
				TlsId: 1, Tls: utlsTemplate(),
				Options: json.RawMessage(tt.options),
				// A row written before the fix still carries it.
				OutJson: json.RawMessage(`{"tls":{"utls":{"enabled":true,"fingerprint":"chrome"}}}`),
			}
			if err := FillOutJson(inbound, "example.com"); err != nil {
				t.Fatal(err)
			}
			var out map[string]interface{}
			if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
				t.Fatal(err)
			}
			tls, ok := out["tls"].(map[string]interface{})
			if !ok {
				t.Fatalf("tls block missing: %s", inbound.OutJson)
			}
			if _, has := tls["utls"]; has != tt.wantUtls {
				t.Errorf("utls present = %v, want %v: %s", has, tt.wantUtls, inbound.OutJson)
			}
		})
	}
}

func TestQuicLinksOmitFingerprint(t *testing.T) {
	tests := []struct {
		inboundType string
		config      string
		wantFp      bool
	}{
		{"tuic", `{"tuic":{"uuid":"u-1","password":"pw"}}`, false},
		{"hysteria2", `{"hysteria2":{"password":"pw"}}`, false},
		{"hysteria", `{"hysteria":{"auth_str":"pw"}}`, false},
		{"vless", `{"vless":{"uuid":"u-1"}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.inboundType, func(t *testing.T) {
			links := LinkGenerator(
				json.RawMessage(tt.config),
				&model.Inbound{
					Type: tt.inboundType, Tag: "in",
					TlsId: 1, Tls: utlsTemplate(),
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(`{"listen_port":443,"up_mbps":10,"down_mbps":10}`),
				},
				"example.com", "")
			if len(links) == 0 {
				t.Fatal("no links generated")
			}
			for _, link := range links {
				if has := strings.Contains(link, "fp=chrome"); has != tt.wantFp {
					t.Errorf("fp in link = %v, want %v: %s", has, tt.wantFp, link)
				}
			}
		})
	}
}

// A link from elsewhere can still carry fp on a QUIC scheme; reading it back as
// uTLS would hand the subscriber the same broken outbound.
func TestQuicLinkFingerprintNotParsedAsUtls(t *testing.T) {
	tests := []struct {
		link     string
		wantUtls bool
	}{
		{"tuic://u-1:pw@example.com:443?sni=example.com&fp=chrome&congestion_control=bbr#t", false},
		{"hysteria2://pw@example.com:443?sni=example.com&fp=chrome#h2", false},
		{"hysteria://example.com:443?auth=pw&upmbps=10&downmbps=10&sni=example.com&fp=chrome#h1", false},
		{"vless://u-1@example.com:443?security=tls&sni=example.com&fp=chrome#v", true},
	}
	for _, tt := range tests {
		t.Run(tt.link[:strings.Index(tt.link, ":")], func(t *testing.T) {
			ob, _, err := GetOutbound(tt.link, 0)
			if err != nil {
				t.Fatal(err)
			}
			tls, ok := (*ob)["tls"].(map[string]interface{})
			if !ok {
				t.Fatalf("tls block missing: %v", *ob)
			}
			if _, has := tls["utls"]; has != tt.wantUtls {
				t.Errorf("utls present = %v, want %v: %v", has, tt.wantUtls, tls)
			}
		})
	}
}

func TestDropQuicLinkFingerprint(t *testing.T) {
	tests := []struct{ in, want string }{
		{"tuic://u:p@h.com:443?sni=h.com&fp=chrome&alpn=h3#a%20b", "tuic://u:p@h.com:443?sni=h.com&alpn=h3#a%20b"},
		{"hysteria2://p@h.com:443?fp=chrome#x", "hysteria2://p@h.com:443#x"},
		{"hy2://p@h.com:443?fp=chrome", "hy2://p@h.com:443"},
		{"hysteria://h.com:443?auth=x&fp=ios&upmbps=1", "hysteria://h.com:443?auth=x&upmbps=1"},
		// Untouched: other schemes, no fp, a parameter merely ending in fp.
		{"vless://u@h.com:443?fp=chrome#v", "vless://u@h.com:443?fp=chrome#v"},
		{"tuic://u:p@h.com:443?sni=h.com#fp=chrome", "tuic://u:p@h.com:443?sni=h.com#fp=chrome"},
		{"tuic://u:p@h.com:443?xfp=1", "tuic://u:p@h.com:443?xfp=1"},
		{"not a link", "not a link"},
	}
	for _, tt := range tests {
		if got := DropQuicLinkFingerprint(tt.in); got != tt.want {
			t.Errorf("DropQuicLinkFingerprint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
