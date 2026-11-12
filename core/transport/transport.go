// Package transport builds the V2Ray server transports for the copies under
// core/protocol/, in place of sing-box's v2ray.NewServerTransport, so that the
// client a request names in X-Forwarded-For is decided by the panel rather than
// by whoever sent the request (#279).
//
// sing-box's HTTP transport servers (ws, http, httpupgrade, grpc lite) take
// the client from sHttp.SourceAddress, which prefers the first entry of
// X-Forwarded-For over the connection, and the gRPC one takes the last entry
// of the same metadata. Either is the client's own to write -- even through a
// CDN, which appends to the header rather than replacing it -- so the online
// list and the per-client IP limit both believed whatever a client sent.
//
// Nothing of those servers is copied here. Each is still built by sing-box,
// and still decides everything about a request; it is only served by a server
// of this package's, which settles the client first -- through the inbound's
// Resolver, proxyheader.Forwarded -- and hands the request on with that as its
// RemoteAddr (its gRPC peer) and the header gone. What sing-box's own server
// does around the handler -- the TLS listener, ALPN, h2c, its timeouts -- is
// done here the same way, which is the part that has to follow upstream:
// scripts/check-protocol-copies.sh pins the files it mirrors.
//
// QUIC carries no HTTP header, and is sing-box's own transport as before.
package transport

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2ray"
	"github.com/sagernet/sing-box/transport/v2raygrpclite"
	"github.com/sagernet/sing-box/transport/v2rayhttp"
	"github.com/sagernet/sing-box/transport/v2rayhttpupgrade"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c" //nolint:staticcheck
)

// Resolver names the client of a request that arrived from peer -- the
// connection's remote address, or the client a PROXY header named -- carrying
// the X-Forwarded-For values forwarded.
type Resolver func(peer M.Socksaddr, forwarded []string) M.Socksaddr

// ForwardedHeader is the header a Resolver decides on, removed from every
// request before sing-box sees it.
const ForwardedHeader = "X-Forwarded-For"

// FromRequest returns the client of request as resolve decides it, and takes
// ForwardedHeader off the request, so that nothing after this sees what the
// client wrote. The naive copy, which serves its own http.Server, reads its
// requests through this too.
func FromRequest(request *http.Request, resolve Resolver) M.Socksaddr {
	forwarded := request.Header.Values(ForwardedHeader)
	request.Header.Del(ForwardedHeader)
	return resolve(M.ParseSocksaddr(request.RemoteAddr), forwarded)
}

// NewServer is v2ray.NewServerTransport, with the client of every HTTP
// transport decided by resolve.
func NewServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayTransportOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (adapter.V2RayServerTransport, error) {
	switch options.Type {
	case C.V2RayTransportTypeHTTP:
		return newHTTPServer(ctx, logger, options.HTTPOptions, tlsConfig, handler, resolve)
	case C.V2RayTransportTypeWebsocket:
		return newWebsocketServer(ctx, logger, options.WebsocketOptions, tlsConfig, handler, resolve)
	case C.V2RayTransportTypeHTTPUpgrade:
		return newHTTPUpgradeServer(ctx, logger, options.HTTPUpgradeOptions, tlsConfig, handler, resolve)
	case C.V2RayTransportTypeGRPC:
		return newGRPCServer(ctx, logger, options.GRPCOptions, tlsConfig, handler, resolve)
	default:
		return v2ray.NewServerTransport(ctx, logger, options, tlsConfig, handler)
	}
}

// httpServer serves one of sing-box's HTTP transport servers, upstream, behind
// the client decided by resolve.
type httpServer struct {
	upstream   http.Handler
	resolve    Resolver
	tlsConfig  tls.ServerConfig
	nextProtos func(tls.ServerConfig)
	// h2c is the transport's prior-knowledge HTTP/2, for those that take it:
	// sing-box's own would hand the streams straight to upstream, past resolve.
	h2c        http.Handler
	httpServer *http.Server
}

func newServer(ctx context.Context, upstream http.Handler, tlsConfig tls.ServerConfig, resolve Resolver) *httpServer {
	s := &httpServer{upstream: upstream, resolve: resolve, tlsConfig: tlsConfig}
	s.httpServer = &http.Server{
		Handler: s,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return log.ContextWithNewID(ctx)
		},
	}
	return s
}

