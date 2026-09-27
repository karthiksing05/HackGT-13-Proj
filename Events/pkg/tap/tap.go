package tap

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMissingSignature = errors.New("missing signature headers")
	ErrBadSignature     = errors.New("bad signature")
	ErrExpired          = errors.New("signature expired")
	ErrReplayedNonce    = errors.New("nonce already used")
	ErrWrongAuthority   = errors.New("wrong authority")
	ErrWrongTag         = errors.New("wrong signature tag")
	ErrUnknownKey       = errors.New("unknown key id")
	ErrBadParams        = errors.New("malformed signature parameters")
)

// NonceStore checks and records single-use nonces.
type NonceStore interface {
	CheckAndRecordNonce(nonce string, expiresAt time.Time) error
}

// KeyDirectory resolves an Ed25519 public key by key ID.
type KeyDirectory interface {
	GetPublicKey(keyID string) (ed25519.PublicKey, error)
}

// ParsedInput contains the parsed fields of an RFC 9421 signature-input.
type ParsedInput struct {
	Label     string
	Created   int64
	Expires   int64
	KeyID     string
	Alg       string
	Nonce     string
	Tag       string
	RawParams string
}

// Sign signs an HTTP request according to the Visa Trusted Agent Protocol (TAP / RFC 9421).
func Sign(req *http.Request, privKey ed25519.PrivateKey, keyID string, tag string, authority string, now time.Time) error {
	if authority == "" {
		authority = req.Host
	}
	path := req.URL.Path
	if path == "" {
		path = "/"
	}

	created := now.Unix()
	expires := now.Add(5 * time.Minute).Unix()
	nonce := "nonce_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	sigParams := fmt.Sprintf(`("@authority" "@path");created=%d;expires=%d;keyid="%s";alg="ed25519";nonce="%s";tag="%s"`,
		created, expires, keyID, nonce, tag)

	sigBase := fmt.Sprintf("\"@authority\": %s\n\"@path\": %s\n\"@signature-params\": %s",
		authority, path, sigParams)

	sigBytes := ed25519.Sign(privKey, []byte(sigBase))
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	req.Header.Set("Signature-Input", "sig2="+sigParams)
	req.Header.Set("Signature", "sig2=:"+sigB64+":")
	return nil
}

// Verify validates the RFC 9421 TAP HTTP message signature on a request.
func Verify(req *http.Request, keyDir KeyDirectory, nonces NonceStore, expectedTag string, expectedAuthority string, now time.Time) (*ParsedInput, error) {
	sigInputHeader := req.Header.Get("Signature-Input")
	sigHeader := req.Header.Get("Signature")
	if sigInputHeader == "" || sigHeader == "" {
		return nil, ErrMissingSignature
	}

	parsed, err := parseSignatureInput(sigInputHeader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadParams, err)
	}

	// 1. Tag check: must match required route tag (e.g. agent-browser-auth or agent-payer-auth)
	if expectedTag != "" && parsed.Tag != expectedTag {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrWrongTag, parsed.Tag, expectedTag)
	}

	// 2. Timestamp check: expires must be <= created + 8 minutes
	maxTTL := int64(8 * 60)
	if parsed.Expires-parsed.Created > maxTTL {
		return nil, fmt.Errorf("%w: TTL exceeds 8 minutes", ErrExpired)
	}
	nowSec := now.Unix()
	// Allow 60s clock skew
	if nowSec < parsed.Created-60 || nowSec > parsed.Expires+60 {
		return nil, ErrExpired
	}

	// 3. Authority check: matches MERCHANT_HOST or dev host
	reqHost := req.Host
	if expectedAuthority != "" {
		// Clean ports if checking hostname, but tolerate exact match or host match
		normExpected := stripPort(expectedAuthority)
		normReqHost := stripPort(reqHost)
		if normReqHost != normExpected && normReqHost != "localhost" && normReqHost != "127.0.0.1" && reqHost != expectedAuthority {
			return nil, fmt.Errorf("%w: got %q, expected %q", ErrWrongAuthority, reqHost, expectedAuthority)
		}
	}

	// 5. Extract signature bytes
	sigBytes, err := extractSignatureBytes(sigHeader, parsed.Label)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadSignature, err)
	}

	// 6. Look up public key
	pubKey, err := keyDir.GetPublicKey(parsed.KeyID)
	if err != nil || pubKey == nil {
		return nil, fmt.Errorf("%w: key %q", ErrUnknownKey, parsed.KeyID)
	}

	// 7. Reconstruct signature base
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	authority := req.Host
	if authority == "" {
		authority = expectedAuthority
	}

	sigBase := fmt.Sprintf("\"@authority\": %s\n\"@path\": %s\n\"@signature-params\": %s",
		authority, path, parsed.RawParams)

	valid := ed25519.Verify(pubKey, []byte(sigBase), sigBytes)
	if !valid && expectedAuthority != "" && authority != expectedAuthority {
		// Also try with expectedAuthority if req.Host had a port difference
		altBase := fmt.Sprintf("\"@authority\": %s\n\"@path\": %s\n\"@signature-params\": %s",
			expectedAuthority, path, parsed.RawParams)
		valid = ed25519.Verify(pubKey, []byte(altBase), sigBytes)
	}
	if !valid {
		return nil, ErrBadSignature
	}

	// 8. Nonce check: single-use. Recorded only for a valid signature, so
	// unsigned junk can't burn nonces.
	if parsed.Nonce == "" {
		return nil, fmt.Errorf("%w: missing nonce", ErrBadParams)
	}
	if nonces != nil {
		if err := nonces.CheckAndRecordNonce(parsed.Nonce, time.Unix(parsed.Expires, 0)); err != nil {
			return nil, ErrReplayedNonce
		}
	}
	return parsed, nil
}

