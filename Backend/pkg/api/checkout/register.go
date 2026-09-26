// Package checkout is the agent checkout (backend-contract §4 D): intents
// persisted in checkout_intents, the agent that moves them through their
// timed states (agent.go), the five endpoints (handlers.go) and the public
// ticket page (ticket.go).
//
// Simulated: nothing is ever bought or charged. The agent only advances the
// stored intent on a timer, and a booking writes a demo ticket that says so.
package checkout

import (
	"Backend/pkg/api"
	"net/http"

	"github.com/gorilla/mux"
)

// H holds the handlers.
type H struct{ d *api.Deps }

// Register mounts the §4 D routes.
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/checkout/intents", d.Protect(h.Create)).Methods("POST")
	r.Handle("/checkout/intents/{id}", d.Protect(h.Get)).Methods("GET")
	r.Handle("/checkout/intents/{id}", d.Protect(h.Patch)).Methods("PATCH")
	r.Handle("/checkout/intents/{id}/approve", d.Protect(h.Approve)).Methods("POST")
	r.Handle("/checkout/intents/{id}/cancel", d.Protect(h.Cancel)).Methods("POST")
	r.Handle("/tickets/{id}", http.HandlerFunc(h.Ticket)).Methods("GET")
}
