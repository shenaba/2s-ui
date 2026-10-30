package service

import (
	"encoding/json"
	"net/netip"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// ApplyProxyProtocol hands the core what it reads per connection and so
// cannot fetch from the database itself: the proxyProtoTrusted setting, and
// whether any inbound reads PROXY headers at all -- the dial guard costs
// clients something (named fronts refused, TCP Fast Open's round trip) and
// stays off otherwise. Called when the panel starts and after every settings
// or inbounds save, a failed inbounds save included: the core may have been
// changed and put back. A failure to read the setting leaves what the core had.
//
// Whether one is in use is asked of the running core, not the rows: the core
// takes a new inbound before its save commits, so the rows read by a save
// committing in between would switch the guard off under it. A restart under
// way is not waited for -- the save would answer only once it is done -- and
// its inbounds turn the guard on themselves as they are built.
func ApplyProxyProtocol() {
	var settingService SettingService
	prefixes, err := settingService.GetProxyProtoTrusted()
	if err != nil {
		logger.Warning("PROXY protocol: keeping the trusted fronts as they were: ", err)
	} else {
		proxyheader.SetTrusted(prefixes)
	}
	proxyheader.RefreshInUse(func() (used, settled bool) {
		if corePtr == nil {
			return false, true
		}
		if !startMu.TryRLock() {
			return false, false
		}
		defer startMu.RUnlock()
		return corePtr.ReadsProxyHeaders(), true
	})
}

// Narrowest ranges a trusted front may be given. Everything inside one is
// believed when it names a client, and is an address the core refuses to dial
// for clients: 0.0.0.0/0 would refuse every client without a header and every
// dial off this host, which stops the whole panel from relaying, and a range
// that wide is never one front.
const (
	minFrontBits4 = 8
	minFrontBits6 = 32
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
