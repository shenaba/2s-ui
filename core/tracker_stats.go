package core

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/database/model"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/atomic"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

// ExtractTarget extracts and normalizes the target host (domain or destination IP) from connection metadata.
func ExtractTarget(domain string, dest M.Socksaddr) string {
	target := strings.TrimSpace(domain)
	if target == "" {
		target = strings.TrimSpace(dest.Fqdn)
	}
	if target == "" && dest.IsValid() {
		if dest.Addr.IsValid() {
			addr := dest.Addr
			if addr.Is4() {
				b := addr.As4()
				target = net.IPv4(b[0], b[1], b[2], 0).String() + "/24"
			} else if addr.Is6() {
				b := addr.As16()
				var masked [16]byte
				copy(masked[:6], b[:6])
				target = net.IP(masked[:]).String() + "/48"
			} else {
				target = addr.String()
			}
		}
	}
	if target == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(target); err == nil {
		target = host
	}
	return strings.ToLower(strings.TrimSpace(target))
}

type Counter struct {
	read  *atomic.Int64
	write *atomic.Int64
}

type TargetCounter struct {
	read   *atomic.Int64
	write  *atomic.Int64
	active *atomic.Int32
}

type StatsTracker struct {
	access         sync.Mutex
	inbounds       map[string]Counter
	outbounds      map[string]Counter
	users          map[string]Counter
	targets        map[string]TargetCounter // key: user + "\x00" + target
	targetIdle     map[string]int
	trafficEnabled atomic.Bool

	// Rate sampling over the user counters, without a second set of counters on
	// the data path: SampleUsers reads them in place, and GetStats, which
	// empties them, books what it took into drained so the next sample still
	// sees it. All three are guarded by access.
	sampled   map[string]UserBytes // each user counter as the last sample read it
	drained   map[string]UserBytes // taken out by GetStats since the last sample
	sampledAt time.Time
}

// UserBytes is one user's traffic in each direction, named from the client's
// side as the stats rows are: Up is what it sent, Down what it received.
type UserBytes struct {
	Up   int64
	Down int64
}

func NewStatsTracker() *StatsTracker {
	st := &StatsTracker{
		inbounds:   make(map[string]Counter),
		outbounds:  make(map[string]Counter),
		users:      make(map[string]Counter),
		targets:    make(map[string]TargetCounter),
		targetIdle: make(map[string]int),
		sampled:    make(map[string]UserBytes),
		drained:    make(map[string]UserBytes),
		// Every counter starts at zero here, so this is where the first
		// sample's window starts.
		sampledAt: time.Now(),
	}
	st.trafficEnabled.Store(true)
	return st
}

func (c *StatsTracker) Reset() {
	c.access.Lock()
	defer c.access.Unlock()
	c.inbounds = make(map[string]Counter)
	c.outbounds = make(map[string]Counter)
	c.users = make(map[string]Counter)
	c.targets = make(map[string]TargetCounter)
	c.targetIdle = make(map[string]int)
	c.sampled = make(map[string]UserBytes)
	c.drained = make(map[string]UserBytes)
	c.sampledAt = time.Now()
}

func (c *StatsTracker) SetTrafficEnabled(enabled bool) {
	c.trafficEnabled.Store(enabled)
}

func (c *StatsTracker) IsTrafficEnabled() bool {
	return c.trafficEnabled.Load()
}

func (c *StatsTracker) getReadCounters(inbound string, outbound string, user string, target string) ([]*atomic.Int64, []*atomic.Int64, func()) {
	var readCounter []*atomic.Int64
	var writeCounter []*atomic.Int64
	var release func()
	c.access.Lock()
	defer c.access.Unlock()

	if inbound != "" {
		readCounter = append(readCounter, c.loadOrCreateCounter(&c.inbounds, inbound).read)
		writeCounter = append(writeCounter, c.inbounds[inbound].write)
	}
	if outbound != "" {
		readCounter = append(readCounter, c.loadOrCreateCounter(&c.outbounds, outbound).read)
		writeCounter = append(writeCounter, c.outbounds[outbound].write)
	}
	if user != "" {
		readCounter = append(readCounter, c.loadOrCreateCounter(&c.users, user).read)
		writeCounter = append(writeCounter, c.users[user].write)
	}
	if c.trafficEnabled.Load() && target != "" {
		key := user + "\x00" + target
		counter := c.loadOrCreateTargetCounter(key)
		counter.active.Add(1)
		readCounter = append(readCounter, counter.read)
		writeCounter = append(writeCounter, counter.write)
		release = func() {
			counter.active.Add(-1)
		}
	}
	return readCounter, writeCounter, release
}

