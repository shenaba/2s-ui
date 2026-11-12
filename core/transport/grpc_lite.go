//go:build !with_grpc

package transport

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
)

// newGRPCServer is v2ray.NewGRPCServer in a build without grpc-go: the lite
// server, always.
func newGRPCServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayGRPCOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler, resolve Resolver) (adapter.V2RayServerTransport, error) {
	return newGRPCLiteServer(ctx, logger, options, tlsConfig, handler, resolve)
}
