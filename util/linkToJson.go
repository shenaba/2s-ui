package util

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/util/common"
)

func GetOutbound(uri string, i int) (*map[string]interface{}, string, error) {
	u, err := url.Parse(uri)
	if isShadowsocksLink(uri) && (err != nil || u.User == nil) {
		if rebuilt, ok := rebuildShadowsocksLink(uri); ok {
			u, err = rebuilt, nil
		}
	}
	if err == nil {
		switch u.Scheme {
		case "vmess":
			return vmess(vmessPayload(uri), i)
		case "vless":
			return vless(u, i)
		case "trojan":
			return trojan(u, i)
		case "hy", "hysteria":
			return hy(u, i)
		case "hy2", "hysteria2":
			return hy2(u, i)
		case "anytls":
			return anytls(u, i)
		case "tuic":
			return tuic(u, i)
		case "ss", "shadowsocks":
			return ss(u, i)
		case "naive+https", "naive+quic", "http2":
			return parseNaiveLink(u, i)
		case "socks", "socks5":
			return socksProxy(u, i)
		case "http", "https":
			return httpProxy(u, i)
		}
	}
	return nil, "", common.NewError("Unsupported link format")
}

// socksProxy and httpProxy read back the links socksLink and httpLink write.
//
// GetOutbound answered "Unsupported link format" for both, and the callers
// drop a link they cannot parse without a word -- so a node replica of a
// socks, http or mixed inbound was served in the plain-link subscription and
// silently missing from the JSON and Clash ones. Those are three of the twelve
// types in InboundTypeWithLink.
//
// Both insist on an explicit port. http:// and https:// are also what an
// ordinary web address looks like, and a text subscription body is fed to
// GetOutbound a line at a time -- so without a rule that tells the two apart,
// every URL in one would become a proxy. A proxy link always carries a port
// (there is no well-known one to fall back on) and never a path.
func proxyEndpoint(u *url.URL) (string, int, error) {
	if u.Path != "" && u.Path != "/" {
		return "", 0, common.NewError("Unsupported link format")
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", 0, common.NewError("Unsupported link format")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, common.NewError("Unsupported link format")
	}
	return host, port, nil
}

// proxyCredentials copies the userinfo proxyUserinfo wrote. Both fields are
// omitted when there is no userinfo at all: sing-box reads their presence as
// "authenticate", and an empty username and password is not the same request
// as none.
func proxyCredentials(out map[string]interface{}, u *url.URL) {
	if u.User == nil {
		return
	}
	out["username"] = u.User.Username()
	password, _ := u.User.Password()
	out["password"] = password
}

