package sub

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

	"github.com/gin-gonic/gin"
)

// The three profile headers, in the order util.GetHeaders returns them.
var testHeaders = []string{"upload=1; download=2; total=3; expire=0", "12", "client-name"}

func newTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return w, c
}

// The dashboard is an HTML page meant to be rendered. Content-Disposition:
// attachment makes the browser download it instead, which is exactly what
// broke the feature the first time around.
func TestDashboardHeadersOmitContentDisposition(t *testing.T) {
	s := &SubHandler{}
	w, c := newTestContext()

	s.addProfileHeaders(c, testHeaders)

	if got := w.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("dashboard response must not be an attachment, got Content-Disposition %q", got)
	}
	if got := w.Header().Get("Profile-Title"); got != "client-name" {
		t.Errorf("Profile-Title = %q, want %q", got, "client-name")
	}
	if got := w.Header().Get("Subscription-Userinfo"); got != testHeaders[0] {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, testHeaders[0])
	}
	if got := w.Header().Get("Profile-Update-Interval"); got != "12" {
		t.Errorf("Profile-Update-Interval = %q, want %q", got, "12")
	}
}

// The raw subscription keeps it: proxy clients rely on it to name the profile.
func TestRawSubscriptionKeepsContentDisposition(t *testing.T) {
	s := &SubHandler{}
	w, c := newTestContext()

	s.addHeaders(c, testHeaders)

	got := w.Header().Get("Content-Disposition")
	if got == "" {
		t.Fatal("raw subscription lost its Content-Disposition header")
	}
	if want := contentDispositionHeader("client-name"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
}

// Holds whether or not the subscriber frontend has been built: index.html when
// it has, the committed fallback.html when it hasn't.
func TestSubscriberIndexAlwaysResolves(t *testing.T) {
	page, err := subscriberIndex()
	if err != nil {
		t.Fatalf("subscriberIndex() failed: %v", err)
	}
	if !bytes.Contains(bytes.ToLower(page), []byte("<html")) {
		t.Errorf("subscriberIndex() did not return an HTML document, got %d bytes", len(page))
	}
}

// Assets carry a year of cache only when the FS actually holds them. A 404
// with explicit freshness is cacheable, so a request that lands while the
// binary is being swapped would otherwise pin a blank dashboard for a year.
// The fixture FS is what lets both halves run on a bare checkout, which is
// what CI builds from.
func TestAssetCacheHeaderOnlyOnHits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	assets := fstest.MapFS{
		"index-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	if err := (&SubHandler{}).initRouter(engine.Group("/sub/"), assets); err != nil {
		t.Fatalf("initRouter: %v", err)
	}

	hit := httptest.NewRecorder()
	engine.ServeHTTP(hit, httptest.NewRequest("GET", "/sub/assets/index-abc123.js", nil))
	if hit.Code != 200 {
		t.Errorf("GET a present asset = %d, want 200", hit.Code)
	}
	if got := hit.Header().Get("Cache-Control"); got != "max-age=31536000" {
		t.Errorf("hit Cache-Control = %q, want max-age=31536000", got)
	}

	miss := httptest.NewRecorder()
	engine.ServeHTTP(miss, httptest.NewRequest("GET", "/sub/assets/does-not-exist.js", nil))
	if miss.Code != 404 {
		t.Fatalf("missing asset = %d, want 404", miss.Code)
	}
	if got := miss.Header().Get("Cache-Control"); got != "" {
		t.Errorf("404 must not be cacheable, got Cache-Control %q", got)
	}
}

// A traversal attempt is not a name the FS holds, so it must not be marked
// cacheable either -- fs.Stat rejects the path before the static handler,
// which does its own rejecting, ever sees it.
func TestAssetCacheHeaderRejectsTraversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	assets := fstest.MapFS{"index-abc123.js": &fstest.MapFile{Data: []byte("x")}}
	if err := (&SubHandler{}).initRouter(engine.Group("/sub/"), assets); err != nil {
		t.Fatalf("initRouter: %v", err)
	}

	for _, p := range []string{"/sub/assets/../../etc/passwd", "/sub/assets/", "/sub/assets//index-abc123.js"} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if got := w.Header().Get("Cache-Control"); got != "" {
			t.Errorf("GET %s set Cache-Control %q", p, got)
		}
	}
}

