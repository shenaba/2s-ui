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
// (GuardDial, from the connection tracker), and a header on one it dialed to
// this host is refused. A direct dial to a named front is refused outright:
// past a NAT -- a Docker port mapping -- it arrives from the front's address,
// with a source port that says nothing about who opened it.
//
// The same goes for a relay the core hands a client to: an outbound to a
// socks or shadowsocks server on this host (a local gost, a WARP client), or
// at a named front's address. That relay opens the onward connection, so the
// core never sees its two ends, and it arrives from a trusted address. Which
// outbound is direct is the router's to say (Route): a name a client asks the
// direct outbound for is dialed by the core itself, and its record is exact.
// A relay on this host connects without a NAT, so a client asking it for a
// PROXY-enabled inbound's port by a name, or by an address of this host, is
// refused -- only that client, and only on those ports; a PROXY-enabled
// inbound on a port clients do ask for (443) takes that port away from them
// through such a relay. A relay asked for a named front's address is refused
// as a direct dial there is. A relay at a named front's address -- an upstream
// inside a listed range -- may reach back through a NAT that changes the
// port, so it is watched instead: a trusted connection from that address
// within relayWindow is refused. An upstream is not usually also a front, so
// the watch costs a real front nothing.
//
// Two paths stay outside this. A relay on this host that a name sends through
// a named front's NAT and back arrives from the front on another port. And
// dials the core makes for an inbound rather than for a client -- a REALITY
// or ShadowTLS handshake server -- are the operator's to point, and never at
// a PROXY-enabled inbound.
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

	// relayWindow is how long a watch on a relay at a named front's address
	// lasts, from the dial and again from the client's first bytes. The relay
	// opens its onward connection by then -- at the dial for socks and HTTP,
	// on the first bytes for shadowsocks and the other lazy protocols -- and
	// the inbound has read the header within readTimeout of accepting it. The
	// rest is the relay's own dial back, a hop inside the operator's network.
	relayWindow = 3 * readTimeout
	// relaySweepAt is how many watches may pile up before the expired ones
	// are cleared. Outbounds bound them anyway.
	relaySweepAt = 1024
)

// Settings is what an inbound asked of PROXY protocol: whether to read
// headers, and the port it listens on, which is what a client handed to a
// relay on this host could reach it by.
type Settings struct {
	Enabled bool
	Port    uint16
}

// Take reports what options ask of PROXY protocol, and clears the request:
// sing-box's own listener refuses to start while it is set. The copies call it
// before building their listener, and keep the answer themselves.
func Take(options *option.ListenOptions) Settings {
	//nolint:staticcheck
	enabled := options.ProxyProtocol
	//nolint:staticcheck
	options.ProxyProtocol = false
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
	inUseMu.Lock()
	since := built
	inUseMu.Unlock()
	found := running()
	inUseMu.Lock()
	defer inUseMu.Unlock()
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
		return
	}
	inUse.Store(&ports)
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
	addr := source.Addr.Unmap()
	if !addr.IsValid() {
		return false
	}
	return onThisHost(addr, local.Addr.Unmap()) || namedFront(addr)
}

// dialed counts the connections the core has open to this host on a client's
// behalf, by both ends. The local end alone is not enough: Linux hands one
// ephemeral port to connections bound for different destinations, so a front's
// connection to the inbound can leave from the very address and port of a core
// dial to some other service here, and would be refused as the core's own.
// From this host there is no NAT in between, so the inbound sees the same two
// ends the dial does. A count rather than a set: a four-tuple can come back
// while the record of its last use is still lingering.
var dialed = struct {
	sync.Mutex
	conns map[fourTuple]int
}{conns: make(map[fourTuple]int)}

