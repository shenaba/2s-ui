package sub

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/tls"

	"gopkg.in/yaml.v3"
)

// mihomo's ech-opts takes the bare base64 of the ECHConfigList. sing-box stores
// the same thing as PEM, one list entry per line, so the armor has to come off
// -- and it has to come off by content, not by position: the panel's own
// generateECHKeyPair splits a PEM that ends in a newline, so the stored list
// carries a trailing empty entry and a positional slice left "-----END ECH
// CONFIGS-----" glued to the end of the value.
func TestEchConfigForClashStripsPemArmor(t *testing.T) {
	configPem, _, err := tls.ECHKeygenDefault("example.com")
	if err != nil {
		t.Fatalf("ECHKeygenDefault: %v", err)
	}

	// Exactly what generateECHKeyPair stores.
	var stored []interface{}
	for _, line := range strings.Split(configPem, "\n") {
		stored = append(stored, line)
	}

	got := echConfigForClash(stored)
	if got == "" {
		t.Fatalf("no ech config produced from %q", configPem)
	}
	if strings.Contains(got, "-----") {
		t.Errorf("ech-opts config still carries PEM armor: %q", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("ech-opts config does not decode as base64: %q: %v", got, err)
	}
	if len(decoded) == 0 {
		t.Errorf("ech-opts config decodes to nothing: %q", got)
	}
}

// A TLS block with no "enabled" key used to mean "on" here while the share-link
// builders read it as "off", so one hand-written or imported row produced a
// Clash proxy with the full TLS block and a link with none at all. It also wrote
// `tls: null`, where mihomo wants a bool.
func TestClashTlsFollowsEnabled(t *testing.T) {
	setupSubDB(t)
	tests := []struct {
		name    string
		tls     map[string]interface{}
		wantTls interface{} // nil = the key must be absent
		wantSni interface{}
	}{
		{"enabled true", map[string]interface{}{"enabled": true, "server_name": "e.com"}, true, "e.com"},
		{"enabled false", map[string]interface{}{"enabled": false, "server_name": "e.com"}, nil, nil},
		{"enabled absent", map[string]interface{}{"server_name": "e.com"}, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbounds := []map[string]interface{}{{
				"type": "trojan", "tag": "tr", "server": "e.com",
				"server_port": float64(443), "password": "pw",
				"tls": tt.tls,
			}}
			raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
			if err != nil {
				t.Fatalf("ConvertToClashMeta: %v", err)
			}
			proxy := firstProxy(t, raw)

			if got := proxy["tls"]; got != tt.wantTls {
				t.Errorf("tls = %#v, want %#v", got, tt.wantTls)
			}
			if got := proxy["sni"]; got != tt.wantSni {
				t.Errorf("sni = %#v, want %#v -- TLS params must follow the same switch", got, tt.wantSni)
			}
		})
	}
}

// The bounds check added for the empty-array case routes it to this branch
// instead of panicking, and the branch used to read both keys back
// unconditionally: a transport carrying neither produced `path: [null]` and
// `host: null`, which mihomo decodes as strings.
func TestClashHttpOptsOmitAbsentPathAndHost(t *testing.T) {
	setupSubDB(t)
	for _, transport := range []map[string]interface{}{
		{"type": "http"},
		{"type": "http", "path": []interface{}{}},
		{"type": "http", "host": []interface{}{}},
	} {
		outbounds := []map[string]interface{}{{
			"type": "vmess", "tag": "vm", "server": "e.com",
			"server_port": float64(80), "uuid": "uuid-1",
			"transport": transport,
		}}
		raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
		if err != nil {
			t.Fatalf("ConvertToClashMeta: %v", err)
		}
		opts, ok := firstProxy(t, raw)["http-opts"].(map[string]interface{})
		if !ok {
			t.Fatalf("http-opts missing for %v", transport)
		}
		for key, value := range opts {
			if value == nil {
				t.Errorf("%v: http-opts.%s is null, want the key omitted", transport, key)
			}
			if list, isList := value.([]interface{}); isList {
				for _, item := range list {
					if item == nil {
						t.Errorf("%v: http-opts.%s holds a null entry", transport, key)
					}
				}
			}
		}
	}
}

