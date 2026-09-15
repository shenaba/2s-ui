package sub

import (
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
