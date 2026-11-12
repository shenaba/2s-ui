package naive

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/core/transport"

	"github.com/sagernet/sing/common/logger"
)

// source judges X-Forwarded-For as proxyheader.Forwarded does, and leaves
// none of it on the request. core/naive_forwarded_test.go covers a client
// speaking for itself end to end; a CDN edge cannot connect from loopback, so
// it is covered here.
func TestSource(t *testing.T) {
	proxyheader.SetCDN([]netip.Prefix{netip.MustParsePrefix("104.16.0.0/13")})
	t.Cleanup(func() { proxyheader.SetCDN(nil) })
	inbound := &Inbound{logger: logger.NOP()}
	for _, tc := range []struct {
		name, remote string
		forwarded    []string
		want         string
	}{
		{"no header", "198.51.100.7:40000", nil, "198.51.100.7:40000"},
		{"client speaking for itself", "198.51.100.7:40000", []string{"1.2.3.4"}, "198.51.100.7:40000"},
		{"CDN edge, last entry only", "104.16.0.1:40000", []string{"1.2.3.4, 203.0.113.9"}, "203.0.113.9:40000"},
		{"CDN edge, header repeated", "104.16.0.1:40000", []string{"1.2.3.4", "203.0.113.9"}, "203.0.113.9:40000"},
		{"this host", "127.0.0.1:40000", []string{"203.0.113.9"}, "127.0.0.1:40000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &http.Request{RemoteAddr: tc.remote, Header: make(http.Header)}
			for _, value := range tc.forwarded {
				request.Header.Add(transport.ForwardedHeader, value)
			}
			if got := inbound.source(request).String(); got != tc.want {
				t.Fatalf("source %s, want %s", got, tc.want)
			}
			if values := request.Header.Values(transport.ForwardedHeader); len(values) != 0 {
				t.Fatalf("X-Forwarded-For left on the request: %q", values)
			}
		})
	}
}
