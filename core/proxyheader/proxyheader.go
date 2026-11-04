// Package proxyheader reads the PROXY protocol header that a front on this
// host -- an nginx stream block routing port 443 by SNI, typically -- puts in
// front of each connection it forwards, so that the inbound behind it sees the
// client's address instead of the front's own (#260).
//
// sing-box took PROXY protocol out of its listeners in 1.6.0, and 1.14.4 still
// refuses an inbound that asks for it. That address is not cosmetic here: the
// online list and the per-client IP limit both run on it, and behind a front
// every client is 127.0.0.1. The copies under core/protocol/ reach this through
// their users.go, so each copy carries only the calls.
//
// Only a trusted connection is read for a header -- one from this host
// (loopback, or the address it arrived on), or from a front the operator named
// (SetTrusted: a Docker bridge's gateway, an nginx on another machine) -- and
// such a connection must carry one. Anything else passes through untouched: a
// header honoured from anywhere else would let any client name its own
// address, and the IP limit would believe it. A trusted connection without a
// header is refused rather than let through as the front, because a front that
// forgot to send one would otherwise go unnoticed while every client collapsed
// into a single address.
//
// Trusting this host has one hole of its own: the core is on this host too.
// A proxy user can ask the direct outbound for 127.0.0.1 and the PROXY port,
// and then send whatever header they like down a connection that arrives from
// loopback. So the core reports every connection it dials for a client
// (GuardDial, from the connection tracker), and one that landed on this host
// on a PROXY-enabled inbound's port is refused before a byte of the client's
// goes down it: no client has a reason to ask its proxy for the panel's own
// front-facing port. It is decided at the dial rather than recognised at the
// inbound, by a record of the dial's two ends, because such a record can be
// made to match the front's own connections: a client dialing in a loop
// cycles the ephemeral ports to that address, which the front's connections
// draw from too. A direct dial to a named front is refused outright: past a
// NAT -- a Docker port mapping -- it arrives from the front's address, on a
// port that says nothing about where it was headed.
//
// The same goes for a relay the core hands a client to: an outbound to a
// socks or shadowsocks server on this host (a local gost, a WARP client), or
// at a named front's address (one on the Docker host, seen from the
// container at the bridge's gateway). That relay opens the onward
// connection, so the core never sees its two ends, and it arrives from a
// trusted address. Which outbound is direct is the router's to say (Route): a
// name a client asks the direct outbound for is dialed by the core itself,
// and judged by where its socket went. A client asking a relay in either place for a
// PROXY-enabled inbound's port, by a name or by an address that lands on this
// host, is refused -- only that client, and only on those ports, so a real
// front at the same address is never caught up in it. A PROXY-enabled
// inbound on a port clients do ask for (443) takes that port away from them
// through such a relay. A relay asked for a named front's address is refused
// outright, as a direct dial there is: through a NAT the port says nothing.
// A tunnel outbound (WireGuard, Tailscale) is judged by the same rule, from
// the addresses its in-tunnel conn reports: asked for loopback on a PROXY
// port it is refused even where that means the peer's own loopback, because
// asked for this host's own tunnel address it really does come back in, from
// an address this host trusts.
//
// Some paths stay outside this. A relay that reaches this inbound through a
// port mapping that changes the port -- a Docker host's relay asked for the
// published 8443 of a container's 443 -- asks for a port the check does not
// know; a mapping that keeps the port (443:443) is caught. So does a direct
// dial, from a container, to the host's public address on a port Docker
// publishes on every interface: the hairpin NAT delivers it from the bridge's
// gateway, a trusted front, and the container cannot see the host's public
// address to tell. Publish a PROXY-enabled inbound's port on 127.0.0.1 only,
// which is what the form says.
// A NAT on this host itself -- an iptables REDIRECT or DNAT in OUTPUT that
// turns a dial to one port into a connection on another -- does the same to
// the core's own dials: the inbound no longer sees the two ends the core
// dialed, so the port check cannot match. A
// PROXY-enabled inbound must not be the target of such a rule. And dials the
// core makes for an inbound rather than for a client -- a REALITY or
// ShadowTLS handshake server -- are the operator's to point, and never at a
// PROXY-enabled inbound.
//
// On a transport that speaks HTTP (ws, http, httpupgrade, grpc lite) the
// header names the client to the transport, as its RemoteAddr; sing-box's
// transport server then prefers an X-Forwarded-For the request carries, as it
// does with no PROXY protocol at all. Behind a CDN that is the address worth
// having; without one it is the client's to choose, on these transports with
// or without a front. The raw TCP listener has no such header to defer to.
//
// A header is read to its exact length and never past it. What follows it is
// the client's TLS ClientHello, and leaving the connection itself in place
// rather than wrapping it in a buffer is what keeps kTLS and vision's splice
// reaching the TCP socket underneath.
package proxyheader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	panelLog "github.com/shenaba/2s-ui/logger"

	"github.com/pires/go-proxyproto"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	// readTimeout bounds how long a trusted connection may take to deliver
	// its header. A front sends it before anything else, so this only ever
	// runs out on a connection that was never going to send one.
	readTimeout = 5 * time.Second
	// handoffTimeout bounds how long a connection whose header is read waits
	// for Accept. A transport that stopped serving without closing its
	// listener would otherwise leave one goroutine and one socket behind for
	// every connection that arrived after it.
	handoffTimeout = 30 * time.Second
	// warnInterval spaces out the warning about untrusted headers: anyone who
	// can reach a public PROXY-enabled inbound can send one per connection.
	warnInterval = time.Minute

	// v1MinLength is "PROXY UNKNOWN\r\n", the shortest v1 header there is, and
	// one short of v2's fixed part: reading this much up front is safe for
	// both and saves a read per byte on the part every header has.
	v1MinLength = 15
	// v1MaxLength is the spec's own worst case.
	v1MaxLength = 107
	// v2FixedLength is the signature, version/command, family and length.
	v2FixedLength = 16
	// v2MaxPayload caps what a v2 header may claim to carry. Addresses take at
	// most 216 bytes (unix); the rest is TLVs, which nginx does not send.
	v2MaxPayload = 4096
)

