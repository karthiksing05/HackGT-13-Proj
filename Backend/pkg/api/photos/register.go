// Package photos serves stored photo bytes publicly (serve.go) and reads
// photo uploads for every area that accepts them (upload.go).
package photos

import (
	"Backend/pkg/api"
	"net/http"

	"github.com/gorilla/mux"
)

type H struct{ d *api.Deps }

// Register mounts GET /photos/{id} (public, unguessable id, immutable cache).
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/photos/{id}", http.HandlerFunc(h.Serve)).Methods("GET", "HEAD")
}
