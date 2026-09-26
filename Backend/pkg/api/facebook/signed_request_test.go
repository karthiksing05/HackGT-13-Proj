package facebook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testSecret = "sq-test-app-secret"

// knownGood was computed outside Go (Python's hmac and base64 modules) for
// testSecret and the payload
// {"algorithm":"HMAC-SHA256","issued_at":1790000000,"user_id":"10158000000000001"}.
const knownGood = "nTJCR6AGRNthPiDIiLqRKDX0QoY_SUeFeAsiPu8dft0." +
	"eyJhbGdvcml0aG0iOiJITUFDLVNIQTI1NiIsImlzc3VlZF9hdCI6MTc5MDAwMDAwMCwidXNlcl9pZCI6IjEwMTU4MDAwMDAwMDAwMDAxIn0"

func TestParseSignedRequestKnownVector(t *testing.T) {
	req, err := ParseSignedRequest(knownGood, testSecret)
	if err != nil {
		t.Fatalf("known-good vector rejected: %v", err)
	}
	if req.UserID != "10158000000000001" || req.Algorithm != "HMAC-SHA256" || !req.IssuedAt.Equal(time.Unix(1790000000, 0)) {
		t.Fatalf("parsed %+v", req)
	}
	// SignRequest produces the same bytes Facebook (and Python) would.
	signed, err := SignRequest(map[string]any{"algorithm": "HMAC-SHA256", "issued_at": 1790000000, "user_id": "10158000000000001"}, testSecret)
	if err != nil || signed != knownGood {
		t.Fatalf("SignRequest = %q, %v; want the known-good vector", signed, err)
	}
	// Padding on either part is tolerated.
	sig, payload, _ := strings.Cut(knownGood, ".")
	if _, err := ParseSignedRequest(sig+"=."+payload, testSecret); err != nil {
		t.Fatalf("padded signature rejected: %v", err)
	}
}

// sign computes a vector in the test itself, independent of SignRequest.
func sign(payloadJSON, secret string) string {
	body := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + body
}

func TestParseSignedRequestComputedVector(t *testing.T) {
	// A numeric user id and a lowercase algorithm name are accepted.
	req, err := ParseSignedRequest(sign(`{"algorithm":"hmac-sha256","user_id":42,"issued_at":1790000100}`, testSecret), testSecret)
	if err != nil || req.UserID != "42" || req.Algorithm != "HMAC-SHA256" {
		t.Fatalf("computed vector: %+v, %v", req, err)
	}
}

func TestParseSignedRequestRejects(t *testing.T) {
	valid := sign(`{"algorithm":"HMAC-SHA256","user_id":"123"}`, testSecret)
	sig, payload, _ := strings.Cut(valid, ".")
	flip := func(s string) string {
		b := []byte(s)
		if b[3] == 'A' {
			b[3] = 'B'
		} else {
			b[3] = 'A'
		}
		return string(b)
	}
	cases := map[string]struct{ raw, secret string }{
		"wrong secret":         {valid, "another-secret"},
		"no secret":            {valid, ""},
		"tampered payload":     {sig + "." + flip(payload), testSecret},
		"tampered signature":   {flip(sig) + "." + payload, testSecret},
		"signature of another": {sig + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","user_id":"999"}`)), testSecret},
		"no dot":               {sig + payload, testSecret},
		"empty":                {"", testSecret},
		"only a dot":           {".", testSecret},
		"not base64":           {"!!!." + payload, testSecret},
		"other algorithm":      {sign(`{"algorithm":"HMAC-SHA1","user_id":"123"}`, testSecret), testSecret},
		"no user id":           {sign(`{"algorithm":"HMAC-SHA256"}`, testSecret), testSecret},
		"empty user id":        {sign(`{"algorithm":"HMAC-SHA256","user_id":" "}`, testSecret), testSecret},
		"negative user id":     {sign(`{"algorithm":"HMAC-SHA256","user_id":-5}`, testSecret), testSecret},
		"fractional user id":   {sign(`{"algorithm":"HMAC-SHA256","user_id":1.5}`, testSecret), testSecret},
		"payload not JSON":     {sign(`not json`, testSecret), testSecret},
		"oversized":            {sign(`{"algorithm":"HMAC-SHA256","user_id":"1","pad":"`+strings.Repeat("x", 9000)+`"}`, testSecret), testSecret},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if req, err := ParseSignedRequest(c.raw, c.secret); err == nil {
				t.Fatalf("accepted: %+v", req)
			}
		})
	}
}
