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
// before the core is touched. accept_no_header is refused outright: a local
// connection must carry a header and a remote one is never read for one, so
// there is nothing left for it to relax. So is the QUIC transport, which
// serves only UDP: the switch would save, read nothing, and still turn the
// dial guard on.
func checkProxyProtocol(inbound *model.Inbound) error {
	var listen struct {
		ProxyProtocol  bool `json:"proxy_protocol"`
		AcceptNoHeader bool `json:"proxy_protocol_accept_no_header"`
		Transport      struct {
			Type string `json:"type"`
		} `json:"transport"`
	}
	// An Options that does not parse is the core's to report, as before.
	if len(inbound.Options) == 0 || json.Unmarshal(inbound.Options, &listen) != nil {
		return nil
	}
	if listen.AcceptNoHeader {
		return common.NewErrorf("inbound %s: proxy_protocol_accept_no_header is not supported; a connection from this host must carry a PROXY header", inbound.Tag)
	}
	if listen.ProxyProtocol && !proxyProtocolTypes[inbound.Type] {
		return common.NewErrorf("inbound %s: PROXY protocol is only supported on vless, vmess, trojan and anytls inbounds, not %s", inbound.Tag, inbound.Type)
	}
	if listen.ProxyProtocol && listen.Transport.Type == "quic" {
		return common.NewErrorf("inbound %s: PROXY protocol is not supported over the QUIC transport, which carries no TCP connection to read a header from", inbound.Tag)
	}
	return nil
}