// The dashboard's whole point is telling a subscriber why their subscription
// stopped working, so a disabled client has to resolve -- getClientBySubId's
// enable filter would answer it with a bare 400 instead. It gets the state and
// nothing else: disabling is how a leaked subscription URL is revoked, so the
// label, the quota and the counters must not keep answering that URL.
func TestClientInfoResolvesDisabledClientWithoutLinks(t *testing.T) {
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "sub.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	links := json.RawMessage(`[{"type":"external","remark":"r","uri":"vless://x@h:443"}]`)
	expired := time.Now().Add(-2 * time.Hour).Unix()
	if err := database.GetDB().Create(&model.Client{
		Name: "alice", Remark: "Alice", Enable: false, Expiry: expired, Links: links,
		Volume: 100 << 30, Up: 60 << 30, Down: 50 << 30,
	}).Error; err != nil {
		t.Fatalf("insert client: %v", err)
	}

	s := SubService{}
	info, err := s.getClientInfoJSON("alice")
	if err != nil {
		t.Fatalf("a disabled client must still resolve for the dashboard: %v", err)
	}
	if info.Enable {
		t.Error("Enable = true, want the disabled flag to reach the page")
	}
	if !info.Expired {
		t.Error("Expired = false for an expiry two hours in the past")
	}
	if len(info.Links) != 0 {
		t.Errorf("a disabled client was handed %d working config links", len(info.Links))
	}
	if info.Remark != "" || info.Title != "alice" {
		t.Errorf("the operator's label leaked: remark %q, title %q", info.Remark, info.Title)
	}
	if info.Volume != 0 || info.Up != 0 || info.Down != 0 || info.Used != 0 || info.Expiry != 0 {
		t.Errorf("counters leaked on a revoked URL: %+v", info)
	}

	// Marshalled, not just present: the page's type says string[], and Go
	// turns a nil slice into null.
	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"links":[]`)) {
		t.Errorf("links must marshal as an empty array, got %s", payload)
	}
}

// The last day before expiry still works, and RemainingDays truncates to 0
// there -- which is why Expired is decided from the timestamp, not from it.
func TestClientInfoNotExpiredOnItsLastDay(t *testing.T) {
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "sub.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	if err := database.GetDB().Create(&model.Client{
		Name: "bob", Enable: true, Expiry: time.Now().Add(6 * time.Hour).Unix(),
		Links: json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatalf("insert client: %v", err)
	}

	info, err := (&SubService{}).getClientInfoJSON("bob")
	if err != nil {
		t.Fatalf("getClientInfoJSON: %v", err)
	}
	if info.RemainingDays != 0 {
		t.Fatalf("RemainingDays = %d, want the truncated 0 this test is about", info.RemainingDays)
	}
	if info.Expired {
		t.Error("Expired = true six hours before expiry")
	}
}

// Subscription-Userinfo carries the same counters getClientInfoJSON withholds
// from a disabled client, so the dashboard response must not set it either --
// otherwise trimming the JSON just moves the leak into a header.
func TestDashboardWithholdsProfileHeadersWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "sub.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	rows := []model.Client{
		{Name: "off", Remark: "Paying customer", Enable: false, Volume: 100 << 30, Up: 99 << 30},
		{Name: "on", Remark: "Paying customer", Enable: true, Volume: 100 << 30, Up: 1 << 30},
	}
	if err := database.GetDB().Create(&rows).Error; err != nil {
		t.Fatalf("insert clients: %v", err)
	}

	serve := func(name string) *httptest.ResponseRecorder {
		w, c := newTestContext()
		(&SubHandler{}).serveDashboard(c, name)
		return w
	}

	off := serve("off")
	if off.Code != 200 {
		t.Fatalf("a disabled client must still get the page, got %d", off.Code)
	}
	for _, h := range []string{"Subscription-Userinfo", "Profile-Title", "Profile-Update-Interval"} {
		if got := off.Header().Get(h); got != "" {
			t.Errorf("disabled client leaked %s: %q", h, got)
		}
	}

	on := serve("on")
	if got := on.Header().Get("Subscription-Userinfo"); got == "" {
		t.Error("a live client lost its Subscription-Userinfo header")
	}
	if got := on.Header().Get("Profile-Title"); got != "Paying customer" {
		t.Errorf("Profile-Title = %q, want the remark", got)
	}
}

// NewSubHandler is the production entry point, and the only caller that reads
// the embedded FS rather than a fixture. A bare checkout has no
// dashboard/assets directory at all -- which is what CI and any from-source
// build start from -- so registering against a missing one has to keep
// working, and the fixture-driven tests above no longer touch that path.
func TestNewSubHandlerRegistersOnABareCheckout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := NewSubHandler(engine.Group("/sub/")); err != nil {
		t.Fatalf("NewSubHandler: %v", err)
	}

	// A name the FS cannot hold either way, so the assertion holds whether or
	// not the dashboard happens to have been built into this tree.
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/sub/assets/not-a-real-bundle.js", nil))
	if w.Code != 404 {
		t.Errorf("missing asset over the embedded FS = %d, want 404", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("404 must not be cacheable, got Cache-Control %q", got)
	}
}
