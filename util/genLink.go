package util

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

var InboundTypeWithLink = []string{"socks", "http", "mixed", "shadowsocks", "naive", "hysteria", "hysteria2", "anytls", "tuic", "vless", "trojan", "vmess"}

type LinkParam struct {
	Key   string
	Value string
}

func JoinRemark(clientRemark, inboundRemark string) string {
	if clientRemark != "" {
		return clientRemark + "-" + inboundRemark
	}
	return inboundRemark
}

func LinkGenerator(clientConfig json.RawMessage, i *model.Inbound, hostname string, clientRemark string) []string {
	inbound, err := i.MarshalFull()
	if err != nil {
		return []string{}
	}

	var tls map[string]interface{}
	if i.TlsId > 0 {
		tls = prepareTls(i.Tls)
	}

	var userConfig map[string]map[string]interface{}
	if err := json.Unmarshal(clientConfig, &userConfig); err != nil {
		return []string{}
	}

	var Addrs []map[string]interface{}
	if err := json.Unmarshal(i.Addrs, &Addrs); err != nil {
		return []string{}
	}
	if len(Addrs) == 0 {
		Addrs = append(Addrs, map[string]interface{}{
			"server":      hostname,
			"server_port": (*inbound)["listen_port"],
			"remark":      JoinRemark(clientRemark, i.Tag),
		})
		if i.TlsId > 0 {
			Addrs[0]["tls"] = tls
		}
	} else {
		for index, addr := range Addrs {
			if addr == nil {
				// A JSON null in the addrs column decodes to a nil map, and the
				// writes below would panic on it. Reading from one is legal, so
				// the guarded read on the next line was never enough.
				Addrs[index] = map[string]interface{}{}
				addr = Addrs[index]
			}
			addrRemark, _ := addr["remark"].(string)
			Addrs[index]["remark"] = JoinRemark(clientRemark, i.Tag+addrRemark)
			if i.TlsId > 0 {
				newTls := map[string]interface{}{}
				for k, v := range tls {
					newTls[k] = v
				}

				// Override tls
				if addrTls, ok := addr["tls"].(map[string]interface{}); ok {
					for k, v := range addrTls {
						newTls[k] = v
					}
				}
				Addrs[index]["tls"] = newTls
			}
		}
	}

	// The panel brackets an IPv6 hostname before it gets here, and an address
	// row carries whatever the operator typed. Keep the bare form as the one
	// stored shape -- vmess puts it in "add" as-is, and every URI builder
	// brackets it back through HostForURI.
	for index := range Addrs {
		if server, ok := Addrs[index]["server"].(string); ok {
			Addrs[index]["server"] = NormalizeHost(server)
		}
	}

	switch i.Type {
	case "socks":
		return socksLink(userConfig["socks"], Addrs, "")
	case "http":
		return httpLink(userConfig["http"], Addrs, "")
	case "mixed":
		// Only the mixed case suffixes: a socks-only inbound has nothing to be
		// told apart from, and renaming its nodes would read as a new node to
		// every client that already has it.
		return append(
			socksLink(userConfig["socks"], Addrs, socksRemarkSuffix),
			httpLink(userConfig["http"], Addrs, httpRemarkSuffix)...,
		)
	case "shadowsocks":
		return shadowsocksLink(userConfig, *inbound, Addrs)
	case "naive":
		return naiveLink(userConfig["naive"], *inbound, Addrs)
	case "hysteria":
		return hysteriaLink(userConfig["hysteria"], *inbound, Addrs)
	case "hysteria2":
		return hysteria2Link(userConfig["hysteria2"], *inbound, Addrs)
	case "tuic":
		return tuicLink(userConfig["tuic"], *inbound, Addrs)
	case "vless":
		return vlessLink(userConfig["vless"], *inbound, Addrs)
	case "anytls":
		return anytlsLink(userConfig["anytls"], Addrs)
	case "trojan":
		return trojanLink(userConfig["trojan"], *inbound, Addrs)
	case "vmess":
		return vmessLink(userConfig["vmess"], *inbound, Addrs)
	}

	return []string{}
}

