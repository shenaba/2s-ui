package util

import (
	"encoding/json"
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