func socksProxy(u *url.URL, i int) (*map[string]interface{}, string, error) {
	host, port, err := proxyEndpoint(u)
	if err != nil {
		return nil, "", err
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	socks := map[string]interface{}{
		"type":        "socks",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		// socksLink writes a SOCKS5 userinfo, and 4/4a carry no password at
		// all. sing-box defaults to 5 too, but writing it keeps the outbound
		// readable on its own.
		"version": "5",
	}
	proxyCredentials(socks, u)
	return &socks, tag, nil
}

func httpProxy(u *url.URL, i int) (*map[string]interface{}, string, error) {
	host, port, err := proxyEndpoint(u)
	if err != nil {
		return nil, "", err
	}
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	http := map[string]interface{}{
		"type":        "http",
		"tag":         tag,
		"server":      host,
		"server_port": port,
	}
	if u.Scheme == "https" {
		// httpLink picks its scheme from each address's own tls.enabled, so
		// the scheme is the only thing in the link that says the listener
		// speaks TLS. server_name is left to sing-box, which defaults it to
		// the server address -- which is what the link carries.
		http["tls"] = map[string]interface{}{"enabled": true}
	}
	proxyCredentials(http, u)
	return &http, tag, nil
}

// vmessPayload is everything after "vmess://", not u.Host: the standard
// alphabet has '/', which url.Parse takes for the start of a path, so a payload
// carrying one was cut short and failed to decode. A non-ASCII remark is enough
// to put one there, and the links this panel generates are standard base64 --
// so a node replica's vmess link could vanish from the JSON and Clash
// subscriptions. No base64 alphabet has '?' or '#', so a query or a remark some
// clients append still ends the payload, as it did before.
func vmessPayload(uri string) string {
	_, payload, _ := strings.Cut(uri, "://")
	if end := strings.IndexAny(payload, "?#"); end != -1 {
		payload = payload[:end]
	}
	return payload
}

func vmess(data string, i int) (*map[string]interface{}, string, error) {
	dataByte, err := B64StrToByte(data)
	if err != nil {
		return nil, "", err
	}
	var dataJson map[string]interface{}
	err = json.Unmarshal(dataByte, &dataJson)
	if err != nil {
		return nil, "", err
	}
	transport := map[string]interface{}{}
	tp_net, _ := dataJson["net"].(string)
	tp_type, _ := dataJson["type"].(string)
	tp_host, _ := dataJson["host"].(string)
	tp_path, _ := dataJson["path"].(string)
	switch strings.ToLower(tp_net) {
	case "tcp", "":
		if tp_type == "http" {
			transport["type"] = tp_type
			if len(tp_host) > 0 {
				transport["host"] = strings.Split(tp_host, ",")
			}
			transport["path"] = tp_path
		}
	case "http", "h2":
		transport["type"] = "http"
		if len(tp_host) > 0 {
			transport["host"] = strings.Split(tp_host, ",")
		}
		transport["path"] = tp_path
	case "ws":
		transport["type"] = tp_net
		transport["path"] = tp_path
		transport["early_data_header_name"] = "Sec-WebSocket-Protocol"
		if len(tp_host) > 0 {
			transport["headers"] = map[string]interface{}{
				"Host": tp_host,
			}
		}
	case "quic":
		transport["type"] = tp_net
	case "grpc":
		transport["type"] = tp_net
		transport["service_name"] = tp_path
	case "httpupgrade":
		transport["type"] = tp_net
		transport["path"] = tp_path
		transport["host"] = tp_host
	default:
		return nil, "", common.NewError("Invalid vmess")
	}
	tls := map[string]interface{}{}
	vmess_tls, _ := dataJson["tls"].(string)
	if vmess_tls == "tls" {
		tls["enabled"] = true
		tls_sni, _ := dataJson["sni"].(string)
		tls_alpn, _ := dataJson["alpn"].(string)
		_, tls_insecure := dataJson["allowInsecure"]
		tls_fp, _ := dataJson["fp"].(string)
		if len(tls_sni) > 0 {
			tls["server_name"] = tls_sni
		}
		if len(tls_alpn) > 0 {
			tls["alpn"] = strings.Split(tls_alpn, ",")
		}
		if tls_insecure {
			tls["insecure"] = true
		}
		if len(tls_fp) > 0 {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": tls_fp,
			}
		}
	}
	tag, _ := dataJson["ps"].(string)
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, tag)
	}
	alter_id := 0
	if aid, ok := dataJson["aid"].(float64); ok {
		alter_id = int(aid)
	}
	// A vmess link carries the port as a *string* by convention -- vmessLink
	// writes fmt.Sprintf("%.0f", port) and so does every other generator -- and
	// this used to hand that string on as server_port. sing-box declares the
	// field uint16 and refuses a JSON string, which fails the **whole** config
	// rather than this one outbound: a single vmess node replica took the
	// entire sing-box subscription down for every client referencing it. The
	// Clash side survived only because mihomo decodes weakly typed.
	//
	// Both shapes are read: some clients write the port as a number instead.
	// Every other protocol here reaches server_port through strconv.Atoi, so
	// this was the only one left carrying the wrong type.
	var vmess_port int
	switch p := dataJson["port"].(type) {
	case string:
		vmess_port, _ = strconv.Atoi(p)
	case float64:
		vmess_port = int(p)
	}
	vmess := map[string]interface{}{
		"type":        "vmess",
		"tag":         tag,
		"server":      dataJson["add"],
		"server_port": vmess_port,
		"uuid":        dataJson["id"],
		"security":    "auto",
		"alter_id":    alter_id,
		"tls":         tls,
		"transport":   transport,
	}
	return &vmess, tag, err
}

