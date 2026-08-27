package service

import (
	"fmt"
	"math"
	"testing"
)

// UsageRatio feeds both the memory threshold alert and the periodic report. Its
// failure mode is quiet: gopsutil returns an empty map when the syscall fails,
// and a zero total divides into a NaN that compares false against every
// threshold -- silently disabling the alert -- and formats as "NaN%" in the
// report.
func TestUsageRatio(t *testing.T) {
	cases := []struct {
		name  string
		in    any
		want  float64
		valid bool
	}{
		{"half used", map[string]interface{}{"current": uint64(512), "total": uint64(1024)}, 50, true},
		{"full", map[string]interface{}{"current": uint64(1024), "total": uint64(1024)}, 100, true},
		{"gopsutil failed", map[string]interface{}{}, 0, false},
		{"zero total", map[string]interface{}{"current": uint64(1), "total": uint64(0)}, 0, false},
		{"missing total", map[string]interface{}{"current": uint64(1)}, 0, false},
		{"wrong numeric type", map[string]interface{}{"current": 512, "total": 1024}, 0, false},
		{"not a map", "nope", 0, false},
		{"nil", nil, 0, false},
	}
	for _, c := range cases {
		got, ok := UsageRatio(c.in)
		if ok != c.valid {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.valid)
			continue
		}
		if ok && got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
		if !ok && math.IsNaN(got) {
			t.Errorf("%s: returned NaN, which would reach the report as \"NaN%%\"", c.name)
		}
	}

	// The report formats with %.0f; make sure a rejected value cannot slip
	// through as a plausible-looking zero percent.
	if _, ok := UsageRatio(map[string]interface{}{}); ok {
		t.Fatal("an empty reading was accepted")
	}
	if s := fmt.Sprintf("%.0f", math.NaN()); s != "NaN" {
		t.Skip("NaN no longer formats as NaN; the guard above is moot")
	}
}
