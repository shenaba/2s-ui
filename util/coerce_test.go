package util

import (
	"reflect"
	"testing"
)

// Not every map these readers see has been through a json.Unmarshal: GetOutbound
// assembles its outbounds in Go, and getTls fills alpn with a strings.Split.
// Those maps go straight to the Clash converter, so a []interface{}-only reader
// silently dropped the alpn of every external and node-replica link.
func TestAsStringList(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"from json", []interface{}{"h2", "http/1.1"}, []string{"h2", "http/1.1"}},
		{"built in go", []string{"h2", "http/1.1"}, []string{"h2", "http/1.1"}},
		{"non-string entries are skipped", []interface{}{"h2", 3, nil}, []string{"h2"}},
		{"empty", []interface{}{}, []string{}},
		{"absent", nil, nil},
		// A bare scalar is a list of one, not "not a list": every caller reads a
		// sing-box Listable[string], which accepts a scalar and writes one back
		// out for a single-element list. This case asserted nil, which is what
		// dropped a single alpn, a single reality short_id and a one-line ECH
		// config -- the shapes sing-box itself produces.
		{"a scalar is a list of one", "h2", []string{"h2"}},
		{"an empty scalar is an empty field", "", nil},
		{"not a list at all", 42, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AsStringList(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AsStringList(%#v) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// The number half of the same split: hy runs the bandwidths through
// strconv.Atoi and vmess stores alter_id as an int, while the same fields out
// of a stored row are float64. The bool matters as much as the value -- it is
// how the Clash converter tells an absent bandwidth from a zero one.
func TestAsInt64(t *testing.T) {
	tests := []struct {
		name   string
		in     interface{}
		want   int64
		wantOk bool
	}{
		{"from json", float64(100), 100, true},
		{"built in go", 100, 100, true},
		{"int64", int64(100), 100, true},
		{"truncated, not rounded", float64(100.9), 100, true},
		{"absent", nil, 0, false},
		{"a string is not a number", "100", 0, false},
		{"a bool is not a number", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AsInt64(tt.in)
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("AsInt64(%#v) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}
