package sub

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// The Clash extension config is free text the operator saves from the settings
// page. YAML that is not a mapping -- a document holding only comments, a bare
// scalar, a list -- decodes to a nil map, and the merge below writes into it.
//
// The panel's own editor allows it: its guard rejects anything whose typeof is
// not "object", and `typeof null` is "object".
func TestClashSurvivesAnExtensionConfigThatIsNotAMapping(t *testing.T) {
	setupSubDB(t)

	outbounds := []map[string]interface{}{
		{"type": "vless", "tag": "node", "server": "e.com", "server_port": 443},
	}

	for _, basicConfig := range []string{
		"# nothing but a comment\n",
		"null\n",
		"~\n",
		"just a scalar\n",
		"- a\n- b\n",
	} {
		t.Run(strings.TrimSpace(basicConfig), func(t *testing.T) {
			got, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicConfig)
			if err != nil {
				t.Fatalf("ConvertToClashMeta: %v", err)
			}
			// The unusable extension is dropped, not the subscription: a
			// profile with no proxies in it is not a profile.
			if !strings.Contains(got, "node") {
				t.Errorf("the proxy is missing from the profile:\n%s", got)
			}
		})
	}
}

// addClientInfo already tolerates a vmess payload that is not base64 and one
// that is not JSON, returning the link untouched both times. A payload that
// decodes to JSON null is the third shape of the same thing, and the only one
// that panics rather than returning.
func TestAddClientInfoSurvivesAVmessPayloadThatIsNotAnObject(t *testing.T) {
	var svc LinkService

	for _, payload := range []string{"null", "[]", `"a string"`, "42"} {
		t.Run(payload, func(t *testing.T) {
			uri := "vmess://" + base64.StdEncoding.EncodeToString([]byte(payload))
			got := svc.addClientInfo(uri, " | 1.5GB")
			if got != uri {
				t.Errorf("got %q, want the link back untouched", got)
			}
		})
	}
}

// And the link it can read still gets the client info appended, so the above is
// not passing because nothing works.
func TestAddClientInfoStillAnnotatesAWellFormedVmessLink(t *testing.T) {
	var svc LinkService

	payload, err := json.Marshal(map[string]interface{}{"ps": "node", "add": "e.com", "port": "443"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	uri := "vmess://" + base64.StdEncoding.EncodeToString(payload)

	got := svc.addClientInfo(uri, " | 1.5GB")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "vmess://"))
	if err != nil {
		t.Fatalf("the annotated link is not base64: %q: %v", got, err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("the annotated link is not JSON: %q: %v", raw, err)
	}
	if back["ps"] != "node | 1.5GB" {
		t.Errorf("ps = %v, want the client info appended", back["ps"])
	}
}
