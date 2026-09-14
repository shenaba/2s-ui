package api

import (
	"encoding/gob"
	"net"
	"net/http"
	"strings"

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

// BaseSessionOptions are the cookie attributes that do not depend on the
// request, so the session store and every login share one definition.
//
// HttpOnly keeps the session out of reach of script. The panel renders
// operator-supplied strings in a number of places, and without it any one of
// them turning into an XSS hands the session over outright. Nothing in this
// frontend reads the cookie -- the router has never consulted it, unlike
// upstream's -- so this costs nothing here.
//
// SameSite=Strict has nothing to do with TLS and is safe in HTTP mode too. It
// stops a cross-site request carrying the session at all, which is the half of
// the CSRF answer that does not depend on the panel checking anything.
func BaseSessionOptions(maxAgeMinutes int) sessions.Options {
	o := sessions.Options{
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	if maxAgeMinutes > 0 {
		o.MaxAge = maxAgeMinutes * 60
	}
	return o
}

// requestIsHTTPS reports whether the browser reached the panel over TLS.
//
// It has to be derived per request rather than hardcoded: a browser will not
// send a Secure cookie over plain HTTP, so a fixed true breaks login on every
// HTTP-only install, and a fixed false gives up the protection on HTTPS ones.
//
// Behind a reverse proxy the panel itself speaks plain HTTP, so the forwarded
// scheme is the only evidence. It is read only from a peer that could plausibly
// be that proxy -- otherwise anyone able to reach an HTTP-only panel directly
// could set the header and make the browser refuse to send the cookie back,
// locking the operator out of their own panel. Which is the same reason
// getRemoteIp only believes forwarding headers behind nginx.
func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if !strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		return false
	}
	ip := net.ParseIP(c.RemoteIP())
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func sessionOptions(c *gin.Context, maxAgeMinutes int) sessions.Options {
	o := BaseSessionOptions(maxAgeMinutes)
	o.Secure = requestIsHTTPS(c)
	return o
}

func SetLoginUser(c *gin.Context, userName string, maxAge int) error {
	options := sessionOptions(c, maxAge)

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
