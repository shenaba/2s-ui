package util

// ShadowsocksClientConfigKey returns the key a client's config blob stores its
// Shadowsocks secret under, for the given inbound method.
//
// A client config only ever carries two of them (see the frontend's
// randomConfigs): "shadowsocks16" holds a 16-byte secret and "shadowsocks" a
// 32-byte one. 2022-blake3-aes-128-gcm is the only method deriving a 128-bit
// key, so it is the only one that reads the short secret. Everything else reads
// "shadowsocks" — including 2022-blake3-aes-256-gcm and -chacha20-poly1305,
// which need the full 32 bytes.
//
// Upstream s-ui routes those two to a third key, "shadowsocks32", that no
// client config contains; the lookup then yields "" and the inbound ends up
// with an empty user list. Keep the two-key mapping.
func ShadowsocksClientConfigKey(method string) string {
	if method == "2022-blake3-aes-128-gcm" {
		return "shadowsocks16"
	}
	return "shadowsocks"
}