func parseSignatureInput(header string) (*ParsedInput, error) {
	// Example: sig2=("@authority" "@path");created=123;expires=456;keyid="sqz-agent-1";alg="ed25519";nonce="...";tag="agent-browser-auth"
	eqIdx := strings.Index(header, "=")
	if eqIdx == -1 {
		return nil, errors.New("no label assignment in Signature-Input")
	}
	label := strings.TrimSpace(header[:eqIdx])
	paramsStr := strings.TrimSpace(header[eqIdx+1:])

	parsed := &ParsedInput{
		Label:     label,
		RawParams: paramsStr,
	}

	parts := strings.Split(paramsStr, ";")
	for _, part := range parts[1:] { // First part is ("@authority" "@path")
		part = strings.TrimSpace(part)
		subEq := strings.Index(part, "=")
		if subEq == -1 {
			continue
		}
		k := strings.TrimSpace(part[:subEq])
		v := strings.Trim(strings.TrimSpace(part[subEq+1:]), "\"")

		switch k {
		case "created":
			val, _ := strconv.ParseInt(v, 10, 64)
			parsed.Created = val
		case "expires":
			val, _ := strconv.ParseInt(v, 10, 64)
			parsed.Expires = val
		case "keyid":
			parsed.KeyID = v
		case "alg":
			parsed.Alg = v
		case "nonce":
			parsed.Nonce = v
		case "tag":
			parsed.Tag = v
		}
	}

	if parsed.Created == 0 || parsed.Expires == 0 {
		return nil, errors.New("missing created or expires parameters")
	}
	return parsed, nil
}

func extractSignatureBytes(sigHeader, label string) ([]byte, error) {
	// Look for <label>=:<base64>: or <label>=<base64>
	var targetVal string
	entries := strings.Split(sigHeader, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		eqIdx := strings.Index(entry, "=")
		if eqIdx == -1 {
			continue
		}
		curLabel := strings.TrimSpace(entry[:eqIdx])
		if curLabel == label {
			targetVal = strings.TrimSpace(entry[eqIdx+1:])
			break
		}
	}
	if targetVal == "" {
		// Fallback: if no comma separated match, check direct prefix
		eqIdx := strings.Index(sigHeader, "=")
		if eqIdx != -1 {
			targetVal = strings.TrimSpace(sigHeader[eqIdx+1:])
		} else {
			targetVal = sigHeader
		}
	}

	// Strip outer colons :<b64>: if present per RFC 9421 byte sequence
	targetVal = strings.TrimPrefix(targetVal, ":")
	targetVal = strings.TrimSuffix(targetVal, ":")

	return base64.StdEncoding.DecodeString(targetVal)
}

func stripPort(host string) string {
	if idx := strings.Index(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}
