// Package checkout is the simulated agent checkout: intents, the ticking
// state machine and the ticket page (backend-D: handlers.go, agent.go).
// Every route is a 501 until that agent lands.
package checkout

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 D routes.
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "POST", "/checkout/intents", true)
	api.Stub(r, d, "GET", "/checkout/intents/{id}", true)
	api.Stub(r, d, "PATCH", "/checkout/intents/{id}", true)
	api.Stub(r, d, "POST", "/checkout/intents/{id}/approve", true)
	api.Stub(r, d, "POST", "/checkout/intents/{id}/cancel", true)
	api.Stub(r, d, "GET", "/tickets/{id}", false)
}
