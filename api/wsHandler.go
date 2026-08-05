package api

import (
	"net/http"

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
	// nil options keep coder/websocket's same-host Origin check (CSWSH
	// protection); DomainValidator has already pinned Host when webDomain is
	// set, and the generated nginx vhost forwards $host. Auth is
	// handshake-only: an open socket outlives its session cookie until it
	// drops — it only carries data the user was already seeing, and the next
	// HTTP call still logs the browser out.
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		// Accept has already written the HTTP error response.
		return
	}
	service.HubServe(conn, getHostname(c))
}
