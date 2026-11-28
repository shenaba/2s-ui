package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shenaba/2s-ui/database"

	"gorm.io/gorm"
)

func TestParsePkgList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
		bad  bool
	}{
		{"lines and comments", "# header\ncom.b.app\ncom.a.app # trailing\n// note\n; ini\n", []string{"com.a.app", "com.b.app"}, false},
		{"commas and spaces", "com.a.app, com.b.app  com.c.app,com.a.app", []string{"com.a.app", "com.b.app", "com.c.app"}, false},
		{"json array", `["com.a.app", 5, "not an id", "com.b.app"]`, []string{"com.a.app", "com.b.app"}, false},
		{"urls are not mined", "https://play.google.com/store/apps/details?id=com.evil.app\nuser@com.mail.app\ncom.ok.app", []string{"com.ok.app"}, false},
		{"quoted", `"com.a.app" 'com.b.app'`, []string{"com.a.app", "com.b.app"}, false},
		{"bom", "\xef\xbb\xbfcom.a.app", []string{"com.a.app"}, false},
		{"html error page", "<!doctype html><title>404</title>", nil, true},
		{"json object", `{"ids":["com.a.app"]}`, nil, true},
		{"nothing usable", "hello\nworld", nil, true},
		{"not utf-8", "\xff\xfe", nil, true},
	}
	for _, c := range cases {
		got, err := parsePkgList([]byte(c.in))
		if c.bad {
			if err == nil {
				t.Errorf("%s: want an error, got %v", c.name, got)
			}
			continue
		}
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestParsePkgListLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= pkgListMaxIDs; i++ {
		b.WriteString("com.app.x" + itoa(i) + "\n")
	}
	if _, err := parsePkgList([]byte(b.String())); err == nil {
		t.Fatal("a list over the ID limit must be refused")
	}
}

// itoa spells a number in letters, so it can sit inside a package ID.
func itoa(i int) string {
	if i == 0 {
		return "z"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('a'+i%10)) + s
	}
	return s
}

