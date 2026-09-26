package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// RandomToken returns n random bytes as unpadded base64url (refresh tokens,
// web-session tokens). crypto/rand failing is not recoverable.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("util: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomHex returns n random bytes as lowercase hex (2n characters).
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("util: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// SHA256Hex is the hex digest stored instead of a secret (refresh tokens, reset codes).
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
