package util

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/sagernet/sing-box/common/tls"
)

// echPems returns a fresh config PEM and key PEM, each split the way
// generateECHKeyPair stores them.
func echPems(t *testing.T) ([]interface{}, []interface{}) {
	t.Helper()
	configPem, keyPem, err := tls.ECHKeygenDefault("public.example.com")
	if err != nil {
		t.Fatalf("ECHKeygenDefault: %v", err)
	}
	split := func(s string) []interface{} {
		var out []interface{}
		for _, line := range strings.Split(s, "\n") {
			out = append(out, line)
		}
		return out
	}
	return split(configPem), split(keyPem)
}

func echTlsRow(t *testing.T, serverEch, clientEch map[string]interface{}) *model.Tls {
	t.Helper()
	server, _ := json.Marshal(map[string]interface{}{"enabled": true, "server_name": "real.example.com", "ech": serverEch})
	client, _ := json.Marshal(map[string]interface{}{"ech": clientEch})
	return &model.Tls{Id: 1, Server: server, Client: client}
}

// A share link carries the ECH config as the bare base64 ECHConfigList, and
// importing that link gives back a config sing-box can pem.Decode (#1339).
func TestEchLinkRoundTrips(t *testing.T) {
	config, key := echPems(t)
	oTls := prepareTls(echTlsRow(t,
		map[string]interface{}{"enabled": true, "key": key},
		map[string]interface{}{"config": config}))

	ech, _ := oTls["ech"].(map[string]interface{})
	if !AsBool(ech["enabled"]) {
		t.Fatalf("client ECH not switched on by the server side: %v", oTls["ech"])
	}
	if _, ok := ech["key"]; ok {
		t.Fatal("the ECH key reached the client half")
	}

	links := vlessLink(map[string]interface{}{"uuid": "u"},
		map[string]interface{}{"transport": map[string]interface{}{"type": "tcp"}},
		[]map[string]interface{}{{"server": "1.2.3.4", "server_port": float64(443), "remark": "r", "tls": oTls}})
	u, err := url.Parse(links[0])
	if err != nil {
		t.Fatal(err)
	}
	want := ECHConfigBase64(config)
	if want == "" || u.Query().Get("ech") != want {
		t.Fatalf("link ech = %q, want %q (%s)", u.Query().Get("ech"), want, links[0])
	}

	out, _, err := GetOutbound(links[0], 0)
	if err != nil {
		t.Fatalf("GetOutbound: %v", err)
	}
	gotEch := (*out)["tls"].(map[string]interface{})["ech"].(map[string]interface{})
	if !AsBool(gotEch["enabled"]) || ECHConfigBase64(gotEch["config"]) != want {
		t.Errorf("imported ech = %v, want the same ECHConfigList back", gotEch)
	}

	// vmess carries it in its JSON body.
	vm := vmessLink(map[string]interface{}{"uuid": "u"}, map[string]interface{}{},
		[]map[string]interface{}{{"server": "1.2.3.4", "server_port": float64(443), "remark": "r", "tls": oTls}})
	if got := vmessObj(t, vm[0])["ech"]; got != want {
		t.Errorf("vmess ech = %v, want %q", got, want)
	}
	vmOut, _, err := GetOutbound(vm[0], 0)
	if err != nil {
		t.Fatalf("GetOutbound vmess: %v", err)
	}
	if e, _ := (*vmOut)["tls"].(map[string]interface{})["ech"].(map[string]interface{}); ECHConfigBase64(e["config"]) != want {
		t.Errorf("vmess imported ech = %v", e)
	}
}

// No ech param for an ECH that is off, has no config, or holds the wrong PEM
// block: the last one would otherwise publish the private key.
func TestEchLinkOmitsUnusableConfig(t *testing.T) {
	config, key := echPems(t)
	for name, row := range map[string]*model.Tls{
		"server ECH off": echTlsRow(t, map[string]interface{}{"enabled": false}, map[string]interface{}{"config": config}),
		"no config":      echTlsRow(t, map[string]interface{}{"enabled": true}, map[string]interface{}{"query_server_name": "ech.example.com"}),
		"key pasted":     echTlsRow(t, map[string]interface{}{"enabled": true}, map[string]interface{}{"config": key}),
	} {
		var params []LinkParam
		getTlsParams(&params, prepareTls(row), "vless")
		for _, p := range params {
			if p.Key == "ech" {
				t.Errorf("%s: link carries ech=%q", name, p.Value)
			}
		}
	}
}

func TestEchFromLink(t *testing.T) {
	config, _ := echPems(t)
	b64 := ECHConfigBase64(config)
	list, _ := base64.StdEncoding.DecodeString(b64)

	got := echFromLink(b64)
	if got == nil || ECHConfigBase64(got["config"]) != b64 {
		t.Errorf("base64 config: %v", got)
	}
	// A + left raw in the query decodes to a space.
	if spaced := strings.ReplaceAll(b64, "+", " "); spaced != b64 {
		if got := echFromLink(spaced); got == nil || ECHConfigBase64(got["config"]) != b64 {
			t.Errorf("config with + read as space: %v", got)
		}
	}
	if got := echFromLink(base64.RawURLEncoding.EncodeToString(list)); got != nil && got["query_server_name"] != nil {
		t.Errorf("url-safe base64 read as a domain: %v", got)
	}

	got = echFromLink("ech.example.com+https://1.1.1.1/dns-query")
	if got == nil || got["query_server_name"] != "ech.example.com" || got["config"] != nil {
		t.Errorf("domain+DNS form: %v", got)
	}
	for _, junk := range []string{"", "   ", "notbase64!", "aGVsbG8="} {
		if got := echFromLink(junk); got != nil {
			t.Errorf("echFromLink(%q) = %v, want nil", junk, got)
		}
	}
}

// One bad config is reported once, however many client links convert it.
func TestBadECHConfigWarnedOnce(t *testing.T) {
	_, key := echPems(t)
	count := func() int {
		n := 0
		for _, line := range logger.GetLogs(10240, "WARNING") {
			if strings.Contains(line, "stored ECH config is not a") {
				n++
			}
		}
		return n
	}
	before := count()
	for i := 0; i < 50; i++ {
		if got := ECHConfigBase64(key); got != "" {
			t.Fatalf("the ECH KEYS block converted: %q", got)
		}
	}
	if got := count() - before; got != 1 {
		t.Errorf("%d warnings for one bad config converted 50 times, want 1", got)
	}
}
