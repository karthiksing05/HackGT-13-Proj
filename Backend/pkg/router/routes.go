// Package router composes the middleware chain and every area package.
// Follow-on agents never edit this file: each area registers its own routes
// in its Register and replaces its 501 stubs there.
package router

import (
	"Backend/pkg/api"
	"Backend/pkg/api/auth"
	"Backend/pkg/api/checkout"
	"Backend/pkg/api/facebook"
	"Backend/pkg/api/integrations"
	"Backend/pkg/api/itineraries"
	"Backend/pkg/api/me"
	"Backend/pkg/api/photos"
	"Backend/pkg/api/planning"
	"Backend/pkg/api/social"
	"Backend/pkg/httpx"
	"Backend/pkg/middleware"
	"net/http"

	"github.com/gorilla/mux"
)

// bodyHeadroom is added to MAX_PHOTO_BYTES for multipart framing.
const bodyHeadroom = 64 << 10

// New builds the handler: recover → request log → CORS → body limit →
// routes (rate limits live in pkg/api/auth, bearer auth per route). ws
// serves GET /ws (nil answers 503 there).
func New(d *api.Deps, ws http.Handler) http.Handler {
	r := mux.NewRouter()
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "Not found.")
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusMethodNotAllowed, "Method not allowed.")
	})

	r.HandleFunc("/healthz", health(d)).Methods("GET")
	if ws != nil {
		r.Handle("/ws", ws).Methods("GET")
	} else {
		r.HandleFunc("/ws", func(w http.ResponseWriter, _ *http.Request) {
			httpx.Error(w, http.StatusServiceUnavailable, "Realtime is unavailable.")
		}).Methods("GET")
	}

	facebook.Register(r, d) // before integrations: /integrations/facebook… wins over {provider}
	auth.Register(r, d)
	me.Register(r, d)
	integrations.Register(r, d)
	photos.Register(r, d)
	itineraries.Register(r, d)
	social.Register(r, d)
	checkout.Register(r, d)
	planning.Register(r, d)

	limit := max(int64(httpx.MaxJSONBytes), d.Cfg.MaxPhotoBytes) + bodyHeadroom
	return httpx.Recover(httpx.RequestLog(middleware.CORS(httpx.BodyLimit(limit)(r))))
}

// health is GET /healthz: {"ok": true} or 503 when Mongo does not answer.
func health(d *api.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Store.Ping(r.Context()); err != nil {
			httpx.Error(w, http.StatusServiceUnavailable, "Database unavailable.")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