// reportedTlsRows remembers which TLS rows have already been complained about.
//
// prepareTls runs once per client per inbound, so a single TLS save on a
// malformed row fans a warning out across every client that references it --
// five hundred identical lines into a ten-thousand-line ring buffer, which
// evicts whatever the operator was actually trying to read. The diagnosis still
// has to live here rather than in TlsService.Save: a row can also arrive
// through ImportDB or apiv2 without passing through that path.
//
// Keyed on the row id, so fixing a row and breaking it again reports nothing
// until the next restart. That is the right trade for a warning.
var reportedTlsRows sync.Map

func warnTlsRowOnce(id uint, args ...interface{}) {
	if _, seen := reportedTlsRows.LoadOrStore(id, struct{}{}); seen {
		return
	}
	logger.Warning(args...)
}

func prepareTls(t *model.Tls) map[string]interface{} {
	var iTls, oTls map[string]interface{}
	if err := json.Unmarshal(t.Client, &oTls); err != nil {
		return nil
	}
	if oTls == nil {
		// A literal "null" client column unmarshals to a nil map and the
		// assignments below would panic on it.
		oTls = map[string]interface{}{}
	}
	if err := json.Unmarshal(t.Server, &iTls); err != nil {
		return nil
	}

	// Link pin params expect the certificate fingerprint in hex, not the
	// base64 SPKI hash that sing-box JSON uses in certificate_public_key_sha256.
	if oTls["certificate_public_key_sha256"] != nil {
		oTls["pinSHA256"] = CertSha256Hex(CertPEMFromTLS(iTls))
	}

	for k, v := range iTls {
		switch k {
		case "enabled", "server_name", "alpn":
			oTls[k] = v
		case "reality":
			// Both assertions used to be bare and panicked on a row whose
			// server half carries reality while its client half does not --
			// hand-written, or carried over from an older schema.
			reality, okServer := v.(map[string]interface{})
			if !okServer {
				warnTlsRowOnce(t.Id, "sub: tls row ", t.Id,
					" has a reality field that is not an object, skipping it")
				continue
			}
			// Repaired rather than skipped, the same way a null client column is
			// repaired above. Dropping it produced a link claiming plain TLS
			// against a reality listener: broken either way, since the public
			// key only exists in the client half, but a link that says reality
			// is refused by the client on import instead of failing later as an
			// unexplained handshake error. Either way the operator has to hear
			// about it -- this is the one row the panel cannot render.
			clientReality, okClient := oTls["reality"].(map[string]interface{})
			if !okClient {
				warnTlsRowOnce(t.Id, "sub: tls row ", t.Id,
					" has reality on the server side only; generated links carry",
					" no public key until the client side is set")
				clientReality = map[string]interface{}{}
			}
			clientReality["enabled"] = reality["enabled"]
			// Through the shared reader: short_id is Listable[string] too, so a
			// row holding a single id can have it stored as a bare string --
			// which the array assertion read as absent, and a reality link
			// without a short_id is one the server refuses.
			if shortIDs := AsStringList(reality["short_id"]); len(shortIDs) > 0 {
				clientReality["short_id"] = shortIDs[common.RandomInt(len(shortIDs))]
			}
			oTls["reality"] = clientReality
		}
	}
	StripServerTlsFields(oTls)
	return oTls
}

// proxyUserinfo builds the userinfo for the two username/password protocols.
// Nil when neither is set: the inbound takes no authentication, and the old
// code formatted the two absent values straight into the link, which produced a
// literal "%!s(<nil>):%!s(<nil>)" as the credentials.
func proxyUserinfo(userConfig map[string]interface{}) *url.Userinfo {
	user := AsString(userConfig["username"])
	pass := AsString(userConfig["password"])
	if user == "" && pass == "" {
		return nil
	}
	return url.UserPassword(user, pass)
}

// remarkSuffix distinguishes the two links a mixed inbound emits for one
// address. Without it both carry the same node name and a subscriber cannot
// tell which is which -- the same reason naiveLink appends -h2/-h3 and
// sub/jsonService.go's pushMixed appends these exact two.
const (
	socksRemarkSuffix = "-socks"
	httpRemarkSuffix  = "-http"
)

func socksLink(userConfig map[string]interface{}, addrs []map[string]interface{}, remarkSuffix string) []string {
	userinfo := proxyUserinfo(userConfig)
	var links []string
	for _, addr := range addrs {
		port, _ := addr["server_port"].(float64)
		// The remark was dropped here, so a socks link arrived unnamed while
		// every other protocol carried its node name.
		links = append(links, linkURL("socks5", userinfo,
			AsString(addr["server"]), port, nil, AsString(addr["remark"])+remarkSuffix))
	}
	return links
}

