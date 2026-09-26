// Package integrations is the calendar connect stub, the hosted pages and
// payment methods (backend-A: calendar.go, payments.go, pages.go). Every
// route below is a 501 until that agent lands.
package integrations

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 A integration and payment routes. The calendar
// provider is constrained so /integrations/facebook (package facebook,
// registered earlier) never matches here.
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "GET", "/integrations", true)
	api.Stub(r, d, "POST", "/integrations/{provider:google|outlook}/connect", true)
	api.Stub(r, d, "GET", "/integrations/{provider:google|outlook}/start", false)
	api.Stub(r, d, "DELETE", "/integrations/{provider:google|outlook}", true)

	api.Stub(r, d, "GET", "/me/payment-methods", true)
	api.Stub(r, d, "POST", "/me/payment-methods/setup", true)
	api.Stub(r, d, "POST", "/me/payment-methods", true)
	api.Stub(r, d, "DELETE", "/me/payment-methods/{id}", true)
	api.Stub(r, d, "GET", "/pay/setup", false)
	api.Stub(r, d, "POST", "/pay/setup", false)
}
