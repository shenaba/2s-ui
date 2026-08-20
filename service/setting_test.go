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
}
