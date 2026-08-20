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

	prompt := request("")
	if !strings.Contains(prompt.Body.String(), `"twoFa":true`) {
		t.Fatalf("missing 2fa prompt: %s", prompt.Body.String())
	}
	var attempts int64
	if err := database.GetDB().Model(&model.LoginAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("2fa prompt created %d limiter rows, want 0", attempts)
	}

	request("000000")
	if err := database.GetDB().Model(&model.LoginAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatalf("count failed-code attempts: %v", err)
	}
	if attempts != 2 {
		t.Errorf("wrong 2fa code created %d limiter rows, want IP and username rows", attempts)
	}
}
