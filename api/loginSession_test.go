package api

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	logging "github.com/op/go-logging"
)

// loginThrough runs a real login request through a real session store, which is
// the only way to reach SetLoginUser: sessions.Default panics without the
// middleware, so the existing login tests all stop before it.
func loginThrough(t *testing.T, username, password string) Msg {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(sessions.Sessions("s-ui-test", cookie.NewStore([]byte("test-secret"))))
	a := ApiService{}
	engine.POST("/api/login", func(c *gin.Context) { a.Login(c) })

	form := url.Values{"user": {username}, "pass": {password}}
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.9:1234"
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)

	var msg Msg
	if err := json.Unmarshal(response.Body.Bytes(), &msg); err != nil {
		t.Fatalf("decode reply: %v (%s)", err, response.Body.String())
	}
	if msg.Success && len(response.Result().Cookies()) == 0 {
		t.Error("login reported success without setting a cookie")
	}
	return msg
}

func seedUser(t *testing.T, username, password string) {
	t.Helper()
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "login.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	users := service.UserService{}
	if err := users.UpdateFirstUser(username, password); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// The session write used to be logged and then ignored, so the reply said
// success with no cookie set. The panel bounced straight back to the login form
// and the operator retyped a password that was never the problem.
//
// A cookie store refuses a value past 4096 bytes, so a long enough username is
// a session that cannot be written -- the same position as a store that is
// misconfigured or unreachable.
func TestLoginReportsASessionThatCouldNotBeStarted(t *testing.T) {
	username := strings.Repeat("u", 5000)
	seedUser(t, username, "correct-password")

	msg := loginThrough(t, username, "correct-password")
	if msg.Success {
		t.Fatalf("login reported success although no session was started: %+v", msg)
	}
	// The detail belongs in the log: it describes the session store, which is
	// nothing the person at the login form can act on.
	if strings.Contains(strings.ToLower(msg.Msg), "securecookie") {
		t.Errorf("the reply leaks the store's own error: %q", msg.Msg)
	}
	if msg.Msg == "" {
		t.Error("the reply says nothing at all")
	}
}

// And an ordinary login still succeeds and sets its cookie, so the above is not
// passing because login is broken.
func TestLoginStartsASession(t *testing.T) {
	seedUser(t, "admin", "correct-password")

	if msg := loginThrough(t, "admin", "correct-password"); !msg.Success {
		t.Fatalf("login failed: %+v", msg)
	}
}

// A wrong password is still a wrong password, and must not be reported as a
// session problem.
func TestLoginRefusesAWrongPassword(t *testing.T) {
	seedUser(t, "admin", "correct-password")

	msg := loginThrough(t, "admin", "wrong-password")
	if msg.Success {
		t.Fatal("a wrong password was accepted")
	}
	if strings.Contains(strings.ToLower(msg.Msg), "session") {
		t.Errorf("a wrong password was reported as a session problem: %q", msg.Msg)
	}
}
