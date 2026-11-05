package proxyheader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pires/go-proxyproto"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// tail stands in for the ClientHello a header is followed by. Every read test
// checks it is still there in full: reading past the header would eat into it.
const tail = "\x16\x03\x01client-hello"

var local = M.ParseSocksaddr("127.0.0.1:12345")

// send writes data into a pipe from its own goroutine (a pipe write blocks
// until it is read) and returns the reading end.
func send(t *testing.T, data string) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		server.Close()
		client.Close()
	})
	go client.Write([]byte(data))
	return server
}

func v2(t *testing.T, header *proxyproto.Header) string {
	t.Helper()
	raw, err := header.Format()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertTail(t *testing.T, conn net.Conn) {
	t.Helper()
	buf := make([]byte, len(tail))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read after header: %v", err)
	}
	if string(buf) != tail {
		t.Fatalf("bytes after header = %q, want %q", buf, tail)
	}
}

func TestRead(t *testing.T) {
	v4 := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51000}
	v6 := &net.TCPAddr{IP: net.ParseIP("2001:db8::7"), Port: 51000}
	front := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
	front6 := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 443}
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"v1 tcp4", "PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n", "203.0.113.7:51000"},
		{"v1 tcp6", "PROXY TCP6 2001:db8::7 ::1 51000 443\r\n", "[2001:db8::7]:51000"},
		{"v2 tcp4", v2(t, proxyproto.HeaderProxyFromAddrs(2, v4, front)), "203.0.113.7:51000"},
		{"v2 tcp6", v2(t, proxyproto.HeaderProxyFromAddrs(2, v6, front6)), "[2001:db8::7]:51000"},
		{"v4-mapped source is unwrapped", "PROXY TCP6 ::ffff:203.0.113.7 ::1 51000 443\r\n", "203.0.113.7:51000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := send(t, c.header+tail)
			got, err := Read(conn)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != c.want {
				t.Fatalf("source = %s, want %s", got, c.want)
			}
			assertTail(t, conn)
		})
	}
}

func TestReadRefuses(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"no header", tail + "........"},
		{"v1 without CRLF", "PROXY TCP4 " + string(make([]byte, 120))},
		{"v1 malformed", "PROXY TCP4 not-an-address 127.0.0.1 1 2\r\n"},
		{"v2 claiming too much", "\r\n\r\n\x00\r\nQUIT\n\x21\x11\xff\xff"},
		{"cut short", "PROXY TC"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			go func() {
				client.Write([]byte(c.data))
				client.Close()
			}()
			if got, err := Read(server); err == nil {
				t.Fatalf("accepted %q as a header naming %s", c.data, got)
			}
		})
	}
}

func TestReadTimesOut(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	start := time.Now()
	if _, err := readWithin(server, 50*time.Millisecond); err == nil {
		t.Fatal("a connection that never sent a header was accepted")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("gave up after %s", elapsed)
	}
}

func TestTrusted(t *testing.T) {
	arrived := M.ParseSocksaddr("198.51.100.1:443")
	cases := []struct {
		source string
		want   bool
	}{
		{"127.0.0.1:5000", true},
		{"127.8.9.10:5000", true},
		{"[::1]:5000", true},
		{"[::ffff:127.0.0.1]:5000", true},
		{"198.51.100.1:5000", true}, // this host, by the address it arrived on
		{"[::ffff:198.51.100.1]:5000", true},
		{"198.51.100.2:5000", false},
		{"203.0.113.7:5000", false},
		{"[2001:db8::7]:5000", false},
	}
	for _, c := range cases {
		if got := Trusted(M.ParseSocksaddr(c.source), arrived); got != c.want {
			t.Errorf("Trusted(%s) = %v, want %v", c.source, got, c.want)
		}
	}
	if Trusted(M.Socksaddr{}, arrived) {
		t.Error("an invalid source counted as trusted")
	}
}

// A front the operator named is trusted although it is not this host: the
// Docker bridge's gateway, which is where a front on the host arrives from.
func TestTrustedNamedFronts(t *testing.T) {
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32"), netip.MustParsePrefix("10.8.0.0/16")})
	defer SetTrusted(nil)
	arrived := M.ParseSocksaddr("172.18.0.2:10443")
	cases := []struct {
		source string
		want   bool
	}{
		{"172.18.0.1:5000", true},
		{"[::ffff:172.18.0.1]:5000", true},
		{"10.8.3.4:5000", true},
		{"172.18.0.3:5000", false},
		{"203.0.113.7:5000", false},
	}
	for _, c := range cases {
		if got := Trusted(M.ParseSocksaddr(c.source), arrived); got != c.want {
			t.Errorf("Trusted(%s) = %v, want %v", c.source, got, c.want)
		}
	}
	SetTrusted(nil)
	if Trusted(M.ParseSocksaddr("172.18.0.1:5000"), arrived) {
		t.Error("a front was still trusted after the list was cleared")
	}
}

// addrConn is a connection the core has dialed, as far as GuardDial looks.
type addrConn struct {
	net.Conn
	local, remote net.Addr
}

func (c addrConn) LocalAddr() net.Addr  { return c.local }
func (c addrConn) RemoteAddr() net.Addr { return c.remote }

func dialedConn(local, remote string) net.Conn {
	return addrConn{local: M.ParseSocksaddr(local).TCPAddr(), remote: M.ParseSocksaddr(remote).TCPAddr()}
}

// inUseForTest switches the dial guard on for one test. The core sets it as
// soon as it builds a PROXY-enabled inbound; these tests build none.
//
// With no ports given it stands for an inbound on every port (port 0).
func inUseForTest(t *testing.T, ports ...uint16) {
	set := portSet{}
	for _, port := range ports {
		set[port] = struct{}{}
	}
	if len(set) == 0 {
		set[0] = struct{}{}
	}
	inUse.Store(&set)
	t.Cleanup(func() { inUse.Store(nil) })
}

// on is the Settings of an inbound that reads headers.
var on = Settings{Enabled: true, Port: 10443}

