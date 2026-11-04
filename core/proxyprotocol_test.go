package core

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/core/proxyheader"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/net/proxy"
)

const proxyTestUUID = "2dd61d93-75d8-4da4-ac0e-6aece7eac365"

// headerDialer is a front on this host: it opens the connection, sends the
// PROXY header first, and leaves the rest to whoever dialled.
type headerDialer struct {
	header string
}

func (d headerDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", destination.String())
	if err != nil {
		return nil, err
	}
	if _, err = conn.Write([]byte(d.header)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d headerDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func echoServer(t *testing.T) M.Socksaddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return M.SocksaddrFromNet(l.Addr())
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// proxiedVLESSCore starts a core with one vless inbound that reads PROXY
// headers, with transport set when it is not nil.
func proxiedVLESSCore(t *testing.T, transport map[string]any) (*Core, M.Socksaddr) {
	t.Helper()
	var c *Core
	var err error
	var port int
	// The probed port is free only until the probe closes.
	for attempt := 0; attempt < 3; attempt++ {
		port = freeTCPPort(t)
		inbound := map[string]any{
			"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": port,
			"proxy_protocol": true,
			"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
		}
		if transport != nil {
			inbound["transport"] = transport
		}
		var raw []byte
		raw, err = json.Marshal(map[string]any{
			"log":       map[string]any{"disabled": true},
			"inbounds":  []any{inbound},
			"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		c = NewCore()
		if err = c.Start(raw); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c, M.ParseSocksaddrHostPort("127.0.0.1", uint16(port))
}

// ping sends one message through a vless connection and waits for the echo.
func ping(t *testing.T, conn net.Conn, target M.Socksaddr) {
	t.Helper()
	client, err := vless.NewClient(proxyTestUUID, "", logger.NOP())
	if err != nil {
		t.Fatal(err)
	}
	conn, err = client.DialConn(conn, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err = io.ReadFull(conn, buf); err != nil {
		t.Fatalf("no echo: %v", err)
	}
}

func sessionSource(t *testing.T, c *Core) string {
	t.Helper()
	sessions := c.GetInstance().ConnTracker().Sessions(func(info *ConnectionInfo) bool { return info.User == "u" })
	if len(sessions) != 1 {
		t.Fatalf("%d sessions for the user, want 1", len(sessions))
	}
	return sessions[0].Source
}

// The source the core records -- what the online list and the per-client IP
// limit run on -- is the client the header names, not the front (#260). Both
// listeners a copy has are covered: its own, and the one a transport serves.
func TestProxyProtocolNamesTheClient(t *testing.T) {
	target := echoServer(t)
	for _, tc := range []struct {
		name      string
		transport map[string]any
	}{
		{"no transport", nil},
		{"websocket", map[string]any{"type": "ws", "path": "/pp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, server := proxiedVLESSCore(t, tc.transport)
			front := headerDialer{header: "PROXY TCP4 203.0.113.7 127.0.0.1 51000 " + strconv.Itoa(int(server.Port)) + "\r\n"}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var conn net.Conn
			var err error
			if tc.transport == nil {
				conn, err = front.DialContext(ctx, "tcp", server)
			} else {
				var ws interface {
					DialContext(context.Context) (net.Conn, error)
				}
				ws, err = v2raywebsocket.NewClient(ctx, front, server, option.V2RayWebsocketOptions{Path: "/pp"}, nil)
				if err == nil {
					conn, err = ws.DialContext(ctx)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			ping(t, conn, target)
			if got := sessionSource(t, c); got != "203.0.113.7:51000" {
				t.Fatalf("core recorded the source as %s, want the client 203.0.113.7:51000", got)
			}
		})
	}
}

// A connection from this host that does not start with a header is refused
// rather than recorded as 127.0.0.1: a front that forgot to send one has to
// show up, not quietly fold every client into one address.
func TestProxyProtocolRefusesALocalConnectionWithoutAHeader(t *testing.T) {
	target := echoServer(t)
	c, server := proxiedVLESSCore(t, nil)
	conn, err := net.Dial("tcp", server.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client, err := vless.NewClient(proxyTestUUID, "", logger.NOP())
	if err != nil {
		t.Fatal(err)
	}
	vconn, err := client.DialConn(conn, target)
	if err != nil {
		t.Fatal(err)
	}
	vconn.SetDeadline(time.Now().Add(5 * time.Second))
	vconn.Write([]byte("ping"))
	if _, err = io.ReadFull(vconn, make([]byte, 4)); err == nil {
		t.Fatal("a local connection without a header was served")
	}
	if sessions := c.GetInstance().ConnTracker().Sessions(nil); len(sessions) != 0 {
		t.Fatalf("%d sessions recorded, want none", len(sessions))
	}
}

// A proxy user asks the core's direct outbound for the PROXY-enabled inbound
// on loopback and sends a header naming an address of their choosing. The
// connection does arrive from this host, but the core dialed it, so the
// header is refused rather than believed (#260). With TCP Fast Open on the
// direct outbound too: such a dial has no socket until its first write, which
// would carry the forged header.
func TestProxyProtocolRefusesAClientLoopingThroughTheCore(t *testing.T) {
	for _, tfo := range []bool{false, true} {
		t.Run("tcp_fast_open="+strconv.FormatBool(tfo), func(t *testing.T) {
			testLoopThroughTheCore(t, tfo)
		})
	}
}

func testLoopThroughTheCore(t *testing.T, tfo bool) {
	target := echoServer(t)
	var c *Core
	var err error
	var mixedPort, ppPort int
	for attempt := 0; attempt < 3; attempt++ {
		mixedPort, ppPort = freeTCPPort(t), freeTCPPort(t)
		var raw []byte
		raw, err = json.Marshal(map[string]any{
			"log": map[string]any{"disabled": true},
			"inbounds": []any{
				map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": mixedPort},
				map[string]any{
					"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": ppPort,
					"proxy_protocol": true,
					"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
				},
			},
			"outbounds": []any{map[string]any{"type": "direct", "tag": "direct", "tcp_fast_open": tfo}},
		})
		if err != nil {
			t.Fatal(err)
		}
		c = NewCore()
		if err = c.Start(raw); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })

	socks, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}

	// Control: the direct outbound does reach loopback, so a refusal below is
	// the guard's and not a route that never got there.
	echo, err := socks.Dial("tcp", target.String())
	if err != nil {
		t.Fatal(err)
	}
	echo.SetDeadline(time.Now().Add(5 * time.Second))
	echo.Write([]byte("ping"))
	if _, err = io.ReadFull(echo, make([]byte, 4)); err != nil {
		t.Fatalf("control: loopback through the direct outbound failed: %v", err)
	}
	echo.Close()

	conn, err := socks.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ppPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.Write([]byte("PROXY TCP4 198.51.100.9 127.0.0.1 51000 " + strconv.Itoa(ppPort) + "\r\n")); err != nil {
		t.Fatal(err)
	}
	client, err := vless.NewClient(proxyTestUUID, "", logger.NOP())
	if err != nil {
		t.Fatal(err)
	}
	vconn, err := client.DialConn(conn, target)
	if err != nil {
		t.Fatal(err)
	}
	vconn.SetDeadline(time.Now().Add(5 * time.Second))
	vconn.Write([]byte("ping"))
	if _, err = io.ReadFull(vconn, make([]byte, 4)); err == nil {
		t.Fatal("a header sent through the core's own outbound was believed")
	}
	if sessions := c.GetInstance().ConnTracker().Sessions(func(info *ConnectionInfo) bool { return info.User == "u" }); len(sessions) != 0 {
		t.Fatalf("the looped connection was recorded as %s", sessions[0].Source)
	}
}

// testFront stands in for nginx stream with proxy_protocol on: it forwards
// every connection to server, after a header naming 203.0.113.7:51000.
func testFront(t *testing.T, server M.Socksaddr) M.Socksaddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	header := "PROXY TCP4 203.0.113.7 127.0.0.1 51000 " + strconv.Itoa(int(server.Port)) + "\r\n"
	go func() {
		for {
			client, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				upstream, err := net.Dial("tcp", server.String())
				if err != nil {
					return
				}
				defer upstream.Close()
				if _, err = upstream.Write([]byte(header)); err != nil {
					return
				}
				go io.Copy(upstream, client)
				io.Copy(client, upstream)
			}()
		}
	}()
	return M.SocksaddrFromNet(l.Addr())
}

// startTestCore starts a core from the config build returns, building it
// again on fresh ports when a probed one was taken in between.
func startTestCore(t *testing.T, build func() map[string]any) *Core {
	t.Helper()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var raw []byte
		raw, err = json.Marshal(build())
		if err != nil {
			t.Fatal(err)
		}
		c := NewCore()
		if err = c.Start(raw); err == nil {
			t.Cleanup(func() { _ = c.Stop() })
			return c
		}
		skipIfFeatureMissing(t, err)
	}
	t.Fatalf("start core: %v", err)
	return nil
}

// Every copy that reads PROXY headers, on both of its listeners: a client core
// dials a server core through a front that sends a header, and the server must
// record the client the header named. Driven through sing-box's own outbounds,
// so each protocol is spoken by the implementation users run.
func TestProxyProtocolEveryCopy(t *testing.T) {
	target := echoServer(t)
	certPath, keyPath := writeTestCert(t)
	ws := map[string]any{"type": "ws", "path": "/pp"}
	serverTLS := map[string]any{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
	clientTLS := map[string]any{"enabled": true, "server_name": "t.example.com", "insecure": true}
	cases := []struct {
		name      string
		inbound   map[string]any
		outbound  map[string]any
		transport map[string]any
		tls       bool
	}{
		{name: "vless", inbound: map[string]any{"type": "vless", "users": []any{map[string]any{"name": "u", "uuid": proxyTestUUID}}},
			outbound: map[string]any{"type": "vless", "uuid": proxyTestUUID}},
		{name: "vless ws", transport: ws, inbound: map[string]any{"type": "vless", "users": []any{map[string]any{"name": "u", "uuid": proxyTestUUID}}},
			outbound: map[string]any{"type": "vless", "uuid": proxyTestUUID}},
		{name: "vmess", inbound: map[string]any{"type": "vmess", "users": []any{map[string]any{"name": "u", "uuid": proxyTestUUID}}},
			outbound: map[string]any{"type": "vmess", "uuid": proxyTestUUID, "security": "auto"}},
		{name: "vmess ws", transport: ws, inbound: map[string]any{"type": "vmess", "users": []any{map[string]any{"name": "u", "uuid": proxyTestUUID}}},
			outbound: map[string]any{"type": "vmess", "uuid": proxyTestUUID, "security": "auto"}},
		{name: "trojan", inbound: map[string]any{"type": "trojan", "users": []any{map[string]any{"name": "u", "password": "p"}}},
			outbound: map[string]any{"type": "trojan", "password": "p"}},
		{name: "trojan ws", transport: ws, inbound: map[string]any{"type": "trojan", "users": []any{map[string]any{"name": "u", "password": "p"}}},
			outbound: map[string]any{"type": "trojan", "password": "p"}},
		{name: "anytls", tls: true, inbound: map[string]any{"type": "anytls", "users": []any{map[string]any{"name": "u", "password": "p"}}},
			outbound: map[string]any{"type": "anytls", "password": "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var serverPort int
			server := startTestCore(t, func() map[string]any {
				serverPort = freeTCPPort(t)
				inbound := map[string]any{"tag": "pp-in", "listen": "127.0.0.1", "listen_port": serverPort, "proxy_protocol": true}
				for key, value := range tc.inbound {
					inbound[key] = value
				}
				if tc.transport != nil {
					inbound["transport"] = tc.transport
				}
				if tc.tls {
					inbound["tls"] = serverTLS
				}
				return map[string]any{
					"log":       map[string]any{"disabled": true},
					"inbounds":  []any{inbound},
					"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
				}
			})
			// What the dial guard is computed from (refreshProxyHeaders).
			if !server.readsProxyHeaders() {
				t.Fatal("the core does not report its PROXY-enabled inbound")
			}
			front := testFront(t, M.ParseSocksaddrHostPort("127.0.0.1", uint16(serverPort)))

			var mixedPort int
			client := startTestCore(t, func() map[string]any {
				mixedPort = freeTCPPort(t)
				outbound := map[string]any{"tag": "out", "server": "127.0.0.1", "server_port": front.Port}
				for key, value := range tc.outbound {
					outbound[key] = value
				}
				if tc.transport != nil {
					outbound["transport"] = tc.transport
				}
				if tc.tls {
					outbound["tls"] = clientTLS
				}
				return map[string]any{
					"log":       map[string]any{"disabled": true},
					"inbounds":  []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": mixedPort}},
					"outbounds": []any{outbound},
				}
			})
			if client.readsProxyHeaders() {
				t.Fatal("a core with no PROXY-enabled inbound reports one")
			}

			socks, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), nil, proxy.Direct)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := socks.Dial("tcp", target.String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = conn.Write([]byte("ping")); err != nil {
				t.Fatal(err)
			}
			if _, err = io.ReadFull(conn, make([]byte, 4)); err != nil {
				t.Fatalf("no echo through %s behind the front: %v", tc.name, err)
			}
			if got := sessionSource(t, server); got != "203.0.113.7:51000" {
				t.Fatalf("core recorded the source as %s, want the client 203.0.113.7:51000", got)
			}
		})
	}
}

// connectRelay is a relay on this host that the core does not run -- a gost
// or a WARP client an outbound points at -- speaking HTTP CONNECT. It opens
// the onward connection itself, where the core cannot see it.
func connectRelay(t *testing.T) M.Socksaddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			client, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				request, err := http.ReadRequest(bufio.NewReader(client))
				if err != nil || request.Method != http.MethodConnect {
					return
				}
				upstream, err := net.Dial("tcp", request.Host)
				if err != nil {
					io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				defer upstream.Close()
				if _, err = io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
					return
				}
				go io.Copy(upstream, client)
				io.Copy(client, upstream)
			}()
		}
	}()
	return M.SocksaddrFromNet(l.Addr())
}

