package service

// Downloading remote Android package lists for the JSON subscription's TUN
// exclude_package (see excludePkg.go). The fetch and the SSRF guard are the
// ones proposed in #326.
//
// The URLs are typed by an admin, but the request is made by the panel host,
// so the guard still matters: without it a list URL is a way to make the panel
// GET anything on its own network (cloud metadata, the panel's own API on
// loopback) and to read back whatever of the answer parses as package IDs.

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
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	pkgListMaxBytes   = 1 << 20
	pkgListMaxIDs     = 5000
	pkgListMaxSources = 10
	pkgListMaxURLLen  = 2048
	pkgListTimeout    = 20 * time.Second
)

var pkgIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func validPkgID(s string) bool { return len(s) <= 255 && pkgIDPattern.MatchString(s) }

// normalizePkgIDs trims, drops anything that is not an Android package ID, and
// returns the rest unique and sorted.
func normalizePkgIDs(records []string) []string {
	seen := make(map[string]struct{}, len(records))
	for _, v := range records {
		v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`))
		if validPkgID(v) {
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// parsePkgList accepts a JSON array of strings, or plain text with one or more
// IDs per line separated by spaces or commas and #, // or ; comments. Tokens
// are taken whole -- a URL, path or address is skipped, never mined for a
// substring that happens to look like an ID -- and an HTML or JSON-object
// document (an error page served with 200) is refused outright.
func parsePkgList(data []byte) ([]string, error) {
	if len(data) > pkgListMaxBytes {
		return nil, errors.New("list is larger than 1 MiB")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("list is not UTF-8")
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if len(data) > 0 && (data[0] == '<' || data[0] == '{') {
		return nil, errors.New("not a package list (HTML or JSON object)")
	}
	var records []string
	if len(data) > 0 && data[0] == '[' {
		var items []any
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, errors.New("invalid JSON array")
		}
		for _, item := range items {
			if s, ok := item.(string); ok {
				records = append(records, s)
			}
		}
	} else {
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 4096), pkgListMaxBytes+1)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") || strings.ContainsAny(line, "<>") {
				continue
			}
			// A trailing comment starts at a token boundary.
			for i, r := range line {
				if (r == '#' || (r == '/' && strings.HasPrefix(line[i:], "//"))) &&
					(i == 0 || unicode.IsSpace(rune(line[i-1])) || line[i-1] == ',') {
					line = line[:i]
					break
				}
			}
			for _, field := range strings.FieldsFunc(line, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
				if !strings.ContainsAny(field, `/\@:=?&%#`) {
					records = append(records, field)
				}
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	ids := normalizePkgIDs(records)
	if len(ids) == 0 {
		return nil, errors.New("no package IDs in the list")
	}
	if len(ids) > pkgListMaxIDs {
		return nil, fmt.Errorf("more than %d package IDs", pkgListMaxIDs)
	}
	return ids, nil
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

var globalIPv6 = netip.MustParsePrefix("2000::/3")

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return ip.Is4() || globalIPv6.Contains(ip)
}

// validatePkgListURL is the check a URL has to pass to be saved, and again for
// every redirect. It cannot see where a hostname resolves to; the dialer in
// fetchPkgList checks that at connect time.
func validatePkgListURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		(u.Port() != "" && u.Port() != "443") {
		return errors.New("only https:// URLs on port 443, without credentials or a #fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || (!strings.Contains(host, ".") && !strings.Contains(host, ":")) ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("not a public hostname")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicIP(ip) {
		return errors.New("not a public address")
	}
	return nil
}

// fetchPkgList downloads and parses one list. Every destination, redirects
// included, is resolved and checked at dial time and then dialled by the
// checked address, so a DNS answer that changes between the check and the
// connect (rebinding) has nothing to change. TLS still verifies the hostname.
// No environment proxy, no cookies, at most three redirects.
func fetchPkgList(ctx context.Context, rawURL string) ([]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if err = validatePkgListURL(u); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if port != "443" {
			return nil, errors.New("destination port is not 443")
		}
		var ips []netip.Addr
		if ip, err := netip.ParseAddr(host); err == nil {
			ips = []netip.Addr{ip}
		} else if ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no addresses for the host")
		}
		local, err := net.InterfaceAddrs()
		if err != nil {
			return nil, errors.New("cannot read this host's addresses")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("the host resolves to a non-public address")
			}
			for _, a := range local {
				if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Unmap() == ip.Unmap() {
					return nil, errors.New("the host resolves to this machine")
				}
			}
		}
		var last error
		for _, ip := range ips {
			c, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer")
		if len(via) > 3 {
			return errors.New("too many redirects")
		}
		return validatePkgListURL(req.URL)
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	req.Header.Set("Accept", "text/plain, application/json;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil, errors.New("not a package list (HTML)")
	}
	if resp.ContentLength > pkgListMaxBytes {
		return nil, errors.New("list is larger than 1 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, pkgListMaxBytes+1))
	if err != nil {
		return nil, err
	}
	return parsePkgList(b)
}