// hostAddrsForTest makes addrs this host's interface addresses for one test.
func hostAddrsForTest(t *testing.T, addrs ...string) {
	set := make(map[netip.Addr]struct{}, len(addrs))
	for _, addr := range addrs {
		set[netip.MustParseAddr(addr)] = struct{}{}
	}
	// Loaded in the future, so it does not expire inside the test.
	hostAddrs.snapshot.Store(&addrSnapshot{addrs: set, loaded: time.Now().Add(time.Hour)})
	t.Cleanup(func() { hostAddrs.snapshot.Store(nil) })
}

// guardDial is GuardDial with the route given rather than asked for.
func guardDial(conn net.Conn, destination M.Socksaddr, route Route) error {
	ports := inUse.Load()
	if ports == nil {
		return nil
	}
	return guardPorts(conn, destination, route, *ports)
}

// guardDirect is guardDial for a direct outbound.
func guardDirect(conn net.Conn, destination M.Socksaddr) error {
	return guardDial(conn, destination, Route{Direct: true})
}

// A direct dial that landed on this host on a PROXY-enabled inbound's port is
// refused at the dial, before a byte of the client's goes down it, and so is
// one to a named front; anything else is left alone.
func TestGuardDial(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "198.51.100.1", "203.0.113.5")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	cases := []struct {
		name    string
		local   string
		remote  string
		refused bool
	}{
		{"loopback on the PROXY port", "127.0.0.1:40001", "127.0.0.1:10443", true},
		{"v4-mapped loopback on the PROXY port", "[::ffff:127.0.0.1]:40002", "[::ffff:127.0.0.1]:10443", true},
		{"this host's own address on the PROXY port", "198.51.100.1:40003", "198.51.100.1:10443", true},
		{"loopback on another port", "127.0.0.1:40008", "127.0.0.1:8080", false},
		{"anywhere else", "198.51.100.1:40004", "203.0.113.7:443", false},
		{"a named front", "172.18.0.2:40005", "172.18.0.1:443", true},
		// A direct outbound bound to an address inside a named range, dialing
		// another of this host's addresses: that arrives from a trusted source.
		{"a source inside a named range, to this host", "172.18.0.1:40006", "203.0.113.5:10443", true},
		{"a source inside a named range, dialing out", "172.18.0.1:40007", "203.0.113.9:443", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := guardDirect(dialedConn(c.local, c.remote), M.ParseSocksaddr(c.remote))
			if (err != nil) != c.refused {
				t.Fatalf("refused = %v (%v), want %v", err != nil, err, c.refused)
			}
		})
	}
}

// With no inbound reading PROXY headers, the guard costs clients nothing: a
// named front and this host are dialed like anywhere else.
func TestGuardDialIdleWithoutProxyInbounds(t *testing.T) {
	inUse.Store(nil)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	if err := guardDirect(dialedConn("172.18.0.2:40030", "172.18.0.1:443"), M.ParseSocksaddr("172.18.0.1:443")); err != nil {
		t.Fatalf("a named front was refused with no PROXY inbound: %v", err)
	}
	if err := guardDirect(dialedConn("127.0.0.1:40031", "127.0.0.1:10443"), M.ParseSocksaddr("127.0.0.1:10443")); err != nil {
		t.Fatalf("a loopback dial was refused with no PROXY inbound: %v", err)
	}
}

// The interface addresses are re-read once they are old, and a failure to
// read them counts every address as this host's rather than none.
func TestAddrCache(t *testing.T) {
	loads := 0
	cache := addrCache{load: func() (map[netip.Addr]struct{}, error) {
		loads++
		return map[netip.Addr]struct{}{netip.MustParseAddr("203.0.113.5"): {}}, nil
	}}
	if !cache.contains(netip.MustParseAddr("203.0.113.5")) || cache.contains(netip.MustParseAddr("203.0.113.9")) {
		t.Fatal("wrong answer from a fresh load")
	}
	cache.contains(netip.MustParseAddr("203.0.113.5"))
	if loads != 1 {
		t.Fatalf("loaded %d times inside the TTL, want 1", loads)
	}
	cache.snapshot.Load().loaded = time.Now().Add(-2 * hostAddrsTTL)
	cache.contains(netip.MustParseAddr("203.0.113.5"))
	if loads != 2 {
		t.Fatalf("loaded %d times after expiry, want 2", loads)
	}
	failedLoads, warnings := 0, 0
	failing := addrCache{
		load: func() (map[netip.Addr]struct{}, error) { failedLoads++; return nil, io.ErrUnexpectedEOF },
		warn: func(error) { warnings++ },
	}
	for i := 0; i < 3; i++ {
		if !failing.contains(netip.MustParseAddr("203.0.113.9")) {
			t.Fatal("an unreadable address list counted the address as elsewhere")
		}
	}
	// A failure is kept for the TTL like a good read: retried on every dial,
	// it would put a syscall behind one lock in front of each connection.
	if failedLoads != 1 || warnings != 1 {
		t.Fatalf("%d loads and %d warnings for three lookups inside the TTL, want 1 each", failedLoads, warnings)
	}
	failing.snapshot.Load().loaded = time.Now().Add(-2 * hostAddrsTTL)
	failing.contains(netip.MustParseAddr("203.0.113.9"))
	if failedLoads != 2 {
		t.Fatalf("a failed read was not retried after the TTL (%d loads)", failedLoads)
	}
	if real, err := interfaceAddrs(); err != nil || len(real) == 0 {
		t.Fatalf("interfaceAddrs = %v, %v; want this machine's addresses", real, err)
	}
}

// RefreshInUse follows the running core, except that it never switches the
// guard off for a Handler built while it was looking, nor when it could not
// look at all.
func TestRefreshInUse(t *testing.T) {
	t.Cleanup(func() { inUse.Store(nil) })
	// As an inbound's Handler does when it is built.
	markInUse(10443)
	RefreshInUse(func() []uint16 { return nil })
	if InUse() {
		t.Fatal("still in use with no inbound reading headers")
	}
	RefreshInUse(func() []uint16 { return []uint16{10443, 10444} })
	if ports := *inUse.Load(); !ports.has(10443) || !ports.has(10444) || ports.has(443) {
		t.Fatalf("ports = %v, want 10443 and 10444", ports)
	}
	RefreshInUse(func() []uint16 { return []uint16{10444} })
	if (*inUse.Load()).has(10443) {
		t.Fatal("kept the port of an inbound no longer running")
	}
	RefreshInUse(func() []uint16 {
		Handler(&recordHandler{}, log.NewNOPFactory().Logger(), Settings{Enabled: true, Port: 10445})
		return nil
	})
	if !InUse() || !(*inUse.Load()).has(10445) {
		t.Fatal("dropped the port of an inbound built while the core was looked at")
	}
}

