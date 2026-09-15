package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
	"github.com/sagernet/sing-box/option"
)

// static_key mode takes what `openvpn --genkey secret` writes -- 256 random
// bytes as hex between OpenVPN's own markers -- rather than a PEM, and sing-box
// parses it as those exact lines. Both ends of a tunnel carry the same one.
func TestGenerateOpenVPNStaticKey(t *testing.T) {
	var server ServerService
	lines := server.GenKeypair("openvpn", "")

	if len(lines) < 3 {
		t.Fatalf("got %d lines: %v", len(lines), lines)
	}
	if lines[len(lines)-1] != "-----END OpenVPN Static key V1-----" {
		t.Errorf("last line = %q, want the END marker", lines[len(lines)-1])
	}

	begin := -1
	for i, l := range lines {
		if l == "-----BEGIN OpenVPN Static key V1-----" {
			begin = i
			break
		}
	}
	if begin < 0 {
		t.Fatalf("no BEGIN marker in %v", lines)
	}

	body := lines[begin+1 : len(lines)-1]
	// 256 bytes of hex is 512 characters, and openvpn writes 32 to a line.
	if len(body) != 16 {
		t.Errorf("%d body lines, want 16", len(body))
	}
	for i, l := range body {
		if len(l) != 32 {
			t.Errorf("body line %d is %d characters, want 32", i, len(l))
		}
	}
	raw, err := hex.DecodeString(strings.Join(body, ""))
	if err != nil {
		t.Fatalf("the body is not hex: %v", err)
	}
	if len(raw) != 256 {
		t.Errorf("the key is %d bytes, want 256", len(raw))
	}
}

// The panel stores the key inline rather than as a path, so this builds an
// endpoint the way the panel would and asks sing-box to construct it.
//
// It covers the shape and nothing more, deliberately stated: sing-box does not
// read the key material at construction -- checked, a list of four nonsense
// words builds just as happily -- so what this pins is that an inline
// static_key is a thing the endpoint accepts at all. The format itself is the
// test above, against what `openvpn --genkey secret` writes.
func TestGeneratedOpenVPNStaticKeyBuildsAnEndpoint(t *testing.T) {
	// Building a box logs through the s-ui logger, which panics uninitialised.
	logger.InitLogger(logging.CRITICAL)

	var server ServerService
	key := server.GenKeypair("openvpn", "")

	raw, err := json.Marshal(map[string]any{
		"log": map[string]any{"level": "error"},
		"endpoints": []any{map[string]any{
			"type": "openvpn-client", "tag": "ovpn-ep",
			"server": "vpn.example.com", "server_port": 1194,
			"mode": "static_key", "network": "udp",
			"address": []string{"10.8.0.2/24"}, "peer_address": "10.8.0.1",
			"cipher": "AES-256-CBC",
			// The panel stores it as the file's lines, which is what the
			// generator returns.
			"static_key": key,
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ctx := core.Context(context.Background(), core.InboundRegistry(), core.OutboundRegistry(),
		core.EndpointRegistry(), core.DNSTransportRegistry(), core.ServiceRegistry(),
		core.CertificateProviderRegistry())
	var options option.Options
	if err = options.UnmarshalJSONContext(ctx, raw); err != nil {
		t.Fatalf("sing-box will not parse the generated key: %v", err)
	}
	instance, err := core.NewBox(core.Options{Context: ctx, Options: options})
	if err != nil {
		// openvpn is behind a build tag; a build without it is not a failure
		// of the key.
		if strings.Contains(err.Error(), "is not included in this build") {
			t.Skip(err.Error())
		}
		t.Fatalf("sing-box will not build an endpoint with the generated key: %v", err)
	}
	instance.Close()
}

// Two calls must not produce the same key, or every panel that generated one
// would be sharing it.
func TestGenerateOpenVPNStaticKeyIsRandom(t *testing.T) {
	var server ServerService
	first := strings.Join(server.GenKeypair("openvpn", ""), "\n")
	second := strings.Join(server.GenKeypair("openvpn", ""), "\n")
	if first == second {
		t.Error("two generated keys are identical")
	}
}
