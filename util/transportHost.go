package util

import "strings"

// TransportHost is the Host a ws or httpupgrade transport names itself, or ""
// for none (or another transport type). httpupgrade keeps it in its host
// field, ws in its Host header -- a Listable in sing-box, so a string or a
// list, of which a client sends the first. Each falls back to the other's
// place, as the Clash converter always read them; sing-box refuses a ws
// transport with a host field, so that fallback only ever sees an external
// link's.
func TransportHost(t map[string]interface{}) string {
	var header, field string
	if headers, ok := t["headers"].(map[string]interface{}); ok {
		for _, host := range AsStringList(headers["Host"]) {
			if host = strings.TrimSpace(host); host != "" {
				header = host
				break
			}
		}
	}
	field, _ = t["host"].(string)
	field = strings.TrimSpace(field)
	switch t["type"] {
	case "ws":
		if header != "" {
			return header
		}
		return field
	case "httpupgrade":
		if field != "" {
			return field
		}
		return header
	}
	return ""
}

// DefaultTransportHost is the Host a ws or httpupgrade transport should send
// when it names none: the TLS server_name. Left empty, sing-box and Xray send
// the server address, and a CDN reached through a clean-IP domain answers that
// with 403, since the Host belongs to no site on its side.
//
// It is "" when the transport has a Host of its own, TLS is off, or there is
// no server_name to use.
func DefaultTransportHost(transport interface{}, tls map[string]interface{}) string {
	if !AsBool(tls["enabled"]) {
		return ""
	}
	t, _ := transport.(map[string]interface{})
	if tt := t["type"]; tt != "ws" && tt != "httpupgrade" {
		return ""
	}
	if TransportHost(t) != "" {
		return ""
	}
	return TlsServerName(tls)
}

// SetTransportHost returns a copy of transport with host set in the field
// sing-box reads for its type. It copies because every address of an inbound
// shares the one transport map.
func SetTransportHost(transport map[string]interface{}, host string) map[string]interface{} {
	out := make(map[string]interface{}, len(transport)+1)
	for key, value := range transport {
		out[key] = value
	}
	if out["type"] == "httpupgrade" {
		out["host"] = host
		return out
	}
	headers := map[string]interface{}{"Host": host}
	if old, ok := transport["headers"].(map[string]interface{}); ok {
		for key, value := range old {
			if key != "Host" {
				headers[key] = value
			}
		}
	}
	out["headers"] = headers
	return out
}