// One built while a refresh looks makes it look again, rather than keep every
// port it had: a port no inbound reads any more would otherwise stay.
func TestRefreshInUseLooksAgain(t *testing.T) {
	t.Cleanup(func() { inUse.Store(nil) })
	markInUse(1111) // an inbound since gone
	looks := 0
	RefreshInUse(func() []uint16 {
		looks++
		if looks == 1 {
			Handler(&recordHandler{}, log.NewNOPFactory().Logger(), Settings{Enabled: true, Port: 2222})
			return nil
		}
		return []uint16{2222}
	})
	ports := inUse.Load()
	if looks != 2 || ports == nil || !ports.has(2222) || ports.has(1111) {
		t.Fatalf("looks = %d, ports = %v; want a second look finding only 2222", looks, ports)
	}
}

// With no PROXY-enabled inbound ever built, a refresh does not even look.
func TestRefreshInUseIdle(t *testing.T) {
	inUseMu.Lock()
	saved := built
	built = 0
	inUseMu.Unlock()
	t.Cleanup(func() {
		inUseMu.Lock()
		built = saved
		inUseMu.Unlock()
	})
	inUse.Store(nil)
	RefreshInUse(func() []uint16 {
		t.Fatal("looked at the cores with nothing to find")
		return nil
	})
}

// Port 0 is an inbound that picked its own port: every port is its.
func TestPortSetAnyPort(t *testing.T) {
	if !(portSet{0: {}}).has(443) {
		t.Fatal("port 0 did not stand for every port")
	}
	if (portSet{10443: {}}).has(443) {
		t.Fatal("matched a port no inbound listens on")
	}
}

// slowConn is a TCP Fast Open dial as sing-box hands it over: no addresses
// until the first write opens the socket.
type slowConn struct {
	net.Conn
	local, remote net.Addr
	open          bool
	written       []byte
}

func (c *slowConn) Write(p []byte) (int, error) {
	c.open = true
	c.written = append(c.written, p...)
	return len(p), nil
}

func (c *slowConn) NeedHandshake() bool {
	return !c.open
}

func (c *slowConn) LocalAddr() net.Addr {
	if !c.open {
		return M.Socksaddr{}
	}
	return c.local
}

func (c *slowConn) RemoteAddr() net.Addr {
	if !c.open {
		return M.Socksaddr{}
	}
	return c.remote
}

// A TCP Fast Open dial would open its socket with the client's first bytes in
// it -- a forged header among them -- before anything could look at where it
// goes. The guard opens it first, empty, and judges it like any other.
func TestGuardDialOpensASlowDialFirst(t *testing.T) {
	inUseForTest(t, 10443)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)

	loop := &slowConn{local: M.ParseSocksaddr("127.0.0.1:40040").TCPAddr(), remote: M.ParseSocksaddr("127.0.0.1:10443").TCPAddr()}
	if err := guardDirect(loop, M.ParseSocksaddr("127.0.0.1:10443")); err == nil {
		t.Fatal("a slow dial to this host's PROXY port was let through")
	}
	if !loop.open || len(loop.written) != 0 {
		t.Fatalf("open = %v, wrote %q; want it opened with nothing sent", loop.open, loop.written)
	}

	front := &slowConn{local: M.ParseSocksaddr("172.18.0.2:40041").TCPAddr(), remote: M.ParseSocksaddr("172.18.0.1:443").TCPAddr()}
	if err := guardDirect(front, M.ParseSocksaddr("172.18.0.1:443")); err == nil {
		t.Fatal("a slow dial to a named front was let through")
	}

	// Over a proxy outbound the slow dial sits under that protocol's conn,
	// which must not be written to: an empty write there could send its
	// request header early.
	inner := &slowConn{local: M.ParseSocksaddr("127.0.0.1:40042").TCPAddr(), remote: M.ParseSocksaddr("127.0.0.1:10443").TCPAddr()}
	outer := &protocolConn{slowConn: inner}
	if err := guardDirect(outer, M.ParseSocksaddr("127.0.0.1:10443")); err == nil {
		t.Fatal("a slow dial under a protocol conn to this host's PROXY port was let through")
	}
	if outer.wrote || !inner.open {
		t.Fatalf("protocol conn written = %v, inner opened = %v; want only the inner one opened", outer.wrote, inner.open)
	}
}

// protocolConn is a proxy protocol's conn over a dial, as sing-box chains them.
type protocolConn struct {
	*slowConn
	wrote bool
}

func (c *protocolConn) Write(p []byte) (int, error) {
	c.wrote = true
	return c.slowConn.Write(p)
}

func (c *protocolConn) Upstream() any {
	return c.slowConn
}

