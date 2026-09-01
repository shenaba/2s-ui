package service

import (
	"encoding/json"
	"testing"
)

// NewClientConfig is the Go counterpart of randomConfigs() in
// frontend/src/types/clients.ts, and its own comment says the two must stay in
// step. Nothing enforced that, and snell drifted: it was added to the frontend
// and not here, so every client the Telegram bot created had no snell block.
//
// A missing block is silent all the way down -- fetchUsers filters the client
// out of the listener's user list (json_extract returns NULL) and the
// subscription emits no outbound for it -- so the client simply cannot connect
// and nothing says why. Worse, it is unrecoverable from the UI: updateConfigs
// only renames the keys a config already has, so no amount of re-saving adds
// the missing one.
func TestNewClientConfigCoversInboundTypesWithUsers(t *testing.T) {
	raw, err := NewClientConfig("alice")
	if err != nil {
		t.Fatalf("NewClientConfig: %v", err)
	}
	var config map[string]map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, inboundType := range inboundTypesWithUsers {
		if _, ok := config[inboundType]; !ok {
			t.Errorf("no credentials for %q: a client created through the bot cannot use that inbound, "+
				"and re-saving it will not add them", inboundType)
		}
	}
}

// The reverse direction: a block for something hasUser does not recognise is
// dead weight in every client row. shadowsocks16 is the one deliberate
// exception -- it is not an inbound type but the second config key
// ShadowsocksClientConfigKey picks between, by the listener's method.
func TestNewClientConfigCarriesNothingUnused(t *testing.T) {
	raw, err := NewClientConfig("alice")
	if err != nil {
		t.Fatalf("NewClientConfig: %v", err)
	}
	var config map[string]map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	service := &InboundService{}
	for key := range config {
		if key == "shadowsocks16" {
			continue
		}
		if !service.hasUser(key) {
			t.Errorf("credentials for %q, which no inbound type asks for", key)
		}
	}
}

// The per-client half of a snell listener. The psk lives on the inbound and is
// shared by everyone on it, so the userkey is the only thing that tells two
// clients apart -- an empty one would make them one identity.
func TestNewClientConfigSnellCarriesAUserkey(t *testing.T) {
	raw, err := NewClientConfig("alice")
	if err != nil {
		t.Fatalf("NewClientConfig: %v", err)
	}
	var config map[string]map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	snell := config["snell"]
	if name, _ := snell["name"].(string); name != "alice" {
		t.Errorf("snell name = %q, want the client's own name", name)
	}
	userkey, _ := snell["userkey"].(string)
	if userkey == "" {
		t.Error("snell userkey is empty; two clients would be one identity")
	}
	if _, hasPsk := snell["psk"]; hasPsk {
		t.Error("psk is the listener's, shared by every client, and must not be per-client")
	}
}
