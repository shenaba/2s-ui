package sub

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"path/filepath"
	"testing"
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

// Assets carry a year of cache only when the embedded FS actually holds them.
// A 404 with explicit freshness is cacheable, so a request that lands while
// the binary is being swapped would otherwise pin a blank dashboard for a year.
func TestAssetCacheHeaderOnlyOnHits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := NewSubHandler(engine.Group("/sub/")); err != nil {
		t.Fatalf("NewSubHandler: %v", err)
	}

	name, ok := embeddedAssetName(t)
	if ok {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", "/sub/assets/"+name, nil))
		if w.Code != 200 {
			t.Errorf("GET %s = %d, want 200", name, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "max-age=31536000" {
			t.Errorf("hit Cache-Control = %q, want the year", got)
		}
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/sub/assets/does-not-exist.js", nil))
	if w.Code != 404 {
		t.Fatalf("missing asset = %d, want 404", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("404 must not be cacheable, got Cache-Control %q", got)
	}
}

// embeddedAssetName picks one file out of the built dashboard, or reports that
// this is a bare checkout serving the fallback page.
func embeddedAssetName(t *testing.T) (string, bool) {
	t.Helper()
	assets, err := subscriberAssets()
	if err != nil {
		t.Fatalf("subscriberAssets: %v", err)
	}
	entries, err := fs.ReadDir(assets, ".")
	if err != nil || len(entries) == 0 {
		return "", false
	}
	return entries[0].Name(), true
}

// The dashboard's whole point is telling a subscriber why their subscription
// stopped working, so a disabled client has to resolve -- getClientBySubId's
// enable filter would answer it with a bare 400 instead.
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
