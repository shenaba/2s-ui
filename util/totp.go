package util

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 with the parameters every authenticator app assumes when the
// otpauth:// URI omits them: SHA-1, 6 digits, 30-second steps. They are
// deliberately not configurable -- Google Authenticator and most of its clones
// ignore the algorithm and digits parameters anyway, so a panel that offered
// them would be promising interoperability it cannot deliver.
const (
	totpDigits = 6
	// Kept next to totpDigits because the two have to move together: this is
	// what keeps the truncated value inside that many digits.
	totpModulus = 1_000_000
	totpPeriod  = 30 * time.Second

	// How many steps either side of the current one are accepted, covering
	// clock drift between the panel and the phone. One step each way is the
	// usual choice: it widens the window a stolen code stays usable to at most
	// 90 seconds, which the login rate limit already bounds the value of.
	totpSkew = 1

	// 160 bits, the shared-secret size RFC 4226 recommends and the one that
	// divides evenly into base32 characters, so the encoded secret needs no
	// padding and stays copy-pasteable.
	totpSecretBytes = 20
)

// Authenticator apps expect an unpadded secret, and users retype it by hand
// when the QR code is not scannable, so this is also what the panel displays.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh base32 shared secret.
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(buf), nil
}

// TOTPKeyURI builds the otpauth:// URI the enrolment QR code encodes. issuer is
// repeated in the label as well as the parameter because that is what apps that
// predate the parameter read, and dropping it there makes every 2S-UI panel
// show up under the bare account name.
func TOTPKeyURI(secret, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	return "otpauth://totp/" + label + "?" + params.Encode()
}

// ValidateTOTPAfter validates a code and rejects replays: after is the counter
// the last accepted code matched, and anything at or below it is refused (RFC
// 6238 §5.2). A malformed secret fails closed. The returned counter is what the
// caller persists, so the same six digits cannot be replayed for the rest of
// their acceptance window -- which is up to 90 seconds here, long enough for a
// code read over someone's shoulder to be worth something on its own.
func ValidateTOTPAfter(secret, code string, after int64) (int64, bool) {
	return validateTOTPAt(secret, code, after, time.Now())
}

func validateTOTPAt(secret, code string, after int64, now time.Time) (int64, bool) {
	key, err := decodeTOTPSecret(secret)
	if err != nil || len(key) == 0 {
		return 0, false
	}
	code = normalizeTOTPCode(code)
	if len(code) != totpDigits {
		return 0, false
	}

	counter := uint64(now.Unix()) / uint64(totpPeriod.Seconds())
	for skew := -totpSkew; skew <= totpSkew; skew++ {
		// Guard the underflow rather than the clock: a machine whose time is
		// still at the epoch would wrap the counter into the far future and
		// accept codes from there for the rest of its uptime.
		if skew < 0 && counter < uint64(-skew) {
			continue
		}
		candidate := uint64(int64(counter) + int64(skew))
		if after > 0 && candidate <= uint64(after) {
			continue
		}
		// Constant time, though the search space here is only a million codes:
		// the rate limit is what makes guessing impractical, and this keeps the
		// comparison from being the weaker half of that pair.
		if hmac.Equal([]byte(totpCodeAt(key, candidate)), []byte(code)) {
			return int64(candidate), true
		}
	}
	return 0, false
}

// decodeTOTPSecret accepts what a user is likely to have pasted: apps display
// the secret in lowercase or in space-separated groups, and some of them pad it
// even though this one does not.
func decodeTOTPSecret(secret string) ([]byte, error) {
	secret = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(secret))
	return totpEncoding.DecodeString(strings.TrimRight(secret, "="))
}

// normalizeTOTPCode strips what phone keyboards and clipboard managers add.
// Some apps display the code as "123 456"; leaving that to fail as malformed
// reads to the user as a wrong code.
func normalizeTOTPCode(code string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code))
}

// totpCodeAt is HOTP (RFC 4226) over a time-derived counter: HMAC-SHA1, then
// the dynamic truncation that picks 31 bits starting at an offset the last
// nibble of the digest chooses.
func totpCodeAt(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, truncated%totpModulus)
}
