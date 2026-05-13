// Package auth handles API key generation, hashing, and verification.
//
// Keys carry 256 bits of entropy from crypto/rand and are encoded with
// base64url so they're URL-safe and copy-pasteable. Only the SHA-256 digest
// is persisted; the plaintext is shown to the user exactly once at creation.
// Because the key is high-entropy, plain SHA-256 (no bcrypt/argon2) is
// sufficient: an attacker who steals the hash cannot feasibly brute-force a
// 256-bit space.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// keyPrefix is prepended to every raw key so logs and audits can identify
// our keys at a glance without exposing the secret material.
const keyPrefix = "upt_"

// rawKeyBytes is the size, in bytes, of the random portion of a key. 32
// bytes = 256 bits, the same security level as SHA-256 itself.
const rawKeyBytes = 32

// NewRawKey returns a fresh, single-use API key. The returned value is the
// secret — callers must show it to the user once and never store it.
func NewRawKey() (string, error) {
	buf := make([]byte, rawKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash returns the hex-encoded SHA-256 digest of raw. Safe to call on
// untrusted input; result is deterministic for use as a lookup key.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Matches verifies a raw key against a stored hash in constant time. Use
// this instead of comparing Hash(raw) to hash directly to avoid timing
// leaks.
func Matches(raw, hash string) bool {
	if raw == "" || hash == "" {
		return false
	}
	candidate := Hash(raw)
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(hash)) == 1
}

// ErrInvalidKeyFormat is returned by Preview for inputs that do not match
// the expected `upt_<...>` shape.
var ErrInvalidKeyFormat = errors.New("invalid api key format")

// Preview returns a short, non-secret label suitable for display in UIs and
// audit logs ("upt_abcd…"). It never returns enough information to use the
// key for authentication.
func Preview(raw string) (string, error) {
	if len(raw) < len(keyPrefix)+8 {
		return "", ErrInvalidKeyFormat
	}
	if raw[:len(keyPrefix)] != keyPrefix {
		return "", ErrInvalidKeyFormat
	}
	return raw[:len(keyPrefix)+4] + "…", nil
}
