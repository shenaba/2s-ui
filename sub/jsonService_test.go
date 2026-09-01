package sub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"

	"gopkg.in/yaml.v3"
)

// setupSubDB gives each test its own sqlite file. The connection has to be
// closed explicitly: there is no database.CloseDB, and on Windows t.TempDir's
// cleanup fails to remove a file that is still open.
func setupSubDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

const (
	replicaOutJson = `{"type":"vless","tag":"vless-node","server":"jp.example.com","server_port":16600,` +
		`"tls":{"enabled":true,"reality":{"enabled":true,"public_key":"pk","short_id":"a1"},` +
		`"server_name":"www.cloudflare.com","utls":{"enabled":true,"fingerprint":"chrome"}},"transport":{}}`

	// What refreshNodeLinks folds into client.Links for that replica.
	replicaLink = `vless://5bdbac27-7a82-42c5-9ef4-751f64bd15e4@jp.example.com:16600` +
		`?security=reality&pbk=pk&sid=a1&sni=www.cloudflare.com&fp=chrome&flow=xtls-rprx-vision#vless-node`

	clientVlessConfig = `{"vless":{"uuid":"5bdbac27-7a82-42c5-9ef4-751f64bd15e4","flow":"xtls-rprx-vision"}}`
)

// seedNodeReplicaClient reproduces the state the panel reaches after adopting a
// node inbound and binding a client to it: a replica row (node_id set, tls_id
// cleared by adoption) plus the "[node] " external link reconciliation writes.
func seedNodeReplicaClient(t *testing.T, subId string) {
	t.Helper()
	db := database.GetDB()

	nodeId := uint(1)
	replica := &model.Inbound{
		Type:    "vless",
		Tag:     "vless-node",
		NodeId:  &nodeId,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(replicaOutJson),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(replica).Error; err != nil {
		t.Fatalf("seed replica: %v", err)
	}

	links := fmt.Sprintf(`[{"remark":"[jp2] vless-node","type":"external","uri":%q}]`, replicaLink)
	client := &model.Client{
		Enable:   true,
		Name:     subId,
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, replica.Id)),
		Links:    json.RawMessage(links),
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

func outboundsOf(t *testing.T, raw string) []map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	list, ok := cfg["outbounds"].([]interface{})
	if !ok {
		t.Fatalf("config has no outbounds array: %s", raw)
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// A client bound to a node replica must get that route exactly once. The replica
// row and the "[node] " external link describe the same route, and emitting both
// makes sing-box reject the whole config with "duplicate outbound tag".
func TestGetJson_NodeReplicaEmittedOnce(t *testing.T) {
	setupSubDB(t)
	seedNodeReplicaClient(t, "subnode")

	raw, _, err := (&JsonService{}).GetJson("subnode", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}

	// Count by type, not by tag: the tag-uniqueness backstop would rename a
	// second copy to "vless-node-2" and a tag-only assertion would miss it.
	var matched []map[string]interface{}
	routes := 0
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] == "vless" {
			routes++
		}
		if ob["tag"] == "vless-node" {
			matched = append(matched, ob)
		}
	}
	if routes != 1 {
		t.Fatalf("node route emitted %d times, want 1:\n%s", routes, *raw)
	}
	if len(matched) != 1 {
		t.Fatalf("vless-node emitted %d times, want 1:\n%s", len(matched), *raw)
	}
	// The surviving copy must be the link-derived one. The replica row loses
	// flow (adoption clears tls_id), and reality without vision cannot connect.
	if matched[0]["flow"] != "xtls-rprx-vision" {
		t.Fatalf("surviving outbound must keep flow, got %v", matched[0]["flow"])
	}

	// The groups must reference it once too, or urltest probes the same route twice.
	for _, ob := range outboundsOf(t, *raw) {
		tag, _ := ob["tag"].(string)
		if tag != "proxy" && tag != "auto" {
			continue
		}
		refs, _ := ob["outbounds"].([]interface{})
		count := 0
		for _, r := range refs {
			if r == "vless-node" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("group %q references vless-node %d times, want 1", tag, count)
		}
	}
}

// Clash shares getData/getOutbounds with the JSON subscription, and mihomo
// rejects a config with a duplicate proxy name just as sing-box does.
func TestGetClash_NodeReplicaEmittedOnce(t *testing.T) {
	setupSubDB(t)
	seedNodeReplicaClient(t, "subnode")

	raw, _, err := (&ClashService{}).GetClash("subnode")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if got := strings.Count(*raw, "name: vless-node"); got != 1 {
		t.Fatalf("proxy name emitted %d times, want 1:\n%s", got, *raw)
	}
}