// Settings is what an inbound asked of PROXY protocol: whether to read
// headers, and the port it listens on, which is what a client handed to a
// relay on this host could reach it by.
type Settings struct {
	Enabled bool
	Port    uint16
}

// Take reports what options ask of PROXY protocol, and clears the request:
// sing-box's own listener refuses to start while either field is set. The
// copies call it before building their listener, and keep the answer
// themselves. accept_no_header is cleared without effect: a save drops it,
// but a row can arrive without one (an imported database), and a connection
// from this host must carry a header regardless -- the stricter reading, and
// one that leaves the core starting rather than failing on sing-box's
// "removed in 1.6.0".
func Take(options *option.ListenOptions) Settings {
	//nolint:staticcheck
	enabled := options.ProxyProtocol
	//nolint:staticcheck
	options.ProxyProtocol = false
	//nolint:staticcheck
	options.ProxyProtocolAcceptNoHeader = false
	return Settings{Enabled: enabled, Port: options.ListenPort}
}

// trusted holds the fronts the operator named beyond this host. The service
// layer keeps it in step with the proxyProtoTrusted setting; it is read per
// connection, so it lives here rather than behind a database read.
var trusted atomic.Pointer[[]netip.Prefix]

// SetTrusted replaces the fronts trusted beyond this host.
func SetTrusted(prefixes []netip.Prefix) {
	prefixes = slices.Clone(prefixes)
	trusted.Store(&prefixes)
}

// inUse is the ports the inbounds that read PROXY headers listen on: nil
// when there are none. GuardDial costs something only then -- it refuses
// dials to the named fronts, and gives up TCP Fast Open's saved round trip --
// so a panel that never turned the switch on is untouched. Port 0 (an inbound
// left to pick its own) stands for every port.
//
// Building an enabled Handler or Listen adds its port (markInUse), before the
// inbound can accept anything; only RefreshInUse takes one away, from what the
// running cores hold. The core calls that itself, whenever its inbounds
// change and whenever a box starts or closes, so every path that rebuilds or
// drops one is covered -- not only the saves that happen to call it. Not from
// the stored inbounds: the core takes a new inbound before the save that
// stores it commits, so a scan of the rows by a save committing in between
// would switch the guard off under a live one.
var (
	inUse   atomic.Pointer[portSet]
	inUseMu sync.Mutex
	// built counts the enabled Handlers and Listens ever built, so that
	// RefreshInUse can tell one appeared while it was looking.
	built uint64
	// refreshMu serializes RefreshInUse. Held while the cores are looked at,
	// which takes their inbound tables' locks: it is never taken under one.
	refreshMu sync.Mutex
)

