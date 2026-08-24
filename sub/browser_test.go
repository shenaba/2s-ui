package sub

import "testing"

func TestIsBrowserUA(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		// Real browsers: must be detected.
		{"chrome desktop", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36", true},
		{"chrome android", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", true},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:126.0) Gecko/20100101 Firefox/126.0", true},
		{"safari mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15", true},
		{"edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.0.0", true},
		{"opera", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 OPR/111.0.0.0", true},
		{"ie11", "Mozilla/5.0 (Windows NT 10.0; WOW64; Trident/7.0; rv:11.0) like Gecko", true},
		{"mobile iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", true},

		// Proxy clients and tools: must NOT be detected.
		{"curl", "curl/8.6.0", false},
		{"wget", "Wget/1.21.4", false},
		{"sing-box", "sing-box/1.9.0", false},
		{"clash", "clash/v1.18.0", false},
		{"clash-verge", "ClashVerge/1.5.3 (mihomo)", false},
		{"v2rayng", "v2rayNG/1.8.20", false},
		{"empty", "", false},
		{"go-http-client", "Go-http-client/1.1", false},
		{"python-requests", "python-requests/2.31.0", false},
		{"curl with mozilla style", "Mozilla/5.0 curl/8.6.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBrowserUA(tt.ua); got != tt.want {
				t.Errorf("isBrowserUA(%q) = %v, want %v", tt.ua, got, tt.want)
			}
		})
	}
}
