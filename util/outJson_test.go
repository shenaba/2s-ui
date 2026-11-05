package util

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

func TestStripServerTlsFields(t *testing.T) {
	tests := []struct {
		name    string
		in      map[string]interface{}
		changed bool
		gone    []string
		kept    []string
	}{
		{
			name: "top-level server fields removed",
			in: map[string]interface{}{
				"enabled":          true,
				"server_name":      "example.com",
				"alpn":             []string{"h2"},
				"certificate_path": "/root/cert/example.com/fullchain.pem",
				"key":              []string{"-----BEGIN PRIVATE KEY-----"},
				"key_path":         "/root/cert/example.com/private.key",
				"acme":             map[string]interface{}{"domain": []string{"example.com"}},
				// sing-box 1.14's replacement for acme. It is a tag naming an
				// entry in certificate_providers, which a client's own config
				// does not have -- sing-box refuses to start on the dangling
				// reference.
				"certificate_provider": "letsencrypt",
			},
			changed: true,
			gone:    []string{"certificate_path", "key", "key_path", "acme", "certificate_provider"},
			kept:    []string{"enabled", "server_name", "alpn"},
		},
		{
			name: "ech private key removed, client ech fields kept",
			in: map[string]interface{}{
				"enabled": true,
				"ech": map[string]interface{}{
					"enabled":     true,
					"key":         []string{"ech-key"},
					"key_path":    "/root/ech.key",
					"config":      []string{"ech-config"},
					"config_path": "/etc/ech.cfg",
				},
			},
			changed: true,
			kept:    []string{"enabled", "ech"},
		},
		{
			name: "clean map untouched",
			in: map[string]interface{}{
				"enabled":     true,
				"server_name": "example.com",
				"certificate": []string{"-----BEGIN CERTIFICATE-----"},
				"utls":        map[string]interface{}{"enabled": true, "fingerprint": "safari"},
			},
			changed: false,
			kept:    []string{"enabled", "server_name", "certificate", "utls"},
		},
		{
			name:    "nil map is a no-op",
			in:      nil,
			changed: false,
		},
		{
			name: "non-map ech value ignored",
			in: map[string]interface{}{
				"ech": "not-a-map",
			},
			changed: false,
			kept:    []string{"ech"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripServerTlsFields(tt.in); got != tt.changed {
				t.Fatalf("changed = %v, want %v", got, tt.changed)
			}
			for _, k := range tt.gone {
				if _, ok := tt.in[k]; ok {
					t.Errorf("field %q should have been removed", k)
				}
			}
			for _, k := range tt.kept {
				if _, ok := tt.in[k]; !ok {
					t.Errorf("field %q should have been kept", k)
				}
			}
		})
	}

	t.Run("nested ech key removed but ech object survives", func(t *testing.T) {
		in := map[string]interface{}{
			"ech": map[string]interface{}{
				"enabled": true,
				"key":     "secret",
				"config":  []string{"cfg"},
			},
		}
		if !StripServerTlsFields(in) {
			t.Fatal("expected changed = true")
		}
		ech := in["ech"].(map[string]interface{})
		if _, ok := ech["key"]; ok {
			t.Error("ech.key should have been removed")
		}
		if _, ok := ech["config"]; !ok {
			t.Error("ech.config should have been kept")
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		in := map[string]interface{}{"certificate_path": "/x"}
		if !StripServerTlsFields(in) {
			t.Fatal("first call should report a change")
		}
		if StripServerTlsFields(in) {
			t.Fatal("second call should be a no-op")
		}
	})
}

// The hysteria client config carries the connection window and the MTU switch
// from the listener, but the stream window is the client's own -- as
// recv_window_conn and recv_window were before sing-box 1.14 renamed them.
// An out_json value the operator set is an override and must survive a save.
func TestFillOutJsonHysteriaQUICFields(t *testing.T) {
	newInbound := func(outJson string) *model.Inbound {
		return &model.Inbound{
			Type:    "hysteria",
			Tag:     "hy-in",
			Options: json.RawMessage(`{"listen_port":443,"up_mbps":100,"down_mbps":200,` +
				`"connection_receive_window":15728640,"disable_path_mtu_discovery":true,` +
				`"stream_receive_window":67108864}`),
			OutJson: json.RawMessage(outJson),
		}
	}

	t.Run("seeded from the listener", func(t *testing.T) {
		inbound := newInbound(`{}`)
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		if out["connection_receive_window"] != float64(15728640) {
			t.Errorf("connection window must carry over, got %v", out)
		}
		if out["disable_path_mtu_discovery"] != true {
			t.Errorf("the MTU switch must carry over, got %v", out)
		}
		// The listener's stream window says what the server receives, not what
		// the client should ask for.
		if _, ok := out["stream_receive_window"]; ok {
			t.Errorf("the stream window is the client's own, got %v", out)
		}
		// The bandwidths swap sides.
		if out["up_mbps"] != float64(200) || out["down_mbps"] != float64(100) {
			t.Errorf("bandwidths must swap, got %v", out)
		}
	})

	t.Run("an out_json override wins", func(t *testing.T) {
		inbound := newInbound(`{"connection_receive_window":1234,"stream_receive_window":4321}`)
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		if out["connection_receive_window"] != float64(1234) {
			t.Errorf("the operator's value must survive a save, got %v", out)
		}
		if out["stream_receive_window"] != float64(4321) {
			t.Errorf("the client's own stream window must survive, got %v", out)
		}
	})
}

// migrateHysteriaQUICFields renames the pre-1.14 window names in every stored
// row once, so a client config only still carries one if it was hand-edited or
// restored from an older backup. Folding it onto the modern name lets such a
// row heal itself on the next save instead of quietly losing the value.
func TestFillOutJsonHysteriaFoldsDeprecatedNames(t *testing.T) {
	inbound := &model.Inbound{
		Type: "hysteria", Tag: "hy-in",
		Options: json.RawMessage(`{"listen_port":443,"up_mbps":100,"down_mbps":200}`),
		OutJson: json.RawMessage(`{
			"recv_window_conn": 111,
			"recv_window": 222,
			"disable_mtu_discovery": true,
			"stream_receive_window": 999
		}`),
	}
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}

	for _, deprecated := range []string{"recv_window_conn", "recv_window", "disable_mtu_discovery"} {
		if _, ok := out[deprecated]; ok {
			t.Errorf("%q must not survive beside its replacement, got %v", deprecated, out)
		}
	}
	if out["connection_receive_window"] != float64(111) {
		t.Errorf("recv_window_conn should fold onto the connection window, got %v", out)
	}
	if out["disable_path_mtu_discovery"] != true {
		t.Errorf("disable_mtu_discovery should fold onto its replacement, got %v", out)
	}
	// The modern name was already set, so it wins and recv_window is dropped --
	// the same precedence sing-box applies when it reads both.
	if out["stream_receive_window"] != float64(999) {
		t.Errorf("the modern name must win over the deprecated one, got %v", out)
	}
}

