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
// each inbound a field and a hook in every handler. Both see the same
// connections at the same point, but the copies under core/protocol/ are diffed
// against sing-box line for line by scripts/check-protocol-copies.sh, and this
// way each one carries a single added line instead of a block in every handler.
//
// Nothing here refuses anything: a wrapped router records what it sees and
// passes it on. Refusing by source address is what the package used to do and
// could not be made correct -- see the package comment.
//
// Every copy assigns metadata.User before calling the router, so the user a
// connection authenticated as is already known here.

// Mode is what a wrapped router does with the connections it sees. Which one an
// inbound wants follows from how its protocol reuses an authenticated
// connection.
type Mode int

const (
	// BindOnly records which user is connected from where, and nothing else.
	//
	// The QUIC inbounds take this because they have no session handle to give:
	// what the registry does for them is answer, at removal time, whether the
	// user being removed is connected at all -- which is what decides between
	// a free in-place update and rebuilding the inbound.
	//
	// anytls takes it too, for the opposite reason: it does have a closer, but
	// it holds it in NewConnection, which the router never sees, so it
	// registers the session there instead.
	BindOnly Mode = iota
	// TrackMuxCarrier ignores everything but a sing-mux carrier connection.
	// These protocols authenticate per connection, so a removed user is already
	// locked out of new ones and ConnTracker closes the routed ones -- except
	// on a multiplex session, where the carrier authenticated once and every
	// stream after that rides it. The carrier is a net.Conn we can close, so it
	// is tracked and cut.
	TrackMuxCarrier
)

type hooks struct {
	registry *Registry
	mode     Mode
}

// enter records one connection on its way to the real router, and returns the
// cleanup to run once the router is done with it, or nil if there is none.
func (h hooks) enter(conn io.Closer, metadata adapter.InboundContext) func() {
	switch h.mode {
	case TrackMuxCarrier:
		if metadata.Destination != singmux.Destination {
			return nil
		}
		source := metadata.Source.String()
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
		return func() { h.registry.Untrack(source) }
	default:
		h.registry.Bind(metadata.User, metadata.Source.String())
		return nil
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
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	r.ConnectionRouterEx.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *RouterEx) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	r.ConnectionRouterEx.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

// The deprecated pair is overridden too, so that a transport still routing
// through it cannot slip a session past the hooks unrecorded.
func (r *RouterEx) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	return r.ConnectionRouterEx.RouteConnection(ctx, conn, metadata)
}

func (r *RouterEx) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	if done := r.enter(conn, metadata); done != nil {
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
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	r.Router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *Router) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	r.Router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

func (r *Router) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	return r.Router.RouteConnection(ctx, conn, metadata)
}

func (r *Router) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	if done := r.enter(conn, metadata); done != nil {
		defer done()
	}
	return r.Router.RoutePacketConnection(ctx, conn, metadata)
}
