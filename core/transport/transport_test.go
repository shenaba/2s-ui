package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	sHttp "github.com/sagernet/sing/protocol/http"

	"golang.org/x/net/http2"
)

const testForwarded = "1.2.3.4, 203.0.113.9"

// settled is the client every test resolver decides on.
var settled = M.ParseSocksaddr("198.51.100.7")

// sourceHandler records the source sing-box's transport server hands on.
type sourceHandler struct {
	sources chan M.Socksaddr
}

func (h sourceHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.sources <- source
	conn.Close()
	if onClose != nil {
		onClose(nil)
	}
}

// recordingResolver decides settled for every request, keeping what it was
// asked.
type recordingResolver struct {
	sync.Mutex
	peer      M.Socksaddr
	forwarded []string
}

func (r *recordingResolver) resolve(peer M.Socksaddr, forwarded []string) M.Socksaddr {
	r.Lock()
	defer r.Unlock()
	r.peer, r.forwarded = peer, forwarded
	return M.SocksaddrFrom(settled.Addr, peer.Port)
}

// serve starts a transport of options' type and returns where it listens.
func serve(t *testing.T, options option.V2RayTransportOptions) (string, *recordingResolver, chan M.Socksaddr) {
	t.Helper()
	resolver := &recordingResolver{}
	handler := sourceHandler{sources: make(chan M.Socksaddr, 1)}
	server, err := NewServer(context.Background(), logger.NOP(), options, nil, handler, resolver.resolve)
	if err != nil {
		t.Fatal(err)
	}
	if reader, reads := server.(interface{ ReadsForwarded() bool }); !reads || !reader.ReadsForwarded() {
		t.Fatalf("a %s transport does not say it reads X-Forwarded-For", options.Type)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(l)
	t.Cleanup(func() { server.Close() })
	return l.Addr().String(), resolver, handler.sources
}

// check has the request's source be the resolver's, which it is only if the
// header did not reach sing-box: its server prefers the header to RemoteAddr.
func check(t *testing.T, resolver *recordingResolver, sources chan M.Socksaddr) {
	t.Helper()
	var source M.Socksaddr
	select {
	case source = <-sources:
	case <-time.After(5 * time.Second):
		t.Fatal("the transport never handed the connection on")
	}
	resolver.Lock()
	defer resolver.Unlock()
	if !resolver.peer.Addr.IsLoopback() {
		t.Fatalf("the resolver was given %s as the peer, want the connection's loopback address", resolver.peer)
	}
	if strings.Join(resolver.forwarded, ",") != testForwarded {
		t.Fatalf("the resolver was given %q, want the header as sent", resolver.forwarded)
	}
	if source.Addr != settled.Addr || source.Port != resolver.peer.Port {
		t.Fatalf("sing-box handed on %s, want the resolver's %s with the peer's port", source, settled)
	}
}

// h2cClient speaks prior-knowledge HTTP/2 without TLS.
func h2cClient() *http.Client {
	return &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

// h2cRequest sends a request whose body stays open, as a tunnel's does.
func h2cRequest(t *testing.T, method, url string, header http.Header) {
	t.Helper()
	body, writer := io.Pipe()
	t.Cleanup(func() { writer.Close() })
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = header
	request.Header.Set("X-Forwarded-For", testForwarded)
	go func() {
		response, err := h2cClient().Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()
}

// The http transport over prior-knowledge HTTP/2, whose streams sing-box's own
// h2c handler would hand straight to its server.
func TestHTTPOverH2C(t *testing.T) {
	addr, resolver, sources := serve(t, option.V2RayTransportOptions{
		Type:        "http",
		HTTPOptions: option.V2RayHTTPOptions{Path: "/h2"},
	})
	h2cRequest(t, http.MethodPut, "http://"+addr+"/h2", http.Header{})
	check(t, resolver, sources)
}

func TestGRPCLite(t *testing.T) {
	addr, resolver, sources := serve(t, option.V2RayTransportOptions{
		Type:        "grpc",
		GRPCOptions: option.V2RayGRPCOptions{ServiceName: "gun", ForceLite: true},
	})
	h2cRequest(t, http.MethodPost, "http://"+addr+"/gun/Tun", http.Header{"Content-Type": {"application/grpc"}})
	check(t, resolver, sources)
}

// sing-box's servers take the client from sHttp.SourceAddress, and only
// ForwardedHeader is taken off a request before they do. Were a sing release
// to start reading another of the headers proxies use for this, it would be
// believed from anyone again; so a request carrying all of them, once through
// FromRequest, must leave SourceAddress with nothing but RemoteAddr.
func TestSourceAddressReadsNoOtherHeader(t *testing.T) {
	request := &http.Request{RemoteAddr: "198.51.100.7:40000", Header: http.Header{}}
	for _, name := range []string{
		ForwardedHeader, "X-Real-IP", "Forwarded", "True-Client-IP", "CF-Connecting-IP",
		"X-Client-IP", "X-Cluster-Client-IP", "Fastly-Client-IP", "X-Original-Forwarded-For",
	} {
		request.Header.Set(name, "1.2.3.4")
	}
	request.Header.Set("Forwarded", "for=1.2.3.4")
	FromRequest(request, func(peer M.Socksaddr, _ []string) M.Socksaddr { return peer })
	if got := sHttp.SourceAddress(request); got.String() != "198.51.100.7:40000" {
		t.Fatalf("SourceAddress took %s from the request, want its RemoteAddr: a header other than %s is read", got, ForwardedHeader)
	}
}
