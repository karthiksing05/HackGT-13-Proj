package httpx

import (
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// Recover turns a handler panic into a logged 500 instead of a dropped
// connection. http.ErrAbortHandler keeps its meaning.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Error().Interface("panic", rec).Str("request_id", RequestID(r)).
				Str("path", r.URL.Path).Bytes("stack", debug.Stack()).Msg("handler panic")
			Error(w, http.StatusInternalServerError, "Something went wrong on our side. Try again.")
		}()
		next.ServeHTTP(w, r)
	})
}
