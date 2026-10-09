// Package quicgrace makes closing a QUIC inbound tell its clients so.
//
// sing-quic's services listen through a single-use quic-go Transport, and
// closing that "abruptly terminates all existing connections, without sending a
// CONNECTION_CLOSE to the peers" (quic-go's own doc comment on Transport.Close).
// Each listener also gets a fresh random stateless-reset key, so the listener
// that replaces it cannot reset the old connections either. A client therefore
// learns that the server is gone only from its own idle timeout -- 30 seconds
// for hysteria -- and every rebuild of a QUIC inbound cost every user on it that
// long, not the instant reconnect it was meant to be.
//
// That matters more here than upstream, because rebuilding is how this panel
// disconnects a removed QUIC user at all (see core/usersession, and
// ErrRestartRequired): a user removal, a certificate change and a core restart
// all go through it.
//
// sing-quic hands listening to the TLS config when the config implements its
// exported ServerConfig interface. Wrap returns such a config: it listens
// exactly as sing-quic would and remembers the connections it accepts, and
// closing the listener sends each of them a CONNECTION_CLOSE before the
// transport goes away. No upstream code is copied or reached into.
//
// Ported from upstream s-ui (#1278), without the half that closes one client's
// session by its source address. That address is not an identity for a QUIC
// session -- quic-go rewrites it on a NAT rebind, and the port is recycled to
// someone else once the session ends -- which is exactly why core/usersession
// does not key QUIC sessions on it. Closing by address could cut the wrong
// user's session; rebuilding the inbound, now that it is graceful, cannot.
package quicgrace

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"net"
	"sync"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	qtls "github.com/sagernet/sing-quic"
	aTLS "github.com/sagernet/sing/common/tls"
)

// Options mirror the sing-quic ListenOptions the service would have passed
// itself. The ServerConfig hook does not carry them, so the inbound states
// them here; getting them wrong changes what the listener sends unprompted.
type Options struct {
	DisableVersionNegotiationPackets bool
	StatelessReset                   bool
}

// Wrap returns config with QUIC listening taken over. Everything else is
// config's own.
func Wrap(config aTLS.ServerConfig, options Options) aTLS.ServerConfig {
	if config == nil {
		return nil
	}
	return &serverConfig{ServerConfig: config, options: options}
}

type serverConfig struct {
	aTLS.ServerConfig
	options Options
}

var _ qtls.ServerConfig = (*serverConfig)(nil)

// ConfigureHTTP3 is what qtls.ConfigureHTTP3 does for a config that is not a
// ServerConfig. It has to be repeated because this one is.
func (c *serverConfig) ConfigureHTTP3() {
	if inner, ok := c.ServerConfig.(qtls.ServerConfig); ok {
		inner.ConfigureHTTP3()
		return
	}
	if std, err := c.STDConfig(); err == nil {
		http3.ConfigureTLSConfig(std)
	}
}

func (c *serverConfig) Listen(conn net.PacketConn, config *quic.Config) (qtls.Listener, error) {
	if inner, ok := c.ServerConfig.(qtls.ServerConfig); ok {
		listener, err := inner.Listen(conn, config)
		if err != nil {
			return nil, err
		}
		return track(listener), nil
	}
	transport, std, err := c.transport(conn)
	if err != nil {
		return nil, err
	}
	listener, err := transport.Listen(std, config)
	if err != nil {
		return nil, qtls.WrapError(err)
	}
	return track(listener), nil
}

func (c *serverConfig) ListenEarly(conn net.PacketConn, config *quic.Config) (qtls.EarlyListener, error) {
	if inner, ok := c.ServerConfig.(qtls.ServerConfig); ok {
		listener, err := inner.ListenEarly(conn, config)
		if err != nil {
			return nil, err
		}
		return track(listener), nil
	}
	transport, std, err := c.transport(conn)
	if err != nil {
		return nil, err
	}
	listener, err := transport.ListenEarly(std, config)
	if err != nil {
		return nil, qtls.WrapError(err)
	}
	return track(listener), nil
}

// transport is the one qtls.ListenWithOptions builds. The handshake timeout
// it applies first has already been applied to config by the time the hook is
// called.
func (c *serverConfig) transport(conn net.PacketConn) (*quic.Transport, *tls.Config, error) {
	std, err := c.STDConfig()
	if err != nil {
		return nil, nil, err
	}
	transport := &quic.Transport{Conn: conn, DisableVersionNegotiationPackets: c.options.DisableVersionNegotiationPackets}
	transport.SetSingleUse(true)
	if c.options.StatelessReset {
		var key quic.StatelessResetKey
		if _, err := rand.Read(key[:]); err != nil {
			return nil, nil, err
		}
		transport.StatelessResetKey = &key
	}
	return transport, std, nil
}

type acceptor interface {
	Accept(ctx context.Context) (*quic.Conn, error)
	Close() error
	Addr() net.Addr
}

func track(inner acceptor) *listener {
	return &listener{acceptor: inner, conns: make(map[*quic.Conn]struct{})}
}

// listener remembers what it accepted until it is closed or the connection
// ends on its own. Bounded by live sessions: each entry is dropped when its
// connection's context ends, which quic-go guarantees for every way a
// connection can end.
type listener struct {
	acceptor
	mu     sync.Mutex
	conns  map[*quic.Conn]struct{}
	closed bool
}

func (l *listener) Accept(ctx context.Context) (*quic.Conn, error) {
	conn, err := l.acceptor.Accept(ctx)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.closed {
		// Accepted in the window between Close collecting the set and the
		// inner listener going away: it would miss its CONNECTION_CLOSE.
		l.mu.Unlock()
		_ = conn.CloseWithError(0, "")
		return nil, quic.ErrServerClosed
	}
	l.conns[conn] = struct{}{}
	l.mu.Unlock()
	go func() {
		<-conn.Context().Done()
		l.mu.Lock()
		delete(l.conns, conn)
		l.mu.Unlock()
	}()
	return conn, nil
}

// Close sends every accepted connection a CONNECTION_CLOSE -- application error
// 0 and no reason, so there is nothing for a prober to read -- then closes the
// listener and, with it, the transport.
//
// The packet goes out over the transport's socket, so the inbound has to close
// this before the UDP listener that owns that socket. CloseWithError does not
// wait on the peer, only on the connection's own run loop, so a dead client
// does not hold this up.
func (l *listener) Close() error {
	l.mu.Lock()
	l.closed = true
	conns := make([]*quic.Conn, 0, len(l.conns))
	for conn := range l.conns {
		conns = append(conns, conn)
	}
	l.mu.Unlock()
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Go(func() { _ = conn.CloseWithError(0, "") })
	}
	wg.Wait()
	return l.acceptor.Close()
}
