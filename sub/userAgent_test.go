package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func TestFormatForUserAgent(t *testing.T) {
	for ua, want := range map[string]string{
		"":                               "",
		"Happ/3.4.1":                     "happ",
		"happ":                           "happ",
		"clash-verge/v2.2.3":             "clash",
		"ClashX/1.118.0":                 "clash",
		"ClashMetaForAndroid/2.11.5":     "clash",
		"FlClash/0.8.80":                 "clash",
		"mihomo/1.19.1":                  "clash",
		"Stash/2.5.0 Clash/1.0":          "clash",
		"SFA/1.11.0 (Android; sing-box)": "json",
		"SFI/1.11.0":                     "json",
		"sing-box 1.11.0 (go1.23)":       "json",
		// Hiddify says it is like Clash and sing-box; only the product counts.
		"HiddifyNext/2.5.7 (android) like ClashMeta v2ray sing-box": "",
		"v2rayNG/1.9.30":          "",
		"Shadowrocket/2070":       "",
		"Mozilla/5.0 (Macintosh)": "",
		"curl/8.4.0":              "",
	} {
		if got := formatForUserAgent(ua); got != want {
			t.Errorf("formatForUserAgent(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestHappLinks(t *testing.T) {
	got := happLinks([]string{
		"socks5://u:p@1.2.3.4:1080#s",
		"SOCKS5://u:p@1.2.3.4:1081#upper",
		"http://u:p@1.2.3.4:8080#h",
		"https://u:p@1.2.3.4:8443#hs",
		"naive+https://u:p@1.2.3.4:443#n",
		"vless://uuid@1.2.3.4:443#v",
		"  ",
		"",
	})
	want := []string{
		"socks://u:p@1.2.3.4:1080#s",
		"socks://u:p@1.2.3.4:1081#upper",
		"naive+https://u:p@1.2.3.4:443#n",
		"vless://uuid@1.2.3.4:443#v",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("happLinks = %v, want %v", got, want)
	}
}

// With no ?format the bare URL answers each well-known client in the format it
// reads, says so in Vary, and leaves everything else -- a browser included --
// on the plain links. An explicit ?format always wins, and the dashboard is
// never picked by User-Agent.
func TestSubFormatFollowsUserAgent(t *testing.T) {
	setupSubDB(t)
	links := `[{"type":"local","remark":"s","uri":"socks5://u:p@1.2.3.4:1080#s"},` +
		`{"type":"local","remark":"h","uri":"http://u:p@1.2.3.4:8080#h"},` +
		`{"type":"local","remark":"v","uri":"vless://5bdbac27-7a82-42c5-9ef4-751f64bd15e4@1.2.3.4:443#v"}]`
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: "ua", Config: json.RawMessage(clientVlessConfig),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(links),
	}).Error; err != nil {
		t.Fatal(err)
	}
	engine, subPath := antiProbingSubEngine(t)

	get := func(query, ua string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", subPath+"ua"+query, nil)
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("GET %s (UA %q) = %d: %s", query, ua, w.Code, w.Body.String())
		}
		return w
	}
	plain := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		raw, err := base64.StdEncoding.DecodeString(w.Body.String())
		if err != nil {
			t.Fatalf("plain links are not base64 (subEncode is on by default): %q", w.Body.String())
		}
		return string(raw)
	}
	isJSON := func(w *httptest.ResponseRecorder) bool {
		var v map[string]interface{}
		return json.Unmarshal(w.Body.Bytes(), &v) == nil && v["outbounds"] != nil
	}
	isClash := func(w *httptest.ResponseRecorder) bool {
		return strings.Contains(w.Body.String(), "proxies:")
	}

	for _, ua := range []string{"", "v2rayNG/1.9.30", "Mozilla/5.0 (Macintosh)"} {
		w := get("", ua)
		if body := plain(w); !strings.Contains(body, "socks5://") || !strings.Contains(body, "http://") {
			t.Errorf("UA %q: plain links changed: %q", ua, body)
		}
		if w.Header().Get("Vary") != "User-Agent" {
			t.Errorf("UA %q: Vary = %q, want User-Agent", ua, w.Header().Get("Vary"))
		}
		// A shared cache that ignores Vary must not store it either.
		if w.Header().Get("Cache-Control") != "private" {
			t.Errorf("UA %q: Cache-Control = %q, want private", ua, w.Header().Get("Cache-Control"))
		}
	}

	happ := plain(get("", "Happ/3.4.1"))
	if !strings.Contains(happ, "socks://u:p@") || strings.Contains(happ, "socks5://") ||
		strings.Contains(happ, "http://") || !strings.Contains(happ, "vless://") {
		t.Errorf("Happ links = %q, want socks:// and vless:// without http://", happ)
	}
	if explicit := plain(get("?format=happ", "")); explicit != happ {
		t.Errorf("?format=happ = %q, want what Happ's User-Agent gets: %q", explicit, happ)
	}

	if w := get("", "clash-verge/v2.2.3"); !isClash(w) {
		t.Errorf("Clash agent did not get a Clash config: %.200s", w.Body.String())
	}
	if w := get("", "SFA/1.11.0 (Android; sing-box 1.11.0)"); !isJSON(w) {
		t.Errorf("sing-box agent did not get a sing-box config: %.200s", w.Body.String())
	}

	// An explicit format wins over the agent, and carries no Vary: the
	// answer no longer depends on it.
	w := get("?format=json", "clash-verge/v2.2.3")
	if !isJSON(w) {
		t.Errorf("?format=json from a Clash agent: %.200s", w.Body.String())
	}
	if w.Header().Get("Vary") != "" || w.Header().Get("Cache-Control") != "" {
		t.Errorf("explicit format sent Vary %q / Cache-Control %q", w.Header().Get("Vary"), w.Header().Get("Cache-Control"))
	}
	if w := get("?format=clash", "SFA/1.11.0"); !isClash(w) {
		t.Errorf("?format=clash from a sing-box agent: %.200s", w.Body.String())
	}
}

