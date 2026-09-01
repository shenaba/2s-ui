package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/util"

	"github.com/gin-gonic/gin"
)

type Msg struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

// generatedProxyPeers covers the usual generated-nginx case, where the vhost
// dials the panel on loopback. It is not the whole story -- see
// generatedProxyPeer.
var generatedProxyPeers = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// generatedProxyPeer is the extra peer to trust when webListen names a concrete
// address: service/acme.go's upstreamAddr only falls back to loopback for an
// empty or wildcard listen, so the generated vhost dials the panel's own bind
// address and nginx reaches it from there. Without this the vhost's own
// X-Forwarded-For is discarded and every login is attributed to that address --
// the last-login column shows it for everybody and the limiter's per-address
// budget becomes one budget for the whole panel.
func generatedProxyPeer(listen string) (netip.Prefix, bool) {
	host := strings.Trim(strings.TrimSpace(listen), "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		// Wildcard listens are dialled on loopback, already covered above.
		return netip.Prefix{}, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), true
}

// getRemoteIp identifies the caller. X-Forwarded-For is accepted only when the
// immediate peer is trusted: the generated nginx is implicitly trusted on the
// address it dials the panel at, while operators of an external proxy configure
// its IP/CIDR through webTrustedProxies. Tying this to webNginx alone made
// custom nginx/Caddy/tunnel deployments record the proxy as every user's IP;
// trusting every peer when that switch was set let direct callers forge a fresh
// limiter identity.
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
		if listen, err := settingService.GetListen(); err != nil {
			logger.Warning("read panel listen address:", err)
		} else if peer, ok := generatedProxyPeer(listen); ok {
			trusted = append(trusted, peer)
		}
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
	return bareHost(c.Request.Host)
}

// bareHost strips the port and any IPv6 brackets, so the result is the host on
// its own — the shape a config file wants (sing-box's "server", Clash's
// "server", the vmess "add"). Whoever builds a URL out of it brackets it back
// with util.HostForURI; doing that here instead would mean every config-shaped
// consumer had to undo it, which is how "[2001:db8::1]" ended up in generated
// configs in the first place (#1220).
//
// Anything reaching link generation must pass through here — a configured
// domain included, since the settings form accepts whatever was pasted in.
func bareHost(host string) string {
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
			// either way. Unwrapping here is what makes the result uniform;
			// discarding the value (what SplitHostPort's zero return used to
			// do) silently blanked the host instead.
			host = util.NormalizeHost(host)
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
