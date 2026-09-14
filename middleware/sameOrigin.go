package middleware

import (
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
// Only the host is compared, never a hardcoded scheme. The panel has to work on
// plain HTTP as well as HTTPS, and behind a TLS-terminating proxy the scheme
// the browser used and the one the panel sees differ anyway -- requiring https
// here would reject every legitimate request in two of the three deployments.
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
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, c.Request.Host) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