func httpLink(userConfig map[string]interface{}, addrs []map[string]interface{}, remarkSuffix string) []string {
	userinfo := proxyUserinfo(userConfig)
	var links []string
	for _, addr := range addrs {
		// Decided per address, not carried over: one TLS-enabled address used
		// to make every address after it https, whatever its own setting.
		//
		// And decided on enabled, not on the key being present: LinkGenerator
		// attaches the tls map to every address whenever the inbound references
		// a TLS row at all, so a row with enabled:false used to produce https
		// links to a plaintext listener. prepareTls can also return a nil map,
		// which is a typed nil and satisfies a bare != nil.
		protocol := "http"
		if tls, ok := addr["tls"].(map[string]interface{}); ok && AsBool(tls["enabled"]) {
			protocol = "https"
		}
		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL(protocol, userinfo,
			AsString(addr["server"]), port, nil, AsString(addr["remark"])+remarkSuffix))
	}
	return links
}

func shadowsocksLink(
	userConfig map[string]map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	var userPass []string
	method, _ := inbound["method"].(string)
	if strings.HasPrefix(method, "2022") {
		inbPass, _ := inbound["password"].(string)
		userPass = append(userPass, inbPass)
	}
	pass, _ := userConfig[ShadowsocksClientConfigKey(method)]["password"].(string)
	userPass = append(userPass, pass)

	// SIP002 specifies base64url without padding for the userinfo. Standard
	// base64 emits '+', '/' and '=', which several clients reject outright.
	userInfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + strings.Join(userPass, ":")))

	var plugin, pluginOpts string
	if raw, ok := inbound["out_json"].(json.RawMessage); ok {
		var outJson map[string]interface{}
		if json.Unmarshal(raw, &outJson) == nil {
			plugin, _ = outJson["plugin"].(string)
			pluginOpts, _ = outJson["plugin_opts"].(string)
		}
	}

	var links []string
	for _, addr := range addrs {
		port, _ := addr["server_port"].(float64)
		var params []LinkParam
		if plugin != "" {
			pluginVal := plugin
			if pluginOpts != "" {
				pluginVal += ";" + pluginOpts
			}
			params = append(params, LinkParam{"plugin", pluginVal})
		}
		// Through url.URL, so a remark holding a space or a '#' is escaped
		// rather than concatenated straight into the fragment.
		u := url.URL{
			Scheme:   "ss",
			Host:     fmt.Sprintf("%s:%.0f", HostForURI(AsString(addr["server"])), port),
			Fragment: AsString(addr["remark"]),
		}
		u.RawQuery = encodeParams(params)
		// url.URL would re-escape a pre-encoded userinfo, so the SIP002 blob is
		// spliced in after the scheme.
		links = append(links, strings.Replace(u.String(), "ss://", "ss://"+userInfo+"@", 1))
	}
	return links
}

func naiveLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	password, _ := userConfig["password"].(string)
	username, _ := userConfig["username"].(string)

	baseUri := "http2://"
	var links []string

	for _, addr := range addrs {
		var params []LinkParam
		params = append(params, LinkParam{"padding", "1"})
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			if sni, ok := tls["server_name"].(string); ok {
				params = append(params, LinkParam{"peer", sni})
			}
			if alpnList := AsStringList(tls["alpn"]); len(alpnList) > 0 {
				params = append(params, LinkParam{"alpn", strings.Join(alpnList, ",")})
			}
			if insecure, ok := tls["insecure"].(bool); ok && insecure {
				params = append(params, LinkParam{"insecure", "1"})
			}
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"tfo", "1"})
		} else {
			params = append(params, LinkParam{"tfo", "0"})
		}

		port, _ := addr["server_port"].(float64)
		server := AsString(addr["server"])
		remark := AsString(addr["remark"])
		uri := baseUri + toBase64([]byte(fmt.Sprintf("%s:%s@%s:%.0f", username, password, HostForURI(server), port)))
		links = append(links, addParams(uri, params, remark))

		// The legacy http2:// form above carries no transport, so a client cannot
		// tell an h2 listener from an h3 one. Emit the plain naive+ form too, one
		// per network the inbound actually listens on -- an unset network means
		// both.
		var schemes []string
		switch network, _ := inbound["network"].(string); network {
		case "tcp":
			schemes = []string{"naive+https"}
		case "udp":
			schemes = []string{"naive+quic"}
		default:
			schemes = []string{"naive+https", "naive+quic"}
		}
		for _, scheme := range schemes {
			// Every link for one address would otherwise carry the same remark and
			// reach the client as identically named nodes. Only the new ones get a
			// suffix; the legacy link keeps its bare remark so it stays the same
			// node for clients that already have it.
			suffix := "-h2"
			if scheme == "naive+quic" {
				suffix = "-h3"
			}
			// url.URL escapes the userinfo with its own set: a space becomes
			// %20, where QueryEscape would make it a '+' that reads back as a
			// literal plus.
			links = append(links, linkURL(scheme, url.UserPassword(username, password),
				server, port, params, remark+suffix))
		}
	}
	return links
}

func hysteriaLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	// Read once, not once per address: it only looks at the inbound, and it
	// reports a malformed out_json through the logger. Left in the loop, one
	// corrupt column fanned that warning out across every address of every
	// client the inbound has -- the fan-out warnTlsRowOnce exists to prevent.
	mport := portHoppingParam(inbound)
	var links []string

	for _, addr := range addrs {
		var params []LinkParam
		if upmbps, ok := inbound["up_mbps"].(float64); ok {
			params = append(params, LinkParam{"downmbps", fmt.Sprintf("%.0f", upmbps)})
		}
		if downmbps, ok := inbound["down_mbps"].(float64); ok {
			params = append(params, LinkParam{"upmbps", fmt.Sprintf("%.0f", downmbps)})
		}
		if auth, ok := userConfig["auth_str"].(string); ok {
			params = append(params, LinkParam{"auth", auth})
		}
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "hysteria")
		}
		if obfs, ok := inbound["obfs"].(string); ok {
			params = append(params, LinkParam{"obfs", obfs})
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"fastopen", "1"})
		} else {
			params = append(params, LinkParam{"fastopen", "0"})
		}
		if mport != "" {
			params = append(params, LinkParam{"mport", mport})
		}

		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("hysteria", nil,
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func hysteria2Link(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	password, _ := userConfig["password"].(string)
	// Loop-invariant, and it logs on a malformed out_json -- see hysteriaLink.
	mport := portHoppingParam(inbound)
	var links []string

	for _, addr := range addrs {
		var params []LinkParam
		if upmbps, ok := inbound["up_mbps"].(float64); ok {
			params = append(params, LinkParam{"downmbps", fmt.Sprintf("%.0f", upmbps)})
		}
		if downmbps, ok := inbound["down_mbps"].(float64); ok {
			params = append(params, LinkParam{"upmbps", fmt.Sprintf("%.0f", downmbps)})
		}
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "hysteria2")
		}
		if obfs, ok := inbound["obfs"].(map[string]interface{}); ok {
			if obfsType, ok := obfs["type"].(string); ok {
				params = append(params, LinkParam{"obfs", obfsType})
			}
			if obfsPassword, ok := obfs["password"].(string); ok {
				params = append(params, LinkParam{"obfs-password", obfsPassword})
			}
		}
		if tfo, ok := inbound["tcp_fast_open"].(bool); ok && tfo {
			params = append(params, LinkParam{"fastopen", "1"})
		} else {
			params = append(params, LinkParam{"fastopen", "0"})
		}
		if mport != "" {
			params = append(params, LinkParam{"mport", mport})
		}

		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("hysteria2", url.User(password),
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func anytlsLink(
	userConfig map[string]interface{},
	addrs []map[string]interface{}) []string {

	password, _ := userConfig["password"].(string)
	var links []string

	for _, addr := range addrs {
		var params []LinkParam
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "anytls")
		}

		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("anytls", url.User(password),
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func tuicLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	password, _ := userConfig["password"].(string)
	uuid, _ := userConfig["uuid"].(string)
	var links []string

	// udp_relay_mode is a client-side (outbound) param and lives in out_json
	var outJson map[string]interface{}
	if raw, ok := inbound["out_json"].(json.RawMessage); ok {
		_ = json.Unmarshal(raw, &outJson)
	}

	for _, addr := range addrs {
		var params []LinkParam
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "tuic")
		}
		if congestionControl, ok := inbound["congestion_control"].(string); ok {
			params = append(params, LinkParam{"congestion_control", congestionControl})
		}
		if udpRelayMode, ok := outJson["udp_relay_mode"].(string); ok && udpRelayMode != "" {
			params = append(params, LinkParam{"udp_relay_mode", udpRelayMode})
		}

		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("tuic", url.UserPassword(uuid, password),
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func vlessLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	uuid, _ := userConfig["uuid"].(string)
	baseParams := getTransportParams(inbound["transport"])
	isTcp := false
	if len(baseParams) == 1 && baseParams[0].Value == "tcp" {
		isTcp = true
	}
	var links []string

	for _, addr := range addrs {
		params := make([]LinkParam, len(baseParams))
		copy(params, baseParams)
		if tls, ok := addr["tls"].(map[string]interface{}); ok && AsBool(tls["enabled"]) {
			getTlsParams(&params, tls, "vless")
			if flow, ok := userConfig["flow"].(string); ok && isTcp {
				params = append(params, LinkParam{"flow", flow})
			}
		}
		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("vless", url.User(uuid),
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func trojanLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	baseParams := getTransportParams(inbound["transport"])
	var links []string

	for _, addr := range addrs {
		params := make([]LinkParam, len(baseParams))
		copy(params, baseParams)
		if tls, ok := addr["tls"].(map[string]interface{}); ok && AsBool(tls["enabled"]) {
			getTlsParams(&params, tls, "trojan")
		}
		port, _ := addr["server_port"].(float64)
		links = append(links, linkURL("trojan", url.User(password),
			AsString(addr["server"]), port, params, AsString(addr["remark"])))
	}

	return links
}

func vmessLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {

	uuid, _ := userConfig["uuid"].(string)
	transportParams := getTransportParams(inbound["transport"])
	var links []string

	baseParams := map[string]interface{}{
		"v":   "2",
		"id":  uuid,
		"aid": 0,
	}

	var net, typ, host, path string
	for _, p := range transportParams {
		switch p.Key {
		case "type":
			net = p.Value
		case "host":
			host = p.Value
		case "path":
			path = p.Value
		case "serviceName":
			// The vmess JSON has no serviceName field; grpc carries the service
			// name in "path". Dropping it meant every grpc vmess link pointed
			// at the default service and simply did not connect.
			if path == "" {
				path = p.Value
			}
		}
	}

	if net == "http" || net == "tcp" {
		baseParams["net"] = "tcp"
		if net == "http" {
			typ = "http"
		}
	} else {
		baseParams["net"] = net
	}

	for _, addr := range addrs {
		obj := make(map[string]interface{})
		for k, v := range baseParams {
			obj[k] = v
		}

		obj["add"], _ = addr["server"].(string)
		port, _ := addr["server_port"].(float64)
		obj["port"] = fmt.Sprintf("%.0f", port)
		obj["ps"], _ = addr["remark"].(string)
		if typ != "" {
			obj["type"] = typ
		}
		if host != "" {
			obj["host"] = host
		}
		if path != "" {
			obj["path"] = path
		}
		populateVmessTlsParams(obj, addr["tls"])

		jsonStr, _ := json.Marshal(obj)

		uri := fmt.Sprintf("vmess://%s", toBase64(jsonStr))
		links = append(links, uri)
	}
	return links
}

func populateVmessTlsParams(obj map[string]interface{}, tlsConfig interface{}) {
	if tlsMap, ok := tlsConfig.(map[string]interface{}); ok && AsBool(tlsMap["enabled"]) {
		obj["tls"] = "tls"
		var tlsParams []LinkParam
		getTlsParams(&tlsParams, tlsMap, "vmess")
		for _, p := range tlsParams {
			switch p.Key {
			case "security":
				// ignore, as "tls" is already set
			case "allowInsecure":
				obj["allowInsecure"] = 1
			case "sni":
				obj["sni"] = p.Value
			case "fp":
				obj["fp"] = p.Value
			case "alpn":
				obj["alpn"] = p.Value
			}
		}
	} else {
		obj["tls"] = "none"
	}
}

func toBase64(d []byte) string {
	return base64.StdEncoding.EncodeToString(d)
}

// rawParamIsSafe reports whether a value may go into the query unescaped.
//
// mport and alpn are the two written raw, because client parsers expect their
// commas literal. Escaping them instead is not an option: url.QueryEscape also
// encodes '/', and alpn's most common value is "h2,http/1.1" -- a client that
// does not url-decode would negotiate a protocol named "http%2F1.1". So the
// values are checked rather than escaped, against the characters each can
// legitimately hold: digits, commas, ranges and colons for a port list, and
// the ALPN identifier alphabet for the other.
//
// It has to be checked because neither value is necessarily this panel's. A
// managed node supplies both through its out_json -- server_ports becomes
// mport, tls.alpn becomes alpn -- and the master generates its subscribers'
// links for that node's replica inbounds from it. An unescaped '&' therefore
// let a node append parameters of its own choosing to links the master hands
// out under its own name.
func rawParamIsSafe(key, value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		ok := c >= '0' && c <= '9'
		switch key {
		case "mport":
			ok = ok || c == ',' || c == '-' || c == ':'
		case "alpn":
			ok = ok || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				c == ',' || c == '.' || c == '-' || c == '/' || c == '+'
		}
		if !ok {
			return false
		}
	}
	return true
}

// encodeParams renders the query. mport and alpn keep their commas, which the
// client parsers expect unescaped; see rawParamIsSafe for what that costs and
// how it is paid for.
func encodeParams(params []LinkParam) string {
	var q []string
	for _, p := range params {
		switch p.Key {
		case "mport", "alpn":
			// Dropped rather than escaped or passed through: every legitimate
			// value is inside the allowed set, so anything outside it is a
			// typo or an injection, and a link carrying either is worse than
			// a link missing one optional parameter.
			if !rawParamIsSafe(p.Key, p.Value) {
				logger.Warning("sub: dropping ", p.Key,
					" from a generated link, it holds characters it cannot hold: ", p.Value)
				continue
			}
			q = append(q, fmt.Sprintf("%s=%s", p.Key, p.Value))
		default:
			q = append(q, fmt.Sprintf("%s=%s", p.Key, url.QueryEscape(p.Value)))
		}
	}
	return strings.Join(q, "&")
}

// linkURL assembles a link from its parts instead of formatting a string and
// parsing it back.
//
// That round trip is what turned a client password into a crash: url.Parse
// refuses a userinfo holding a space or a stray '%', addParams dropped the
// error, and the nil *url.URL was dereferenced on the next line. One client
// whose password contained a space took link generation down for every client
// on the inbound. url.URL escapes the userinfo itself, so there is nothing
// left here to get wrong.
func linkURL(scheme string, user *url.Userinfo, host string, port float64, params []LinkParam, remark string) string {
	u := url.URL{
		Scheme:   scheme,
		User:     user,
		Host:     fmt.Sprintf("%s:%.0f", HostForURI(host), port),
		Fragment: remark,
	}
	u.RawQuery = encodeParams(params)
	return u.String()
}

// addParams is the path for links whose authority is already encoded: the
// base64 payload of http2://. Everything else builds through linkURL.
func addParams(uri string, params []LinkParam, remark string) string {
	URL, err := url.Parse(uri)
	if err != nil || URL == nil {
		// Reported rather than dereferenced. Every caller that could reach here
		// with an unparseable string now goes through linkURL instead, so this
		// is a backstop -- but it used to be the crash itself.
		logger.Warning("sub: unable to parse a generated link, returning it unadorned: ", err)
		if q := encodeParams(params); q != "" {
			uri += "?" + q
		}
		if remark != "" {
			uri += "#" + url.PathEscape(remark)
		}
		return uri
	}
	URL.RawQuery = encodeParams(params)
	URL.Fragment = remark
	return URL.String()
}

// PortHoppingRanges renders a stored server_ports list the way every consumer
// outside sing-box spells it: comma-separated, with a dash between the ends of
// a range where sing-box writes a colon.
//
// Both consumers want the dash -- mihomo's `ports` and the "mport" query param
// hysteria and hysteria2 links carry -- and this package's own decoder says so
// too: linkToJson's hy2 reads mport back through
// strings.ReplaceAll(..., "-", ":"), which only round-trips a link that was
// written with dashes. Exported so the Clash converter shares it rather than
// keeping a second copy that can disagree about the separator, which is exactly
// how the link half came to emit "20000:30000" while the Clash half emitted
// "20000-30000" for the same row.
//
// A numeric entry is accepted as well as a string one. Not because sing-box
// writes one -- server_ports is a Listable[string] there and a JSON number is
// refused at parse, so a row holding one already fails the JSON subscription
// before this runs; NormalizePortRanges is what keeps that shape out of the
// column. It is read here so a row that somehow holds one still renders a link
// and a Clash proxy rather than an entry silently folded to "".
//
// Both list shapes are read too -- hy2 builds server_ports with a strings.Split,
// so an external or node-replica link carries []string where a stored row
// carries []interface{}.
func PortHoppingRanges(v interface{}) string {
	var ports []string
	switch entries := v.(type) {
	case []string:
		ports = entries
	case []interface{}:
		ports = make([]string, 0, len(entries))
		for _, entry := range entries {
			switch p := entry.(type) {
			case string:
				ports = append(ports, p)
			case float64:
				ports = append(ports, fmt.Sprintf("%.0f", p))
			}
		}
	default:
		return ""
	}
	rendered := make([]string, 0, len(ports))
	for _, port := range ports {
		// sing-box has no way to say "one port" but its own range, so a stored
		// row spells it "443:443"; every client spells it bare. Folded back
		// here, and serverPortsFromMport expands it again on the way in --
		// each side keeps its own spelling and the conversion stays at the
		// boundary.
		if start, end, isRange := strings.Cut(port, ":"); isRange && start == end {
			port = start
		}
		rendered = append(rendered, strings.ReplaceAll(port, ":", "-"))
	}
	return strings.Join(rendered, ",")
}

// normalizePortRange turns one port-hopping entry into the only shape sing-box
// accepts in server_ports: "start:end".
//
// Two things have to change, and only the first is obvious. The separator is a
// dash everywhere outside sing-box. The one that was missing is a *single*
// port: "443" is what every client writes for one port and a legal mport entry,
// but sing-box parses each server_ports entry as a range and refuses a bare one
// -- and it refuses it at **startup**, not at parse, so the config is accepted
// and then `bad port range: 443` comes out of NewBox. A single port is its own
// range, so it goes out as "443:443", which is verified to start.
//
// Returns "" for an entry that is not a port at all, so a caller can drop it
// rather than write something the subscriber's client will choke on.
func normalizePortRange(entry string) string {
	entry = strings.ReplaceAll(strings.TrimSpace(entry), "-", ":")
	if entry == "" {
		return ""
	}
	start, end, isRange := strings.Cut(entry, ":")
	if !isRange {
		end = start
	}
	// ParseUint with a 16-bit bound, byte for byte the check sing-box performs
	// on the same text. Atoi is not the same check and let two shapes through
	// the guard rather than around it: it accepts a value above 65535, and a
	// leading '+', both of which reach NewBox as `bad port range`.
	for _, part := range []string{start, end} {
		if _, err := strconv.ParseUint(part, 10, 16); err != nil {
			return ""
		}
	}
	return start + ":" + end
}

// NormalizePortRanges applies normalizePortRange to a stored server_ports list,
// reading either list shape and dropping entries that are not ports.
//
// This is the write-side guard. The read side cannot be the only one: an
// out_json row reaches the JSON subscription verbatim through getOutbounds, so
// an operator typing the entirely reasonable "443,20000:30000" into the free
// text box in OutJson.vue hands every sing-box subscriber a config that parses
// and then will not start -- no link, no round trip, nothing for the link
// builders to fix.
func NormalizePortRanges(v interface{}) []string {
	var entries []string
	switch list := v.(type) {
	case []string:
		entries = list
	case []interface{}:
		for _, item := range list {
			switch p := item.(type) {
			case string:
				entries = append(entries, p)
			case float64:
				entries = append(entries, fmt.Sprintf("%.0f", p))
			}
		}
	default:
		return nil
	}
	var ports []string
	for _, entry := range entries {
		if normalized := normalizePortRange(entry); normalized != "" {
			ports = append(ports, normalized)
		}
	}
	return ports
}

// serverPortsFromMport is PortHoppingRanges backwards: it reads the "mport"
// param off a hysteria or hysteria2 link and returns what server_ports wants.
// It used to be a bare ReplaceAll of the dash, which left a single port as the
// bare "443" sing-box refuses to start on.
func serverPortsFromMport(mport string) []string {
	if mport == "" {
		return nil
	}
	return NormalizePortRanges(strings.Split(mport, ","))
}

// portHoppingParam reads the multi-port range hysteria and hysteria2 advertise
// as "mport".
//
// It used to assert on inbound["out_json"] and bail out of the whole function
// when the unmarshal failed, so an inbound whose out_json was never filled --
// a row from an old backup, or one a migration wrote -- produced no links at
// all for those two protocols, silently.
func portHoppingParam(inbound map[string]interface{}) string {
	raw, ok := inbound["out_json"].(json.RawMessage)
	if !ok || len(raw) == 0 {
		return ""
	}
	var outJson map[string]interface{}
	if err := json.Unmarshal(raw, &outJson); err != nil {
		logger.Warning("sub: unable to read out_json for port hopping: ", err)
		return ""
	}
	return PortHoppingRanges(outJson["server_ports"])
}

func getTransportParams(t interface{}) []LinkParam {
	var params []LinkParam
	trasport, _ := t.(map[string]interface{})
	var transportType string
	if tt, ok := trasport["type"].(string); ok {
		transportType = tt
	} else {
		transportType = "tcp"
	}
	params = append(params, LinkParam{"type", transportType})
	if transportType == "tcp" {
		return params
	}

	switch transportType {
	case "http":
		if hosts := AsStringList(trasport["host"]); len(hosts) > 0 {
			params = append(params, LinkParam{"host", strings.Join(hosts, ",")})
		}
		if path, ok := trasport["path"].(string); ok {
			params = append(params, LinkParam{"path", path})
		}
	case "ws":
		if path, ok := trasport["path"].(string); ok {
			if maxED, ok := trasport["max_early_data"].(float64); ok && maxED > 0 {
				if edName, _ := trasport["early_data_header_name"].(string); edName == "Sec-WebSocket-Protocol" {
					sep := "?"
					if strings.Contains(path, "?") {
						sep = "&"
					}
					path = fmt.Sprintf("%s%sed=%d", path, sep, int(maxED))
				}
			}
			params = append(params, LinkParam{"path", path})
		}
		if headers, ok := trasport["headers"].(map[string]interface{}); ok {
			if host, ok := headers["Host"].(string); ok {
				params = append(params, LinkParam{"host", host})
			}
		}
	case "grpc":
		if serviceName, ok := trasport["service_name"].(string); ok {
			params = append(params, LinkParam{"serviceName", serviceName})
		}
	case "httpupgrade":
		if host, ok := trasport["host"].(string); ok {
			params = append(params, LinkParam{"host", host})
		}
		if path, ok := trasport["path"].(string); ok {
			params = append(params, LinkParam{"path", path})
		}
	}
	return params
}

func getTlsParams(params *[]LinkParam, tls map[string]interface{}, protocol string) {
	if reality, ok := tls["reality"].(map[string]interface{}); ok && AsBool(reality["enabled"]) {
		*params = append(*params, LinkParam{"security", "reality"})
		if pbk, ok := reality["public_key"].(string); ok {
			*params = append(*params, LinkParam{"pbk", pbk})
		}
		if sid, ok := reality["short_id"].(string); ok {
			*params = append(*params, LinkParam{"sid", sid})
		}
	} else {
		*params = append(*params, LinkParam{"security", "tls"})
		if insecure, ok := tls["insecure"].(bool); ok && insecure {
			*params = append(*params, LinkParam{insecureKeyFor(protocol), "1"})
		}
		if pin, ok := tls["pinSHA256"].(string); ok && pin != "" {
			*params = append(*params, LinkParam{pcsKeyFor(protocol), pin})
		}
		if disableSni, ok := tls["disable_sni"].(bool); ok && disableSni {
			*params = append(*params, LinkParam{"disable_sni", "1"})
		}
	}
	if utls, ok := tls["utls"].(map[string]interface{}); ok {
		if fingerprint, ok := utls["fingerprint"].(string); ok {
			*params = append(*params, LinkParam{"fp", fingerprint})
		}
	}
	if sni, ok := tls["server_name"].(string); ok {
		*params = append(*params, LinkParam{"sni", sni})
	}
	if alpnList := AsStringList(tls["alpn"]); len(alpnList) > 0 {
		*params = append(*params, LinkParam{"alpn", strings.Join(alpnList, ",")})
	}
}

func insecureKeyFor(protocol string) string {
	switch protocol {
	case "vless", "trojan", "vmess":
		return "allowInsecure"
	}
	return "insecure"
}

// Xray-based clients read the certificate pin as "pcs"; only the hysteria
// URI scheme (hy/hy2) uses "pinSHA256" (upstream #1093 follow-up).
func pcsKeyFor(protocol string) string {
	switch protocol {
	case "hysteria", "hysteria2":
		return "pinSHA256"
	}
	return "pcs"
}
