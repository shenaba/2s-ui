package quicgrace

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
	sbtls "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	qtls "github.com/sagernet/sing-quic"
	"github.com/sagernet/sing/common/json/badoption"
)

func serverTLS(t *testing.T) sbtls.ServerConfig {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "quicgrace"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"quicgrace"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config, err := sbtls.NewSTDServer(context.Background(), log.NewNOPFactory().Logger(), option.InboundTLSOptions{
		Enabled:     true,
		ALPN:        badoption.Listable[string]{"quicgrace"},
		Certificate: badoption.Listable[string]{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))},
		Key:         badoption.Listable[string]{string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))},
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

// connect listens through listen and connects n clients. It returns the
// listener and the clients once the server has accepted all of them.
func connect(t *testing.T, listen func(net.PacketConn) (qtls.Listener, error), n int) (qtls.Listener, []*quic.Conn) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	listener, err := listen(pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan struct{}, n)
	go func() {
		for {
			if _, err := listener.Accept(context.Background()); err != nil {
				return
			}
			accepted <- struct{}{}
		}
	}()
	clients := make([]*quic.Conn, 0, n)
	for range n {
		// An idle timeout far longer than the wait below, so a client that
		// notices only through it cannot pass for one that was told.
		client, err := quic.DialAddr(context.Background(), pc.LocalAddr().String(),
			&tls.Config{InsecureSkipVerify: true, NextProtos: []string{"quicgrace"}},
			&quic.Config{MaxIdleTimeout: 20 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.CloseWithError(0, "") })
		clients = append(clients, client)
	}
	for range n {
		select {
		case <-accepted:
		case <-time.After(5 * time.Second):
			t.Fatal("server never accepted")
		}
	}
	return listener, clients
}

// closedWithin reports how the client learned its session ended, or nil if it
// did not within wait.
func closedWithin(client *quic.Conn, wait time.Duration) error {
	select {
	case <-client.Context().Done():
		return context.Cause(client.Context())
	case <-time.After(wait):
		return nil
	}
}

// isRemoteClose reports whether the server told the client. A connection
// closed before its handshake completes -- which ListenEarly hands out -- cannot
// carry an application close yet, so QUIC sends it as a transport close with
// the APPLICATION_ERROR code instead (RFC 9000, 10.2.3). Both are the client
// being told, which is the whole point.
func isRemoteClose(err error) bool {
	var appErr *quic.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Remote
	}
	var transportErr *quic.TransportError
	return errors.As(err, &transportErr) && transportErr.Remote &&
		transportErr.ErrorCode == quic.ApplicationErrorErrorCode
}

// Through Wrap, closing the listener reaches every client as a CONNECTION_CLOSE
// at once, for both listen paths sing-quic uses.
func TestClosingTellsEveryClient(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listen func(qtls.ServerConfig, net.PacketConn) (qtls.Listener, error)
	}{
		{"Listen", func(c qtls.ServerConfig, pc net.PacketConn) (qtls.Listener, error) {
			return c.Listen(pc, &quic.Config{})
		}},
		{"ListenEarly", func(c qtls.ServerConfig, pc net.PacketConn) (qtls.Listener, error) {
			return c.ListenEarly(pc, &quic.Config{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, clients := connect(t, func(pc net.PacketConn) (qtls.Listener, error) {
				wrapped := Wrap(serverTLS(t), Options{StatelessReset: true}).(qtls.ServerConfig)
				return tc.listen(wrapped, pc)
			}, 3)
			_ = listener.Close()
			for i, client := range clients {
				if err := closedWithin(client, 2*time.Second); !isRemoteClose(err) {
					t.Errorf("client %d got %v, want a remote application close at once", i, err)
				}
			}
		})
	}
}

// Without Wrap -- sing-quic's own listen -- the client hears nothing and is
// left to its idle timeout. This is the reason the package exists, and it is
// checked so that a quic-go or sing-quic change that fixes it shows up here
// rather than leaving a wrapper nobody needs.
func TestUnwrappedCloseIsSilent(t *testing.T) {
	listener, clients := connect(t, func(pc net.PacketConn) (qtls.Listener, error) {
		return qtls.ListenWithOptions(pc, serverTLS(t), &quic.Config{}, qtls.ListenOptions{StatelessReset: true})
	}, 1)
	_ = listener.Close()
	if err := closedWithin(clients[0], 2*time.Second); err != nil {
		t.Logf("sing-quic now tells the client itself (%v); quicgrace may no longer be needed", err)
	}
}

// sing-quic routes listening through the config only when it implements
// qtls.ServerConfig, so a Wrap that lost that would silently do nothing.
func TestWrapIsHonouredBySingQuic(t *testing.T) {
	l, clients := connect(t, func(pc net.PacketConn) (qtls.Listener, error) {
		return qtls.ListenWithOptions(pc, Wrap(serverTLS(t), Options{StatelessReset: true}), &quic.Config{}, qtls.ListenOptions{})
	}, 1)
	if _, ok := l.(*listener); !ok {
		t.Fatalf("qtls.ListenWithOptions did not listen through Wrap, got %T", l)
	}
	_ = l.Close()
	if err := closedWithin(clients[0], 2*time.Second); !isRemoteClose(err) {
		t.Fatalf("client got %v, want a remote application close at once", err)
	}
}

func TestWrapNil(t *testing.T) {
	if Wrap(nil, Options{}) != nil {
		t.Fatal("a nil config must stay nil, or the service would take TLS as configured")
	}
}
