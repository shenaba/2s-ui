package util

import (
	"strings"
	"testing"
	"time"
)

// The seed RFC 6238 appendix B publishes its vectors against, encoded here
// rather than pasted as base32 so the test pins the algorithm and not a
// transcription of it.
var rfc6238Secret = totpEncoding.EncodeToString([]byte("12345678901234567890"))

// acceptsAt is validateTOTPAt with replay rejection switched off, which is what
// every test below except TestTOTPRejectsReplay is about.
func acceptsAt(secret, code string, now time.Time) bool {
	_, ok := validateTOTPAt(secret, code, 0, now)
	return ok
}

// RFC 6238 appendix B, SHA-1 column, truncated to the six digits this
// implementation emits. These are the numbers every other TOTP implementation
// agrees on, so passing them is what says an authenticator app will interop.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		at := time.Unix(c.unix, 0)
		if !acceptsAt(rfc6238Secret, c.code, at) {
			t.Errorf("t=%d: %s rejected, want accepted", c.unix, c.code)
		}
	}
}

// A code from the neighbouring step is accepted so a phone whose clock is off
// by a few seconds can still log in; two steps out is not, or the acceptance
// window would quietly stretch to two and a half minutes.
func TestTOTPAcceptsOneStepOfDrift(t *testing.T) {
	// Far enough from the epoch that stepping back stays positive, and not a
	// multiple of the period, so neither neighbour is the current step.
	base := time.Unix(1111111109, 0)
	code := totpCodeAt([]byte("12345678901234567890"), uint64(base.Unix())/30)

	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		if !acceptsAt(rfc6238Secret, code, base.Add(offset)) {
			t.Errorf("offset %v: rejected, want accepted", offset)
		}
	}
	for _, offset := range []time.Duration{-90 * time.Second, 90 * time.Second} {
		if acceptsAt(rfc6238Secret, code, base.Add(offset)) {
			t.Errorf("offset %v: accepted, want rejected", offset)
		}
	}
}

// Stepping back from a counter below the skew must not wrap: an unsynchronised
// clock still sitting near the epoch would otherwise validate against counters
// near 2^64 and keep doing so for its whole uptime.
func TestTOTPNearEpochDoesNotWrapCounter(t *testing.T) {
	wrapped := totpCodeAt([]byte("12345678901234567890"), ^uint64(0))
	if acceptsAt(rfc6238Secret, wrapped, time.Unix(0, 0)) {
		t.Error("a code from the wrapped counter was accepted at the epoch")
	}
	// The real code for that moment still works, so the guard skips the
	// underflowing step rather than failing the whole check.
	if !acceptsAt(rfc6238Secret, totpCodeAt([]byte("12345678901234567890"), 0), time.Unix(0, 0)) {
		t.Error("the current step was rejected at the epoch")
	}
}

// What users actually paste: apps group the digits, and the secret is shown
// lowercase or hyphenated depending on where it was copied from.
func TestTOTPNormalizesUserInput(t *testing.T) {
	at := time.Unix(59, 0)
	if !acceptsAt(rfc6238Secret, " 287 082 ", at) {
		t.Error("a space-separated code was rejected")
	}
	if !acceptsAt(strings.ToLower(rfc6238Secret), "287082", at) {
		t.Error("a lowercase secret was rejected")
	}
	if !acceptsAt(rfc6238Secret+"======", "287082", at) {
		t.Error("a padded secret was rejected")
	}
}

// Every malformed input has to fail closed. A corrupted secret is the one that
// matters: treating it as "no second factor configured" would turn a bad row
// into a bypass.
func TestTOTPRejectsMalformedInput(t *testing.T) {
	at := time.Unix(59, 0)
	cases := []struct {
		name   string
		secret string
		code   string
	}{
		{"empty secret", "", "287082"},
		{"secret is not base32", "not-a-secret!", "287082"},
		{"empty code", rfc6238Secret, ""},
		{"code too short", rfc6238Secret, "28708"},
		{"code too long", rfc6238Secret, "2870820"},
		{"wrong code", rfc6238Secret, "000000"},
	}
	for _, c := range cases {
		if acceptsAt(c.secret, c.code, at) {
			t.Errorf("%s: accepted, want rejected", c.name)
		}
	}
}

// A code stays valid for up to 90 seconds, so accepting it twice would leave
// the second factor working as a static password for whoever read it over the
// user's shoulder. The counter it matched is what the caller stores to stop that.
func TestTOTPRejectsReplay(t *testing.T) {
	at := time.Unix(1111111109, 0)

	counter, ok := validateTOTPAt(rfc6238Secret, "081804", 0, at)
	if !ok {
		t.Fatal("first use was rejected")
	}
	if counter != 1111111109/30 {
		t.Errorf("matched counter %d, want %d", counter, 1111111109/30)
	}

	if _, ok := validateTOTPAt(rfc6238Secret, "081804", counter, at); ok {
		t.Error("the same code was accepted twice")
	}
	// The neighbouring step is still in the acceptance window, and replaying
	// the older code must not lock it out.
	next := totpCodeAt([]byte("12345678901234567890"), uint64(counter)+1)
	if _, ok := validateTOTPAt(rfc6238Secret, next, counter, at); !ok {
		t.Error("the next step's code was rejected after a replay")
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	first, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(first, "=") {
		t.Errorf("secret carries padding: %q", first)
	}
	key, err := decodeTOTPSecret(first)
	if err != nil {
		t.Fatalf("decode own secret: %v", err)
	}
	if len(key) != totpSecretBytes {
		t.Errorf("secret decodes to %d bytes, want %d", len(key), totpSecretBytes)
	}

	second, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if first == second {
		t.Error("two generated secrets are identical")
	}
}

// The label carries the issuer as well as the parameter: apps that predate the
// parameter read it from there, and without it every panel enrols under the
// bare account name.
func TestTOTPKeyURI(t *testing.T) {
	uri := TOTPKeyURI("ABCD", "admin", "2S-UI")
	if !strings.HasPrefix(uri, "otpauth://totp/2S-UI:admin?") {
		t.Errorf("unexpected label: %q", uri)
	}
	if !strings.Contains(uri, "secret=ABCD") || !strings.Contains(uri, "issuer=2S-UI") {
		t.Errorf("missing parameters: %q", uri)
	}
}
