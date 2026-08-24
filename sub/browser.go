package sub

import (
	"strings"
)

// browserTokens are substrings that, alongside a "Mozilla/" prefix, mark a
// User-Agent as a real web browser. The Mozilla/ prefix is the strong signal —
// real browsers always send it — and the engine/product tokens that follow
// disambiguate browsers from generic HTTP clients that happen to mimic the
// prefix.
var browserTokens = []string{
	"chrome",
	"chromium",
	"firefox",
	"safari",
	"applewebkit",
	"gecko",
	"edg",
	"opera",
	"opr",
	"vivaldi",
	"brave",
	"trident",
	"msie",
}

// isBrowserUA reports whether the given HTTP User-Agent belongs to a real web
// browser. Anything else — proxy clients (sing-box, Clash, v2rayN, ...) and
// command-line tools (curl, wget, ...) — returns false so the subscription
// endpoint can keep serving them raw config text.
func isBrowserUA(userAgent string) bool {
	ua := strings.ToLower(userAgent)

	// The overwhelming majority of HTTP clients, browsers or not, send a bare
	// protocol or product token (curl/7.x, sing-box/1.x, Clash/...). Real
	// browsers are the ones that open with "Mozilla/". Requiring the prefix is
	// intentionally strict so we never mislabel a proxy client as a browser.
	if !strings.Contains(ua, "mozilla/") {
		return false
	}

	if strings.Contains(ua, "curl/") || strings.Contains(ua, "wget/") ||
		strings.Contains(ua, "python-requests") || strings.Contains(ua, "go-http-client") {
		return false
	}

	for _, token := range browserTokens {
		if strings.Contains(ua, token) {
			return true
		}
	}

	return false
}