// portSet is the PROXY-enabled inbounds' ports. Never modified once stored.
type portSet map[uint16]struct{}

// has reports whether a connection to port could reach one of them.
func (s portSet) has(port uint16) bool {
	if _, any := s[0]; any {
		return true
	}
	_, found := s[port]
	return found
}

// InUse reports whether the dial guard is on.
func InUse() bool {
	return inUse.Load() != nil
}

func markInUse(port uint16) {
	inUseMu.Lock()
	defer inUseMu.Unlock()
	built++
	ports := portSet{port: {}}
	if current := inUse.Load(); current != nil {
		for p := range *current {
			ports[p] = struct{}{}
		}
	}
	inUse.Store(&ports)
}

// RefreshInUse recomputes the ports of the inbounds that read PROXY headers,
// from what running reports the cores run. It must wait out an inbound being
// added: one is built, and can be accepting, before the core lists it. The
// ports of a Handler or Listen built while running looks are kept -- it may
// be in a core running did not see.
func RefreshInUse(running func() []uint16) {
	// One at a time, from the look to the store: two running at once could
	// otherwise finish in the wrong order, and an older look -- taken before a
	// box started -- would switch the guard off under that box's inbounds.
	refreshMu.Lock()
	defer refreshMu.Unlock()
	for attempt := 1; ; attempt++ {
		inUseMu.Lock()
		since := built
		idle := since == 0 && inUse.Load() == nil
		inUseMu.Unlock()
		if idle {
			// No PROXY-enabled inbound was ever built: nothing to look for,
			// and a panel that never turned the switch on is spared the scan.
			return
		}
		found := running()
		inUseMu.Lock()
		// One built while looking may be in a core the look missed. Look
		// again rather than keep every port: those could be gone, and
		// nothing else would come to clear them. A few tries, then keep
		// them -- on, not off, is the side to err on.
		if built != since && attempt < 3 {
			inUseMu.Unlock()
			continue
		}
		ports := make(portSet, len(found))
		for _, port := range found {
			ports[port] = struct{}{}
		}
		if current := inUse.Load(); built != since && current != nil {
			for port := range *current {
				ports[port] = struct{}{}
			}
		}
		if len(ports) == 0 {
			inUse.Store(nil)
		} else {
			inUse.Store(&ports)
		}
		inUseMu.Unlock()
		return
	}
}

