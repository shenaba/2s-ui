package api

import "testing"

// normalizeHost feeds the server field of every generated link and the
// advertised subscription URI, and now also sanitises the configured web
// domain — which is whatever the operator pasted into the settings form.
func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare domain", "example.com", "example.com"},
		{"domain with port", "example.com:2053", "example.com"},
		{"surrounding whitespace", "  example.com  ", "example.com"},
		{"ipv4", "10.0.0.1", "10.0.0.1"},
		{"ipv4 with port", "10.0.0.1:2053", "10.0.0.1"},
		{"bracketed ipv6 with port", "[2001:db8::1]:2053", "[2001:db8::1]"},
		// Both used to come back empty: SplitHostPort errors without a port and
		// its zero return was assigned unconditionally.
		{"bare ipv6 gets bracketed", "2001:db8::1", "[2001:db8::1]"},
		{"bracketed ipv6 without a port stays bracketed once", "[2001:db8::1]", "[2001:db8::1]"},
		// Regression guard: SplitHostPort splits "https://example.com" at the
		// scheme colon and returns "https", which used to be baked into every
		// link as the server name. Empty makes the caller fall back to the
		// request Host.
		{"pasted https url", "https://example.com", ""},
		{"pasted http url with path", "http://example.com/app/", ""},
		{"bare domain with a trailing slash", "example.com/", ""},
		{"backslash", `example.com\app`, ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeHost(tc.in); got != tc.want {
				t.Errorf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
