package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func findUsernameFor(h *APIv2Handler, token string) string {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		c.Request.Header.Set("Token", token)
	}
	return h.findUsername(c)
}

func TestFindUsername(t *testing.T) {
	now := time.Now().Unix()
	tokens := []TokenInMemory{
		{Token: "expired-token", Username: "alice", Expiry: now - 60},
		{Token: "live-token", Username: "bob", Expiry: now + 3600},
		{Token: "forever-token", Username: "carol", Expiry: 0},
	}
	h := &APIv2Handler{tokens: &tokens}

	tests := []struct {
		name  string
		token string
		want  string
	}{
		// An expired entry used to be spliced out of the slice being ranged
		// over, which shifted every later element back by one and skipped it --
		// so the token stored right after an expired one could not log in.
		{"the entry after an expired one is still reachable", "live-token", "bob"},
		{"no expiry means no expiry", "forever-token", "carol"},
		{"an expired token is refused", "expired-token", ""},
		{"an unknown token is refused", "nope", ""},
		// Without this, a request with no Token header matches a stored token
		// that is somehow empty.
		{"an absent header is refused", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findUsernameFor(h, tt.token); got != tt.want {
				t.Errorf("findUsername = %q, want %q", got, tt.want)
			}
		})
	}
}

// An empty token slice and a nil one both have to answer "no", not panic.
func TestFindUsernameWithNoTokens(t *testing.T) {
	if got := findUsernameFor(&APIv2Handler{}, "anything"); got != "" {
		t.Errorf("nil token slice gave %q", got)
	}
	empty := []TokenInMemory{}
	if got := findUsernameFor(&APIv2Handler{tokens: &empty}, "anything"); got != "" {
		t.Errorf("empty token slice gave %q", got)
	}
}
