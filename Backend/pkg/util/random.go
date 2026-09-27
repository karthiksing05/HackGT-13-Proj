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
	return base64.RawURLEncoding.EncodeToString(randomBytes(n))
}

// RandomHex returns n random bytes as lowercase hex (2n characters).
func RandomHex(n int) string {
	return hex.EncodeToString(randomBytes(n))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("util: crypto/rand failed: " + err.Error())
	}
	return b
}

// SHA256Hex is the hex digest stored instead of a secret (refresh tokens, reset codes).
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
