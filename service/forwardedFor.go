package service

import (
	"net/netip"
	"strings"
	"unicode"

	"github.com/shenaba/2s-ui/core/proxyheader"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// xffTrustedCDN is the setting naming the CDN edges whose X-Forwarded-For an
// inbound believes, for the one entry an edge appends (#279; see
// core/proxyheader/forwarded.go). Its default is the keyword below, which
// stands for Cloudflare's published ranges as this build knows them, so that
// a panel behind Cloudflare keeps its clients' addresses without the operator
// having to list them -- and keeps up when a later build updates them.
const xffTrustedCDNKey = "xffTrustedCDN"

// cdnCloudflare is the keyword for Cloudflare's ranges.
const cdnCloudflare = "cloudflare"

// cloudflareRanges is https://www.cloudflare.com/ips/ as of this build.
var cloudflareRanges = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

// Widest ranges a CDN entry may be given. A CDN's own are wide (Cloudflare's
// widest are an IPv4 /13 and an IPv6 /29), but an edge is believed for every
// client it names, and a range wider than these is no one network's.
const (
	minCDNBits4 = 8
	minCDNBits6 = 16
)

// parseXFFTrustedCDN reads an xffTrustedCDN value: addresses, CIDRs and the
// keyword cloudflare, separated by commas or spaces.
func parseXFFTrustedCDN(raw string) ([]netip.Prefix, error) {
	var listed []string
	var prefixes []netip.Prefix
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		if strings.EqualFold(part, cdnCloudflare) {
			for _, cidr := range cloudflareRanges {
				prefixes = append(prefixes, netip.MustParsePrefix(cidr))
			}
			continue
		}
		listed = append(listed, part)
	}
	own, err := parseProxyProtoTrusted(strings.Join(listed, ","))
	if err != nil {
		return nil, err
	}
	for _, prefix := range own {
		least := minCDNBits6
		if prefix.Addr().Is4() {
			least = minCDNBits4
		}
		if prefix.Bits() < least {
			return nil, common.NewErrorf("trusted CDN range %s is too broad: every address in it would be believed for any client it names (at least /%d)", prefix, least)
		}
		// This host is never a CDN edge, and a client can have the core dial
		// it: listed, it would let a client speak for itself.
		if addr := prefix.Masked().Addr(); addr.IsLoopback() || addr.IsUnspecified() {
			return nil, common.NewErrorf("trusted CDN range %s is this host; a reverse proxy here is believed by inbounds that listen on 127.0.0.1", prefix)
		}
	}
	return append(prefixes, own...), nil
}

// applyXFFTrustedCDN hands the core the xffTrustedCDN setting. A stored value
// that does not parse is not used at all: the core then believes no CDN, and
// says why -- the clients behind one are seen as its edges, which is wrong but
// not exploitable.
func applyXFFTrustedCDN(settingService *SettingService) {
	raw, err := settingService.getString(xffTrustedCDNKey)
	if err != nil {
		logger.Warning("X-Forwarded-For: keeping the trusted CDN ranges as they were: ", err)
		return
	}
	prefixes, err := parseXFFTrustedCDN(raw)
	if err != nil {
		logger.Error("X-Forwarded-For: ignoring the stored trusted CDN ranges, believing no CDN: ", err)
		proxyheader.SetCDN(nil)
		return
	}
	proxyheader.SetCDN(prefixes)
}