func namedFront(addr netip.Addr) bool {
	prefixes := trusted.Load()
	if prefixes == nil {
		return false
	}
	for _, prefix := range *prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func onThisHost(addr, local netip.Addr) bool {
	return addr.IsLoopback() || addr == local
}

// Trusted reports whether a connection from source that arrived on local may
// speak for a client: it comes from this host, or from a front the operator
// named.
func Trusted(source, local M.Socksaddr) bool {
	addr := plain(source.Addr)
	if !addr.IsValid() {
		return false
	}
	return onThisHost(addr, plain(local.Addr)) || namedFront(addr)
}

func endpoint(addr M.Socksaddr) netip.AddrPort {
	return netip.AddrPortFrom(addr.Addr.Unmap(), addr.Port)
}

func addrsOf(conn net.Conn) (remote, local M.Socksaddr) {
	if conn.RemoteAddr() != nil {
		remote = M.SocksaddrFromNet(conn.RemoteAddr()).Unwrap()
	}
	if conn.LocalAddr() != nil {
		local = M.SocksaddrFromNet(conn.LocalAddr()).Unwrap()
	}
	return remote, local
}

// Route is what the core knows, at routing time, about how the outbound a
// client was handed reaches its destination.
type Route struct {
	// Direct is a direct outbound without a detour: the socket the core
	// opens goes where the client asked.
	Direct bool
	// Server is where the outbound's own server is configured, when it has
	// one: the only clue to where a transport that keeps its socket out of
	// sight (gRPC, HTTP/2) goes.
	Server M.Socksaddr
}

// GuardDial is told about each connection the core has just dialed for a
// client, to destination -- the address the client asked for -- along the
// route the router chose, before a byte of the client's goes down it. While
// PROXY protocol is in use, it refuses a dial that landed on this host on a
// PROXY-enabled inbound's port, a direct dial to a front the operator named,
// and a dial to a relay that could carry the client back into a
// PROXY-enabled inbound (see the package comment).
//
// route is asked only once the guard is known to be on, from the same reading
// of it the dial is judged by: a guard that switched on just before would
// otherwise judge a direct dial with no route, as a relay.
func GuardDial(conn net.Conn, destination M.Socksaddr, route func() Route) error {
	ports := inUse.Load()
	if ports == nil {
		return nil
	}
	var r Route
	if route != nil {
		r = route()
	}
	return guardPorts(conn, destination, r, *ports)
}

// earlyConn is a dial that has not opened its socket yet: sing-box's TCP Fast
// Open dial, which says so through NeedHandshake.
type earlyConn interface {
	NeedHandshake() bool
}

func guardPorts(conn net.Conn, destination M.Socksaddr, route Route, ports portSet) error {
	destination = destination.Unwrap()
	// The socket underneath, not the outermost conn: a proxy protocol's conn
	// can report the address it was asked for as its remote one.
	socket := innermost(conn)
	remote, local := addrsOf(socket)
	if !remote.Addr.IsValid() || !local.Addr.IsValid() {
		// A TCP Fast Open dial has no socket yet: it opens one on its first
		// write, which carries the client's first bytes -- a forged header
		// among them -- so by the time the addresses exist it is too late to
		// look at them. A write with no data makes it connect now, the plain
		// way (tfo-go dials without TFO when there is nothing to send), at the
		// cost of the round trip TFO saves. Only that dial is written to: any
		// other conn an empty write reaches is a protocol's, which could send
		// a request header or an empty frame ahead of the payload.
		if early, isEarly := socket.(earlyConn); isEarly && early.NeedHandshake() {
			_, err := socket.Write(nil)
			if err != nil {
				return err
			}
			remote, local = addrsOf(innermost(conn))
		}
	}
	if !remote.Addr.IsValid() || !local.Addr.IsValid() {
		// Where the socket goes cannot be seen -- a gRPC or HTTP/2 transport
		// keeps it inside its own client -- so the configured server says.
		if route.Direct || !serverMayRelay(route.Server) {
			return nil
		}
		return relayGuard(ports, destination)
	}
	remoteAddr, localAddr := plain(remote.Addr), plain(local.Addr)
	// Landed on this host: loopback, the address it left from, or another of
	// this host's own -- which a direct outbound bound inside a named range
	// (inet4_bind_address) reaches from a trusted source.
	here := onThisHost(remoteAddr, localAddr) || hostAddrs.contains(remoteAddr)
	// A direct outbound whose socket went somewhere else than the address
	// asked for is not direct: the group it was picked from changed its mind
	// between routing and dialing.
	direct := route.Direct && (!destination.IsIP() || endpoint(destination) == endpoint(remote))
	switch {
	case direct && here && ports.has(remote.Port):
		// From this host there is no NAT: the port dialed is the port the
		// inbound listens on.
		return E.New("refused to dial ", remote, ": a PROXY-enabled inbound on this host, which would believe a header the client sent")
	case direct && !here && namedFront(remoteAddr):
		return E.New("refused to dial ", remote, ": a trusted PROXY protocol front, which a client could speak through as anyone")
	case !direct && (here || namedFront(remoteAddr)):
		return relayGuard(ports, destination)
	}
	return nil
}

// plain is addr as matched against this host's and the named fronts'
// addresses: unmapped, and without the zone a link-local one carries.
func plain(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}

// onThisHostAsked reports whether a client asking for addr reaches this host:
// loopback, one of its own addresses, or the unspecified address, which a
// connect on Linux takes as loopback.
func onThisHostAsked(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsUnspecified() || hostAddrs.contains(addr)
}

// serverMayRelay says whether an outbound's configured server is somewhere a
// relay could carry a client back in from: on this host, or at a named
// front's address. A server given by name is taken as off this host unless it
// is localhost: it is the operator's to point, and one that resolved here
// would be a chain the operator built on purpose. Nothing configured is
// taken as this host, which costs at most a refused dial.
func serverMayRelay(server M.Socksaddr) bool {
	switch {
	case !server.IsValid():
		return true
	case server.IsFqdn():
		name := strings.TrimSuffix(strings.ToLower(server.Fqdn), ".")
		return name == "localhost" || strings.HasSuffix(name, ".localhost")
	}
	addr := plain(server.Addr)
	return onThisHostAsked(addr) || namedFront(addr)
}

// relayGuard decides a dial to a relay, which opens the onward connection
// itself where the core cannot see its two ends: one on this host, or at a
// named front's address. destination is what the client asked it for; an
// address off this host and outside the named fronts cannot lead back into an
// inbound. A name can lead anywhere, so it is taken as this host. The relay
// connects to this host without a NAT, so the port asked for is the port it
// arrives on: a dial that could reach a PROXY-enabled inbound's port is
// refused, and nothing else is touched. A named front's address, asked for,
// may be a NAT that says nothing about the port, and is refused outright, as
// a direct dial there is.
func relayGuard(ports portSet, destination M.Socksaddr) error {
	asked := plain(destination.Addr)
	switch {
	case destination.IsIP() && namedFront(asked):
		return E.New("refused to relay to ", destination, ": a trusted PROXY protocol front, which a client could speak through as anyone")
	case destination.IsIP() && !onThisHostAsked(asked):
		return nil
	case ports.has(destination.Port):
		return E.New("refused to relay to ", destination, ": it could reach a PROXY-enabled inbound, ",
			"which would believe a header the client sent; give PROXY-enabled inbounds a port clients do not ask for")
	}
	return nil
}

// hostAddrs is this host's interface addresses, for telling a dial to one of
// them from one leaving the host. Re-read when older than hostAddrsTTL, by the
// first dial to need it: an address added since is missed for that long.
var hostAddrs = addrCache{load: interfaceAddrs, warn: func(err error) {
	panelLog.Warning("PROXY protocol: cannot read this host's addresses, counting every one as this host's for now: ", err)
}}

const hostAddrsTTL = 5 * time.Second

type addrCache struct {
	load     func() (map[netip.Addr]struct{}, error)
	warn     func(error)
	snapshot atomic.Pointer[addrSnapshot]
	reload   sync.Mutex
}

// addrSnapshot is one read of the addresses. A failed read is kept too, as
// unknown, for as long as a good one would be: every dial on a host whose own
// address is a named front comes through here, and retrying on each of them
// would put a syscall behind one lock in front of every connection.
type addrSnapshot struct {
	addrs   map[netip.Addr]struct{}
	unknown bool
	loaded  time.Time
}

func (c *addrCache) contains(addr netip.Addr) bool {
	snapshot := c.snapshot.Load()
	if snapshot == nil || time.Since(snapshot.loaded) >= hostAddrsTTL {
		c.reload.Lock()
		snapshot = c.snapshot.Load()
		if snapshot == nil || time.Since(snapshot.loaded) >= hostAddrsTTL {
			addrs, err := c.load()
			snapshot = &addrSnapshot{addrs: addrs, unknown: err != nil, loaded: time.Now()}
			c.snapshot.Store(snapshot)
			if err != nil && c.warn != nil {
				c.warn(err)
			}
		}
		c.reload.Unlock()
	}
	if snapshot.unknown {
		// Cannot tell: taken as this host's, which costs at most a refused
		// dial to a PROXY-enabled inbound's port.
		return true
	}
	_, found := snapshot.addrs[addr]
	return found
}

func interfaceAddrs() (map[netip.Addr]struct{}, error) {
	list, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	addrs := make(map[netip.Addr]struct{}, len(list))
	for _, item := range list {
		ipNet, isIPNet := item.(*net.IPNet)
		if !isIPNet {
			continue
		}
		if addr, ok := netip.AddrFromSlice(ipNet.IP); ok {
			addrs[addr.Unmap()] = struct{}{}
		}
	}
	return addrs, nil
}

// innermost follows conn's Upstream chain to the connection at the bottom.
func innermost(conn net.Conn) net.Conn {
	for {
		wrapper, isWrapper := conn.(common.WithUpstream)
		if !isWrapper {
			return conn
		}
		next, isConn := wrapper.Upstream().(net.Conn)
		if !isConn || next == nil {
			return conn
		}
		conn = next
	}
}

// errNoClient is a header that names no client: a v2 LOCAL command, which is
// how a front health-checks its backend, or a v1 UNKNOWN. Such a connection is
// closed rather than passed on as the front's own -- which is what refusing a
// trusted connection without a header is there to prevent -- and quietly: a
// front checking every few seconds would otherwise fill the log.
var errNoClient = errors.New("PROXY header names no client (a front's health check)")

// Read consumes the header conn starts with and returns the client address it
// names, or errNoClient when it names none.
func Read(conn net.Conn) (M.Socksaddr, error) {
	return readWithin(conn, readTimeout)
}

func readWithin(conn net.Conn, timeout time.Duration) (M.Socksaddr, error) {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return M.Socksaddr{}, err
	}
	raw, err := readRaw(conn)
	if err != nil {
		return M.Socksaddr{}, err
	}
	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return M.Socksaddr{}, err
	}
	header, err := proxyproto.Read(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return M.Socksaddr{}, err
	}
	if header.Command.IsLocal() || header.SourceAddr == nil {
		return M.Socksaddr{}, errNoClient
	}
	client := M.SocksaddrFromNet(header.SourceAddr).Unwrap()
	if !client.IsValid() {
		return M.Socksaddr{}, errNoClient
	}
	return client, nil
}

