package api

import (
	"net/http"
	"time"

	"github.com/shenaba/2s-ui/service"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// WsHandler upgrades the panel's push channel. It is registered directly on
// the engine as a static sibling of the api group — putting it inside the
// group would conflict with the GET /:getAction wildcard and panic at startup.
func WsHandler(c *gin.Context) {
	// Browser websocket handshakes cannot carry X-Requested-With, so
	// checkLogin's redirect-vs-JSON split is useless here; a bare 401 lets the
	// client treat it like any other failed connect.
	if !IsLogin(c) {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	// Authentication is checked here and nowhere else, so without a deadline an
	// open socket would keep streaming the full config — client credentials and
	// the subscription URI — long after its session cookie stopped being
	// accepted. Cap the connection at the session's own max age measured from
	// now: an active session pays one silent reconnect per session lifetime,
	// and an expired one is refused at that reconnect's handshake.
	//
	// A zero deadline means the setting is unset, which makes the cookie a
	// browser-session cookie — it dies with the tab, and so does the socket.
	var deadline time.Time
	var ss service.SettingService
	if maxAge, err := ss.GetSessionMaxAge(); err == nil && maxAge > 0 {
		deadline = time.Now().Add(time.Duration(maxAge) * time.Minute)
	}
	// nil options keep coder/websocket's same-host Origin check (CSWSH
	// protection); DomainValidator has already pinned Host when webDomain is
	// set, and the generated nginx vhost forwards $host.
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		// Accept has already written the HTTP error response.
		return
	}
	service.HubServe(conn, getHostname(c), deadline)
}
