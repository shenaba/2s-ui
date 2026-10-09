package core

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
)

// Removing a QUIC inbound has to tell its clients. This is how the panel
// disconnects a removed QUIC user at all -- the user-table swap returns
// ErrRestartRequired and the inbound is rebuilt -- and without a
// CONNECTION_CLOSE each client found out only from its own idle timeout, so
// "reconnects once" meant "hangs for up to 30 seconds, then reconnects".
//
// core/quicgrace has its own tests; this one is about the wiring: the copies
// have to route their listener through it, and Close has to stop the service
// before the listener closes the socket the packet goes out on. Get either
// wrong and the package tests still pass while nothing reaches a client.
func TestRemovingAQUICInboundTellsItsClients(t *testing.T) {
	certPath, keyPath := writeTestCert(t)
	quicTLS := map[string]any{
		"enabled": true, "server_name": "t.example.com", "alpn": []string{"h3"},
		"certificate_path": certPath, "key_path": keyPath,
	}
	for _, tc := range []struct {
		name    string
		options map[string]any
	}{
		{"hysteria", map[string]any{"type": "hysteria", "up_mbps": 100, "down_mbps": 100,
			"users": []any{map[string]any{"name": "u", "auth_str": "p"}}}},
		{"hysteria2", map[string]any{"type": "hysteria2",
			"users": []any{map[string]any{"name": "u", "password": "p"}}}},
		{"tuic", map[string]any{"type": "tuic", "congestion_control": "cubic",
			"users": []any{map[string]any{"name": "u", "uuid": "2dd61d93-75d8-4da4-ac0e-6aece7eac365", "password": "p"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := freeUDPPort(t)
			inbound := map[string]any{"tag": tc.name + "-in", "listen": "127.0.0.1", "listen_port": port, "tls": quicTLS}
			for key, value := range tc.options {
				inbound[key] = value
			}
			raw, err := json.Marshal(map[string]any{
				"log":       map[string]any{"disabled": true},
				"inbounds":  []any{inbound},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			c := NewCore()
			err = c.Start(raw)
			skipIfFeatureMissing(t, err)
			if err != nil {
				t.Fatalf("start core: %v", err)
			}
			t.Cleanup(func() { _ = c.Stop() })

			// A bare QUIC handshake is enough: the session exists from the
			// moment it is accepted, before any protocol authentication, and
			// that session is what a rebuild used to strand.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := quic.DialAddr(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
				&tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}},
				&quic.Config{MaxIdleTimeout: 30 * time.Second})
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			t.Cleanup(func() { _ = client.CloseWithError(0, "") })
			// The server accepts asynchronously; give it a moment to hand the
			// connection to the service, which is what the wrapper tracks.
			time.Sleep(200 * time.Millisecond)

			if err := c.RemoveInbound(tc.name + "-in"); err != nil {
				t.Fatalf("remove inbound: %v", err)
			}

			select {
			case <-client.Context().Done():
				cause := context.Cause(client.Context())
				var appErr *quic.ApplicationError
				var transportErr *quic.TransportError
				if !(errors.As(cause, &appErr) && appErr.Remote) &&
					!(errors.As(cause, &transportErr) && transportErr.Remote) {
					t.Fatalf("client ended with %v, want a close sent by the server", cause)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("client was not told: it would sit out its idle timeout")
			}
		})
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()
	return port
}
