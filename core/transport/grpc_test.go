//go:build with_grpc

package transport

import (
	"context"
	"errors"
	"io"
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
	// NewConnectionEx deliberately closes the server side as soon as it records
	// the resolved source. The client may therefore observe EOF while sending.
	// That is fine only if check confirms the server handled the stream.
	if err = stream.Send(&v2raygrpc.Hunk{Data: []byte("x")}); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	check(t, resolver, sources)
}