// fourTuple is a connection by its client end and its server end.
type fourTuple struct {
	client, server netip.AddrPort
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

// Guard is what GuardDial keeps about one connection the core dialed for a
// client. A nil Guard keeps nothing.
type Guard struct {
	release func()
	watch   []netip.Addr
}

// Release is called once the connection ends.
func (g *Guard) Release() {
	if g != nil && g.release != nil {
		g.release()
	}
}

// WatchesRelay reports whether Payload has anything to do.
func (g *Guard) WatchesRelay() bool {
	return g != nil && len(g.watch) > 0
}

// Payload is called once, when the client's first bytes are relayed down the
// connection: a lazy relay -- shadowsocks, vmess -- opens its onward
// connection only then, so the watch on it starts over.
func (g *Guard) Payload() {
	if g == nil {
		return
	}
	now := time.Now()
	for _, addr := range g.watch {
		watchRelay(addr, now)
	}
}

// GuardDial is told about each connection the core has just dialed for a
// client, to destination -- the address the client asked for -- along the
// route the router chose. While PROXY protocol is in use, a direct dial to a
// front the operator named is refused, and one an inbound would trust is
// remembered until readTimeout after Release, so that a PROXY-enabled inbound
// it reaches can tell it from a real front (dialedByCore). A dial to a relay
// that could carry the client back into a PROXY-enabled inbound is refused, or
// watched (see the package comment).
//
// The record outlives the connection because the inbound's check can come
// after it: a client that sends its forged header and resets at once has the
// core close both ends, while the inbound may still be reading the header off
// its buffer. That read started when the inbound accepted, before this dial
// returned, and ends within readTimeout of it, so readTimeout after the close
// always covers it.
func GuardDial(conn net.Conn, destination M.Socksaddr, route Route) (*Guard, error) {
	return guardDial(conn, destination, route, readTimeout)
}

// earlyConn is a dial that has not opened its socket yet: sing-box's TCP Fast
// Open dial, which says so through NeedHandshake.
type earlyConn interface {
	NeedHandshake() bool
}

func guardDial(conn net.Conn, destination M.Socksaddr, route Route, linger time.Duration) (*Guard, error) {
	ports := inUse.Load()
	if ports == nil {
		return nil, nil
	}
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
				return nil, err
			}
			remote, local = addrsOf(innermost(conn))
		}
	}
	if !remote.Addr.IsValid() || !local.Addr.IsValid() {
		// Where the socket goes cannot be seen -- a gRPC or HTTP/2 transport
		// keeps it inside its own client -- so the configured server says.
		if route.Direct {
			return nil, nil
		}
		here, at, relays := serverPlace(route.Server)
		if !relays {
			return nil, nil
		}
		return relayGuard(*ports, here, at, destination)
	}
	remoteAddr, localAddr := plain(remote.Addr), plain(local.Addr)
	here := onThisHost(remoteAddr, localAddr)
	// A direct outbound whose socket went somewhere else than the address
	// asked for is not direct: the group it was picked from changed its mind
	// between routing and dialing.
	direct := route.Direct && (!destination.IsIP() || endpoint(destination) == endpoint(remote))
	var guard *Guard
	switch {
	case direct && !here && namedFront(remoteAddr):
		return nil, E.New("refused to dial ", remote, ": a trusted PROXY protocol front, which a client could speak through as anyone")
	case !direct && (here || namedFront(remoteAddr)):
		var err error
		guard, err = relayGuard(*ports, here, remoteAddr, destination)
		if err != nil {
			return nil, err
		}
	}
	// Remembered whenever an inbound could read it for a header. One bound
	// for this host from this host (here) is trusted by the same rule on
	// arrival. So is one leaving from a named front's address -- a direct
	// outbound bound inside a named range with inet4_bind_address -- but
	// only one bound for another of this host's addresses can reach an
	// inbound: on a cloud VM whose own address is inside the listed VPC
	// range that is every dial, and the rest are not worth a record each.
	if here || (namedFront(localAddr) && hostAddrs.contains(remoteAddr)) {
		if guard == nil {
			guard = &Guard{}
		}
		guard.release = remember(fourTuple{client: endpoint(local), server: endpoint(remote)}, linger)
	}
	return guard, nil
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

// serverPlace says where an outbound's configured server is: on this host
// (here), at a named front's address (at), and whether it is either, so that
// it could relay a client back in. A server given by name is taken as off
// this host unless it is localhost: it is the operator's to point, and one
// that resolved here would be a chain the operator built on purpose. Nothing
// configured is taken as this host, which costs at most a refused dial.
func serverPlace(server M.Socksaddr) (here bool, at netip.Addr, relays bool) {
	switch {
	case !server.IsValid():
		return true, netip.Addr{}, true
	case server.IsFqdn():
		name := server.Fqdn
		local := name == "localhost" || len(name) > len(".localhost") && name[len(name)-len(".localhost"):] == ".localhost"
		return local, netip.Addr{}, local
	}
	addr := plain(server.Addr)
	if onThisHostAsked(addr) {
		return true, netip.Addr{}, true
	}
	if namedFront(addr) {
		return false, addr, true
	}
	return false, netip.Addr{}, false
}

// remember records key until linger after the returned release is called.
func remember(key fourTuple, linger time.Duration) func() {
	dialed.Lock()
	dialed.conns[key]++
	dialed.Unlock()
	forget := func() {
		dialed.Lock()
		if dialed.conns[key]--; dialed.conns[key] <= 0 {
			delete(dialed.conns, key)
		}
		dialed.Unlock()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if linger <= 0 {
				forget()
				return
			}
			time.AfterFunc(linger, forget)
		})
	}
}

