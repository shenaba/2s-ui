package sub

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"

	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"

	"gopkg.in/yaml.v3"
)

// mihomo's ech-opts takes the bare base64 of the ECHConfigList. sing-box stores
// the same thing as PEM, one list entry per line, so the armor has to come off
// -- and it has to come off by content, not by position: the panel's own
// generateECHKeyPair splits a PEM that ends in a newline, so the stored list
// carries a trailing empty entry and a positional slice left "-----END ECH
// CONFIGS-----" glued to the end of the value.
func TestECHConfigBase64StripsPemArmor(t *testing.T) {
	configPem, _, err := tls.ECHKeygenDefault("example.com")
	if err != nil {
		t.Fatalf("ECHKeygenDefault: %v", err)
	}

	// Exactly what generateECHKeyPair stores.
	var stored []interface{}
	for _, line := range strings.Split(configPem, "\n") {
		stored = append(stored, line)
	}

	got := util.ECHConfigBase64(stored)
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

// GetOutbound assembles an outbound in Go instead of unmarshalling one, and
// GetExternalOutbounds hands that map straight to the Clash converter -- so
// every value arrives in its Go shape: a list is []string where a stored row
// gives []interface{}, a number is int where a stored row gives float64. Each
// assertion here was written for the JSON shape only and silently dropped the
// field for every external and "[node] " replica link.
//
// One table over every such field, so the next one added is caught here rather
// than in a bug report: a field that survives a stored row and vanishes from a
// pasted link is this bug, whatever its name.
func TestClashKeepsGoShapedFieldsOfExternalLinks(t *testing.T) {
	setupSubDB(t)

	tests := []struct {
		name string
		link string
		key  string
		want interface{}
	}{
		{
			// getTls fills alpn with a strings.Split.
			name: "alpn",
			link: "vless://11111111-1111-1111-1111-111111111111@e.com:443" +
				"?security=tls&sni=x.com&alpn=h2,http/1.1&type=tcp#node",
			key:  "alpn",
			want: []interface{}{"h2", "http/1.1"},
		},
		{
			// getTransport splits the host query param the same way.
			name: "http transport host",
			link: "vless://11111111-1111-1111-1111-111111111111@e.com:443" +
				"?security=tls&sni=x.com&type=http&host=a.com&path=/p#node",
			key:  "h2-opts",
			want: map[string]interface{}{"host": "a.com", "path": "/p"},
		},
		{
			// hy2 splits mport into a []string.
			name: "port hopping",
			link: "hysteria2://pw@e.com:443?sni=x.com&mport=443,20000-30000#node",
			key:  "ports",
			want: "443,20000-30000",
		},
		{
			// hy2 builds the obfs object itself, and the hysteria2 branch
			// below reads it back with a type assertion on that exact shape.
			name: "hysteria2 gecko obfs",
			link: "hysteria2://pw@e.com:443?sni=x.com&obfs=gecko&obfs-password=hunter2#node",
			key:  "obfs",
			want: "gecko",
		},
		{
			// hy runs the bandwidths through strconv.Atoi, yielding ints.
			name: "hysteria bandwidth",
			link: "hy://e.com:443?auth=a&upmbps=100&downmbps=200#node",
			key:  "up",
			want: 100,
		},
		{
			// vmess stores alter_id as an int. mihomo, unlike sing-box, still
			// speaks non-AEAD vmess, so the value has to survive.
			name: "vmess alterId",
			link: "vmess://eyJ2IjoiMiIsInBzIjoibiIsImFkZCI6ImUuY29tIiwicG9ydCI6IjQ0MyIsImlkIjoiMTExMTExMTEtMTExMS0xMTExLTExMTEtMTExMTExMTExMTExIiwiYWlkIjo2NCwibmV0IjoidGNwIiwidGxzIjoibm9uZSJ9",
			key:  "alterId",
			want: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbound, _, err := util.GetOutbound(tt.link, 0)
			if err != nil {
				t.Fatalf("GetOutbound(%q): %v", tt.link, err)
			}
			outbounds := []map[string]interface{}{*outbound}
			raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
			if err != nil {
				t.Fatalf("ConvertToClashMeta: %v", err)
			}
			got, ok := firstProxy(t, raw)[tt.key]
			if !ok {
				t.Fatalf("%s is missing from the proxy entirely:\n%s", tt.key, raw)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s = %#v, want %#v:\n%s", tt.key, got, tt.want, raw)
			}
		})
	}
}

