package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/util"

	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

// deprecated.Manager is required in the parse context; nothing here asserts on
// the notes, so they are dropped.
type discardDeprecatedNotes struct{}

func (discardDeprecatedNotes) ReportDeprecated(deprecated.Note) {}

// The JSON subscription has to survive sing-box's own parser, and one bad
// outbound does not degrade to a missing node -- option.Options.UnmarshalJSON
// fails the whole document, so the subscriber gets nothing at all.
//
// That is what a vmess node replica used to do. A vmess link carries its port
// as a string by convention, the decoder handed that string straight on as
// server_port, and sing-box declares the field uint16: every client that
// referenced a vmess replica got a subscription it could not import, with the
// error naming an outbound index rather than the node. The Clash subscription
// stayed fine, because mihomo decodes weakly typed -- which is why this never
// looked like a link-decoding bug.
//
// Building the box, not just parsing the document, is the point. Parsing is the
// weaker half and stopping there hides a whole class: a hysteria2 outbound
// whose server_ports holds a bare "443" unmarshals happily and then fails
// NewBox with `bad port range: 443`, so a parse-only check calls a subscription
// healthy that no client can start. Asserting on the type of one field is
// weaker still -- these are the two checks the subscriber actually performs,
// in the order they perform them.
func TestJsonSubscriptionParsesAsSingBoxConfig(t *testing.T) {
	// An ordered slice rather than a map, the same way the link tests do it:
	// map iteration is randomised, so a failing build reports its subtests in a
	// different order every run.
	tests := []struct {
		name     string
		protocol string
		config   string
		options  string // "" = the shared default below
		outJson  string
	}{
		// One per protocol whose links the panel generates and reads back --
		// GetOutbound has no case for socks/http/mixed, and naive is not a
		// sing-box outbound type.
		{"vmess", "vmess", `{"vmess":{"uuid":"11111111-1111-1111-1111-111111111111","alterId":0}}`, "", ""},
		{"vless", "vless", `{"vless":{"uuid":"11111111-1111-1111-1111-111111111111"}}`, "", ""},
		{"trojan", "trojan", `{"trojan":{"password":"p4ss"}}`, "", ""},
		{"hysteria2", "hysteria2", `{"hysteria2":{"password":"p4ss"}}`, "", ""},
		{"anytls", "anytls", `{"anytls":{"password":"p4ss"}}`, "", ""},
		{"tuic", "tuic", `{"tuic":{"uuid":"11111111-1111-1111-1111-111111111111","password":"p4ss"}}`, "", ""},
		{"shadowsocks", "shadowsocks", `{"shadowsocks":{"password":"c2hvcnRrZXkxMjM0NTY3OA=="}}`, "", ""},
		{
			// hysteria v1 is the one protocol carrying a requirement only
			// NewBox enforces: sing-quic refuses a client with no bandwidth.
			// The listener requires it too, so a hysteria inbound that runs at
			// all has the values for hysteriaOut to copy -- which is exactly
			// why the generated link has to keep carrying them.
			"hysteria", "hysteria",
			`{"hysteria":{"auth_str":"a"}}`,
			`{"listen_port":443,"up_mbps":100,"down_mbps":200}`,
			"",
		},
		{
			// The port-hopping round trip, which is where parse-only passed and
			// NewBox did not: a single port has to leave as the bare "443" the
			// link format uses and come back as the "443:443" sing-box starts
			// on. A stored row spelling it bare exercises the same path.
			"hysteria2 port hopping", "hysteria2",
			`{"hysteria2":{"password":"p4ss"}}`,
			"",
			`{"server_ports":["443","20000:30000"]}`,
		},
	}

	for _, tt := range tests {
		protocol, config := tt.protocol, tt.config
		options := tt.options
		if options == "" {
			options = `{"listen_port":443,"method":"aes-128-gcm"}`
		}
		t.Run(tt.name, func(t *testing.T) {
			setupSubDB(t)

			// Exactly what refreshNodeLinks folds into client.Links: a link the
			// panel generated itself, stored as an external entry.
			generated := util.LinkGenerator(
				json.RawMessage(config),
				&model.Inbound{
					Type: protocol, Tag: protocol + "-node",
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(options),
					OutJson: json.RawMessage(tt.outJson),
				},
				"node.example.com", "alice")
			if len(generated) == 0 {
				t.Fatalf("no link generated for %s", protocol)
			}

			client := &model.Client{
				Enable:   true,
				Name:     "sub-" + tt.name,
				Config:   json.RawMessage(config),
				Inbounds: json.RawMessage(`[]`),
				Links: json.RawMessage(fmt.Sprintf(
					`[{"remark":"[node] %s","type":"external","uri":%q}]`, protocol, generated[0])),
			}
			if err := database.GetDB().Create(client).Error; err != nil {
				t.Fatalf("seed client: %v", err)
			}

			raw, _, err := (&JsonService{}).GetJson("sub-"+tt.name, "json")
			if err != nil {
				t.Fatalf("GetJson: %v", err)
			}
			// The replica has to have survived as an outbound, or the parse
			// below would pass by having nothing to check.
			var found bool
			for _, ob := range outboundsOf(t, *raw) {
				if ob["type"] == protocol {
					found = true
				}
			}
			if !found {
				t.Fatalf("the external %s link produced no outbound:\n%s", protocol, *raw)
			}

			ctx := core.Context(context.Background(), core.InboundRegistry(), core.OutboundRegistry(),
				core.EndpointRegistry(), core.DNSTransportRegistry(), core.ServiceRegistry(),
				core.CertificateProviderRegistry())
			ctx = service.ContextWith[deprecated.Manager](ctx, discardDeprecatedNotes{})

			var opts option.Options
			if err := opts.UnmarshalJSONContext(ctx, []byte(*raw)); err != nil {
				t.Fatalf("sing-box cannot parse the subscription: %v\n%s", err, *raw)
			}
			instance, err := core.NewBox(core.Options{Context: ctx, Options: opts})
			if err != nil {
				t.Fatalf("sing-box parses the subscription but cannot start it: %v\n%s", err, *raw)
			}
			instance.Close()
		})
	}
}