// firstProxy pulls the single generated proxy out of a rendered Clash config.
func firstProxy(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal clash config: %v", err)
	}
	proxies, _ := cfg["proxies"].([]interface{})
	if len(proxies) != 1 {
		t.Fatalf("got %d proxies, want 1:\n%s", len(proxies), raw)
	}
	proxy, ok := proxies[0].(map[string]interface{})
	if !ok {
		t.Fatalf("proxy is not a mapping:\n%s", raw)
	}
	return proxy
}

// pem.Decode is just as happy with an ECH KEYS block as with an ECH CONFIGS
// one, and generateECHKeyPair hands the operator both concatenated into one
// array while the field is a free-text textarea. Without a type check, pasting
// the wrong half published the ECH private key to every subscriber.
func TestEchConfigForClashRefusesTheKeyBlock(t *testing.T) {
	configPem, keyPem, err := tls.ECHKeygenDefault("example.com")
	if err != nil {
		t.Fatalf("ECHKeygenDefault: %v", err)
	}

	list := func(s string) []interface{} {
		out := []interface{}{}
		for _, line := range strings.Split(s, "\n") {
			out = append(out, line)
		}
		return out
	}

	if got := echConfigForClash(list(keyPem)); got != "" {
		t.Errorf("an ECH KEYS block must never reach a subscriber, got %q", got)
	}
	// The whole generator output, keys first, is what an operator pasting the
	// button's result wholesale would produce.
	if got := echConfigForClash(list(keyPem + configPem)); got != "" {
		t.Errorf("a key block in front must not be published, got %q", got)
	}
	if got := echConfigForClash(list(configPem)); got == "" {
		t.Errorf("the config block itself must still be accepted")
	}
}

// sing-box writes a single port as a number and a range as a string. The Clash
// converter asserted every entry was a string, so numeric entries were folded
// into empty strings and reached mihomo as ",20000-30000" or ",".
func TestPortHoppingRangesAcceptsNumbers(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"strings", []interface{}{"20000:30000", "40000:50000"}, "20000-30000,40000-50000"},
		{"numbers", []interface{}{float64(443), float64(8443)}, "443,8443"},
		{"mixed", []interface{}{float64(443), "20000:30000"}, "443,20000-30000"},
		{"empty", []interface{}{}, ""},
		{"absent", nil, ""},
		{"not a list", "20000:30000", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portHoppingRanges(tt.in); got != tt.want {
				t.Errorf("portHoppingRanges = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every one of these is a shape the textarea in the TLS drawer can produce, or
// that an older row can hold.
func TestEchConfigForClashHandlesStoredShapes(t *testing.T) {
	const body = "AEb+DQBCAAAgACCcU7maLJBQY8q0VucyTJIVJ9DThF3CI8NjiSMlChH2NQAMAAEA"
	const body2 = "AQABAAIAAQADAAtleGFtcGxlLmNvbQAA"
	want := body + body2

	list := func(lines ...string) []interface{} {
		out := make([]interface{}, 0, len(lines))
		for _, l := range lines {
			out = append(out, l)
		}
		return out
	}

	tests := []struct {
		name   string
		stored []interface{}
		want   string
	}{
		{"wrapped body with a trailing blank", list(
			"-----BEGIN ECH CONFIGS-----", body, body2, "-----END ECH CONFIGS-----", ""), want},
		{"no trailing blank", list(
			"-----BEGIN ECH CONFIGS-----", body, body2, "-----END ECH CONFIGS-----"), want},
		{"empty list", nil, ""},
		{"not pem at all", list("just some text"), ""},
		// A list holding something that is not a string no longer panics.
		{"non-string entries", []interface{}{
			"-----BEGIN ECH CONFIGS-----", body, body2, float64(7), "-----END ECH CONFIGS-----"}, want},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := echConfigForClash(tt.stored); got != tt.want {
				t.Errorf("echConfigForClash = %q, want %q", got, tt.want)
			}
		})
	}
}