// anytls was missing from the UDP default, and its own branch copied sni and
// insecure off the TLS block unconditionally: `skip-cert-verify: null` when
// insecure was unset, and both keys even with TLS off (#1291).
func TestClashAnytlsUdpAndTlsFields(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	tests := []struct {
		name     string
		tls      map[string]interface{}
		wantSni  interface{} // nil = the key must be absent
		wantSkip interface{}
	}{
		{"insecure unset", map[string]interface{}{"enabled": true, "server_name": "a.com"}, "a.com", nil},
		{"insecure true", map[string]interface{}{"enabled": true, "server_name": "a.com", "insecure": true}, "a.com", true},
		{"tls off", map[string]interface{}{"enabled": false, "server_name": "a.com", "insecure": true}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbounds := []map[string]interface{}{{
				"type": "anytls", "tag": "at", "server": "a.com",
				"server_port": float64(443), "password": "pw",
				"tls": tt.tls,
			}}
			raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
			if err != nil {
				t.Fatalf("ConvertToClashMeta: %v", err)
			}
			proxy := firstProxy(t, raw)
			if proxy["udp"] != true {
				t.Errorf("udp = %#v with subClashUdp on, want true", proxy["udp"])
			}
			for key, want := range map[string]interface{}{"sni": tt.wantSni, "skip-cert-verify": tt.wantSkip} {
				got, present := proxy[key]
				if want == nil && present {
					t.Errorf("%s = %#v, want the key omitted", key, got)
				} else if want != nil && got != want {
					t.Errorf("%s = %#v, want %#v", key, got, want)
				}
			}
		})
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
func TestECHConfigBase64RefusesTheKeyBlock(t *testing.T) {
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

	if got := util.ECHConfigBase64(list(keyPem)); got != "" {
		t.Errorf("an ECH KEYS block must never reach a subscriber, got %q", got)
	}
	// The whole generator output, keys first, is what an operator pasting the
	// button's result wholesale would produce.
	if got := util.ECHConfigBase64(list(keyPem + configPem)); got != "" {
		t.Errorf("a key block in front must not be published, got %q", got)
	}
	if got := util.ECHConfigBase64(list(configPem)); got == "" {
		t.Errorf("the config block itself must still be accepted")
	}
}

// Every one of these is a shape the textarea in the TLS drawer can produce, or
// that an older row can hold.
func TestECHConfigBase64HandlesStoredShapes(t *testing.T) {
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

	pem := "-----BEGIN ECH CONFIGS-----\n" + body + "\n" + body2 + "\n-----END ECH CONFIGS-----\n"

	tests := []struct {
		name   string
		stored interface{}
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
		// config is Listable[string]: sing-box writes a one-element list back
		// out as a bare scalar, and an operator pasting a PEM types it as one
		// string. Both used to read as absent, and ech-opts was then left out
		// of a profile whose server requires ECH.
		{"a whole PEM in one string", pem, want},
		{"an empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := util.ECHConfigBase64(tt.stored); got != tt.want {
				t.Errorf("ECHConfigBase64 = %q, want %q", got, tt.want)
			}
		})
	}
}

// The hysteria2 branch copied the obfs type and password and stopped there, so
// a gecko inbound's packet sizes never reached a Clash subscriber (#292).
// mihomo spells them obfs-min-packet-size and obfs-max-packet-size.
func TestClashHysteria2GeckoPacketSizes(t *testing.T) {
	setupSubDB(t)

	tests := []struct {
		name    string
		obfs    map[string]interface{}
		wantMin interface{} // nil = the key must be absent
		wantMax interface{}
	}{
		{
			name: "gecko with sizes",
			obfs: map[string]interface{}{
				"type": "gecko", "password": "hunter2",
				"min_packet_size": float64(100), "max_packet_size": float64(1200),
			},
			wantMin: 100, wantMax: 1200,
		},
		{
			// What a decoded link holds: the URI scheme carries no sizes.
			name: "gecko without sizes",
			obfs: map[string]interface{}{"type": "gecko", "password": "hunter2"},
		},
		{
			name: "gecko with zero sizes",
			obfs: map[string]interface{}{
				"type": "gecko", "password": "hunter2",
				"min_packet_size": float64(0), "max_packet_size": float64(0),
			},
		},
		{
			// A row that kept the sizes after its type was switched back.
			name: "salamander ignores sizes",
			obfs: map[string]interface{}{
				"type": "salamander", "password": "hunter2",
				"min_packet_size": float64(100), "max_packet_size": float64(1200),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbounds := []map[string]interface{}{{
				"type": "hysteria2", "tag": "hy2", "server": "e.com",
				"server_port": float64(443), "password": "pw",
				"tls":  map[string]interface{}{"enabled": true, "server_name": "e.com"},
				"obfs": tt.obfs,
			}}
			raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
			if err != nil {
				t.Fatalf("ConvertToClashMeta: %v", err)
			}
			proxy := firstProxy(t, raw)
			if proxy["obfs"] != tt.obfs["type"] || proxy["obfs-password"] != "hunter2" {
				t.Errorf("obfs = %v / %v, want %v / hunter2:\n%s",
					proxy["obfs"], proxy["obfs-password"], tt.obfs["type"], raw)
			}
			for key, want := range map[string]interface{}{
				"obfs-min-packet-size": tt.wantMin,
				"obfs-max-packet-size": tt.wantMax,
			} {
				got, ok := proxy[key]
				if want == nil {
					if ok {
						t.Errorf("%s = %#v, want it absent:\n%s", key, got, raw)
					}
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v, want %#v:\n%s", key, got, want, raw)
				}
			}
		})
	}
}