func (c *StatsTracker) loadOrCreateCounter(obj *map[string]Counter, name string) Counter {
	counter, loaded := (*obj)[name]
	if loaded {
		return counter
	}
	counter = Counter{read: &atomic.Int64{}, write: &atomic.Int64{}}
	(*obj)[name] = counter
	return counter
}

func (c *StatsTracker) loadOrCreateTargetCounter(name string) TargetCounter {
	counter, loaded := c.targets[name]
	if loaded {
		return counter
	}
	counter = TargetCounter{read: &atomic.Int64{}, write: &atomic.Int64{}, active: &atomic.Int32{}}
	c.targets[name] = counter
	return counter
}

type trackerConn struct {
	net.Conn
	onClose func()
	closed  atomic.Bool
}

func (c *trackerConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.onClose != nil {
			c.onClose()
		}
	}
	return c.Conn.Close()
}

// Upstream and the Replaceable pair let sing's copy loop see through this
// wrapper to the counter conn beneath it, the way sing-box's own trackedConn
// does. Without them bufio.Copy stops unwrapping here, so every routed
// connection lost splice and the read-waiter / vectorised fast paths. Only the
// copy loop is unwrapped; Close is still called on this wrapper.
func (c *trackerConn) Upstream() any {
	return c.Conn
}

func (c *trackerConn) ReaderReplaceable() bool {
	return true
}

func (c *trackerConn) WriterReplaceable() bool {
	return true
}

type trackerPacketConn struct {
	network.PacketConn
	onClose func()
	closed  atomic.Bool
}

func (c *trackerPacketConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.onClose != nil {
			c.onClose()
		}
	}
	return c.PacketConn.Close()
}

// See trackerConn.Upstream: keeps the batch and read-waiter packet paths.
func (c *trackerPacketConn) Upstream() any {
	return c.PacketConn
}

func (c *trackerPacketConn) ReaderReplaceable() bool {
	return true
}

func (c *trackerPacketConn) WriterReplaceable() bool {
	return true
}

func (c *StatsTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	var target string
	if c.trafficEnabled.Load() {
		target = ExtractTarget(metadata.Domain, metadata.Destination)
	}
	readCounter, writeCounter, release := c.getReadCounters(metadata.Inbound, matchOutbound.Tag(), metadata.User, target)
	countedConn := bufio.NewInt64CounterConn(conn, readCounter, writeCounter)
	if release != nil {
		return &trackerConn{Conn: countedConn, onClose: release}
	}
	return countedConn
}

func (c *StatsTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	var target string
	if c.trafficEnabled.Load() {
		target = ExtractTarget(metadata.Domain, metadata.Destination)
	}
	readCounter, writeCounter, release := c.getReadCounters(metadata.Inbound, matchOutbound.Tag(), metadata.User, target)
	countedConn := bufio.NewInt64CounterPacketConn(conn, readCounter, nil, writeCounter, nil)
	if release != nil {
		return &trackerPacketConn{PacketConn: countedConn, onClose: release}
	}
	return countedConn
}

