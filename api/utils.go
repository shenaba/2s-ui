package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-gonic/gin"
)

type Msg struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

var generatedProxyPeers = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// getRemoteIp identifies the caller. X-Forwarded-For is accepted only when the
// immediate peer is trusted: the generated nginx is implicitly trusted on
// loopback, while operators of an external proxy configure its IP/CIDR through
// webTrustedProxies. Tying this to webNginx alone made custom nginx/Caddy/tunnel
// deployments record the proxy as every user's IP; trusting every peer when
// that switch was set let direct callers forge a fresh limiter identity.
func getRemoteIp(c *gin.Context) string {
	var settingService service.SettingService
	trusted, err := settingService.GetWebTrustedProxies()
	if err != nil {
		logger.Warning("read trusted proxies:", err)
		trusted = nil
	}
	if behindProxy, err := settingService.GetWebNginx(); err != nil {
		logger.Warning("read reverse-proxy setting:", err)
	} else if behindProxy {
		trusted = append(trusted, generatedProxyPeers...)
	}
	return clientIP(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), trusted)
}

// clientIP strips trusted proxies from the right of an X-Forwarded-For chain
// and returns the first untrusted hop. The generated nginx appends the address
// it observed, so client-forged entries remain to the left and can never win.
// A malformed chain fails closed to the socket peer. X-Real-IP is deliberately
// ignored because proxies that do not overwrite it would pass pure client
// input through unchanged.
func clientIP(remoteAddr, forwardedFor string, trusted []netip.Prefix) string {
	peerText, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		peerText = strings.Trim(remoteAddr, "[]")
	}
	peer, err := netip.ParseAddr(peerText)
	if err != nil {
		return peerText
	}
	peer = peer.Unmap()
	if !addressInPrefixes(peer, trusted) || strings.TrimSpace(forwardedFor) == "" {
		return peer.String()
	}

	parts := strings.Split(forwardedFor, ",")
	leftmost := peer
	for i := len(parts) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return peer.String()
		}
		addr = addr.Unmap()
		leftmost = addr
		if !addressInPrefixes(addr, trusted) {
			return addr.String()
		}
	}
	return leftmost.String()
}

func addressInPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
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