func TestValidatePkgListURL(t *testing.T) {
	ok := []string{"https://example.com/list.txt", "https://example.com:443/a", "https://1.1.1.1/a", "https://[2606:4700:4700::1111]/a"}
	bad := []string{
		"http://example.com/a", "https://example.com:8443/a", "https://u:p@example.com/a", "https://example.com/a#x",
		"https://localhost/a", "https://intranet/a", "https://box.local/a", "https://svc.internal/a",
		"https://127.0.0.1/a", "https://10.0.0.1/a", "https://169.254.169.254/latest", "https://[::1]/a",
		"https://[fd00::1]/a", "https://100.64.0.1/a", "https://[::ffff:127.0.0.1]/a",
	}
	for _, s := range ok {
		u, _ := url.Parse(s)
		if err := validatePkgListURL(u); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range bad {
		u, _ := url.Parse(s)
		if validatePkgListURL(u) == nil {
			t.Errorf("%s: want refused", s)
		}
	}
}

func TestParseExcludePkgURLs(t *testing.T) {
	got, err := parseExcludePkgURLs(" https://a.example/1 \n\nhttps://a.example/1\nhttps://b.example/2\n")
	if err != nil || !slices.Equal(got, []string{"https://a.example/1", "https://b.example/2"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseExcludePkgURLs("https://a.example/1\nhttp://b.example/2"); err == nil {
		t.Fatal("a plain http URL must be refused")
	}
	many := make([]string, pkgListMaxSources+1)
	for i := range many {
		many[i] = "https://a.example/" + itoa(i)
	}
	if _, err := parseExcludePkgURLs(strings.Join(many, "\n")); err == nil {
		t.Fatal("more URLs than the limit must be refused")
	}
	for _, s := range []string{"-1", "366", "x", ""} {
		if _, err := parseExcludePkgInterval(s); err == nil {
			t.Errorf("interval %q must be refused", s)
		}
	}
}

func TestPkgListDue(t *testing.T) {
	now := time.Unix(100*86400, 0)
	if !pkgListDue(pkgListSource{}, 0, now, false) {
		t.Error("a URL never tried is due")
	}
	if pkgListDue(pkgListSource{Tried: 1}, 0, now, false) {
		t.Error("interval 0 never refreshes by itself")
	}
	if !pkgListDue(pkgListSource{Tried: 1}, 0, now, true) {
		t.Error("force is always due")
	}
	week := now.Unix() - 7*86400
	if pkgListDue(pkgListSource{Tried: week + 1}, 7, now, false) || !pkgListDue(pkgListSource{Tried: week}, 7, now, false) {
		t.Error("interval boundary")
	}
}

func TestPkgListCacheKeepsLastGood(t *testing.T) {
	c := pkgListCache{}
	u := "https://a.example/1"
	c.record(u, []string{"com.a.app"}, nil, time.Unix(1000, 0))
	c.record(u, nil, errors.New("HTTP 503"), time.Unix(2000, 0))
	s := c[u]
	if !slices.Equal(s.IDs, []string{"com.a.app"}) || s.OK != 1000 || s.Tried != 2000 || s.Err != "HTTP 503" {
		t.Fatalf("a failure must keep the last good list: %+v", s)
	}
	c.record(u, []string{"com.b.app"}, nil, time.Unix(3000, 0))
	if s = c[u]; s.Err != "" || s.OK != 3000 || !slices.Equal(s.IDs, []string{"com.b.app"}) {
		t.Fatalf("a success replaces it: %+v", s)
	}
	// A long error is cut on a rune boundary: byte 200 falls inside "é".
	c.record(u, nil, errors.New(strings.Repeat("a", 199)+strings.Repeat("é", 10)), time.Unix(4000, 0))
	if s = c[u]; !utf8.ValidString(s.Err) || len(s.Err) != 199 {
		t.Fatalf("truncated error = %q (%d bytes)", s.Err, len(s.Err))
	}
}

func newExcludePkgDB(t *testing.T) *SettingService {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "pkg.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return &SettingService{}
}

func TestRunPkgRefresh(t *testing.T) {
	s := newExcludePkgDB(t)
	a, b := "https://a.example/1", "https://b.example/2"
	if err := s.saveSetting(excludePkgURLsKey, a+"\n"+b); err != nil {
		t.Fatal(err)
	}
	if err := s.saveSetting(excludePkgIntervalKey, "1"); err != nil {
		t.Fatal(err)
	}
	// The downloads run concurrently.
	var mu sync.Mutex
	var calls []string
	fetch := func(_ context.Context, u string) ([]string, error) {
		mu.Lock()
		calls = append(calls, u)
		mu.Unlock()
		if u == b {
			return nil, errors.New("HTTP 404")
		}
		return []string{"com.a.app", "com.shared.app"}, nil
	}
	if err := runPkgRefresh(false, fetch); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("both new URLs are due, fetched %v", calls)
	}
	ids, err := s.GetExcludePkgsForSub()
	if err != nil || !slices.Equal(ids, []string{"com.a.app", "com.shared.app"}) {
		t.Fatalf("ids = %v, %v", ids, err)
	}

	// Nothing is due again within the interval.
	calls = nil
	if err := runPkgRefresh(false, fetch); err != nil || len(calls) != 0 {
		t.Fatalf("second pass fetched %v, %v", calls, err)
	}

	// Removing a URL drops its IDs from the subscription at once, and the
	// next refresh prunes it from the cache.
	if err := s.saveSetting(excludePkgURLsKey, b); err != nil {
		t.Fatal(err)
	}
	if ids, _ = s.GetExcludePkgsForSub(); len(ids) != 0 {
		t.Fatalf("a removed URL's IDs must not be served: %v", ids)
	}
	if err := runPkgRefresh(false, fetch); err != nil {
		t.Fatal(err)
	}
	raw, _ := s.getString(excludePkgCacheKey)
	var c pkgListCache
	if err := json.Unmarshal([]byte(raw), &c); err != nil || len(c) != 1 || c[b].Err == "" {
		t.Fatalf("cache after prune = %s", raw)
	}

	st, err := GetExcludePkgStatus()
	if err != nil || len(st.Sources) != 1 || st.Sources[0].URL != b || st.Sources[0].Err == "" {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestExcludePkgSettingsSave(t *testing.T) {
	s := newExcludePkgDB(t)
	if _, err := s.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	save := func(m map[string]string) error {
		b, _ := json.Marshal(m)
		return database.GetDB().Transaction(func(tx *gorm.DB) error { return s.Save(tx, b) })
	}
	if err := save(map[string]string{excludePkgURLsKey: "https://a.example/1\n\nhttps://a.example/1"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.getString(excludePkgURLsKey); v != "https://a.example/1" {
		t.Fatalf("stored %q", v)
	}
	if err := save(map[string]string{excludePkgURLsKey: "https://127.0.0.1/a"}); err == nil {
		t.Fatal("a private address must be refused at save")
	}
	if err := save(map[string]string{excludePkgIntervalKey: "400"}); err == nil {
		t.Fatal("an out-of-range interval must be refused at save")
	}
	if err := save(map[string]string{excludePkgCacheKey: "{}"}); err == nil {
		t.Fatal("the cache is not writable through the settings form")
	}
	all, _ := s.GetAllSetting()
	if _, ok := (*all)[excludePkgCacheKey]; ok {
		t.Fatal("the cache must not reach the settings form")
	}
}
