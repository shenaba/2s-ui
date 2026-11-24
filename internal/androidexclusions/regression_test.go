package androidexclusions

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStrictJSON(t *testing.T) {
	for _, raw := range []string{`null`, `{"enabled":true}{}`, `{"enabled":true} garbage`, `[]`} {
		if _, e := DecodeOptions(raw); e == nil {
			t.Errorf("accepted options %q", raw)
		}
		if _, _, e := Apply(raw, "", Options{Enabled: true}, nil); e == nil {
			t.Errorf("accepted template %q", raw)
		}
	}
}

func TestNormalizeExistingAndUniqueStats(t *testing.T) {
	raw := `{"inbounds":[{"type":"tun","exclude_package":["a.b","a.b","bad id","c.d"]},{"type":"tun","exclude_package":["c.d","e.f"]}],"rules":[{"package_name":"c.d"}]}`
	out, st, e := Apply(raw, "", Options{Enabled: true, Manual: "a.b"}, nil)
	if e != nil || st.Total != 2 || st.Removed != 1 {
		t.Fatal(out, st, e)
	}
	var cfg struct {
		Inbounds []struct {
			Exclude []string `json:"exclude_package"`
		}
	}
	if e = json.Unmarshal([]byte(out), &cfg); e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(cfg.Inbounds[0].Exclude, []string{"a.b"}) || !slices.Equal(cfg.Inbounds[1].Exclude, []string{"a.b", "e.f"}) {
		t.Fatal(out)
	}
	// A normalization-only change must not return the unchecked original.
	out, _, e = Apply(`{"inbounds":[{"type":"tun","exclude_package":["a.b","a.b","bad id"]}]}`, "", Options{Enabled: true}, nil)
	if e != nil || !slices.Equal(excluded(t, out), []string{"a.b"}) {
		t.Fatal(out, e)
	}
}

func TestDefaultTUNAndExplicitAbsence(t *testing.T) {
	o := Options{Enabled: true, Manual: "a.b"}
	for _, raw := range []string{"", `{}`, `{"dns":{"rules":[{"package_name":"c.d"}]}}`} {
		out, st, e := Apply(raw, "", o, nil)
		if e != nil || st.Warning != "" || st.Total != 1 || !slices.Equal(excluded(t, out), []string{"a.b"}) {
			t.Fatal(out, st, e)
		}
		var got, stock map[string]any
		_ = json.Unmarshal([]byte(out), &got)
		_ = json.Unmarshal([]byte(DefaultTemplate), &stock)
		ins := got["inbounds"].([]any)
		orig := stock["inbounds"].([]any)
		if len(ins) != len(orig) {
			t.Fatal(out)
		}
		delete(ins[0].(map[string]any), "exclude_package")
		x, _ := json.Marshal(ins)
		y, _ := json.Marshal(orig)
		if string(x) != string(y) {
			t.Fatal("stock inbound changed")
		}
	}
	raw := `{"inbounds":[{"type":"mixed"}]}`
	out, st, e := Apply(raw, "", o, nil)
	if e != nil || st.Warning != "no_tun" || out != raw {
		t.Fatal(out, st, e)
	}
}

func TestLegacyCacheMigration(t *testing.T) {
	u := "https://example.com/a"
	raw := `{"https://example.com/a":["a.b","c.d"]}`
	c := DecodeCache(raw, `{"sources":{"https://example.com/a":{"checked":10,"attempted":20,"failed":true}}}`)
	if v := c[u]; v.OK != 10 || v.Tried != 20 || v.Err == "" || !slices.Equal(v.IDs, []string{"a.b", "c.d"}) {
		t.Fatal(v)
	}
	b, _ := json.Marshal(c)
	if v := DecodeCache(string(b), "")[u]; v.OK != 10 || v.Tried != 20 || !slices.Equal(v.IDs, c[u].IDs) {
		t.Fatal(v)
	}
}

func testIDs(prefix string, n int) []string {
	v := make([]string, n)
	for i := range v {
		v[i] = fmt.Sprintf("%s.p%d", prefix, i)
	}
	return v
}

func TestCombinedLimitKeepsLastGood(t *testing.T) {
	u, v := "https://example.com/a", "https://example.com/b"
	o := Options{Enabled: true, Sources: []string{u, v}}
	c := Cache{u: {IDs: testIDs("a", MaxPackages-1)}, v: {IDs: []string{"b.old"}, OK: 10}}
	c.RecordChecked(o, v, []string{"b.new", "b.other"}, nil, time.Unix(20, 0))
	if s := c[v]; s.OK != 10 || s.Tried != 20 || s.Err == "" || !slices.Equal(s.IDs, []string{"b.old"}) {
		t.Fatal(s)
	}
	c[v] = Source{IDs: []string{"b.new", "b.other"}}
	if _, _, e := Apply(tpl, "", o, c); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	o.Sources = []string{u}
	delete(c, v)
	raw := `{"inbounds":[{"type":"tun","exclude_package":["b.one","b.two"]}]}`
	if _, _, e := Apply(raw, "", o, c); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
}

func TestCacheBytesLimit(t *testing.T) {
	u, v := "https://example.com/a", "https://example.com/b"
	o := Options{Enabled: true, Sources: []string{u, v}}
	ids := testIDs(strings.Repeat("a", 240), 10000)
	c := Cache{u: {IDs: ids}, v: {IDs: ids}}
	if e := CheckLimits(o, c); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
}

func TestRecordInvalidDoesNotErase(t *testing.T) {
	c := Cache{"u": {IDs: []string{"a.b"}, OK: 10}}
	c.Record("u", []string{"bad id"}, nil, time.Unix(20, 0))
	if s := c["u"]; s.OK != 10 || s.Err == "" || !slices.Equal(s.IDs, []string{"a.b"}) {
		t.Fatal(s)
	}
}
