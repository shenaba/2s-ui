package api

import (
	"net/netip"
	"testing"
)

// The generated vhost dials the panel at webListen when that names a concrete
// address (service/acme.go's upstreamAddr), so loopback alone is not the whole
// set of peers the panel's own nginx can arrive from.
func TestGeneratedProxyPeerFollowsWebListen(t *testing.T) {
	cases := []struct {
		name   string
		listen string
		want   string
	}{
		{"empty listen is dialled on loopback", "", ""},
		{"v4 wildcard is dialled on loopback", "0.0.0.0", ""},
		{"v6 wildcard is dialled on loopback", "::", ""},
		{"bracketed v6 wildcard", "[::]", ""},
		{"not an address", "eth0", ""},
		{"concrete v4 bind", "10.0.0.5", "10.0.0.5/32"},
		{"concrete v6 bind", "2001:db8::5", "2001:db8::5/128"},
		{"bracketed v6 bind", "[2001:db8::5]", "2001:db8::5/128"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefix, ok := generatedProxyPeer(tc.listen)
			if tc.want == "" {
				if ok {
					t.Errorf("generatedProxyPeer(%q) = %v, want no extra peer", tc.listen, prefix)
				}
				return
			}
			if !ok {
				t.Fatalf("generatedProxyPeer(%q) returned no peer, want %s", tc.listen, tc.want)
			}
			if got := prefix.String(); got != tc.want {
				t.Errorf("generatedProxyPeer(%q) = %q, want %q", tc.listen, got, tc.want)
			}
		})
	}

	// The property that matters: with the panel bound to a concrete address,
	// the header its own nginx sets is honoured rather than discarded.
	peer, ok := generatedProxyPeer("10.0.0.5")
	if !ok {
		t.Fatal("concrete bind produced no peer")
	}
	trusted := append(append([]netip.Prefix{}, generatedProxyPeers...), peer)
	if got := clientIP("10.0.0.5:5000", "203.0.113.7", trusted); got != "203.0.113.7" {
		t.Errorf("clientIP() = %q, want the forwarded client", got)
	}
}

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