// readRaw reads exactly one header off r, signature included, and nothing
// after it. Parsing is go-proxyproto's; only the framing is done here, because
// its reader buffers past the header and the bytes it took would have to be
// handed back through a wrapper.
func readRaw(r io.Reader) ([]byte, error) {
	buf := make([]byte, v1MinLength, v1MaxLength)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, E.Cause(err, "read PROXY header")
	}
	switch {
	case bytes.HasPrefix(buf, proxyproto.SIGV1):
		for {
			need := v1Remaining(buf)
			if need == 0 {
				return buf, nil
			}
			if len(buf)+need > v1MaxLength {
				return nil, proxyproto.ErrVersion1HeaderTooLong
			}
			buf = buf[:len(buf)+need]
			_, err = io.ReadFull(r, buf[len(buf)-need:])
			if err != nil {
				return nil, E.Cause(err, "read PROXY header")
			}
		}
	case bytes.HasPrefix(buf, proxyproto.SIGV2):
		buf = append(buf, 0)
		_, err = io.ReadFull(r, buf[v1MinLength:])
		if err != nil {
			return nil, E.Cause(err, "read PROXY header")
		}
		length := int(binary.BigEndian.Uint16(buf[v2FixedLength-2:]))
		if length > v2MaxPayload {
			return nil, proxyproto.ErrInvalidLength
		}
		header := make([]byte, v2FixedLength+length)
		copy(header, buf)
		_, err = io.ReadFull(r, header[v2FixedLength:])
		if err != nil {
			return nil, E.Cause(err, "read PROXY header")
		}
		return header, nil
	default:
		return nil, E.New("no PROXY header from a trusted front")
	}
}

