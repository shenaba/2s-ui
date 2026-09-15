package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// SameOrigin rejects state-changing requests that did not come from the panel's
// own pages.
//
// The session is a cookie, so without this any page the operator visits while
// logged in can post to the panel in their name -- add a client, change the
// password, import a database. SameSite=Strict on the cookie already covers
// current browsers; this is the half that does not depend on the browser being
// current, and it is what answers a request that arrives with an Origin from
// somewhere else.
//
// Only the hostname is compared -- never a hardcoded scheme, and never the
// port. The panel has to work on plain HTTP as well as HTTPS, and behind a
// TLS-terminating proxy the scheme the browser used and the one the panel sees
// differ anyway, so requiring https here would reject every legitimate request
// in two of the three deployments. The port is dropped for the same kind of
// reason: nginx's $host, which the vhost this panel generates forwards as
// Host, carries no port, while the browser's Origin carries the one it
// actually dialled -- so comparing them whole rejects every write on a proxied
// panel reachable on anything but 443. DomainValidator already strips the port
// before its own comparison, for the same deployment.
//
// Mounted on the cookie-authenticated group only. apiv2 authenticates with a
// Token header, which a cross-site page cannot set without a CORS preflight the
// panel never answers.
func SameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			origin = c.GetHeader("Referer")
		}
		if origin == "" {
			// Neither header. A cross-site form post cannot set a custom one,
			// so the panel's own XHR marker tells the two apart -- and it is
			// already what checkLogin answers on.
			if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
				c.Next()
				return
			}
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || !strings.EqualFold(hostname(u.Host), hostname(c.Request.Host)) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// hostname drops the port from a host:port, leaving anything without one --
// and an IPv6 literal's brackets -- as it is.
func hostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}