// A client handed to a relay on this host cannot loop back into a
// PROXY-enabled inbound through it either: the relay's connection would arrive
// from loopback, and the core never dials it, so the core refuses to hand it
// that port at all.
func TestProxyProtocolRefusesAClientLoopingThroughARelay(t *testing.T) {
	relay := connectRelay(t)
	direct := map[string]any{"type": "direct", "tag": "direct"}
	http := map[string]any{"type": "http", "tag": "relay", "server": "127.0.0.1", "server_port": relay.Port}
	t.Run("relay outbound", func(t *testing.T) {
		testLoopThroughARelay(t, relay, []any{http})
	})
	// The router dials what the selector has picked: the guard must judge
	// that, not the selector.
	t.Run("relay picked by a selector", func(t *testing.T) {
		selector := map[string]any{"type": "selector", "tag": "pick", "outbounds": []any{"relay", "direct"}, "default": "relay"}
		testLoopThroughARelay(t, relay, []any{selector, http, direct})
	})
}

func testLoopThroughARelay(t *testing.T, relay M.Socksaddr, outbounds []any) {
	target := echoServer(t)
	var mixedPort, ppPort int
	c := startTestCore(t, func() map[string]any {
		mixedPort, ppPort = freeTCPPort(t), freeTCPPort(t)
		return map[string]any{
			"log": map[string]any{"disabled": true},
			"inbounds": []any{
				map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": mixedPort},
				map[string]any{
					"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": ppPort,
					"proxy_protocol": true,
					"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
				},
			},
			"outbounds": outbounds,
		}
	})
	socks, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}

	// Control: the relay does reach loopback.
	echo, err := socks.Dial("tcp", target.String())
	if err != nil {
		t.Fatal(err)
	}
	echo.SetDeadline(time.Now().Add(5 * time.Second))
	echo.Write([]byte("ping"))
	if _, err = io.ReadFull(echo, make([]byte, 4)); err != nil {
		t.Fatalf("control: loopback through the relay failed: %v", err)
	}
	echo.Close()

	// Refused at the dial: the relay is never asked to connect there, so no
	// header the client sends can reach the inbound.
	for _, host := range []string{"127.0.0.1", "localhost", "0.0.0.0"} {
		conn, err := socks.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(ppPort)))
		if err == nil {
			conn.Close()
			t.Fatalf("a relay on this host was handed %s:%d, a PROXY-enabled inbound's port", host, ppPort)
		}
	}
	if sessions := c.GetInstance().ConnTracker().Sessions(func(info *ConnectionInfo) bool { return info.User == "u" }); len(sessions) != 0 {
		t.Fatalf("a relayed connection was recorded as %s", sessions[0].Source)
	}
}