// v1Remaining is the fewest bytes that can still come before the v1 header
// that line starts can end, so reading exactly that many never reads past it:
// 0 once it has. A v1 line has no length field, and reading it a byte at a
// time cost a syscall per byte on every connection behind nginx, which sends
// only v1. The fields of "PROXY TCP4 src dst sport dport\r\n" each have a shortest
// form, which bounds what is left; anything else (UNKNOWN, or a line the
// parser will refuse anyway) falls back to the two bytes CRLF still needs.
func v1Remaining(line []byte) int {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return 0
	}
	if line[len(line)-1] == '\r' {
		return 1
	}
	fields := bytes.Split(line, []byte(" "))
	var shortestAddr int
	switch {
	case len(fields) > 2 && string(fields[1]) == "TCP4":
		shortestAddr = len("1.1.1.1")
	case len(fields) > 2 && string(fields[1]) == "TCP6":
		shortestAddr = len("::")
	default:
		return 2
	}
	shortest := []int{len("PROXY"), len("TCP4"), shortestAddr, shortestAddr, 1, 1}
	field := len(fields) - 1
	if field >= len(shortest) {
		return 2
	}
	need := max(0, shortest[field]-len(fields[field]))
	for _, n := range shortest[field+1:] {
		need += 1 + n
	}
	return need + len("\r\n")
}

