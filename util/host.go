package util

import (
	"net/netip"
	"strings"
)

// NormalizeHost strips URI brackets from an IPv6 literal ("[::1]" -> "::1").
// Bare hosts, IPv4 addresses and domains are returned unchanged. Config
// formats -- sing-box JSON, Clash YAML, the vmess "add" field -- want the bare
// form; only a URI authority needs the brackets (#1220).
func NormalizeHost(host string) string {
	if len(host) > 2 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// HostForURI returns the host formatted for a URI authority: an IPv6 literal
// is wrapped in brackets so "host:port" stays parseable, everything else is
// returned unchanged. Bracketing is idempotent -- the input is normalized
// first, so an already-bracketed literal does not gain a second pair.
//
// The colon test is a real address parse, not a substring check: an address
// row's server field takes free text, and something like "example.com:443"
// contains a colon without being an IPv6 literal. Bracketing that would turn
// one malformed authority into a different malformed authority; leaving it
// alone at least keeps the operator's own spelling visible in the link.
func HostForURI(host string) string {
	h := NormalizeHost(host)
	if addr, err := netip.ParseAddr(h); err == nil && addr.Is6() {
		return "[" + h + "]"
	}
	return h
}