func (c *StatsTracker) GetStats() *[]model.Stats {
	c.access.Lock()
	defer c.access.Unlock()

	dt := time.Now().Unix()

	// Emit only directions that actually moved traffic; a zero-traffic row would
	// just bloat the stats table without changing any chart bucket.
	s := []model.Stats{}
	appendStat := func(resource, tag string, down, up int64) {
		if down > 0 {
			s = append(s, model.Stats{DateTime: dt, Resource: resource, Tag: tag, Direction: false, Traffic: down})
		}
		if up > 0 {
			s = append(s, model.Stats{DateTime: dt, Resource: resource, Tag: tag, Direction: true, Traffic: up})
		}
	}

	for inbound, counter := range c.inbounds {
		appendStat("inbound", inbound, counter.write.Swap(0), counter.read.Swap(0))
	}
	for outbound, counter := range c.outbounds {
		appendStat("outbound", outbound, counter.write.Swap(0), counter.read.Swap(0))
	}
	for user, counter := range c.users {
		down, up := counter.write.Swap(0), counter.read.Swap(0)
		appendStat("user", user, down, up)
		if down != 0 || up != 0 {
			d := c.drained[user]
			d.Up += up
			d.Down += down
			c.drained[user] = d
		}
	}
	return &s
}

// GetTargetStats drains traffic counted for each target site.
func (c *StatsTracker) GetTargetStats() *[]model.TargetStats {
	c.access.Lock()
	defer c.access.Unlock()

	if !c.trafficEnabled.Load() {
		if len(c.targets) > 0 {
			c.targets = make(map[string]TargetCounter)
			c.targetIdle = make(map[string]int)
		}
		return &[]model.TargetStats{}
	}

	s := []model.TargetStats{}
	dt := time.Now().Unix()

	for key, counter := range c.targets {
		down := counter.write.Swap(0)
		up := counter.read.Swap(0)
		if down > 0 || up > 0 {
			c.targetIdle[key] = 0
			var u, t string
			if idx := strings.IndexByte(key, 0); idx >= 0 {
				u = key[:idx]
				t = key[idx+1:]
			} else {
				t = key
			}
			s = append(s, model.TargetStats{
				DateTime: dt,
				User:     u,
				Target:   t,
				Up:       up,
				Down:     down,
			})
		} else if counter.active.Load() <= 0 {
			c.targetIdle[key]++
			if c.targetIdle[key] >= 6 {
				delete(c.targets, key)
				delete(c.targetIdle, key)
			}
		} else {
			c.targetIdle[key] = 0
		}
	}
	return &s
}

// SampleUsers reports what each user moved since the previous sample, and how
// long ago that was. It reads the counters without emptying them -- GetStats
// owns that, on its own schedule -- so a user's bytes since the last sample
// are its counter now, plus whatever GetStats drained in between, minus the
// counter as the last sample read it. Bytes added between the read here and a
// later drain are not lost: the drain books them, and the next sample counts
// them. Users that moved nothing are left out. The first sample measures from
// when the tracker was made or last Reset, when every counter was zero.
//
// A window shorter than minWindow is not closed: ok is false, nothing moves,
// and its bytes roll into the next sample. Dividing a few milliseconds' bytes
// by a floored window would show a speed nobody had; dividing by the real one
// would show a burst nobody sent.
func (c *StatsTracker) SampleUsers(now time.Time, minWindow time.Duration) (moved map[string]UserBytes, window time.Duration, ok bool) {
	c.access.Lock()
	defer c.access.Unlock()

	window = now.Sub(c.sampledAt)
	if window < minWindow {
		return nil, window, false
	}
	moved = make(map[string]UserBytes)
	for user, counter := range c.users {
		cur := UserBytes{Up: counter.read.Load(), Down: counter.write.Load()}
		prev, drained := c.sampled[user], c.drained[user]
		delta := UserBytes{
			Up:   cur.Up + drained.Up - prev.Up,
			Down: cur.Down + drained.Down - prev.Down,
		}
		if delta.Up != 0 || delta.Down != 0 {
			moved[user] = delta
		}
		c.sampled[user] = cur
	}
	clear(c.drained)
	c.sampledAt = now
	return moved, window, true
}

// RoutedFlow is sing-box 1.14's hook for tracking a TUN flow. Counters here are
// per inbound/outbound tag, which a flow does not add to; see ConnTracker.
func (c *StatsTracker) RoutedFlow(ctx context.Context, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) tun.FlowTracker {
	return nil
}
