// Package androidexclusions prepares Android application lists for subscription templates.
// It does not access the panel database or the subscription generator.
package androidexclusions

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 1 << 20
const MaxPackages = 20000

var packageID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func validID(s string) bool { return len(s) <= 255 && packageID.MatchString(s) }

// Parse accepts plain-text lists and JSON arrays of strings. A text record is
// a whole ID token, optionally quoted, separated by spaces or commas.
// URLs, HTML and comments are not mined for substrings. A bare hostname
// with package-name syntax cannot be distinguished from an ID by syntax.
// Empty or unusable downloads must not erase the last good remote snapshot.
func Parse(data []byte) ([]string, error) {
	if len(data) > MaxBytes {
		return nil, errors.New("list exceeds size limit")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("list is not UTF-8")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.TrimSpace(data)
	// Reject whole HTML/XML error documents even if the server lies about MIME.
	if len(data) > 0 && (data[0] == '<' || data[0] == '{') {
		return nil, errors.New("HTML/XML or JSON object is not a package list")
	}
	var records []string
	var items []any
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, errors.New("invalid JSON package list")
		}
		for _, item := range items {
			if v, ok := item.(string); ok {
				records = append(records, v)
			}
		}
	} else {
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 4096), MaxBytes+1)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
				continue
			}
			if strings.ContainsAny(line, "<>") {
				continue
			}
			// Strip comments at token boundaries, including tabs.
			for i, r := range line {
				if (r == '#' || (r == '/' && strings.HasPrefix(line[i:], "//"))) && (i == 0 || unicode.IsSpace(rune(line[i-1])) || line[i-1] == ',') {
					line = line[:i]
					break
				}
			}
			// Keep whole whitespace-delimited tokens; never search within a
			// URL, email address or path. Commas separate plain list entries.
			for _, field := range strings.FieldsFunc(line, func(r rune) bool {
				return unicode.IsSpace(r) || r == ','
			}) {
				if strings.ContainsAny(field, "/\\@:=?&%#") {
					continue
				}
				records = append(records, field)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	result := Normalize(records)
	if len(result) == 0 {
		return nil, errors.New("no valid package IDs")
	}
	if len(result) > MaxPackages {
		return nil, errors.New("too many package IDs")
	}
	return result, nil
}

// Normalize returns a stable, unique, case-sensitive sequence.
func Normalize(records []string) []string {
	seen := make(map[string]struct{})
	for _, v := range records {
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "\"'")
		v = strings.TrimSpace(v)
		if validID(v) {
			seen[v] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for v := range seen {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// Filter conservatively reserves every ID explicitly referenced by any rule,
// regardless of outbound, action, invert or nested logical mode. Callers pass
// only effective rule trees (including DNS and available rule-set bodies).
// An unsupported regexp returns an error instead of risking a bypass.
func Filter(candidates []string, rules ...any) (result []string, removed int, err error) {
	exact := map[string]struct{}{}
	var patterns []*regexp.Regexp
	var add func(any, bool) error
	add = func(v any, pattern bool) error {
		switch x := v.(type) {
		case string:
			if pattern {
				r, e := regexp.Compile(x)
				if e != nil {
					return fmt.Errorf("invalid package rule regexp: %w", e)
				}
				patterns = append(patterns, r)
			} else {
				exact[x] = struct{}{}
			}
		case []any:
			for _, item := range x {
				if e := add(item, pattern); e != nil {
					return e
				}
			}
		case []string:
			for _, item := range x {
				if e := add(item, pattern); e != nil {
					return e
				}
			}
		case nil:
		default:
			return errors.New("invalid package rule value")
		}
		return nil
	}
	var visit func(any) error
	visit = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				switch k {
				case "package_name":
					if e := add(item, false); e != nil {
						return e
					}
				case "package_name_regex":
					if e := add(item, true); e != nil {
						return e
					}
				default:
					if e := visit(item); e != nil {
						return e
					}
				}
			}
		case []any:
			for _, item := range x {
				if e := visit(item); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for _, rule := range rules {
		if e := visit(rule); e != nil {
			return nil, 0, e
		}
	}
	result = make([]string, 0, len(candidates))
	for _, id := range Normalize(candidates) {
		_, blocked := exact[id]
		if !blocked {
			for _, r := range patterns {
				if r.MatchString(id) {
					blocked = true
					break
				}
			}
		}
		if blocked {
			removed++
		} else {
			result = append(result, id)
		}
	}
	return result, removed, nil
}

var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	// Only currently allocated global IPv6 space, not special/future prefixes.
	return ip.Is4() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

func validateURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("only HTTPS URLs without credentials or fragments on port 443 are allowed")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || !strings.Contains(host, ".") && !strings.Contains(host, ":") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("non-public hostname")
	}
	if ip, e := netip.ParseAddr(host); e == nil && !publicIP(ip) {
		return errors.New("non-public address")
	}
	return nil
}

// Fetch resolves and validates every destination at dial time, then connects
// directly to the checked IP. TLS still verifies the original hostname. No
// environment proxies, cookies, or redirects to unvalidated destinations.
func Fetch(ctx context.Context, rawURL string) ([]string, error) {
	u, e := url.Parse(rawURL)
	if e != nil {
		return nil, errors.New("invalid source URL")
	}
	if e = validateURL(u); e != nil {
		return nil, e
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		if port != "443" {
			return nil, errors.New("invalid destination port")
		}
		var ips []netip.Addr
		if ip, e := netip.ParseAddr(host); e == nil {
			ips = []netip.Addr{ip}
		} else {
			ips, e = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if e != nil {
				return nil, e
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("no DNS addresses")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("non-public DNS address")
			}
		}
		local, e := net.InterfaceAddrs()
		if e != nil {
			return nil, errors.New("cannot inspect local addresses")
		}
		for _, a := range local {
			p, e := netip.ParsePrefix(a.String())
			if e != nil {
				continue
			}
			for _, ip := range ips {
				if p.Addr().Unmap() == ip.Unmap() {
					return nil, errors.New("local interface address")
				}
			}
		}
		var last error
		for _, ip := range ips {
			c, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer")
		if len(via) > 3 {
			return errors.New("too many redirects")
		}
		return validateURL(req.URL)
	}}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, errors.New("invalid source request")
	}
	req.Header.Set("Accept", "text/plain, application/json, */*;q=0.1")
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source HTTP status %d", resp.StatusCode)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil, errors.New("HTML response is not a package list")
	}
	if resp.ContentLength > MaxBytes {
		return nil, errors.New("list exceeds size limit")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if e != nil {
		return nil, e
	}
	return Parse(b)
}
