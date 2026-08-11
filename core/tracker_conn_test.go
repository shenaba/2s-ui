package core

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return a
}

func TestNormalizeSrc(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"v4 unchanged", "1.2.3.4", "1.2.3.4"},
		// Both spellings of one v4 client have to collapse, or a single device
		// eats two slots depending on which path it arrived by.
		{"v4-mapped collapses to v4", "::ffff:1.2.3.4", "1.2.3.4"},
		// SLAAC privacy extensions rotate the host bits, so counting full
		// addresses would let one device exhaust any limit by itself.
		{"v6 masked to /64", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::"},
		{"v6 already a prefix", "2001:db8:1:2::", "2001:db8:1:2::"},
		{"v6 loopback", "::1", "::"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeSrc(mustAddr(t, c.in))
			if got.String() != c.want {
				t.Errorf("normalizeSrc(%s) = %s, want %s", c.in, got, c.want)
			}
		})
	}

	t.Run("invalid address stays invalid", func(t *testing.T) {
		if normalizeSrc(netip.Addr{}).IsValid() {
			t.Error("an invalid address normalized into a valid one")
		}
	})
}

// track registers a connection directly so the test controls the timestamps,
// which RoutedConnection reads off the real clock.
func track(tr *ConnTracker, user, ip string, createdAt, lastActive int64, t *testing.T) *ConnectionInfo {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	info := &ConnectionInfo{
		ID:        tr.generateConnectionID(),
		Conn:      client,
		Inbound:   "in",
		User:      user,
		Type:      "tcp",
		Source:    normalizeSrc(mustAddr(t, ip)),
		CreatedAt: createdAt,
	}
	info.lastActive.Store(lastActive)
	tr.trackConnection(info.ID, info)
	return info
}

func TestUserIPs(t *testing.T) {
	tr := NewConnTracker()
	track(tr, "alice", "1.1.1.1", 100, 900, t)
	// Same IP, second connection: the window has to span both, taking the
	// earliest start and the latest activity.
	track(tr, "alice", "1.1.1.1", 50, 300, t)
	track(tr, "alice", "2.2.2.2", 200, 400, t)
	track(tr, "bob", "3.3.3.3", 10, 20, t)
	// Domain-routed connections carry no source and must not appear.
	noSource := &ConnectionInfo{ID: "no-source", Inbound: "in", User: "alice", Type: "tcp"}
	tr.trackConnection(noSource.ID, noSource)

	users, now := tr.UserIPs()
	// Zero is a legitimate reading: on Windows the monotonic clock advances in
	// ~0.5ms ticks and this test finishes inside one. Nothing may require the
	// tracker's timestamps to be distinct -- ordering falls back to the address.
	if now < 0 {
		t.Errorf("now = %d, want a non-negative monotonic reading", now)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want alice and bob", len(users))
	}

	alice := users["alice"]
	if len(alice) != 2 {
		t.Fatalf("alice has %d IPs, want 2", len(alice))
	}
	w := alice[mustAddr(t, "1.1.1.1")]
	if w.FirstSeen != 50 {
		t.Errorf("FirstSeen = %d, want the earlier connection's 50", w.FirstSeen)
	}
	if w.LastSeen != 900 {
		t.Errorf("LastSeen = %d, want the later activity 900", w.LastSeen)
	}

	if len(users["bob"]) != 1 {
		t.Errorf("bob has %d IPs, want 1", len(users["bob"]))
	}
}

func TestCloseConnByUserIPs(t *testing.T) {
	tr := NewConnTracker()
	doomed := track(tr, "alice", "1.1.1.1", 100, 100, t)
	kept := track(tr, "alice", "2.2.2.2", 100, 100, t)
	// Same IP, different client: matching on the IP alone would take this one
	// down with it.
	other := track(tr, "bob", "1.1.1.1", 100, 100, t)

	closed := tr.CloseConnByUserIPs(map[string]map[netip.Addr]struct{}{
		"alice": {mustAddr(t, "1.1.1.1"): {}},
	})
	if closed != 1 {
		t.Errorf("closed %d connections, want 1", closed)
	}

	tr.access.Lock()
	remaining := len(tr.connections)
	_, doomedStillTracked := tr.connections[doomed.ID]
	_, keptStillTracked := tr.connections[kept.ID]
	_, otherStillTracked := tr.connections[other.ID]
	tr.access.Unlock()

	if remaining != 2 {
		t.Errorf("%d connections remain, want 2", remaining)
	}
	if doomedStillTracked {
		t.Error("the dropped connection is still tracked")
	}
	if !keptStillTracked {
		t.Error("another IP of the same client was dropped")
	}
	if !otherStillTracked {
		t.Error("the same IP under a different client was dropped")
	}

	// The connection really is closed, not just forgotten.
	if _, err := doomed.Conn.Read(make([]byte, 1)); err == nil {
		t.Error("the dropped connection is still readable")
	}
}