// However many times clients have been refused this host's PROXY port, the
// front's own connections from this host -- on whatever ephemeral port Linux
// hands them, the refused dials' among them -- are read as before: nothing
// about those dials outlives them for a front's connection to match.
func TestRefusedDialsNeverCatchTheFront(t *testing.T) {
	inUseForTest(t, 10443)
	for port := 40020; port < 40120; port++ {
		from := "127.0.0.1:" + strconv.Itoa(port)
		if err := guardDirect(dialedConn(from, "127.0.0.1:10443"), M.ParseSocksaddr("localhost:10443")); err == nil {
			t.Fatalf("a client dial from %s to the PROXY port was let through", from)
		}
	}
	const header = "PROXY TCP4 198.51.100.9 127.0.0.1 51000 443\r\n"
	next := &recordHandler{size: len(tail), got: make(chan reached, 1)}
	metadata := adapter.InboundContext{Source: M.ParseSocksaddr("127.0.0.1:40020"), OriginDestination: M.ParseSocksaddr("127.0.0.1:10443")}
	Handler(next, log.NewNOPFactory().Logger(), on).NewConnection(context.Background(), send(t, header+tail), metadata, nil)
	select {
	case got := <-next.got:
		if got.metadata.Source.String() != "198.51.100.9:51000" {
			t.Fatalf("source = %s, want 198.51.100.9:51000", got.metadata.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the front's connection was refused")
	}
}

func TestTake(t *testing.T) {
	options := option.ListenOptions{ListenPort: 10443}
	if Take(&options).Enabled {
		t.Fatal("reported a request that was never made")
	}
	//nolint:staticcheck
	options.ProxyProtocol = true
	if got := Take(&options); got != (Settings{Enabled: true, Port: 10443}) {
		t.Fatalf("Take = %+v, want the request and its port", got)
	}
	//nolint:staticcheck
	if options.ProxyProtocol {
		t.Fatal("left the request set, which sing-box's listener refuses to start with")
	}
	// A row that arrived without a save can carry accept_no_header too.
	//nolint:staticcheck
	options.ProxyProtocol, options.ProxyProtocolAcceptNoHeader = true, true
	Take(&options)
	//nolint:staticcheck
	if options.ProxyProtocolAcceptNoHeader {
		t.Fatal("left accept_no_header set, which sing-box's listener refuses to start with")
	}
}

// Refreshes run one at a time: one that looked before another stored cannot
// store after it.
func TestRefreshInUseIsSerialized(t *testing.T) {
	t.Cleanup(func() { inUse.Store(nil) })
	markInUse(10443)
	looking := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		RefreshInUse(func() []uint16 {
			close(looking)
			<-release
			return nil // an old look: the box with 8443 had not started yet
		})
	}()
	<-looking
	second := make(chan struct{})
	go func() {
		defer close(second)
		RefreshInUse(func() []uint16 { return []uint16{8443} })
	}()
	select {
	case <-second:
		t.Fatal("a second refresh ran while the first was looking")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	<-second
	if ports := inUse.Load(); ports == nil || !ports.has(8443) {
		t.Fatal("the later refresh did not have the last word")
	}
}

// reached is what the inbound behind Handler saw: the metadata it was handed
// and the first bytes it could read.
type reached struct {
	metadata adapter.InboundContext
	data     string
}

type recordHandler struct {
	size int
	got  chan reached
}

func (h *recordHandler) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	buf := make([]byte, h.size)
	n, _ := io.ReadFull(conn, buf)
	h.got <- reached{metadata: metadata, data: string(buf[:n])}
}

func TestHandler(t *testing.T) {
	const header = "PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n"
	cases := []struct {
		name   string
		source string
		data   string
		// The source and bytes the inbound must see; refused when wantSource
		// is empty.
		wantSource string
		wantData   string
	}{
		{"local with header", "127.0.0.1:40000", header + tail, "203.0.113.7:51000", tail},
		{"local without header is refused", "127.0.0.1:40000", tail, "", ""},
		{"remote is left alone", "203.0.113.9:40000", tail, "203.0.113.9:40000", tail},
		// Not read as a header: it reaches the inbound as data, whose own
		// protocol then refuses it.
		{"remote header is not believed", "203.0.113.9:40000", header, "203.0.113.9:40000", header},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next := &recordHandler{size: len(c.wantData), got: make(chan reached, 1)}
			conn := send(t, c.data)
			metadata := adapter.InboundContext{Source: M.ParseSocksaddr(c.source), OriginDestination: local}
			closed := make(chan struct{})
			Handler(next, log.NewNOPFactory().Logger(), on).NewConnection(context.Background(), conn, metadata, func(error) { close(closed) })
			select {
			case got := <-next.got:
				if c.wantSource == "" {
					t.Fatalf("refused connection reached the inbound from %s", got.metadata.Source)
				}
				if got.metadata.Source.String() != c.wantSource {
					t.Fatalf("source = %s, want %s", got.metadata.Source, c.wantSource)
				}
				if got.data != c.wantData {
					t.Fatalf("inbound read %q, want %q", got.data, c.wantData)
				}
			default:
				if c.wantSource != "" {
					t.Fatal("connection never reached the inbound")
				}
				select {
				case <-closed:
				default:
					t.Fatal("refused connection was not closed")
				}
			}
		})
	}
}

func TestListen(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := Listen(raw, log.NewNOPFactory().Logger(), on)
	defer l.Close()

	// A local connection that never sends its header must not hold up the
	// one behind it.
	stalled, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n" + tail)); err != nil {
		t.Fatal(err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
		if got := conn.RemoteAddr().String(); got != "203.0.113.7:51000" {
			t.Fatalf("RemoteAddr = %s, want 203.0.113.7:51000", got)
		}
		// kTLS casts down to the socket this way, and closes the connection
		// when it cannot get there.
		if _, ok := N.CastReader[interface {
			io.Reader
			syscall.Conn
		}](conn); !ok {
			t.Fatal("the TCP socket is not reachable under the accepted conn")
		}
		if _, ok := N.UnwrapWriter(conn).(*net.TCPConn); !ok {
			t.Fatal("the TCP socket is not reachable for writes")
		}
		assertTail(t, conn)
	case <-time.After(2 * time.Second):
		t.Fatal("Accept stalled behind a connection that sent nothing")
	}

	l.Close()
	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept succeeded on a closed listener")
	}
}

// countingReader counts the reads a header took.
type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

