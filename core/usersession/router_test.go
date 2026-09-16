package usersession

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
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

func TestGateRefusesMutedSource(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, GateAndBind)
	metadata := metadataFor("gone", "1.2.3.4:1000", plainDestination)

	// Learn the user, then remove them: with no closer, this mutes the source.
	router.Registry().Bind("gone", "1.2.3.4:1000")
	router.Registry().CloseUsers(KeepSet(nil))

	conn, peer := net.Pipe()
	defer peer.Close()
	var reported error
	router.RouteConnectionEx(context.Background(), conn, metadata, func(err error) { reported = err })

	if next.calls != 0 {
		t.Error("a muted source must not reach the router")
	}
	if !closed(t, peer) {
		t.Error("the refused connection must be closed")
	}
	if reported != ErrRemoved {
		t.Errorf("close handler got %v, want ErrRemoved", reported)
	}
}

func TestGateBindsAndForwards(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, GateAndBind)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	// The bind is what makes the next removal able to find this session.
	if cut := router.Registry().CloseUsers(KeepSet(nil)); cut != 1 {
		t.Errorf("cut = %d, want 1 -- the connection was not bound to its user", cut)
	}
}

// The deprecated pair is what shadowsocks' MultiInbound still routes through.
// Leaving it to the embedded router would let those connections past the hooks
// without a word, so it gets the same coverage as the Ex form.
func TestGateCoversDeprecatedRoutePath(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, GateAndBind)
	router.Registry().Bind("gone", "1.2.3.4:1000")
	router.Registry().CloseUsers(KeepSet(nil))

	conn, peer := net.Pipe()
	defer peer.Close()
	err := router.RouteConnection(context.Background(), conn, metadataFor("gone", "1.2.3.4:1000", plainDestination))

	if next.calls != 0 {
		t.Error("a muted source must not reach the router on the deprecated path either")
	}
	if err != ErrRemoved {
		t.Errorf("RouteConnection returned %v, want ErrRemoved", err)
	}
}

func TestMuxCarrierIsTrackedForTheRoutersLifetime(t *testing.T) {
	var trackedDuring bool
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)
	next.whileIn = func() {
		// The router blocks for as long as the multiplex session lives, so the
		// carrier has to be closable right here -- that is the whole point.
		trackedDuring = router.Registry().KickUserSessions("live") == 1
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
	// And it is forgotten once the session is over.
	if kicked := router.Registry().KickUserSessions("live"); kicked != 0 {
		t.Errorf("kicked = %d after the session ended, want 0", kicked)
	}
}

func TestMuxModeIgnoresPlainConnections(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	// A plain connection authenticates on its own and ConnTracker closes it, so
	// it has no business in the registry.
	if cut := router.Registry().CloseUsers(KeepSet(nil)); cut != 0 {
		t.Errorf("cut = %d, want 0 -- a plain connection was registered", cut)
	}
}

// trojan routes unauthenticated fallback traffic through this same router, so
// this mode must never refuse anything.
func TestMuxModeDoesNotGate(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, TrackMuxCarrier)
	router.Registry().Bind("gone", "1.2.3.4:1000")
	router.Registry().CloseUsers(KeepSet(nil))

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Error("the mux mode must not refuse a connection, fallback traffic shares this router")
	}
}

func TestBindOnlyNeitherGatesNorTracks(t *testing.T) {
	next := &fakeRouter{}
	router := WrapRouterEx(next, BindOnly)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	router.RouteConnectionEx(context.Background(), conn, metadataFor("live", "1.2.3.4:1000", plainDestination), nil)

	if next.calls != 1 {
		t.Fatalf("router calls = %d, want 1", next.calls)
	}
	// Bound, so a removal finds it -- but muted rather than closed, because
	// anytls registers the closable session elsewhere.
	if cut := router.Registry().CloseUsers(KeepSet(nil)); cut != 1 {
		t.Errorf("cut = %d, want 1", cut)
	}
	if closed(t, peer) {
		t.Error("BindOnly must not close the connection it saw")
	}
}
