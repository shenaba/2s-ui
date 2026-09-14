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
// Parsing the whole document, rather than asserting on the type of one field,
// is the point: it is the check the subscriber actually performs.
func TestJsonSubscriptionParsesAsSingBoxConfig(t *testing.T) {
	links := map[string]string{
		// One per protocol whose links the panel generates and reads back --
		// GetOutbound has no case for socks/http/mixed, and naive is not a
		// sing-box outbound type.
		"vmess":       `{"vmess":{"uuid":"11111111-1111-1111-1111-111111111111","alterId":0}}`,
		"vless":       `{"vless":{"uuid":"11111111-1111-1111-1111-111111111111"}}`,
		"trojan":      `{"trojan":{"password":"p4ss"}}`,
		"hysteria2":   `{"hysteria2":{"password":"p4ss"}}`,
		"anytls":      `{"anytls":{"password":"p4ss"}}`,
		"tuic":        `{"tuic":{"uuid":"11111111-1111-1111-1111-111111111111","password":"p4ss"}}`,
		"shadowsocks": `{"shadowsocks":{"password":"c2hvcnRrZXkxMjM0NTY3OA=="}}`,
	}

	for protocol, config := range links {
		t.Run(protocol, func(t *testing.T) {
			setupSubDB(t)

			// Exactly what refreshNodeLinks folds into client.Links: a link the
			// panel generated itself, stored as an external entry.
			generated := util.LinkGenerator(
				json.RawMessage(config),
				&model.Inbound{
					Type: protocol, Tag: protocol + "-node",
					Addrs:   json.RawMessage("null"),
					Options: json.RawMessage(`{"listen_port":443,"method":"aes-128-gcm"}`),
				},
				"node.example.com", "alice")
			if len(generated) == 0 {
				t.Fatalf("no link generated for %s", protocol)
			}

			client := &model.Client{
				Enable:   true,
				Name:     "sub-" + protocol,
				Config:   json.RawMessage(config),
				Inbounds: json.RawMessage(`[]`),
				Links: json.RawMessage(fmt.Sprintf(
					`[{"remark":"[node] %s","type":"external","uri":%q}]`, protocol, generated[0])),
			}
			if err := database.GetDB().Create(client).Error; err != nil {
				t.Fatalf("seed client: %v", err)
			}

			raw, _, err := (&JsonService{}).GetJson("sub-"+protocol, "json")
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
				t.Errorf("sing-box refuses the whole subscription: %v\n%s", err, *raw)
			}
		})
	}
}
