package api

import (
	"net/netip"
	"testing"
)

func TestClientIPTrustsOnlyConfiguredProxyChain(t *testing.T) {
	custom := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name       string
		remoteAddr string
		forwarded  string
		trusted    []netip.Prefix
		want       string
	}{
		{
			name:       "direct caller cannot forge header",
			remoteAddr: "203.0.113.9:1234",
			forwarded:  "198.51.100.4",
			trusted:    custom,
			want:       "203.0.113.9",
		},
		{
			name:       "generated loopback proxy",
			remoteAddr: "127.0.0.1:1234",
			forwarded:  "192.0.2.1, 203.0.113.9",
			trusted:    generatedProxyPeers,
			want:       "203.0.113.9",
		},
		{
			name:       "trusted proxy chain is stripped from the right",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "198.51.100.4, 10.0.0.3",
			trusted:    custom,
			want:       "198.51.100.4",
		},
		{
			name:       "malformed chain fails closed",
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "198.51.100.4, unknown",
			trusted:    custom,
			want:       "10.0.0.2",
		},
		{
			name:       "trusted peer without header remains the peer",
			remoteAddr: "10.0.0.2:1234",
			trusted:    custom,
			want:       "10.0.0.2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(tc.remoteAddr, tc.forwarded, tc.trusted); got != tc.want {
				t.Errorf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

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
