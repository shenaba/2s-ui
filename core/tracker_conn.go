package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

// ConnGate reports whether a freshly routed connection may proceed. The panel
// installs one to enforce per-client IP limits; the deny state behind it is
// policy that has to outlive a core restart, so it lives in the service layer
// and is reached through the package-level gate below rather than being copied
// into each tracker.
type ConnGate func(user string, source netip.Addr) bool

var connGate atomic.Pointer[ConnGate]

// SetConnGate installs (or with a nil gate, removes) the connection gate. It is
// read per connection instead of captured in NewConnTracker so that installing
// it never has to be ordered against core startup: Box, and with it every
// tracker, is rebuilt from scratch on each Start.
func SetConnGate(gate ConnGate) {
	if gate == nil {
		connGate.Store(nil)
		return
	}
	connGate.Store(&gate)
}

func gateAllows(user string, source netip.Addr) bool {
	gate := connGate.Load()
	if gate == nil {
		return true
	}
	return (*gate)(user, source)
}

var ipLimitActive atomic.Bool

// SetIPLimitActive tells the trackers whether any client currently has an IP
// limit. When none does, lastActive has no reader -- planIPLimit only looks at
// clients with limit_ip > 0 -- so stamping it on every read and write would be
// a clock call per packet that nothing consumes.
//
// Switching this on leaves already-established connections reporting their
// creation time until their next read, so the first scan afterwards may see
// them as idle. That errs towards not counting a connection rather than
// evicting a live one, and corrects itself on the following scan.
func SetIPLimitActive(active bool) {
	ipLimitActive.Store(active)
}

// IPLimitActive reports whether lastActive is being stamped at all. Readers of
// IPWindow.LastSeen need it: with no limit anywhere the field never moves off
// the connection's creation time, so an idle test against it measures age, not
// inactivity.
func IPLimitActive() bool {
	return ipLimitActive.Load()
}

// IPv6IdentityPrefixBits is how much of an IPv6 address identifies one
// subscriber. This is policy rather than mechanism -- /128 lets a single SLAAC
// host exhaust any limit through address rotation, /48 would hold an entire
// site to one slot -- but unlike the ban list it cannot live in the service
// layer: the normalized address is the map key every later stage groups on, so
// it has to be decided at track time. Change the trade-off here.
//
// Exported because anything displaying a normalized v6 address has to say which
// prefix it stands for: the bare masked address is one no client ever used.
const IPv6IdentityPrefixBits = 64

// normalizeSrc reduces a source address to the identity an IP limit counts.
//
// Unmap first: the same v4 client can surface as 1.2.3.4 on one path and
// ::ffff:1.2.3.4 on another, and counting those separately would let a single
// phone exhaust a limit of two on its own.
//
// Then mask v6 to /64. SLAAC privacy extensions hand one host a rotating set of
// addresses inside its prefix, so an unmasked count is effectively unbounded
// per device. The cost is that distinct devices behind one /64 count as one --
// which is already what happens to every device behind one IPv4 NAT, so this
// keeps the two families consistent instead of introducing a new asymmetry.
func normalizeSrc(addr netip.Addr) netip.Addr {
	addr = addr.Unmap()
	if !addr.Is6() {
		return addr
	}
	prefix, err := addr.Prefix(IPv6IdentityPrefixBits)
	if err != nil {
		return addr
	}
	return prefix.Addr()
}

// IPWindow is one source IP's liveness window for a user, expressed in the
// owning tracker's monotonic basis (see ConnTracker.epoch).
type IPWindow struct {
	FirstSeen int64
	LastSeen  int64
}

type ConnectionInfo struct {
	ID         string
	Conn       net.Conn
	PacketConn network.PacketConn
	Inbound    string
	User       string
	Type       string // "tcp" or "udp"

	// Source is the normalized client address, invalid when the inbound routed
	// by domain rather than IP. CreatedAt and lastActive are nanoseconds since
	// the owning tracker's epoch: a monotonic basis, so an NTP step cannot
	// reorder who connected first.
	Source     netip.Addr
	CreatedAt  int64
	lastActive atomic.Int64
}

// Every tracker gets a distinct generation so readers can tell that the one
// they are looking at is not the one they saw last time. Comparing timestamps
// instead would miss the common case: a core that ran briefly before crashing
// leaves a small reading behind, and the replacement passes it within seconds.
var trackerGeneration atomic.Uint64

type ConnTracker struct {
	access      sync.Mutex
	connections map[string]*ConnectionInfo
	epoch       time.Time
	generation  uint64
}

func NewConnTracker() *ConnTracker {
	return &ConnTracker{
		connections: make(map[string]*ConnectionInfo),
		epoch:       time.Now(),
		generation:  trackerGeneration.Add(1),
	}
}