// Handler puts reading the header in front of next, for an inbound's own
// listener, when enabled -- and is next itself when not. A connection another
// inbound's detour injects reaches next directly rather than through here, so
// its source -- already the client's -- is never read for a second header.
func Handler(next adapter.ConnectionHandler, logger logger.ContextLogger, settings Settings) adapter.ConnectionHandler {
	if !settings.Enabled {
		return next
	}
	markInUse(settings.Port)
	return &handler{next: next, logger: logger}
}

type handler struct {
	next   adapter.ConnectionHandler
	logger logger.ContextLogger
}

func (h *handler) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if !Trusted(metadata.Source, metadata.OriginDestination) {
		h.next.NewConnection(ctx, sniff(conn, h.logger, metadata.Source), metadata, onClose)
		return
	}
	client, err := Read(conn)
	if err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		logRefused(ctx, h.logger, metadata.Source, err)
		return
	}
	// Debug: sing-box's listener has already logged this connection at Info,
	// from the front.
	h.logger.DebugContext(ctx, "inbound connection from ", client, " via ", metadata.Source)
	metadata.Source = client
	// The conn says so too, as on a transport: REALITY and TLS read the
	// client from RemoteAddr, not from the metadata.
	h.next.NewConnection(ctx, &sourceConn{Conn: conn, remote: client.TCPAddr()}, metadata, onClose)
}

// logRefused reports a trusted connection that was refused. One that closed
// before sending a byte is a port check or a front's own TCP health check,
// and one whose header names no client is a front's PROXY health check: either
// would otherwise put an error in the log every few seconds.
func logRefused(ctx context.Context, logger logger.ContextLogger, source M.Socksaddr, err error) {
	err = E.Cause(err, "process connection from ", source)
	if errors.Is(err, io.EOF) || errors.Is(err, errNoClient) {
		logger.DebugContext(ctx, err)
		return
	}
	logger.ErrorContext(ctx, err)
}

// sniff watches the first bytes an untrusted connection sends for a PROXY
// signature. Such a connection is never read for a header, so the signature
// goes on to the protocol, which refuses it in terms of its own -- a TLS or
// VLESS error that says nothing about a front the operator forgot to list.
// It only looks at what the protocol reads anyway, across as many reads as
// the first signatureLength bytes take to arrive, and steps aside for reading
// once it has seen them (ReaderReplaceable). Writes it never looks at, so it
// steps aside for those from the start (WriterReplaceable): kTLS, vision's
// splice and a half-close all still reach the socket.
func sniff(conn net.Conn, logger logger.ContextLogger, source M.Socksaddr) net.Conn {
	return &sniffConn{Conn: conn, logger: logger, source: source}
}

type sniffConn struct {
	net.Conn
	logger logger.ContextLogger
	source M.Socksaddr
	// seen and length are the first bytes read so far; only Read touches
	// them. done is read by whoever unwraps the conn, from any goroutine.
	seen   [signatureLength]byte
	length int
	done   atomic.Bool
}

// signatureLength is enough of either signature to tell it from the start of
// a TLS record or an HTTP request.
const signatureLength = 5

func (c *sniffConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && !c.done.Load() {
		c.length += copy(c.seen[c.length:], p[:n])
		seen := c.seen[:c.length]
		switch {
		case !bytes.HasPrefix(proxyproto.SIGV1, seen) && !bytes.HasPrefix(proxyproto.SIGV2, seen):
			c.done.Store(true)
		case c.length == signatureLength:
			untrustedWarning.warn(c.logger, c.source, time.Now())
			c.done.Store(true)
		}
	}
	return n, err
}

func (c *sniffConn) Upstream() any {
	return c.Conn
}

func (c *sniffConn) ReaderReplaceable() bool {
	return c.done.Load()
}

func (c *sniffConn) WriterReplaceable() bool {
	return true
}

// untrustedWarning spaces out sniff's warning to one per warnInterval, saying
// how many it held back: one is all it takes to point at the setting.
var untrustedWarning warnLimiter

type warnLimiter struct {
	sync.Mutex
	last       time.Time
	suppressed int
}