// The dashboard and its JSON are reached only by naming them: under any agent
// an explicit ?format=page/info is still the page, and no agent turns the bare
// URL into one -- handing a proxy client HTML reads as a broken subscription.
func TestSubDashboardIsNeverPickedByUserAgent(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: "dash", Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	engine, subPath := antiProbingSubEngine(t)
	serve := func(query, ua string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", subPath+"dash"+query, nil)
		req.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	isPage := func(w *httptest.ResponseRecorder) bool {
		return strings.Contains(w.Header().Get("Content-Type"), "text/html")
	}
	for _, ua := range []string{"clash-verge/v2.2.3", "SFA/1.11.0", "Happ/3.4.1", "Mozilla/5.0 (Macintosh)", ""} {
		if w := serve("?format=page", ua); w.Code != 200 || !isPage(w) {
			t.Errorf("UA %q: ?format=page = %d %q, want the dashboard", ua, w.Code, w.Header().Get("Content-Type"))
		}
		if w := serve("?format=info", ua); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("UA %q: ?format=info = %d %q, want its JSON", ua, w.Code, w.Header().Get("Content-Type"))
		}
		if w := serve("", ua); isPage(w) {
			t.Errorf("UA %q: the bare URL served the dashboard", ua)
		}
	}
}

// A format the agent picked falls back to the plain links when it cannot be
// built -- one inbound row whose addrs are not JSON fails the JSON and Clash
// builds, not the stored links -- while a format named in the query is still
// answered as asked.
func TestSubAgentFormatFallsBackToLinks(t *testing.T) {
	setupSubDB(t)
	db := database.GetDB()
	in := &model.Inbound{Type: "vless", Tag: "bad", Options: json.RawMessage(`{}`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"bad","server":"h","server_port":1}`)}
	if err := db.Create(in).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE inbounds SET addrs = ? WHERE id = ?", []byte(`{not json`), in.Id).Error; err != nil {
		t.Fatal(err)
	}
	ids, _ := json.Marshal([]uint{in.Id})
	if err := db.Create(&model.Client{Enable: true, Name: "fb", Config: json.RawMessage(clientVlessConfig),
		Inbounds: ids, Links: json.RawMessage(`[{"type":"local","remark":"v","uri":"vless://u@h:1#v"}]`)}).Error; err != nil {
		t.Fatal(err)
	}
	engine, subPath := antiProbingSubEngine(t)
	serve := func(query, ua string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", subPath+"fb"+query, nil)
		req.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	links := base64.StdEncoding.EncodeToString([]byte("vless://u@h:1#v"))
	for _, ua := range []string{"clash.meta/1.19.1", "SFA/1.11.0"} {
		if w := serve("", ua); w.Code != 200 || w.Body.String() != links {
			t.Errorf("UA %q: bare URL = %d %q, want the plain links", ua, w.Code, w.Body.String())
		}
	}
	for _, q := range []string{"?format=clash", "?format=json"} {
		if w := serve(q, "clash.meta/1.19.1"); w.Code != 400 {
			t.Errorf("%s with a broken row = %d, want the error it always gave", q, w.Code)
		}
	}
}

// The panel's QR codes encode the client name as a path segment
// (encodeURIComponent), and names may hold '#', '?' or spaces: the server
// has to decode it back to the client.
func TestSubResolvesAnEncodedClientName(t *testing.T) {
	setupSubDB(t)
	name := "vip #1?a"
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: name, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[{"type":"local","remark":"v","uri":"vless://u@h:1#v"}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	engine, subPath := antiProbingSubEngine(t)
	// What encodeURIComponent makes of the name.
	req := httptest.NewRequest("GET", subPath+"vip%20%231%3Fa?format=happ", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("encoded name = %d %q, want the client's subscription", w.Code, w.Body.String())
	}
}