// Generation identifies this tracker instance. Box is rebuilt on every core
// Start, so a caller holding state derived from the previous tracker's clock
// can use this to notice that the basis changed and discard it.
func (c *ConnTracker) Generation() uint64 {
	return c.generation
}

func (c *ConnTracker) monoNow() int64 {
	return int64(time.Since(c.epoch))
}

// touch records data-plane activity, skipping the clock call entirely when no
// client has an IP limit -- see SetIPLimitActive.
func (c *ConnTracker) touch(info *ConnectionInfo) {
	if !ipLimitActive.Load() {
		return
	}
	info.lastActive.Store(c.monoNow())
}

// admit applies the gate to a new connection. Unauthenticated inbounds (dns,
// direct) carry no user, and a gate keyed on "" would deny all of them at once,
// so those always pass.
func (c *ConnTracker) admit(user string, source netip.Addr) bool {
	if user == "" || !source.IsValid() {
		return true
	}
	return gateAllows(user, source)
}

func (c *ConnTracker) Reset() {
	c.access.Lock()
	defer c.access.Unlock()
	for _, connInfo := range c.connections {
		if connInfo.Conn != nil {
			_ = connInfo.Conn.Close()
		}
		if connInfo.PacketConn != nil {
			_ = connInfo.PacketConn.Close()
		}
	}
	c.connections = make(map[string]*ConnectionInfo)
}

func (c *ConnTracker) generateConnectionID() string {
	return uuid.Must(uuid.NewV4()).String()
}

// Closing the connection is the only way a tracker can refuse one: sing-box
// hands our return value straight to the outbound handler and has no reject
// path, so the outbound still dials once before the copy fails. Refusing at the
// inbound instead would mean editing the protocol copies in core/protocol/,
// which have to stay verbatim against upstream.
func (c *ConnTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	source := normalizeSrc(metadata.Source.Addr)
	if !c.admit(metadata.User, source) {
		_ = conn.Close()
		return conn
	}

	connID := c.generateConnectionID()
	now := c.monoNow()
	connInfo := &ConnectionInfo{
		ID:        connID,
		Conn:      conn,
		Inbound:   metadata.Inbound,
		User:      metadata.User,
		Type:      "tcp",
		Source:    source,
		CreatedAt: now,
	}
	connInfo.lastActive.Store(now)

	c.trackConnection(connID, connInfo)

	return c.createWrappedConn(conn, connInfo)
}

func (c *ConnTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	source := normalizeSrc(metadata.Source.Addr)
	if !c.admit(metadata.User, source) {
		_ = conn.Close()
		return conn
	}

	connID := c.generateConnectionID()
	now := c.monoNow()
	connInfo := &ConnectionInfo{
		ID:         connID,
		PacketConn: conn,
		Inbound:    metadata.Inbound,
		User:       metadata.User,
		Type:       "udp",
		Source:     source,
		CreatedAt:  now,
	}
	connInfo.lastActive.Store(now)

	c.trackConnection(connID, connInfo)

	return c.createWrappedPacketConn(conn, connInfo)
}

// UserIPs snapshots the live source IPs of every user want accepts, returning
// now in the same monotonic basis as the windows so a caller never mixes
// clocks. The maps are freshly built because the caller reads them on a cron
// goroutine, well after the tracker lock is gone.
//
// want filters (nil means every user) because this runs under the mutex that
// every connection setup and teardown takes: the walk over c.connections is
// unavoidable, but building a nested map for each of hundreds of users when the
// caller wants one of them is allocation held against the data plane. Both
// callers know exactly which names they need.
func (c *ConnTracker) UserIPs(want func(user string) bool) (map[string]map[netip.Addr]IPWindow, int64) {
	c.access.Lock()
	defer c.access.Unlock()

	users := make(map[string]map[netip.Addr]IPWindow)
	for _, connInfo := range c.connections {
		if connInfo.User == "" || !connInfo.Source.IsValid() {
			continue
		}
		if want != nil && !want(connInfo.User) {
			continue
		}
		ips, ok := users[connInfo.User]
		if !ok {
			ips = make(map[netip.Addr]IPWindow)
			users[connInfo.User] = ips
		}
		lastActive := connInfo.lastActive.Load()
		window, seen := ips[connInfo.Source]
		if !seen {
			ips[connInfo.Source] = IPWindow{FirstSeen: connInfo.CreatedAt, LastSeen: lastActive}
			continue
		}
		if connInfo.CreatedAt < window.FirstSeen {
			window.FirstSeen = connInfo.CreatedAt
		}
		if lastActive > window.LastSeen {
			window.LastSeen = lastActive
		}
		ips[connInfo.Source] = window
	}
	return users, c.monoNow()
}

