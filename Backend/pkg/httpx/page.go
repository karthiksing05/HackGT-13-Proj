package httpx

import (
	"encoding/base64"
	"net/http"
	"strconv"
)

// Cursors are opaque to the app: the server encodes whatever it needs to
// resume (an id, a timestamp) as unpadded base64url.

// EncodeCursor wraps a resume token; "" stays "".
func EncodeCursor(s string) string {
	if s == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// DecodeCursor unwraps a cursor; ok is false for garbage (treat as no cursor).
func DecodeCursor(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	if len(s) > 512 {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Cursor reads ?cursor= from the request (decoded; garbage reads as none).
func Cursor(r *http.Request) string {
	c, _ := DecodeCursor(r.URL.Query().Get("cursor"))
	return c
}

// NextCursor is the next_cursor value for a page: nil when done.
func NextCursor(resume string) *string {
	if resume == "" {
		return nil
	}
	c := EncodeCursor(resume)
	return &c
}

// Limit reads ?limit= clamped to [1, max], defaulting to def.
func Limit(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