// relayGuard decides a dial to a relay, which opens the onward connection
// itself where the core cannot see its two ends: one on this host (here), or
// at relayAt, a named front's address. destination is what the client asked
// it for; an address off this host and outside the named fronts cannot lead
// back into an inbound. A name can lead anywhere, so it is taken as this host.
//
// A relay on this host connects to this host without a NAT, so the port asked
// for is the port it arrives on: a dial that could reach a PROXY-enabled
// inbound's port is refused, and nothing else is touched. A named front's
// address, asked for, may be a NAT that says nothing about the port, and is
// refused outright, as a direct dial there is. A relay at a named front's
// address arrives from that address, possibly through a NAT of its own, so it
// is watched by that address instead (Guard, relayed): a front is not usually
// also an outbound's server, so the watch costs no real front anything.
func relayGuard(ports portSet, here bool, relayAt netip.Addr, destination M.Socksaddr) (*Guard, error) {
	asked := plain(destination.Addr)
	switch {
	case destination.IsIP() && namedFront(asked):
		return nil, E.New("refused to relay to ", destination, ": a trusted PROXY protocol front, which a client could speak through as anyone")
	case destination.IsIP() && !onThisHostAsked(asked):
		return nil, nil
	case here:
		if ports.has(destination.Port) {
			return nil, E.New("refused to relay to ", destination, " through a proxy on this host: it could reach a PROXY-enabled inbound, ",
				"which would believe a header the client sent; give PROXY-enabled inbounds a port clients do not ask for")
		}
		return nil, nil
	case !relayAt.IsValid():
		return nil, nil
	}
	guard := &Guard{watch: []netip.Addr{relayAt}}
	guard.Payload()
	return guard, nil
}

// relays holds the watches on relays at named fronts' addresses, each until
// the time it lapses. Keyed by an outbound's server, so there are only ever as
// many as there are outbounds; swept at most once a relayWindow all the same.
var relays = struct {
	sync.Mutex
	until map[netip.Addr]time.Time
	swept time.Time
}{until: make(map[netip.Addr]time.Time)}

func watchRelay(addr netip.Addr, now time.Time) {
	relays.Lock()
	defer relays.Unlock()
	if until := now.Add(relayWindow); until.After(relays.until[addr]) {
		relays.until[addr] = until
	}
	if len(relays.until) > relaySweepAt && now.Sub(relays.swept) >= relayWindow {
		relays.swept = now
		for addr, until := range relays.until {
			if !now.Before(until) {
				delete(relays.until, addr)
			}
		}
	}
}

// relayed reports whether a connection from source matches a watch on a
// relay.
func relayed(source M.Socksaddr) bool {
	addr := plain(source.Addr)
	relays.Lock()
	defer relays.Unlock()
	return time.Now().Before(relays.until[addr])
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
		// Cannot tell: remembered, which costs a record, not a client.
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

// dialedByCore reports whether a connection from source that arrived on local
// is one the core dialed for a client.
func dialedByCore(source, local M.Socksaddr) bool {
	dialed.Lock()
	defer dialed.Unlock()
	return dialed.conns[fourTuple{client: endpoint(source), server: endpoint(local)}] > 0
}

// accept reads the header off a trusted conn from source that arrived on
// local, and refuses one the core dialed itself, or one a relay at a named
// front's address may have opened. Checked only after the header is read: the core reports its
// dial before it relays a byte the client sent, so by now it has been told.
func accept(conn net.Conn, source, local M.Socksaddr) (M.Socksaddr, error) {
	client, err := Read(conn)
	if err != nil {
		return M.Socksaddr{}, err
	}
	if dialedByCore(source, local) {
		return M.Socksaddr{}, E.New("PROXY header on a connection this core dialed for a client")
	}
	if relayed(source) {
		return M.Socksaddr{}, E.New("PROXY header on a connection from ", source.Addr, ", an outbound server the core just handed a client to, which may have relayed it back")
	}
	return client, nil
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
	client, err := accept(conn, metadata.Source, metadata.OriginDestination)
	if err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		logRefused(ctx, h.logger, metadata.Source, err)
		return
	}
	// Debug: sing-box's listener has already logged this connection at Info,
	// from the front.
	h.logger.DebugContext(ctx, "inbound connection from ", client, " via ", metadata.Source)
	metadata.Source = client
	h.next.NewConnection(ctx, conn, metadata, onClose)
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
			go l.handshake(conn, source, local)
			continue
		}
		select {
		case l.conns <- sniff(conn, l.logger, source):
		case <-l.closing:
			conn.Close()
		}
	}
}

func (l *listener) handshake(conn net.Conn, source, local M.Socksaddr) {
	client, err := accept(conn, source, local)
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
// for the socket underneath (Upstream, Reader/WriterReplaceable). A transport
// serving TLS puts it under the TLS conn, and kTLS has to reach the TCP socket
// through it -- sing-box closes a connection whose kTLS setup cannot.
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