// snell has no TLS options on either side and its v5 obfs options are not v6
// options, so either one reaching the client config makes sing-box reject the
// whole thing rather than just that outbound.
func TestFillOutJsonSnellDropsFieldsItCannotCarry(t *testing.T) {
	// TlsId > 0 so addTls actually writes a tls block before the per-protocol
	// switch runs -- with no reference the block is dropped anyway and the
	// assertion below would pass without testing anything.
	inbound := &model.Inbound{
		Type: "snell", Tag: "snell-in",
		TlsId: 1,
		Tls: &model.Tls{
			Server: json.RawMessage(`{"enabled": true, "server_name": "example.com"}`),
			Client: json.RawMessage(`{"enabled": true}`),
		},
		Options: json.RawMessage(`{"listen_port":443,"version":6,"psk":"shared","mode":"default"}`),
		OutJson: json.RawMessage(`{"obfs_mode": "http", "obfs_host": "example.com"}`),
	}
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}

	for _, dropped := range []string{"tls", "obfs_mode", "obfs_host"} {
		if _, ok := out[dropped]; ok {
			t.Errorf("%q is not a snell outbound option, got %v", dropped, out)
		}
	}
	if out["version"] != float64(6) || out["psk"] != "shared" || out["mode"] != "default" {
		t.Errorf("the options snell does carry must survive, got %v", out)
	}
}

