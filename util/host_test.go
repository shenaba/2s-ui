package util

import "testing"

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bracketed ipv6", "[2001:db8::1]", "2001:db8::1"},
		{"bare ipv6", "2001:db8::1", "2001:db8::1"},
		{"bracketed loopback", "[::1]", "::1"},
		{"ipv4", "1.2.3.4", "1.2.3.4"},
		{"domain", "example.com", "example.com"},
		{"empty", "", ""},
		// Not an address at all, but the panel never rejected it either --
		// stripping the first and last byte of "[]" or "[" would corrupt it.
		{"empty brackets", "[]", "[]"},
		{"lone bracket", "[", "["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeHost(tt.in); got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHostForURI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare ipv6 gets brackets", "2001:db8::1", "[2001:db8::1]"},
		// Idempotent: an already-bracketed literal must not gain a second pair.
		{"bracketed ipv6 unchanged", "[2001:db8::1]", "[2001:db8::1]"},
		{"loopback", "::1", "[::1]"},
		{"ipv4-mapped", "::ffff:1.2.3.4", "[::ffff:1.2.3.4]"},
		{"link-local with zone", "fe80::1%eth0", "[fe80::1%eth0]"},
		{"ipv4", "1.2.3.4", "1.2.3.4"},
		{"domain", "example.com", "example.com"},
		{"empty", "", ""},
		// Colons alone don't make an IPv6 literal. The address-row server field
		// takes free text, so a pasted "host:port" reaches here; bracketing it
		// would invent a second malformed shape instead of leaving the
		// operator's spelling recognisable.
		{"host with port is left alone", "example.com:443", "example.com:443"},
		{"garbage with colons is left alone", "a:b:c", "a:b:c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HostForURI(tt.in); got != tt.want {
				t.Errorf("HostForURI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
