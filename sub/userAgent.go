package sub

import "strings"

// formatForUserAgent picks the subscription format a well-known client reads
// when the request names none (upstream s-ui 1.6.8). It matches only the
// product token, the part before the first "/" or space: Hiddify announces
// itself "HiddifyNext/2.5 (android) like ClashMeta v2ray sing-box", and it
// reads the plain links best. An unknown client gets "", the plain links, so
// the bare URL is unchanged for everything not listed here.
//
// Only data formats are ever picked. The dashboard (?format=page) and its
// JSON (?format=info) stay behind the explicit query: a browser is not a
// proxy client, and handing an HTML page to one would read as a broken
// subscription rather than a cosmetic glitch.
func formatForUserAgent(userAgent string) string {
	product := strings.ToLower(strings.TrimSpace(userAgent))
	if i := strings.IndexAny(product, "/ "); i >= 0 {
		product = product[:i]
	}
	switch {
	case product == "happ":
		return "happ"
	// clash-verge, ClashX, ClashMetaForAndroid, ClashforWindows, FlClash,
	// clash-nyanpasu, mihomo and Stash all load a Clash/mihomo config.
	case strings.Contains(product, "clash"), product == "mihomo", product == "stash":
		return "clash"
	// The sing-box CLI and its own apps for Android, iOS, macOS and tvOS.
	case product == "sing-box", product == "sfa", product == "sfi", product == "sfm", product == "sft":
		return "json"
	}
	return ""
}

// happLinks rewrites the plain links for Happ, which takes SOCKS5 as socks://
// and has no URI format for HTTP proxies: an http(s):// line is one node it
// cannot read. naive+https:// and the rest keep their own schemes.
func happLinks(links []string) []string {
	result := make([]string, 0, len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if scheme, rest, ok := strings.Cut(link, "://"); ok {
			switch strings.ToLower(scheme) {
			case "http", "https":
				continue
			case "socks5":
				link = "socks://" + rest
			}
		}
		result = append(result, link)
	}
	return result
}
