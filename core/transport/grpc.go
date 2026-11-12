//go:build with_grpc

package transport

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raygrpc"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// forwardedMetadata is ForwardedHeader as gRPC metadata keys it.
const forwardedMetadata = "x-forwarded-for"

// newGRPCServer is v2ray.NewGRPCServer: grpc-go's server, unless the lite one
// is asked for. sing-box's grpc-go server is built for its Tun handler alone
// and registered on a grpc.Server of this package's, whose interceptor settles
// the client before Tun reads it.
func newGRPCServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayGRPCOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (adapter.V2RayServerTransport, error) {
	if options.ForceLite {
		return newGRPCLiteServer(ctx, logger, options, tlsConfig, handler, resolve)
	}
	upstream, err := v2raygrpc.NewServer(ctx, logger, options, nil, handler)
	if err != nil {
		return nil, err
	}
	var serverOptions []grpc.ServerOption
	if tlsConfig != nil {
		if !common.Contains(tlsConfig.NextProtos(), http2.NextProtoTLS) {
			tlsConfig.SetNextProtos(append([]string{"h2"}, tlsConfig.NextProtos()...))
		}
		serverOptions = append(serverOptions, grpc.Creds(v2raygrpc.NewTLSTransportCredentials(tlsConfig)))
	}
	if options.IdleTimeout > 0 {
		serverOptions = append(serverOptions, grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(options.IdleTimeout),
			Timeout: time.Duration(options.PingTimeout),
		}))
	}
	serverOptions = append(serverOptions, grpc.StreamInterceptor(forwardedInterceptor(resolve)))
	server := &grpcServer{grpc.NewServer(serverOptions...)}
	v2raygrpc.RegisterGunServiceCustomNameServer(server.Server, upstream, options.ServiceName)
	return server, nil
}

// forwardedInterceptor hands each stream on with the client resolve decides as
// its peer, and without the metadata it was decided on.
func forwardedInterceptor(resolve Resolver) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := stream.Context()
		var source M.Socksaddr
		from, hasPeer := peer.FromContext(ctx)
		if hasPeer && from.Addr != nil {
			source = M.SocksaddrFromNet(from.Addr)
		}
		incoming, _ := metadata.FromIncomingContext(ctx)
		forwarded := incoming.Get(forwardedMetadata)
		client := resolve(source, forwarded)
		if hasPeer {
			settled := *from
			settled.Addr = client.TCPAddr()
			ctx = peer.NewContext(ctx, &settled)
		}
		if len(forwarded) > 0 {
			incoming = incoming.Copy()
			delete(incoming, forwardedMetadata)
			ctx = metadata.NewIncomingContext(ctx, incoming)
		}
		return handler(srv, &serverStream{ServerStream: stream, ctx: ctx})
	}
}

type serverStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStream) Context() context.Context {
	return s.ctx
}

// grpcServer is v2raygrpc.Server's transport side, around a grpc.Server of
// this package's.
type grpcServer struct {
	*grpc.Server
}

// ReadsForwarded tells proxyheader that this transport takes the client from
// X-Forwarded-For, through the Resolver it was built with.
func (s *grpcServer) ReadsForwarded() bool {
	return true
}

func (s *grpcServer) Network() []string {
	return []string{N.NetworkTCP}
}

func (s *grpcServer) ServePacket(net.PacketConn) error {
	return os.ErrInvalid
}

func (s *grpcServer) Close() error {
	s.Server.Stop()
	return nil
}