func vless(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	security := query.Get("security")
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 80
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	} else {
		if security == "tls" || security == "reality" {
			port = 443
		}
	}
	tp_type := query.Get("type")
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	vless := map[string]interface{}{
		"type":        "vless",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"uuid":        u.User.Username(),
		"flow":        query.Get("flow"),
		"tls":         getTls(security, &query),
		"transport":   getTransport(tp_type, &query),
	}
	return &vless, tag, nil
}

func trojan(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	security := query.Get("security")
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 80
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	} else {
		if security == "tls" || security == "reality" {
			port = 443
		}
	}
	tp_type := query.Get("type")
	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	trojan := map[string]interface{}{
		"type":        "trojan",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    u.User.Username(),
		"tls":         getTls(security, &query),
		"transport":   getTransport(tp_type, &query),
	}
	return &trojan, tag, nil
}

func hy(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}

	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	hy := map[string]interface{}{
		"type":        "hysteria",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"obfs":        query.Get("obfsParam"),
		"auth_str":    query.Get("auth"),
		"tls":         getTls(security, &query),
	}
	down, _ := strconv.Atoi(query.Get("downmbps"))
	up, _ := strconv.Atoi(query.Get("upmbps"))
	recv_window_conn, _ := strconv.Atoi(query.Get("recv_window_conn"))
	recv_window, _ := strconv.Atoi(query.Get("recv_window"))
	// Both bandwidths are mandatory for hysteria v1 and the URI makes them
	// optional, so a link can legally arrive without them -- and sing-quic
	// refuses such a client with "missing upload speed" at start-up, after the
	// document has already parsed. That failure is not survivable by the one
	// outbound: option.Options fails whole, so a single bandwidth-less external
	// link costs every client referencing it its entire sing-box subscription.
	//
	// Refused as a link instead. GetExternalOutbounds already skips a link it
	// cannot decode, so the bad one drops itself rather than the document, and
	// api/linkConvert tells the operator why at the moment they paste it --
	// which is the only place a useful message can still reach them.
	if down <= 0 || up <= 0 {
		return nil, "", common.NewError("hysteria link is missing upmbps/downmbps, which sing-box requires")
	}
	hy["down_mbps"] = down
	hy["up_mbps"] = up
	if recv_window_conn > 0 {
		hy["recv_window_conn"] = recv_window_conn
	}
	if recv_window > 0 {
		hy["recv_window"] = recv_window
	}
	return &hy, tag, nil
}

// The hysteria2 obfs types, as sing-box's constant package spells them. This
// package stays clear of the sing-box module, so they are repeated here once
// for the link parser and the Clash export to share; a test in sub, which does
// import sing-box, holds them to the originals.
const (
	Hysteria2ObfsSalamander = "salamander"
	Hysteria2ObfsGecko      = "gecko"
)

func hy2(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}

	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	hy2 := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    u.User.Username(),
		"tls":         getTls(security, &query),
	}
	down, _ := strconv.Atoi(query.Get("downmbps"))
	up, _ := strconv.Atoi(query.Get("upmbps"))
	obfs := query.Get("obfs")
	mport := serverPortsFromMport(query.Get("mport"))
	fastopen := query.Get("fastopen")
	if down > 0 {
		hy2["down_mbps"] = down
	}
	if up > 0 {
		hy2["up_mbps"] = up
	}
	// Both types sing-box knows. The URI scheme carries no packet sizes for
	// gecko, and none are needed: they only shape what this side sends, and a
	// client without them pads to its core's own default range.
	//
	// Only with a password, though. sing-box and mihomo both refuse an obfs
	// that has none, and a refused outbound takes the subscriber's whole
	// config with it, where an outbound with no obfs is one node that does
	// not connect.
	obfsPassword := query.Get("obfs-password")
	switch obfs {
	case Hysteria2ObfsSalamander, Hysteria2ObfsGecko:
		if obfsPassword != "" {
			hy2["obfs"] = map[string]interface{}{
				"type":     obfs,
				"password": obfsPassword,
			}
		}
	}
	if len(mport) > 0 {
		hy2["server_ports"] = mport
	}
	if fastopen == "1" || fastopen == "true" {
		hy2["fastopen"] = true
	}
	return &hy2, tag, nil
}

