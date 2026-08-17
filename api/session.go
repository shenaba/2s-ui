package api

import (
	"encoding/gob"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	loginUser = "LOGIN_USER"
	// Digest of the credentials this session was issued under; see
	// service.CredentialFingerprint. Sessions issued before this field existed
	// carry no value and are rejected, so an upgrade costs everyone one login.
	loginCred = "LOGIN_CRED"
)

func init() {
	gob.Register(model.User{})
}

func SetLoginUser(c *gin.Context, userName string, maxAge int) error {
	options := sessions.Options{
		Path:   "/",
		Secure: false,
	}
	if maxAge > 0 {
		options.MaxAge = maxAge * 60
	}

	s := sessions.Default(c)
	s.Set(loginUser, userName)
	s.Set(loginCred, service.CredentialFingerprint(userName))
	s.Options(options)

	return s.Save()
}

func SetMaxAge(c *gin.Context) error {
	s := sessions.Default(c)
	s.Options(sessions.Options{
		Path: "/",
	})
	return s.Save()
}

func GetLoginUser(c *gin.Context) string {
	s := sessions.Default(c)
	obj := s.Get(loginUser)
	if obj == nil {
		return ""
	}
	objStr, ok := obj.(string)
	if !ok || objStr == "" {
		return ""
	}

	// The cookie is signed, so its contents are trustworthy -- what it cannot
	// say is whether the credentials still are. An empty fingerprint means the
	// user row is gone or unreadable and vouches for nothing, so it is compared
	// as a rejection rather than matched against a session that carries none.
	want := service.CredentialFingerprint(objStr)
	if want == "" {
		return ""
	}
	if cred, ok := s.Get(loginCred).(string); !ok || cred != want {
		return ""
	}
	return objStr
}

func IsLogin(c *gin.Context) bool {
	return GetLoginUser(c) != ""
}

func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	s.Options(sessions.Options{
		Path:   "/",
		MaxAge: -1,
	})
	s.Save()
}
