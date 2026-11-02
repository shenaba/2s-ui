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
			target = dest.Addr.String()
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

type StatsTracker struct {
	access     sync.Mutex
	inbounds   map[string]Counter
	outbounds  map[string]Counter
	users      map[string]Counter
	targets    map[string]Counter // key: user + "\x00" + target
	targetIdle map[string]int

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
	return &StatsTracker{
		inbounds:   make(map[string]Counter),
		outbounds:  make(map[string]Counter),
		users:      make(map[string]Counter),
		targets:    make(map[string]Counter),
		targetIdle: make(map[string]int),
		sampled:    make(map[string]UserBytes),
		drained:    make(map[string]UserBytes),
		// Every counter starts at zero here, so this is where the first
		// sample's window starts.
		sampledAt: time.Now(),
	}
}

func (c *StatsTracker) Reset() {
	c.access.Lock()
	defer c.access.Unlock()
	c.inbounds = make(map[string]Counter)
	c.outbounds = make(map[string]Counter)
	c.users = make(map[string]Counter)
	c.targets = make(map[string]Counter)
	c.targetIdle = make(map[string]int)
	c.sampled = make(map[string]UserBytes)
	c.drained = make(map[string]UserBytes)
	c.sampledAt = time.Now()
}

func (c *StatsTracker) getReadCounters(inbound string, outbound string, user string, target ...string) ([]*atomic.Int64, []*atomic.Int64) {
	var readCounter []*atomic.Int64
	var writeCounter []*atomic.Int64
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
	if len(target) > 0 && target[0] != "" {
		t := target[0]
		key := user + "\x00" + t
		counter := c.loadOrCreateCounter(&c.targets, key)
		readCounter = append(readCounter, counter.read)
		writeCounter = append(writeCounter, counter.write)
	}
	return readCounter, writeCounter
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

func (c *StatsTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	target := ExtractTarget(metadata.Domain, metadata.Destination)
	readCounter, writeCounter := c.getReadCounters(metadata.Inbound, matchOutbound.Tag(), metadata.User, target)
	return bufio.NewInt64CounterConn(conn, readCounter, writeCounter)
}

func (c *StatsTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	target := ExtractTarget(metadata.Domain, metadata.Destination)
	readCounter, writeCounter := c.getReadCounters(metadata.Inbound, matchOutbound.Tag(), metadata.User, target)
	return bufio.NewInt64CounterPacketConn(conn, readCounter, nil, writeCounter, nil)
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
		} else {
			c.targetIdle[key]++
			if c.targetIdle[key] >= 6 {
				delete(c.targets, key)
				delete(c.targetIdle, key)
			}
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
