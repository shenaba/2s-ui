package util

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

func TestNaiveLinkSchemesFollowNetwork(t *testing.T) {
	tests := []struct {
		network string
		want    []string
	}{
		// The plain naive+ scheme has to match the listener's network, or the
		// link is dead. The legacy http2:// form goes out either way, for
		// clients that parse nothing else -- on a udp-only inbound that one is
		// dead too, and kept anyway rather than diverging from upstream.
		{"tcp", []string{"http2://", "naive+https://"}},
		{"udp", []string{"http2://", "naive+quic://"}},

		// Unset means sing-box listens on both.
		{"", []string{"http2://", "naive+https://", "naive+quic://"}},
	}

	addrs := []map[string]interface{}{{
		"server":      "example.com",
		"server_port": float64(443),
		"remark":      "naive-in",
	}}

	for _, tt := range tests {
		name := tt.network
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			inbound := map[string]interface{}{}
			if tt.network != "" {
				inbound["network"] = tt.network
			}
			links := naiveLink(map[string]interface{}{"username": "u", "password": "p"}, inbound, addrs)

			if len(links) != len(tt.want) {
				t.Fatalf("got %d links %v, want %d", len(links), links, len(tt.want))
			}
			for i, prefix := range tt.want {
				if !strings.HasPrefix(links[i], prefix) {
					t.Errorf("link %d = %q, want prefix %q", i, links[i], prefix)
				}
			}
		})
	}
}

func TestNaiveLinkEscapesUserinfo(t *testing.T) {
	links := naiveLink(
		// A space must survive as %20: '+' would read back as a literal plus.
		map[string]interface{}{"username": "a b", "password": "p@ss/word"},
		map[string]interface{}{"network": "tcp"},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "naive-in",
		}},
	)

	const want = "naive+https://a%20b:p%40ss%2Fword@example.com:443"
	if len(links) != 2 || !strings.HasPrefix(links[1], want) {
		t.Errorf("got %v, want a link starting with %q", links, want)
	}
}

func TestNaiveLinkRemarksDistinguishTransport(t *testing.T) {
	links := naiveLink(
		map[string]interface{}{"username": "u", "password": "p"},
		map[string]interface{}{},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "naive-in",
		}},
	)

	// Three links for one address, so the plain ones say which transport they
	// are. The legacy link keeps the bare remark: renaming it would read as a
	// different node to a client that already has it.
	want := []string{"#naive-in", "#naive-in-h2", "#naive-in-h3"}
	if len(links) != len(want) {
		t.Fatalf("got %d links %v, want %d", len(links), links, len(want))
	}
	for i, fragment := range want {
		if !strings.HasSuffix(links[i], fragment) {
			t.Errorf("link %d = %q, want suffix %q", i, links[i], fragment)
		}
	}
}