// util repeats the two obfs type names instead of importing sing-box for them.
// This is where a rename upstream would show.
func TestHysteria2ObfsTypesMatchSingBox(t *testing.T) {
	if util.Hysteria2ObfsSalamander != C.Hysteria2ObfsTypeSalamander {
		t.Errorf("salamander = %q, sing-box has %q", util.Hysteria2ObfsSalamander, C.Hysteria2ObfsTypeSalamander)
	}
	if util.Hysteria2ObfsGecko != C.Hysteria2ObfsTypeGecko {
		t.Errorf("gecko = %q, sing-box has %q", util.Hysteria2ObfsGecko, C.Hysteria2ObfsTypeGecko)
	}
}

// With no config the client looks ECH up in DNS only under an explicit
// query_server_name; a config that does not convert leaves ech-opts out whole.
func TestClashEchOpts(t *testing.T) {
	configPem, keyPem, err := tls.ECHKeygenDefault("public.example.com")
	if err != nil {
		t.Fatalf("ECHKeygenDefault: %v", err)
	}
	lines := func(s string) []interface{} {
		var out []interface{}
		for _, l := range strings.Split(s, "\n") {
			out = append(out, l)
		}
		return out
	}
	cases := []struct {
		name string
		ech  map[string]interface{}
		want map[string]interface{} // nil = no ech-opts
	}{
		{"config", map[string]interface{}{"enabled": true, "config": lines(configPem), "query_server_name": "q.example.com"},
			map[string]interface{}{"enable": true, "config": util.ECHConfigBase64(lines(configPem))}},
		{"query name only", map[string]interface{}{"enabled": true, "query_server_name": "q.example.com"},
			map[string]interface{}{"enable": true, "query-server-name": "q.example.com"}},
		// Neither: ECH is left to the server's tolerance of plain TLS rather
		// than a DNS lookup that a key_path or config_path row cannot answer.
		{"neither", map[string]interface{}{"enabled": true}, nil},
		{"config_path only", map[string]interface{}{"enabled": true, "config_path": "/etc/ech.pem"}, nil},
		{"key pasted as config", map[string]interface{}{"enabled": true, "config": lines(keyPem)}, nil},
		{"off", map[string]interface{}{"enabled": false, "config": lines(configPem)}, nil},
	}
	for _, tc := range cases {
		outbounds := []map[string]interface{}{{
			"type": "trojan", "tag": "tr", "server": "e.com", "server_port": float64(443), "password": "pw",
			"tls": map[string]interface{}{"enabled": true, "server_name": "e.com", "ech": tc.ech},
		}}
		raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
		if err != nil {
			t.Fatalf("%s: ConvertToClashMeta: %v", tc.name, err)
		}
		got, _ := firstProxy(t, raw)["ech-opts"].(map[string]interface{})
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ech-opts = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestClashVlessEncryption(t *testing.T) {
	const enc = "mlkem768x25519plus.native.0rtt.AAAA"
	outbounds := []map[string]interface{}{{
		"type": "vless", "tag": "v", "server": "e.com", "server_port": float64(443),
		"uuid": "00000000-0000-4000-8000-000000000001", "encryption": enc,
	}}
	raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
	if err != nil {
		t.Fatalf("ConvertToClashMeta: %v", err)
	}
	if got := firstProxy(t, raw)["encryption"]; got != enc {
		t.Errorf("encryption = %v, want %q", got, enc)
	}
}

// Clash reads the Host through the same helper as links and the JSON
// subscription: the transport's own, else the SNI under TLS, never an empty one.
func TestClashWsHostFollowsSharedRule(t *testing.T) {
	tlsOn := map[string]interface{}{"enabled": true, "server_name": "cdn.example.com"}
	for _, tc := range []struct {
		name      string
		transport map[string]interface{}
		tls       map[string]interface{}
		want      interface{}
	}{
		{"own header", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": "own.example.com"}}, tlsOn, "own.example.com"},
		{"header list", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": []interface{}{"list.example.com"}}}, tlsOn, "list.example.com"},
		{"empty header", map[string]interface{}{"type": "ws", "headers": map[string]interface{}{"Host": ""}}, tlsOn, "cdn.example.com"},
		{"httpupgrade host", map[string]interface{}{"type": "httpupgrade", "host": "hu.example.com"}, tlsOn, "hu.example.com"},
		{"sni fallback", map[string]interface{}{"type": "ws"}, tlsOn, "cdn.example.com"},
		{"no tls", map[string]interface{}{"type": "ws"}, nil, nil},
	} {
		ob := map[string]interface{}{
			"type": "vless", "tag": "v", "server": "1.2.3.4", "server_port": float64(443),
			"uuid": "00000000-0000-4000-8000-000000000001", "transport": tc.transport,
		}
		if tc.tls != nil {
			ob["tls"] = tc.tls
		}
		outbounds := []map[string]interface{}{ob}
		raw, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		wsOpts, _ := firstProxy(t, raw)["ws-opts"].(map[string]interface{})
		headers, _ := wsOpts["headers"].(map[string]interface{})
		if got := headers["Host"]; got != tc.want {
			t.Errorf("%s: Host = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
