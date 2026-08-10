package api

import (
	"net"
	"net/http"
	"strings"

	"github.com/shenaba/2s-ui/logger"

	"github.com/gin-gonic/gin"
)

type Msg struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

func getRemoteIp(c *gin.Context) string {
	value := c.GetHeader("X-Forwarded-For")
	if value != "" {
		ips := strings.Split(value, ",")
		return ips[0]
	} else {
		addr := c.Request.RemoteAddr
		ip, _, _ := net.SplitHostPort(addr)
		return ip
	}
}

func getHostname(c *gin.Context) string {
	return normalizeHost(c.Request.Host)
}

// normalizeHost strips the port and brackets a bare IPv6 literal, so the result
// is usable as the server field of a generated link. Anything reaching link
// generation must pass through here — a configured domain included, since the
// settings form accepts whatever was pasted into it.
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	// A pasted URL is rejected outright rather than trimmed into shape:
	// SplitHostPort on "https://example.com" splits at the scheme's colon and
	// hands back "https", which would then be baked into every generated link
	// as the server name. Returning empty lets the caller fall back to the
	// request Host, which is at least reachable.
	if strings.ContainsAny(host, "/\\") {
		return ""
	}
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		} else {
			// No port to split off, so this is a bare IPv6 literal — a Host
			// header always brackets one, but the settings field takes it
			// either way. Unwrap so the re-bracketing below is idempotent;
			// discarding the value here (what SplitHostPort's zero return
			// used to do) silently blanked the host instead of bracketing it.
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return host
}

func jsonMsg(c *gin.Context, msg string, err error) {
	jsonMsgObj(c, msg, nil, err)
}

func jsonObj(c *gin.Context, obj interface{}, err error) {
	jsonMsgObj(c, "", obj, err)
}

func jsonMsgObj(c *gin.Context, msg string, obj interface{}, err error) {
	m := Msg{
		Obj: obj,
	}
	if err == nil {
		m.Success = true
		if msg != "" {
			m.Msg = msg
		}
	} else {
		m.Success = false
		if msg != "" {
			m.Msg = msg + ": " + err.Error()
		} else {
			m.Msg = err.Error()
		}
		logger.Warning("failed :", err)
	}
	c.JSON(http.StatusOK, m)
}

func pureJsonMsg(c *gin.Context, success bool, msg string) {
	if success {
		c.JSON(http.StatusOK, Msg{
			Success: true,
			Msg:     msg,
		})
	} else {
		c.JSON(http.StatusOK, Msg{
			Success: false,
			Msg:     msg,
		})
	}
}

func checkLogin(c *gin.Context) {
	if !IsLogin(c) {
		if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
			pureJsonMsg(c, false, "Invalid login")
		} else {
			c.Redirect(http.StatusTemporaryRedirect, "/login")
		}
		c.Abort()
	} else {
		c.Next()
	}
}
