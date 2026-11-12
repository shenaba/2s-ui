package vless

import (
	"context"
	"net"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/core/transport"
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// UpdateUsers swaps the user table of a running inbound and cuts the multiplex
// sessions of everyone who just left it. VLESS authenticates per connection, so
// the table alone already shuts a removed user out of new ones -- except on a
// multiplex session, where one authenticated carrier connection keeps serving
// every stream opened after the change.
func (h *Inbound) UpdateUsers(users []option.VLESSUser) error {
	h.service.UpdateUsers(common.Map(users, func(it option.VLESSUser) string {
		return it.Name
	}), common.Map(users, func(it option.VLESSUser) string {
		return it.UUID
	}), common.Map(users, func(it option.VLESSUser) string {
		return it.Flow
	}))
	// Unclosable is deliberately not checked: a mux carrier is a net.Conn and
	// is cut outright, so a removed user never costs this inbound the rebuild a
	// QUIC one does. The only thing that can be counted here is a packet conn
	// addressed to the mux destination -- not a carrier, and not worth
	// disconnecting everyone else for.
	h.sessions().CloseUsers(usersession.KeepSet(common.Map(users, func(it option.VLESSUser) string {
		return it.Name
	})))
	return nil
}

// withUserSessions installs the session registry in front of the router. Only
// the multiplex carrier is tracked: every other connection authenticates on its
// own and is already covered by ConnTracker. See core/usersession.
func withUserSessions(router adapter.ConnectionRouterEx) adapter.ConnectionRouterEx {
	return usersession.WrapRouterEx(router, usersession.TrackMuxCarrier)
}

// sessions reaches the registry withUserSessions installed. The assertion is
// bare on purpose: reordering NewInbound so that it no longer holds would
// otherwise turn every hook here into a silent no-op.
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
func takeProxyHdr(options *option.ListenOptions) proxyHdrSettings {
	return proxyheader.Take(options)
}

// proxyHdrSettings is the type of the proxyHdr field, named here so that the
// copy's own file needs no import for it.
type proxyHdrSettings = proxyheader.Settings

// proxySettings is proxyHdr as it applies: off over a transport that carries
// no TCP. The transport is built before any of these are called.
func (h *Inbound) proxySettings() proxyHdrSettings {
	if h.transport == nil {
		return h.proxyHdr
	}
	return h.proxyHdr.Over(h.transport)
}

func (h *Inbound) connectionHandler() adapter.ConnectionHandler {
	return proxyheader.Handler(h, h.logger, h.proxySettings())
}

// GuardedPort tells the core this inbound believes a header a client
// could send it through the core -- it reads PROXY headers, or X-Forwarded-For
// from this host -- and on which port: proxyheader's dial guard stays on while
// it runs, and refuses to hand a client to a relay on this host for that port.
func (h *Inbound) GuardedPort() (uint16, bool) {
	settings := h.proxySettings()
	return settings.Port, settings.Guarded()
}

func (h *Inbound) proxyListener(l net.Listener) net.Listener {
	return proxyheader.Listen(l, h.logger, h.proxySettings())
}

// newServerTransport is v2ray.NewServerTransport with X-Forwarded-For believed
// only from a CDN edge or, for an inbound listening on loopback, a reverse
// proxy on this host (#279). proxyHdr is taken after the transport is built,
// and before anything is served, so it is read when a request comes; only a
// transport that reads X-Forwarded-For calls this, so TrustsHost is all there
// is to ask.
func newServerTransport(ctx context.Context, logger logger.ContextLogger, options option.V2RayTransportOptions, tlsConfig tls.ServerConfig, handler *inboundTransportHandler) (adapter.V2RayServerTransport, error) {
	h := (*Inbound)(handler)
	return transport.NewServer(ctx, logger, options, tlsConfig, handler, func(peer M.Socksaddr, forwarded []string) M.Socksaddr {
		return proxyheader.Forwarded(h.logger, peer, forwarded, h.proxyHdr.TrustsHost())
	})
}