// An IPv6 hostname reaches LinkGenerator bracketed (api.normalizeHost does
// that) or bare (an address row holds whatever the operator typed). Both have
// to come out the same way: bracketed inside a URI authority, bare in the
// vmess "add" field, which is JSON rather than a URI (#1220).
func TestLinkGeneratorIPv6Hostname(t *testing.T) {
	newInbound := func(inboundType, addrs string) *model.Inbound {
		return &model.Inbound{
			Type:    inboundType,
			Tag:     "v6-in",
			Addrs:   json.RawMessage(addrs),
			Options: json.RawMessage(`{"listen_port":443}`),
		}
	}

	t.Run("vless brackets the authority", func(t *testing.T) {
		for _, hostname := range []string{"[2001:db8::1]", "2001:db8::1"} {
			links := LinkGenerator(
				json.RawMessage(`{"vless":{"uuid":"uuid-1"}}`),
				newInbound("vless", "null"), hostname, "")

			const want = "vless://uuid-1@[2001:db8::1]:443"
			if len(links) != 1 || !strings.HasPrefix(links[0], want) {
				t.Errorf("hostname %q gave %v, want a link starting with %q", hostname, links, want)
			}
		}
	})

	t.Run("vmess add stays bare", func(t *testing.T) {
		links := LinkGenerator(
			json.RawMessage(`{"vmess":{"uuid":"uuid-1"}}`),
			newInbound("vmess", "null"), "[2001:db8::1]", "")

		if len(links) != 1 {
			t.Fatalf("got %d links %v, want 1", len(links), links)
		}
		raw, err := B64StrToByte(strings.TrimPrefix(links[0], "vmess://"))
		if err != nil {
			t.Fatalf("decoding %q: %v", links[0], err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("unmarshalling %q: %v", raw, err)
		}
		if got := obj["add"]; got != "2001:db8::1" {
			t.Errorf(`add = %q, want "2001:db8::1"`, got)
		}
	})

	t.Run("address rows are normalised too", func(t *testing.T) {
		addrs := `[{"server":"[2001:db8::2]","server_port":8443,"remark":"-alt"}]`
		links := LinkGenerator(
			json.RawMessage(`{"trojan":{"password":"pw"}}`),
			newInbound("trojan", addrs), "example.com", "")

		const want = "trojan://pw@[2001:db8::2]:8443"
		if len(links) != 1 || !strings.HasPrefix(links[0], want) {
			t.Errorf("got %v, want a link starting with %q", links, want)
		}
	})
}

// A client credential is operator-supplied text. The builders used to format it
// into a string and parse that back, and url.Parse refuses a userinfo holding a
// space or a stray '%': addParams dropped the error and dereferenced the nil
// *url.URL, so one such client took every link on its inbound down with it.
func TestLinkGeneratorEscapesAwkwardCredentials(t *testing.T) {
	const awkward = `p ss%wor#d/x`

	tests := []struct {
		inboundType string
		config      string
		wantUser    string // userinfo username after parsing back
		wantPass    string // "" when the scheme carries the secret as the username
	}{
		{"trojan", `{"trojan":{"password":` + strconv.Quote(awkward) + `}}`, awkward, ""},
		{"hysteria2", `{"hysteria2":{"password":` + strconv.Quote(awkward) + `}}`, awkward, ""},
		{"anytls", `{"anytls":{"password":` + strconv.Quote(awkward) + `}}`, awkward, ""},
		{"tuic", `{"tuic":{"uuid":"uuid-1","password":` + strconv.Quote(awkward) + `}}`, "uuid-1", awkward},
		{"socks", `{"socks":{"username":"u v","password":` + strconv.Quote(awkward) + `}}`, "u v", awkward},
		{"http", `{"http":{"username":"u v","password":` + strconv.Quote(awkward) + `}}`, "u v", awkward},
	}

	for _, tt := range tests {
		t.Run(tt.inboundType, func(t *testing.T) {
			links := LinkGenerator(
				json.RawMessage(tt.config),
				&model.Inbound{
					Type:    tt.inboundType,
					Tag:     "in",
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(`{"listen_port":443}`),
				},
				"example.com", "")

			if len(links) != 1 {
				t.Fatalf("got %d links %v, want 1", len(links), links)
			}
			u, err := url.Parse(links[0])
			if err != nil {
				t.Fatalf("the generated link does not parse: %q: %v", links[0], err)
			}
			if got := u.User.Username(); got != tt.wantUser {
				t.Errorf("username = %q, want %q (link %q)", got, tt.wantUser, links[0])
			}
			pass, _ := u.User.Password()
			if pass != tt.wantPass {
				t.Errorf("password = %q, want %q (link %q)", pass, tt.wantPass, links[0])
			}
			if u.Host != "example.com:443" {
				t.Errorf("host = %q, want %q", u.Host, "example.com:443")
			}
		})
	}
}

// socks was the one protocol that dropped the remark, so its nodes reached the
// client unnamed while every other protocol carried a name.
func TestSocksAndHttpLinksCarryRemark(t *testing.T) {
	addrs := []map[string]interface{}{{
		"server":      "example.com",
		"server_port": float64(1080),
		"remark":      "alice-in",
	}}
	cfg := map[string]interface{}{"username": "u", "password": "p"}

	// An ordered slice with subtests, not a map: Go randomises map iteration,
	// so with a t.Fatalf in the loop a failing build reported a different set
	// of assertions on each run and -run could not isolate either case.
	tests := []struct {
		name  string
		links []string
	}{
		{"socks", socksLink(cfg, addrs, "")},
		{"http", httpLink(cfg, addrs, "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.links) != 1 {
				t.Fatalf("got %d links %v, want 1", len(tt.links), tt.links)
			}
			if !strings.HasSuffix(tt.links[0], "#alice-in") {
				t.Errorf("link = %q, want it to end with %q", tt.links[0], "#alice-in")
			}
		})
	}
}

