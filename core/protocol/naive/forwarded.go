package naive

import (
	"net/http"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/core/transport"

	upstream "github.com/sagernet/sing-box/protocol/naive"
	// The HTTP/3 server: it installs itself on the upstream package.
	_ "github.com/sagernet/sing-box/protocol/naive/quic"
	M "github.com/sagernet/sing/common/metadata"
)

// sing-box's naive/quic package fills in ConfigureHTTP3ListenerFunc and
// WrapError on the upstream naive package, from its init. The copy declares
// its own, which nothing would fill, so they are taken from upstream here:
// imported packages are initialized before this one, so both are set by now.
// The server is handed this package's Inbound as its handler, so HTTP/3
// requests come through the copy's ServeHTTP like the others.
func init() {
	ConfigureHTTP3ListenerFunc = upstream.ConfigureHTTP3ListenerFunc
	WrapError = upstream.WrapError
}

// source is sHttp.SourceAddress with X-Forwarded-For believed only as
// proxyheader.Forwarded believes it (#279): from a CDN edge, for the entry it
// appended. The header is taken off the request either way, so nothing after
// this sees what the client wrote.
//
// A reverse proxy on this host is never believed (hostForwards false), even
// for an inbound listening on loopback, unlike over core/transport. A front
// for naive has to carry its CONNECT tunnels, padding and all, which a
// reverse proxy that appends the header -- nginx's http module, a CDN --
// does not; and believing this host would put the inbound's port under the
// dial guard for a front that is not there. A front that only passes bytes
// adds no header, and its clients are served as this host's own address, as
// they were before.
func (n *Inbound) source(request *http.Request) M.Socksaddr {
	return transport.FromRequest(request, func(peer M.Socksaddr, forwarded []string) M.Socksaddr {
		return proxyheader.Forwarded(n.logger, peer, forwarded, false)
	})
}
