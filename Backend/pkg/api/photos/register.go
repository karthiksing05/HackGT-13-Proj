// Package photos serves stored photo bytes publicly (backend-A: serve.go).
package photos

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts GET /photos/{id} (public, unguessable id, immutable cache).
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "GET", "/photos/{id}", false)
}