// Filtering the replica out can leave a client with no routes at all — it only
// references node inbounds and reconciliation has not written their links yet.
// The config still has to import: urltest with an empty (or null) outbounds list
// is rejected outright, which is worse than the duplicate this fix removed.
func TestGetJson_NoRoutesStillImports(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	nodeId := uint(1)
	replica := &model.Inbound{
		Type: "vless", Tag: "vless-node", NodeId: &nodeId,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(replicaOutJson),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(replica).Error; err != nil {
		t.Fatalf("seed replica: %v", err)
	}
	client := &model.Client{
		Enable: true, Name: "subempty",
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, replica.Id)),
		Links:    json.RawMessage(`[]`), // reconcile has not run
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	raw, _, err := (&JsonService{}).GetJson("subempty", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	// A literal null is what a *[]string over a nil slice produces — assert on
	// the text too, since a decoded nil and an empty list look alike in Go.
	if strings.Contains(*raw, `"outbounds": null`) {
		t.Fatalf("no outbounds list may serialise to null:\n%s", *raw)
	}
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] == "urltest" {
			t.Fatalf("urltest must not be emitted with no routes to probe:\n%s", *raw)
		}
		if ob["tag"] == "proxy" {
			refs, _ := ob["outbounds"].([]interface{})
			if len(refs) == 0 {
				t.Fatalf("proxy selector must offer at least direct:\n%s", *raw)
			}
			for _, r := range refs {
				if r == "auto" {
					t.Fatalf("proxy must not reference the absent urltest:\n%s", *raw)
				}
			}
		}
	}
}

// Clash twin of TestGetJson_NoRoutesStillImports: a url-test group whose
// proxies list is empty fails mihomo's config parse just as an empty urltest
// fails sing-box's, so the default groups have to degrade — no Auto, and the
// Proxy selector falling back to the built-in DIRECT.
func TestGetClash_NoRoutesStillImports(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	nodeId := uint(1)
	replica := &model.Inbound{
		Type: "vless", Tag: "vless-node", NodeId: &nodeId,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(replicaOutJson),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(replica).Error; err != nil {
		t.Fatalf("seed replica: %v", err)
	}
	client := &model.Client{
		Enable: true, Name: "subclashempty",
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, replica.Id)),
		Links:    json.RawMessage(`[]`), // reconcile has not run
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	raw, _, err := (&ClashService{}).GetClash("subclashempty")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(*raw), &cfg); err != nil {
		t.Fatalf("unmarshal clash yaml: %v", err)
	}
	groups, _ := cfg["proxy-groups"].([]interface{})
	if len(groups) == 0 {
		t.Fatalf("default groups must still be injected:\n%s", *raw)
	}
	var proxyGroup map[string]interface{}
	for _, g := range groups {
		gm, _ := g.(map[string]interface{})
		if gm == nil {
			continue
		}
		if gm["name"] == "Auto" {
			t.Fatalf("Auto must not be emitted with no routes to probe:\n%s", *raw)
		}
		if proxies, _ := gm["proxies"].([]interface{}); len(proxies) == 0 {
			t.Fatalf("group %v has an empty proxies list, which mihomo rejects:\n%s", gm["name"], *raw)
		}
		if gm["name"] == "Proxy" {
			proxyGroup = gm
		}
	}
	if proxyGroup == nil {
		t.Fatalf("Proxy group missing:\n%s", *raw)
	}
	if proxies, _ := proxyGroup["proxies"].([]interface{}); len(proxies) != 1 || proxies[0] != "DIRECT" {
		t.Fatalf("Proxy must fall back to the built-in DIRECT alone, got %v", proxies)
	}
}

