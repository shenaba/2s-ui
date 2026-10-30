package trojan

import (
	"net"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
)

// UpdateUsers swaps the user table of a running inbound and cuts the multiplex
// sessions of everyone who just left it -- see the note in vless/users.go.
// Sessions are only cut once the table actually changed: trojan rejects a
// duplicate name and fails the whole update, and that must leave the inbound
// exactly as it was.
func (h *Inbound) UpdateUsers(users []option.TrojanUser) error {
	err := h.service.UpdateUsers(common.Map(users, func(it option.TrojanUser) string {
		return it.Name
	}), common.Map(users, func(it option.TrojanUser) string {
		return it.Password
	}))
	if err != nil {
		return err
	}
	// Unclosable is not checked here; see the note in vless/users.go.
	h.sessions().CloseUsers(usersession.KeepSet(common.Map(users, func(it option.TrojanUser) string {
		return it.Name
	})))
	return nil
}

// withUserSessions installs the session registry in front of the router. Only
// the multiplex carrier is tracked: every other connection authenticates on its
// own and is already covered by ConnTracker. The fallback path routes
// unauthenticated visitors through this same router, which is one more reason
// nothing here may refuse a connection.
func withUserSessions(router adapter.ConnectionRouterEx) adapter.ConnectionRouterEx {
	return usersession.WrapRouterEx(router, usersession.TrackMuxCarrier)
}

func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.RouterEx).Registry()
}

// KickUser cuts the sessions of a user who stays on the inbound -- see
// Core.DisconnectUser.
func (h *Inbound) KickUser(user string) usersession.Result {
	return h.sessions().KickUserSessions(user)
}

// takeProxyHdr, connectionHandler and proxyListener are the PROXY protocol
// hooks (#260): NewInbound keeps whether the inbound asked for headers, and
// both of its listeners -- its own, and the one a transport serves -- read
// them when it did. See core/proxyheader.
func takeProxyHdr(options *option.ListenOptions) bool {
	return proxyheader.Take(options)
}

func (h *Inbound) connectionHandler() adapter.ConnectionHandler {
	return proxyheader.Handler(h, h.logger, h.proxyHdr)
}

// ReadsProxyHeaders tells the core this inbound reads PROXY headers, which
// keeps proxyheader's dial guard on while it runs.
func (h *Inbound) ReadsProxyHeaders() bool {
	return h.proxyHdr
}

func (h *Inbound) proxyListener(l net.Listener) net.Listener {
	return proxyheader.Listen(l, h.logger, h.proxyHdr)
}
