package network

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// serveOnce accepts one connection, wraps it, and reads from it the way a TLS
// handshake would -- which is what triggers the peek.
func serveOnce(t *testing.T, ln net.Listener, out chan<- []byte) {
	t.Helper()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(out)
			return
		}
		wrapped := NewAutoHttpsConn(conn)
		buf := make([]byte, 512)
		n, _ := wrapped.Read(buf)
		out <- buf[:n]
	}()
}

// A plaintext request to the TLS port is answered with a redirect. The response
// used to be built from a zero-valued http.Response, whose status line reads
// "HTTP/0.0 307 Temporary Redirect" -- http.ReadResponse refuses it, and so do
// curl and every browser, so this redirect never worked at all.
func TestAutoHttpsConnRedirectsPlainHTTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	handled := make(chan []byte, 1)
	serveOnce(t, ln, handled)

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := fmt.Fprint(client, "GET /app/x?y=1 HTTP/1.1\r\nHost: panel.example.com\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("the redirect is not a readable HTTP response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTemporaryRedirect)
	}
	if resp.Proto != "HTTP/1.1" {
		t.Errorf("proto = %q, want HTTP/1.1", resp.Proto)
	}
	const want = "https://panel.example.com/app/x?y=1"
	if got := resp.Header.Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if !resp.Close {
		t.Error("the response must ask the client to close: the connection is torn down right after")
	}
}

// Anything that is not a parseable HTTP request is handed on untouched, which
// is how the TLS handshake sees its own first bytes.
func TestAutoHttpsConnPassesNonHTTPThrough(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	handled := make(chan []byte, 1)
	serveOnce(t, ln, handled)

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// The opening bytes of a TLS ClientHello.
	hello := []byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01}
	if _, err := client.Write(hello); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-handled:
		if len(got) != len(hello) {
			t.Fatalf("read %d bytes %v, want the %d written", len(got), got, len(hello))
		}
		for i := range hello {
			if got[i] != hello[i] {
				t.Fatalf("byte %d = %#x, want %#x", i, got[i], hello[i])
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the handshake bytes never reached the caller")
	}
}

// A peer that connects and then says nothing used to pin a goroutine and a file
// descriptor for the life of the process: the peek had no deadline at all.
func TestAutoHttpsConnTimesOutASilentPeer(t *testing.T) {
	restore := firstReadTimeout
	firstReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { firstReadTimeout = restore })

	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := NewAutoHttpsConn(server).Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a peer that sent nothing must surface as an error, not a short read")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Read never returned: the peek has no deadline")
	}
}

// The read error is held and handed to the caller rather than reported as a
// zero-length read, which let the TLS handshake retry against a connection that
// was already gone.
func TestAutoHttpsConnSurfacesTheReadError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	// Closed before anything is written, so the peek's Read fails at once.
	client.Close()

	buf := make([]byte, 16)
	n, err := NewAutoHttpsConn(server).Read(buf)
	if err == nil {
		t.Fatalf("read returned n=%d with no error, want the peer's error", n)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}
