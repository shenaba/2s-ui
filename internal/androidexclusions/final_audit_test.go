package androidexclusions

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestRejectBrokenStructuredResponses(t *testing.T) {
	for _, raw := range []string{`["com.old", "com.new"`, `{"error": "com.fake" }`, "<!-- error -->\n<html>\ncom.fake\n</html>"} {
		ids, err := Parse([]byte(raw))
		if err == nil {
			t.Errorf("accepted broken response: %q => %v", raw, ids)
		}
	}
}

func TestAllExplicitPackageRulesWin(t *testing.T) {
	raw := `{"inbounds":[{"type":"tun","tag":"keep","exclude_package":["com.route","com.dns","com.server","com.regex","com.manual"]},{"type":"mixed","tag":"other","listen_port":2080}],"rules":[{"type":"logical","mode":"and","rules":[{"package_name":"com.route"}]}],"dns":{"rules":[{"package_name":["com.dns"]}]},"log":{"level":"warn"}}`
	server := `{"route":{"rules":[{"package_name":"com.server"},{"package_name_regex":"^com\\.regex$"}]}}`
	o := Options{Enabled: true, Manual: "com.manual,com.remote", Sources: []string{"https://example.com/list"}}
	c := Cache{o.Sources[0]: {IDs: []string{"com.remote", "com.server"}}}
	out, st, err := Apply(raw, server, o, c)
	if err != nil || st.Total != 2 || st.Removed != 4 || !slices.Equal(excluded(t, out), []string{"com.manual", "com.remote"}) {
		t.Fatal(out, st, err)
	}
	var orig, got map[string]any
	if json.Unmarshal([]byte(raw), &orig) != nil || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatal("bad JSON")
	}
	delete(orig["inbounds"].([]any)[0].(map[string]any), "exclude_package")
	delete(got["inbounds"].([]any)[0].(map[string]any), "exclude_package")
	a, _ := json.Marshal(orig)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatal("unrelated configuration changed")
	}
	o.Enabled = false
	out, st, err = Apply(raw, server, o, c)
	if err != nil || out != raw || st != (Stats{}) {
		t.Fatal("disabled feature changed template")
	}
}

func TestFailedDownloadPreservesSnapshot(t *testing.T) {
	u := "https://example.com/list"
	o := Options{Enabled: true, Sources: []string{u}}
	c := Cache{}
	c.RecordChecked(o, u, []string{"com.old"}, nil, time.Unix(1, 0))
	ids, err := Parse([]byte(`["com.one", "com.two"`))
	c.RecordChecked(o, u, ids, err, time.Unix(2, 0))
	if s := c[u]; !slices.Equal(s.IDs, []string{"com.old"}) || s.OK != 1 || s.Tried != 2 || s.Err == "" {
		t.Fatal(s)
	}
}
