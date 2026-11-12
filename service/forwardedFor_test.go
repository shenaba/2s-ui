package service

import (
	"net/netip"
	"testing"
)

func TestParseXFFTrustedCDN(t *testing.T) {
	prefixes, err := parseXFFTrustedCDN(defaultValueMap[xffTrustedCDNKey])
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != len(cloudflareRanges) {
		t.Fatalf("the default stands for %d ranges, want Cloudflare's %d", len(prefixes), len(cloudflareRanges))
	}
	edge := netip.MustParseAddr("104.16.1.1")
	found := false
	for _, prefix := range prefixes {
		found = found || prefix.Contains(edge)
	}
	if !found {
		t.Fatalf("the default does not cover the Cloudflare edge %s", edge)
	}

	prefixes, err = parseXFFTrustedCDN("Cloudflare, 192.0.2.0/24 2001:db8::/32,198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != len(cloudflareRanges)+3 {
		t.Fatalf("parsed %d ranges, want Cloudflare's and three more", len(prefixes))
	}
	if prefixes, err = parseXFFTrustedCDN(""); err != nil || len(prefixes) != 0 {
		t.Fatalf("an empty value parsed as %v, %v; want no CDN", prefixes, err)
	}

	for _, bad := range []string{"0.0.0.0/0", "10.0.0.0/7", "::/0", "2001::/15", "127.0.0.1", "::1", "not-an-address"} {
		if _, err = parseXFFTrustedCDN(bad); err == nil {
			t.Errorf("%q was accepted as a CDN range", bad)
		}
	}
}
