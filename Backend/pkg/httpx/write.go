// Package httpx holds the HTTP plumbing shared by every handler package:
// JSON decoding with a size cap, response writers with the app's error shape,
// the X-Time-Zone header, cursors, formatting helpers, rate limiting, panic
// recovery and request logging.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// StatusError is a status plus the sentence the app shows. Handlers return it
// from helpers and pass it to Fail; anything else that reaches Fail is a 500.
type StatusError struct {
	Status  int
	Message string
	Err     error // optional cause for the log line
}

func (e *StatusError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *StatusError) Unwrap() error { return e.Err }

// E builds a StatusError.
func E(status int, message string) *StatusError {
	return &StatusError{Status: status, Message: message}
}

// Wrap attaches a cause to a StatusError for the log line without changing the message.
func Wrap(status int, message string, err error) *StatusError {
	return &StatusError{Status: status, Message: message, Err: err}
}

// GenericBadRequest is the sentence for malformed or unreadable JSON.
const GenericBadRequest = "Check the details and try again."

func BadRequest(message string) *StatusError   { return E(http.StatusBadRequest, message) }
func Unauthorized(message string) *StatusError { return E(http.StatusUnauthorized, message) }
func Forbidden(message string) *StatusError    { return E(http.StatusForbidden, message) }
func NotFound(message string) *StatusError     { return E(http.StatusNotFound, message) }
func Conflict(message string) *StatusError     { return E(http.StatusConflict, message) }

// ErrorBody is the wire shape of every non-2xx response: {"error": "<code>", "message": "<sentence>"}.
type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// JSON writes v with the given status. A nil v writes "null"; use NoContent for 204.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn().Err(err).Msg("write json")
	}
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Error writes {"error": code, "message": message}; code is the snake_case
// status text ("not_found", "not_implemented").
func Error(w http.ResponseWriter, status int, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	JSON(w, status, ErrorBody{Error: Code(status), Message: message})
}

// Code is the snake_case form of the status text, the "error" key of ErrorBody.
func Code(status int) string {
	return strings.ToLower(strings.ReplaceAll(http.StatusText(status), " ", "_"))
}

// Fail writes err: a *StatusError with its status and sentence, a *MaxBytesError
// as 413, anything else as a 500 logged with the request id. Store errors are
// mapped by api.Fail before they get here.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *StatusError
	if errors.As(err, &e) {
		if e.Status >= 500 {
			log.Error().Err(err).Str("request_id", RequestID(r)).Str("path", r.URL.Path).Msg("request failed")
		}
		Error(w, e.Status, e.Message)
		return
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		Error(w, http.StatusRequestEntityTooLarge, "That's too big to send.")
		return
	}
	log.Error().Err(err).Str("request_id", RequestID(r)).Str("path", r.URL.Path).Msg("request failed")
	Error(w, http.StatusInternalServerError, "Something went wrong on our side. Try again.")
}
