package core

import (
	"sync"

	"github.com/shenaba/2s-ui/core/proxyheader"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// proxyReaders is the inbound managers of every box started and not yet
// closed. Whether any inbound reads PROXY protocol headers (#260) is a
// question about all of them: a box being built for a restart, or a second
// core in a test, must not switch the dial guard off under another's.
var proxyReaders = struct {
	sync.Mutex
	managers map[*inboundManager]struct{}
}{managers: make(map[*inboundManager]struct{})}

// startReadingProxyHeaders counts m's inbounds from now on. Called before
// the box starts any of them, so a look from elsewhere cannot miss one that
// is accepting.
func startReadingProxyHeaders(m *inboundManager) {
	proxyReaders.Lock()
	proxyReaders.managers[m] = struct{}{}
	proxyReaders.Unlock()
	refreshProxyHeaders()
}

// stopReadingProxyHeaders stops counting m's inbounds, once its box has
// closed them.
func stopReadingProxyHeaders(m *inboundManager) {
	proxyReaders.Lock()
	_, counted := proxyReaders.managers[m]
	delete(proxyReaders.managers, m)
	proxyReaders.Unlock()
	if counted {
		refreshProxyHeaders()
	}
}

// refreshProxyHeaders recomputes the ports the dial guard watches over. It
// runs whenever an inbound table finishes changing and whenever a box starts
// or closes, which between them cover every way an inbound comes or goes.
func refreshProxyHeaders() {
	proxyheader.RefreshInUse(func() []uint16 {
		proxyReaders.Lock()
		managers := make([]*inboundManager, 0, len(proxyReaders.managers))
		for m := range proxyReaders.managers {
			managers = append(managers, m)
		}
		proxyReaders.Unlock()
		var ports []uint16
		for _, m := range managers {
			ports = append(ports, proxyHeaderPorts(m.settled())...)
		}
		return ports
	})
}

// proxyHeaderPortReader is a copy under core/protocol/ that can read PROXY
// headers, saying whether it does and on which port (its users.go).
type proxyHeaderPortReader interface {
	ProxyHeaderPort() (uint16, bool)
}

func proxyHeaderPorts(inbounds []adapter.Inbound) []uint16 {
	var ports []uint16
	for _, inbound := range inbounds {
		if reader, isReader := inbound.(proxyHeaderPortReader); isReader {
			if port, reads := reader.ProxyHeaderPort(); reads {
				ports = append(ports, port)
			}
		}
	}
	return ports
}

// proxyRoute is what proxyheader.GuardDial needs to know about the outbound a
// client is dialed through -- the last of the chain the router resolved, so
// never a group -- whether it is direct, and where its server is. A group is
// still followed, for a tracker called without a chain. A direct outbound with
// a detour dials through another outbound, and is not direct.
func (m *outboundManager) proxyRoute(outbound adapter.Outbound) proxyheader.Route {
	for range 8 {
		group, isGroup := outbound.(adapter.OutboundGroup)
		if !isGroup {
			break
		}
		outbound = group.Selected(N.NetworkTCP)
	}
	if outbound == nil {
		return proxyheader.Route{}
	}
	options := m.options(outbound.Tag())
	if options == nil && m.endpoint != nil {
		if endpoints, isTable := m.endpoint.(*endpointManager); isTable {
			options = endpoints.options(outbound.Tag())
		}
	}
	if outbound.Type() == C.TypeDirect {
		direct, isDirect := options.(*option.DirectOutboundOptions)
		return proxyheader.Route{Direct: !isDirect || direct.Detour == ""}
	}
	var route proxyheader.Route
	if server, hasServer := options.(interface{ Build() M.Socksaddr }); hasServer {
		route.Server = server.Build()
	}
	return route
}
