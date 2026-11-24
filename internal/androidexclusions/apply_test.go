package androidexclusions

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

const tpl = `{"inbounds":[{"type":"tun","exclude_package":["a.b"]}],` +
	`"rules":[{"package_name":["c.d"],"action":"route","outbound":"proxy"}]}`

func excluded(t *testing.T, raw string) []string {
	t.Helper()
	var x struct {
		Inbounds []struct {
			Exclude []string `json:"exclude_package"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(raw), &x); err != nil || len(x.Inbounds) == 0 {
		t.Fatalf("bad result %q: %v", raw, err)
	}
	return x.Inbounds[0].Exclude
}

func TestApplyMergesAndRemovesVPNApps(t *testing.T) {
	o := Options{Enabled: true, Sources: []string{"https://x.example/l.txt"}}
	c := Cache{"https://x.example/l.txt": {IDs: []string{"a.b", "c.d", "e.f"}}}
	out, st, err := Apply(tpl, "", o, c)
	if err != nil {
		t.Fatal(err)
	}
	if got := excluded(t, out); !slices.Equal(got, []string{"a.b", "e.f"}) {
		t.Fatalf("exclude_package = %v", got)
	}
	if st.Total != 2 || st.Removed != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestApplyDisabledReturnsTemplateUntouched(t *testing.T) {
	out, st, err := Apply(tpl, "", Options{}, Cache{})
	if err != nil || out != tpl || st != (Stats{}) {
		t.Fatalf("out=%q st=%+v err=%v", out, st, err)
	}
}

func TestApplyIncludePackageWarning(t *testing.T) {
	raw := `{"inbounds":[{"type":"tun","include_package":["x.y"],"exclude_package":["a.b"]}]}`
	out, st, err := Apply(raw, "", Options{Enabled: true}, Cache{})
	if err != nil || st.Warning != "include_package" {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if got := excluded(t, out); len(got) != 0 {
		t.Fatalf("exclude_package must be dropped, got %v", got)
	}
}

func TestApplyBadTemplateErrorsAndStripIsSafe(t *testing.T) {
	if _, _, err := Apply(`{"inbounds":[{"type":"tun","exclude_package":5}]}`, "", Options{Enabled: true}, Cache{}); err == nil {
		t.Fatal("want error for invalid exclude_package")
	}
	out, err := Strip(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if got := excluded(t, out); len(got) != 0 {
		t.Fatalf("Strip left %v", got)
	}
}

func TestRecordKeepsLastGoodList(t *testing.T) {
	c := Cache{}
	u := "https://x.example/l.txt"
	t1, t2 := time.Unix(1000, 0), time.Unix(2000, 0)
	c.Record(u, []string{"b.b", "a.a"}, nil, t1)
	c.Record(u, nil, errors.New("boom"), t2)
	s := c[u]
	if !slices.Equal(s.IDs, []string{"a.a", "b.b"}) || s.OK != 1000 || s.Tried != 2000 || s.Err == "" {
		t.Fatalf("%+v", s)
	}
	c.Record(u, []string{"c.c"}, nil, time.Unix(3000, 0))
	if s = c[u]; s.Err != "" || s.OK != 3000 || !slices.Equal(s.IDs, []string{"c.c"}) {
		t.Fatalf("%+v", s)
	}
}

func TestDueUsesLastAttempt(t *testing.T) {
	now := time.Unix(10*86400, 0)
	on := Options{Enabled: true}
	if !Due(on, Source{}, now, false) {
		t.Fatal("never tried must be due")
	}
	if Due(on, Source{Tried: 1}, now, false) {
		t.Fatal("interval 0 must not refresh by itself")
	}
	if !Due(on, Source{Tried: 1}, now, true) {
		t.Fatal("force must refresh")
	}
	week := Options{Enabled: true, Interval: 7}
	if Due(week, Source{Tried: now.Unix() - 6*86400}, now, false) || !Due(week, Source{Tried: now.Unix() - 7*86400}, now, false) {
		t.Fatal("interval boundary")
	}
	if Due(Options{Interval: 1}, Source{}, now, true) {
		t.Fatal("disabled must never be due")
	}
}

func TestPruneAndMerge(t *testing.T) {
	o := Options{Enabled: true, Manual: "m.m, a.a", Sources: []string{"https://x.example/1"}}
	c := Cache{"https://x.example/1": {IDs: []string{"a.a", "z.z"}}, "https://gone.example/2": {IDs: []string{"q.q"}}}
	if !c.Prune(o) || len(c) != 1 {
		t.Fatalf("prune: %v", c)
	}
	if got := Merge(o, c); !slices.Equal(got, []string{"a.a", "m.m", "z.z"}) {
		t.Fatalf("merge: %v", got)
	}
}

func TestDecodeOptionsRejectsBadSources(t *testing.T) {
	if _, err := DecodeOptions(`{"enabled":true,"sources":["http://x.example/a"]}`); err == nil {
		t.Fatal("plain http must be rejected")
	}
	if _, err := DecodeOptions(`{"enabled":true,"sources":["https://127.0.0.1/a"]}`); err == nil {
		t.Fatal("private address must be rejected")
	}
	o, err := DecodeOptions(`{"enabled":true,"sources":[" https://x.example/a ","https://x.example/a",""],"interval":3}`)
	if err != nil || len(o.Sources) != 1 || o.Interval != 3 {
		t.Fatalf("%+v %v", o, err)
	}
}