// CloseConnByUserIPs closes every tracked connection whose (user, source IP)
// pair appears in drop. Unlike CloseConnByInboundUsers this takes a drop set
// rather than a keep set: the caller has already decided exactly which IPs lose
// their slot, and inverting that would force it to enumerate everyone staying.
func (c *ConnTracker) CloseConnByUserIPs(drop map[string]map[netip.Addr]struct{}) int {
	c.access.Lock()
	defer c.access.Unlock()

	closedCount := 0
	for connID, connInfo := range c.connections {
		ips, ok := drop[connInfo.User]
		if !ok {
			continue
		}
		if _, ok := ips[connInfo.Source]; !ok {
			continue
		}
		if connInfo.Conn != nil {
			connInfo.Conn.Close()
		}
		if connInfo.PacketConn != nil {
			connInfo.PacketConn.Close()
		}
		delete(c.connections, connID)
		closedCount++
	}
	return closedCount
}

func (c *ConnTracker) CloseConnByInbound(inbound string) int {
	c.access.Lock()
	defer c.access.Unlock()

	closedCount := 0
	for connID, connInfo := range c.connections {
		if connInfo.Inbound == inbound {
			if connInfo.Conn != nil {
				connInfo.Conn.Close()
			}
			if connInfo.PacketConn != nil {
				connInfo.PacketConn.Close()
			}
			delete(c.connections, connID)
			closedCount++
		}
	}
	return closedCount
}

func (c *ConnTracker) CloseConnByInboundUsers(inbound string, keepUsers map[string]struct{}) int {
	c.access.Lock()
	defer c.access.Unlock()

	closedCount := 0
	for connID, connInfo := range c.connections {
		if connInfo.Inbound != inbound {
			continue
		}
		if _, keep := keepUsers[connInfo.User]; keep {
			continue
		}
		if connInfo.Conn != nil {
			connInfo.Conn.Close()
		}
		if connInfo.PacketConn != nil {
			connInfo.PacketConn.Close()
		}
		delete(c.connections, connID)
		closedCount++
	}
	return closedCount
}

func (c *ConnTracker) trackConnection(connID string, connInfo *ConnectionInfo) {
	c.access.Lock()
	defer c.access.Unlock()
	c.connections[connID] = connInfo
}

func (c *ConnTracker) untrackConnection(connID string) {
	c.access.Lock()
	defer c.access.Unlock()
	delete(c.connections, connID)
}

// shouldUntrackIOErr reports whether err indicates the connection is done (peer closed, reset, etc.).
func shouldUntrackIOErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return !ne.Temporary()
	}
	return true
}

// The wrappers hold the ConnectionInfo rather than its id: touching lastActive
// on every read would otherwise mean taking the tracker mutex to look the entry
// up, on the hottest path there is.
func (c *ConnTracker) createWrappedConn(conn net.Conn, info *ConnectionInfo) *wrappedConn {
	return &wrappedConn{
		Conn:    conn,
		tracker: c,
		info:    info,
	}
}

func (c *ConnTracker) createWrappedPacketConn(conn network.PacketConn, info *ConnectionInfo) *wrappedPacketConn {
	return &wrappedPacketConn{
		PacketConn: conn,
		tracker:    c,
		info:       info,
	}
}

type wrappedConn struct {
	net.Conn
	tracker     *ConnTracker
	info        *ConnectionInfo
	untrackOnce sync.Once
}

func (w *wrappedConn) doUntrack() {
	w.untrackOnce.Do(func() {
		w.tracker.untrackConnection(w.info.ID)
	})
}

func (w *wrappedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if n > 0 {
		w.tracker.touch(w.info)
	}
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Write(b []byte) (int, error) {
	n, err := w.Conn.Write(b)
	if n > 0 {
		w.tracker.touch(w.info)
	}
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Close() error {
	w.doUntrack()
	return w.Conn.Close()
}

func (w *wrappedConn) Upstream() any {
	return w.Conn
}

type wrappedPacketConn struct {
	network.PacketConn
	tracker     *ConnTracker
	info        *ConnectionInfo
	untrackOnce sync.Once
}

func (w *wrappedPacketConn) doUntrack() {
	w.untrackOnce.Do(func() {
		w.tracker.untrackConnection(w.info.ID)
	})
}

func (w *wrappedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	dest, err := w.PacketConn.ReadPacket(buffer)
	if err == nil {
		w.tracker.touch(w.info)
	}
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return dest, err
}

func (w *wrappedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := w.PacketConn.WritePacket(buffer, destination)
	if err == nil {
		w.tracker.touch(w.info)
	}
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return err
}

func (w *wrappedPacketConn) Close() error {
	w.doUntrack()
	return w.PacketConn.Close()
}

func (w *wrappedPacketConn) Upstream() any {
	return w.PacketConn
}
