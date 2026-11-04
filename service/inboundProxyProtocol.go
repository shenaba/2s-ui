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

// ApplyProxyProtocol hands the core the proxyProtoTrusted setting, which it
// reads per connection and so cannot fetch from the database itself. Called
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

// checkProxyProtoFronts refuses a proxyProtoTrusted value whose ranges are too
// broad to be fronts.
func checkProxyProtoFronts(prefixes []netip.Prefix) error {
	for _, prefix := range prefixes {
		least := minFrontBits6
		if prefix.Addr().Is4() {
			least = minFrontBits4
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
	var listen struct {
		ProxyProtocol bool `json:"proxy_protocol"`
		Transport     struct {
			Type string `json:"type"`
		} `json:"transport"`
	}
	// An Options that does not parse is the core's to report, as before.
	if len(inbound.Options) == 0 || json.Unmarshal(inbound.Options, &listen) != nil {
		return nil
	}
	if listen.ProxyProtocol && !proxyProtocolTypes[inbound.Type] {
		return common.NewErrorf("inbound %s: PROXY protocol is only supported on vless, vmess, trojan and anytls inbounds, not %s", inbound.Tag, inbound.Type)
	}
	if listen.ProxyProtocol && listen.Transport.Type == "quic" {
		return common.NewErrorf("inbound %s: PROXY protocol is not supported over the QUIC transport, which carries no TCP connection to read a header from", inbound.Tag)
	}
	return dropAcceptNoHeader(inbound)
}

// dropAcceptNoHeader removes proxy_protocol_accept_no_header from the
// inbound's options, when it is there at all.
func dropAcceptNoHeader(inbound *model.Inbound) error {
	var options map[string]json.RawMessage
	if json.Unmarshal(inbound.Options, &options) != nil {
		return nil
	}
	if _, found := options["proxy_protocol_accept_no_header"]; !found {
		return nil
	}
	delete(options, "proxy_protocol_accept_no_header")
	raw, err := json.Marshal(options)
	if err != nil {
		return err
	}
	inbound.Options = raw
	return nil
}