// A client asking the direct outbound for a PROXY-enabled inbound by name is
// dialed by the core itself, and recorded exactly: the front's own
// connections to that inbound, at the same time, still get through.
func TestProxyProtocolDirectDialByNameLeavesTheFrontAlone(t *testing.T) {
	target := echoServer(t)
	var mixedPort, ppPort int
	c := startTestCore(t, func() map[string]any {
		mixedPort, ppPort = freeTCPPort(t), freeTCPPort(t)
		return map[string]any{
			"log": map[string]any{"disabled": true},
			"inbounds": []any{
				map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": mixedPort},
				map[string]any{
					"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": ppPort,
					"proxy_protocol": true,
					"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
				},
			},
			"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		}
	})
	socks, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	// Held open, never sending a header: it would only time out.
	loop, err := socks.Dial("tcp", net.JoinHostPort("localhost", strconv.Itoa(ppPort)))
	if err == nil {
		defer loop.Close()
	}

	front := headerDialer{header: "PROXY TCP4 203.0.113.7 127.0.0.1 51000 " + strconv.Itoa(ppPort) + "\r\n"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := front.DialContext(ctx, "tcp", M.ParseSocksaddrHostPort("127.0.0.1", uint16(ppPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ping(t, conn, target)
	if got := sessionSource(t, c); got != "203.0.113.7:51000" {
		t.Fatalf("core recorded the source as %s, want the client 203.0.113.7:51000", got)
	}
}

// The dial guard follows the inbounds the core runs, however they change: it
// goes off when the last PROXY-enabled one is removed or the core stops, and
// on again when one is added.
func TestProxyProtocolGuardFollowsTheInbounds(t *testing.T) {
	if proxyheader.InUse() {
		t.Fatal("the guard is on before any core runs")
	}
	c, addr := proxiedVLESSCore(t, nil)
	if !proxyheader.InUse() {
		t.Fatal("the guard is off with a PROXY-enabled inbound running")
	}
	if err := c.RemoveInbound("pp-in"); err != nil {
		t.Fatal(err)
	}
	if proxyheader.InUse() {
		t.Fatal("the guard stayed on after the PROXY-enabled inbound was removed")
	}
	raw, err := json.Marshal(map[string]any{
		"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": addr.Port,
		"proxy_protocol": true,
		"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.AddInbound(raw); err != nil {
		t.Fatal(err)
	}
	if !proxyheader.InUse() {
		t.Fatal("the guard is off after a PROXY-enabled inbound was added")
	}
	if err = c.Stop(); err != nil {
		t.Fatal(err)
	}
	if proxyheader.InUse() {
		t.Fatal("the guard stayed on after the core stopped")
	}

	// A core that builds a PROXY-enabled inbound and then fails to start
	// leaves the guard as it found it.
	failing, err := json.Marshal(map[string]any{
		"log": map[string]any{"disabled": true},
		"inbounds": []any{map[string]any{
			"type": "vless", "tag": "pp-in", "listen": "127.0.0.1", "listen_port": addr.Port,
			"proxy_protocol": true,
			"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
		}},
		// Built after the inbounds, and refused there: a cipher that is none.
		"outbounds": []any{map[string]any{"type": "shadowsocks", "tag": "bad", "server": "127.0.0.1", "server_port": 1, "method": "no-such-cipher", "password": "p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = NewCore().Start(failing); err == nil {
		t.Fatal("a core with an outbound it cannot build started")
	}
	if proxyheader.InUse() {
		t.Fatal("the guard stayed on after a core failed to start")
	}
}

// readsProxyHeaders reports whether an inbound in the running core reads PROXY
// protocol headers. It waits out an inbound being added or replaced, which can
// be accepting before it is listed.
func (c *Core) readsProxyHeaders() bool {
	box, err := c.running()
	if err != nil {
		return false
	}
	return len(proxyHeaderPorts(box.inbound.settled())) > 0
}
