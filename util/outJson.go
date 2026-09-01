package util

import (
	"encoding/json"

	"github.com/shenaba/2s-ui/util/common"

	"github.com/shenaba/2s-ui/database/model"
)

// Fill Inbound's out_json
func FillOutJson(i *model.Inbound, hostname string) error {
	switch i.Type {
	// Listeners with no client half at all: they either accept nothing from
	// outside (direct, tun, redirect, tproxy) or dial out to reach their own
	// edge (cloudflared).
	case "direct", "tun", "redirect", "tproxy", "cloudflared":
		return nil
	}
	var outJson map[string]interface{}
	err := json.Unmarshal(i.OutJson, &outJson)
	if err != nil {
		return err
	}

	if outJson == nil {
		outJson = make(map[string]interface{})
	}

	if i.TlsId > 0 {
		addTls(&outJson, i.Tls)
	} else {
		delete(outJson, "tls")
	}

	inbound, err := i.MarshalFull()
	if err != nil {
		return err
	}

	outJson["type"] = i.Type
	outJson["tag"] = i.Tag
	outJson["server"] = NormalizeHost(hostname)
	outJson["server_port"] = (*inbound)["listen_port"]

	switch i.Type {
	case "http", "socks", "mixed", "anytls":
	case "naive":
		naiveOut(&outJson, *inbound)
	case "shadowsocks":
		shadowsocksOut(&outJson, *inbound)
	case "snell":
		snellOut(&outJson, *inbound)
	case "shadowtls":
		shadowTlsOut(&outJson, *inbound)
	case "hysteria":
		hysteriaOut(&outJson, *inbound)
	case "hysteria2":
		hysteria2Out(&outJson, *inbound)
	case "tuic":
		tuicOut(&outJson, *inbound)
	case "vless":
		vlessOut(&outJson, *inbound)
	case "trojan":
		trojanOut(&outJson, *inbound)
	case "vmess":
		vmessOut(&outJson, *inbound)
	default:
		for key := range outJson {
			delete(outJson, key)
		}
	}

	i.OutJson, err = json.MarshalIndent(outJson, "", "  ")
	if err != nil {
		return err
	}

	return nil
}

// serverOnlyTlsFields are inbound-side TLS fields that must never reach a
// client: certificate_path/key_path point at files on the panel host, key is
// the server private key, and acme / certificate_provider are the panel's
// issuance config. A leaked certificate_path makes sing-box clients fail
// outright on a missing file (issue #51); a leaked certificate_provider is a
// tag that names nothing in the client's own config, which sing-box refuses to
// start on.
//
// acme stays listed even though sing-box 1.14 replaced it with
// certificate_provider: rows written before the migration still carry it, and
// the panel is not the only thing that ever wrote this column.
var serverOnlyTlsFields = []string{"certificate_path", "key", "key_path", "acme", "certificate_provider"}

// serverOnlyEchFields hold the server's ECH private key inside the nested
// ech object; the client-side ech fields (config, config_path, ...) stay.
var serverOnlyEchFields = []string{"key", "key_path"}

// StripServerTlsFields removes server-only TLS fields from a client-facing
// TLS object in place and reports whether anything was removed.
func StripServerTlsFields(tls map[string]interface{}) bool {
	changed := false
	for _, field := range serverOnlyTlsFields {
		if _, ok := tls[field]; ok {
			delete(tls, field)
			changed = true
		}
	}
	if ech, ok := tls["ech"].(map[string]interface{}); ok {
		for _, field := range serverOnlyEchFields {
			if _, ok := ech[field]; ok {
				delete(ech, field)
				changed = true
			}
		}
	}
	return changed
}

