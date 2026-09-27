// Package itineraries is sidequests, calendar days, events, transit, past
// events, ratings, insights, search, places and the must-see activity
// search (backend-B, backend-contract §4 B). Views are per viewer
// (view.go); View, Views, PublishUpdated and Leave are exported for the
// social area, which in turn installs its thread and forum-post views with
// UseSocial.
package itineraries

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 B routes, every one behind a bearer token.
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/itineraries", d.Protect(h.Create)).Methods("POST")
	r.Handle("/itineraries", d.Protect(h.List)).Methods("GET")
	r.Handle("/itineraries/{id}", d.Protect(h.Get)).Methods("GET")
	r.Handle("/itineraries/{id}", d.Protect(h.Patch)).Methods("PATCH")
	r.Handle("/itineraries/{id}", d.Protect(h.Delete)).Methods("DELETE")
	r.Handle("/itineraries/{id}/leave", d.Protect(h.LeaveHandler)).Methods("POST")
	r.Handle("/itineraries/{id}/items/{itemId}", d.Protect(h.PatchNotes)).Methods("PATCH")
	r.Handle("/itineraries/{id}/items/{itemId}/transit", d.Protect(h.TransitOptions)).Methods("GET")
	r.Handle("/itineraries/{id}/items/{itemId}/transit", d.Protect(h.SelectTransit)).Methods("PUT")

	r.Handle("/events/{id}", d.Protect(h.Event)).Methods("GET")
	r.Handle("/events/{id}", d.Protect(h.PatchNotes)).Methods("PATCH")
	r.Handle("/events/{id}/transit", d.Protect(h.TransitOptions)).Methods("GET")
	r.Handle("/events/{id}/transit", d.Protect(h.SelectTransit)).Methods("PUT")

	r.Handle("/calendar/days", d.Protect(h.CalendarDays)).Methods("GET")
	r.Handle("/me/past-events", d.Protect(h.PastEvents)).Methods("GET")
	r.Handle("/me/insights", d.Protect(h.Insights)).Methods("GET")
	r.Handle("/ratings/{itemId}", d.Protect(h.Rate)).Methods("PUT")
	r.Handle("/search", d.Protect(h.Search)).Methods("GET")
	r.Handle("/places/search", d.Protect(h.SearchPlaces)).Methods("GET")
	r.Handle("/places/reverse", d.Protect(h.Reverse)).Methods("GET")
	r.Handle("/activities/search", d.Protect(h.SearchActivities)).Methods("GET")
}
