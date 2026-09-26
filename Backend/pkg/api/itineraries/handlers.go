package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Sentences for host/member rules.
const (
	MsgHostOnly     = "Only the host can edit this sidequest."
	MsgMemberDelete = "You joined this sidequest, so you can leave it but not delete it."
	MsgHostLeave    = "You host this sidequest. Delete it instead."
	MsgNeedTitle    = "Give your sidequest a name."
)

// maxList caps GET /itineraries.
const maxList = 100

type H struct{ d *api.Deps }

// List is GET /itineraries?status=active (the default) or past → [Itinerary]:
// the viewer's plans, soonest first (past: most recent first).
func (h *H) List(w http.ResponseWriter, r *http.Request) {
	ctx, uid, now := r.Context(), api.UserID(r), h.d.Clock()
	var docs []*models.Itinerary
	var err error
	switch r.URL.Query().Get("status") {
	case "", models.ItineraryActive:
		docs, err = h.d.Store.Itineraries().ListActive(ctx, uid, now, maxList)
	case models.ItineraryPast:
		docs, err = h.d.Store.Itineraries().ListPast(ctx, uid, now, maxList)
	default:
		httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := Views(ctx, h.d, docs, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// Get is GET /itineraries/{id} → Itinerary (members only; 404 otherwise).
func (h *H) Get(w http.ResponseWriter, r *http.Request) {
	uid := api.UserID(r)
	it, err := h.d.Store.Itineraries().ForMember(r.Context(), mux.Vars(r)["id"], uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := View(r.Context(), h.d, it, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// Patch is PATCH /itineraries/{id} (ItineraryUpdate) → Itinerary. Host only
// (a member gets 403). A new date moves the plan by whole days in the
// request's time zone; a new date, start, back-by or stop order re-times
// it. Every member hears itinerary.updated; the forum refreshes when the
// plan is or was posted there.
func (h *H) Patch(w http.ResponseWriter, r *http.Request) {
	var req contract.ItineraryUpdate
	if !httpx.Decode(w, r, &req) {
		return
	}
	ctx, uid := r.Context(), api.UserID(r)
	it, err := h.d.Store.Itineraries().ForMember(ctx, mux.Vars(r)["id"], uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if it.HostID != uid {
		api.Fail(w, r, httpx.Forbidden(MsgHostOnly))
		return
	}
	set, err := edit(it, req, httpx.TZ(r), h.d.Clock())
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	updated, err := h.d.Store.Itineraries().UpdateHost(ctx, it.ID, uid, set)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	PublishUpdated(ctx, h.d, updated, updated.MemberIDs)
	if it.Visibility != models.VisibilityJustMe || updated.Visibility != models.VisibilityJustMe {
		realtime.ForumUpdate(h.d.Publish())
	}
	out, err := View(ctx, h.d, updated, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// edit applies an update to a copy of it (the mock server's
// updateItinerary) and returns the $set that stores the result.
func edit(it *models.Itinerary, req contract.ItineraryUpdate, tz *time.Location, now time.Time) (bson.M, error) {
	set := bson.M{}
	next := *it
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, httpx.BadRequest(MsgNeedTitle)
		}
		set["title"] = title
	}
	if req.Visibility != nil {
		if !req.Visibility.Valid() {
			return nil, httpx.BadRequest(httpx.GenericBadRequest)
		}
		set["visibility"] = string(*req.Visibility)
	}
	if req.Date != nil {
		// A different day moves the plan there (joining locks with it);
		// calendar blocks belonged to the old day.
		oldDay := it.Date.In(tz)
		if day, err := httpx.ParseDay(it.DateKey, tz); err == nil {
			oldDay = day
		}
		y, m, d := localDay(req.Date.Time, tz)
		newDay := time.Date(y, m, d, 0, 0, 0, 0, tz)
		if days := daysBetween(oldDay, newDay); days != 0 {
			shift := func(t time.Time) time.Time { return t.In(tz).AddDate(0, 0, days) }
			next.Start, next.BackBy = shift(it.Start), shift(it.BackBy)
			items := make([]models.ItineraryItem, 0, len(it.Items))
			for _, item := range it.Items {
				if item.Kind == kindBusy {
					continue
				}
				item.Start, item.End = shift(item.Start), shift(item.End)
				items = append(items, item)
			}
			next.Items = items
			if it.LockAt != nil {
				set["lockAt"] = shift(*it.LockAt)
			}
			set["date"] = newDay
			set["dateKey"] = newDay.Format("2006-01-02")
			set["tz"] = tz.String()
		}
	}
	if req.Start != nil {
		next.Start = req.Start.Time
	}
	if req.BackBy != nil {
		next.BackBy = req.BackBy.Time
	}
	if !next.BackBy.After(next.Start) {
		return nil, httpx.BadRequest(MsgWindow)
	}
	if req.StopOrder != nil && !keepsAStop(it, req.StopOrder) {
		return nil, httpx.BadRequest(MsgNoStops)
	}
	if req.Date != nil || req.Start != nil || req.BackBy != nil || req.StopOrder != nil {
		next.Items = retime(&next, req.StopOrder, store.NewID)
		set["start"] = next.Start
		set["backBy"] = next.BackBy
		set["items"] = next.Items
		status := models.ItineraryActive
		if next.BackBy.Before(now) {
			status = models.ItineraryPast
		}
		set["status"] = status
	}
	return set, nil
}

// keepsAStop reports whether order names at least one of the plan's stops.
func keepsAStop(it *models.Itinerary, order []string) bool {
	for _, id := range order {
		for _, item := range it.Items {
			if item.ID == id && item.Kind != models.ItemTransit && item.Kind != kindBusy {
				return true
			}
		}
	}
	return false
}

// daysBetween is the number of calendar days from a's date to b's (both
// local midnights), safe across DST changes.
func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return int(time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// Delete is DELETE /itineraries/{id} → 204. Host only (a member gets 403):
// the group thread with its messages, expenses and photos goes too, every
// member hears itinerary.removed, and the forum refreshes when it was posted.
func (h *H) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), api.UserID(r)
	it, err := h.d.Store.Itineraries().ForMember(ctx, mux.Vars(r)["id"], uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if it.HostID != uid {
		api.Fail(w, r, httpx.Forbidden(MsgMemberDelete))
		return
	}
	gone, err := h.d.Store.Itineraries().DeleteHost(ctx, it.ID, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	realtime.ItineraryRemoved(h.d.Publish(), gone.MemberIDs, gone.ID)
	if gone.Visibility != models.VisibilityJustMe {
		realtime.ForumUpdate(h.d.Publish())
	}
	httpx.NoContent(w)
}

// LeaveHandler is POST /itineraries/{id}/leave → 204 (members; the host gets 400).
func (h *H) LeaveHandler(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), api.UserID(r)
	it, err := h.d.Store.Itineraries().ForMember(ctx, mux.Vars(r)["id"], uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := Leave(ctx, h.d, it, uid, httpx.TZ(r)); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// Leave takes userID off a plan they joined: out of its members and its
// group thread, their join marked cancelled. They hear itinerary.removed;
// everyone still on it hears itinerary.updated; the thread's members hear
// thread.updated (through the social views, see UseSocial); the forum
// refreshes when the plan is posted there. The host cannot leave (400).
// pkg/api/social can call it for DELETE /forum/posts/{id}/join-requests.
func Leave(ctx context.Context, d *api.Deps, it *models.Itinerary, userID string, tz *time.Location) error {
	if it.HostID == userID {
		return httpx.BadRequest(MsgHostLeave)
	}
	after, threadID, err := d.Store.Itineraries().Leave(ctx, it.ID, userID)
	if err != nil {
		return err
	}
	realtime.ItineraryRemoved(d.Publish(), []string{userID}, it.ID)
	PublishUpdated(ctx, d, after, after.MemberIDs)
	if threadID != "" {
		if s := currentSocial(); s != nil {
			s.ThreadChanged(ctx, d, threadID, tz)
		}
	}
	if after.Visibility != models.VisibilityJustMe {
		realtime.ForumUpdate(d.Publish())
	}
	return nil
}
