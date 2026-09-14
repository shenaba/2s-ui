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
		{"same host on a custom port", http.MethodPost,
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
