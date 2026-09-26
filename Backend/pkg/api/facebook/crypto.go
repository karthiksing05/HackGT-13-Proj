package facebook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Facebook user tokens are sealed with AES-256-GCM before they reach Mongo
// (facebook_accounts.accessTokenEnc). The key is FB_TOKEN_KEY (64 hex
// characters) or, when that is unset, HKDF-SHA256 of JWT_SECRET. Rotating
// either makes stored tokens unreadable; those connections then ask the
// person to sign in to Facebook again instead of failing.

const (
	sealVersion = "v1."
	hkdfSalt    = "sidequestz/facebook"
	hkdfInfo    = "access-token-key/v1"
)

var errSealed = errors.New("facebook: sealed token unreadable")

// tokenBox seals and opens tokens; each ciphertext is bound to its user id
// (additional data), so a sealed token copied to another user never opens.
type tokenBox struct{ aead cipher.AEAD }

// tokenKey is FB_TOKEN_KEY decoded, or the HKDF default from JWT_SECRET.
func tokenKey(hexKey, jwtSecret string) ([]byte, error) {
	if hexKey = strings.TrimSpace(hexKey); hexKey != "" {
		key, err := hex.DecodeString(hexKey)
		if err != nil || len(key) != 32 {
			return nil, errors.New("FB_TOKEN_KEY must be 64 hex characters (32 bytes)")
		}
		return key, nil
	}
	if jwtSecret == "" {
		return nil, errors.New("FB_TOKEN_KEY or JWT_SECRET is required to store Facebook tokens")
	}
	return hkdf.Key(sha256.New, []byte(jwtSecret), []byte(hkdfSalt), hkdfInfo, 32)
}

func newTokenBox(hexKey, jwtSecret string) (*tokenBox, error) {
	key, err := tokenKey(hexKey, jwtSecret)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &tokenBox{aead: aead}, nil
}

func sealedFor(userID string) []byte { return []byte("facebook_accounts.accessTokenEnc|" + userID) }

// seal returns "v1." + base64url(nonce ‖ ciphertext ‖ tag).
func (b *tokenBox) seal(userID, token string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(token)+b.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(token), sealedFor(userID))
	return sealVersion + base64.RawURLEncoding.EncodeToString(out), nil
}

// open reverses seal; a wrong key, user or tampered value is errSealed.
func (b *tokenBox) open(userID, sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, sealVersion)
	if !ok {
		return "", errSealed
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) < b.aead.NonceSize()+b.aead.Overhead() {
		return "", errSealed
	}
	nonce, body := data[:b.aead.NonceSize()], data[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, body, sealedFor(userID))
	if err != nil {
		return "", errSealed
	}
	return string(plain), nil
}
