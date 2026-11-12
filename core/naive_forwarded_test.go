package core

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sagernet/quic-go/http3"
	M "github.com/sagernet/sing/common/metadata"

	"golang.org/x/net/http2"
)

// The naive inbound took its client from the first entry of X-Forwarded-For
// from anyone, like the HTTP transports did (#279), so a naive client could
// name its own address to the online list and the per-client IP limit. Here
// a client sends one over each of the three ways naive serves a CONNECT --
// HTTP/1.1, which the inbound hijacks, prior-knowledge HTTP/2, and HTTP/3 --
// and the core must record the connection's own address. That holds for an
// inbound listening on loopback too: naive never believes this host.
func TestNaiveIgnoresForwardedFor(t *testing.T) {
	target := echoServer(t)
	certPath, keyPath := writeTestCert(t)
	for _, tc := range []struct {
		name    string
		network string
		dial    func(t *testing.T, server, target M.Socksaddr) io.ReadWriter
	}{
		{name: "http/1.1", network: "tcp", dial: naiveHTTP1},
		{name: "h2c", network: "tcp", dial: naiveH2C},
		{name: "http/3", network: "udp", dial: naiveHTTP3},
	} {
		for _, listen := range []string{"0.0.0.0", "127.0.0.1"} {
			t.Run(tc.name+" listening on "+listen, func(t *testing.T) {
				var port int
				server := startTestCore(t, func() map[string]any {
					inbound := map[string]any{
						"type": "naive", "tag": "naive-in", "listen": listen, "network": tc.network,
						"users": []any{map[string]any{"username": "u", "password": "p"}},
					}
					if tc.network == "udp" {
						port = freeUDPPort(t)
						inbound["tls"] = map[string]any{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
					} else {
						port = freeTCPPort(t)
					}
					inbound["listen_port"] = port
					return map[string]any{
						"log":       map[string]any{"disabled": true},
						"inbounds":  []any{inbound},
						"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
					}
				})
				if server.guardsAnInbound() {
					t.Fatal("dial guard watching a naive inbound, which believes no header from this host")
				}
				conn := tc.dial(t, M.ParseSocksaddrHostPort("127.0.0.1", uint16(port)), target)
				naivePing(t, conn)
				got, _, err := net.SplitHostPort(sessionSource(t, server))
				if err != nil {
					t.Fatal(err)
				}
				if got != "127.0.0.1" {
					t.Fatalf("core recorded the source as %s, want the connection's own 127.0.0.1", got)
				}
			})
		}
	}
}

// naiveForwarded is the header every client below sends: an address of its
// choosing, then one a CDN would have appended.
const naiveForwarded = "1.2.3.4, 203.0.113.9"

func naiveHeaders(header http.Header) {
	header.Set("Padding", "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
	header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")))
	header.Set("X-Forwarded-For", naiveForwarded)
}

// naiveHTTP1 opens a tunnel with an HTTP/1.1 CONNECT, written by hand.
func naiveHTTP1(t *testing.T, server, target M.Socksaddr) io.ReadWriter {
	conn, err := net.DialTimeout("tcp", server.String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: target.String()},
		Host:   target.String(),
		Header: make(http.Header),
	}
	naiveHeaders(request.Header)
	if err = request.Write(conn); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT: %s", response.Status)
	}
	return struct {
		io.Reader
		io.Writer
	}{reader, conn}
}

// naiveH2C opens a tunnel with a CONNECT over prior-knowledge HTTP/2.
func naiveH2C(t *testing.T, server, target M.Socksaddr) io.ReadWriter {
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return naiveRoundTrip(t, transport, "http", server, target)
}

// naiveHTTP3 opens a tunnel with a CONNECT over HTTP/3.
func naiveHTTP3(t *testing.T, server, target M.Socksaddr) io.ReadWriter {
	transport := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, NextProtos: []string{http3.NextProtoH3}}}
	t.Cleanup(func() { transport.Close() })
	return naiveRoundTrip(t, transport, "https", server, target)
}

func naiveRoundTrip(t *testing.T, transport http.RoundTripper, scheme string, server, target M.Socksaddr) io.ReadWriter {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	body, writer := io.Pipe()
	t.Cleanup(func() { writer.Close() })
	request, err := http.NewRequestWithContext(ctx, http.MethodConnect, scheme+"://"+server.String(), body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = target.String()
	request.URL.Host = server.String()
	request.Header.Set("-connect-authority", target.String())
	naiveHeaders(request.Header)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT: %s", response.Status)
	}
	return struct {
		io.Reader
		io.Writer
	}{response.Body, writer}
}

// naivePing sends one message through a naive tunnel and waits for the echo,
// in naive's padded framing: a 16-bit length, a padding length, the data, the
// padding.
func naivePing(t *testing.T, conn io.ReadWriter) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		frame := []byte{0, 4, 3, 'p', 'i', 'n', 'g', 0, 0, 0}
		if _, err := conn.Write(frame); err != nil {
			done <- err
			return
		}
		header := make([]byte, 3)
		if _, err := io.ReadFull(conn, header); err != nil {
			done <- err
			return
		}
		data := make([]byte, int(binary.BigEndian.Uint16(header[:2]))+int(header[2]))
		if _, err := io.ReadFull(conn, data); err != nil {
			done <- err
			return
		}
		if got := string(data[:binary.BigEndian.Uint16(header[:2])]); got != "ping" {
			done <- io.ErrUnexpectedEOF
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("no echo: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no echo")
	}
}
