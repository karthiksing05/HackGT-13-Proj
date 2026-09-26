package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"errors"
	"net/http"
	"strings"
)

// Places: at most 10 results; the home base plus the 4 nearest without a
// query; a dropped pin names the catalog place within 150 m.
const (
	placesLimit    = 10
	placesNearby   = 4
	reverseRadiusM = 150
	droppedPin     = "Dropped pin"
)

// SearchPlaces is GET /places/search?q=&near=lat,lng → [Place] from the
// viewer's catalog: names containing q, nearest to near (else the home
// base) first; without q, the home base and the 4 places nearest to it.
func (h *H) SearchPlaces(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	query := r.URL.Query()
	near := homePoint(user)
	if s := query.Get("near"); s != "" {
		p, ok := travel.ParsePoint(s)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
			return
		}
		near = &p
	}
	q := strings.TrimSpace(query.Get("q"))
	limit := placesLimit
	home := view.HomeBase(user.HomeBase)
	if q == "" {
		limit = placesNearby + 1 // one spare: the home base may be a catalog place too
	}
	places, err := h.d.Store.Catalog().SearchPlaces(r.Context(), user.Catalog, q, near, limit)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := []contract.Place{}
	if q == "" && home != nil {
		out = append(out, *home)
	}
	nearby := 0
	for _, p := range places {
		if q == "" {
			if nearby == placesNearby || (home != nil && strings.EqualFold(p.Name, home.Name)) {
				continue
			}
			nearby++
		}
		out = append(out, contract.Place{Name: p.Name, Coordinate: &contract.Coordinate{Lat: p.Lat, Lng: p.Lng}})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// Reverse is GET /places/reverse?lat=&lng= → Place: the nearest catalog
// place within 150 m, else "Dropped pin" at the coordinate.
func (h *H) Reverse(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	pt, ok := travel.ParsePoint(query.Get("lat") + "," + query.Get("lng"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	place, err := h.d.Store.Catalog().Nearest(r.Context(), user.Catalog, pt, reverseRadiusM)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.JSON(w, http.StatusOK, contract.Place{Name: droppedPin, Coordinate: &contract.Coordinate{Lat: pt.Lat, Lng: pt.Lng}})
	case err != nil:
		api.Fail(w, r, err)
	default:
		httpx.JSON(w, http.StatusOK, contract.Place{Name: place.Name, Coordinate: &contract.Coordinate{Lat: place.Lat, Lng: place.Lng}})
	}
}
