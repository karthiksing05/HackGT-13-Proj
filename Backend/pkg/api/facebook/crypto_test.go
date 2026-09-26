package facebook

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const testJWTSecret = "test-secret-test-secret-test-secret-0000"

func TestTokenKeySources(t *testing.T) {
	// The HKDF default, checked against a vector computed outside Go
	// (RFC 5869 with Python's hmac: salt "sidequestz/facebook", info
	// "access-token-key/v1").
	key, err := tokenKey("", testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key); got != "863021e94f09b63bf2b4af6ed366b77c7fac9a154aa0a2071a1ea9de3d14f339" {
		t.Fatalf("HKDF key = %s", got)
	}
	other, err := tokenKey("", testJWTSecret+"x")
	if err != nil || bytes.Equal(other, key) {
		t.Fatal("another JWT secret must give another key")
	}

	// FB_TOKEN_KEY wins when set.
	hexKey := strings.Repeat("ab", 32)
	key, err = tokenKey(" "+hexKey+" ", testJWTSecret)
	if err != nil || hex.EncodeToString(key) != hexKey {
		t.Fatalf("FB_TOKEN_KEY not used: %x, %v", key, err)
	}
	for _, bad := range []string{"abcd", strings.Repeat("zz", 32), strings.Repeat("ab", 31), strings.Repeat("ab", 33)} {
		if _, err := tokenKey(bad, testJWTSecret); err == nil || !strings.Contains(err.Error(), "FB_TOKEN_KEY") {
			t.Errorf("FB_TOKEN_KEY %q accepted: %v", bad, err)
		}
	}
	if _, err := tokenKey("", ""); err == nil {
		t.Fatal("no key material accepted")
	}
}

func TestTokenBoxSealsPerUser(t *testing.T) {
	box, err := newTokenBox("", testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	const token = "EAAlong-token-value"
	a, err := box.seal("user-a", token)
	if err != nil {
		t.Fatal(err)
	}
	b, err := box.seal("user-a", token)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, token) || !strings.HasPrefix(a, "v1.") {
		t.Fatalf("sealed values must be fresh, prefixed and opaque: %q %q", a, b)
	}
	if got, err := box.open("user-a", a); err != nil || got != token {
		t.Fatalf("open = %q, %v", got, err)
	}
	// Bound to the user: another user's document never opens it.
	if _, err := box.open("user-b", a); err == nil {
		t.Fatal("token opened for another user")
	}
	// Another key (FB_TOKEN_KEY rotated) fails cleanly.
	rotated, err := newTokenBox(strings.Repeat("01", 32), testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.open("user-a", a); err == nil {
		t.Fatal("opened with another key")
	}
	// Tampering, truncation and foreign formats are rejected.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(a, "v1."))
	raw[len(raw)-1] ^= 1
	for name, sealed := range map[string]string{
		"tampered":  "v1." + base64.RawURLEncoding.EncodeToString(raw),
		"truncated": a[:10],
		"no prefix": strings.TrimPrefix(a, "v1."),
		"plaintext": token,
		"empty":     "",
	} {
		if _, err := box.open("user-a", sealed); err == nil {
			t.Errorf("%s value opened", name)
		}
	}
}
