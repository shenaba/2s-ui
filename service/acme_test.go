package service

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

	build := func(t *testing.T, sides ...ProxySide) []VhostOptions {
		t.Helper()
		specs, err := BuildVhostSpecs(sides...)
		if err != nil {
			t.Fatalf("BuildVhostSpecs() unexpected error: %v", err)
		}
		return specs
	}

	// A shared domain must collapse into two locations in one vhost: emitting a server
	// block each makes nginx drop the second as a conflicting server name, silently
	// taking one side offline.
	specs := build(t, panel, sub)
	if len(specs) != 1 {
		t.Fatalf("same domain should collapse into 1 vhost, got %d", len(specs))
	}
	if len(specs[0].Endpoints) != 2 || specs[0].Endpoints[0].Name != "panel" {
		t.Errorf("want 2 endpoints with panel first (fixed order is what makes the generated content comparable): %+v", specs[0].Endpoints)
	}

	// Distinct domains get one vhost each.
	sub2 := sub
	sub2.Domain = "b.com"
	if specs := build(t, panel, sub2); len(specs) != 2 {
		t.Errorf("distinct domains should yield 2 vhosts, got %d", len(specs))
	}

	// A disabled side takes no part; with both off there are no vhosts at all, which is
	// how SyncVhosts wipes every generated config — the one and only cleanup path for
	// switching the proxy off.
	off := panel
	off.Enabled = false
	if specs := build(t, off, sub); len(specs) != 1 || specs[0].Endpoints[0].Name != "subscription" {
		t.Errorf("a disabled side must not participate: %+v", specs)
	}
	if specs := build(t, off); len(specs) != 0 {
		t.Errorf("want no vhosts when both sides are off, got %+v", specs)
	}

	// Switched ON with no domain is an error, NOT a silent skip: skipping shrinks specs
	// (here to empty) and SyncVhosts then deletes the generated vhosts, while the
	// service already runs plaintext — nothing left answering on 443.
	blank := panel
	blank.Domain = "  "
	if _, err := BuildVhostSpecs(blank); err == nil {
		t.Error("an enabled side with a blank domain must be an error, not a silent skip")
	}
	if _, err := BuildVhostSpecs(panel, ProxySide{Name: "subscription", Enabled: true}); err == nil {
		t.Error("one good side does not excuse an enabled side with no domain")
	}
	// A disabled side with no domain is the normal untouched state and must stay silent.
	blankOff := blank
	blankOff.Enabled = false
	if specs, err := BuildVhostSpecs(blankOff); err != nil || len(specs) != 0 {
		t.Errorf("a disabled side without a domain is fine, got %+v / %v", specs, err)
	}

	// Case is normalised: two spellings of one domain emitting a vhost each means the
	// second is silently dropped as conflicting.
	upper := sub
	upper.Domain = "A.COM"
	specs = build(t, panel, upper)
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

	// A unix socket has no port, so Port stays 0 and conflictsOnPort excludes it
	// as not-443.
	if got := parseNginxConflicts(`conflicting server name "a.com" on unix:/var/run/nginx.sock, ignored`); len(got) != 1 ||
		got[0].Port != 0 || got[0].Addr != "unix:/var/run/nginx.sock" {
		t.Errorf("unix socket parsed wrong: %+v", got)
	}

	// Verbatim from nginx 1.22.1 on Debian: the prefix is a timestamp and pid, not the
	// "nginx: " the cases above assume — warnings go through the error log, and only
	// the final verdict lines carry that prefix. Parsing looks for the marker anywhere
	// in the line, and this case exists so a rewrite cannot quietly start anchoring it.
	real := `2026/07/30 09:50:32 [warn] 2031855#2031855: conflicting server name "us.koiup.com" on 0.0.0.0:443, ignored
2026/07/30 09:50:32 [warn] 2031855#2031855: conflicting server name "us.koiup.com" on [::]:443, ignored
nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful`
	gotReal := parseNginxConflicts(real)
	wantReal := []nginxConflict{
		{Name: "us.koiup.com", Addr: "0.0.0.0:443", Port: 443},
		{Name: "us.koiup.com", Addr: "[::]:443", Port: 443},
	}
	if !reflect.DeepEqual(gotReal, wantReal) {
		t.Errorf("real nginx 1.22.1 output parsed as %+v, want %+v", gotReal, wantReal)
	}
}

