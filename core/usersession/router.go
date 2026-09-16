package usersession

import (
	"context"
	"io"
	"net"

	"github.com/sagernet/sing-box/adapter"
	N "github.com/sagernet/sing/common/network"

	singmux "github.com/sagernet/sing-mux"
)

// The registry is reached by wrapping an inbound's router rather than by giving
// each inbound a field and a hook in every handler. Both put the gate in the
// same place -- before anything is routed, so the connections the router
// answers itself (hijack-dns) are covered and nothing is dialed before the
// refusal -- but the copies under core/protocol/ are diffed against sing-box
// line for line by scripts/check-protocol-copies.sh, and this way each one
// carries a single added line instead of a block in every handler.
//
// Every copy assigns metadata.User before calling the router, so the user a
// connection authenticated as is already known here.

// Mode is what a wrapped router does with the connections it sees. Which one an
// inbound wants follows from how its protocol reuses an authenticated
// connection.
type Mode int

const (
	// GateAndBind refuses connections from a muted session and records the user
	// behind every other one. For the QUIC protocols the session itself has no
	// closer this package can reach, so muting is the only thing that stops a
	// removed user, and the gate is what does the work.
	GateAndBind Mode = iota
	// TrackMuxCarrier ignores everything but a sing-mux carrier connection.
	// These protocols authenticate per connection, so a removed user is already
	// locked out of new ones and ConnTracker closes the routed ones -- except
	// on a multiplex session, where the carrier authenticated once and every
	// stream after that rides it. The carrier is a net.Conn we can close, so it
	// is tracked and cut rather than muted.
	TrackMuxCarrier
	// BindOnly records the user and nothing else, for an inbound that reaches
	// its session by another route. anytls holds its session in NewConnection,
	// which never goes through the router.
	BindOnly
)

type hooks struct {
	registry *Registry
	mode     Mode
}

// enter applies the mode to one connection on its way to the real router. It
// reports whether the connection may proceed, and returns the cleanup to run
// once the router is done with it.
func (h hooks) enter(conn io.Closer, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) (bool, func()) {
	source := metadata.Source.String()
	switch h.mode {
	case TrackMuxCarrier:
		if metadata.Destination != singmux.Destination {
			return true, nil
		}
		// Only a stream-oriented carrier can be closed; a packet conn reaching
		// here would not be a mux carrier anyway. Assigned through a nil-able
		// io.Closer rather than passed directly: a nil net.Conn put into an
		// interface is not a nil interface, and would register as a closer that
		// panics when the session is cut.
		var carrier io.Closer
		if streamConn, ok := conn.(net.Conn); ok {
			carrier = streamConn
		}
		// One call, one lock: see BindAndTrack for why these cannot be split.
		h.registry.BindAndTrack(metadata.User, source, carrier)
		// The router blocks for as long as the multiplex session lives, so
		// untracking when it returns is not early.
		return true, func() { h.registry.Untrack(source) }
	case BindOnly:
		h.registry.Bind(metadata.User, source)
		return true, nil
	default:
		if !h.registry.Allowed(source) {
			Reject(conn, onClose)
			return false, nil
		}
		h.registry.Bind(metadata.User, source)
		return true, nil
	}
}

// RouterEx wraps an inbound whose router field is an adapter.ConnectionRouterEx.
type RouterEx struct {
	adapter.ConnectionRouterEx
	hooks
}

// WrapRouterEx returns router with the session hooks in front of it.
func WrapRouterEx(router adapter.ConnectionRouterEx, mode Mode) *RouterEx {
	return &RouterEx{
		ConnectionRouterEx: router,
		hooks:              hooks{registry: NewRegistry(), mode: mode},
	}
}

// Registry is how the owning inbound reaches the registry it just installed.
func (r *RouterEx) Registry() *Registry {
	return r.registry
}

func (r *RouterEx) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	proceed, done := r.enter(conn, metadata, onClose)
	if !proceed {
		return
	}
	if done != nil {
		defer done()
	}
	r.ConnectionRouterEx.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *RouterEx) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	proceed, done := r.enter(conn, metadata, onClose)
	if !proceed {
		return
	}
	if done != nil {
		defer done()
	}
	r.ConnectionRouterEx.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

// The deprecated pair is overridden too, not for completeness: shadowsocks'
// MultiInbound still routes through it, and leaving it to the embedded router
// would let those connections past the hooks without a word.
func (r *RouterEx) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	proceed, done := r.enter(conn, metadata, nil)
	if !proceed {
		return ErrRemoved
	}
	if done != nil {
		defer done()
	}
	return r.ConnectionRouterEx.RouteConnection(ctx, conn, metadata)
}

func (r *RouterEx) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	proceed, done := r.enter(conn, metadata, nil)
	if !proceed {
		return ErrRemoved
	}
	if done != nil {
		defer done()
	}
	return r.ConnectionRouterEx.RoutePacketConnection(ctx, conn, metadata)
}

// Router wraps an inbound whose router field is the full adapter.Router, which
// hysteria and hysteria2 hold. Everything it does not override is forwarded by
// the embedded interface.
type Router struct {
	adapter.Router
	hooks
}

// WrapRouter returns router with the session hooks in front of it.
func WrapRouter(router adapter.Router, mode Mode) *Router {
	return &Router{
		Router: router,
		hooks:  hooks{registry: NewRegistry(), mode: mode},
	}
}

func (r *Router) Registry() *Registry {
	return r.registry
}

func (r *Router) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	proceed, done := r.enter(conn, metadata, onClose)
	if !proceed {
		return
	}
	if done != nil {
		defer done()
	}
	r.Router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *Router) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	proceed, done := r.enter(conn, metadata, onClose)
	if !proceed {
		return
	}
	if done != nil {
		defer done()
	}
	r.Router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

func (r *Router) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	proceed, done := r.enter(conn, metadata, nil)
	if !proceed {
		return ErrRemoved
	}
	if done != nil {
		defer done()
	}
	return r.Router.RouteConnection(ctx, conn, metadata)
}

func (r *Router) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	proceed, done := r.enter(conn, metadata, nil)
	if !proceed {
		return ErrRemoved
	}
	if done != nil {
		defer done()
	}
	return r.Router.RoutePacketConnection(ctx, conn, metadata)
}