func anytls(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}

	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	anytls := map[string]interface{}{
		"type":        "anytls",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    u.User.Username(),
		"tls":         getTls(security, &query),
	}
	return &anytls, tag, nil
}

func tuic(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}

	security := query.Get("security")
	if len(security) == 0 {
		security = "tls"
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	password, _ := u.User.Password()
	tuic := map[string]interface{}{
		"type":               "tuic",
		"tag":                tag,
		"server":             host,
		"server_port":        port,
		"uuid":               u.User.Username(),
		"password":           password,
		"congestion_control": query.Get("congestion_control"),
		"udp_relay_mode":     query.Get("udp_relay_mode"),
		"tls":                getTls(security, &query),
	}
	return &tuic, tag, nil
}

func isShadowsocksLink(uri string) bool {
	scheme, _, ok := strings.Cut(uri, "://")
	return ok && (strings.EqualFold(scheme, "ss") || strings.EqualFold(scheme, "shadowsocks"))
}

// rebuildShadowsocksLink rewrites the ss links url.Parse cannot find a
// userinfo in as a SIP002 link with a base64url userinfo, which it reads whole.
// Each of these was dropped from every subscription that parses links (#1281):
//
//   - the legacy form, ss://BASE64(method:password@host:port), which has no
//     userinfo to find -- providers still serve it;
//   - a userinfo in standard base64 with a '/' in it: url.Parse ends the
//     authority there, so the '@' lands in the path;
//   - a plain-text 2022 userinfo whose base64 key has a '/', which SIP002 says
//     to percent-encode and some generators do not.
//
// Only reached when the plain parse failed or found no userinfo, so a link that
// already parses is never touched.
func rebuildShadowsocksLink(uri string) (*url.URL, bool) {
	scheme, rest, _ := strings.Cut(uri, "://")
	rest, fragment, hasFragment := strings.Cut(rest, "#")

	var userInfo, hostAndRest string
	if at := strings.Index(rest, "@"); at != -1 {
		// A base64 userinfo has no '@', so the first one ends it.
		encoded := rest[:at]
		hostAndRest = rest[at+1:]
		if decoded, err := decodeAnyBase64(encoded); err == nil {
			userInfo = string(decoded)
		} else if plain, err := url.PathUnescape(encoded); err == nil {
			userInfo = plain
		} else {
			return nil, false
		}
	} else {
		// Legacy: the whole body is one blob, possibly followed by a plugin
		// query. A trailing '/' may be the separator before that query or the
		// blob's own last character, so both readings are tried.
		body, query, hasQuery := strings.Cut(rest, "?")
		decoded, err := decodeAnyBase64(body)
		if err != nil {
			if decoded, err = decodeAnyBase64(strings.TrimSuffix(body, "/")); err != nil {
				return nil, false
			}
		}
		// The last '@': a password may hold one, a host:port cannot.
		at := strings.LastIndex(string(decoded), "@")
		if at == -1 {
			return nil, false
		}
		userInfo, hostAndRest = string(decoded[:at]), string(decoded[at+1:])
		if hasQuery {
			hostAndRest += "/?" + query
		}
	}
	if method, _, ok := strings.Cut(userInfo, ":"); !ok || method == "" {
		return nil, false
	}

	rebuilt := scheme + "://" + base64.RawURLEncoding.EncodeToString([]byte(userInfo)) + "@" + hostAndRest
	if hasFragment {
		rebuilt += "#" + fragment
	}
	u, err := url.Parse(rebuilt)
	if err != nil || u.User == nil {
		return nil, false
	}
	return u, true
}