// addTls reads the same TLS row prepareTls does, on the write path: FillOutJson
// runs on every inbound save and on every certificate renewal through
// UpdateOutJsons, and the renewal path has no recover in front of it. All four
// assertions here used to be bare.
func TestFillOutJsonToleratesLopsidedTls(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string // the nested object that must survive
	}{
		{
			// The server half carries reality, the client half does not.
			name:   "reality on the server side only",
			server: `{"enabled":true,"reality":{"enabled":true,"short_id":["ab"]}}`,
			want:   "reality",
		},
		{
			name:   "ech on the server side only",
			server: `{"enabled":true,"ech":{"enabled":true,"key":["x"]}}`,
			want:   "ech",
		},
		{
			// No enabled key at all: the guard itself asserted on it.
			name:   "reality without enabled",
			server: `{"enabled":true,"reality":{"short_id":["ab"]}}`,
		},
		{
			name:   "ech without enabled",
			server: `{"enabled":true,"ech":{"key":["x"]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Type: "trojan", Tag: "tr-in",
				TlsId: 1,
				Tls: &model.Tls{
					Server: json.RawMessage(tt.server),
					Client: json.RawMessage(`{"enabled":true}`),
				},
				Options: json.RawMessage(`{"listen_port":443}`),
				OutJson: json.RawMessage(`{}`),
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
				t.Fatalf("tls block must survive, got %v", out)
			}
			if tt.want == "" {
				return
			}
			if _, ok := tls[tt.want].(map[string]interface{}); !ok {
				t.Errorf("%q must be carried over to the client config, got %v", tt.want, tls)
			}
		})
	}
}

// FillOutJson merges into the stored out_json, so a key it stops writing has to
// be removed explicitly. naiveOut never did, and clearing the QUIC congestion
// control field left "quic": true and the old algorithm behind (upstream #1243).
func TestFillOutJsonNaiveClearsStaleQuicFields(t *testing.T) {
	newInbound := func(options string) *model.Inbound {
		return &model.Inbound{
			Type: "naive", Tag: "naive-in",
			Options: json.RawMessage(options),
			// What a previous save with bbr_standard selected left behind.
			OutJson: json.RawMessage(`{"quic":true,"quic_congestion_control":"bbr"}`),
		}
	}

	t.Run("the field is cleared", func(t *testing.T) {
		inbound := newInbound(`{"listen_port":443}`)
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		for _, stale := range []string{"quic", "quic_congestion_control"} {
			if _, ok := out[stale]; ok {
				t.Errorf("%q must not survive clearing the field, got %v", stale, out)
			}
		}
	})

	// An empty string is what the form posts for a cleared select, and it used
	// to satisfy the type assertion and write "quic": true with no algorithm.
	t.Run("an empty value is not a selection", func(t *testing.T) {
		inbound := newInbound(`{"listen_port":443,"quic_congestion_control":""}`)
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		if _, ok := out["quic"]; ok {
			t.Errorf("an empty selection must not turn quic on, got %v", out)
		}
	})

	t.Run("a real selection still lands", func(t *testing.T) {
		inbound := newInbound(`{"listen_port":443,"quic_congestion_control":"bbr2_variant"}`)
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		if out["quic"] != true || out["quic_congestion_control"] != "bbr2" {
			t.Errorf("the selected algorithm must be written, got %v", out)
		}
	})
}

// server_ports is the operator's own free-text field and out_json reaches the
// JSON subscription verbatim, so a shape sing-box will not start on has to be
// caught on save. sing-box parses each entry as a range and refuses a bare port
// at start-up, not at parse: `443` unmarshals and then dies on
// `bad port range: 443` in every subscriber's client.
func TestFillOutJsonNormalizesPortHoppingRanges(t *testing.T) {
	tests := []struct {
		name    string
		outJson string
		want    interface{}
	}{
		{"a bare single port becomes its own range",
			`{"server_ports":["443","20000:30000"]}`, []interface{}{"443:443", "20000:30000"}},
		{"a dash range becomes a colon range",
			`{"server_ports":["20000-30000"]}`, []interface{}{"20000:30000"}},
		{"an already correct row survives untouched",
			`{"server_ports":["20000:30000"]}`, []interface{}{"20000:30000"}},
		{"nothing usable is dropped rather than served",
			`{"server_ports":["nonsense"]}`, nil},
		// Atoi called these ports and sing-box does not: it reads the same text
		// with ParseUint into a uint16, so both reach NewBox as
		// `bad port range` after the document has already parsed.
		{"a port above 65535 is not a port",
			`{"server_ports":["99999"]}`, nil},
		{"a signed number is not a port",
			`{"server_ports":["+443"]}`, nil},
		{"the top of the range is bounded too",
			`{"server_ports":["20000:99999"]}`, nil},
		{"65535 is still a port",
			`{"server_ports":["65535"]}`, []interface{}{"65535:65535"}},
		{"no port hopping at all stays absent",
			`{}`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Type: "hysteria2", Tag: "hy2-in",
				Options: json.RawMessage(`{"listen_port":443}`),
				OutJson: json.RawMessage(tt.outJson),
			}
			if err := FillOutJson(inbound, "example.com"); err != nil {
				t.Fatal(err)
			}
			var out map[string]interface{}
			if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
				t.Fatal(err)
			}
			got, present := out["server_ports"]
			if tt.want == nil {
				if present {
					t.Errorf("server_ports = %#v, want the key absent", got)
				}
				return
			}
			gotJson, _ := json.Marshal(got)
			wantJson, _ := json.Marshal(tt.want)
			if string(gotJson) != string(wantJson) {
				t.Errorf("server_ports = %s, want %s", gotJson, wantJson)
			}
		})
	}
}

// sing-box 1.13 removed these two ECH options and rejects a client config with
// either one set. They used to be copied across from the server half, so a
// stale true there broke every subscription the inbound fed.
func TestFillOutJsonDropsLegacyECHOptions(t *testing.T) {
	inbound := &model.Inbound{
		Type: "trojan", Tag: "tr-in",
		TlsId: 1,
		Tls: &model.Tls{
			Server: json.RawMessage(`{"enabled":true,"ech":{"enabled":true,"key":["x"],"pq_signature_schemes_enabled":true,"dynamic_record_sizing_disabled":true}}`),
			Client: json.RawMessage(`{"enabled":true}`),
		},
		Options: json.RawMessage(`{"listen_port":443}`),
		OutJson: json.RawMessage(`{}`),
	}
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}
	tls, _ := out["tls"].(map[string]interface{})
	ech, ok := tls["ech"].(map[string]interface{})
	if !ok || ech["enabled"] != true {
		t.Fatalf("ech must still be switched on for the client, got %v", tls)
	}
	for _, removed := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"} {
		if _, present := ech[removed]; present {
			t.Errorf("%q must not reach the client config, got %v", removed, ech)
		}
	}
}

// A ShadowTLS client verifies the certificate the handshake server answers
// with, so it has to send that server's name. Left out, sing-box sends the
// panel's own address and every connection fails verification.
func TestFillOutJsonShadowTlsServerName(t *testing.T) {
	cases := []struct {
		name, handshake, want string
	}{
		{"domain", "www.example.com", "www.example.com"},
		{"ip has no name to send", "203.0.113.7", ""},
		{"ipv6", "2001:db8::1", ""},
		{"bracketed ipv6", "[2001:db8::1]", ""},
		{"zoned ipv6", "fe80::1%eth0", ""},
		{"unset", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inbound := &model.Inbound{
				Type: "shadowtls", Tag: "stls-in",
				Options: json.RawMessage(fmt.Sprintf(
					`{"listen_port":443,"version":3,"handshake":{"server":%q,"server_port":443}}`, c.handshake)),
				OutJson: json.RawMessage(`{}`),
			}
			if err := FillOutJson(inbound, "panel.example.com"); err != nil {
				t.Fatal(err)
			}
			var out map[string]interface{}
			if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
				t.Fatal(err)
			}
			tls, _ := out["tls"].(map[string]interface{})
			if tls["enabled"] != true {
				t.Fatalf("tls = %v, want enabled", out["tls"])
			}
			got, _ := tls["server_name"].(string)
			if got != c.want {
				t.Errorf("server_name = %q, want %q", got, c.want)
			}
		})
	}
}

// The client side of a TLS template also carries what an OpenConnect or
// OpenVPN endpoint needs for its own connection: the client certificate it
// presents, the CA and fingerprints it checks its server against. An inbound
// sharing the template must not hand any of that to its subscribers -- the
// certificate is a credential, the paths name panel-host files, and
// peer_fingerprint is a field sing-box clients reject -- while the stored
// template keeps all of it, since the endpoint projection reads it from there.
func TestFillOutJsonKeepsTheEndpointTlsToItself(t *testing.T) {
	client := `{"enabled":true,"certificate":"VPN-CA","peer_fingerprint":["ab12"],` +
		`"client_certificate":["c"],"client_certificate_path":"/p/c.pem","client_key":["k"],"client_key_path":"/p/k.pem"}`
	build := func(server string) map[string]interface{} {
		t.Helper()
		inbound := &model.Inbound{
			Type: "trojan", Tag: "tr-in",
			TlsId: 1,
			Tls: &model.Tls{
				Server: json.RawMessage(server),
				Client: json.RawMessage(client),
			},
			Options: json.RawMessage(`{"listen_port":443}`),
			OutJson: json.RawMessage(`{}`),
		}
		if err := FillOutJson(inbound, "example.com"); err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
			t.Fatal(err)
		}
		tls, _ := out["tls"].(map[string]interface{})
		return tls
	}

	// A provider-issued certificate: nothing for a subscriber to pin or trust
	// beyond the system roots, so no certificate at all -- not the VPN's CA.
	tls := build(`{"enabled":true,"server_name":"example.com"}`)
	if tls["server_name"] != "example.com" {
		t.Fatalf("the rest of the client config must still be built, got %v", tls)
	}
	for _, field := range []string{"certificate", "peer_fingerprint", "client_certificate", "client_certificate_path", "client_key", "client_key_path"} {
		if _, present := tls[field]; present {
			t.Errorf("%q must not reach a subscriber, got %v", field, tls)
		}
	}

	// The inbound's own certificate is what a subscriber trusts.
	tls = build(`{"enabled":true,"server_name":"example.com","certificate":["SRV"]}`)
	if got := fmt.Sprint(tls["certificate"]); got != "[SRV]" {
		t.Errorf("certificate = %v, want the inbound's own [SRV]", tls["certificate"])
	}

	stored := map[string]interface{}{}
	if err := json.Unmarshal([]byte(client), &stored); err != nil {
		t.Fatal(err)
	}
	if StripServerTlsFields(stored) {
		t.Errorf("cleaning the stored template must keep the endpoint's TLS, left %v", stored)
	}
}

func TestFillOutJsonHysteria2Realm(t *testing.T) {
	inbound := &model.Inbound{
		Type: "hysteria2",
		Tag:  "hy2-in",
		Options: json.RawMessage(`{"listen_port": 443, "realm": {
			"server_url": "https://realm.example.com", "token": "server-token", "realm_id": "home",
			"stun_servers": ["stun.example.com"], "port_mapping": {"enabled": true}}}`),
		OutJson: json.RawMessage(`{"server_ports": ["2000:3000"], "hop_interval": "30s", "realm": {"token": "client-token", "realm_id": "stale"}}`),
	}

	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"server", "server_port", "server_ports", "hop_interval"} {
		if v, ok := out[key]; ok {
			t.Errorf("%s conflicts with realm, got %v", key, v)
		}
	}
	realm, _ := out["realm"].(map[string]interface{})
	if realm["server_url"] != "https://realm.example.com" || realm["realm_id"] != "home" {
		t.Errorf("realm not taken from the inbound: %v", realm)
	}
	if realm["token"] != "client-token" {
		t.Errorf("token = %v, want the client token kept from out_json", realm["token"])
	}
	if _, ok := realm["port_mapping"]; ok {
		t.Errorf("port_mapping is the server's gateway setting and must not reach clients")
	}

	// Dropping the realm on the inbound brings the server address back.
	inbound.Options = json.RawMessage(`{"listen_port": 443}`)
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	out = nil
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["realm"]; ok {
		t.Errorf("realm survived its removal from the inbound")
	}
	if out["server"] != "example.com" {
		t.Errorf("server = %v, want example.com", out["server"])
	}
}
