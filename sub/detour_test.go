package sub

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"

	"gopkg.in/yaml.v3"
)

// seedShadowTlsChain stores the shape issue #255 asks for: a shadowsocks
// listener whose client config dials through a ShadowTLS one, both on a client
// with a remark -- the remark is what makes the subscription's tags differ from
// the inbound tags a detour names. onShadowTls false leaves the client off the
// ShadowTLS inbound.
func seedShadowTlsChain(t *testing.T, subId, ssAddrs, stlsAddrs string, onShadowTls bool) {
	t.Helper()
	db := database.GetDB()

	ss := &model.Inbound{
		Type:  "shadowsocks",
		Tag:   "ss-in",
		Addrs: json.RawMessage(ssAddrs),
		OutJson: json.RawMessage(`{"type":"shadowsocks","tag":"ss-in","server":"panel.example.com",` +
			`"server_port":20000,"method":"aes-128-gcm","detour":"stls-in"}`),
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":20000,"method":"aes-128-gcm"}`),
	}
	// The out_json as it was stored before shadowTlsOut wrote an SNI: the read
	// side has to fill it in from the handshake server.
	stls := &model.Inbound{
		Type:  "shadowtls",
		Tag:   "stls-in",
		Addrs: json.RawMessage(stlsAddrs),
		OutJson: json.RawMessage(`{"type":"shadowtls","tag":"stls-in","server":"panel.example.com",` +
			`"server_port":443,"version":3,"tls":{"enabled":true}}`),
		Options: json.RawMessage(`{"listen_port":443,"version":3,"detour":"ss-in",` +
			`"handshake":{"server":"www.example.com","server_port":443}}`),
	}
	for _, in := range []*model.Inbound{ss, stls} {
		if err := db.Create(in).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", in.Tag, err)
		}
	}
	ids := fmt.Sprintf(`[%d]`, ss.Id)
	if onShadowTls {
		ids = fmt.Sprintf(`[%d,%d]`, ss.Id, stls.Id)
	}
	if err := db.Create(&model.Client{
		Enable: true, Name: subId, Remark: "alice",
		Config:   json.RawMessage(`{"shadowsocks":{"password":"pw"},"shadowtls":{"name":"alice","password":"stls-pw"}}`),
		Inbounds: json.RawMessage(ids),
		Links:    json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

func outboundsByTag(t *testing.T, raw string) map[string]map[string]interface{} {
	t.Helper()
	byTag := make(map[string]map[string]interface{})
	for _, ob := range outboundsOf(t, raw) {
		tag, _ := ob["tag"].(string)
		byTag[tag] = ob
	}
	return byTag
}

// groupHas reports whether the proxy and auto groups both offer tag; it fails
// the test if they disagree, since both are built from the same list.
func groupHas(t *testing.T, byTag map[string]map[string]interface{}, tag string) bool {
	t.Helper()
	var in []bool
	for _, group := range []string{"proxy", "auto"} {
		members, _ := byTag[group]["outbounds"].([]interface{})
		found := false
		for _, m := range members {
			if m == tag {
				found = true
			}
		}
		in = append(in, found)
	}
	if in[0] != in[1] {
		t.Fatalf("proxy and auto disagree about %q", tag)
	}
	return in[0]
}

// A detour in out_json names the inbound tag; the subscription has to name the
// tag the target ended up with, which the client remark has changed. Left as
// stored it names nothing, and sing-box refuses the whole config.
func TestJsonDetourThroughShadowTls(t *testing.T) {
	setupSubDB(t)
	seedShadowTlsChain(t, "subchain", `[]`, `[]`, true)

	raw, _, err := (&JsonService{}).GetJson("subchain", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)

	ss, stls := byTag["alice-ss-in"], byTag["alice-stls-in"]
	if ss == nil || stls == nil {
		t.Fatalf("want alice-ss-in and alice-stls-in:\n%s", *raw)
	}
	if ss["detour"] != "alice-stls-in" {
		t.Errorf("detour = %v, want the ShadowTLS outbound's own tag alice-stls-in", ss["detour"])
	}
	if stls["password"] != "stls-pw" {
		t.Errorf("shadowtls password = %v, want the client's", stls["password"])
	}
	tls, _ := stls["tls"].(map[string]interface{})
	if tls["server_name"] != "www.example.com" {
		t.Errorf("shadowtls server_name = %v, want the handshake server", tls["server_name"])
	}

	if !groupHas(t, byTag, "alice-ss-in") {
		t.Errorf("the shadowsocks route is missing from the groups:\n%s", *raw)
	}
	if groupHas(t, byTag, "alice-stls-in") {
		t.Errorf("a ShadowTLS outbound something dials through is no node of its own:\n%s", *raw)
	}

	buildSingBox(t, *raw)
}

// Every ShadowTLS address gets its own shadowsocks route, paired with it. The
// shadowsocks inbound's own addresses are not used: the detour decides where
// the connection goes.
func TestJsonDetourPairsShadowTlsAddresses(t *testing.T) {
	setupSubDB(t)
	seedShadowTlsChain(t, "subpairs",
		`[{"server":"x.example.com","server_port":1,"remark":"-x"}]`,
		`[{"server":"a.example.com","server_port":443,"remark":"-a"},`+
			`{"server":"b.example.com","server_port":8443,"remark":"-b"}]`,
		true)

	raw, _, err := (&JsonService{}).GetJson("subpairs", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)

	for _, suffix := range []string{"-a", "-b"} {
		ss := byTag["alice-ss-in"+suffix]
		if ss == nil {
			t.Fatalf("no alice-ss-in%s:\n%s", suffix, *raw)
		}
		if ss["detour"] != "alice-stls-in"+suffix {
			t.Errorf("alice-ss-in%s detour = %v, want alice-stls-in%s", suffix, ss["detour"], suffix)
		}
	}
	if _, ok := byTag["alice-ss-in-x"]; ok {
		t.Errorf("the shadowsocks inbound's own address must not become a route:\n%s", *raw)
	}

	buildSingBox(t, *raw)
}

// uniqueOutboundTags renames a collision after the pairing is made, so the
// detour has to follow the rename rather than keep the name it was built with.
func TestJsonDetourFollowsRenamedTarget(t *testing.T) {
	setupSubDB(t)
	seedShadowTlsChain(t, "subrename", `[]`,
		`[{"server":"a.example.com","server_port":443},{"server":"b.example.com","server_port":8443}]`,
		true)

	raw, _, err := (&JsonService{}).GetJson("subrename", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)

	pairs := map[string]string{"alice-ss-in": "alice-stls-in", "alice-ss-in-2": "alice-stls-in-2"}
	for ssTag, stlsTag := range pairs {
		ss, stls := byTag[ssTag], byTag[stlsTag]
		if ss == nil || stls == nil {
			t.Fatalf("want %s and %s:\n%s", ssTag, stlsTag, *raw)
		}
		if ss["detour"] != stlsTag {
			t.Errorf("%s detour = %v, want %s", ssTag, ss["detour"], stlsTag)
		}
	}
	if byTag["alice-stls-in"]["server"] == byTag["alice-stls-in-2"]["server"] {
		t.Errorf("the two ShadowTLS addresses collapsed into one:\n%s", *raw)
	}

	buildSingBox(t, *raw)
}

// A client that is not on the ShadowTLS inbound has no credentials in it, so
// the detour is dropped and the route dials the shadowsocks inbound directly,
// as it did before detours were offered.
func TestJsonDetourDroppedWithoutTarget(t *testing.T) {
	setupSubDB(t)
	seedShadowTlsChain(t, "sublone", `[]`, `[]`, false)

	raw, _, err := (&JsonService{}).GetJson("sublone", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)

	ss := byTag["alice-ss-in"]
	if ss == nil {
		t.Fatalf("no alice-ss-in:\n%s", *raw)
	}
	if detour, ok := ss["detour"]; ok {
		t.Errorf("detour = %v, want none: the target is not in this subscription", detour)
	}
	if !groupHas(t, byTag, "alice-ss-in") {
		t.Errorf("the shadowsocks route is missing from the groups:\n%s", *raw)
	}

	buildSingBox(t, *raw)
}

// mihomo has no ShadowTLS proxy, only the shadowsocks plugin: the pair becomes
// one proxy aimed at the ShadowTLS listener, and UDP stays off because
// ShadowTLS carries TCP alone.
func TestClashDetourThroughShadowTls(t *testing.T) {
	setupSubDB(t)
	seedShadowTlsChain(t, "subchainclash", `[]`, `[]`, true)
	if err := database.GetDB().Create(&model.Setting{Key: "subClashUdp", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	clash, _, err := (&ClashService{}).GetClash("subchainclash")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	proxy := clashProxy(t, *clash, "alice-ss-in")
	if proxy["server"] != "panel.example.com" || proxy["port"] != 443 {
		t.Errorf("server = %v:%v, want the ShadowTLS listener panel.example.com:443", proxy["server"], proxy["port"])
	}
	if proxy["plugin"] != "shadow-tls" {
		t.Errorf("plugin = %v, want shadow-tls", proxy["plugin"])
	}
	opts, _ := proxy["plugin-opts"].(map[string]interface{})
	if opts["host"] != "www.example.com" || opts["password"] != "stls-pw" || opts["version"] != 3 {
		t.Errorf("plugin-opts = %v, want host www.example.com, the client's password, version 3", opts)
	}
	if _, ok := proxy["udp"]; ok {
		t.Errorf("udp must stay off through ShadowTLS:\n%s", *clash)
	}

	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(*clash), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Proxies {
		if p["name"] == "alice-stls-in" {
			t.Errorf("the ShadowTLS outbound must not be a proxy of its own:\n%s", *clash)
		}
	}
}

// seedSS stores a shadowsocks inbound whose client config carries detour
// (none when ""), and returns its id.
func seedSS(t *testing.T, tag string, port int, detour string) uint {
	t.Helper()
	out := fmt.Sprintf(`{"type":"shadowsocks","tag":%q,"server":"panel.example.com","server_port":%d,"method":"aes-128-gcm"`, tag, port)
	if detour != "" {
		out += fmt.Sprintf(`,"detour":%q`, detour)
	}
	in := &model.Inbound{
		Type: "shadowsocks", Tag: tag,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(out + "}"),
		Options: json.RawMessage(fmt.Sprintf(`{"listen_port":%d,"method":"aes-128-gcm"}`, port)),
	}
	if err := database.GetDB().Create(in).Error; err != nil {
		t.Fatalf("seed inbound %s: %v", tag, err)
	}
	return in.Id
}

func seedClientOn(t *testing.T, subId, remark, links string, ids ...uint) {
	t.Helper()
	raw, _ := json.Marshal(ids)
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: subId, Remark: remark,
		Config:   json.RawMessage(`{"shadowsocks":{"password":"pw"},"shadowtls":{"name":"alice","password":"stls-pw"},` + clientVlessConfig[1:]),
		Inbounds: json.RawMessage(raw),
		Links:    json.RawMessage(links),
	}).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

// A detour naming no inbound is the operator's own, written by hand or through
// the API: it may name an outbound the subscription carries under exactly that
// tag, as an external link's is, and has to reach the config as stored.
func TestJsonDetourToExternalOutboundKept(t *testing.T) {
	setupSubDB(t)
	id := seedSS(t, "ss-in", 20000, "relay-node")
	seedClientOn(t, "subext", "",
		`[{"remark":"relay","type":"external","uri":"trojan://p4ss@relay.example.com:443?security=tls&sni=relay.example.com#relay-node"}]`, id)

	raw, _, err := (&JsonService{}).GetJson("subext", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)
	if byTag["relay-node"] == nil {
		t.Fatalf("the external link produced no relay-node outbound:\n%s", *raw)
	}
	if got := byTag["ss-in"]["detour"]; got != "relay-node" {
		t.Errorf("detour = %v, want the stored relay-node", got)
	}
	buildSingBox(t, *raw)
}

// Two configs riding each other would be a loop, which sing-box refuses the
// whole config over. Each rides an entry that rides another, so the one-hop
// rule refuses both: neither keeps a detour.
func TestJsonDetourLoopBroken(t *testing.T) {
	setupSubDB(t)
	a := seedSS(t, "ss-a", 20001, "ss-b")
	b := seedSS(t, "ss-b", 20002, "ss-a")
	seedClientOn(t, "subloop", "", `[]`, a, b)

	raw, _, err := (&JsonService{}).GetJson("subloop", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)
	for _, tag := range []string{"ss-a", "ss-b"} {
		if got, ok := byTag[tag]["detour"]; ok {
			t.Errorf("%s detour = %v, want none:\n%s", tag, got, *raw)
		}
	}
	buildSingBox(t, *raw)
}

// The loop check itself, for detours left as stored -- names outside the
// client's inbounds, which the one-hop rule never sees: the outbound whose
// detour closes the loop loses it, and only that one.
func TestResolveDetoursBreaksLoops(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"tag": "a", "detour": "b"},
		{"tag": "b", "detour": "a"},
		{"tag": "c", "detour": "a"},
		{"tag": "self", "detour": "self"},
		{"tag": "lost", "detour": "nowhere"},
	}
	resolveDetours(outbounds, nil)

	got := map[string]interface{}{}
	for _, out := range outbounds {
		got[out["tag"].(string)] = out["detour"]
	}
	want := map[string]interface{}{"a": nil, "b": "a", "c": "a", "self": nil, "lost": nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("detours = %v, want %v", got, want)
	}
}

// A ShadowTLS config carrying a detour that names nothing is still a plain
// target: its own detour is dropped, and whatever rides it keeps riding it.
func TestJsonDetourTargetWithDanglingDetour(t *testing.T) {
	setupSubDB(t)
	ss := seedSS(t, "ss-in", 20000, "stls-in")
	stls := &model.Inbound{
		Type: "shadowtls", Tag: "stls-in",
		Addrs: json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"shadowtls","tag":"stls-in","server":"panel.example.com",` +
			`"server_port":443,"version":3,"tls":{"enabled":true,"server_name":"www.example.com"},"detour":"nowhere"}`),
		Options: json.RawMessage(`{"listen_port":443,"version":3}`),
	}
	if err := database.GetDB().Create(stls).Error; err != nil {
		t.Fatal(err)
	}
	seedClientOn(t, "subdangle", "alice", `[]`, ss, stls.Id)

	raw, _, err := (&JsonService{}).GetJson("subdangle", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	byTag := outboundsByTag(t, *raw)
	if got := byTag["alice-ss-in"]["detour"]; got != "alice-stls-in" {
		t.Errorf("ss detour = %v, want alice-stls-in", got)
	}
	if got, ok := byTag["alice-stls-in"]["detour"]; ok {
		t.Errorf("shadowtls kept the detour %v, which names nothing", got)
	}
	buildSingBox(t, *raw)
}

// FillOutJson empties a ShadowTLS v1/v2 client config down to its tls block.
// Served, that is an outbound with no type, and sing-box refuses the whole
// subscription over it -- every other route of the client with it.
func TestJsonSkipsTypelessClientConfig(t *testing.T) {
	setupSubDB(t)
	ss := seedSS(t, "ss-in", 20000, "")
	v2 := &model.Inbound{
		Type: "shadowtls", Tag: "stls-v2",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"tls":{"enabled":true,"server_name":"www.example.com"}}`),
		Options: json.RawMessage(`{"listen_port":443,"version":2,"password":"shared"}`),
	}
	if err := database.GetDB().Create(v2).Error; err != nil {
		t.Fatal(err)
	}
	seedClientOn(t, "subv2", "alice", `[]`, ss, v2.Id)

	raw, _, err := (&JsonService{}).GetJson("subv2", "json")
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	for _, ob := range outboundsOf(t, *raw) {
		if ob["type"] == nil || ob["type"] == "" {
			t.Errorf("outbound with no type served: %v", ob)
		}
	}
	if outboundsByTag(t, *raw)["alice-ss-in"] == nil {
		t.Errorf("the other route is missing:\n%s", *raw)
	}
	buildSingBox(t, *raw)
}

