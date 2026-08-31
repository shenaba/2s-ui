package notify

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// silentServer accepts connections and then says nothing at all -- no greeting,
// ever. That is the shape that matters: a refused connection fails fast on its
// own, while a server that completes the TCP handshake and then stops is what
// used to block the sender forever.
//
// closeAll is the way out. The accepted connections are parked rather than
// closed, because closing one would hand net/smtp an EOF and let it return an
// error for the wrong reason -- but a test that has given up needs to unblock
// whatever is still reading from them, or it leaves a goroutine running past
// its own cleanup.
type silentServer struct {
	host string
	port int

	mu     sync.Mutex
	closed bool
	ln     net.Listener
	held   []net.Conn
}

func newSilentServer(t *testing.T) *silentServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &silentServer{ln: ln}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			// Racing closeAll: it has already taken the lock and closed
			// everything it knew about, so this one is ours to close.
			if s.closed {
				s.mu.Unlock()
				conn.Close()
				continue
			}
			s.held = append(s.held, conn)
			s.mu.Unlock()
		}
	}()

	host, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	s.host, s.port = host, port

	t.Cleanup(s.closeAll)
	return s
}

// closeAll stops the listener and drops every parked connection, which is what
// releases a sender still blocked reading a greeting. Safe to call twice.
func (s *silentServer) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.ln.Close()
	for _, conn := range s.held {
		conn.Close()
	}
	s.held = nil
}

func TestSendSMTPGivesUpOnASilentServer(t *testing.T) {
	srv := newSilentServer(t)

	restore := smtpTimeout
	smtpTimeout = 200 * time.Millisecond
	// Restored only after every sender has been joined below, so the write
	// cannot race a goroutine still reading it inside dialSMTP.
	defer func() { smtpTimeout = restore }()

	// Every security mode, because each takes its own route to a connection and
	// only the "none" one ever went through a single shared dial.
	for _, security := range []string{"none", "starttls", "tls"} {
		cfg := Config{
			Lang: "en",
			SMTP: SMTPConfig{
				Host: srv.host, Port: srv.port, From: "panel@example.com",
				To: []string{"admin@example.com"}, Security: security,
			},
		}

		done := make(chan error, 1)
		go func() { done <- sendSMTP(cfg, Event{Kind: CoreUp, At: time.Now()}) }()

		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: a server that never answers reported success", security)
			}
		case <-time.After(10 * time.Second):
			// Not just a slow test: this is the subscriber worker, the settings
			// page's test request, or a report job, blocked with no way out.
			// Tear the server down so the sender returns and this test does not
			// leave a goroutine reading smtpTimeout behind it.
			srv.closeAll()
			<-done
			t.Fatalf("%s: sendSMTP never returned; the send path has no timeout", security)
		}
	}
}
