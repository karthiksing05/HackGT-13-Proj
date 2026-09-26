// Package itineraries is sidequests, calendar days, events, transit, past
// events, ratings, insights, search and places (backend-B). Every route is
// a 501 until that agent lands.
package itineraries

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 B routes.
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "POST", "/itineraries", true)
	api.Stub(r, d, "GET", "/itineraries", true)
	api.Stub(r, d, "GET", "/itineraries/{id}", true)
	api.Stub(r, d, "PATCH", "/itineraries/{id}", true)
	api.Stub(r, d, "DELETE", "/itineraries/{id}", true)
	api.Stub(r, d, "POST", "/itineraries/{id}/leave", true)
	api.Stub(r, d, "PATCH", "/itineraries/{id}/items/{itemId}", true)
	api.Stub(r, d, "GET", "/itineraries/{id}/items/{itemId}/transit", true)
	api.Stub(r, d, "PUT", "/itineraries/{id}/items/{itemId}/transit", true)

	api.Stub(r, d, "GET", "/events/{id}", true)
	api.Stub(r, d, "PATCH", "/events/{id}", true)
	api.Stub(r, d, "GET", "/events/{id}/transit", true)
	api.Stub(r, d, "PUT", "/events/{id}/transit", true)

	api.Stub(r, d, "GET", "/calendar/days", true)
	api.Stub(r, d, "GET", "/me/past-events", true)
	api.Stub(r, d, "GET", "/me/insights", true)
	api.Stub(r, d, "PUT", "/ratings/{itemId}", true)
	api.Stub(r, d, "GET", "/search", true)
	api.Stub(r, d, "GET", "/places/search", true)
	api.Stub(r, d, "GET", "/places/reverse", true)
}
