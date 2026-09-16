package usersession

import (
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	singmux "github.com/sagernet/sing-mux"
)

// fakeRouter is the next hop. It records what reached it and, while it is
// "routing", lets a test look at the registry -- which is how the carrier's
// lifetime is asserted: a mux carrier must be tracked for exactly as long as
// the router call lasts.
type fakeRouter struct {
	calls   int
	last    adapter.InboundContext
	whileIn func()
}

func (r *fakeRouter) enter(metadata adapter.InboundContext) {
	r.calls++
	r.last = metadata
	if r.whileIn != nil {
		r.whileIn()
	}
}

func (r *fakeRouter) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	r.enter(metadata)
	return nil
}

func (r *fakeRouter) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	r.enter(metadata)
	return nil
}

func (r *fakeRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.enter(metadata)
}

func (r *fakeRouter) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.enter(metadata)
}

// fakePacketConn is the smallest thing that satisfies N.PacketConn. Only Close
// is ever reached; the rest exist to satisfy the interface.
type fakePacketConn struct {
	closed bool
}

func (c *fakePacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	return M.Socksaddr{}, io.EOF
}
func (c *fakePacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error { return nil }
func (c *fakePacketConn) Close() error                                                  { c.closed = true; return nil }
func (c *fakePacketConn) LocalAddr() net.Addr                                           { return M.Socksaddr{} }
func (c *fakePacketConn) SetDeadline(t time.Time) error                                 { return nil }
func (c *fakePacketConn) SetReadDeadline(t time.Time) error                             { return nil }
func (c *fakePacketConn) SetWriteDeadline(t time.Time) error                            { return nil }

func metadataFor(user string, source string, destination M.Socksaddr) adapter.InboundContext {
	return adapter.InboundContext{
		User:        user,
		Source:      M.ParseSocksaddr(source),
		Destination: destination,
	}
}

var plainDestination = M.ParseSocksaddr("example.com:443")

// closed reports whether one end of a pipe was closed, by reading from the
// other end. The deadline is what makes "still open" answerable at all: a pipe
// nobody closed would otherwise block this read forever.
func closed(t *testing.T, peer net.Conn) bool {
	t.Helper()
	// net.Pipe refuses a deadline once either end is closed, so this failing is
	// itself the answer.
	if err := peer.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		return true
	}
	buf := make([]byte, 1)
	_, err := peer.Read(buf)
	if err == nil || os.IsTimeout(err) {
		return false
	}
	return true
}

// Nothing here refuses a connection, in any mode: refusing by source address is
// what this package used to do and could not be made correct. Every mode is
// asserted against that below -- a gate creeping back in is the regression this
// file exists to catch.
func TestNoModeEverRefuses(t *testing.T) {
	for _, mode := range []struct {
		name string
		mode Mode
	}{
		{"BindOnly", BindOnly},
		{"TrackMuxCarrier", TrackMuxCarrier},
	} {
		t.Run(mode.name, func(t *testing.T) {
			next := &fakeRouter{}
			router := WrapRouterEx(next, mode.mode)
			// Learn a user, then remove them. Whatever the registry made of
			// that, the next connection from the same address still goes
			// through: for TrackMuxCarrier because trojan's fallback shares
			// this router, and for BindOnly because the address may by now
			// belong to somebody else entirely.
			router.Registry().Bind("gone", "1.2.3.4:1000")
			router.Registry().CloseUsers(KeepSet(nil))

			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			var reported error
			router.RouteConnectionEx(context.Background(), conn, metadataFor("gone", "1.2.3.4:1000", plainDestination), func(err error) { reported = err })

			if next.calls != 1 {
				t.Errorf("router calls = %d, want 1 -- the connection was refused", next.calls)
			}
			if reported != nil {
				t.Errorf("close handler got %v, want nothing", reported)
			}
			if closed(t, peer) {
				t.Error("the connection was closed")
			}
		})
	}
}

func TestBindOnlyRecordsTheUser(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, BindOnly)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	// Bound, so a removal knows this user is connected -- but with no closer,
	// because the session this connection belongs to is either a QUIC one
	// (unreachable) or an anytls one registered elsewhere.
	got := router.Registry().CloseUsers(KeepSet(nil))
	if got != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v, want {Unclosable:1}", got)
	}
	if closed(t, peer) {
		t.Error("BindOnly must not close the connection it saw")
	}
}

// The deprecated pair is overridden so a transport still routing through it
// cannot slip a session past unrecorded.
func TestDeprecatedRoutePathIsRecorded(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, BindOnly)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	err := router.RouteConnection(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination))
	if err != nil {
		t.Fatalf("RouteConnection returned %v", err)
	}
	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	got := router.Registry().CloseUsers(KeepSet(nil))
	if got != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v, want {Unclosable:1} -- the deprecated path did not record the user", got)
	}
}

func TestMuxCarrierIsTrackedForTheRoutersLifetime(t *testing.T) {
	var trackedDuring bool
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)
	next.whileIn = func() {
		// The router blocks for as long as the multiplex session lives, so the
		// carrier has to be closable right here -- that is the whole point.
		trackedDuring = router.Registry().KickUserSessions("live") == (Result{Cut: 1})
	}

	conn, peer := net.Pipe()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", singmux.Destination), nil)

	if !trackedDuring {
		t.Error("the mux carrier was not closable while the router had it")
	}
	if !closed(t, peer) {
		t.Error("kicking the user must have closed the carrier")
	}
}

// The carrier is forgotten once its session ends. Left behind, the entry
// outlives the session it names, and the next removal cuts a connection that
// is not there any more -- or, on an inbound that reports instead, asks for a
// restart on behalf of a client that already left.
//
// Kicking first (as the test above does) deletes the entry by itself, so this
// has to be its own case to mean anything.
func TestMuxCarrierIsUntrackedWhenTheSessionEnds(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	// fakeRouter returns immediately, which stands in for the session ending.
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", singmux.Destination), nil)

	got := router.Registry().CloseUsers(KeepSet(nil))
	if got != (Result{}) {
		t.Errorf("CloseUsers = %+v, want an empty result -- the carrier outlived its session", got)
	}
}

func TestMuxModeIgnoresPlainConnections(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)
	var registeredDuring Result
	next.whileIn = func() {
		// Asked while the router still has the connection. Asking afterwards
		// proves nothing: the deferred Untrack empties the registry either way,
		// so the assertion would hold even if every connection were registered.
		registeredDuring = router.Registry().CloseUsers(KeepSet(nil))
	}

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	// A plain connection authenticates on its own and ConnTracker closes it, so
	// it has no business in the registry.
	if registeredDuring != (Result{}) {
		t.Errorf("CloseUsers = %+v mid-route, want an empty result -- a plain connection was registered", registeredDuring)
	}
	if closed(t, peer) {
		t.Error("a plain connection must not be closed by this layer")
	}
}

// A packet conn cannot be a mux carrier. If one is registered as though it
// were, a removal closes it -- so the carrier slot has to stay empty, which is
// also what keeps a nil net.Conn out of a non-nil io.Closer.
func TestMuxModeDoesNotTakeAPacketConnAsCarrier(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)
	conn := &fakePacketConn{}
	var registeredDuring Result
	next.whileIn = func() {
		registeredDuring = router.Registry().CloseUsers(KeepSet(nil))
	}

	router.RoutePacketConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", singmux.Destination), nil)

	if registeredDuring != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v mid-route, want {Unclosable:1}", registeredDuring)
	}
	if conn.closed {
		t.Error("a packet conn was closed as though it were a mux carrier")
	}
}