// An inbound with no authentication reaches these with an empty (or nil) user
// config, and the two absent values used to be formatted straight into the link
// as the literal text "%!s(<nil>):%!s(<nil>)".
func TestSocksAndHttpLinksOmitEmptyCredentials(t *testing.T) {
	addrs := []map[string]interface{}{{
		"server":      "example.com",
		"server_port": float64(1080),
		"remark":      "no-auth",
	}}

	for _, cfg := range []map[string]interface{}{nil, {}} {
		for _, links := range [][]string{socksLink(cfg, addrs, ""), httpLink(cfg, addrs, "")} {
			if len(links) != 1 {
				t.Fatalf("got %d links %v, want 1", len(links), links)
			}
			if strings.Contains(links[0], "@") {
				t.Errorf("link = %q, want no userinfo at all when neither is set", links[0])
			}
		}
	}
}

// The scheme was decided once outside the loop, so a single TLS-enabled address
// turned every address after it into https whatever its own setting was; and it
// was decided on the key being present rather than on enabled, so a TLS config
// that is switched off still produced https.
func TestHttpLinkDecidesSchemePerAddress(t *testing.T) {
	links := httpLink(
		map[string]interface{}{"username": "u", "password": "p"},
		[]map[string]interface{}{
			{"server": "tls.example.com", "server_port": float64(443), "remark": "a",
				"tls": map[string]interface{}{"enabled": true}},
			{"server": "plain.example.com", "server_port": float64(80), "remark": "b"},
			// LinkGenerator attaches the tls map to every address whenever the
			// inbound references a TLS row, without consulting enabled.
			{"server": "off.example.com", "server_port": float64(80), "remark": "c",
				"tls": map[string]interface{}{"enabled": false}},
			// prepareTls returns a nil map on a malformed row, which is a typed
			// nil and satisfies a bare != nil.
			{"server": "nil.example.com", "server_port": float64(80), "remark": "d",
				"tls": map[string]interface{}(nil)},
		},
		"",
	)

	want := []string{"https://", "http://", "http://", "http://"}
	if len(links) != len(want) {
		t.Fatalf("got %d links %v, want %d", len(links), links, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(links[i], prefix) {
			t.Errorf("link %d = %q, want prefix %q", i, links[i], prefix)
		}
	}
}

// A mixed inbound emits one socks and one http link per address. Without a
// suffix both carry the same node name and a subscriber cannot tell them apart
// — the problem naiveLink solves with -h2/-h3 and pushMixed with these exact two.
func TestMixedLinksAreDistinguishable(t *testing.T) {
	links := LinkGenerator(
		json.RawMessage(`{"socks":{"username":"u","password":"p"},"http":{"username":"u","password":"p"}}`),
		&model.Inbound{
			Type:    "mixed",
			Tag:     "mix-in",
			Addrs:   json.RawMessage("null"),
			Options: json.RawMessage(`{"listen_port":1080}`),
		},
		"example.com", "alice")

	want := []string{"#alice-mix-in-socks", "#alice-mix-in-http"}
	if len(links) != len(want) {
		t.Fatalf("got %d links %v, want %d", len(links), links, len(want))
	}
	for i, fragment := range want {
		if !strings.HasSuffix(links[i], fragment) {
			t.Errorf("link %d = %q, want suffix %q", i, links[i], fragment)
		}
	}

	// A socks-only inbound has nothing to be told apart from, so its nodes keep
	// the bare name — renaming them would read as a new node to every client
	// that already has one.
	plain := LinkGenerator(
		json.RawMessage(`{"socks":{"username":"u","password":"p"}}`),
		&model.Inbound{
			Type:    "socks",
			Tag:     "socks-in",
			Addrs:   json.RawMessage("null"),
			Options: json.RawMessage(`{"listen_port":1080}`),
		},
		"example.com", "alice")
	if len(plain) != 1 || !strings.HasSuffix(plain[0], "#alice-socks-in") {
		t.Errorf("got %v, want a single link ending in %q", plain, "#alice-socks-in")
	}
}

func TestShadowsocksLinkUsesSIP002Userinfo(t *testing.T) {
	links := shadowsocksLink(
		map[string]map[string]interface{}{
			"shadowsocks": {"password": "pa+ss/word=="},
		},
		map[string]interface{}{"method": "aes-128-gcm"},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(8388),
			// A space in the node name used to be concatenated straight into
			// the fragment.
			"remark": "alice in",
		}},
	)

	if len(links) != 1 {
		t.Fatalf("got %d links %v, want 1", len(links), links)
	}
	link := links[0]

	rest := strings.TrimPrefix(link, "ss://")
	at := strings.Index(rest, "@")
	if at < 0 {
		t.Fatalf("link %q carries no userinfo", link)
	}
	userInfo := rest[:at]

	// SIP002: base64url, no padding. Standard base64's '+', '/' and '=' are
	// what several clients reject outright.
	if strings.ContainsAny(userInfo, "+/=") {
		t.Errorf("userinfo %q is standard base64, want base64url without padding", userInfo)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(userInfo)
	if err != nil {
		t.Fatalf("userinfo %q does not decode as base64url: %v", userInfo, err)
	}
	if want := "aes-128-gcm:pa+ss/word=="; string(decoded) != want {
		t.Errorf("userinfo decodes to %q, want %q", decoded, want)
	}
	if !strings.HasSuffix(link, "#alice%20in") {
		t.Errorf("link = %q, want the remark escaped as %q", link, "#alice%20in")
	}
}