// addTls function
func addTls(out *map[string]interface{}, tls *model.Tls) {
	var tlsServer, tlsConfig map[string]interface{}
	err := json.Unmarshal(tls.Server, &tlsServer)
	if err != nil {
		return
	}
	err = json.Unmarshal(tls.Client, &tlsConfig)
	if err != nil {
		return
	}
	if tlsConfig == nil {
		// A literal "null" client column unmarshals to a nil map and the
		// assignments below would panic on it.
		tlsConfig = map[string]interface{}{}
	}

	if enabled, ok := tlsServer["enabled"]; ok {
		tlsConfig["enabled"] = enabled
	}
	if serverName, ok := tlsServer["server_name"]; ok {
		tlsConfig["server_name"] = serverName
	}
	if alpn, ok := tlsServer["alpn"]; ok {
		tlsConfig["alpn"] = alpn
	}
	if minVersion, ok := tlsServer["min_version"]; ok {
		tlsConfig["min_version"] = minVersion
	}
	if maxVersion, ok := tlsServer["max_version"]; ok {
		tlsConfig["max_version"] = maxVersion
	}
	if _, pinned := tlsConfig["certificate_public_key_sha256"]; !pinned {
		if certificate, ok := tlsServer["certificate"]; ok {
			tlsConfig["certificate"] = certificate
		}
	}
	if cipherSuites, ok := tlsServer["cipher_suites"]; ok {
		tlsConfig["cipher_suites"] = cipherSuites
	}
	if reality, ok := tlsServer["reality"].(map[string]interface{}); ok && reality["enabled"].(bool) {
		realityConfig := tlsConfig["reality"].(map[string]interface{})
		realityConfig["enabled"] = true
		if shortIDs, ok := reality["short_id"].([]interface{}); ok && len(shortIDs) > 0 {
			realityConfig["short_id"] = shortIDs[common.RandomInt(len(shortIDs))]
		}
		tlsConfig["reality"] = realityConfig
	}
	if ech, ok := tlsServer["ech"].(map[string]interface{}); ok && ech["enabled"].(bool) {
		echConfig := tlsConfig["ech"].(map[string]interface{})
		echConfig["enabled"] = true
		echConfig["pq_signature_schemes_enabled"] = ech["pq_signature_schemes_enabled"]
		echConfig["dynamic_record_sizing_disabled"] = ech["dynamic_record_sizing_disabled"]
		tlsConfig["ech"] = echConfig
	}

	// The client config is stored alongside the server config and may carry
	// server-only fields (legacy rows, upstream imports) — never ship them.
	StripServerTlsFields(tlsConfig)

	(*out)["tls"] = tlsConfig
}

func naiveOut(out *map[string]interface{}, inbound map[string]interface{}) {
	if quic_congestion_control, ok := inbound["quic_congestion_control"].(string); ok {
		(*out)["quic"] = true
		switch quic_congestion_control {
		case "bbr_standard":
			(*out)["quic_congestion_control"] = "bbr"
		case "bbr2_variant":
			(*out)["quic_congestion_control"] = "bbr2"
		default:
			(*out)["quic_congestion_control"] = quic_congestion_control
		}
	}

}

func shadowsocksOut(out *map[string]interface{}, inbound map[string]interface{}) {
	if method, ok := inbound["method"].(string); ok {
		(*out)["method"] = method
	}
}

func shadowTlsOut(out *map[string]interface{}, inbound map[string]interface{}) {
	if version, ok := inbound["version"].(float64); ok && int(version) == 3 {
		(*out)["version"] = 3
	} else {
		for key := range *out {
			delete(*out, key)
		}
	}
	(*out)["tls"] = map[string]interface{}{"enabled": true}
}

// hysteriaQUICFieldsFromInbound are the transport options seeded from the
// listener into the client config. sing-box 1.14 gives both sides the same
// names, so they copy across unchanged -- unlike the bandwidths, which swap.
// They are the same two the deprecated recv_window_conn / disable_mtu_discovery
// used to carry, which is why the stream window is not among them: that one has
// always been the client's own, set in the client-config tab.
//
// A value already in out_json is the operator's override and wins, the same way
// an explicit network does on a shadowsocks client config. Clearing the field
// there lets the listener's value seed it again on the next save.
var hysteriaQUICFieldsFromInbound = []string{"connection_receive_window", "disable_path_mtu_discovery"}

// hysteriaDeprecatedOutFields are the names hysteria used before sing-box 1.14
// gave every QUIC protocol the same ones, mapped to their replacements as the
// outbound side spells them -- what an inbound calls recv_window_client an
// outbound calls recv_window, and both mean the stream window.
//
// migrateHysteriaQUICFields renames these in every stored row once, so a client
// config only carries one afterwards if it was hand-edited or restored from a
// pre-1.14 backup. Folding it in rather than deleting it means such a row heals
// itself on the next save instead of silently losing the value; the modern name
// wins when both are present, matching how sing-box reads them.
var hysteriaDeprecatedOutFields = map[string]string{
	"recv_window_conn":      "connection_receive_window",
	"recv_window":           "stream_receive_window",
	"disable_mtu_discovery": "disable_path_mtu_discovery",
}

