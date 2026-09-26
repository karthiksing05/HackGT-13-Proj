package facebook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// SignedRequest is the verified payload of Facebook's deauthorize and
// data-deletion callbacks.
type SignedRequest struct {
	Algorithm string
	UserID    string // the app-scoped Facebook user id
	IssuedAt  time.Time
}

// maxSignedRequest bounds what is decoded before the signature is checked.
const maxSignedRequest = 8 << 10

var errSignedRequest = errors.New("facebook: invalid signed_request")

// ParseSignedRequest verifies "<signature>.<payload>" (both base64url, with
// or without padding): the signature must be HMAC-SHA256 of the payload
// exactly as sent, keyed with the app secret, and the payload must name the
// HMAC-SHA256 algorithm and a user id.
func ParseSignedRequest(raw, appSecret string) (SignedRequest, error) {
	raw = strings.TrimSpace(raw)
	sigPart, payloadPart, ok := strings.Cut(raw, ".")
	if !ok || sigPart == "" || payloadPart == "" || appSecret == "" || len(raw) > maxSignedRequest {
		return SignedRequest{}, errSignedRequest
	}
	sig, err := decodeBase64URL(sigPart)
	if err != nil {
		return SignedRequest{}, errSignedRequest
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(payloadPart))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return SignedRequest{}, errSignedRequest
	}
	payload, err := decodeBase64URL(payloadPart)
	if err != nil {
		return SignedRequest{}, errSignedRequest
	}
	var body struct {
		Algorithm string          `json:"algorithm"`
		UserID    json.RawMessage `json:"user_id"`
		IssuedAt  int64           `json:"issued_at"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || !strings.EqualFold(body.Algorithm, "HMAC-SHA256") {
		return SignedRequest{}, errSignedRequest
	}
	userID := jsonID(body.UserID)
	if userID == "" {
		return SignedRequest{}, errSignedRequest
	}
	req := SignedRequest{Algorithm: strings.ToUpper(body.Algorithm), UserID: userID}
	if body.IssuedAt > 0 {
		req.IssuedAt = time.Unix(body.IssuedAt, 0).UTC()
	}
	return req, nil
}

// SignRequest builds a signed_request the way Facebook does (tests and local
// tooling).
func SignRequest(payload any, appSecret string) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + body, nil
}

func decodeBase64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// jsonID reads an id sent as a JSON string or number.
func jsonID(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if _, err := strconv.ParseUint(n.String(), 10, 64); err == nil {
			return n.String()
		}
	}
	return ""
}
