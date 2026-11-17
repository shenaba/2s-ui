package util

import "strings"

// IsQuicProtocol reports whether a protocol runs its TLS handshake inside
// QUIC: hysteria, hysteria2 and tuic.
func IsQuicProtocol(protocol string) bool {
	switch protocol {
	case "hysteria", "hysteria2", "tuic":
		return true
	}
	return false
}

// StripQuicTls drops the client TLS fields a QUIC protocol cannot use.
//
// uTLS forges a TCP ClientHello, and QUIC carries its own: sing-box builds a
// QUIC client from the standard library config, which a uTLS client refuses to
// hand over ("unsupported usage for uTLS"), so a tuic/hysteria outbound with
// utls enabled fails on every connection. One TLS template is commonly shared
// by a vless or trojan inbound, where a fingerprint is wanted, and a QUIC one,
// where it breaks the client -- so it is the QUIC client config that drops it,
// not the template.
func StripQuicTls(protocol string, tls map[string]interface{}) {
	if tls == nil || !IsQuicProtocol(protocol) {
		return
	}
	delete(tls, "utls")
}

// StripQuicOutboundTls is StripQuicTls on a whole sing-box outbound.
func StripQuicOutboundTls(outbound map[string]interface{}) {
	protocol, _ := outbound["type"].(string)
	tls, _ := outbound["tls"].(map[string]interface{})
	StripQuicTls(protocol, tls)
}

// DropQuicLinkFingerprint removes the fp parameter from a hysteria, hysteria2
// or tuic share link, for links stored before the generator stopped writing
// one (getTlsParams). A client that reads fp as uTLS cannot connect over QUIC.
//
// Plain string surgery rather than url.Parse: re-encoding would rewrite the
// userinfo and the remark of a link that is otherwise served byte for byte.
func DropQuicLinkFingerprint(uri string) string {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok {
		return uri
	}
	switch strings.ToLower(scheme) {
	case "hysteria", "hysteria2", "hy2", "tuic":
	default:
		return uri
	}
	body, fragment, hasFragment := strings.Cut(rest, "#")
	base, query, hasQuery := strings.Cut(body, "?")
	if !hasQuery {
		return uri
	}
	params := strings.Split(query, "&")
	kept := params[:0]
	for _, param := range params {
		if param == "fp" || strings.HasPrefix(param, "fp=") {
			continue
		}
		kept = append(kept, param)
	}
	if len(kept) == len(params) {
		return uri
	}
	out := scheme + "://" + base
	if len(kept) > 0 {
		out += "?" + strings.Join(kept, "&")
	}
	if hasFragment {
		out += "#" + fragment
	}
	return out
}
