package proxyheader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
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
		{"v1 unknown keeps the source", "PROXY UNKNOWN\r\n", local.String()},
		{"v2 tcp4", v2(t, proxyproto.HeaderProxyFromAddrs(2, v4, front)), "203.0.113.7:51000"},
		{"v2 tcp6", v2(t, proxyproto.HeaderProxyFromAddrs(2, v6, front6)), "[2001:db8::7]:51000"},
		{"v2 local keeps the source", v2(t, &proxyproto.Header{Version: 2, Command: proxyproto.LOCAL, TransportProtocol: proxyproto.UNSPEC}), local.String()},
		{"v4-mapped source is unwrapped", "PROXY TCP6 ::ffff:203.0.113.7 ::1 51000 443\r\n", "203.0.113.7:51000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := send(t, c.header+tail)
			got, err := Read(conn, local)
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
			if got, err := Read(server, local); err == nil {
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
	if _, err := readWithin(server, local, 50*time.Millisecond); err == nil {
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
func inUseForTest(t *testing.T) {
	inUse.Store(true)
	t.Cleanup(func() { inUse.Store(false) })
}

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

func TestGuardDial(t *testing.T) {
	inUseForTest(t)
	hostAddrsForTest(t, "198.51.100.1", "203.0.113.5")
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	cases := []struct {
		name    string
		local   string
		remote  string
		refused bool
		marked  bool
	}{
		{"loopback is remembered", "127.0.0.1:40001", "127.0.0.1:10443", false, true},
		{"v4-mapped loopback is remembered", "[::ffff:127.0.0.1]:40002", "[::ffff:127.0.0.1]:10443", false, true},
		{"this host's own address is remembered", "198.51.100.1:40003", "198.51.100.1:10443", false, true},
		{"anywhere else is left alone", "198.51.100.1:40004", "203.0.113.7:443", false, false},
		{"a named front is refused", "172.18.0.2:40005", "172.18.0.1:443", true, false},
		// A direct outbound bound to an address inside a named range: the
		// inbound trusts that source, so a dial to another of this host's
		// addresses must be remembered too ...
		{"a source inside a named range is remembered", "172.18.0.1:40006", "203.0.113.5:10443", false, true},
		// ... and one leaving the host from there need not be.
		{"a source inside a named range dialing out is left alone", "172.18.0.1:40007", "203.0.113.9:443", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			release, err := guardDial(dialedConn(c.local, c.remote), 0)
			if (err != nil) != c.refused {
				t.Fatalf("refused = %v, want %v", err != nil, c.refused)
			}
			// What the inbound sees is the dial's two ends, unmapped.
			source := M.ParseSocksaddr(c.local).Unwrap()
			arrived := M.ParseSocksaddr(c.remote).Unwrap()
			if got := dialedByCore(source, arrived); got != c.marked {
				t.Fatalf("dialedByCore(%s, %s) = %v, want %v", source, arrived, got, c.marked)
			}
			release()
			release() // a second release must not take another dial's count
			if dialedByCore(source, arrived) {
				t.Fatal("still remembered after release")
			}
		})
	}
}

// With no inbound reading PROXY headers, the guard costs clients nothing: a
// named front is dialed like anywhere else, and nothing is remembered.
func TestGuardDialIdleWithoutProxyInbounds(t *testing.T) {
	inUse.Store(false)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)
	if _, err := guardDial(dialedConn("172.18.0.2:40030", "172.18.0.1:443"), 0); err != nil {
		t.Fatalf("a named front was refused with no PROXY inbound: %v", err)
	}
	release, _ := guardDial(dialedConn("127.0.0.1:40031", "127.0.0.1:10443"), 0)
	defer release()
	if dialedByCore(M.ParseSocksaddr("127.0.0.1:40031"), M.ParseSocksaddr("127.0.0.1:10443")) {
		t.Fatal("a loopback dial was remembered with no PROXY inbound")
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
	t.Cleanup(func() { inUse.Store(false) })
	inUse.Store(true)
	RefreshInUse(func() (bool, bool) { return false, true })
	if inUse.Load() {
		t.Fatal("still in use with no inbound reading headers")
	}
	RefreshInUse(func() (bool, bool) { return true, true })
	if !inUse.Load() {
		t.Fatal("not in use with an inbound reading headers")
	}
	RefreshInUse(func() (bool, bool) { return false, false })
	if !inUse.Load() {
		t.Fatal("switched off by a look that could not be taken")
	}
	RefreshInUse(func() (bool, bool) {
		Handler(&recordHandler{}, log.NewNOPFactory().Logger(), true)
		return false, true
	})
	if !inUse.Load() {
		t.Fatal("switched off under an inbound built while the core was looked at")
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
	inUseForTest(t)
	SetTrusted([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})
	defer SetTrusted(nil)

	loop := &slowConn{local: M.ParseSocksaddr("127.0.0.1:40040").TCPAddr(), remote: M.ParseSocksaddr("127.0.0.1:10443").TCPAddr()}
	release, err := guardDial(loop, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if !loop.open || len(loop.written) != 0 {
		t.Fatalf("open = %v, wrote %q; want it opened with nothing sent", loop.open, loop.written)
	}
	if !dialedByCore(M.ParseSocksaddr("127.0.0.1:40040"), M.ParseSocksaddr("127.0.0.1:10443")) {
		t.Fatal("a slow dial to this host was not remembered")
	}

	front := &slowConn{local: M.ParseSocksaddr("172.18.0.2:40041").TCPAddr(), remote: M.ParseSocksaddr("172.18.0.1:443").TCPAddr()}
	if _, err := guardDial(front, 0); err == nil {
		t.Fatal("a slow dial to a named front was let through")
	}

	// Over a proxy outbound the slow dial sits under that protocol's conn,
	// which must not be written to: an empty write there could send its
	// request header early.
	inner := &slowConn{local: M.ParseSocksaddr("127.0.0.1:40042").TCPAddr(), remote: M.ParseSocksaddr("127.0.0.1:10443").TCPAddr()}
	outer := &protocolConn{slowConn: inner}
	release, err = guardDial(outer, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if outer.wrote || !inner.open {
		t.Fatalf("protocol conn written = %v, inner opened = %v; want only the inner one opened", outer.wrote, inner.open)
	}
	if !dialedByCore(M.ParseSocksaddr("127.0.0.1:40042"), M.ParseSocksaddr("127.0.0.1:10443")) {
		t.Fatal("a slow dial under a protocol conn was not remembered")
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

// A four-tuple can come back while the record of its last use still lingers:
// releasing one must not forget the other.
func TestGuardDialCountsARepeatedConnection(t *testing.T) {
	inUseForTest(t)
	first, _ := guardDial(dialedConn("127.0.0.1:40010", "127.0.0.1:10443"), 0)
	second, _ := guardDial(dialedConn("127.0.0.1:40010", "127.0.0.1:10443"), 0)
	source, arrived := M.ParseSocksaddr("127.0.0.1:40010"), M.ParseSocksaddr("127.0.0.1:10443")
	first()
	if !dialedByCore(source, arrived) {
		t.Fatal("releasing one use forgot the other")
	}
	second()
	if dialedByCore(source, arrived) {
		t.Fatal("still remembered after both were released")
	}
}

// Linux gives connections to different destinations the same ephemeral port,
// so a front can reach the inbound from the very address and port of a core
// dial to some other service here. That front is not the core.
func TestGuardDialTellsAFrontOnASharedPort(t *testing.T) {
	inUseForTest(t)
	release, _ := guardDial(dialedConn("198.51.100.1:40011", "198.51.100.1:443"), 0)
	defer release()
	if dialedByCore(M.ParseSocksaddr("198.51.100.1:40011"), M.ParseSocksaddr("198.51.100.1:10443")) {
		t.Fatal("a front sharing a core dial's local port was taken for the core")
	}
}

// A client that sends its forged header and resets at once gets the core to
// close both ends while the inbound may still be reading that header: the
// dial has to stay remembered past its release, until the read is over.
func TestGuardDialOutlivesTheConnection(t *testing.T) {
	inUseForTest(t)
	source, arrived := M.ParseSocksaddr("127.0.0.1:40050"), M.ParseSocksaddr("127.0.0.1:10443")
	// Long enough that no scheduling delay can see it expire.
	release, err := guardDial(dialedConn(source.String(), arrived.String()), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if !dialedByCore(source, arrived) {
		t.Fatal("forgotten the moment the connection closed")
	}
	t.Cleanup(func() {
		dialed.Lock()
		delete(dialed.conns, fourTuple{client: endpoint(source), server: endpoint(arrived)})
		dialed.Unlock()
	})

	// And it does go: a short linger, waited out.
	source = M.ParseSocksaddr("127.0.0.1:40051")
	release, err = guardDial(dialedConn(source.String(), arrived.String()), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for dialedByCore(source, arrived) {
		if time.Now().After(deadline) {
			t.Fatal("never forgotten")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A header arriving on a connection the core dialed itself is a client
// looping back through the direct outbound, not a front, and is refused --
// on both listeners.
func TestCoresOwnDialIsRefused(t *testing.T) {
	inUseForTest(t)
	const header = "PROXY TCP4 198.51.100.9 127.0.0.1 51000 443\r\n"
	// The inbound's own end is local: it is where the core dialed.
	release, err := guardDial(dialedConn("127.0.0.1:40020", local.String()), 0)
	if err != nil {
		t.Fatal(err)
	}

	next := &recordHandler{size: len(tail), got: make(chan reached, 1)}
	metadata := adapter.InboundContext{Source: M.ParseSocksaddr("127.0.0.1:40020"), OriginDestination: local}
	closed := make(chan struct{})
	Handler(next, log.NewNOPFactory().Logger(), true).NewConnection(context.Background(), send(t, header+tail), metadata, func(error) { close(closed) })
	select {
	case got := <-next.got:
		t.Fatalf("the core's own dial reached the inbound as %s", got.metadata.Source)
	case <-closed:
	}

	// Once that connection is gone, the same endpoint is a front again.
	release()
	Handler(next, log.NewNOPFactory().Logger(), true).NewConnection(context.Background(), send(t, header+tail), metadata, nil)
	select {
	case got := <-next.got:
		if got.metadata.Source.String() != "198.51.100.9:51000" {
			t.Fatalf("source = %s, want 198.51.100.9:51000", got.metadata.Source)
		}
	default:
		t.Fatal("a front's connection was refused")
	}
}

func TestTake(t *testing.T) {
	var options option.ListenOptions
	if Take(&options) {
		t.Fatal("reported a request that was never made")
	}
	//nolint:staticcheck
	options.ProxyProtocol = true
	if !Take(&options) {
		t.Fatal("lost the request")
	}
	//nolint:staticcheck
	if options.ProxyProtocol {
		t.Fatal("left the request set, which sing-box's listener refuses to start with")
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
			Handler(next, log.NewNOPFactory().Logger(), true).NewConnection(context.Background(), conn, metadata, func(error) { close(closed) })
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
	l := Listen(raw, log.NewNOPFactory().Logger(), true)
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
			Handler(next, logs, true).NewConnection(context.Background(), send(t, c.data), metadata, nil)
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
	Handler(next, logs, true).NewConnection(context.Background(), server, metadata, nil)
	if len(logs.errors) != 0 || len(logs.debugs) != 1 {
		t.Fatalf("errors = %q, debugs = %q; want the refusal at debug only", logs.errors, logs.debugs)
	}
}

// Disabled, Handler and Listen are what they were given.
func TestDisabledIsUntouched(t *testing.T) {
	next := &recordHandler{}
	if Handler(next, log.NewNOPFactory().Logger(), false) != adapter.ConnectionHandler(next) {
		t.Fatal("a disabled Handler wrapped the inbound")
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if Listen(raw, log.NewNOPFactory().Logger(), false) != raw {
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
