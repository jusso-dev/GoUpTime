package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func NewRawKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return "upt_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func Matches(raw, hash string) bool {
	candidate := Hash(raw)
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(hash)) == 1
}
