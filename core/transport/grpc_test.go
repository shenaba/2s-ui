//go:build with_grpc

package transport

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raygrpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// grpc-go's transport, which took the last entry of the metadata from anyone.
func TestGRPC(t *testing.T) {
	addr, resolver, sources := serve(t, option.V2RayTransportOptions{
		Type:        "grpc",
		GRPCOptions: option.V2RayGRPCOptions{ServiceName: "gun"},
	})
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(metadata.AppendToOutgoingContext(context.Background(), forwardedMetadata, testForwarded))
	defer cancel()
	client := v2raygrpc.NewGunServiceClient(conn).(v2raygrpc.GunServiceCustomNameClient)
	stream, err := client.TunCustomName(ctx, "gun")
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&v2raygrpc.Hunk{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	check(t, resolver, sources)
}
