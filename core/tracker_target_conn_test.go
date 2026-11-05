package core

import (
	"net"
	"testing"

	"github.com/sagernet/sing/common/bufio"
	N "github.com/sagernet/sing/common/network"
)

// sing's copy loop unwraps a routed conn down to its counters (and from there
// to the socket, for splice). The wrapper that releases a target counter on
// Close sits on top of the counter conn, so it has to be see-through, or every
// routed connection falls back to a plain read/write loop.
func TestTrackerConnKeepsCopyFastPath(t *testing.T) {
	c := NewStatsTracker()
	read, write, release := c.getReadCounters("in", "out", "alice", "example.com")
	if release == nil {
		t.Fatal("no release func for a target counter")
	}
	local, remote := net.Pipe()
	defer remote.Close()
	wrapped := &trackerConn{Conn: bufio.NewInt64CounterConn(local, read, write), onClose: release}

	_, readFuncs := N.UnwrapCountReader(wrapped, nil)
	_, writeFuncs := N.UnwrapCountWriter(wrapped, nil)
	if len(readFuncs) != len(read) || len(writeFuncs) != len(write) {
		t.Fatalf("unwrap found %d read / %d write counters, want %d / %d",
			len(readFuncs), len(writeFuncs), len(read), len(write))
	}
	for _, f := range readFuncs {
		f(10)
	}
	key := "alice\x00example.com"
	if got := c.targets[key].read.Load(); got != 10 {
		t.Errorf("target read counter = %d after unwrapped count, want 10", got)
	}

	wrapped.Close()
	wrapped.Close()
	if got := c.targets[key].active.Load(); got != 0 {
		t.Errorf("active = %d after a double Close, want 0", got)
	}
}

func TestTrackerPacketConnKeepsCopyFastPath(t *testing.T) {
	c := NewStatsTracker()
	read, write, release := c.getReadCounters("in", "out", "alice", "example.com")
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback UDP: %v", err)
	}
	counted := bufio.NewInt64CounterPacketConn(bufio.NewPacketConn(udp), read, nil, write, nil)
	wrapped := &trackerPacketConn{PacketConn: counted, onClose: release}

	_, readFuncs := N.UnwrapCountPacketReader(wrapped, nil)
	_, writeFuncs := N.UnwrapCountPacketWriter(wrapped, nil)
	if len(readFuncs) != len(read) || len(writeFuncs) != len(write) {
		t.Fatalf("unwrap found %d read / %d write counters, want %d / %d",
			len(readFuncs), len(writeFuncs), len(read), len(write))
	}

	wrapped.Close()
	if got := c.targets["alice\x00example.com"].active.Load(); got != 0 {
		t.Errorf("active = %d after Close, want 0", got)
	}
}
