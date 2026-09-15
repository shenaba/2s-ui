package util

import "encoding/base64"

// Function to return decoded bytes if a string is Base64 encoded
func StrOrBase64Encoded(str string) string {
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err == nil {
		return string(decoded)
	}
	return str
}

// StrOrBase64AnyEncoded is StrOrBase64Encoded for a string that is *known* to
// be base64, in whichever of the four forms the writer happened to use.
//
// SIP002 specifies base64url without padding for the ss:// userinfo, which
// StdEncoding rejects on both counts, so the panel could not read back the
// links it generates itself. Third-party ss links are base64url too.
//
// Kept separate from StrOrBase64Encoded rather than widening it: that one is
// also handed text that may not be base64 at all -- an external subscription
// body, a naive hostname -- and the unpadded alphabets have no padding to fail
// on, so widening it there would start "decoding" plain strings into garbage.
func StrOrBase64AnyEncoded(str string) string {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawURLEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
	} {
		if decoded, err := enc.DecodeString(str); err == nil {
			return string(decoded)
		}
	}
	return str
}

func B64StrToByte(str string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(str)
}

func ByteToB64Str(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
