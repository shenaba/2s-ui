package proxyheader

import (
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// X-Forwarded-For (#279). On a transport that speaks HTTP (ws, http,
// httpupgrade, grpc), sing-box takes the client from the request's
// X-Forwarded-For whenever it carries one, from whoever sent it, so any client
// could name its own address to the online list and the per-client IP limit,
// one per connection. core/transport puts Forwarded in front of that instead:
// the header is believed only from a peer that has a reason to send it, and
// only as far as that peer vouches for it.
//
// Two kinds of peer do:
//
//   - A CDN edge (SetCDN: Cloudflare's ranges by default). It appends the
//     address that connected to it, so the last entry is the client and
//     anything before it is the client's own to write. That one entry is all
//     an edge is believed for, even when it is itself in a CDN range: a
//     client reaching the edge through a relay inside one would otherwise
//     have the edge vouch for whatever the client wrote before it. Trusting
//     a CDN's ranges trusts whatever that CDN sends, including another
//     account's zone or Worker pointed at this server; only the CDN's own
//     origin authentication (Cloudflare's Authenticated Origin Pulls) or an
//     empty list rules that out, which the setting's hint says.
//
//   - This host, for an inbound that listens on loopback only and reads no
//     PROXY header (Settings.TrustsHost): nothing else can reach one, so what connects is
//     a reverse proxy on this host -- an nginx terminating TLS for it, which
//     may itself sit behind a CDN, and then the entry it vouches for is an
//     edge, which vouches for one more. A second reverse proxy on this host
//     in front of the first is not followed: the entry it would vouch for is
//     the one a client that had the core dial the first proxy wrote itself.
//     The core is on this host too, and a
//     client could ask it for 127.0.0.1 and that port and write the header
//     itself, so such an inbound's port is watched by the dial guard exactly
//     as a PROXY-enabled inbound's is (see the package comment), and nothing
//     the guard does not cover is believed. An inbound listening anywhere
//     else is not believed from this host at all: its port is one clients ask
//     for (443), and guarding it would refuse them every relay to it.
//
// Everything else -- an untrusted peer, or a trusted one whose header is
// missing or does not parse where it has to be read -- is the peer itself. The
// header never travels on: the transport sees the address decided here and
// nothing to second-guess it with.

// cdn holds the CDN edges believed for one entry of X-Forwarded-For. The
// service layer keeps it in step with the xffTrustedCDN setting.
var cdn atomic.Pointer[[]netip.Prefix]

// SetCDN replaces the CDN edges believed for X-Forwarded-For.
func SetCDN(prefixes []netip.Prefix) {
	prefixes = slices.Clone(prefixes)
	cdn.Store(&prefixes)
}

// cdnEdge reports whether a peer at addr is a CDN edge. An address of this
// host's never is, whatever range it falls in: a client can have the core
// dial this host, and the guard watches only the ports of the inbounds that
// believe this host. A list that holds this host -- a server inside a range
// listed whole, a mistyped prefix -- would otherwise let any client speak
// for itself through the core.
func cdnEdge(addr netip.Addr) bool {
	prefixes := cdn.Load()
	if prefixes == nil || !containsAddr(*prefixes, addr) {
		return false
	}
	return !addr.IsLoopback() && !addr.IsUnspecified() && !hostAddrs.containsNow(addr)
}

func containsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// hostForwardsWarning spaces out the warning about a header from this host that
// is not believed.
var hostForwardsWarning warnLimiter

// Forwarded returns the client of a request that arrived from peer with the
// X-Forwarded-For values forwarded, in the order the request carries them.
// hostForwards is the inbound's Settings.TrustsHost. The port is always
// peer's: the header carries none, and the connection is still the peer's.
func Forwarded(logger logger.ContextLogger, peer M.Socksaddr, forwarded []string, hostForwards bool) M.Socksaddr {
	peer = peer.Unwrap()
	addr := plain(peer.Addr)
	if !addr.IsValid() || len(forwarded) == 0 {
		return peer
	}
	hops := forwardedHops(forwarded)
	// pop takes the entry the current peer vouches for, the last one left. One
	// that does not parse ends the walk where it is: entries are appended by
	// the hops that were believed, so a bad one means the chain is not what it
	// claims, and skipping it would let the client's own entries through.
	pop := func() (netip.Addr, bool) {
		if len(hops) == 0 {
			return netip.Addr{}, false
		}
		next, err := netip.ParseAddr(hops[len(hops)-1])
		hops = hops[:len(hops)-1]
		if err != nil {
			return netip.Addr{}, false
		}
		return plain(next), true
	}
	client := addr
	if addr.IsLoopback() {
		if !hostForwards {
			hostForwardsWarning.warnf(logger, time.Now(), "X-Forwarded-For from ", peer, heldBack{}, " is not believed: ",
				"a reverse proxy on this host is believed only over ws, http, httpupgrade and gRPC, by an inbound that ",
				"listens on 127.0.0.1 or ::1, so its clients are served as this host's own address")
			return peer
		}
		next, ok := pop()
		if !ok {
			return peer
		}
		// Loopback again is where the walk ends, not a hop to skip: the guard
		// watches this inbound's port, not the reverse proxy's. A client that
		// has the core dial the proxy (127.0.0.1:443) arrives at it from
		// loopback, and the proxy appends that behind whatever the client
		// wrote.
		client = next
		if client.IsLoopback() {
			return M.SocksaddrFrom(client, peer.Port)
		}
	}
	if cdnEdge(client) {
		if next, ok := pop(); ok {
			client = next
		}
	}
	return M.SocksaddrFrom(client, peer.Port)
}

// forwardedHops splits X-Forwarded-For values into their entries, in order. A
// request may carry the header more than once; a proxy that appends to it adds
// to the last.
func forwardedHops(forwarded []string) []string {
	var hops []string
	for _, value := range forwarded {
		for hop := range strings.SplitSeq(value, ",") {
			hop = strings.TrimSpace(hop)
			// An entry may carry a port, and IPv6 then comes bracketed.
			if addrPort, err := netip.ParseAddrPort(hop); err == nil {
				hop = addrPort.Addr().String()
			} else {
				hop = strings.TrimSuffix(strings.TrimPrefix(hop, "["), "]")
			}
			hops = append(hops, hop)
		}
	}
	return hops
}
