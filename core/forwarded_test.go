package core

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/net/proxy"
)

// The source the core records over an HTTP transport -- what the online list
// and the per-client IP limit run on -- is not whatever X-Forwarded-For the
// client sends (#279). sing-box took the header's first entry from anyone.
// Here the client sends one through each transport that carries headers, from
// loopback: an inbound listening beyond loopback ignores it, and one listening
// on loopback believes the entry its reverse proxy appended -- the last -- and
// has its port watched by the dial guard.
func TestForwardedForOverEveryHTTPTransport(t *testing.T) {
	target := echoServer(t)
	headers := map[string]any{"X-Forwarded-For": "1.2.3.4, 203.0.113.9"}
	transports := map[string]map[string]any{
		"ws":          {"type": "ws", "path": "/xff", "headers": headers},
		"httpupgrade": {"type": "httpupgrade", "path": "/xff", "headers": headers},
		"http":        {"type": "http", "path": "/xff", "headers": headers},
	}
	for name, transport := range transports {
		for _, tc := range []struct {
			listen  string
			proxy   bool
			want    string
			guarded bool
		}{
			{listen: "0.0.0.0", want: "127.0.0.1"},
			{listen: "127.0.0.1", want: "203.0.113.9", guarded: true},
			// Behind an nginx stream block, which names whoever connected to it
			// in a PROXY header and adds no X-Forwarded-For: the client's core
			// dialed the front from loopback, and the header it wrote is not
			// believed for being named as this host.
			{listen: "127.0.0.1", proxy: true, want: "127.0.0.1", guarded: true},
		} {
			label := name + " listening on " + tc.listen
			if tc.proxy {
				label += " behind a PROXY front"
			}
			t.Run(label, func(t *testing.T) {
				var serverPort int
				server := startTestCore(t, func() map[string]any {
					serverPort = freeTCPPort(t)
					return map[string]any{
						"log": map[string]any{"disabled": true},
						"inbounds": []any{map[string]any{
							"type": "vless", "tag": "xff-in", "listen": tc.listen, "listen_port": serverPort,
							"users":          []any{map[string]any{"name": "u", "uuid": proxyTestUUID}},
							"transport":      transport,
							"proxy_protocol": tc.proxy,
						}},
						"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
					}
				})
				if got := server.guardsAnInbound(); got != tc.guarded {
					t.Fatalf("dial guard watching the inbound: %v, want %v", got, tc.guarded)
				}
				// A reverse proxy on this host, as nginx would be: the client's
				// own core dialing the inbound's port is refused by the guard
				// when the inbound believes this host, which is the point.
				front := tcpForwarder(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(serverPort)), tc.proxy)
				var mixedPort int
				startTestCore(t, func() map[string]any {
					mixedPort = freeTCPPort(t)
					return map[string]any{
						"log":      map[string]any{"disabled": true},
						"inbounds": []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": mixedPort}},
						"outbounds": []any{map[string]any{
							"type": "vless", "tag": "out", "server": "127.0.0.1", "server_port": front.Port,
							"uuid": proxyTestUUID, "transport": transport,
						}},
					}
				})
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
					t.Fatalf("no echo over %s: %v", name, err)
				}
				got, _, err := net.SplitHostPort(sessionSource(t, server))
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("core recorded the source as %s, want %s", got, tc.want)
				}
			})
		}
	}
}

// tcpForwarder is a reverse proxy on this host that passes bytes to target
// untouched: the headers are the client's, and the connection the inbound sees
// is from loopback. With proxyHeader it is an nginx stream block instead,
// naming whoever connected to it in a PROXY header first.
func tcpForwarder(t *testing.T, target string, proxyHeader bool) M.Socksaddr {
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
				upstream, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer upstream.Close()
				if proxyHeader {
					from := client.RemoteAddr().(*net.TCPAddr)
					to := upstream.RemoteAddr().(*net.TCPAddr)
					header := "PROXY TCP4 " + from.IP.String() + " " + to.IP.String() + " " +
						strconv.Itoa(from.Port) + " " + strconv.Itoa(to.Port) + "\r\n"
					if _, err = upstream.Write([]byte(header)); err != nil {
						return
					}
				}
				go io.Copy(upstream, client)
				io.Copy(client, upstream)
			}()
		}
	}()
	return M.SocksaddrFromNet(l.Addr())
}
