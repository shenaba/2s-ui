package sub

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

	"gopkg.in/yaml.v3"
)

// seedMieruClient gives one client a mieru inbound and a vless one, the vless
// one so there is something that must survive in the sing-box subscription.
func seedMieruClient(t *testing.T, subId string) {
	t.Helper()
	db := database.GetDB()
	mieru := &model.Inbound{
		Type: "mieru", Tag: "mieru-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"mieru","tag":"mieru-in","server":"example.com","server_port":2999,"transport":"UDP"}`),
		Options: json.RawMessage(`{"listen_port":2999,"transport":"UDP"}`),
	}
	vless := &model.Inbound{
		Type: "vless", Tag: "vless-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"vless-in","server":"example.com","server_port":443}`),
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	for _, in := range []*model.Inbound{mieru, vless} {
		if err := db.Create(in).Error; err != nil {
			t.Fatalf("seed %s: %v", in.Tag, err)
		}
	}
	client := &model.Client{
		Enable: true, Name: subId,
		Config: json.RawMessage(`{"mieru":{"name":"` + subId + `","password":"pw"},` +
			`"vless":{"name":"` + subId + `","uuid":"5bdbac27-7a82-42c5-9ef4-751f64bd15e4"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d,%d]`, mieru.Id, vless.Id)),
		Links:    json.RawMessage(`[]`),
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

// Upstream sing-box has no mieru outbound and refuses a whole config over one
// type it does not know, so the sing-box subscription leaves mieru out -- and
// keeps everything else, which is the point of leaving it out.
func TestGetJson_LeavesMieruOut(t *testing.T) {
	setupSubDB(t)
	seedMieruClient(t, "mieru-sub")

	raw, _, err := (&JsonService{}).GetJson("mieru-sub", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	sawVless := false
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] == "mieru" {
			t.Fatalf("mieru outbound in the sing-box subscription:\n%s", *raw)
		}
		if ob["type"] == "vless" {
			sawVless = true
		}
		if refs, ok := ob["outbounds"].([]interface{}); ok {
			for _, r := range refs {
				if r == "mieru-in" {
					t.Fatalf("group %v still names the dropped mieru outbound:\n%s", ob["tag"], *raw)
				}
			}
		}
	}
	if !sawVless {
		t.Fatalf("the vless node went missing with mieru:\n%s", *raw)
	}
}

// mihomo does speak mieru, so the Clash subscription carries it, logged in as
// the client by name.
func TestGetClash_CarriesMieru(t *testing.T) {
	setupSubDB(t)
	seedMieruClient(t, "mieru-clash")

	raw, _, err := (&ClashService{}).GetClash("mieru-clash")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(*raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	proxies, _ := cfg["proxies"].([]interface{})
	var mieru map[string]interface{}
	for _, p := range proxies {
		if m, ok := p.(map[string]interface{}); ok && m["type"] == "mieru" {
			mieru = m
		}
	}
	if mieru == nil {
		t.Fatalf("no mieru proxy:\n%s", *raw)
	}
	want := map[string]interface{}{
		"server": "example.com", "port": 2999, "transport": "UDP",
		"username": "mieru-clash", "password": "pw",
	}
	for key, value := range want {
		if mieru[key] != value {
			t.Errorf("%s = %#v, want %#v", key, mieru[key], value)
		}
	}
	if _, ok := mieru["tls"]; ok {
		t.Errorf("mieru has no TLS, got %v", mieru)
	}
	if strings.Contains(*raw, "traffic-pattern") {
		t.Errorf("traffic-pattern written for an inbound without one:\n%s", *raw)
	}
}

// A mieru outbound dialling through a ShadowTLS one takes it along when it is
// the only thing riding it: the link that kept that carrier out of the groups
// goes with the rider, and a bare ShadowTLS outbound is no proxy. A carrier
// something else still rides stays, and so does one nothing ever rode.
func TestDropSingBoxUnsupported_TakesOrphanedCarriers(t *testing.T) {
	mk := func(typ, tag string) map[string]interface{} {
		return map[string]interface{}{"type": typ, "tag": tag}
	}
	mieruA, carrierA := mk("mieru", "mieru-a"), mk("shadowtls", "stls-a")
	mieruB, vlessB, carrierB := mk("mieru", "mieru-b"), mk("vless", "vless-b"), mk("shadowtls", "stls-b")
	lone := mk("shadowtls", "stls-lone")
	plain := mk("trojan", "trojan")

	outbounds := []map[string]interface{}{mieruA, carrierA, mieruB, vlessB, carrierB, lone, plain}
	tags := []string{"mieru-a", "stls-a", "mieru-b", "vless-b", "stls-b", "stls-lone", "trojan"}
	links := []detourLink{
		{from: mieruA, to: carrierA},
		{from: mieruB, to: carrierB},
		{from: vlessB, to: carrierB},
	}

	outbounds, tags, links = dropSingBoxUnsupported(outbounds, tags, links)

	if got, want := strings.Join(tags, ","), "vless-b,stls-b,stls-lone,trojan"; got != want {
		t.Fatalf("kept %s, want %s", got, want)
	}
	for i, out := range outbounds {
		if out["tag"] != tags[i] {
			t.Errorf("outbound %d is %v, its tag says %s", i, out["tag"], tags[i])
		}
	}
	if len(links) != 1 || links[0].from["tag"] != "vless-b" || links[0].to["tag"] != "stls-b" {
		t.Fatalf("kept links %v, want only vless-b -> stls-b", links)
	}
	// The surviving carrier is still hidden from the groups, as before.
	if got, want := strings.Join(groupMembers(tags, links), ","), "vless-b,stls-lone,trojan"; got != want {
		t.Errorf("group members %s, want %s", got, want)
	}
}