func (w *warnLimiter) warn(logger logger.ContextLogger, source M.Socksaddr, now time.Time) {
	w.Lock()
	if !w.last.IsZero() && now.Sub(w.last) < warnInterval {
		w.suppressed++
		w.Unlock()
		return
	}
	suppressed := w.suppressed
	w.last, w.suppressed = now, 0
	w.Unlock()
	var held string
	if suppressed > 0 {
		held = " (and " + strconv.Itoa(suppressed) + " more since the last warning)"
	}
	logger.Warn("PROXY header from ", source, held, ", which is not a trusted front, passed on as data: ",
		"if this is your front, add it under Settings > Interface > Trusted PROXY Protocol Fronts")
}

// Listen is Handler for the listener a V2Ray transport serves, which takes the
// client address from RemoteAddr. Headers are read on a goroutine per
// connection and only finished ones are handed to Accept: an HTTP server asks
// for RemoteAddr on its accept loop, so one front that went quiet would
// otherwise stall every connection behind it. A connection that is not read
// for one -- most of them, on a public inbound -- needs none of that, and is
// handed over from the accept loop itself, as a plain listener would.
func Listen(l net.Listener, logger logger.ContextLogger, settings Settings) net.Listener {
	if !settings.Enabled {
		return l
	}
	markInUse(settings.Port)
	return listen(l, logger, handoffTimeout)
}

func listen(l net.Listener, logger logger.ContextLogger, handoff time.Duration) net.Listener {
	pl := &listener{
		Listener: l,
		logger:   logger,
		handoff:  handoff,
		conns:    make(chan net.Conn),
		done:     make(chan struct{}),
		closing:  make(chan struct{}),
	}
	go pl.loop()
	return pl
}

type listener struct {
	net.Listener
	logger    logger.ContextLogger
	handoff   time.Duration
	conns     chan net.Conn
	done      chan struct{}
	err       error // set before done is closed
	closing   chan struct{}
	closeOnce sync.Once
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, l.err
	}
}

// Close also wakes the accept loop if it is waiting to hand a connection
// over, which closing the listener underneath alone would not.
func (l *listener) Close() error {
	l.closeOnce.Do(func() { close(l.closing) })
	return l.Listener.Close()
}

func (l *listener) loop() {
	defer close(l.done)
	var acceptDelay time.Duration
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			// The same backoff sing-box's own accept loop uses.
			//nolint:staticcheck
			if netError, isNetError := err.(net.Error); isNetError && netError.Temporary() {
				if acceptDelay == 0 {
					acceptDelay = 5 * time.Millisecond
				} else {
					acceptDelay = min(2*acceptDelay, time.Second)
				}
				l.logger.Error(err, ", retrying in ", acceptDelay)
				time.Sleep(acceptDelay)
				continue
			}
			l.err = err
			return
		}
		acceptDelay = 0
		source := M.SocksaddrFromNet(conn.RemoteAddr()).Unwrap()
		local := M.SocksaddrFromNet(conn.LocalAddr()).Unwrap()
		if Trusted(source, local) {
			go l.handshake(conn, source)
			continue
		}
		select {
		case l.conns <- sniff(conn, l.logger, source):
		case <-l.closing:
			conn.Close()
		}
	}
}

func (l *listener) handshake(conn net.Conn, source M.Socksaddr) {
	client, err := Read(conn)
	if err != nil {
		conn.Close()
		logRefused(context.Background(), l.logger, source, err)
		return
	}
	conn = &sourceConn{Conn: conn, remote: client.TCPAddr()}
	timer := time.NewTimer(l.handoff)
	defer timer.Stop()
	select {
	case l.conns <- conn:
	case <-l.done:
		conn.Close()
	case <-l.closing:
		conn.Close()
	case <-timer.C:
		conn.Close()
		l.logger.Error("connection from ", source, " closed: the transport is no longer accepting")
	}
}

// sourceConn reports the client the header named as its remote address, and
// is otherwise the connection itself: it steps aside for anything that looks
// for the socket underneath (Upstream, Reader/WriterReplaceable). Both
// listeners hand one on. TLS goes over it, and kTLS and vision's splice have
// to reach the TCP socket through it -- sing-box closes a connection whose
// kTLS setup cannot.
type sourceConn struct {
	net.Conn
	remote net.Addr
}

func (c *sourceConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *sourceConn) Upstream() any {
	return c.Conn
}

func (c *sourceConn) ReaderReplaceable() bool {
	return true
}

func (c *sourceConn) WriterReplaceable() bool {
	return true
}