// The data plane calls touch on every read and write, so it has to cost nothing
// on a panel where no client has an IP limit -- lastActive has no reader there.
func TestTouchRespectsIPLimitActive(t *testing.T) {
	tr := NewConnTracker()
	info := &ConnectionInfo{ID: "x"}
	info.lastActive.Store(-1)

	SetIPLimitActive(false)
	tr.touch(info)
	if info.lastActive.Load() != -1 {
		t.Error("lastActive was stamped while no client has an IP limit")
	}

	SetIPLimitActive(true)
	defer SetIPLimitActive(false)
	tr.touch(info)
	if info.lastActive.Load() < 0 {
		t.Error("lastActive was not stamped with a limit active")
	}
}

func TestConnGate(t *testing.T) {
	newMetadata := func(user, ip string) adapter.InboundContext {
		return adapter.InboundContext{
			Inbound: "in",
			User:    user,
			Source:  M.Socksaddr{Addr: mustAddr(t, ip), Port: 1080},
		}
	}
	routeOne := func(tr *ConnTracker, metadata adapter.InboundContext) net.Conn {
		client, server := net.Pipe()
		t.Cleanup(func() {
			client.Close()
			server.Close()
		})
		return tr.RoutedConnection(context.Background(), client, metadata, nil, nil)
	}

	t.Run("denied connection is closed and never tracked", func(t *testing.T) {
		SetConnGate(func(user string, source netip.Addr) bool {
			return !(user == "alice" && source == mustAddr(t, "1.1.1.1"))
		})
		defer SetConnGate(nil)

		tr := NewConnTracker()
		denied := routeOne(tr, newMetadata("alice", "1.1.1.1"))
		if _, err := denied.Read(make([]byte, 1)); err == nil {
			t.Error("a denied connection was left open")
		}
		routeOne(tr, newMetadata("alice", "2.2.2.2"))
		routeOne(tr, newMetadata("bob", "1.1.1.1"))

		users, _ := tr.UserIPs()
		if _, banned := users["alice"][mustAddr(t, "1.1.1.1")]; banned {
			t.Error("the denied IP was tracked anyway")
		}
		if len(users["alice"]) != 1 {
			t.Errorf("alice has %d IPs, want only the allowed one", len(users["alice"]))
		}
		if len(users["bob"]) != 1 {
			t.Error("the gate leaked across clients")
		}
	})

	// dns and direct inbounds route without authenticating, so a gate keyed on
	// the empty user would deny all of them at once.
	t.Run("connections without a user bypass the gate", func(t *testing.T) {
		SetConnGate(func(string, netip.Addr) bool { return false })
		defer SetConnGate(nil)

		tr := NewConnTracker()
		conn := routeOne(tr, newMetadata("", "1.1.1.1"))
		if _, ok := conn.(*wrappedConn); !ok {
			t.Fatalf("userless connection was refused, got %T", conn)
		}
		tr.access.Lock()
		tracked := len(tr.connections)
		tr.access.Unlock()
		if tracked != 1 {
			t.Errorf("%d connections tracked, want the userless one", tracked)
		}
	})

	t.Run("no gate installed allows everything", func(t *testing.T) {
		SetConnGate(nil)
		tr := NewConnTracker()
		routeOne(tr, newMetadata("alice", "1.1.1.1"))
		users, _ := tr.UserIPs()
		if len(users["alice"]) != 1 {
			t.Error("a connection was refused with no gate installed")
		}
	})
}