// v1Remaining must never ask for more than is left of the line, whatever the
// field lengths, and must take far fewer reads than a byte at a time.
func TestReadV1NeverOverreads(t *testing.T) {
	headers := []string{
		"PROXY TCP4 1.1.1.1 1.1.1.1 1 1\r\n",
		"PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n",
		"PROXY TCP4 255.255.255.255 255.255.255.255 65535 65535\r\n",
		"PROXY TCP6 :: :: 1 1\r\n",
		"PROXY TCP6 2001:db8::7 ::1 51000 443\r\n",
		"PROXY TCP6 ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff 65535 65535\r\n",
		"PROXY UNKNOWN\r\n",
		"PROXY UNKNOWN ffff:f::f ffff:f::f 65535 65535\r\n",
	}
	for _, header := range headers {
		r := &countingReader{r: bytes.NewReader([]byte(header + tail))}
		raw, err := readRaw(r)
		if err != nil {
			t.Fatalf("%q: %v", header, err)
		}
		if string(raw) != header {
			t.Fatalf("read %q, want exactly %q", raw, header)
		}
		rest, _ := io.ReadAll(r)
		if string(rest) != tail {
			t.Fatalf("%q: bytes after it = %q, want %q", header, rest, tail)
		}
		if strings.HasPrefix(header, "PROXY TCP") && r.reads > len(header)/4 {
			t.Errorf("%q took %d reads", header, r.reads)
		}
	}
}

// warnLogger records what was logged at Warn and Error, and at Debug.
type warnLogger struct {
	logger.ContextLogger
	warns, errors, debugs []string
}

func (l *warnLogger) Warn(args ...any) { l.warns = append(l.warns, fmt.Sprint(args...)) }
func (l *warnLogger) ErrorContext(_ context.Context, args ...any) {
	l.errors = append(l.errors, fmt.Sprint(args...))
}
func (l *warnLogger) DebugContext(_ context.Context, args ...any) {
	l.debugs = append(l.debugs, fmt.Sprint(args...))
}

// An untrusted connection is never read for a header, so a PROXY header on
// one reaches the protocol as data; the operator is told why, since the
// protocol's own error would not say.
func TestUntrustedHeaderIsReported(t *testing.T) {
	for _, c := range []struct {
		name string
		data string
		warn bool
	}{
		{"v1 header", "PROXY TCP4 192.0.2.1 127.0.0.1 1 443\r\n", true},
		{"v2 header", "\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0c" + string(make([]byte, 12)), true},
		{"TLS", tail, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			resetWarnings()
			logs := &warnLogger{ContextLogger: log.NewNOPFactory().Logger()}
			next := &recordHandler{size: len(c.data), got: make(chan reached, 1)}
			metadata := adapter.InboundContext{Source: M.ParseSocksaddr("203.0.113.9:40000"), OriginDestination: local}
			Handler(next, logs, on).NewConnection(context.Background(), send(t, c.data), metadata, nil)
			got := <-next.got
			if got.data != c.data {
				t.Fatalf("inbound read %q, want it untouched", got.data)
			}
			if (len(logs.warns) > 0) != c.warn {
				t.Fatalf("warnings = %q, want one: %v", logs.warns, c.warn)
			}
		})
	}
}

func resetWarnings() {
	untrustedWarning.Lock()
	untrustedWarning.last, untrustedWarning.suppressed = time.Time{}, 0
	untrustedWarning.Unlock()
}

// The warning goes out once per interval, and the next one says how many it
// held back: anyone can send a PROXY header to a public inbound.
func TestUntrustedWarningIsRateLimited(t *testing.T) {
	var limiter warnLimiter
	logs := &warnLogger{ContextLogger: log.NewNOPFactory().Logger()}
	source := M.ParseSocksaddr("203.0.113.9:40000")
	start := time.Now()
	for i := 0; i < 5; i++ {
		limiter.warn(logs, source, start.Add(time.Duration(i)*time.Second))
	}
	if len(logs.warns) != 1 {
		t.Fatalf("%d warnings inside one interval, want 1", len(logs.warns))
	}
	limiter.warn(logs, source, start.Add(warnInterval+time.Second))
	if len(logs.warns) != 2 || !strings.Contains(logs.warns[1], "4 more") {
		t.Fatalf("warnings = %q, want a second one counting the 4 held back", logs.warns)
	}
}

// A trusted connection that closes without a byte is a port check or a
// front's TCP health check: refused, but not logged as an error.
func TestSilentLocalConnectionIsNotAnError(t *testing.T) {
	logs := &warnLogger{ContextLogger: log.NewNOPFactory().Logger()}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := raw.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client.Close()
	next := &recordHandler{got: make(chan reached, 1)}
	metadata := adapter.InboundContext{Source: M.ParseSocksaddr("127.0.0.1:40000"), OriginDestination: local}
	Handler(next, logs, on).NewConnection(context.Background(), server, metadata, nil)
	if len(logs.errors) != 0 || len(logs.debugs) != 1 {
		t.Fatalf("errors = %q, debugs = %q; want the refusal at debug only", logs.errors, logs.debugs)
	}
}

// Disabled, Handler and Listen are what they were given.
func TestDisabledIsUntouched(t *testing.T) {
	next := &recordHandler{}
	if Handler(next, log.NewNOPFactory().Logger(), Settings{}) != adapter.ConnectionHandler(next) {
		t.Fatal("a disabled Handler wrapped the inbound")
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if Listen(raw, log.NewNOPFactory().Logger(), Settings{}) != raw {
		t.Fatal("a disabled Listen wrapped the listener")
	}
}

// A transport that stops calling Accept without closing the listener must
// not keep every later connection open behind it.
func TestListenGivesUpOnAStalledTransport(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := listen(raw, log.NewNOPFactory().Logger(), 100*time.Millisecond)
	defer l.Close()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.Write([]byte("PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n"))
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	if _, err = client.Read(make([]byte, 1)); err == nil {
		t.Fatal("read data from a connection nobody accepted")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the connection was held for %s", elapsed)
	}
}

// through is the route of an outbound with a server of its own.
var through = Route{}

// A relay on this host -- a local socks server an outbound points at -- opens
// the onward connection itself, without a NAT. Asked for a PROXY-enabled
// inbound's port by anything that could land on this host, it is refused;
// asked for any other port, or for somewhere off this host, it is dialed.
func TestRelayOnThisHost(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "198.51.100.1", "fe80::1")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	cases := []struct {
		destination string
		refused     bool
	}{
		{"127.0.0.1:10443", true},
		{"198.51.100.1:10443", true},
		{"localtest.me:10443", true},
		{"0.0.0.0:10443", true},
		{"[::]:10443", true},
		{"[fe80::1%eth0]:10443", true},
		// Through the front's NAT the port says nothing.
		{"172.18.0.1:8443", true},
		{"127.0.0.1:8080", false},
		{"example.com:443", false},
		{"203.0.113.7:10443", false},
	}
	for _, c := range cases {
		t.Run(c.destination, func(t *testing.T) {
			err := guardDial(dialedConn("127.0.0.1:40050", "127.0.0.1:1080"), M.ParseSocksaddr(c.destination), through)
			if (err != nil) != c.refused {
				t.Fatalf("refused = %v (%v), want %v", err != nil, err, c.refused)
			}
		})
	}
}

