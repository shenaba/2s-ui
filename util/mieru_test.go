package util

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"

	"github.com/enfein/mieru/v3/pkg/appctl"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// A traffic pattern as the inbound form stores it: base64 of the protobuf.
const testTrafficPattern = "CAESAhABGgIIARoCCAI="

// The share link is mieru's own mierus:// form, and the proof it is right is
// that mieru's own parser takes it: the port lives in the query beside its
// transport, not in the authority, and the profile name is required.
func TestMieruLinkParsesInMieru(t *testing.T) {
	links := LinkGenerator(
		json.RawMessage(`{"mieru":{"name":"alice","password":"p@ss word"}}`),
		&model.Inbound{
			Type:    "mieru",
			Tag:     "mieru-in",
			Addrs:   json.RawMessage("null"),
			Options: json.RawMessage(`{"listen_port":2999,"transport":"UDP","traffic_pattern":"` + testTrafficPattern + `"}`),
			OutJson: json.RawMessage(`{"multiplexing":"MULTIPLEXING_HIGH"}`),
		},
		"example.com", "")
	if len(links) != 1 {
		t.Fatalf("got %d links %v, want 1", len(links), links)
	}

	profile, err := appctl.URLToClientProfile(links[0])
	if err != nil {
		t.Fatalf("mieru refuses %q: %v", links[0], err)
	}
	if profile.GetUser().GetName() != "alice" || profile.GetUser().GetPassword() != "p@ss word" {
		t.Errorf("user %v, want alice with her password", profile.GetUser())
	}
	if profile.GetProfileName() != "mieru-in" {
		t.Errorf("profile %q, want the node's remark", profile.GetProfileName())
	}
	servers := profile.GetServers()
	if len(servers) != 1 || servers[0].GetDomainName() != "example.com" {
		t.Fatalf("servers %v, want example.com", servers)
	}
	bindings := servers[0].GetPortBindings()
	if len(bindings) != 1 || bindings[0].GetPort() != 2999 || bindings[0].GetProtocol() != mierupb.TransportProtocol_UDP {
		t.Errorf("port bindings %v, want 2999/UDP", bindings)
	}
	if profile.GetMultiplexing().GetLevel() != mierupb.MultiplexingLevel_MULTIPLEXING_HIGH {
		t.Errorf("multiplexing %v, want the level picked on the client side", profile.GetMultiplexing())
	}
	if profile.GetTrafficPattern() == nil {
		t.Error("the traffic pattern was dropped; a client without the server's cannot connect")
	}

	u, _ := url.Parse(links[0])
	if u.Port() != "" {
		t.Errorf("port in the authority of %q; mierus carries it in the query", links[0])
	}
}

// A client missing the mieru block -- one the backfill could not reach -- gets
// no link rather than one with an empty user that mieru refuses.
func TestMieruLinkNeedsCredentials(t *testing.T) {
	links := LinkGenerator(
		json.RawMessage(`{"trojan":{"password":"x"}}`),
		&model.Inbound{Type: "mieru", Tag: "m", Addrs: json.RawMessage("null"), Options: json.RawMessage(`{"listen_port":1}`)},
		"example.com", "")
	if len(links) != 0 {
		t.Errorf("links %v for a client without mieru credentials", links)
	}
}

func TestFillOutJsonMieru(t *testing.T) {
	inbound := &model.Inbound{
		Type: "mieru", Tag: "mieru-in",
		Options: json.RawMessage(`{"listen_port":2999,"transport":"UDP","traffic_pattern":"` + testTrafficPattern + `","users":[{"name":"a","password":"b"}]}`),
		OutJson: json.RawMessage(`{"tls":{"enabled":true}}`),
	}
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "mieru" || out["transport"] != "UDP" || out["traffic_pattern"] != testTrafficPattern {
		t.Errorf("out_json %v", out)
	}
	if out["server_port"] != float64(2999) {
		t.Errorf("server_port %v, want 2999", out["server_port"])
	}
	for _, dropped := range []string{"tls", "users"} {
		if _, ok := out[dropped]; ok {
			t.Errorf("%q leaked into the client half: %v", dropped, out)
		}
	}

	// No transport on the inbound is TCP, the inbound's own default.
	inbound.Options = json.RawMessage(`{"listen_port":2999}`)
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(inbound.OutJson, &out)
	if out["transport"] != "TCP" {
		t.Errorf("transport %v, want TCP", out["transport"])
	}
}

// The listener takes the transport name in either case. Whatever tells a
// client which one to use has to read it the same way, or an inbound stored as
// "udp" listens on UDP while every link and config says TCP.
func TestMieruTransportIgnoresCase(t *testing.T) {
	for in, want := range map[string]string{"UDP": "UDP", "udp": "UDP", "Udp": "UDP", "TCP": "TCP", "tcp": "TCP", "": "TCP"} {
		if got := MieruTransport(in); got != want {
			t.Errorf("MieruTransport(%q) = %s, want %s", in, got, want)
		}
	}
	if got := MieruTransport(nil); got != "TCP" {
		t.Errorf("MieruTransport(nil) = %s, want TCP", got)
	}

	inbound := &model.Inbound{
		Type: "mieru", Tag: "mieru-in",
		Addrs:   json.RawMessage("null"),
		Options: json.RawMessage(`{"listen_port":2999,"transport":"udp"}`),
		OutJson: json.RawMessage(`{}`),
	}
	links := LinkGenerator(json.RawMessage(`{"mieru":{"name":"alice","password":"pw"}}`), inbound, "example.com", "")
	if len(links) != 1 || !strings.Contains(links[0], "protocol=UDP") {
		t.Errorf("link %v does not carry protocol=UDP", links)
	}
	if err := FillOutJson(inbound, "example.com"); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &out); err != nil {
		t.Fatal(err)
	}
	if out["transport"] != "UDP" {
		t.Errorf("out_json transport %v, want UDP", out["transport"])
	}
}
