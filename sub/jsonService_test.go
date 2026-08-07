package sub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

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
