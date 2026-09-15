package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func sameOriginStatus(t *testing.T, method, host string, headers map[string]string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(SameOrigin())
	handle := func(c *gin.Context) { c.Status(http.StatusOK) }
	engine.GET("/x", handle)
	engine.POST("/x", handle)

	req := httptest.NewRequest(method, "http://"+host+"/x", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code
}

func TestSameOrigin(t *testing.T) {
	const host = "panel.example.com"

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		// A read cannot change anything, and the panel is also fetched by
		// clients that send no Origin at all.
		{"GET is never blocked", http.MethodGet, nil, http.StatusOK},
		{"GET from elsewhere is not blocked either", http.MethodGet,
			map[string]string{"Origin": "https://evil.example"}, http.StatusOK},

		{"same origin over https", http.MethodPost,
			map[string]string{"Origin": "https://" + host}, http.StatusOK},
		// The panel runs on plain HTTP too, and behind a TLS-terminating proxy
		// the scheme the browser used is not the one we see. Only the host is
		// compared.
		{"same host over http", http.MethodPost,
			map[string]string{"Origin": "http://" + host}, http.StatusOK},

		{"another origin", http.MethodPost,
			map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		// A subdomain is a different host, and the cookie is not scoped to it.
		{"a subdomain is not the same host", http.MethodPost,
			map[string]string{"Origin": "https://evil." + host}, http.StatusForbidden},
		{"a host that merely starts the same", http.MethodPost,
			map[string]string{"Origin": "https://" + host + ".evil.example"}, http.StatusForbidden},

		// Referer is the fallback when Origin is absent.
		{"referer from the panel", http.MethodPost,
			map[string]string{"Referer": "https://" + host + "/app/"}, http.StatusOK},
		{"referer from elsewhere", http.MethodPost,
			map[string]string{"Referer": "https://evil.example/page"}, http.StatusForbidden},

		// Neither header: a cross-site form post cannot set a custom one, so
		// the panel's own XHR marker tells them apart.
		{"no headers but the XHR marker", http.MethodPost,
			map[string]string{"X-Requested-With": "XMLHttpRequest"}, http.StatusOK},
		{"no headers at all", http.MethodPost, nil, http.StatusForbidden},
		{"an unparseable origin", http.MethodPost,
			map[string]string{"Origin": "::not a url::"}, http.StatusForbidden},
		// "null" is what a sandboxed iframe or a data: document sends.
		{"a null origin", http.MethodPost,
			map[string]string{"Origin": "null"}, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameOriginStatus(t, tt.method, host, tt.headers); got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

// The request Host and the Origin do not always carry the same port, and the
// panel is reachable in all of these shapes:
//
//   - directly on its own port, where the browser dialled exactly what the
//     panel sees;
//   - behind the nginx vhost this panel generates, which forwards $host --
//     no port -- while the browser's Origin carries the one it dialled;
//   - behind a proxy that forwards $http_host, where the two match again.
//
// So the hostname is what is compared. Comparing host:port whole rejected
// every write on a proxied panel reachable on anything but 443, which is a
// configuration that works today -- DomainValidator strips the port before its
// own comparison for the same reason.
func TestSameOriginIgnoresThePort(t *testing.T) {
	tests := []struct {
		name   string
		host   string // what the panel sees as Host
		origin string // what the browser sends
		want   int
	}{
		{"direct, same port on both", "1.2.3.4:2095", "http://1.2.3.4:2095", http.StatusOK},
		{"nginx $host, browser on 8443", "panel.example.com", "https://panel.example.com:8443", http.StatusOK},
		{"nginx $http_host, both carry the port", "panel.example.com:8443", "https://panel.example.com:8443", http.StatusOK},
		{"nginx $host, browser on 443", "panel.example.com", "https://panel.example.com", http.StatusOK},
		{"IPv6 literal", "[::1]:2095", "http://[::1]:2095", http.StatusOK},
		{"IPv6 literal, port only on one side", "[::1]", "http://[::1]:2095", http.StatusOK},

		// Dropping the port must not start accepting a different host.
		{"another host on the same port", "panel.example.com:8443", "https://evil.example:8443", http.StatusForbidden},
		{"a subdomain, ports aside", "panel.example.com", "https://evil.panel.example.com:8443", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sameOriginStatus(t, http.MethodPost, tt.host, map[string]string{"Origin": tt.origin})
			if got != tt.want {
				t.Errorf("Host %q, Origin %q -> %d, want %d", tt.host, tt.origin, got, tt.want)
			}
		})
	}
}
