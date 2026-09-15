package util

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// mport and alpn go into the query unescaped, and neither value is necessarily
// this panel's: a managed node supplies both through its out_json, and the
// master generates its own subscribers' links for that node's replica inbounds
// from it. An '&' in either therefore appended a parameter of the node's
// choosing to a link the master hands out under its own name.
//
// The two cases below are the ones that were demonstrated end to end: the
// panel's own parser read insecure=1 back as tls.insecure = true, which turns
// off certificate verification in the subscriber's client.
func TestNodeSuppliedValuesCannotAppendLinkParameters(t *testing.T) {
	t.Run("alpn", func(t *testing.T) {
		addrs := `[{"server":"jp.example.com","server_port":443,"tls":{"enabled":true,` +
			`"server_name":"good.example","alpn":["h3&insecure=1"]}}]`
		links := LinkGenerator(
			json.RawMessage(`{"hysteria2":{"password":"pw"}}`),
			&model.Inbound{
				Type: "hysteria2", Tag: "node",
				Addrs:   json.RawMessage(addrs),
				Options: json.RawMessage(`{"listen_port":443}`),
			},
			"example.com", "victim")
		assertNoInjectedInsecure(t, links)
	})

	t.Run("mport", func(t *testing.T) {
		links := LinkGenerator(
			json.RawMessage(`{"hysteria2":{"password":"pw"}}`),
			&model.Inbound{
				Type: "hysteria2", Tag: "node",
				Addrs:   json.RawMessage("null"),
				Options: json.RawMessage(`{"listen_port":443}`),
				OutJson: json.RawMessage(`{"server_ports":["443&insecure=1"]}`),
			},
			"example.com", "victim")
		assertNoInjectedInsecure(t, links)
	})
}

func assertNoInjectedInsecure(t *testing.T, links []string) {
	t.Helper()
	if len(links) == 0 {
		t.Fatal("no links generated")
	}
	for _, link := range links {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("the panel produced an unparseable link %q: %v", link, err)
		}
		if _, injected := u.Query()["insecure"]; injected {
			t.Errorf("a node-supplied value appended insecure= to %q", link)
		}
		// And the panel's own reader has to agree, since that is what feeds the
		// JSON and Clash subscriptions.
		ob, _, err := GetOutbound(link, 0)
		if err != nil {
			t.Fatalf("the panel cannot parse its own link %q: %v", link, err)
		}
		if tls, ok := (*ob)["tls"].(map[string]interface{}); ok {
			if tls["insecure"] == true {
				t.Errorf("link %q parses to tls.insecure = true", link)
			}
		}
	}
}

// The commas have to survive: that is the whole reason these two are written
// raw, and escaping them is what the exception exists to avoid.
func TestLegitimateRawParametersAreKeptVerbatim(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"alpn", "h2,http/1.1"},
		{"alpn", "h3"},
		{"alpn", "spdy/3.1,h2"},
		{"mport", "443,20000-30000"},
		{"mport", "443"},
		{"mport", "20000:30000"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			got := encodeParams([]LinkParam{{tt.key, tt.value}})
			want := tt.key + "=" + tt.value
			if got != want {
				t.Errorf("encodeParams = %q, want %q", got, want)
			}
		})
	}
}

// Everything outside the allowed set is dropped rather than escaped or passed
// through: every legitimate value is inside it, so what is left is a typo or an
// injection, and a link missing one optional parameter beats a link carrying
// either.
func TestUnexpectedRawParametersAreDropped(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"alpn", "h3&insecure=1"},
		{"alpn", "h3#fragment"},
		{"alpn", "h3 h2"},
		{"alpn", ""},
		{"mport", "443&insecure=1"},
		{"mport", "443;444"},
		{"mport", "not-a-port"},
	} {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			got := encodeParams([]LinkParam{{"sni", "keep.example"}, {tt.key, tt.value}})
			if strings.Contains(got, tt.key+"=") {
				t.Errorf("encodeParams = %q, want %s dropped", got, tt.key)
			}
			// The rest of the query is unaffected.
			if !strings.Contains(got, "sni=keep.example") {
				t.Errorf("encodeParams = %q, want the other parameters kept", got)
			}
		})
	}
}
