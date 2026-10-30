package service

import "testing"

func TestParseTrustedProxies(t *testing.T) {
	prefixes, err := parseTrustedProxies("127.0.0.1, 10.0.0.0/8\n2001:db8::/32")
	if err != nil {
		t.Fatalf("parse valid proxies: %v", err)
	}
	want := []string{"127.0.0.1/32", "10.0.0.0/8", "2001:db8::/32"}
	if len(prefixes) != len(want) {
		t.Fatalf("got %d prefixes, want %d", len(prefixes), len(want))
	}
	for i, prefix := range prefixes {
		if got := prefix.String(); got != want[i] {
			t.Errorf("prefix %d = %q, want %q", i, got, want[i])
		}
	}

	if _, err := parseTrustedProxies("127.0.0.1, not-an-address"); err == nil {
		t.Error("invalid proxy entry was accepted")
	}

	// A v4-mapped entry, as a dual-stack socket shows it, is stored as the
	// IPv4 it names: addresses are unmapped before they are matched.
	mapped, err := parseTrustedProxies("::ffff:172.18.0.1/128, ::ffff:10.0.0.0/104, ::ffff:172.18.0.9")
	if err != nil {
		t.Fatalf("parse v4-mapped: %v", err)
	}
	for i, want := range []string{"172.18.0.1/32", "10.0.0.0/8", "172.18.0.9/32"} {
		if got := mapped[i].String(); got != want {
			t.Errorf("v4-mapped prefix %d = %q, want %q", i, got, want)
		}
	}
	if _, err := parseTrustedProxies("::ffff:0.0.0.0/80"); err == nil {
		t.Error("a v4-mapped prefix shorter than 96 bits was accepted")
	}
}