// A relay at a named front's address -- a socks server on the Docker host,
// seen from the container at the gateway that is also the front -- is held to
// the same rule, and nothing else: the front's own connections from that
// address are never caught up in it, however much the relay is used.
func TestRelayAtANamedFront(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "172.17.0.2")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.17.0.1/32")})
	defer SetTrusted(nil)
	relay := func() net.Conn { return dialedConn("172.17.0.2:40080", "172.17.0.1:40000") }
	for _, destination := range []string{"example.com:443", "203.0.113.7:10443"} {
		err := guardDial(relay(), M.ParseSocksaddr(destination), through)
		if err != nil {
			t.Fatalf("a relay at the gateway was refused %s: %v", destination, err)
		}
	}
	for _, destination := range []string{"example.com:10443", "172.17.0.2:10443", "172.17.0.1:8443"} {
		if err := guardDial(relay(), M.ParseSocksaddr(destination), through); err == nil {
			t.Fatalf("a relay at the gateway was handed %s", destination)
		}
	}
	conn := send(t, "PROXY TCP4 203.0.113.7 172.17.0.2 51000 10443\r\n"+tail)
	client, err := Read(conn)
	if err != nil {
		t.Fatalf("the front's own connection was refused: %v", err)
	}
	if client.String() != "203.0.113.7:51000" {
		t.Fatalf("client = %s, want 203.0.113.7:51000", client)
	}
}

// What a client asks the direct outbound for, the core dials itself, and is
// judged by where the socket went: a name that lands on this host's PROXY
// port is refused, one on another port is not.
func TestDirectDialByName(t *testing.T) {
	inUseForTest(t, 10443)
	if err := guardDirect(dialedConn("127.0.0.1:40055", "127.0.0.1:10443"), M.ParseSocksaddr("localhost:10443")); err == nil {
		t.Fatal("a direct dial by name to this host's PROXY port was let through")
	}
	if err := guardDirect(dialedConn("127.0.0.1:40056", "127.0.0.1:8080"), M.ParseSocksaddr("localhost:8080")); err != nil {
		t.Fatalf("a direct dial by name to another port here was refused: %v", err)
	}
}

// A direct outbound whose socket went somewhere else than the address asked
// for is judged as a relay.
func TestDirectThatWentElsewhereIsARelay(t *testing.T) {
	inUseForTest(t, 10443)
	if err := guardDial(dialedConn("127.0.0.1:40057", "127.0.0.1:1080"), M.ParseSocksaddr("127.0.0.1:10443"), Route{Direct: true}); err == nil {
		t.Fatal("a dial to a relay on this host, routed as direct, was let through")
	}
}

// A direct dial to a named front is refused however the client named it.
func TestDirectDialToANamedFrontByName(t *testing.T) {
	inUseForTest(t)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	if err := guardDial(dialedConn("172.18.0.2:40058", "172.18.0.1:443"), M.ParseSocksaddr("gateway.example:443"), Route{Direct: true}); err == nil {
		t.Fatal("a direct dial by name to a named front was let through")
	}
}

// An outbound whose server is inside a named range but is no relay of ours --
// an upstream in the same VPC -- is dialed for anything off this host.
func TestOutboundServerInANamedRangeIsDialed(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "10.0.0.2")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	defer SetTrusted(nil)
	for _, destination := range []string{"203.0.113.7:443", "example.com:443"} {
		err := guardDial(dialedConn("10.0.0.2:40080", "10.0.0.7:443"), M.ParseSocksaddr(destination), through)
		if err != nil {
			t.Fatalf("an outbound to a server in a named range was refused %s: %v", destination, err)
		}
	}
}

// A named front on the link is trusted from its zoned address, as the socket
// reports it.
func TestTrustedLinkLocalFront(t *testing.T) {
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("fe80::1/128")})
	defer SetTrusted(nil)
	if !Trusted(M.ParseSocksaddr("[fe80::1%eth0]:40000"), M.ParseSocksaddr("[fe80::2%eth0]:10443")) {
		t.Fatal("a link-local named front was not trusted from its zoned address")
	}
}

// writeConn has no addresses, as a gRPC or HTTP/2 transport's conn has none,
// and records what is written to it.
type writeConn struct {
	addrConn
	writes int
}

func (c *writeConn) Write(p []byte) (int, error) {
	c.writes++
	return len(p), nil
}

// A conn without addresses that is not a slow dial is not written to -- an
// empty write on a gRPC conn sends an empty frame -- and is judged by the
// server its outbound is configured with.
func TestAddresslessConn(t *testing.T) {
	inUseForTest(t, 10443)
	cases := []struct {
		name    string
		server  string
		refused bool
	}{
		{"server off this host", "198.51.100.20:443", false},
		{"server by name", "cdn.example.com:443", false},
		{"server on this host", "127.0.0.1:8443", true},
		{"server at localhost", "localhost:8443", true},
		{"server at localhost, fully qualified", "localhost.:8443", true},
		{"server at LOCALHOST", "LOCALHOST:8443", true},
		{"server at a .localhost name", "relay.localhost:8443", true},
		{"no server known", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := &writeConn{addrConn: addrConn{local: M.Socksaddr{}, remote: M.Socksaddr{}}}
			var route Route
			if c.server != "" {
				route.Server = M.ParseSocksaddr(c.server)
			}
			err := guardDial(conn, M.ParseSocksaddr("example.com:10443"), route)
			if (err != nil) != c.refused {
				t.Fatalf("refused = %v (%v), want %v", err != nil, err, c.refused)
			}
			if conn.writes != 0 {
				t.Fatalf("written to %d times", conn.writes)
			}
		})
	}
}

