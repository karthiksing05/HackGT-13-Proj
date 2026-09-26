package api

import (
	"Backend/pkg/httpx"
	"Backend/pkg/middleware"
	"Backend/pkg/store"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
)

// Sentences for store errors that reach Fail without a more specific one.
const (
	MsgNotFound  = "That's no longer available."
	MsgForbidden = "You don't have access to that."
	MsgConflict  = "That changed. Check it and try again."
	MsgEmail     = "An account with that email already exists."
	MsgUsername  = "That username is taken."
)

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

func unauthorized() *httpx.StatusError {
	return httpx.Unauthorized(middleware.SessionExpired)
}

// Fail writes any handler error: *httpx.StatusError as is, store.ErrNotFound
// → 404, ErrForbidden → 403, ErrConflict → 409 (email/username taken get
// their sentences), anything else → 500 logged with the request id.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var status *httpx.StatusError
	switch {
	case errors.As(err, &status):
		httpx.Fail(w, r, err)
	case errors.Is(err, store.ErrEmailTaken):
		httpx.Error(w, http.StatusConflict, MsgEmail)
	case errors.Is(err, store.ErrUsernameTaken):
		httpx.Error(w, http.StatusConflict, MsgUsername)
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, MsgNotFound)
	case errors.Is(err, store.ErrForbidden):
		httpx.Error(w, http.StatusForbidden, MsgForbidden)
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, http.StatusConflict, MsgConflict)
	default:
		httpx.Fail(w, r, err)
	}
}

// NotImplemented is the answer of every route an area agent has not built yet.
func NotImplemented(w http.ResponseWriter, _ *http.Request) {
	httpx.Error(w, http.StatusNotImplemented, "Not built yet")
}

// Stub registers a 501 placeholder so the route exists with the right path,
// method and auth from day one; the area agent replaces it with a handler.
func Stub(r *mux.Router, d *Deps, method, path string, protected bool) {
	var h http.Handler = http.HandlerFunc(NotImplemented)
	if protected {
		h = d.Protect(NotImplemented)
	}
	r.Handle(path, h).Methods(method)
}