// out_json is filled on save, but a row restored from an old backup or written
// by a migration can reach here in any of these shapes. Each one used to cost
// the whole protocol its links: an absent key panicked on the bare assertion,
// and an empty or unparseable value returned an empty slice.
func TestHysteriaLinksSurviveBadOutJson(t *testing.T) {
	addrs := []map[string]interface{}{{
		"server":      "example.com",
		"server_port": float64(443),
		"remark":      "hy-in",
	}}

	shapes := []struct {
		name    string
		inbound map[string]interface{}
	}{
		{"key absent", map[string]interface{}{}},
		{"empty raw", map[string]interface{}{"out_json": json.RawMessage("")}},
		{"json null", map[string]interface{}{"out_json": json.RawMessage("null")}},
		{"unparseable", map[string]interface{}{"out_json": json.RawMessage("not json")}},
		{"no server_ports", map[string]interface{}{"out_json": json.RawMessage(`{"quic":true}`)}},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			for _, tt := range []struct {
				name  string
				links []string
			}{
				{"hysteria", hysteriaLink(map[string]interface{}{"auth_str": "a"}, shape.inbound, addrs)},
				{"hysteria2", hysteria2Link(map[string]interface{}{"password": "p"}, shape.inbound, addrs)},
			} {
				if len(tt.links) != 1 {
					t.Errorf("%s: got %d links %v, want 1", tt.name, len(tt.links), tt.links)
					continue
				}
				if strings.Contains(tt.links[0], "mport=") {
					t.Errorf("%s link = %q, want no mport when out_json carries no ports", tt.name, tt.links[0])
				}
			}
		})
	}
}