// withH2C has s take prior-knowledge HTTP/2 as sing-box's server does.
func (s *httpServer) withH2C(idleTimeout time.Duration) {
	//nolint:staticcheck
	s.h2c = h2c.NewHandler(s, &http2.Server{IdleTimeout: idleTimeout})
}

// withHeaderLimits gives s the header timeout and size sing-box's server has.
func (s *httpServer) withHeaderLimits() {
	s.httpServer.ReadHeaderTimeout = C.TCPTimeout
	s.httpServer.MaxHeaderBytes = http.DefaultMaxHeaderBytes
}

func newWebsocketServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayWebsocketOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (*httpServer, error) {
	upstream, err := v2raywebsocket.NewServer(ctx, logger, options, nil, handler)
	if err != nil {
		return nil, err
	}
	s := newServer(ctx, upstream, tlsConfig, resolve)
	s.withHeaderLimits()
	return s, nil
}

func newHTTPServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayHTTPOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (*httpServer, error) {
	upstream, err := v2rayhttp.NewServer(ctx, logger, options, nil, handler)
	if err != nil {
		return nil, err
	}
	s := newServer(ctx, upstream, tlsConfig, resolve)
	s.withHeaderLimits()
	s.withH2C(time.Duration(options.IdleTimeout))
	s.nextProtos = func(config tls.ServerConfig) {
		if len(config.NextProtos()) == 0 {
			config.SetNextProtos([]string{http2.NextProtoTLS, "http/1.1"})
		} else if !common.Contains(config.NextProtos(), http2.NextProtoTLS) {
			config.SetNextProtos(append([]string{http2.NextProtoTLS}, config.NextProtos()...))
		}
	}
	return s, nil
}

func newHTTPUpgradeServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayHTTPUpgradeOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (*httpServer, error) {
	upstream, err := v2rayhttpupgrade.NewServer(ctx, logger, options, nil, handler)
	if err != nil {
		return nil, err
	}
	s := newServer(ctx, upstream, tlsConfig, resolve)
	s.withHeaderLimits()
	// HTTP/1.1 only: the upgrade hijacks the connection.
	s.httpServer.TLSNextProto = make(map[string]func(*http.Server, *tls.STDConn, http.Handler))
	s.nextProtos = func(config tls.ServerConfig) {
		if len(config.NextProtos()) == 0 {
			config.SetNextProtos([]string{"http/1.1"})
		}
	}
	return s, nil
}

func newGRPCLiteServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayGRPCOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (*httpServer, error) {
	upstream, err := v2raygrpclite.NewServer(ctx, logger, options, nil, handler)
	if err != nil {
		return nil, err
	}
	s := newServer(ctx, upstream, tlsConfig, resolve)
	s.withH2C(time.Duration(options.IdleTimeout))
	s.nextProtos = func(config tls.ServerConfig) {
		if !common.Contains(config.NextProtos(), http2.NextProtoTLS) {
			config.SetNextProtos(append([]string{"h2"}, config.NextProtos()...))
		}
	}
	return s, nil
}

func (s *httpServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if s.h2c != nil && request.Method == "PRI" && len(request.Header) == 0 && request.URL.Path == "*" && request.Proto == "HTTP/2.0" {
		s.h2c.ServeHTTP(writer, request)
		return
	}
	request.RemoteAddr = FromRequest(request, s.resolve).String()
	s.upstream.ServeHTTP(writer, request)
}

// ReadsForwarded tells proxyheader that this transport takes the client from
// X-Forwarded-For, through the Resolver it was built with.
func (s *httpServer) ReadsForwarded() bool {
	return true
}

func (s *httpServer) Network() []string {
	return []string{N.NetworkTCP}
}

func (s *httpServer) Serve(listener net.Listener) error {
	if s.tlsConfig != nil {
		if s.nextProtos != nil {
			s.nextProtos(s.tlsConfig)
		}
		listener = aTLS.NewListener(listener, s.tlsConfig)
	}
	return s.httpServer.Serve(listener)
}

func (s *httpServer) ServePacket(listener net.PacketConn) error {
	return os.ErrInvalid
}

func (s *httpServer) Close() error {
	return common.Close(common.PtrOrNil(s.httpServer))
}
