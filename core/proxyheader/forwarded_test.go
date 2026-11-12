package proxyheader

import (
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// cdnForTest makes prefixes the trusted CDN ranges for one test.
func cdnForTest(t *testing.T, prefixes ...string) {
	t.Helper()
	parsed := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		parsed = append(parsed, netip.MustParsePrefix(prefix))
	}
	SetCDN(parsed)
	t.Cleanup(func() { SetCDN(nil) })
}

func TestForwarded(t *testing.T) {
	// 192.0.2.0/24 stands for a CDN's edges; this host is 198.51.100.1.
	cdnForTest(t, "192.0.2.0/24", "2001:db8:cd::/48")
	hostAddrsForTest(t, "198.51.100.1")
	cases := []struct {
		name         string
		peer         string
		forwarded    []string
		hostForwards bool
		want         string
	}{
		{name: "a client is never believed for itself",
			peer: "203.0.113.9:40000", forwarded: []string{"1.2.3.4"}, want: "203.0.113.9:40000"},
		{name: "no header",
			peer: "192.0.2.10:40000", want: "192.0.2.10:40000"},
		{name: "a CDN edge vouches for the entry it appended",
			peer: "192.0.2.10:40000", forwarded: []string{"203.0.113.9"}, want: "203.0.113.9:40000"},
		// Cloudflare appends to a header the client sent: the first entry is
		// the client's own, which is what sing-box believed.
		{name: "an entry the client wrote ahead of the edge's is not believed",
			peer: "192.0.2.10:40000", forwarded: []string{"1.2.3.4, 203.0.113.9"}, want: "203.0.113.9:40000"},
		{name: "headers sent more than once are one list",
			peer: "192.0.2.10:40000", forwarded: []string{"1.2.3.4", "203.0.113.9"}, want: "203.0.113.9:40000"},
		// A client reaching the edge through a relay whose address is itself in
		// a CDN range: the edge vouches for the relay, and no further.
		{name: "an edge vouches for one entry only",
			peer: "192.0.2.10:40000", forwarded: []string{"1.2.3.4, 192.0.2.77"}, want: "192.0.2.77:40000"},
		{name: "an IPv6 edge",
			peer: "[2001:db8:cd::10]:40000", forwarded: []string{"2001:db8:1::9"}, want: "[2001:db8:1::9]:40000"},
		{name: "an entry with a port",
			peer: "192.0.2.10:40000", forwarded: []string{"[2001:db8:1::9]:443"}, want: "[2001:db8:1::9]:40000"},
		{name: "an entry that does not parse ends the walk",
			peer: "192.0.2.10:40000", forwarded: []string{"1.2.3.4, unknown"}, want: "192.0.2.10:40000"},
		// The core can dial this host for a client; an address of its own is
		// not an edge whatever range it is listed in.
		{name: "this host is never an edge",
			peer: "198.51.100.1:40000", forwarded: []string{"1.2.3.4"}, want: "198.51.100.1:40000"},
		{name: "this host is not believed by an inbound listening beyond loopback",
			peer: "127.0.0.1:40000", forwarded: []string{"203.0.113.9"}, want: "127.0.0.1:40000"},
		{name: "a reverse proxy on this host vouches for its client",
			peer: "127.0.0.1:40000", forwarded: []string{"1.2.3.4, 203.0.113.9"}, hostForwards: true, want: "203.0.113.9:40000"},
		{name: "a reverse proxy on this host behind a CDN",
			peer: "127.0.0.1:40000", forwarded: []string{"1.2.3.4, 203.0.113.9, 192.0.2.10"}, hostForwards: true, want: "203.0.113.9:40000"},
		// A client that has the core dial the reverse proxy itself arrives at
		// it from loopback: what it wrote ahead of that is not believed, and
		// neither is a CDN edge it wrote there.
		{name: "loopback behind this host's reverse proxy ends the walk",
			peer: "[::1]:40000", forwarded: []string{"1.2.3.4, 203.0.113.9, 127.0.0.1"}, hostForwards: true, want: "127.0.0.1:40000"},
		{name: "a forged edge behind loopback is not followed",
			peer: "127.0.0.1:40000", forwarded: []string{"1.2.3.4, 192.0.2.10, 127.0.0.1"}, hostForwards: true, want: "127.0.0.1:40000"},
		{name: "a reverse proxy on this host that sent no entry",
			peer: "127.0.0.1:40000", forwarded: []string{""}, hostForwards: true, want: "127.0.0.1:40000"},
		// With a PROXY header the peer is the client the front named, and
		// believing this host has nothing to do with it.
		{name: "a client a PROXY header named",
			peer: "203.0.113.9:40000", forwarded: []string{"1.2.3.4"}, hostForwards: true, want: "203.0.113.9:40000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Forwarded(logger.NOP(), M.ParseSocksaddr(tc.peer), tc.forwarded, tc.hostForwards)
			if got.String() != tc.want {
				t.Fatalf("Forwarded(%s, %q) = %s, want %s", tc.peer, tc.forwarded, got, tc.want)
			}
		})
	}
}