// A header that names no client -- a v2 LOCAL health check, a v1 UNKNOWN --
// is consumed and refused: the front's own address is not a client's.
func TestReadHeaderNamingNoClient(t *testing.T) {
	for _, header := range []string{
		"PROXY UNKNOWN\r\n",
		v2(t, &proxyproto.Header{Version: 2, Command: proxyproto.LOCAL, TransportProtocol: proxyproto.UNSPEC}),
	} {
		conn := send(t, header+tail)
		if _, err := Read(conn); err != errNoClient {
			t.Fatalf("Read(%q) = %v, want errNoClient", header, err)
		}
		assertTail(t, conn)
	}
}

// A front's PROXY health check is closed without reaching the inbound, and
// logged at debug only: one every few seconds is not an error.
func TestHealthCheckIsQuiet(t *testing.T) {
	logs := &warnLogger{ContextLogger: log.NewNOPFactory().Logger()}
	next := &recordHandler{got: make(chan reached, 1)}
	metadata := adapter.InboundContext{Source: M.ParseSocksaddr("127.0.0.1:40000"), OriginDestination: local}
	check := v2(t, &proxyproto.Header{Version: 2, Command: proxyproto.LOCAL, TransportProtocol: proxyproto.UNSPEC})
	closed := make(chan struct{})
	Handler(next, logs, on).NewConnection(context.Background(), send(t, check), metadata, func(error) { close(closed) })
	select {
	case got := <-next.got:
		t.Fatalf("a health check reached the inbound from %s", got.metadata.Source)
	case <-closed:
	}
	if len(logs.errors) != 0 || len(logs.debugs) != 1 {
		t.Fatalf("errors = %q, debugs = %q; want it at debug only", logs.errors, logs.debugs)
	}
}

// An untrusted connection steps aside for writes from the start -- vision's
// splice and a half-close reach the socket -- and for reads once its first
// bytes are seen, however many reads they take.
func TestSniffStepsAside(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := raw.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	resetWarnings()
	logs := &warnLogger{ContextLogger: log.NewNOPFactory().Logger()}
	conn := sniff(server, logs, M.ParseSocksaddr("203.0.113.9:40000"))
	if _, ok := N.UnwrapWriter(conn).(*net.TCPConn); !ok {
		t.Fatal("the TCP socket is not reachable for writes")
	}
	if _, ok := N.UnwrapWriter(conn).(N.WriteCloser); !ok {
		t.Fatal("a half-close cannot reach the socket")
	}

	// The signature split over two reads.
	if _, err := client.Write([]byte("PR")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if conn.(*sniffConn).ReaderReplaceable() {
		t.Fatal("stepped aside before the signature could be told")
	}
	if _, err := client.Write([]byte("OXY TCP4")); err != nil {
		t.Fatal(err)
	}
	buf = make([]byte, 8)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if !conn.(*sniffConn).ReaderReplaceable() {
		t.Fatal("still in the way after the signature was seen")
	}
	if len(logs.warns) != 1 {
		t.Fatalf("warnings = %q, want one for a signature split across reads", logs.warns)
	}
}

// chanListener hands out the conns sent to it.
type chanListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *chanListener) Addr() net.Addr { return M.ParseSocksaddr("198.51.100.1:443").TCPAddr() }

// closeConn records being closed.
type closeConn struct {
	addrConn
	closed chan struct{}
}

func (c *closeConn) Close() error {
	close(c.closed)
	return nil
}

// An untrusted connection goes from the accept loop straight to Accept, with
// nothing read; and closing the listener while it waits there closes it.
func TestListenHandsUntrustedConnsOver(t *testing.T) {
	inner := &chanListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	l := Listen(inner, log.NewNOPFactory().Logger(), on)
	remote := M.ParseSocksaddr("203.0.113.9:40000").TCPAddr()
	at := M.ParseSocksaddr("198.51.100.1:443").TCPAddr()

	first := &closeConn{addrConn: addrConn{local: at, remote: remote}, closed: make(chan struct{})}
	inner.conns <- first
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if got := conn.RemoteAddr().String(); got != remote.String() {
		t.Fatalf("RemoteAddr = %s, want %s", got, remote)
	}

	// Nobody accepts this one: Close must still get it closed.
	second := &closeConn{addrConn: addrConn{local: at, remote: remote}, closed: make(chan struct{})}
	inner.conns <- second
	l.Close()
	select {
	case <-second.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("a connection waiting for Accept outlived the listener")
	}
	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept succeeded on a closed listener")
	}
}

// remoteHandler records the RemoteAddr of the conn it is handed, and whether
// the TCP socket is still reachable under it.
type remoteHandler struct {
	got chan net.Conn
}

func (h *remoteHandler) NewConnection(_ context.Context, conn net.Conn, _ adapter.InboundContext, _ N.CloseHandlerFunc) {
	h.got <- conn
}

// On the inbound's own listener the conn names the client as its remote
// address, as on a transport -- REALITY and TLS read it there -- and the
// socket is still reachable under it for kTLS and splice.
func TestHandlerConnNamesTheClient(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := raw.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err = client.Write([]byte("PROXY TCP4 203.0.113.7 127.0.0.1 51000 443\r\n" + tail)); err != nil {
		t.Fatal(err)
	}
	next := &remoteHandler{got: make(chan net.Conn, 1)}
	metadata := adapter.InboundContext{
		Source:            M.SocksaddrFromNet(server.RemoteAddr()),
		OriginDestination: M.SocksaddrFromNet(server.LocalAddr()),
	}
	Handler(next, log.NewNOPFactory().Logger(), on).NewConnection(context.Background(), server, metadata, nil)
	conn := <-next.got
	if got := conn.RemoteAddr().String(); got != "203.0.113.7:51000" {
		t.Fatalf("RemoteAddr = %s, want 203.0.113.7:51000", got)
	}
	if _, ok := N.UnwrapReader(conn).(*net.TCPConn); !ok {
		t.Fatal("the TCP socket is not reachable for reads")
	}
	if _, ok := N.UnwrapWriter(conn).(*net.TCPConn); !ok {
		t.Fatal("the TCP socket is not reachable for writes")
	}
	assertTail(t, conn)
}

