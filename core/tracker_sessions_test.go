package core

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

// routeSession routes one TCP connection through tr and returns the wrapped
// end plus the peer the test talks to.
func routeSession(t *testing.T, tr *ConnTracker, metadata adapter.InboundContext) (net.Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return tr.RoutedConnection(context.Background(), client, metadata, nil, nil), server
}

func TestSessionsSnapshot(t *testing.T) {
	tr := NewConnTracker()
	conn, peer := routeSession(t, tr, adapter.InboundContext{
		Inbound: "vless-in",
		User:    "alice",
		// A v6 source: the list must show the address the client used, port
		// included, not the /64 identity the IP limit counts.
		Source:        M.Socksaddr{Addr: mustAddr(t, "2001:db8::1234"), Port: 51022},
		Destination:   M.Socksaddr{Addr: mustAddr(t, "104.18.33.45"), Port: 443},
		Domain:        "api.example.com",
		RouteOutbound: "warp",
		RouteRule:     "domain_suffix=example.com => route(warp)",
	})

	// up is what the client sent, down what it was sent.
	go func() {
		_, _ = peer.Write([]byte("hello"))
		_, _ = io.ReadFull(peer, make([]byte, 3))
	}()
	if _, err := io.ReadFull(conn, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}

	sessions := tr.Sessions(nil)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.Source != "[2001:db8::1234]:51022" {
		t.Errorf("source = %q, want the raw address with its port", s.Source)
	}
	if s.Destination != "104.18.33.45:443" || s.Domain != "api.example.com" {
		t.Errorf("destination = %q / %q", s.Destination, s.Domain)
	}
	if s.Outbound != "warp" || s.Rule == "" || s.Network != "tcp" {
		t.Errorf("outbound/rule/network = %q / %q / %q", s.Outbound, s.Rule, s.Network)
	}
	if s.Upload != 5 || s.Download != 3 {
		t.Errorf("up/down = %d/%d, want 5/3", s.Upload, s.Download)
	}
	if s.CreatedAt == 0 {
		t.Error("createdAt is not a wall-clock time")
	}
}

// The client's own name is the domain when nothing was sniffed.
func TestSessionsDomainFallsBackToFqdn(t *testing.T) {
	tr := NewConnTracker()
	routeSession(t, tr, adapter.InboundContext{
		User:        "alice",
		Source:      M.Socksaddr{Addr: mustAddr(t, "1.1.1.1"), Port: 1},
		Destination: M.ParseSocksaddrHostPort("www.example.org", 443),
	})
	if got := tr.Sessions(nil)[0].Domain; got != "www.example.org" {
		t.Errorf("domain = %q", got)
	}
}

func TestSessionsFilter(t *testing.T) {
	tr := NewConnTracker()
	for _, user := range []string{"alice", "alice", "bob"} {
		routeSession(t, tr, adapter.InboundContext{
			User:   user,
			Source: M.Socksaddr{Addr: mustAddr(t, "1.1.1.1"), Port: 1},
		})
	}
	got := tr.Sessions(func(info *ConnectionInfo) bool { return info.User == "alice" })
	if len(got) != 2 {
		t.Errorf("got %d sessions for alice, want 2", len(got))
	}
}

func TestCloseConnByUser(t *testing.T) {
	tr := NewConnTracker()
	alice, _ := routeSession(t, tr, adapter.InboundContext{User: "alice", Source: M.Socksaddr{Addr: mustAddr(t, "1.1.1.1"), Port: 1}})
	routeSession(t, tr, adapter.InboundContext{User: "bob", Source: M.Socksaddr{Addr: mustAddr(t, "2.2.2.2"), Port: 1}})

	if n := tr.CloseConnByUser("alice"); n != 1 {
		t.Errorf("closed %d, want 1", n)
	}
	if _, err := alice.Read(make([]byte, 1)); err == nil {
		t.Error("alice's connection is still open")
	}
	if left := tr.Sessions(nil); len(left) != 1 || left[0].User != "bob" {
		t.Errorf("left = %+v, want only bob", left)
	}
	if n := tr.CloseConnByUser(""); n != 0 {
		t.Error("an empty name matched the userless connections")
	}
}
