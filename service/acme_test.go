package service

import (
	"reflect"
	"strings"
	"testing"
)

// Every nginx-facing decision in EnsureVhost / SyncVhosts / ensureNginxServerBlock is
// deliberately a pure function that touches neither the filesystem nor exec, so it can
// be verified on a machine without nginx.

func TestVhostDomainOf(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/etc/nginx/conf.d/s-ui-proxy-a.com.conf", "a.com"},
		// Historical prefix: still on disk on upgraded installs. It has to be recognised
		// so it can be deleted before generation, or it collides with the newly generated
		// file on 443 and the proxy can never be switched on.
		{"/etc/nginx/conf.d/s-ui-panel-a.com.conf", "a.com"},
		{"/etc/nginx/conf.d/s-ui-proxy-sub.a.com.conf", "sub.a.com"},
		// The ACME validation block must never be swept away as a proxy — deleting it
		// breaks certificate renewal.
		{"/etc/nginx/conf.d/s-ui-acme-a.com.conf", ""},
		{"/etc/nginx/conf.d/default.conf", ""},
		{"/etc/nginx/nginx.conf", ""},
		{"/etc/nginx/conf.d/s-ui-proxy-a.com.conf.bak", ""},
	}
	for _, tt := range tests {
		if got := vhostDomainOf(tt.path); got != tt.want {
			t.Errorf("vhostDomainOf(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestBuildVhostSpecs(t *testing.T) {
	panel := ProxySide{Name: "panel", Enabled: true, Domain: "a.com", Path: "/app/", Port: 2095}
	sub := ProxySide{Name: "subscription", Enabled: true, Domain: "a.com", Path: "/sub/", Port: 2096}

	// A shared domain must collapse into two locations in one vhost: emitting a server
	// block each makes nginx drop the second as a conflicting server name, silently
	// taking one side offline.
	specs := BuildVhostSpecs(panel, sub)
	if len(specs) != 1 {
		t.Fatalf("same domain should collapse into 1 vhost, got %d", len(specs))
	}
	if len(specs[0].Endpoints) != 2 || specs[0].Endpoints[0].Name != "panel" {
		t.Errorf("want 2 endpoints with panel first (fixed order is what makes the generated content comparable): %+v", specs[0].Endpoints)
	}

	// Distinct domains get one vhost each.
	sub2 := sub
	sub2.Domain = "b.com"
	if specs := BuildVhostSpecs(panel, sub2); len(specs) != 2 {
		t.Errorf("distinct domains should yield 2 vhosts, got %d", len(specs))
	}

	// A disabled side takes no part; with both off there are no vhosts at all, which is
	// how SyncVhosts wipes every generated config — the one and only cleanup path for
	// switching the proxy off.
	off := panel
	off.Enabled = false
	if specs := BuildVhostSpecs(off, sub); len(specs) != 1 || specs[0].Endpoints[0].Name != "subscription" {
		t.Errorf("a disabled side must not participate: %+v", specs)
	}
	if specs := BuildVhostSpecs(off); len(specs) != 0 {
		t.Errorf("want no vhosts when both sides are off, got %+v", specs)
	}

	// A blank domain is always skipped, else we would emit a dead config with an empty
	// server_name.
	blank := panel
	blank.Domain = "  "
	if specs := BuildVhostSpecs(blank); len(specs) != 0 {
		t.Errorf("blank domain should be skipped, got %+v", specs)
	}

	// Case is normalised: two spellings of one domain emitting a vhost each means the
	// second is silently dropped as conflicting.
	upper := sub
	upper.Domain = "A.COM"
	specs = BuildVhostSpecs(panel, upper)
	if len(specs) != 1 || specs[0].Domain != "a.com" {
		t.Errorf("same domain in different case should merge and lowercase: %+v", specs)
	}
}

func TestParseNginxConflicts(t *testing.T) {
	// A dual-stack block reports once for v4 and once for v6.
	out := `nginx: [warn] conflicting server name "a.com" on 0.0.0.0:443, ignored
nginx: [warn] conflicting server name "a.com" on [::]:443, ignored
nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful`
	got := parseNginxConflicts(out)
	want := []nginxConflict{
		{Name: "a.com", Addr: "0.0.0.0:443", Port: 443},
		{Name: "a.com", Addr: "[::]:443", Port: 443},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNginxConflicts() = %+v, want %+v", got, want)
	}

	clean := `nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful`
	if got := parseNginxConflicts(clean); len(got) != 0 {
		t.Errorf("clean nginx -t output should parse to no conflicts, got %+v", got)
	}

	// A unix socket has no port, so Port stays 0 and vhostConflictsOn443 excludes it
	// as not-443.
	if got := parseNginxConflicts(`conflicting server name "a.com" on unix:/var/run/nginx.sock, ignored`); len(got) != 1 ||
		got[0].Port != 0 || got[0].Addr != "unix:/var/run/nginx.sock" {
		t.Errorf("unix socket parsed wrong: %+v", got)
	}
}

func TestVhostConflictsOn443(t *testing.T) {
	const domain = "us.koiup.com"
	tests := []struct {
		name string
		out  string
		want bool
	}{
		// Regression guard: a duplicate on :80 (the ACME validation block clashing with
		// another :80 block) has nothing to do with this 443 vhost. The old code used two
		// bare Contains calls, misread it as a 443 conflict, and rolled back a perfectly
		// good config.
		{"duplicate on :80", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:80, ignored`, false},
		{"duplicate on v6 :80", `nginx: [warn] conflicting server name "us.koiup.com" on [::]:80, ignored`, false},
		{"443 v4", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:443, ignored`, true},
		{"443 v6", `nginx: [warn] conflicting server name "us.koiup.com" on [::]:443, ignored`, true},
		{"443 on an explicit v6 address", `nginx: [warn] conflicting server name "us.koiup.com" on [2001:db8::1]:443, ignored`, true},
		{"443 on an explicit v4 address", `nginx: [warn] conflicting server name "us.koiup.com" on 1.2.3.4:443, ignored`, true},
		{"domain differs in case", `nginx: [warn] conflicting server name "US.KOIUP.COM" on 0.0.0.0:443, ignored`, true},
		{"another domain conflicts on 443", `nginx: [warn] conflicting server name "other.com" on 0.0.0.0:443, ignored`, false},
		// Reproduces the old bug exactly: the two Contains calls matched independently
		// across the whole text — "conflicting server name" from line one, the domain from
		// line two — so it declared a conflict.
		{"other domain on 443 plus our domain on 80", `nginx: [warn] conflicting server name "other.com" on 0.0.0.0:443, ignored
nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:80, ignored`, false},
		{"443 and 80 interleaved", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:80, ignored
nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:443, ignored`, true},
		{"unix socket", `nginx: [warn] conflicting server name "us.koiup.com" on unix:/var/run/nginx.sock, ignored`, false},
		// If the wording ever changes and the address will not parse, err on the side of
		// true: a 443 block that really was dropped takes the panel offline.
		{"missing the on-clause", `nginx: [warn] conflicting server name "us.koiup.com", ignored`, true},
		{"no conflict", "nginx: configuration file /etc/nginx/nginx.conf test is successful", false},
	}
	for _, tt := range tests {
		if got := vhostConflictsOn443(parseNginxConflicts(tt.out), domain); got != tt.want {
			t.Errorf("%s: vhostConflictsOn443() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNginxListensOn(t *testing.T) {
	tests := []struct {
		block string
		port  int
		want  bool
	}{
		{"listen 443 ssl;", 443, true},
		{"listen 443 ssl;", 80, false},
		{"listen [::]:443 ssl http2;", 443, true},
		{"listen *:443;", 443, true},
		{"listen 1.2.3.4:443;", 443, true},
		{"listen 127.0.0.1:8443;", 443, false},
		{"listen 80;\nlisten [::]:80;", 80, true},
		// With an address but no port, nginx defaults to 443 or 80 by the ssl parameter.
		{"listen 1.2.3.4 ssl;", 443, true},
		{"listen 1.2.3.4;", 80, true},
		{"listen unix:/var/run/nginx.sock;", 80, false},
		// An upstream's `server host:443;` is not a listen and must not match.
		{"server 1.2.3.4:443;", 443, false},
		{"listen_backlog 443;", 443, false},
	}
	for _, tt := range tests {
		if got := nginxListensOn(tt.block, tt.port); got != tt.want {
			t.Errorf("nginxListensOn(%q, %d) = %v, want %v", tt.block, tt.port, got, tt.want)
		}
	}
}

// fakeDump mimics nginx -T output: warn/ok lines mixed in by CombinedOutput first
// (belonging to no file), then one section per file.
const fakeDump = `nginx: [warn] conflicting server name "a.com" on 0.0.0.0:443, ignored
nginx: configuration file /etc/nginx/nginx.conf test is successful
# configuration file /etc/nginx/nginx.conf:
http {
    include /etc/nginx/conf.d/*.conf;
}

# configuration file /etc/nginx/conf.d/s-ui-acme-a.com.conf:
server {
    listen 80;
    listen [::]:80;
    server_name a.com;
    root /var/www/html;
}

# configuration file /etc/nginx/conf.d/s-ui-proxy-a.com.conf:
# Generated by s-ui for a.com - do not edit.
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name a.com;
    location /app/ {
        proxy_pass http://127.0.0.1:2095;
    }
}

# configuration file /etc/nginx/sites-enabled/default:
server { listen 443 ssl; server_name a.com; location / { proxy_pass http://127.0.0.1:8080; } }
server {
    listen 443 ssl;
    server_name b.com;
}
`

func TestNginxFilesServing(t *testing.T) {
	got := nginxFilesServing(fakeDump, "a.com", 443)
	want := []string{
		"/etc/nginx/conf.d/s-ui-proxy-a.com.conf",
		// the compact one-line form must be extracted too
		"/etc/nginx/sites-enabled/default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nginxFilesServing(443) = %v, want %v", got, want)
	}

	got80 := nginxFilesServing(fakeDump, "a.com", 80)
	want80 := []string{"/etc/nginx/conf.d/s-ui-acme-a.com.conf"}
	if !reflect.DeepEqual(got80, want80) {
		t.Errorf("nginxFilesServing(80) = %v, want %v", got80, want80)
	}

	// ensureNginxServerBlock's guard rests on this: the proxy file listens on 443 only
	// and must not pose as the :80 validation block.
	if files := nginxFilesServing(fakeDump, "b.com", 80); len(files) != 0 {
		t.Errorf("b.com only has a 443 block and must not match on 80: %v", files)
	}
}

func TestSplitNginxDump(t *testing.T) {
	secs := splitNginxDump(fakeDump)
	if len(secs) != 4 {
		t.Fatalf("got %d sections, want 4: %+v", len(secs), secs)
	}
	if secs[0].Path != "/etc/nginx/nginx.conf" {
		t.Errorf("first section is %q, want /etc/nginx/nginx.conf (warn lines before the first header should be dropped)", secs[0].Path)
	}
	if strings.Contains(secs[0].Body, "conflicting server name") {
		t.Error("warn lines before the first header must not leak into the first section body")
	}
}

func TestNginxServerBlocks(t *testing.T) {
	conf := `server {
    listen 443 ssl;
    server_name a.com;
    location / { proxy_pass http://127.0.0.1:2095; }
}
upstream backend {
    server 127.0.0.1:9000;
}
# server { listen 80; server_name commented.com; }
server { listen 80; server_name b.com; }`

	blocks := nginxServerBlocks(conf)
	if len(blocks) != 2 {
		t.Fatalf("got %d server blocks, want 2: %q", len(blocks), blocks)
	}
	// The nested location braces in the first block must balance, not end early.
	if !strings.Contains(blocks[0], "a.com") || !strings.Contains(blocks[0], "proxy_pass") {
		t.Errorf("first block is incomplete: %q", blocks[0])
	}
	if !strings.Contains(blocks[1], "b.com") {
		t.Errorf("second block should be b.com: %q", blocks[1])
	}
	for _, b := range blocks {
		if strings.Contains(b, "commented.com") {
			t.Error("a commented-out server block must not be extracted")
		}
		if strings.Contains(b, ":9000") {
			t.Error("an upstream block must not be taken for a server block")
		}
	}
}
