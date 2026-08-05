package util

import "testing"

func TestShadowsocksClientConfigKey(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		// The 128-bit method is the only one on the short secret.
		{"2022-blake3-aes-128-gcm", "shadowsocks16"},

		// The other 2022 methods need 32 bytes, which is what "shadowsocks"
		// holds. Routing them anywhere else empties the inbound's user list.
		{"2022-blake3-aes-256-gcm", "shadowsocks"},
		{"2022-blake3-chacha20-poly1305", "shadowsocks"},

		// Legacy methods take any length.
		{"aes-256-gcm", "shadowsocks"},
		{"chacha20-ietf-poly1305", "shadowsocks"},
		{"none", "shadowsocks"},
		{"", "shadowsocks"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if got := ShadowsocksClientConfigKey(tt.method); got != tt.want {
				t.Errorf("ShadowsocksClientConfigKey(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}
