package service

import (
	"encoding/json"
	"net/netip"
	"sync"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

var applyProxyProtocolMu sync.Mutex

// ApplyProxyProtocol hands the core the proxyProtoTrusted setting, and the
// xffTrustedCDN one (forwardedFor.go), which it reads per connection and so
// cannot fetch from the database itself. Called
// when the panel starts, after every settings save, and after a database
// import. A failure to read the setting leaves what the core had. A stored
// value that does not parse, or is too broad to be fronts, is not used at
// all: the core then trusts only this host, and says why.
//
// Whether any inbound reads PROXY headers -- which decides whether the dial
// guard runs at all -- is the core's own to track: it recomputes that every
// time its inbounds change, by whatever path.
func ApplyProxyProtocol() {
	// One at a time, each reading what is committed when it holds the lock:
	// two saves applying at once would otherwise leave whichever read first.
	applyProxyProtocolMu.Lock()
	defer applyProxyProtocolMu.Unlock()
	var settingService SettingService
	applyXFFTrustedCDN(&settingService)
	raw, err := settingService.getString("proxyProtoTrusted")
	if err != nil {
		logger.Warning("PROXY protocol: keeping the trusted fronts as they were: ", err)
		return
	}
	prefixes, err := parseProxyProtoFronts(raw)
	if err != nil {
		logger.Error("PROXY protocol: ignoring the stored trusted fronts, trusting only this host: ", err)
		proxyheader.SetTrusted(nil)
		return
	}
	proxyheader.SetTrusted(prefixes)
}

// Widest ranges a trusted front may be given. Everything inside one is
// believed when it names a client, and is an address the core refuses to dial
// for clients. A /16 holds a Docker bridge network or a VPC subnet; a /48 is
// a whole site's IPv6. Anything wider is never one front, and would let
// millions of hosts name any client to the IP limit and the online list.
const (
	minFrontBits4 = 16
	minFrontBits6 = 48
)

// nat64Prefixes are the IPv6 prefixes that embed IPv4 addresses: the
// well-known one (RFC 6052) and the local-use one (RFC 8215).
var nat64Prefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

// checkProxyProtoFronts refuses a proxyProtoTrusted value whose ranges are too
// broad to be fronts.
func checkProxyProtoFronts(prefixes []netip.Prefix) error {
	for _, prefix := range prefixes {
		least := minFrontBits6
		if prefix.Addr().Is4() {
			least = minFrontBits4
		}
		// A NAT64 prefix carries IPv4 addresses inside it, so its breadth is
		// that of the IPv4 part: 64:ff9b::/96 is every IPv4 host, seen from
		// an IPv6-only machine. Held to an IPv4 /16 there. Only the
		// well-known prefixes can be told: one a network chose for its own
		// translator is an ordinary IPv6 range to this check, and listing it
		// trusts every IPv4 client behind it.
		for _, nat64 := range nat64Prefixes {
			if prefix.Overlaps(nat64) {
				least = max(least, 128-32+minFrontBits4)
			}
		}
		if prefix.Bits() < least {
			return common.NewErrorf("trusted PROXY protocol front %s is too broad: every address in it would be believed for any client, and the core would refuse to dial it; name the fronts themselves (at least /%d)", prefix, least)
		}
		// This host is trusted without being listed. Listed, its loopback
		// would also count as a named front, which a client is refused when
		// it asks a relay for it -- every port of 127.0.0.1 through a relay.
		if addr := prefix.Masked().Addr(); addr.IsLoopback() || addr.IsUnspecified() {
			return common.NewErrorf("trusted PROXY protocol front %s is this host, which is always trusted; list only fronts elsewhere", prefix)
		}
	}
	return nil
}

// proxyProtocolTypes are the inbounds that can read a PROXY protocol header
// (#260): the TCP protocols this panel carries its own copy of, which is where
// the reading is hooked in (core/proxyheader). Every other type is sing-box's
// own, whose listener refuses the field with a message about it being removed
// in 1.6.0 -- true, and no help to the operator who just switched it on.
var proxyProtocolTypes = map[string]bool{
	"anytls": true,
	"trojan": true,
	"vless":  true,
	"vmess":  true,
}

// checkProxyProtocol refuses a PROXY protocol request the core cannot honour,
// before the core is touched: an inbound type without the hook, or the QUIC
// transport, which serves only UDP -- the switch would save, read nothing, and
// still turn the dial guard on.
//
// accept_no_header is dropped from the options rather than refused: a local
// connection must carry a header and a remote one is never read for one, so
// there is nothing for it to relax, and the core ignores it (proxyheader.Take).
// Refusing it left an inbound that arrived with it -- an imported database --
// impossible to save from the form, which has no switch for it.
func checkProxyProtocol(inbound *model.Inbound) error {
	// One decode serves both the checks and the drop.
	var options map[string]json.RawMessage
	// An Options that does not parse is the core's to report, as before.
	if len(inbound.Options) == 0 || json.Unmarshal(inbound.Options, &options) != nil {
		return nil
	}
	var enabled bool
	_ = json.Unmarshal(options["proxy_protocol"], &enabled)
	var transport struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(options["transport"], &transport)
	if enabled && !proxyProtocolTypes[inbound.Type] {
		return common.NewErrorf("inbound %s: PROXY protocol is only supported on vless, vmess, trojan and anytls inbounds, not %s", inbound.Tag, inbound.Type)
	}
	if enabled && transport.Type == "quic" {
		return common.NewErrorf("inbound %s: PROXY protocol is not supported over the QUIC transport, which carries no TCP connection to read a header from", inbound.Tag)
	}
	if _, found := options["proxy_protocol_accept_no_header"]; !found {
		return nil
	}
	// Only now re-encoded, so an inbound without the field is stored
	// byte for byte as it came.
	delete(options, "proxy_protocol_accept_no_header")
	raw, err := json.Marshal(options)
	if err != nil {
		return err
	}
	inbound.Options = raw
	return nil
}

// dropUnsupportedProxyProtocol removes the PROXY protocol request from an
// inbound of a type that cannot honour it, on its way to the core. A save
// refuses one, but a row can arrive without one (an imported database), and
// sing-box's listener refuses to start with it -- which fails the whole core,
// every inbound with it. Dropped, that inbound serves as it would without.
func dropUnsupportedProxyProtocol(inboundJson []byte, inboundType string) []byte {
	if proxyProtocolTypes[inboundType] {
		return inboundJson
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(inboundJson, &options) != nil {
		return inboundJson
	}
	_, asked := options["proxy_protocol"]
	_, acceptNoHeader := options["proxy_protocol_accept_no_header"]
	if !asked && !acceptNoHeader {
		return inboundJson
	}
	delete(options, "proxy_protocol")
	delete(options, "proxy_protocol_accept_no_header")
	raw, err := json.Marshal(options)
	if err != nil {
		return inboundJson
	}
	var tag string
	_ = json.Unmarshal(options["tag"], &tag)
	logger.Warning("inbound ", tag, ": PROXY protocol is not supported on ", inboundType, " inbounds; started without it")
	return raw
}
