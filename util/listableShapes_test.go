package util

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// reality.short_id is a sing-box Listable[string]: it accepts a bare scalar and
// writes one back out for a single-element list. Asserting only the array shape
// read a single stored id as absent, and a reality link with no short_id is one
// the listener refuses -- with a handshake error that says nothing about why.
//
// Both sides of the panel derive it: prepareTls for a locally generated link,
// addTls for the out_json snapshot a node hands its own subscribers.
func TestRealityShortIdSurvivesBothStoredShapes(t *testing.T) {
	const wantSid = "a1b2"

	for _, stored := range []string{`["a1b2"]`, `"a1b2"`} {
		server := `{"enabled":true,"reality":{"enabled":true,"handshake":{"server":"www.cloudflare.com"},` +
			`"short_id":` + stored + `}}`

		t.Run("prepareTls "+stored, func(t *testing.T) {
			got := prepareTls(&model.Tls{
				Server: json.RawMessage(server),
				Client: json.RawMessage(`{"enabled":true,"reality":{"enabled":true,"public_key":"pk"}}`),
			})
			reality, ok := got["reality"].(map[string]interface{})
			if !ok {
				t.Fatalf("reality missing from %v", got)
			}
			if reality["short_id"] != wantSid {
				t.Errorf("short_id = %#v, want %q", reality["short_id"], wantSid)
			}
		})

		t.Run("addTls "+stored, func(t *testing.T) {
			out := map[string]interface{}{}
			addTls(&out, &model.Tls{
				Server: json.RawMessage(server),
				Client: json.RawMessage(`{"enabled":true,"reality":{"enabled":true,"public_key":"pk"}}`),
			})
			tls, ok := out["tls"].(map[string]interface{})
			if !ok {
				t.Fatalf("tls missing from %v", out)
			}
			reality, ok := tls["reality"].(map[string]interface{})
			if !ok {
				t.Fatalf("reality missing from %v", tls)
			}
			if reality["short_id"] != wantSid {
				t.Errorf("short_id = %#v, want %q", reality["short_id"], wantSid)
			}
		})
	}
}

// alpn is the same Listable, and it reaches the generated link rather than the
// outbound map -- so a single stored value used to vanish from the query string.
func TestAlpnSurvivesBothStoredShapes(t *testing.T) {
	for _, stored := range []string{`["h3"]`, `"h3"`} {
		t.Run(stored, func(t *testing.T) {
			got := prepareTls(&model.Tls{
				Server: json.RawMessage(`{"enabled":true}`),
				Client: json.RawMessage(`{"enabled":true,"alpn":` + stored + `}`),
			})
			if alpn := AsStringList(got["alpn"]); len(alpn) != 1 || alpn[0] != "h3" {
				t.Errorf("alpn = %#v, want one entry h3", got["alpn"])
			}
		})
	}
}
