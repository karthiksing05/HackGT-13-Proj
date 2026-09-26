// Package integrations is the calendar connect (calendar.go), saved cards
// (payments.go) and the hosted pages the app opens for both in its web
// sheet (pages.go). Both are simulated on purpose (docs/ROADMAP.md): no
// calendar provider is contacted and no card is charged or stored in full.
package integrations

import (
	"Backend/pkg/api"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type H struct{ d *api.Deps }

// pageTTL is how long a hosted-page link (?t=) works; each works once.
const pageTTL = 15 * time.Minute

// Register mounts the §4 A integration and payment routes. The calendar
// provider is constrained so /integrations/facebook (package facebook,
// registered earlier) never matches here.
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/integrations", d.Protect(h.List)).Methods("GET")
	r.Handle("/integrations/{provider:google|outlook}/connect", d.Protect(h.Connect)).Methods("POST")
	r.Handle("/integrations/{provider:google|outlook}/start", http.HandlerFunc(h.Start)).Methods("GET")
	r.Handle("/integrations/{provider:google|outlook}", d.Protect(h.Disconnect)).Methods("DELETE")

	r.Handle("/me/payment-methods", d.Protect(h.ListCards)).Methods("GET")
	r.Handle("/me/payment-methods/setup", d.Protect(h.CardSetup)).Methods("POST")
	r.Handle("/me/payment-methods", d.Protect(h.AddCard)).Methods("POST")
	r.Handle("/me/payment-methods/{id}", d.Protect(h.DeleteCard)).Methods("DELETE")
	r.Handle("/pay/setup", http.HandlerFunc(h.CardPage)).Methods("GET")
	r.Handle("/pay/setup", http.HandlerFunc(h.SubmitCardPage)).Methods("POST")
}
