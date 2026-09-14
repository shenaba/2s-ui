package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The session is the whole authentication story here, so the cookie carrying it
// must be out of reach of script and must not ride along on a cross-site
// request. Nothing in this frontend reads the cookie, so HttpOnly costs nothing.
func TestBaseSessionOptions(t *testing.T) {
	o := BaseSessionOptions(60)
	if !o.HttpOnly {
		t.Error("the session cookie must be HttpOnly: an XSS anywhere in the panel would otherwise hand the session over")
	}
	if o.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", o.SameSite)
	}
	if o.Path != "/" {
		t.Errorf("Path = %q, want /", o.Path)
	}
	if o.MaxAge != 60*60 {
		t.Errorf("MaxAge = %d, want %d", o.MaxAge, 60*60)
	}
	// Zero means a session cookie, not an immediately expired one.
	if got := BaseSessionOptions(0).MaxAge; got != 0 {
		t.Errorf("MaxAge for an unset lifetime = %d, want 0", got)
	}
}

func requestIsHTTPSFor(t *testing.T, remoteAddr string, overTLS bool, proto string) bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	c.Request.RemoteAddr = remoteAddr
	if overTLS {
		c.Request.TLS = &tls.ConnectionState{}
	}
	if proto != "" {
		c.Request.Header.Set("X-Forwarded-Proto", proto)
	}
	return requestIsHTTPS(c)
}

func TestRequestIsHTTPS(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		overTLS    bool
		proto      string
		want       bool
	}{
		{"direct TLS", "203.0.113.9:40000", true, "", true},
		{"direct plain HTTP", "203.0.113.9:40000", false, "", false},

		// Behind a reverse proxy the panel speaks plain HTTP, so the forwarded
		// scheme is the only evidence -- believed only from a peer that could
		// plausibly be that proxy.
		{"loopback proxy says https", "127.0.0.1:40000", false, "https", true},
		{"private proxy says https", "10.1.2.3:40000", false, "https", true},
		{"ipv6 loopback proxy", "[::1]:40000", false, "https", true},

		// A client on the internet setting this header on an HTTP-only panel
		// would otherwise make the browser refuse to send the cookie back,
		// locking the operator out of their own panel.
		{"a public peer is not believed", "203.0.113.9:40000", false, "https", false},
		{"http forwarded scheme", "127.0.0.1:40000", false, "http", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestIsHTTPSFor(t, tt.remoteAddr, tt.overTLS, tt.proto); got != tt.want {
				t.Errorf("requestIsHTTPS = %v, want %v", got, tt.want)
			}
		})
	}
}