func TestForwardedBelievesNoCDNUnlessSet(t *testing.T) {
	SetCDN(nil)
	got := Forwarded(logger.NOP(), M.ParseSocksaddr("192.0.2.10:40000"), []string{"203.0.113.9"}, false)
	if got.String() != "192.0.2.10:40000" {
		t.Fatalf("believed %s with no CDN set", got)
	}
}

func TestHostForwardsOnlyOnLoopbackOverAForwardsReader(t *testing.T) {
	loopback := Settings{Port: 10000, loopback: true}
	if got := loopback.Over(plainTransport{}); got.HostForwards() || got.Guarded() {
		t.Fatal("a transport that reads no X-Forwarded-For believes this host")
	}
	if got := loopback.Over(readsForwarded{}); !got.HostForwards() || !got.Guarded() {
		t.Fatal("an inbound on loopback over an HTTP transport does not believe this host, or is not guarded")
	}
	public := Settings{Port: 443}
	if got := public.Over(readsForwarded{}); got.HostForwards() || got.Guarded() {
		t.Fatal("an inbound listening beyond loopback believes this host")
	}
	// Behind an nginx stream block on this host, the PROXY header names the
	// client: a client that had the core dial the front is named as this host,
	// and the front appends no X-Forwarded-For, so one would be the client's.
	proxied := Settings{Enabled: true, Port: 10443, loopback: true}
	if proxied.TrustsHost() {
		t.Fatal("a PROXY-enabled inbound on loopback believes X-Forwarded-For from this host")
	}
	if got := proxied.Over(readsForwarded{}); got.HostForwards() || !got.Guarded() {
		t.Fatal("a PROXY-enabled inbound on loopback believes this host, or is not guarded")
	}
}

type plainTransport struct{}

func (plainTransport) Network() []string { return []string{"tcp"} }

type readsForwarded struct{ plainTransport }

func (readsForwarded) ReadsForwarded() bool { return true }

// An expired address list is answered from at once on the CDN path while one
// goroutine reloads it, so a burst of requests from an edge never queues
// behind the syscall; the reload's answer is used once it lands.
func TestAddrCacheContainsNowDoesNotWait(t *testing.T) {
	release := make(chan struct{})
	loads := make(chan struct{}, 4)
	first := true
	cache := addrCache{load: func() (map[netip.Addr]struct{}, error) {
		loads <- struct{}{}
		if first {
			first = false
			return map[netip.Addr]struct{}{netip.MustParseAddr("203.0.113.5"): {}}, nil
		}
		<-release
		return map[netip.Addr]struct{}{netip.MustParseAddr("203.0.113.6"): {}}, nil
	}}
	if !cache.containsNow(netip.MustParseAddr("203.0.113.5")) {
		t.Fatal("wrong answer from the first load")
	}
	<-loads
	cache.snapshot.Store(&addrSnapshot{addrs: cache.snapshot.Load().addrs, loaded: time.Now().Add(-2 * hostAddrsTTL)})
	done := make(chan bool)
	go func() { done <- cache.containsNow(netip.MustParseAddr("203.0.113.5")) }()
	select {
	case stale := <-done:
		if !stale {
			t.Fatal("the expired list was not answered from")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("containsNow waited for the reload")
	}
	<-loads
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for !cache.containsNow(netip.MustParseAddr("203.0.113.6")) {
		if time.Now().After(deadline) {
			t.Fatal("the background reload never landed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