// The default proxy/auto/direct tags are injected after the routes are named, so
// a route carrying one of those names collides with them just as fatally as two
// routes colliding with each other.
func TestGetJson_TagCollidingWithDefaultIsRenamed(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	local := &model.Inbound{
		Type: "vless", Tag: "direct",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"direct","server":"1.2.3.4","server_port":443,"transport":{}}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(local).Error; err != nil {
		t.Fatalf("seed local inbound: %v", err)
	}
	client := &model.Client{
		Enable: true, Name: "subdefault",
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, local.Id)),
		Links:    json.RawMessage(`[]`),
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	raw, _, err := (&JsonService{}).GetJson("subdefault", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	seen := map[string]int{}
	for _, ob := range outboundsOf(t, *raw) {
		if tag, ok := ob["tag"].(string); ok {
			seen[tag]++
		}
	}
	for tag, n := range seen {
		if n > 1 {
			t.Fatalf("tag %q emitted %d times:\n%s", tag, n, *raw)
		}
	}
	// The route yields, the injected default keeps the name.
	if seen["direct-2"] != 1 {
		t.Fatalf("colliding route must be renamed to direct-2, got tags %v", seen)
	}
}

// Backstop: a tag can still collide from sources the replica filter does not
// cover — here a hand-added external link whose fragment matches a local
// inbound's tag. That must degrade to a renamed route, never to a config the
// client refuses wholesale.
func TestGetJson_CollidingExternalLinkIsRenamed(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	local := &model.Inbound{
		Type:    "vless",
		Tag:     "HK",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"HK","server":"1.2.3.4","server_port":443,"transport":{}}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(local).Error; err != nil {
		t.Fatalf("seed local inbound: %v", err)
	}

	// Two external links: one collides with the local tag, one already carries a
	// "-1" suffix. That second one is the point of the case — a renamer that
	// hands out "-1" for the first collision (as the old links-only dedup did)
	// produces two "HK-1" here and the subscription is rejected anyway.
	links := `[
		{"remark":"pasted","type":"external","uri":"vless://aaaaaaaa-0000-4000-8000-000000000001@5.6.7.8:443?security=tls#HK"},
		{"remark":"pasted","type":"external","uri":"vless://aaaaaaaa-0000-4000-8000-000000000002@9.10.11.12:443?security=tls#HK-1"}
	]`
	client := &model.Client{
		Enable:   true,
		Name:     "subcollide",
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, local.Id)),
		Links:    json.RawMessage(links),
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	raw, _, err := (&JsonService{}).GetJson("subcollide", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}

	seen := map[string]int{}
	for _, ob := range outboundsOf(t, *raw) {
		if tag, ok := ob["tag"].(string); ok {
			seen[tag]++
		}
	}
	for tag, n := range seen {
		if n > 1 {
			t.Fatalf("tag %q emitted %d times, every tag must be unique:\n%s", tag, n, *raw)
		}
	}
	// All three routes survive, and the renamed one skips past the pre-existing
	// HK-1 rather than colliding with it.
	for _, want := range []string{"HK", "HK-1", "HK-2"} {
		if seen[want] != 1 {
			t.Fatalf("expected tag %q exactly once, got %d (tags: %v)", want, seen[want], seen)
		}
	}
}

// seedLocalIPv6Client seeds a plain (non-replica) inbound whose out_json still
// holds a bracketed IPv6 literal, which is what the panel stored before
// FillOutJson started normalising it.
func seedLocalIPv6Client(t *testing.T, subId, remark string) {
	t.Helper()
	db := database.GetDB()

	inbound := &model.Inbound{
		Type:    "vless",
		Tag:     "v6-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"v6-in","server":"[2001:db8::1]","server_port":443}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.Client{
		Enable:   true,
		Name:     subId,
		Remark:   remark,
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

// A share link already carries the client's remark; the JSON and Clash
// subscriptions now name their nodes the same way, so one subscriber's nodes
// are tellable from another's in a shared client app.
func TestSubTagsCarryClientRemark(t *testing.T) {
	setupSubDB(t)
	seedLocalIPv6Client(t, "subv6", "alice")

	raw, _, err := (&JsonService{}).GetJson("subv6", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var found bool
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] != "vless" {
			continue
		}
		found = true
		if ob["tag"] != "alice-v6-in" {
			t.Errorf("json tag = %v, want %q", ob["tag"], "alice-v6-in")
		}
	}
	if !found {
		t.Fatalf("no vless outbound emitted:\n%s", *raw)
	}

	clash, _, err := (&ClashService{}).GetClash("subv6")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if !strings.Contains(*clash, "name: alice-v6-in") {
		t.Errorf("clash proxy is not named after the client:\n%s", *clash)
	}
}

// An IPv6 server reaches sing-box and mihomo bare. Bracketed, both read it as a
// domain name and the node is simply dead (#1220).
func TestSubEmitsBareIPv6Server(t *testing.T) {
	setupSubDB(t)
	seedLocalIPv6Client(t, "subv6", "")

	raw, _, err := (&JsonService{}).GetJson("subv6", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var found bool
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] != "vless" {
			continue
		}
		found = true
		if ob["server"] != "2001:db8::1" {
			t.Errorf("json server = %v, want %q", ob["server"], "2001:db8::1")
		}
	}
	if !found {
		t.Fatalf("no vless outbound emitted:\n%s", *raw)
	}

	clash, _, err := (&ClashService{}).GetClash("subv6")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if got := clashProxy(t, *clash, "v6-in")["server"]; got != "2001:db8::1" {
		t.Errorf("clash server = %v, want a bare IPv6 literal:\n%s", got, *clash)
	}
}