func ss(u *url.URL, i int) (*map[string]interface{}, string, error) {
	query, _ := url.ParseQuery(u.RawQuery)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port := 443
	if len(portStr) > 0 {
		port, _ = strconv.Atoi(portStr)
	}
	method := u.User.Username()
	password, ok := u.User.Password()
	if !ok {
		// Any base64 form: SIP002 mandates base64url without padding, which is
		// what this panel now writes and what third-party links carry, while
		// links written before that are standard base64.
		decrypted := StrOrBase64AnyEncoded(method)
		decrypted_arr := strings.Split(decrypted, ":")
		if len(decrypted_arr) > 1 {
			method = decrypted_arr[0]
			password = strings.Join(decrypted_arr[1:], ":")
		} else {
			return nil, "", common.NewError("Unsupported shadowsocks")
		}
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	ss := map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}

	v2ray_type := query.Get("type")
	if len(v2ray_type) > 0 {
		pl_arr := []string{}
		host_header := query.Get("host")
		if query.Get("security") == "tls" {
			pl_arr = append(pl_arr, "tls")
		}
		if v2ray_type == "quic" {
			pl_arr = append(pl_arr, "mode=quic")
		}
		if len(host_header) > 0 {
			pl_arr = append(pl_arr, "host="+host_header)
		}
		ss["plugin"] = "v2ray-plugin"
		ss["plugin_opts"] = strings.Join(pl_arr, ";")
	}
	plugin := query.Get("plugin")
	if len(plugin) > 0 {
		pl_arr := strings.Split(plugin, ";")
		if len(pl_arr) > 0 {
			ss["plugin"] = pl_arr[0]
			ss["plugin_opts"] = strings.Join(pl_arr[1:], ";")
		}
	}
	return &ss, tag, nil
}

func parseNaiveLink(u *url.URL, i int) (*map[string]interface{}, string, error) {
	var host, portStr, username, password string
	var port int

	switch u.Scheme {
	case "http2":
		decoded := StrOrBase64Encoded(u.Hostname())
		if idx := strings.Index(decoded, "@"); idx != -1 {
			userInfo := decoded[:idx]
			hostPort := decoded[idx+1:]
			if idx2 := strings.Index(userInfo, ":"); idx2 != -1 {
				username = userInfo[:idx2]
				password = userInfo[idx2+1:]
			} else {
				username = userInfo
			}
			host, portStr, _ = net.SplitHostPort(hostPort)
			if portStr != "" {
				port, _ = strconv.Atoi(portStr)
			} else {
				port = 443
			}
		} else {
			return nil, "", common.NewError("Invalid naive link (http2)")
		}
	case "naive+https", "naive+quic":
		host, portStr, _ = net.SplitHostPort(u.Host)
		if portStr != "" {
			port, _ = strconv.Atoi(portStr)
		} else {
			port = 443
		}
		if u.User != nil {
			username = u.User.Username()
			password, _ = u.User.Password()
		}
	default:
		return nil, "", common.NewError("Unsupported naive scheme")
	}

	tag := u.Fragment
	if i > 0 {
		tag = fmt.Sprintf("%d.%s", i, u.Fragment)
	}
	if tag == "" {
		tag = fmt.Sprintf("naive-%d", i)
	}

	naive := map[string]interface{}{
		"type":        "naive",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"username":    username,
		"password":    password,
		"tls":         map[string]interface{}{"enabled": true},
	}

	query := u.Query()
	if peer := query.Get("peer"); peer != "" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["server_name"] = peer
		}
	}
	if insecure := query.Get("insecure"); insecure == "1" || insecure == "true" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["insecure"] = true
		}
	}
	if alpn := query.Get("alpn"); alpn != "" {
		if tls, ok := naive["tls"].(map[string]interface{}); ok {
			tls["alpn"] = strings.Split(alpn, ",")
		}
	}
	if u.Scheme == "naive+quic" {
		naive["quic"] = true
	}

	return &naive, tag, nil
}

