package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// checkLogin answered 200 with {"success":false,"msg":"Invalid login"}, so the
// only way to tell an expired session from a refusal was to match on English
// prose. The panel is not the only thing that talks to this API.
//
// The body is unchanged on purpose: anything already matching on the message
// keeps working, and the status is what a client should have been able to read
// all along.
func TestCheckLoginAnswersUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("s-ui-test", cookie.NewStore([]byte("test-secret"))))
	// Wired the way the real router wires it: as middleware, so c.Abort stops
	// the chain rather than only the rest of one handler.
	engine.Use(func(c *gin.Context) { checkLogin(c) })
	engine.GET("/api/load", func(c *gin.Context) {
		c.JSON(http.StatusOK, Msg{Success: true})
	})

	t.Run("an api call", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/load", nil)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)

		if response.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
		var msg Msg
		if err := json.Unmarshal(response.Body.Bytes(), &msg); err != nil {
			t.Fatalf("decode reply: %v (%s)", err, response.Body.String())
		}
		if msg.Success {
			t.Error("an unauthenticated request was reported as a success")
		}
		// The frontend's logout path matches on this, and an older one still
		// will after this change.
		if msg.Msg != "Invalid login" {
			t.Errorf("msg = %q, want the message kept as it was", msg.Msg)
		}
	})

	// A browser asking for a page still gets sent to the login form rather than
	// a JSON body it would render as text.
	t.Run("a page request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/load", nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)

		if response.Code != http.StatusTemporaryRedirect {
			t.Errorf("status = %d, want %d", response.Code, http.StatusTemporaryRedirect)
		}
		if got := response.Header().Get("Location"); got != "/login" {
			t.Errorf("Location = %q, want /login", got)
		}
	})
}
