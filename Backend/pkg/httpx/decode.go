package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxJSONBytes caps every JSON request body (config MAX_JSON_BYTES mirrors it).
const MaxJSONBytes = 1 << 20

// Decode reads a JSON body into v (1 MB cap, unknown keys allowed). On failure
// it writes 400 "Check the details and try again." (413 when too large) and
// returns false; the handler just returns.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decode(w, r, v, false)
}

// DecodeOptional is Decode for bodies that may be empty (POST /auth/logout,
// join requests): an empty body leaves v untouched and returns true.
func DecodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	return decode(w, r, v, true)
}

func decode(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	if r.Body == nil {
		if optional {
			return true
		}
		Error(w, http.StatusBadRequest, GenericBadRequest)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBytes)
	err := json.NewDecoder(r.Body).Decode(v)
	switch {
	case err == nil:
		return true
	case errors.Is(err, io.EOF) && optional:
		return true
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		Error(w, http.StatusRequestEntityTooLarge, "That's too big to send.")
		return false
	}
	Error(w, http.StatusBadRequest, GenericBadRequest)
	return false
}

// BodyLimit caps every request body at n bytes (photo uploads set the ceiling;
// Decode applies the tighter JSON cap).
func BodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
