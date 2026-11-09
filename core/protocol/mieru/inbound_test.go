package mieru

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"
	N "github.com/sagernet/sing/common/network"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

// echoRouter stands in for sing-box's router: every TCP connection is echoed
// back, and the metadata it arrived with is handed to the test.
type echoRouter struct {
	adapter.Router
	routed chan adapter.InboundContext
}

func (r *echoRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.routed <- metadata
	go func() {
		_, _ = io.Copy(conn, conn)
		conn.Close()
		if onClose != nil {
			onClose(nil)
		}
	}()
}

func (r *echoRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.routed <- metadata
	conn.Close()
	if onClose != nil {
		onClose(nil)
	}
}

func freePort(t *testing.T, network string) uint16 {
	t.Helper()
	if network == "udp" {
		conn, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

func startInbound(t *testing.T, transport string, users []User) (*Inbound, *echoRouter, uint16) {
	t.Helper()
	ctx := context.Background()
	logger := log.NewNOPFactory().NewLogger("mieru")
	router := &echoRouter{routed: make(chan adapter.InboundContext, 16)}
	network := "tcp"
	if transport == TransportUDP {
		network = "udp"
	}
	port := freePort(t, network)
	created, err := NewInbound(ctx, router, logger, "mieru-in", InboundOptions{
		ListenOptions: option.ListenOptions{
			Listen:     common.Ptr(badoption.Addr(netip.MustParseAddr("127.0.0.1"))),
			ListenPort: port,
		},
		Users:     users,
		Transport: transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	in := created.(*Inbound)
	if err = in.Start(adapter.StartStateStart, adapter.NewScope(ctx, logger)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in, router, port
}

func newClient(t *testing.T, transport string, port uint16, name string, password string) mieruclient.Client {
	t.Helper()
	protocol := mierupb.TransportProtocol_TCP
	if transport == TransportUDP {
		protocol = mierupb.TransportProtocol_UDP
	}
	client := mieruclient.NewClient()
	err := client.Store(&mieruclient.ClientConfig{Profile: &mierupb.ClientProfile{
		ProfileName: proto.String("test"),
		User:        &mierupb.User{Name: proto.String(name), Password: proto.String(password)},
		Servers: []*mierupb.ServerEndpoint{{
			IpAddress: proto.String("127.0.0.1"),
			PortBindings: []*mierupb.PortBinding{{
				Port:     proto.Int32(int32(port)),
				Protocol: protocol.Enum(),
			}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Stop() })
	return client
}

var destination = &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 443}

func dial(client mieruclient.Client) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.DialContext(ctx, destination)
}

// roundTrip sends a line through the echo router and reads it back.
func roundTrip(conn net.Conn) error {
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte("ping")); err != nil {
		return err
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if string(reply) != "ping" {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func expectRouted(t *testing.T, router *echoRouter, user string) {
	t.Helper()
	select {
	case metadata := <-router.routed:
		if metadata.User != user {
			t.Fatalf("routed as user %q, want %q", metadata.User, user)
		}
		if metadata.InboundType != TypeMieru || metadata.Inbound != "mieru-in" {
			t.Fatalf("routed from %s/%s", metadata.InboundType, metadata.Inbound)
		}
		if got := metadata.Destination.String(); got != destination.String() {
			t.Fatalf("destination %s, want %s", got, destination)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection never reached the router")
	}
}

// expectCut waits for a connection the server is meant to have closed.
//
// The deadline is judged by the clock, not by the error: a mieru session
// reports its read timeout as a plain error that is no net.Error, so a test
// asking err.Timeout() took every idle connection for a cut one.
func expectCut(t *testing.T, conn net.Conn) {
	t.Helper()
	const wait = 5 * time.Second
	start := time.Now()
	conn.SetReadDeadline(start.Add(wait))
	_, err := conn.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("connection still carries data")
	}
	if time.Since(start) >= wait-500*time.Millisecond {
		t.Fatalf("connection was not closed (read ended at the deadline: %v)", err)
	}
}

func TestMieruTCPRoutesAndRemovesUsers(t *testing.T) {
	users := []User{{Name: "alice", Password: "alice-pass"}, {Name: "bob", Password: "bob-pass"}}
	in, router, port := startInbound(t, TransportTCP, users)

	alice := newClient(t, TransportTCP, port, "alice", "alice-pass")
	aliceConn, err := dial(alice)
	if err != nil {
		t.Fatal(err)
	}
	defer aliceConn.Close()
	expectRouted(t, router, "alice")
	if err = roundTrip(aliceConn); err != nil {
		t.Fatal(err)
	}

	bob := newClient(t, TransportTCP, port, "bob", "bob-pass")
	bobConn, err := dial(bob)
	if err != nil {
		t.Fatal(err)
	}
	defer bobConn.Close()
	expectRouted(t, router, "bob")

	// Removing alice in place closes her underlay, and with it the session
	// she already had open, while bob's carries on.
	if err = in.UpdateUsers(users[1:]); err != nil {
		t.Fatal(err)
	}
	expectCut(t, aliceConn)
	if err = roundTrip(bobConn); err != nil {
		t.Fatalf("bob was disconnected by alice's removal: %v", err)
	}
	if conn, err := dial(alice); err == nil {
		conn.Close()
		t.Fatal("a removed user opened a new session")
	}

	// A kick cuts bob too, but he stays on the inbound and may reconnect.
	if r := in.KickUser("bob"); r.Cut == 0 {
		t.Fatalf("kick cut nothing: %+v", r)
	}
	expectCut(t, bobConn)
	again := newClient(t, TransportTCP, port, "bob", "bob-pass")
	conn, err := dial(again)
	if err != nil {
		t.Fatalf("kicked user could not reconnect: %v", err)
	}
	conn.Close()
}

func TestMieruWrongPasswordIsRefused(t *testing.T) {
	_, _, port := startInbound(t, TransportTCP, []User{{Name: "alice", Password: "alice-pass"}})
	client := newClient(t, TransportTCP, port, "alice", "not-her-pass")
	if conn, err := dial(client); err == nil {
		conn.Close()
		t.Fatal("wrong password was accepted")
	}
}

func TestMieruStartsWithoutUsers(t *testing.T) {
	in, router, port := startInbound(t, TransportTCP, nil)
	if err := in.UpdateUsers([]User{{Name: "carol", Password: "carol-pass"}}); err != nil {
		t.Fatal(err)
	}
	conn, err := dial(newClient(t, TransportTCP, port, "carol", "carol-pass"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expectRouted(t, router, "carol")
}

func TestMieruUDPRemovesUsers(t *testing.T) {
	users := []User{{Name: "alice", Password: "alice-pass"}}
	in, router, port := startInbound(t, TransportUDP, users)

	conn, err := dial(newClient(t, TransportUDP, port, "alice", "alice-pass"))
	if err != nil {
		t.Fatal(err)
	}
	expectRouted(t, router, "alice")
	if err = roundTrip(conn); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if err = in.UpdateUsers(nil); err != nil {
		t.Fatal(err)
	}
	if conn, err := dial(newClient(t, TransportUDP, port, "alice", "alice-pass")); err == nil {
		conn.Close()
		t.Fatal("a removed user opened a new UDP session")
	}
}

func TestMieruRejectsBadOptions(t *testing.T) {
	ctx := context.Background()
	logger := log.NewNOPFactory().NewLogger("mieru")
	for name, options := range map[string]InboundOptions{
		"transport": {ListenOptions: option.ListenOptions{ListenPort: 1}, Transport: "QUIC"},
		"port":      {Transport: TransportTCP},
		"no name":   {ListenOptions: option.ListenOptions{ListenPort: 1}, Users: []User{{Password: "x"}}},
		"duplicate": {ListenOptions: option.ListenOptions{ListenPort: 1}, Users: []User{{Name: "a", Password: "x"}, {Name: "a", Password: "y"}}},
		"pattern":   {ListenOptions: option.ListenOptions{ListenPort: 1}, TrafficPattern: "%%%"},
	} {
		if _, err := NewInbound(ctx, nil, logger, "t", options); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
