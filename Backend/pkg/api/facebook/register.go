// Package facebook is the Graph API connector (backend-E: handlers.go,
// graph.go, crypto.go, mapping.go, signed_request.go). Every route is a 501
// until that agent lands. The router registers this package before
// integrations so /integrations/facebook… is matched here.
package facebook

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 E routes.
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "GET", "/integrations/facebook", true)
	api.Stub(r, d, "POST", "/integrations/facebook/connect", true)
	api.Stub(r, d, "GET", "/integrations/facebook/callback", false)
	api.Stub(r, d, "POST", "/integrations/facebook/import", true)
	api.Stub(r, d, "DELETE", "/integrations/facebook", true)
	api.Stub(r, d, "POST", "/integrations/facebook/deauthorize", false)
	api.Stub(r, d, "POST", "/integrations/facebook/data-deletion", false)
	api.Stub(r, d, "GET", "/integrations/facebook/deletion-status", false)
}