// clashProxy returns the named proxy from a generated Clash document. Reading
// the parsed value rather than grepping the text keeps these assertions off
// whatever the base config template happens to contain.
func clashProxy(t *testing.T, doc, name string) map[string]interface{} {
	t.Helper()
	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal clash config: %v", err)
	}
	for _, proxy := range cfg.Proxies {
		if proxy["name"] == name {
			return proxy
		}
	}
	t.Fatalf("no proxy named %q in:\n%s", name, doc)
	return nil
}

// The Clash UDP default is opt-in: mihomo keeps UDP off unless the proxy says
// otherwise, but turning it on for everyone would change every existing
// subscription.
func TestClashUdpDefaultIsOptIn(t *testing.T) {
	setupSubDB(t)
	seedLocalIPv6Client(t, "subv6", "")

	clash, _, err := (&ClashService{}).GetClash("subv6")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if _, ok := clashProxy(t, *clash, "v6-in")["udp"]; ok {
		t.Fatalf("udp must stay off until the setting is on:\n%s", *clash)
	}

	if err := database.GetDB().Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	clash, _, err = (&ClashService{}).GetClash("subv6")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	proxy := clashProxy(t, *clash, "v6-in")
	if proxy["udp"] != true {
		t.Errorf("udp = %v with subClashUdp on, want true:\n%s", proxy["udp"], *clash)
	}
	if proxy["packet-encoding"] != "xudp" {
		t.Errorf("packet-encoding = %v, want xudp so the proxy can carry UDP:\n%s", proxy["packet-encoding"], *clash)
	}
}

