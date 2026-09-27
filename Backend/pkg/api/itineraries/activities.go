package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/httpx"
	"Backend/pkg/planner"
	"Backend/pkg/travel"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// The must-see search: 20 results unless asked, never more than 50; the
// catalog read stops at activitiesFetch of each kind before the pick rules
// and the ranking.
const (
	activitiesLimit    = 20
	activitiesMaxLimit = 50
	activitiesFetch    = 500
)

// Sentences for a malformed must-see search.
const (
	msgBadNear  = "Check the location and try again."
	msgBadDate  = "Check the date and try again."
	msgBadLimit = "Check how many results to show and try again."
)

// SearchActivities is GET /activities/search?q=&near=lat,lng&date=YYYY-MM-DD&limit=
// → [ActivityHit]: Create's must-see search over the viewer's catalog, on
// the plan's local day in X-Time-Zone (today, in the account's business
// time, without a date). What can be a pick, and the ranking, are the
// planner's (planner.ActivityHits): events on that day and not over, places
// open that day, nothing the viewer's age rules out. A limit above 50 is 50.
func (h *H) SearchActivities(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var near *travel.Point
	if s := query.Get("near"); s != "" {
		p, ok := travel.ParsePoint(s)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, msgBadNear)
			return
		}
		near = &p
	}
	limit := activitiesLimit
	if s := query.Get("limit"); s != "" {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 {
			httpx.Error(w, http.StatusBadRequest, msgBadLimit)
			return
		}
		limit = min(n, activitiesMaxLimit)
	}
	tz := httpx.TZ(r)
	now := h.d.BusinessNow(r.Context())
	day := httpx.LocalMidnight(now, tz)
	if s := query.Get("date"); s != "" {
		d, err := httpx.ParseDay(s, tz)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, msgBadDate)
			return
		}
		day = d
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	q := strings.TrimSpace(query.Get("q"))
	from, to := day, day.AddDate(0, 0, 1)
	acts, err := h.d.Store.Catalog().SearchActivities(r.Context(), user.Catalog, q, from, to, activitiesFetch)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, planner.ActivityHits(acts, planner.HitSearch{
		Query: q, Near: near, From: from, To: to, Now: now, TZ: tz,
		AgeBracket: string(view.AgeBracket(user.BirthDate, now)), Limit: limit,
	}))
}

// Activity is GET /activities/{id}?date=YYYY-MM-DD → ActivityDetail: one
// event or place of the viewer's catalog, for Create › Review's stop pane.
// Like the must-see search it reads only the viewer's catalog, so an id
// that is malformed, unknown or in the other catalog is a 404. date is the
// plan's day in X-Time-Zone (today, in the account's business time, without
// one); only the opening-hours line reads it.
func (h *H) Activity(w http.ResponseWriter, r *http.Request) {
	tz := httpx.TZ(r)
	day := httpx.LocalMidnight(h.d.BusinessNow(r.Context()), tz)
	if s := r.URL.Query().Get("date"); s != "" {
		d, err := httpx.ParseDay(s, tz)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, msgBadDate)
			return
		}
		day = d
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	act, err := h.d.Store.Catalog().Activity(r.Context(), user.Catalog, mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, planner.ActivityDetail(act, day))
}