func getTransport(tp_type string, q *url.Values) map[string]interface{} {
	transport := map[string]interface{}{}
	tp_host := q.Get("host")
	tp_path := q.Get("path")
	switch strings.ToLower(tp_type) {
	case "tcp", "":
		if q.Get("headerType") == "http" {
			transport["type"] = "http"
			if len(tp_host) > 0 {
				transport["host"] = strings.Split(tp_host, ",")
			}
			transport["path"] = tp_path
		}
	case "http", "h2":
		transport["type"] = "http"
		if len(tp_host) > 0 {
			transport["host"] = strings.Split(tp_host, ",")
		}
		transport["path"] = tp_path
	case "ws":
		transport["type"] = "ws"
		transport["path"] = tp_path
		if len(tp_host) > 0 {
			transport["headers"] = map[string]interface{}{
				"Host": tp_host,
			}
		}
	case "quic":
		transport["type"] = "quic"
	case "grpc":
		transport["type"] = "grpc"
		transport["service_name"] = q.Get("serviceName")
	case "httpupgrade":
		transport["type"] = "httpupgrade"
		transport["path"] = tp_path
		transport["host"] = tp_host
	}
	return transport
}

// GetOutboundForClash is GetOutbound for the Clash export, which can carry an
// Xray XHTTP transport that sing-box cannot. A vless link with type=xhttp (or
// its old name splithttp) keeps host/path/mode and the JSON "extra" blob on its
// transport for ConvertToClashMeta to map onto mihomo's xhttp-opts. Everything
// else, and every sing-box JSON caller, gets GetOutbound's result unchanged.
// sing-box has no XHTTP transport, and getTransport leaves one empty, so there
// such a link is still served as a plain-TCP vless -- a node that cannot
// connect, as it was before the Clash export learned this.
func GetOutboundForClash(uri string, i int) (*map[string]interface{}, string, error) {
	outbound, tag, err := GetOutbound(uri, i)
	if err != nil || outbound == nil {
		return outbound, tag, err
	}
	u, parseErr := url.Parse(uri)
	if parseErr != nil || u.Scheme != "vless" {
		return outbound, tag, err
	}
	q := u.Query()
	tp := strings.ToLower(q.Get("type"))
	if tp != "xhttp" && tp != "splithttp" {
		return outbound, tag, err
	}
	transport := map[string]interface{}{"type": "xhttp"}
	// Presence, not value: an explicit empty host still overrides the one in
	// extra (xhttpOptsFromTransport).
	for _, key := range []string{"host", "path", "mode"} {
		if q.Has(key) {
			transport[key] = q.Get(key)
		}
	}
	if raw := q.Get("extra"); raw != "" && raw != "null" {
		var extra map[string]interface{}
		// UseNumber keeps byte counts like scMaxEachPostBytes exact, and the
		// second Decode refuses an extra with anything after the object.
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&extra) == nil && len(extra) > 0 {
			var trailing interface{}
			if decoder.Decode(&trailing) == io.EOF {
				transport["extra"] = extra
			}
		}
	}
	(*outbound)["transport"] = transport
	return outbound, tag, nil
}

func getTls(security string, q *url.Values) map[string]interface{} {
	tls := map[string]interface{}{}
	tls_fp := q.Get("fp")
	tls_sni := q.Get("sni")
	tls_allow_insecure := q.Get("allowInsecure")
	tls_insecure := q.Get("insecure")
	tls_alpn := q.Get("alpn")
	tls_ech := q.Get("ech")
	disable_sni := q.Get("disable_sni")
	switch security {
	case "tls":
		tls["enabled"] = true
	case "reality":
		tls["enabled"] = true
		tls["reality"] = map[string]interface{}{
			"enabled":    true,
			"public_key": q.Get("pbk"),
			"short_id":   q.Get("sid"),
		}
	}
	if len(tls_sni) > 0 {
		tls["server_name"] = tls_sni
	}
	if len(tls_alpn) > 0 {
		tls["alpn"] = strings.Split(tls_alpn, ",")
	}
	if tls_insecure == "1" || tls_insecure == "true" || tls_allow_insecure == "1" || tls_allow_insecure == "true" {
		tls["insecure"] = true
	}
	if len(tls_fp) > 0 {
		tls["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": tls_fp,
		}
	}
	if len(tls_ech) > 0 {
		tls["ech"] = map[string]interface{}{
			"enabled": true,
			"config": []string{
				tls_ech,
			},
		}
	}
	if disable_sni == "1" || disable_sni == "true" {
		tls["disable_sni"] = true
	}
	return tls
}