// One reader for both the TLS block and the shadow-tls plugin, so the two
// cannot disagree about when a fingerprint is sent.
func TestClashFingerprint(t *testing.T) {
	tests := []struct {
		name string
		tls  string
		want string
	}{
		{"enabled", `{"utls":{"enabled":true,"fingerprint":"chrome"}}`, "chrome"},
		{"disabled", `{"utls":{"enabled":false,"fingerprint":"chrome"}}`, ""},
		{"enabled with no fingerprint", `{"utls":{"enabled":true,"fingerprint":""}}`, ""},
		{"no utls", `{"enabled":true}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tls map[string]interface{}
			if err := json.Unmarshal([]byte(tt.tls), &tls); err != nil {
				t.Fatal(err)
			}
			if got := clashFingerprint(tls); got != tt.want {
				t.Errorf("clashFingerprint(%s) = %q, want %q", tt.tls, got, tt.want)
			}
		})
	}
}

// A rider refused by the one-hop rule loses its detour for every client alike.
// Left as stored, it survived whenever the inbound tag matched a final one --
// no remark -- and was dropped otherwise.
func TestJsonDetourTwoHopRefusedAlike(t *testing.T) {
	for _, remark := range []string{"", "alice"} {
		t.Run("remark="+remark, func(t *testing.T) {
			setupSubDB(t)
			a := seedSS(t, "ss-a", 20001, "ss-b")
			b := seedSS(t, "ss-b", 20002, "ss-c")
			c := seedSS(t, "ss-c", 20003, "")
			seedClientOn(t, "subhops", remark, `[]`, a, b, c)

			raw, _, err := (&JsonService{}).GetJson("subhops", "json")
			if err != nil {
				t.Fatalf("GetJson: %v", err)
			}
			byTag := outboundsByTag(t, *raw)
			name := func(tag string) string { return util.JoinRemark(remark, tag) }
			if got, ok := byTag[name("ss-a")]["detour"]; ok {
				t.Errorf("ss-a rides an entry that rides another, want no detour, got %v", got)
			}
			if got := byTag[name("ss-b")]["detour"]; got != name("ss-c") {
				t.Errorf("ss-b detour = %v, want %s", got, name("ss-c"))
			}
			buildSingBox(t, *raw)
		})
	}
}

// A detour to anything but ShadowTLS is mihomo's dialer-proxy; one whose
// target has no Clash form is left out rather than served dialling its own
// server directly.
func TestClashDialerProxy(t *testing.T) {
	setupSubDB(t)
	a := seedSS(t, "ss-a", 20001, "ss-b")
	b := seedSS(t, "ss-b", 20002, "")
	lone := seedSS(t, "ss-lone", 20003, "naive-in")
	naive := &model.Inbound{
		Type: "naive", Tag: "naive-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"naive","tag":"naive-in","server":"panel.example.com","server_port":443}`),
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	if err := database.GetDB().Create(naive).Error; err != nil {
		t.Fatal(err)
	}
	seedClientOn(t, "subdialer", "alice", `[]`, a, b, lone, naive.Id)

	clash, _, err := (&ClashService{}).GetClash("subdialer")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if got := clashProxy(t, *clash, "alice-ss-a")["dialer-proxy"]; got != "alice-ss-b" {
		t.Errorf("dialer-proxy = %v, want alice-ss-b", got)
	}
	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(*clash), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Proxies {
		if p["name"] == "alice-ss-lone" {
			t.Errorf("ss-lone rides naive, which mihomo has no form for: it must be left out\n%s", *clash)
		}
	}
}

// Dropping one proxy can strand another riding it, all the way down a chain.
func TestApplyDialerProxiesCascades(t *testing.T) {
	proxy := func(name string) interface{} { return map[string]interface{}{"name": name} }
	proxies := []interface{}{proxy("a"), proxy("b"), proxy("c"), proxy("d")}
	names := []string{"a", "b", "c", "d"}
	// c rides b rides "gone"; d rides a, which stays.
	via := map[string]string{"b": "gone", "c": "b", "d": "a"}

	proxies, names = applyDialerProxies(proxies, names, via)
	if !reflect.DeepEqual(names, []string{"a", "d"}) {
		t.Fatalf("names = %v, want [a d]", names)
	}
	if got := proxies[1].(map[string]interface{})["dialer-proxy"]; got != "a" {
		t.Errorf("d dialer-proxy = %v, want a", got)
	}
	if _, ok := proxies[0].(map[string]interface{})["dialer-proxy"]; ok {
		t.Errorf("a rides nothing, got a dialer-proxy")
	}
}

// A rider is built outside addrCopies, so it has to get the ws Host fallback
// on its own: without it sing-box sends the rider's server as the Host.
func TestJsonDetourRiderGetsWsHostFallback(t *testing.T) {
	front := &model.Inbound{
		Type: "vless", Tag: "front",
		OutJson: json.RawMessage(`{"type":"vless","tag":"front","server":"front.example.com","server_port":443}`),
		Addrs:   json.RawMessage(`[]`),
	}
	rider := &model.Inbound{
		Type: "vless", Tag: "rider",
		OutJson: json.RawMessage(`{"type":"vless","tag":"rider","server":"panel.example.com","server_port":8443,` +
			`"detour":"front","tls":{"enabled":true,"server_name":"cdn.example.com"},"transport":{"type":"ws","path":"/ws"}}`),
		Addrs: json.RawMessage(`[]`),
	}
	outbounds, _, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{"vless":{"uuid":"u"}}`),
		[]*model.Inbound{front, rider}, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, out := range *outbounds {
		if out["tag"] != "rider" {
			continue
		}
		found = true
		headers, _ := out["transport"].(map[string]interface{})["headers"].(map[string]interface{})
		if headers["Host"] != "cdn.example.com" {
			t.Errorf("rider Host = %v, want the SNI", headers["Host"])
		}
	}
	if !found {
		t.Fatalf("no rider outbound in %v", *outbounds)
	}
}