func hysteriaOut(out *map[string]interface{}, inbound map[string]interface{}) {
	delete(*out, "down_mbps")
	delete(*out, "up_mbps")
	delete(*out, "obfs")

	for deprecated, quic := range hysteriaDeprecatedOutFields {
		value, carried := (*out)[deprecated]
		if !carried {
			continue
		}
		delete(*out, deprecated)
		if _, set := (*out)[quic]; !set {
			(*out)[quic] = value
		}
	}

	if upMbps, ok := inbound["down_mbps"]; ok {
		(*out)["up_mbps"] = upMbps
	}
	if downMbps, ok := inbound["up_mbps"]; ok {
		(*out)["down_mbps"] = downMbps
	}
	if obfs, ok := inbound["obfs"]; ok {
		(*out)["obfs"] = obfs
	}
	for _, field := range hysteriaQUICFieldsFromInbound {
		if _, set := (*out)[field]; set {
			continue
		}
		if value, ok := inbound[field]; ok {
			(*out)[field] = value
		}
	}
}

// snellOut builds the client half of a snell listener.
//
// Only version 6 has one: sing-box's snell outbound speaks 4 and 6 while the
// inbound speaks 5 and 6, so a v5 listener has no generated client config at
// all -- Surge is what still talks to it, configured by hand. Wiping out_json
// is how the rest of this file says "no client config", and getOutbounds skips
// an inbound whose out_json is empty.
func snellOut(out *map[string]interface{}, inbound map[string]interface{}) {
	version, _ := inbound["version"].(float64)
	if int(version) != 6 {
		for key := range *out {
			delete(*out, key)
		}
		return
	}
	(*out)["version"] = 6
	// snell has no TLS options on either side -- its outbound is dialer, server,
	// psk, userkey, reuse and network -- so a tls block addTls left here would
	// be an unknown field, and sing-box rejects the whole config over one of
	// those, not just the outbound. Same for the v5-only obfs options, which a
	// listener switched from v5 to v6 can still be carrying.
	delete(*out, "tls")
	delete(*out, "obfs_mode")
	delete(*out, "obfs_host")

	// The psk is shared by every client on the listener; the per-client userkey
	// is folded in by the subscription from the client's own config.
	if psk, ok := inbound["psk"]; ok {
		(*out)["psk"] = psk
	}
	delete(*out, "mode")
	if mode, ok := inbound["mode"]; ok {
		(*out)["mode"] = mode
	}
}

func hysteria2Out(out *map[string]interface{}, inbound map[string]interface{}) {
	delete(*out, "down_mbps")
	delete(*out, "up_mbps")
	delete(*out, "obfs")

	if upMbps, ok := inbound["down_mbps"]; ok {
		(*out)["up_mbps"] = upMbps
	}
	if downMbps, ok := inbound["up_mbps"]; ok {
		(*out)["down_mbps"] = downMbps
	}
	if obfs, ok := inbound["obfs"]; ok {
		(*out)["obfs"] = obfs
	}
}

func tuicOut(out *map[string]interface{}, inbound map[string]interface{}) {
	delete(*out, "zero_rtt_handshake")
	delete(*out, "heartbeat")
	if congestionControl, ok := inbound["congestion_control"].(string); ok {
		(*out)["congestion_control"] = congestionControl
	} else {
		(*out)["congestion_control"] = "cubic"
	}
	if zeroRTT, ok := inbound["zero_rtt_handshake"].(bool); ok {
		(*out)["zero_rtt_handshake"] = zeroRTT
	}
	if heartbeat, ok := inbound["heartbeat"]; ok {
		(*out)["heartbeat"] = heartbeat
	}
}

func vlessOut(out *map[string]interface{}, inbound map[string]interface{}) {
	delete(*out, "transport")
	if transport, ok := inbound["transport"]; ok {
		(*out)["transport"] = transport
	}
}

func trojanOut(out *map[string]interface{}, inbound map[string]interface{}) {
	delete(*out, "transport")
	if transport, ok := inbound["transport"]; ok {
		(*out)["transport"] = transport
	}
}

func vmessOut(out *map[string]interface{}, inbound map[string]interface{}) {
	(*out)["alter_id"] = 0
	delete(*out, "transport")
	if transport, ok := inbound["transport"]; ok {
		(*out)["transport"] = transport
	}
}
