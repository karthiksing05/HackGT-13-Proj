package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// pastPage is the page size of GET /me/past-events.
const pastPage = 50

// PastEvents is GET /me/past-events?unrated=true|false&cursor= →
// {items: [PastEvent], next_cursor}: the stops of the viewer's plans that
// have ended, most recent first; unrated=true leaves out the rated ones.
func (h *H) PastEvents(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), api.UserID(r)
	q := r.URL.Query()
	limit := httpx.Limit(r, pastPage, pastPage)
	query := store.PastStopsQuery{UserID: uid, Now: h.d.Clock(), Limit: limit + 1}
	if raw := q.Get("cursor"); raw != "" {
		after, ok := parsePastCursor(httpx.Cursor(r))
		if !ok {
			httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
			return
		}
		query.After = after
	}
	if unrated := q.Get("unrated"); strings.EqualFold(unrated, "true") || unrated == "1" {
		rated, err := h.d.Store.Ratings().RatedItemIDs(ctx, uid)
		if err != nil {
			api.Fail(w, r, err)
			return
		}
		query.Exclude = rated
	}
	rows, err := h.d.Store.Itineraries().PastStops(ctx, query)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1].Item
		next = httpx.NextCursor(strconv.FormatInt(last.Start.UnixMilli(), 10) + "|" + last.ID)
	}
	events, err := h.pastEvents(ctx, uid, rows)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.NewPage(events, next))
}

// parsePastCursor reads "<start unix ms>|<item id>".
func parsePastCursor(s string) (*store.PastStopCursor, bool) {
	ms, id, ok := strings.Cut(s, "|")
	if !ok || id == "" {
		return nil, false
	}
	n, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return nil, false
	}
	return &store.PastStopCursor{Start: time.UnixMilli(n).UTC(), ItemID: id}, true
}

// pastEvents renders ended stops with the viewer's ratings.
func (h *H) pastEvents(ctx context.Context, uid string, rows []store.PastStop) ([]contract.PastEvent, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Item.ID)
	}
	ratings, err := h.d.Store.Ratings().ForUser(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	out := make([]contract.PastEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, pastEvent(row, ratings[row.Item.ID]))
	}
	return out, nil
}

// pastEvent is one ended stop: "solo" or "with N others", and a group when
// the plan had company.
func pastEvent(row store.PastStop, rating *models.Rating) contract.PastEvent {
	kind, company := contract.KindSidequest, "solo"
	if row.Members > 1 {
		kind, company = contract.KindGroup, "with "+httpx.Plural(row.Members-1, "other", "others")
	}
	place := ""
	if row.Item.Place != nil {
		place = row.Item.Place.Name
	}
	return contract.PastEvent{
		ID:      row.Item.ID,
		Title:   row.Item.Title,
		Place:   place,
		Company: company,
		Date:    contract.NewTime(row.Item.Start),
		Kind:    kind,
		Rating:  ratingOut(rating),
	}
}
