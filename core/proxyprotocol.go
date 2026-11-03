package core

import (
	"sync"

	"github.com/shenaba/2s-ui/core/proxyheader"

	"github.com/sagernet/sing-box/adapter"
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

// refreshProxyHeaders recomputes whether the dial guard is needed. It runs
// whenever an inbound table finishes changing and whenever a box starts or
// closes, which between them cover every way an inbound comes or goes.
func refreshProxyHeaders() {
	proxyheader.RefreshInUse(func() (used, settled bool) {
		proxyReaders.Lock()
		managers := make([]*inboundManager, 0, len(proxyReaders.managers))
		for m := range proxyReaders.managers {
			managers = append(managers, m)
		}
		proxyReaders.Unlock()
		for _, m := range managers {
			if readsProxyHeaders(m.settled()) {
				return true, true
			}
		}
		return false, true
	})
}

func readsProxyHeaders(inbounds []adapter.Inbound) bool {
	for _, inbound := range inbounds {
		if reader, isReader := inbound.(interface{ ReadsProxyHeaders() bool }); isReader && reader.ReadsProxyHeaders() {
			return true
		}
	}
	return false
}

// ReadsProxyHeaders reports whether an inbound in the running core reads PROXY
// protocol headers. It waits out an inbound being added or replaced, which can
// be accepting before it is listed. The copies under core/protocol/ that can
// read headers say so through ReadsProxyHeaders in their users.go.
func (c *Core) ReadsProxyHeaders() bool {
	box, err := c.running()
	if err != nil {
		return false
	}
	return readsProxyHeaders(box.inbound.settled())
}
