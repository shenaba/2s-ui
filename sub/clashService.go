package sub

import (
	"encoding/base64"
	"encoding/pem"
	"regexp"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/util"

	"gopkg.in/yaml.v3"
)

type ClashService struct {
	service.SettingService
	JsonService
	LinkService
}

const basicClashConfig = `mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
external-controller: 127.0.0.1:9090
tun:
  enable: true
  stack: system
  auto-route: true
  auto-detect-interface: true
  dns-hijack:
    - any:53
dns:
  enable: true
  ipv6: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver:
    - 8.8.8.8
    - 1.1.1.1
  nameserver:
    - https://doh.pub/dns-query
    - https://1.0.0.1/dns-query
  fallback:
    - tcp://9.9.9.9:53
  fake-ip-filter:
    - "*.lan"
    - localhost
    - "*.local"
rules:
  - GEOIP,Private,DIRECT
  - MATCH,Proxy
`

const ProxyGroups = `- name: Proxy
  type: select
  proxies: []
- name: Auto
  type: url-test
  proxies: []
  url: http://www.gstatic.com/generate_204
  interval: 300
  tolerance: 50
`

// echConfigPemType is the block sing-box writes for an ECH config; its key goes
// into an "ECH KEYS" block, which must never leave the panel.
const echConfigPemType = "ECH CONFIGS"

