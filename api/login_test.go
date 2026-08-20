package api

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-gonic/gin"
	logging "github.com/op/go-logging"
)

func TestTwoFaPromptDoesNotConsumeFailureBudget(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "login.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	users := service.UserService{}
	if err := users.UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if err := database.GetDB().Model(&model.User{}).
		Where("username = ?", "admin").Update("two_fa_secret", "!").Error; err != nil {
		t.Fatalf("enable test 2fa: %v", err)
	}

	gin.SetMode(gin.TestMode)
	a := ApiService{}
	request := func(code string) *httptest.ResponseRecorder {
		form := url.Values{"user": {"admin"}, "pass": {"correct-password"}}
		if code != "" {
			form.Set("code", code)
		}
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.7:1234"
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = req
		a.Login(context)
		return response
	}

	// The failure budget is what a real user needs to type a code with, so the
	// prompt must not touch it. Scope names mirror service's own constants,
	// which are unexported.
	failureRows := func() int64 {
		var n int64
		if err := database.GetDB().Model(&model.LoginAttempt{}).
			Where("scope in ?", []string{"ip", "user"}).Count(&n).Error; err != nil {
			t.Fatalf("count failure rows: %v", err)
		}
		return n
	}
	promptRows := func() int64 {
		var n int64
		if err := database.GetDB().Model(&model.LoginAttempt{}).
			Where("scope = ?", "prompt").Count(&n).Error; err != nil {
			t.Fatalf("count prompt rows: %v", err)
		}
		return n
	}

	prompt := request("")
	if !strings.Contains(prompt.Body.String(), `"twoFa":true`) {
		t.Fatalf("missing 2fa prompt: %s", prompt.Body.String())
	}
	if got := failureRows(); got != 0 {
		t.Fatalf("2fa prompt spent %d rows of the failure budget, want 0", got)
	}
	// ...but it is metered, or it would be an unbounded bcrypt comparison for
	// whoever holds a leaked password.
	if got := promptRows(); got != 1 {
		t.Fatalf("2fa prompt created %d prompt rows, want 1", got)
	}

	request("000000")
	if got := failureRows(); got != 2 {
		t.Errorf("wrong 2fa code created %d failure rows, want IP and username rows", got)
	}
}

// The prompt budget is a ceiling, not a tight limit: it has to outlast anything
// a real two-step login does, and still stop before the panel will burn bcrypt
// comparisons forever.
func TestTwoFaPromptEventuallyRefuses(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "prompt.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	users := service.UserService{}
	if err := users.UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if err := database.GetDB().Model(&model.User{}).
		Where("username = ?", "admin").Update("two_fa_secret", "!").Error; err != nil {
		t.Fatalf("enable test 2fa: %v", err)
	}

	// Every prompt costs a bcrypt comparison, so shrink the budget rather than
	// spending the shipped one -- the property under test is the ratio to the
	// failure budget, not the shipped number.
	settings := service.SettingService{}
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatalf("materialise settings: %v", err)
	}
	if err := database.GetDB().
		Exec(`UPDATE settings SET value = ? WHERE key = ?`, "1", "loginMaxFailures").Error; err != nil {
		t.Fatalf("shrink failure budget: %v", err)
	}
	maxFailures, _, _, err := settings.GetLoginGuard()
	if err != nil {
		t.Fatalf("read login guard settings: %v", err)
	}

	gin.SetMode(gin.TestMode)
	a := ApiService{}
	prompt := func() string {
		form := url.Values{"user": {"admin"}, "pass": {"correct-password"}}
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.7:1234"
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = req
		a.Login(context)
		return response.Body.String()
	}

	allowed := 0
	refused := false
	for i := 0; i < maxFailures*100; i++ {
		body := prompt()
		if strings.Contains(body, "too many failed attempts") {
			refused = true
			break
		}
		if !strings.Contains(body, `"twoFa":true`) {
			t.Fatalf("prompt %d answered unexpectedly: %s", i+1, body)
		}
		allowed++
	}
	if !refused {
		t.Fatalf("the prompt path never ran out of budget after %d prompts", allowed)
	}
	// Strictly larger than the failure budget, or a real two-step login would be
	// rationed by the same number that rations wrong passwords -- which is the
	// thing this scope exists to stop.
	if allowed <= maxFailures {
		t.Errorf("only %d prompts allowed, want more than the %d-failure budget", allowed, maxFailures)
	}
}