// GuardDial asks for the route only once it has read the guard as on, and the
// route it gets is what the dial is judged by.
func TestGuardDialAsksForTheRouteWhenOn(t *testing.T) {
	inUse.Store(nil)
	asked := false
	if err := GuardDial(dialedConn("127.0.0.1:40090", "127.0.0.1:10443"), M.ParseSocksaddr("localhost:10443"), func() Route {
		asked = true
		return Route{}
	}); err != nil || asked {
		t.Fatalf("with the guard off: err = %v, route asked = %v", err, asked)
	}
	inUseForTest(t, 10443)
	// Judged by the route it was handed: direct, to another port here.
	err := GuardDial(dialedConn("127.0.0.1:40091", "127.0.0.1:8080"), M.ParseSocksaddr("localhost:8080"), func() Route {
		asked = true
		return Route{Direct: true}
	})
	if err != nil || !asked {
		t.Fatalf("with the guard on: err = %v, route asked = %v; want a direct dial let through", err, asked)
	}
}

// A named range that holds this host's own address -- a VPC listed whole --
// does not make that address a front to a relay: this host is reached
// without a NAT, so only its PROXY ports are refused.
func TestRelayToThisHostInsideANamedRange(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "10.0.1.5")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/16")})
	defer SetTrusted(nil)
	relay := func() net.Conn { return dialedConn("127.0.0.1:40100", "127.0.0.1:40000") }
	if err := guardDial(relay(), M.ParseSocksaddr("10.0.1.5:80"), through); err != nil {
		t.Fatalf("a relay was refused this host's own site: %v", err)
	}
	if err := guardDial(relay(), M.ParseSocksaddr("10.0.1.5:10443"), through); err == nil {
		t.Fatal("a relay was handed this host's PROXY port")
	}
	if err := guardDial(relay(), M.ParseSocksaddr("10.0.2.9:80"), through); err == nil {
		t.Fatal("a relay was handed another address in the named range")
	}
}

// A front may leave from any of this host's addresses, not only the one the
// connection arrived on -- but only from one known to be this host's.
func TestTrustedFromAnotherOfThisHostsAddresses(t *testing.T) {
	hostAddrsForTest(t, "198.51.100.1", "198.51.100.2")
	if !Trusted(M.ParseSocksaddr("198.51.100.2:40000"), M.ParseSocksaddr("198.51.100.1:10443")) {
		t.Fatal("a front leaving from another of this host's addresses was not trusted")
	}
	if Trusted(M.ParseSocksaddr("203.0.113.9:40000"), M.ParseSocksaddr("198.51.100.1:10443")) {
		t.Fatal("a stranger was trusted")
	}
	// Unreadable, the addresses vouch for no one.
	hostAddrs.snapshot.Store(&addrSnapshot{unknown: true, loaded: time.Now().Add(time.Hour)})
	if Trusted(M.ParseSocksaddr("198.51.100.2:40000"), M.ParseSocksaddr("198.51.100.1:10443")) {
		t.Fatal("trusted an address when this host's could not be read")
	}
}

// A direct dial to an address that cannot be this host or a named front is
// decided without opening a TCP Fast Open dial: it keeps its round trip.
func TestDirectDialOffHostKeepsFastOpen(t *testing.T) {
	inUseForTest(t, 10443)
	hostAddrsForTest(t, "198.51.100.1")
	away := &slowConn{local: M.ParseSocksaddr("198.51.100.1:40110").TCPAddr(), remote: M.ParseSocksaddr("203.0.113.7:443").TCPAddr()}
	if err := guardDirect(away, M.ParseSocksaddr("203.0.113.7:443")); err != nil {
		t.Fatal(err)
	}
	if away.open {
		t.Fatal("opened a fast-open dial to an address off this host")
	}
	// By name, where it goes is only known once it is open.
	named := &slowConn{local: M.ParseSocksaddr("198.51.100.1:40111").TCPAddr(), remote: M.ParseSocksaddr("203.0.113.7:443").TCPAddr()}
	if err := guardDirect(named, M.ParseSocksaddr("example.com:443")); err != nil {
		t.Fatal(err)
	}
	if !named.open {
		t.Fatal("judged a fast-open dial by name without opening it")
	}
}

// fakeTransport is a V2Ray transport as far as Over looks.
type fakeTransport []string

func (t fakeTransport) Network() []string { return t }

// Over a transport that carries no TCP, nothing reads a header.
func TestSettingsOverTransport(t *testing.T) {
	on := Settings{Enabled: true, Port: 443}
	if !on.Over(nil).Enabled || !on.Over(fakeTransport{N.NetworkTCP}).Enabled {
		t.Fatal("switched off over TCP")
	}
	if on.Over(fakeTransport{N.NetworkUDP}).Enabled {
		t.Fatal("left on over a transport that carries no TCP")
	}
}

// A fallback the operator configured may go to a named front -- the Docker
// host's site, at the gateway -- but not onto a PROXY-enabled inbound's port
// here, where the client's own bytes would be read for a header.
func TestFallbackDial(t *testing.T) {
	inUseForTest(t, 10443)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	fallback := Route{Direct: true, Fallback: true}
	if err := guardDial(dialedConn("172.18.0.2:40120", "172.18.0.1:80"), M.ParseSocksaddr("172.18.0.1:80"), fallback); err != nil {
		t.Fatalf("a fallback to a named front was refused: %v", err)
	}
	if err := guardDial(dialedConn("127.0.0.1:40121", "127.0.0.1:10443"), M.ParseSocksaddr("127.0.0.1:10443"), fallback); err == nil {
		t.Fatal("a fallback onto this host's PROXY port was let through")
	}
	if err := guardDirect(dialedConn("172.18.0.2:40122", "172.18.0.1:80"), M.ParseSocksaddr("172.18.0.1:80")); err == nil {
		t.Fatal("a client's own dial to a named front was let through")
	}
}