// The generated links are parsed back by the panel itself: genNodeReplicaLinks
// stores a node's link as an "external" entry and GetExternalOutbounds decodes
// it for the JSON and Clash subscriptions, discarding it without a word when
// the parse fails. Switching the ss userinfo to SIP002's base64url broke that
// round trip, and nothing caught it.
func TestLinkGeneratorRoundTripsThroughGetOutbound(t *testing.T) {
	tests := []struct {
		inboundType string
		options     string
		config      string
	}{
		// Lengths chosen so the base64 payload is not a multiple of 3: that is
		// the only case the old standard-base64 decoder happened to accept.
		{"shadowsocks", `{"listen_port":8388,"method":"aes-128-gcm"}`,
			`{"shadowsocks":{"password":"hunter2"}}`},
		{"shadowsocks", `{"listen_port":8388,"method":"aes-256-gcm"}`,
			`{"shadowsocks":{"password":"pass"}}`},
		{"trojan", `{"listen_port":443}`, `{"trojan":{"password":"pw"}}`},
		{"vless", `{"listen_port":443}`, `{"vless":{"uuid":"uuid-1"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.inboundType+" "+tt.options, func(t *testing.T) {
			links := LinkGenerator(
				json.RawMessage(tt.config),
				&model.Inbound{
					Type:    tt.inboundType,
					Tag:     "in",
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(tt.options),
				},
				"example.com", "")
			if len(links) == 0 {
				t.Fatalf("no links generated")
			}
			for _, link := range links {
				ob, _, err := GetOutbound(link, 0)
				if err != nil {
					t.Errorf("the panel cannot parse its own link %q: %v", link, err)
					continue
				}
				if ob == nil {
					t.Errorf("link %q parsed to nothing", link)
				}
			}
		})
	}
}

// prepareTls reads a TLS row's two halves. A row whose server half carries
// reality while its client half does not -- hand-written, or migrated from an
// older schema -- used to panic on a bare assertion here.
func TestPrepareTlsToleratesLopsidedReality(t *testing.T) {
	const server = `{"enabled":true,"server_name":"e.com","reality":{"enabled":true,"short_id":["ab"]}}`

	t.Run("client half missing", func(t *testing.T) {
		got := prepareTls(&model.Tls{
			Server: json.RawMessage(server),
			Client: json.RawMessage(`{"enabled":true}`),
		})
		// Repaired, not dropped: a link that says plain TLS against a reality
		// listener fails with an unexplained handshake error, while one that
		// says reality is refused on import.
		reality, ok := got["reality"].(map[string]interface{})
		if !ok {
			t.Fatalf("reality must survive, got %v", got)
		}
		if reality["enabled"] != true {
			t.Errorf("reality.enabled = %v, want true", reality["enabled"])
		}
		if reality["short_id"] != "ab" {
			t.Errorf("short_id = %v, want the one the server offers", reality["short_id"])
		}
	})

	t.Run("server half is not an object", func(t *testing.T) {
		got := prepareTls(&model.Tls{
			Server: json.RawMessage(`{"enabled":true,"reality":"yes"}`),
			Client: json.RawMessage(`{"enabled":true}`),
		})
		if got == nil {
			t.Fatalf("a malformed reality must not lose the whole config")
		}
		if _, ok := got["reality"]; ok {
			t.Errorf("a non-object reality must be skipped, got %v", got)
		}
	})
}

// sing-box writes a single port as a number and a range as a string; the old
// code asserted every entry was a string.
//
// The separator is the other half: sing-box stores "20000:30000", every
// consumer of the rendered value wants "20000-30000". linkToJson's hy2 is the
// proof inside this package -- it reads mport back through
// strings.ReplaceAll(..., "-", ":"), so a link written with colons does not
// round-trip through the panel's own decoder, let alone a client's.
func TestPortHoppingParamAcceptsNumbers(t *testing.T) {
	tests := []struct {
		name    string
		outJson string
		want    string
	}{
		{"strings", `{"server_ports":["20000:30000","40000:50000"]}`, "20000-30000,40000-50000"},
		{"numbers", `{"server_ports":[443,8443]}`, "443,8443"},
		{"mixed", `{"server_ports":[443,"20000:30000"]}`, "443,20000-30000"},
		{"absent", `{}`, ""},
		{"unparseable", `not json`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := portHoppingParam(map[string]interface{}{
				"out_json": json.RawMessage(tt.outJson),
			})
			if got != tt.want {
				t.Errorf("portHoppingParam = %q, want %q", got, tt.want)
			}
		})
	}
}

// The renderer both consumers share: the "mport" link param above and mihomo's
// `ports`. It lived in sub/ as a second copy, which is how the two came to
// disagree about the separator for the same stored row.
func TestPortHoppingRangesAcceptsNumbers(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"strings", []interface{}{"20000:30000", "40000:50000"}, "20000-30000,40000-50000"},
		{"numbers", []interface{}{float64(443), float64(8443)}, "443,8443"},
		{"mixed", []interface{}{float64(443), "20000:30000"}, "443,20000-30000"},
		{"empty", []interface{}{}, ""},
		{"absent", nil, ""},
		{"not a list", "20000:30000", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PortHoppingRanges(tt.in); got != tt.want {
				t.Errorf("PortHoppingRanges = %q, want %q", got, tt.want)
			}
		})
	}
}

// The panel is one of the clients that reads its own links back: adopting a
// node stores the generated link and GetOutbound decodes it. hy2 reads mport
// through ReplaceAll("-", ":"), so only a link written with dashes comes back
// as the server_ports the row started with.
func TestHysteria2LinkPortHoppingRoundTrips(t *testing.T) {
	links := hysteria2Link(
		map[string]interface{}{"password": "p4ss"},
		map[string]interface{}{
			"out_json": json.RawMessage(`{"server_ports":[443,"20000:30000"]}`),
		},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "hy2-in",
		}},
	)
	if len(links) != 1 {
		t.Fatalf("got %d links %v, want 1", len(links), links)
	}
	if !strings.Contains(links[0], "mport=443,20000-30000") {
		t.Errorf("link = %q, want mport rendered with a dash", links[0])
	}

	out, _, err := GetOutbound(links[0], 0)
	if err != nil {
		t.Fatalf("the generated link does not decode: %q: %v", links[0], err)
	}
	got, _ := (*out)["server_ports"].([]string)
	want := []string{"443", "20000:30000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("server_ports = %#v, want %#v (link %q)", got, want, links[0])
	}
}

// The vmess JSON has no serviceName field, so a grpc service name has to travel
// in "path". Dropping it pointed every grpc vmess link at the default service.
func TestVmessGrpcLinkCarriesServiceName(t *testing.T) {
	links := vmessLink(
		map[string]interface{}{"uuid": "uuid-1"},
		map[string]interface{}{
			"transport": map[string]interface{}{"type": "grpc", "service_name": "TunService"},
		},
		[]map[string]interface{}{{
			"server":      "example.com",
			"server_port": float64(443),
			"remark":      "grpc-in",
		}},
	)

	if len(links) != 1 {
		t.Fatalf("got %d links %v, want 1", len(links), links)
	}
	raw, err := B64StrToByte(strings.TrimPrefix(links[0], "vmess://"))
	if err != nil {
		t.Fatalf("decoding %q: %v", links[0], err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshalling %q: %v", raw, err)
	}
	if got := obj["path"]; got != "TunService" {
		t.Errorf("path = %v, want %q", got, "TunService")
	}
}

// Every one of these shapes is legal JSON that the panel's own forms do not
// produce but a hand-written config, an import or an older schema can leave
// behind. Each used to panic on a bare type assertion, inside the subscription
// handler where it costs every client on the inbound.
func TestLinkBuildersTolerateMalformedOptions(t *testing.T) {
	tests := []struct {
		name string
		run  func()
	}{
		{"tls without enabled", func() {
			vlessLink(
				map[string]interface{}{"uuid": "u"},
				map[string]interface{}{},
				[]map[string]interface{}{{"server": "e.com", "server_port": float64(443), "remark": "r",
					"tls": map[string]interface{}{"server_name": "e.com"}}},
			)
		}},
		{"reality without enabled", func() {
			var params []LinkParam
			getTlsParams(&params, map[string]interface{}{
				"enabled":  true,
				"reality":  map[string]interface{}{"public_key": "pk"},
				"disabled": nil,
			}, "vless")
		}},
		{"alpn holding a number", func() {
			var params []LinkParam
			getTlsParams(&params, map[string]interface{}{
				"enabled": true,
				"alpn":    []interface{}{"h2", float64(3)},
			}, "trojan")
		}},
		{"http transport host holding a number", func() {
			getTransportParams(map[string]interface{}{
				"type": "http",
				"host": []interface{}{"a.example.com", float64(1)},
			})
		}},
		{"address rows missing server and remark", func() {
			trojanLink(
				map[string]interface{}{"password": "p"},
				map[string]interface{}{},
				[]map[string]interface{}{{"server_port": float64(443)}},
			)
		}},
		{"vmess tls without enabled", func() {
			populateVmessTlsParams(map[string]interface{}{},
				map[string]interface{}{"server_name": "e.com"})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			tt.run()
		})
	}
}