// echConfigForClash converts a stored ECH config into what mihomo's ech-opts
// wants, and returns "" when there is nothing usable to convert.
//
// The two sides disagree on shape. sing-box holds the PEM text line by line
// (common/tls/ech.go joins the list with newlines and pem.Decodes it, block
// type "ECH CONFIGS"), while mihomo wants the bare base64 of the ECHConfigList
// with no armor around it.
//
// Decoding the PEM is what makes that robust. Slicing the list positionally --
// everything but the first and last entry -- is right only when it holds
// exactly BEGIN, one body line, END: generateECHKeyPair splits a PEM that ends
// in a newline, so the panel's own output carries a trailing empty entry, and a
// body over 64 characters wraps onto a second line. Both shapes left the END
// marker in the value mihomo received.
//
// Concatenating every line instead, which upstream changed this to in 1.6.1,
// hands mihomo the armor in every case. Do not follow it.
func echConfigForClash(config []interface{}) string {
	lines := make([]string, 0, len(config))
	for _, line := range config {
		if s, ok := line.(string); ok {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	// The type is checked, not just the parse. pem.Decode is just as happy with
	// an "ECH KEYS" or "CERTIFICATE" block, and generateECHKeyPair hands the
	// operator the config PEM and the key PEM concatenated into one array while
	// the field itself is a free-text textarea -- so pasting the wrong half
	// would have published the ECH private key to every subscriber, silently.
	block, _ := pem.Decode([]byte(strings.Join(lines, "\n") + "\n"))
	if block == nil || block.Type != echConfigPemType {
		logger.Warning("sub: stored ECH config is not a ", echConfigPemType,
			" PEM block, omitting ech-opts")
		return ""
	}
	// Standard base64 with padding, which is byte for byte the PEM body this
	// used to concatenate -- so a config that already worked keeps working.
	return base64.StdEncoding.EncodeToString(block.Bytes)
}

func (s *ClashService) GetClash(subId string) (*string, []string, error) {

	client, inDatas, err := s.getData(subId)
	if err != nil {
		return nil, nil, err
	}

	outbounds, outTags, err := s.getOutbounds(client.Config, inDatas, client.Remark)
	if err != nil {
		return nil, nil, err
	}

	extOutbounds, extTags := s.LinkService.GetExternalOutbounds(&client.Links)
	*outbounds = append(*outbounds, extOutbounds...)
	*outTags = append(*outTags, extTags...)
	uniqueOutboundTags(*outbounds, *outTags)

	basicConfig, err := s.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	resultStr, err := s.ConvertToClashMeta(outbounds, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	updateInterval, _ := s.SettingService.GetSubUpdates()
	headers := util.GetHeaders(client, updateInterval)

	return &resultStr, headers, nil
}

func (s *ClashService) getClashConfig() (string, error) {
	subClashExt, err := s.SettingService.GetSubClashExt()
	if err != nil {
		return "", err
	}

	return subClashExt, nil
}

func (s *ClashService) ConvertToClashMeta(outbounds *[]map[string]interface{}, basicConfig string) (string, error) {
	var proxies []interface{}
	proxyTags := make([]string, 0)
	// One read for all three: they are needed on every subscription fetch and
	// were three separate SELECTs on the settings table.
	noDefGrp, sprtAll, defaultUdp, _ := s.SettingService.GetSubClashFlags()
	for _, obMap := range *outbounds {

		t, _ := obMap["type"].(string)
		if t == "selector" || t == "urltest" || t == "direct" {
			continue
		}

		proxy := make(map[string]interface{})
		proxy["name"] = obMap["tag"]
		proxy["type"] = t

		// Bare form only: yaml.Marshal quotes an IPv6 literal by itself, while a
		// bracketed one round-trips as the literal string "[::1]" and reaches
		// mihomo as a domain name (#1220).
		server, _ := obMap["server"].(string)
		proxy["server"] = util.NormalizeHost(server)

		proxy["port"] = obMap["server_port"]

		switch t {
		case "vmess", "vless", "tuic":
			proxy["uuid"] = obMap["uuid"]
			if t == "vmess" {
				// Through the shared reader: vmess() stores alter_id as a Go
				// int, so a .(float64) here answered 0 for every external link
				// and mihomo -- which, unlike sing-box, still speaks
				// non-AEAD vmess -- could not connect to an alterId node.
				if alterId, ok := util.AsInt64(obMap["alter_id"]); ok {
					proxy["alterId"] = int(alterId)
				} else {
					proxy["alterId"] = 0
				}
				proxy["cipher"] = "auto"
			}
			if t == "vless" {
				if flow, ok := obMap["flow"].(string); ok {
					proxy["flow"] = flow
				}
			}
			if t == "tuic" {
				proxy["password"] = obMap["password"]
				if congestion_control, ok := obMap["congestion_control"].(string); ok {
					proxy["congestion-controller"] = congestion_control
				}
			}
		case "trojan":
			proxy["password"] = obMap["password"]
		case "socks", "http":
			if t == "socks" {
				proxy["type"] = "socks5"
			}
			proxy["username"] = obMap["username"]
			proxy["password"] = obMap["password"]
		case "hysteria", "hysteria2":
			// hy() runs the bandwidths through strconv.Atoi, so an external
			// link carries them as Go ints and the .(float64) these replace
			// dropped both -- mihomo needs them to size its send window, and a
			// hysteria proxy without them falls back to its own default.
			// The value is still passed through rather than the parsed one, so
			// a stored row keeps whatever it holds.
			if _, ok := util.AsInt64(obMap["up_mbps"]); ok {
				proxy["up"] = obMap["up_mbps"]
			}
			if _, ok := util.AsInt64(obMap["down_mbps"]); ok {
				proxy["down"] = obMap["down_mbps"]
			}
			if t == "hysteria" {
				proxy["auth-str"] = obMap["auth_str"]
				if obfs, ok := obMap["obfs"].(string); ok {
					proxy["obfs"] = obfs
				}
			} else {
				proxy["password"] = obMap["password"]
				if obfs, ok := obMap["obfs"].(map[string]interface{}); ok {
					proxy["obfs"] = obfs["type"]
					proxy["obfs-password"] = obfs["password"]
				}
			}

			// The same renderer the "mport" link param goes through: mihomo's
			// `ports` and the link both want a dash where sing-box stores a
			// colon, and two copies of that rule drifted apart once already.
			if ports := util.PortHoppingRanges(obMap["server_ports"]); ports != "" {
				proxy["ports"] = ports
			}
		case "anytls":
			proxy["password"] = obMap["password"]
			if tls, ok := obMap["tls"].(map[string]interface{}); ok {
				proxy["sni"] = tls["server_name"]
				proxy["skip-cert-verify"] = tls["insecure"]
			}
		case "shadowsocks":
			proxy["type"] = "ss"
			proxy["cipher"] = obMap["method"]
			proxy["password"] = obMap["password"]
			// The plain-UDP default is decided below with every other protocol;
			// this branch used to answer for itself whenever the listener was
			// not TCP-only, which made subClashUdp mean nothing here.
			//
			// UDP over TCP is the exception and turns udp on by itself: it is
			// not a default anyone can opt out of later, it is the operator
			// having already said this client carries UDP, and mihomo reads
			// udp-over-tcp only when udp is set. It is also the one form that
			// works on a TCP-only listener -- carrying UDP inside the TCP
			// stream is the whole point -- so the network check below must not
			// gate it.
			if uot, ok := obMap["udp_over_tcp"].(bool); ok && uot {
				proxy["udp"] = true
				proxy["udp-over-tcp"] = true
			}
		default:
			continue
		}

		// Mihomo keeps UDP off unless the proxy opts in, and subClashUdp is the
		// single switch that decides it -- shadowsocks included, which is why
		// the case above no longer answers for itself. A default, not an
		// override: an outbound restricted to TCP keeps its answer, since
		// getOutbounds copies a TCP-only shadowsocks listener's network onto
		// it even when the operator never opened the client-config tab.
		network, _ := obMap["network"].(string)
		if defaultUdp && network != "tcp" {
			switch proxy["type"] {
			case "vmess", "vless":
				proxy["udp"] = true
				// The panel lets the operator pick the packet encoding per
				// inbound and the sing-box subscription serves it verbatim;
				// hardcoding xudp here would make the two subscriptions
				// disagree about the same inbound. Absent means "none" in the
				// form, and mihomo needs an encoding to carry UDP at all, so
				// only that case falls back to xudp.
				if pe, ok := obMap["packet_encoding"].(string); ok && pe != "" {
					proxy["packet-encoding"] = pe
				} else {
					proxy["packet-encoding"] = "xudp"
				}
			case "trojan", "ss", "socks5":
				proxy["udp"] = true
			}
		}

		// TLS params
		//
		// A missing enabled key means off, the same reading the link builders
		// use. It used to mean on here (only an explicit false turned TLS off),
		// so one hand-written or imported row produced a Clash proxy with the
		// full TLS block and a share link with none at all.
		tls, isTls := obMap["tls"].(map[string]interface{})
		if isTls {
			isTls = util.AsBool(tls["enabled"])
		}
		if isTls {
			// A literal true, not the value read back: an absent key wrote
			// `tls: null` into the YAML, and mihomo wants a bool there.
			proxy["tls"] = true

			switch t {
			case "hysteria", "hysteria2", "tuic":
				proxy["alpn"] = []string{"h3"}
			default:
				// Through the same reader the link builders use, rather than
				// passing the raw []interface{} on: mihomo decodes alpn into a
				// []string, so one non-string entry poisons the whole proxy.
				if alpn := util.AsStringList(tls["alpn"]); len(alpn) > 0 {
					proxy["alpn"] = alpn
				}
			}

			// Add reality if exists
			// Comma-ok on enabled as well: a config written by hand, or one
			// carried over from an older schema, can leave the key absent, and
			// the bare assertion took the whole subscription endpoint down.
			if reality, ok := tls["reality"].(map[string]interface{}); ok && util.AsBool(reality["enabled"]) {
				reality_opts := make(map[string]interface{})
				if pbk, ok := reality["public_key"].(string); ok {
					reality_opts["public-key"] = pbk
				}
				if sid, ok := reality["short_id"].(string); ok {
					reality_opts["short-id"] = sid
				}
				proxy["reality-opts"] = reality_opts
			}
			if utls, ok := tls["utls"].(map[string]interface{}); ok {
				if enabled, ok := utls["enabled"].(bool); ok && enabled {
					if fp, ok := utls["fingerprint"].(string); ok {
						proxy["client-fingerprint"] = fp
					}
				}
			}
			if sni, ok := tls["server_name"].(string); ok {
				if t == "vless" || t == "vmess" {
					proxy["servername"] = sni
				} else {
					proxy["sni"] = sni
				}
			}
			if insecure, ok := tls["insecure"].(bool); ok && insecure {
				proxy["skip-cert-verify"] = insecure
			}
			if fp := util.CertSha256Hex(util.CertPEMFromTLS(tls)); fp != "" {
				proxy["fingerprint"] = fp
			}
			// ech outbounds
			if ech, ok := tls["ech"].(map[string]interface{}); ok && util.AsBool(ech["enabled"]) {
				ech_config, _ := ech["config"].([]interface{})
				if ech_string := echConfigForClash(ech_config); ech_string != "" {
					proxy["ech-opts"] = map[string]interface{}{
						"enable": true,
						"config": ech_string,
					}
				}
			}
		}

		// Transport if exist
		if transport, ok := obMap["transport"].(map[string]interface{}); ok {
			tt, _ := transport["type"].(string)
			switch tt {
			case "http":
				httpOpts := make(map[string]interface{})
				if path, ok := transport["path"].([]interface{}); ok && len(path) > 0 {
					httpOpts["path"] = path[0]
				} else if path, ok := transport["path"].(string); ok {
					httpOpts["path"] = path
				}
				// Through the shared reader as well: getTransport splits the
				// host query param, so an external link carries []string here
				// and the bare .([]interface{}) dropped the Host outright --
				// a listener that routes on it then refuses the request. The
				// path on the line above is a plain string from the same
				// decoder, which is why only the host was lost.
				if hosts := util.AsStringList(transport["host"]); len(hosts) > 0 {
					httpOpts["host"] = hosts[0]
				}
				if isTls {
					proxy["network"] = "h2"
					proxy["h2-opts"] = httpOpts
				} else {
					proxy["network"] = "http"
					// Only the keys that are actually set. Reading them back
					// unconditionally emitted `path: [null]` and `host: null`
					// for a transport that carries neither, and mihomo decodes
					// both as strings. The empty-array case reaches here now
					// that the bounds check above stops it panicking.
					httpProxyOpts := make(map[string]interface{}, 2)
					if path, ok := httpOpts["path"]; ok {
						httpProxyOpts["path"] = []interface{}{path}
					}
					if host, ok := httpOpts["host"]; ok {
						httpProxyOpts["host"] = host
					}
					proxy["http-opts"] = httpProxyOpts
				}
			case "ws", "httpupgrade":
				proxy["network"] = "ws"
				wsOpts := make(map[string]interface{})
				if path, ok := transport["path"].(string); ok {
					wsOpts["path"] = path
				}
				wsHeaders := make(map[string]interface{})
				// Only the Host header is carried into Clash
				if headers, ok := transport["headers"].(map[string]interface{}); ok {
					if v, ok := headers["Host"]; ok {
						if arr, ok := v.([]interface{}); ok {
							if len(arr) > 0 {
								wsHeaders["Host"] = arr[0]
							}
						} else {
							wsHeaders["Host"] = v
						}
					}
				}
				if _, hasHost := wsHeaders["Host"]; !hasHost {
					if host, ok := transport["host"].(string); ok && host != "" {
						wsHeaders["Host"] = host
					} else if isTls {
						if sni, ok := tls["server_name"].(string); ok && sni != "" {
							wsHeaders["Host"] = sni
						}
					}
				}
				if len(wsHeaders) > 0 {
					wsOpts["headers"] = wsHeaders
				}
				// mihomo needs both keys before it will use early data: the
				// header name alone leaves max-early-data at 0, which disables it.
				if maxED, ok := transport["max_early_data"].(float64); ok && maxED > 0 {
					wsOpts["max-early-data"] = int(maxED)
				}
				if ed, ok := transport["early_data_header_name"].(string); ok {
					wsOpts["early-data-header-name"] = ed
				}
				if tt == "httpupgrade" {
					wsOpts["v2ray-http-upgrade"] = true
				}
				proxy["ws-opts"] = wsOpts
			case "grpc":
				proxy["network"] = "grpc"
				grpcOpts := make(map[string]interface{})
				if service_name, ok := transport["service_name"].(string); ok {
					grpcOpts["grpc-service-name"] = service_name
				}
				proxy["grpc-opts"] = grpcOpts
			}
		}

		// Multiplex
		if mux, ok := obMap["multiplex"].(map[string]interface{}); ok {
			if enabled, ok := mux["enabled"].(bool); ok && enabled {
				smux := make(map[string]interface{})
				smux["enabled"] = true
				if protocol, ok := mux["protocol"].(string); ok {
					smux["protocol"] = protocol
				}
				if _, ok := mux["max_connections"].(float64); ok {
					smux["max-connections"] = mux["max_connections"]
				}
				if _, ok := mux["min_streams"].(float64); ok {
					smux["min-streams"] = mux["min_streams"]
				}
				if _, ok := mux["max_streams"].(float64); ok {
					smux["max-streams"] = mux["max_streams"]
				}
				if _, ok := mux["padding"].(bool); ok {
					smux["padding"] = mux["padding"]
				}
				if brutal, ok := mux["brutal"].(map[string]interface{}); ok {
					if enabled, ok := brutal["enabled"].(bool); ok && enabled {
						brutalOpts := make(map[string]interface{})
						brutalOpts["enabled"] = true
						if _, ok := brutal["up_mbps"].(float64); ok {
							brutalOpts["up"] = brutal["up_mbps"]
						}
						if _, ok := brutal["down_mbps"].(float64); ok {
							brutalOpts["down"] = brutal["down_mbps"]
						}
						smux["brutal-opts"] = brutalOpts
					}
				}
				proxy["smux"] = smux
			}
		}

		proxies = append(proxies, proxy)
		proxyTags = append(proxyTags, obMap["tag"].(string))
	}

	// Merge proxies and proxy groups if exist
	var output map[string]interface{}
	if err := yaml.Unmarshal([]byte(basicConfig), &output); err != nil {
		logger.Warning("sub: the Clash extension config is not valid YAML: ", err)
	}
	if output == nil {
		// Valid YAML that is not a mapping -- a bare scalar, a list, or a
		// document holding only comments -- decodes to a nil map, and so does
		// one that failed to decode at all. The merge below writes into it,
		// which took every Clash subscription down with a 500.
		//
		// The shipped defaults rather than an empty profile: they carry the
		// dns and rules blocks, and mihomo has nothing to route with without
		// them. The operator's own config is what is unusable here, not ours.
		logger.Warning("sub: the Clash extension config is not a YAML mapping, using the defaults")
		if err := yaml.Unmarshal([]byte(basicClashConfig), &output); err != nil {
			return "", err
		}
	}

	if p, ok := output["proxies"].([]interface{}); ok {
		output["proxies"] = append(p, proxies...)
	} else {
		output["proxies"] = proxies
	}

	if err := buildProxyGroups(output, proxyTags, noDefGrp, sprtAll); err != nil {
		return "", err
	}

	result, err := yaml.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func buildProxyGroups(output map[string]interface{}, proxyTags []string, noDefGrp bool, sprtAll bool) error {
	customGroups := proxyGroupList(output["proxy-groups"])
	var defaultGroups []map[string]interface{}

	if !noDefGrp {
		if err := yaml.Unmarshal([]byte(ProxyGroups), &defaultGroups); err != nil {
			return err
		}
		defaultGroups[1]["proxies"] = proxyTags
		defaultGroups[0]["proxies"] = append([]string{defaultGroups[1]["name"].(string)}, proxyTags...)
		// Don't inject a duplicate "Auto" if the user already defines one.
		if hasGroupNamed(customGroups, "Auto") {
			defaultGroups = defaultGroups[:1]
			defaultGroups[0]["proxies"] = proxyTags
		}
		// No routes at all — e.g. the client only references node replicas and
		// reconciliation has not written their links yet. A url-test group with
		// an empty proxies list fails mihomo's config parse outright, taking
		// the whole subscription with it (the Clash twin of the empty urltest
		// the JSON path degrades around). Drop Auto and point Proxy at the
		// built-in DIRECT so the profile still imports.
		if len(proxyTags) == 0 {
			defaultGroups = defaultGroups[:1]
			defaultGroups[0]["proxies"] = []string{"DIRECT"}
		}
	}
	// noDefGrp leaves defaultGroups nil, so this is the custom groups alone.
	proxyGroups := mergeProxyGroups(defaultGroups, customGroups)

	if len(proxyGroups) > 0 || !noDefGrp {
		resolveProxyGroupTags(proxyGroups, proxyTags, sprtAll)
		output["proxy-groups"] = proxyGroups
	}
	return nil
}

func proxyGroupList(pg interface{}) []map[string]interface{} {
	switch list := pg.(type) {
	case []map[string]interface{}:
		return list
	case []interface{}:
		groups := make([]map[string]interface{}, 0, len(list))
		for _, item := range list {
			if group, ok := item.(map[string]interface{}); ok {
				groups = append(groups, group)
			}
		}
		return groups
	}
	return nil
}

func mergeProxyGroups(base, extra []map[string]interface{}) []map[string]interface{} {
	groups := make([]map[string]interface{}, 0, len(base)+len(extra))
	index := make(map[string]int)

	// Not append(base, extra...): that writes into base's backing array when it
	// has spare capacity, which base[:1] above deliberately leaves.
	all := make([]map[string]interface{}, 0, len(base)+len(extra))
	all = append(all, base...)
	all = append(all, extra...)

	for _, group := range all {
		if gname, ok := group["name"].(string); ok {
			if i, exists := index[gname]; exists {
				mergeProxyGroup(groups[i], group)
				continue
			}
			index[gname] = len(groups)
		}
		groups = append(groups, group)
	}
	return groups
}

func mergeProxyGroup(dst, src map[string]interface{}) {
	for key, value := range src {
		switch key {
		case "name":
			continue
		case "proxies":
			dst["proxies"] = appendProxyNames(proxyNames(dst["proxies"]), proxyNames(value))
		default:
			if _, exists := dst[key]; !exists {
				dst[key] = value
			}
		}
	}
}

func resolveProxyGroupTags(groups []map[string]interface{}, proxyTags []string, sprtAll bool) {
	for _, group := range groups {
		proxies := proxyNames(group["proxies"])
		if sprtAll {
			proxies = expandAllProxyTag(proxies, proxyTags)
		}
		if filter, _ := group["filter"].(string); filter != "" {
			proxies = appendProxyNames(proxies, filteredProxyTags(proxyTags, filter))
		}
		if len(proxies) > 0 {
			group["proxies"] = proxies
		}
	}
}

func expandAllProxyTag(proxies []string, proxyTags []string) []string {
	result := make([]string, 0, len(proxies)+len(proxyTags))
	for _, proxy := range proxies {
		if strings.EqualFold(proxy, "all") {
			result = appendProxyNames(result, proxyTags)
		} else {
			result = appendProxyNames(result, []string{proxy})
		}
	}
	return result
}

func filteredProxyTags(proxyTags []string, filter string) []string {
	re, err := regexp.Compile(filter)
	if err != nil {
		logger.Warning("sub: invalid Clash proxy-group filter:", err)
		return nil
	}
	var result []string
	for _, tag := range proxyTags {
		if re.MatchString(tag) {
			result = append(result, tag)
		}
	}
	return result
}

func proxyNames(value interface{}) []string {
	switch list := value.(type) {
	case []string:
		return append([]string(nil), list...)
	case []interface{}:
		proxies := make([]string, 0, len(list))
		for _, item := range list {
			if name, ok := item.(string); ok {
				proxies = append(proxies, name)
			}
		}
		return proxies
	}
	return nil
}

func appendProxyNames(proxies []string, names []string) []string {
	seen := make(map[string]bool, len(proxies)+len(names))
	result := make([]string, 0, len(proxies)+len(names))
	for _, name := range append(proxies, names...) {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func hasGroupNamed(groups []map[string]interface{}, name string) bool {
	for _, group := range groups {
		if gname, ok := group["name"].(string); ok && gname == name {
			return true
		}
	}
	return false
}