// The packet encoding is a per-inbound choice the sing-box subscription serves
// verbatim; the UDP default must not quietly replace it with xudp.
func TestClashUdpKeepsConfiguredPacketEncoding(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	inbound := &model.Inbound{
		Type:    "vless",
		Tag:     "pa-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"pa-in","server":"example.com","server_port":443,"packet_encoding":"packetaddr"}`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: "subpa",
		Config:   json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	clash, _, err := (&ClashService{}).GetClash("subpa")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if got := clashProxy(t, *clash, "pa-in")["packet-encoding"]; got != "packetaddr" {
		t.Errorf("packet-encoding = %v, want the configured %q:\n%s", got, "packetaddr", *clash)
	}
}

// A shadowsocks listener bound to TCP only must not be advertised as carrying
// UDP. The restriction lives in the inbound's options, not in out_json, so the
// Clash converter can only see it because getOutbounds copies it across.
func TestClashUdpRespectsTcpOnlyListener(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	inbound := &model.Inbound{
		Type:    "shadowsocks",
		Tag:     "ss-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"shadowsocks","tag":"ss-in","server":"example.com","server_port":443,"method":"aes-128-gcm"}`),
		Options: json.RawMessage(`{"method":"aes-128-gcm","network":"tcp"}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: "subss",
		Config:   json.RawMessage(`{"shadowsocks":{"password":"pw"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	clash, _, err := (&ClashService{}).GetClash("subss")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if _, ok := clashProxy(t, *clash, "ss-in")["udp"]; ok {
		t.Errorf("a tcp-only listener must not be advertised with udp:\n%s", *clash)
	}

	// The same restriction belongs in the sing-box subscription, where it is a
	// real outbound option rather than a hint.
	raw, _, err := (&JsonService{}).GetJson("subss", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var found bool
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] != "shadowsocks" {
			continue
		}
		found = true
		if ob["network"] != "tcp" {
			t.Errorf(`json network = %v, want "tcp":\n%s`, ob["network"], *raw)
		}
	}
	if !found {
		t.Fatalf("no shadowsocks outbound emitted:\n%s", *raw)
	}
}


// runAddHTTPClients drives addHTTPClients over a settings template the way
// addOthers does, and hands back both halves it can write to.
//
// The outbound list is part of the input, not scenery: the declared client has
// to name route.final as its detour, so addHTTPClients has to be able to find
// that outbound and see it is not an optionless direct one.
func runAddHTTPClients(t *testing.T, template string) (map[string]interface{}, map[string]interface{}) {
	t.Helper()
	return runAddHTTPClientsWith(t, template, []map[string]interface{}{
		{"type": "selector", "tag": "proxy", "outbounds": []interface{}{"auto"}},
		{"type": "direct", "tag": "direct"},
	})
}

func runAddHTTPClientsWith(t *testing.T, template string, outbounds []map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	t.Helper()
	var othersJson map[string]interface{}
	if err := json.Unmarshal([]byte(template), &othersJson); err != nil {
		t.Fatal(err)
	}
	route := map[string]interface{}{"final": "proxy"}
	if ruleSet, ok := othersJson["rule_set"]; ok {
		route["rule_set"] = ruleSet
	}
	if final, ok := othersJson["final"].(string); ok && final != "" {
		route["final"] = final
	}
	jsonConfig := map[string]interface{}{}
	(&JsonService{}).addHTTPClients(&jsonConfig, route, othersJson, outbounds)
	return jsonConfig, route
}

// A remote rule-set with no client of its own would use the implicit default,
// which sing-box 1.14 reports as deprecated in the client's log.
func TestAddHTTPClientsDeclaresDefault(t *testing.T) {
	jsonConfig, route := runAddHTTPClients(t, `{
		"rule_set": [
			{"tag": "geosite-ir", "type": "remote", "format": "binary", "url": "https://e.com/a.srs"}
		]
	}`)

	clients, ok := jsonConfig["http_clients"].([]interface{})
	if !ok || len(clients) != 1 {
		t.Fatalf("expected one declared http client, got %v", jsonConfig["http_clients"])
	}
	client, _ := clients[0].(map[string]interface{})
	if client["tag"] != defaultHTTPClientTag {
		t.Errorf("unexpected client: %v", client)
	}
	// The detour is the whole point: sing-box's implicit client dials through
	// the default outbound, and a declared client can only say that by naming
	// route.final. Leaving it out would send rule-set downloads out directly,
	// which on a censored network is where they stop arriving.
	if client["detour"] != "proxy" {
		t.Errorf("the default download client must dial through route.final, got %v", client)
	}
	if route["default_http_client"] != defaultHTTPClientTag {
		t.Errorf("the route must point at it, got %v", route["default_http_client"])
	}
}

// sing-box refuses a detour to a direct outbound carrying no options of its
// own, and no detour already means the same thing.
func TestAddHTTPClientsOmitsNoopDetour(t *testing.T) {
	jsonConfig, _ := runAddHTTPClients(t, `{
		"final": "direct",
		"rule_set": [{"tag": "a", "type": "remote", "format": "binary", "url": "https://e.com/a.srs"}]
	}`)

	clients, ok := jsonConfig["http_clients"].([]interface{})
	if !ok || len(clients) != 1 {
		t.Fatalf("expected one declared http client, got %v", jsonConfig["http_clients"])
	}
	client, _ := clients[0].(map[string]interface{})
	if _, hasDetour := client["detour"]; hasDetour {
		t.Errorf("a no-op detour must be left out, got %v", client)
	}
}

// Nothing is declared when the detour cannot be named: a client that dials
// directly is not the fallback it would be replacing.
func TestAddHTTPClientsSkipsWhenFinalIsUnknown(t *testing.T) {
	jsonConfig, route := runAddHTTPClientsWith(t, `{
		"final": "gone",
		"rule_set": [{"tag": "a", "type": "remote", "format": "binary", "url": "https://e.com/a.srs"}]
	}`, []map[string]interface{}{{"type": "direct", "tag": "direct"}})

	if _, ok := jsonConfig["http_clients"]; ok {
		t.Errorf("no client should have been declared, got %v", jsonConfig["http_clients"])
	}
	if _, ok := route["default_http_client"]; ok {
		t.Errorf("no default should have been set, got %v", route["default_http_client"])
	}
}

// Rule-sets that name their own client need no default, and a config with no
// remote rule-set at all must not grow one.
func TestAddHTTPClientsLeavesConfigsAlone(t *testing.T) {
	for name, template := range map[string]string{
		"every rule-set has a client": `{
			"rule_set": [
				{"tag": "a", "type": "remote", "format": "binary", "url": "https://e.com/a.srs",
				 "http_client": {"detour": "proxy"}}
			]
		}`,
		"local rule-set only": `{
			"rule_set": [{"tag": "a", "type": "local", "format": "binary", "path": "/a.srs"}]
		}`,
		"no rule-set": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			jsonConfig, route := runAddHTTPClients(t, template)
			if _, ok := jsonConfig["http_clients"]; ok {
				t.Errorf("no client should have been declared, got %v", jsonConfig["http_clients"])
			}
			if _, ok := route["default_http_client"]; ok {
				t.Errorf("no default should have been set, got %v", route["default_http_client"])
			}
		})
	}
}

// A template that declares its own clients decides for itself.
func TestAddHTTPClientsKeepsTemplateClients(t *testing.T) {
	jsonConfig, route := runAddHTTPClients(t, `{
		"http_clients": [{"tag": "over-proxy", "detour": "proxy"}],
		"default_http_client": "over-proxy",
		"rule_set": [
			{"tag": "a", "type": "remote", "format": "binary", "url": "https://e.com/a.srs"}
		]
	}`)

	clients, ok := jsonConfig["http_clients"].([]interface{})
	if !ok || len(clients) != 1 {
		t.Fatalf("the template's clients must be carried over, got %v", jsonConfig["http_clients"])
	}
	client, _ := clients[0].(map[string]interface{})
	if client["tag"] != "over-proxy" || client["detour"] != "proxy" {
		t.Errorf("the template's client must be unchanged, got %v", client)
	}
	if route["default_http_client"] != "over-proxy" {
		t.Errorf("the template's default must win, got %v", route["default_http_client"])
	}
}

func runAddOthersRoute(t *testing.T, template string) map[string]interface{} {
	t.Helper()
	var othersJson map[string]interface{}
	if err := json.Unmarshal([]byte(template), &othersJson); err != nil {
		t.Fatal(err)
	}
	route := map[string]interface{}{"final": "proxy"}
	if defaultDomainResolver, ok := othersJson["default_domain_resolver"].(string); ok && defaultDomainResolver != "" {
		route["default_domain_resolver"] = defaultDomainResolver
	} else if fallback := fallbackDomainResolver(othersJson); fallback != "" {
		route["default_domain_resolver"] = fallback
	}
	return route
}

// With two or more DNS servers and nothing naming the resolver for dial
// fields, sing-box guesses and reports the guess as deprecated.
func TestFallbackDomainResolver(t *testing.T) {
	for name, expect := range map[string]struct {
		template string
		want     string
	}{
		"final server wins": {`{"dns": {"servers": [
			{"tag": "proxy-dns", "type": "tcp", "server": "8.8.8.8"},
			{"tag": "local-dns", "type": "local"}
		], "final": "local-dns"}}`, "local-dns"},
		"first server without a final": {`{"dns": {"servers": [
			{"tag": "a", "type": "local"}, {"tag": "b", "type": "local"}
		]}}`, "a"},
		"single server needs no choice": {`{"dns": {"servers": [
			{"tag": "a", "type": "local"}
		], "final": "a"}}`, ""},
		"no dns section": {`{}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var othersJson map[string]interface{}
			if err := json.Unmarshal([]byte(expect.template), &othersJson); err != nil {
				t.Fatal(err)
			}
			if got := fallbackDomainResolver(othersJson); got != expect.want {
				t.Errorf("want %q, got %q", expect.want, got)
			}
		})
	}
}

// An explicit choice in the template must not be second-guessed.
func TestDefaultDomainResolverFromTemplate(t *testing.T) {
	route := runAddOthersRoute(t, `{
		"default_domain_resolver": "direct-dns",
		"dns": {"servers": [
			{"tag": "proxy-dns", "type": "tcp", "server": "8.8.8.8"},
			{"tag": "direct-dns", "type": "local"}
		], "final": "proxy-dns"}
	}`)
	if route["default_domain_resolver"] != "direct-dns" {
		t.Errorf("the template's choice must win, got %v", route["default_domain_resolver"])
	}
}

// seedSnellClient seeds a snell listener of the given version plus a client
// holding the per-client key for it.
func seedSnellClient(t *testing.T, subId string, version int) {
	t.Helper()
	db := database.GetDB()

	inbound := &model.Inbound{
		Type:    "snell",
		Tag:     "snell-in",
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(fmt.Sprintf(`{"version":%d,"psk":"shared-psk-value","mode":"default"}`, version)),
		OutJson: json.RawMessage(`{}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	// FillOutJson is what the panel runs on save; the subscription reads what
	// it left behind.
	if err := util.FillOutJson(inbound, "example.com"); err != nil {
		t.Fatalf("fill out_json: %v", err)
	}
	if err := db.Model(model.Inbound{}).Where("id = ?", inbound.Id).
		Update("out_json", inbound.OutJson).Error; err != nil {
		t.Fatalf("store out_json: %v", err)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: subId,
		Config:   json.RawMessage(`{"snell":{"name":"` + subId + `","userkey":"client-key"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

// A snell v6 listener is reachable by a sing-box client, so the subscription
// has to carry the shared psk from the listener and the per-client userkey from
// the client's own config.
func TestSubEmitsSnellV6(t *testing.T) {
	setupSubDB(t)
	seedSnellClient(t, "subsnell", 6)

	raw, _, err := (&JsonService{}).GetJson("subsnell", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var found bool
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] != "snell" {
			continue
		}
		found = true
		if ob["version"] != float64(6) {
			t.Errorf("version = %v, want 6", ob["version"])
		}
		if ob["psk"] != "shared-psk-value" {
			t.Errorf("psk = %v, want the listener's", ob["psk"])
		}
		if ob["userkey"] != "client-key" {
			t.Errorf("userkey = %v, want the client's", ob["userkey"])
		}
		if ob["mode"] != "default" {
			t.Errorf("mode = %v, want the listener's", ob["mode"])
		}
		if _, leaked := ob["name"]; leaked {
			t.Errorf("the client config's name is not an outbound option, got %v", ob)
		}
	}
	if !found {
		t.Fatalf("no snell outbound emitted:\n%s", *raw)
	}
}

// sing-box's snell outbound speaks versions 4 and 6 while the inbound speaks 5
// and 6, so a v5 listener has no client config to generate. Emitting a v5
// outbound anyway would produce a config sing-box refuses to load.
func TestSubSkipsSnellV5(t *testing.T) {
	setupSubDB(t)
	seedSnellClient(t, "subsnell5", 5)

	raw, _, err := (&JsonService{}).GetJson("subsnell5", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] == "snell" {
			t.Fatalf("a v5 listener has no generated client config:\n%s", *raw)
		}
	}
}

// subClashUdp is the single switch for the udp flag, shadowsocks included. The
// protocol branch used to answer for itself whenever the listener was not
// TCP-only, which made the setting mean nothing there.
func TestClashUdpAppliesToShadowsocks(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()

	inbound := &model.Inbound{
		Type:    "shadowsocks",
		Tag:     "ss-udp",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"shadowsocks","tag":"ss-udp","server":"example.com","server_port":443,"method":"aes-128-gcm","network":"udp"}`),
		Options: json.RawMessage(`{"method":"aes-128-gcm","network":"udp"}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: "subssudp",
		Config:   json.RawMessage(`{"shadowsocks":{"password":"pw"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	clash, _, err := (&ClashService{}).GetClash("subssudp")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if _, ok := clashProxy(t, *clash, "ss-udp")["udp"]; ok {
		t.Errorf("udp must stay off until the setting is on:\n%s", *clash)
	}

	if err := db.Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	clash, _, err = (&ClashService{}).GetClash("subssudp")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if clashProxy(t, *clash, "ss-udp")["udp"] != true {
		t.Errorf("udp = %v with subClashUdp on, want true:\n%s", clashProxy(t, *clash, "ss-udp")["udp"], *clash)
	}
}

// seedShadowsocksClient seeds a shadowsocks listener plus a client, letting the
// caller decide the listener's network and whether the client config asks for
// UDP over TCP.
func seedShadowsocksClient(t *testing.T, subId, tag, network string, uot bool) {
	t.Helper()
	db := database.GetDB()

	outJson := fmt.Sprintf(
		`{"type":"shadowsocks","tag":%q,"server":"example.com","server_port":443,`+
			`"method":"aes-128-gcm","udp_over_tcp":%t}`, tag, uot)
	inbound := &model.Inbound{
		Type: "shadowsocks", Tag: tag,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(outJson),
		Options: json.RawMessage(fmt.Sprintf(`{"method":"aes-128-gcm","network":%q}`, network)),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: subId,
		Config:   json.RawMessage(`{"shadowsocks":{"password":"pw"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

// UDP over TCP is its own opt-in, not something the subClashUdp default gates:
// mihomo reads udp-over-tcp only when udp is set, so emitting one without the
// other describes a transport that is never used.
func TestClashUdpOverTcpTurnsUdpOnByItself(t *testing.T) {
	setupSubDB(t)
	seedShadowsocksClient(t, "subuot", "ss-uot", "udp", true)

	clash, _, err := (&ClashService{}).GetClash("subuot")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	proxy := clashProxy(t, *clash, "ss-uot")
	if proxy["udp-over-tcp"] != true {
		t.Errorf("udp-over-tcp = %v, want true:\n%s", proxy["udp-over-tcp"], *clash)
	}
	// subClashUdp is off here; UoT still has to bring udp with it.
	if proxy["udp"] != true {
		t.Errorf("udp = %v, want true so mihomo actually reads udp-over-tcp:\n%s", proxy["udp"], *clash)
	}
}

// Carrying UDP inside the TCP stream is what UoT is for, so a TCP-only listener
// is precisely where it applies -- the network check must not gate it.
func TestClashUdpOverTcpAppliesToTcpOnlyListener(t *testing.T) {
	setupSubDB(t)
	seedShadowsocksClient(t, "subuottcp", "ss-uot-tcp", "tcp", true)

	clash, _, err := (&ClashService{}).GetClash("subuottcp")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	proxy := clashProxy(t, *clash, "ss-uot-tcp")
	if proxy["udp"] != true || proxy["udp-over-tcp"] != true {
		t.Errorf("a tcp-only listener is where UoT belongs, got udp=%v uot=%v:\n%s",
			proxy["udp"], proxy["udp-over-tcp"], *clash)
	}
}

// Without UoT the plain-UDP default still answers, and it stays off until the
// setting is on.
func TestClashUdpWithoutUdpOverTcpStaysOff(t *testing.T) {
	setupSubDB(t)
	seedShadowsocksClient(t, "subnouot", "ss-no-uot", "udp", false)

	clash, _, err := (&ClashService{}).GetClash("subnouot")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	proxy := clashProxy(t, *clash, "ss-no-uot")
	if _, ok := proxy["udp"]; ok {
		t.Errorf("udp must stay off until subClashUdp is on:\n%s", *clash)
	}
	if _, ok := proxy["udp-over-tcp"]; ok {
		t.Errorf("udp-over-tcp must not appear when the client config says false:\n%s", *clash)
	}
}

// addOthers returns before it writes `route` on every failure path, so a
// template the panel cannot parse used to produce a subscription with outbounds
// and no route section at all: every rule the operator wrote silently gone, and
// nothing anywhere saying so. Failing the fetch is what tells them.
func TestGetJsonFailsOnUnparseableTemplate(t *testing.T) {
	setupSubDB(t)
	seedLocalIPv6Client(t, "subbad", "")
	if err := database.GetDB().Create(&model.Setting{
		Key: "subJsonExt", Value: `{"dns": {`,
	}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	raw, _, err := (&JsonService{}).GetJson("subbad", "json")
	if err == nil {
		t.Fatalf("a template that cannot be parsed must fail the fetch, got:\n%s", *raw)
	}
	if raw != nil {
		t.Errorf("no config should be handed back alongside the error, got:\n%s", *raw)
	}
}

// The healthy path still carries the template's routing through untouched.
func TestGetJsonKeepsRouteFromTemplate(t *testing.T) {
	setupSubDB(t)
	seedLocalIPv6Client(t, "subok", "")
	if err := database.GetDB().Create(&model.Setting{
		Key:   "subJsonExt",
		Value: `{"final": "proxy", "rules": [{"action": "sniff"}]}`,
	}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	raw, _, err := (&JsonService{}).GetJson("subok", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var cfg struct {
		Route struct {
			Final string           `json:"final"`
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(*raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Route.Final != "proxy" {
		t.Errorf("route.final = %q, want the template's", cfg.Route.Final)
	}
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0]["action"] != "sniff" {
		t.Errorf("the template's rules must survive, got %v", cfg.Route.Rules)
	}
}