func TestConflictsOnPort(t *testing.T) {
	const domain = "us.koiup.com"
	tests := []struct {
		name string
		out  string
		want bool
	}{
		// Regression guard: a duplicate on :80 says nothing about this 443 vhost. The old
		// code used two bare Contains calls, misread it, and rolled back a good config.
		{"duplicate on :80", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:80, ignored`, false},
		{"duplicate on v6 :80", `nginx: [warn] conflicting server name "us.koiup.com" on [::]:80, ignored`, false},
		{"443 v4", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:443, ignored`, true},
		{"443 v6", `nginx: [warn] conflicting server name "us.koiup.com" on [::]:443, ignored`, true},
		{"443 on an explicit v6 address", `nginx: [warn] conflicting server name "us.koiup.com" on [2001:db8::1]:443, ignored`, true},
		{"443 on an explicit v4 address", `nginx: [warn] conflicting server name "us.koiup.com" on 1.2.3.4:443, ignored`, true},
		{"domain differs in case", `nginx: [warn] conflicting server name "US.KOIUP.COM" on 0.0.0.0:443, ignored`, true},
		{"another domain conflicts on 443", `nginx: [warn] conflicting server name "other.com" on 0.0.0.0:443, ignored`, false},
		// Reproduces the old bug exactly: the two Contains calls matched independently
		// across the text — the marker from line one, the domain from line two.
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
		if got := conflictsOnPort(parseNginxConflicts(tt.out), domain, httpsPort); got != tt.want {
			t.Errorf("%s: conflictsOnPort(443) = %v, want %v", tt.name, got, tt.want)
		}
	}

	// The :80 direction, used by ensureNginxServerBlock right after it writes the
	// validation block. Its guard concluded no :80 block existed, so a warn naming :80
	// proves it missed one and our block would silently replace the user's.
	on80 := []struct {
		name string
		out  string
		want bool
	}{
		{"our domain on :80", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:80, ignored`, true},
		{"our domain on v6 :80", `nginx: [warn] conflicting server name "us.koiup.com" on [::]:80, ignored`, true},
		{"domain differs in case", `nginx: [warn] conflicting server name "US.KOIUP.COM" on 0.0.0.0:80, ignored`, true},
		// The proxy vhost colliding on 443 says nothing about the validation block.
		{"our domain on :443", `nginx: [warn] conflicting server name "us.koiup.com" on 0.0.0.0:443, ignored`, false},
		{"another domain on :80", `nginx: [warn] conflicting server name "other.com" on 0.0.0.0:80, ignored`, false},
		{"unix socket", `nginx: [warn] conflicting server name "us.koiup.com" on unix:/var/run/nginx.sock, ignored`, false},
		// Same fail-open rule as 443: an unparsed address costs a rollback plus an
		// explanation, while missing a real one hands the user's site to our block.
		{"missing the on-clause", `nginx: [warn] conflicting server name "us.koiup.com", ignored`, true},
		{"no conflict", "nginx: configuration file /etc/nginx/nginx.conf test is successful", false},
	}
	for _, tt := range on80 {
		if got := conflictsOnPort(parseNginxConflicts(tt.out), domain, httpPort); got != tt.want {
			t.Errorf("%s: conflictsOnPort(80) = %v, want %v", tt.name, got, tt.want)
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

func TestBuildVhostConfWebsocketUpgrade(t *testing.T) {
	panel := ProxySide{Name: "panel", Enabled: true, Domain: "a.com", Path: "/app/", Port: 2095}
	sub := ProxySide{Name: "subscription", Enabled: true, Domain: "a.com", Path: "/sub/", Port: 2096}
	specs, err := BuildVhostSpecs(panel, sub)
	if err != nil || len(specs) != 1 {
		t.Fatalf("BuildVhostSpecs() = %+v, %v", specs, err)
	}

	conf := buildVhostConf(specs[0], "cert.pem", "key.pem", nginxEnv{})

	// The panel's /ws push channel dies with a 400 behind nginx unless every
	// generated location speaks HTTP/1.1 upstream and forwards the upgrade
	// hop-by-hop headers. One occurrence per location, or a location was left
	// out and websockets break only for that side.
	//
	// Connection is passed through rather than pinned to "upgrade": the
	// subscription location never speaks WebSocket and must not advertise an
	// upgrade on every ordinary request.
	for _, directive := range []string{
		"proxy_http_version 1.1;",
		"proxy_set_header Upgrade           $http_upgrade;",
		"proxy_set_header Connection        $http_connection;",
	} {
		if got := strings.Count(conf, directive); got != len(specs[0].Endpoints) {
			t.Errorf("%q appears %d times, want %d (once per location):\n%s",
				directive, got, len(specs[0].Endpoints), conf)
		}
	}
}

// The instance id is what keeps two panels on one host out of each other's nginx
// files, so it has to differ exactly when the database location differs.
func TestVhostOwnerID(t *testing.T) {
	system, test := t.TempDir(), t.TempDir()

	t.Setenv("SUI_DB_FOLDER", system)
	a := vhostOwnerID()
	if a == "" || a != vhostOwnerID() {
		t.Fatalf("id must be non-empty and stable for one DB folder, got %q then %q", a, vhostOwnerID())
	}

	t.Setenv("SUI_DB_FOLDER", test)
	if b := vhostOwnerID(); b == a {
		t.Errorf("two DB folders must not share an id, both got %q", a)
	}

	// One install reached through a symlink is still one instance: a different id here
	// would rewrite the vhost on every start just to flip the tag.
	link := filepath.Join(t.TempDir(), "db")
	if err := os.Symlink(system, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	t.Setenv("SUI_DB_FOLDER", link)
	if got := vhostOwnerID(); got != a {
		t.Errorf("id through a symlink = %q, want the real folder's %q", got, a)
	}
}

func TestVhostOwnerOf(t *testing.T) {
	spec := VhostOptions{Domain: "a.com", Endpoints: []ProxyEndpoint{{Name: "panel", Path: "/app/", Port: 2095}}}

	// Round trip through the real generator: the sweep reads what EnsureVhost writes,
	// and a tag one side emits but the other cannot find makes every file a stranger.
	if got := vhostOwnerOf(buildVhostConf(spec, "cert.pem", "key.pem", nginxEnv{owner: "0123456789abcdef"})); got != "0123456789abcdef" {
		t.Errorf("owner of a generated conf = %q, want the id it was generated with", got)
	}
	// What a build that predates the tag wrote.
	if got := vhostOwnerOf(buildVhostConf(spec, "cert.pem", "key.pem", nginxEnv{})); got != "" {
		t.Errorf("an untagged conf must report no owner, got %q", got)
	}
	// Only the leading comment header counts; a look-alike further down is not a claim.
	body := "# Generated by s-ui for a.com - do not edit.\nserver {\n    " + vhostOwnerMarker + "feedface\n}\n"
	if got := vhostOwnerOf(body); got != "" {
		t.Errorf("a marker inside the server block must not count, got %q", got)
	}
}

// vhostFixture lays out a conf.d the way the panels would have left it.
type vhostFixture struct {
	t   *testing.T
	dir string
}

func newVhostFixture(t *testing.T) vhostFixture {
	return vhostFixture{t: t, dir: t.TempDir()}
}

func (f vhostFixture) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0644); err != nil {
		f.t.Fatal(err)
	}
}

// generated writes the file an instance with this id produces for domain; owner ""
// yields what a build without the tag wrote.
func (f vhostFixture) generated(prefix, domain, owner string) {
	f.t.Helper()
	spec := VhostOptions{Domain: domain, Endpoints: []ProxyEndpoint{{Name: "panel", Path: "/app/", Port: 2095}}}
	f.write(prefix+domain+".conf", buildVhostConf(spec, "cert.pem", "key.pem", nginxEnv{owner: owner}))
}

func (f vhostFixture) files() []string {
	f.t.Helper()
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func wantedSet(domains ...string) map[string]bool {
	set := map[string]bool{}
	for _, d := range domains {
		set[d] = true
	}
	return set
}

// Two panels on one host share conf.d and the filename prefix. Each one's sweep must
// leave the other's vhost alone — going by prefix had every start delete the other
// instance's live 443 entrypoint as "stale".
func TestSweepStaleVhostsLeavesOtherInstances(t *testing.T) {
	const system, test, idle = "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"

	f := newVhostFixture(t)
	f.generated("s-ui-proxy-", "panel.example.com", system)
	f.generated("s-ui-proxy-", "test.example.com", test)
	all := f.files()

	sweeps := []struct {
		who    string
		owner  string
		wanted map[string]bool
	}{
		{"the system panel, serving its own domain", system, wantedSet("panel.example.com")},
		{"the test panel, serving its own domain", test, wantedSet("test.example.com")},
		// The case from the field: started with another SUI_DB_FOLDER, proxy never
		// switched on. Its wanted set is empty, so by prefix alone EVERYTHING is stale.
		{"a third panel with the proxy off", idle, wantedSet()},
	}
	for _, s := range sweeps {
		if removed := sweepStaleVhosts(f.dir, s.wanted, s.owner); removed != 0 {
			t.Errorf("%s removed %d file(s), want 0", s.who, removed)
		}
		if got := f.files(); !reflect.DeepEqual(got, all) {
			t.Fatalf("after the sweep by %s conf.d = %v, want %v untouched", s.who, got, all)
		}
	}

	// And each still cleans up after itself: the test panel switches its proxy off and
	// takes its own vhost with it, not the system panel's.
	if removed := sweepStaleVhosts(f.dir, wantedSet(), test); removed != 1 {
		t.Errorf("the test panel switching off removed %d file(s), want its own 1", removed)
	}
	if got, want := f.files(), []string{"s-ui-proxy-panel.example.com.conf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conf.d = %v, want %v", got, want)
	}
}

// With one panel on the host the sweep must still do what SyncVhosts relies on it
// for: finish a switch-off, and drop the old domain after a rename.
func TestSweepStaleVhostsSingleInstance(t *testing.T) {
	const self = "aaaaaaaaaaaaaaaa"

	layout := func(t *testing.T) vhostFixture {
		f := newVhostFixture(t)
		f.generated("s-ui-proxy-", "new.example.com", self)
		f.generated("s-ui-proxy-", "old.example.com", self)
		// Never ours to remove, whatever the wanted set says: the ACME validation block
		// (losing it breaks renewal) and anything the operator wrote.
		f.write("s-ui-acme-old.example.com.conf", "server { listen 80; server_name old.example.com; }\n")
		f.write("default.conf", "server { listen 80 default_server; }\n")
		return f
	}
	untouchable := []string{"default.conf", "s-ui-acme-old.example.com.conf"}

	t.Run("domain changed", func(t *testing.T) {
		f := layout(t)
		if removed := sweepStaleVhosts(f.dir, wantedSet("new.example.com"), self); removed != 1 {
			t.Errorf("removed %d file(s), want 1 (the old domain)", removed)
		}
		want := append([]string{}, untouchable...)
		want = append(want, "s-ui-proxy-new.example.com.conf")
		if got := f.files(); !reflect.DeepEqual(got, want) {
			t.Errorf("conf.d = %v, want %v", got, want)
		}
	})

	// Both sides off: specs is empty and every vhost this panel generated has to go,
	// or 443 keeps forwarding plaintext to a port that is back on TLS.
	t.Run("proxy switched off", func(t *testing.T) {
		f := layout(t)
		if removed := sweepStaleVhosts(f.dir, wantedSet(), self); removed != 2 {
			t.Errorf("removed %d file(s), want 2 (both generated vhosts)", removed)
		}
		if got := f.files(); !reflect.DeepEqual(got, untouchable) {
			t.Errorf("conf.d = %v, want only %v", got, untouchable)
		}
	})

	// The wanted set is lowercased by the caller; a file on disk in another case is
	// still the wanted domain, not a stale one.
	t.Run("wanted domain in another case", func(t *testing.T) {
		f := newVhostFixture(t)
		f.generated("s-ui-proxy-", "New.Example.com", self)
		if removed := sweepStaleVhosts(f.dir, wantedSet("new.example.com"), self); removed != 0 {
			t.Errorf("removed %d file(s), want 0", removed)
		}
	})

	// Files from before the tag existed carry no owner. They cannot be told from another
	// instance's, so they stay — under either prefix, proxy on or off. The upgrade path
	// does not need them swept: a still-configured domain is rewritten with the tag by
	// the first reconciliation (see TestBuildVhostConfOwnerTagIsDrift).
	t.Run("untagged legacy files are kept", func(t *testing.T) {
		f := newVhostFixture(t)
		f.generated("s-ui-proxy-", "legacy.example.com", "")
		f.generated("s-ui-panel-", "older.example.com", "")
		all := f.files()
		for _, wanted := range []map[string]bool{wantedSet(), wantedSet("new.example.com")} {
			if removed := sweepStaleVhosts(f.dir, wanted, self); removed != 0 {
				t.Errorf("wanted=%v removed %d untagged file(s), want 0", wanted, removed)
			}
		}
		if got := f.files(); !reflect.DeepEqual(got, all) {
			t.Errorf("conf.d = %v, want %v untouched", got, all)
		}
	})
}

// The upgrade path hangs on this: EnsureVhost and CheckVhosts compare whole files, so
// the tag has to be part of the content for an untagged file left by an older build
// to count as drift and be rewritten — which is the only way it ever gets an owner.
func TestBuildVhostConfOwnerTagIsDrift(t *testing.T) {
	spec := VhostOptions{Domain: "a.com", Endpoints: []ProxyEndpoint{{Name: "panel", Path: "/app/", Port: 2095}}}
	legacy := buildVhostConf(spec, "cert.pem", "key.pem", nginxEnv{})
	tagged := buildVhostConf(spec, "cert.pem", "key.pem", nginxEnv{owner: "0123456789abcdef"})

	if legacy == tagged {
		t.Fatal("tagged and untagged output are identical: an upgraded install would never rewrite its file")
	}
	// The tag is a comment and nothing else: the server block nginx parses is unchanged.
	if got := strings.Replace(tagged, "# Only the panel instance with this id cleans this file up.\n"+
		vhostOwnerMarker+"0123456789abcdef\n", "", 1); got != legacy {
		t.Errorf("the tag must be the only difference from the untagged output:\n%s", tagged)
	}
}
