package middleware

import (
	"Backend/pkg/httpx"
	"net/http"
)

// ErrorResponse is the {error, message} body; httpx.ErrorBody is the same type.
type ErrorResponse = httpx.ErrorBody

// WriteJSON is kept for older call sites; new code uses httpx.JSON.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	if data == nil {
		w.WriteHeader(status)
		return
	}
	httpx.JSON(w, status, data)
}

// WriteError is kept for older call sites; new code uses httpx.Error.
func WriteError(w http.ResponseWriter, status int, message string) {
	httpx.Error(w, status, message)
}
