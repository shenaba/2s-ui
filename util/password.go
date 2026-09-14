package util

import (
	"crypto/subtle"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// dummyHash is a valid bcrypt hash of a value nobody can guess. BurnPasswordCheck
// compares against it so a login for an unknown username costs the same as one
// for a known username with the wrong password.
const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

func IsHashedPassword(stored string) bool {
	return strings.HasPrefix(stored, "$2")
}

func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(plain, stored string) bool {
	if IsHashedPassword(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil
	}
	// Constant time for the legacy plaintext rows too: == returns on the first
	// differing byte.
	return subtle.ConstantTimeCompare([]byte(plain), []byte(stored)) == 1
}

// BurnPasswordCheck spends the work CheckPassword would and always fails. Call
// it on the no-such-user path, where returning early makes an unknown username
// answer a whole bcrypt round faster than a known one and so enumerable.
func BurnPasswordCheck(plain string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(plain))
}
