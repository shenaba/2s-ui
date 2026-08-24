package sub

import (
	"bytes"
	"net/http/httptest"
	"testing"

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
