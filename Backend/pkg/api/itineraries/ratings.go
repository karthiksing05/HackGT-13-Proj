package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// MsgStars is the 400 for a rating outside 1–5.
const MsgStars = "Pick 1 to 5 stars."

// ratedTimeout bounds the background taste-vector update after a rating.
const ratedTimeout = 10 * time.Second

// Rate is PUT /ratings/{itemId} (Rating) → 204 for an item of a plan the
// viewer is on (404 otherwise). A new or changed rating of a stop moves the
// viewer's taste tags (users.taste, see tasteTargets) and, when the stop is
// a catalog activity, folds into their taste vectors in the background
// (api.Profiles; a failing ML service never fails the rating).
func (h *H) Rate(w http.ResponseWriter, r *http.Request) {
	var req contract.Rating
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Stars < 1 || req.Stars > 5 {
		httpx.Error(w, http.StatusBadRequest, MsgStars)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx, uid := r.Context(), user.ID.Hex()
	it, i, err := h.d.Store.Itineraries().FindItem(ctx, mux.Vars(r)["itemId"], uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	item := it.Items[i]
	tags := cleanTags(req.Tags)
	var note *string
	if req.Note != nil && strings.TrimSpace(*req.Note) != "" {
		text := strings.TrimSpace(*req.Note)
		note = &text
	}
	prev, err := h.d.Store.Ratings().Upsert(ctx, models.Rating{
		UserID: uid, ItemID: item.ID, ItineraryID: it.ID, ActivityID: item.ActivityID,
		Stars: req.Stars, Tags: tags, Note: note,
	})
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	// Re-saving the same stars must not step the taste twice; a newly added
	// "Too crowded" still counts.
	starsChanged := prev == nil || prev.Stars != req.Stars
	crowdedNew := tooCrowded(tags) && (prev == nil || !tooCrowded(prev.Tags))
	if item.Kind != models.ItemTransit && (starsChanged || crowdedNew) {
		targets := h.tasteTargets(ctx, user, it, &item, req.Stars, tags, starsChanged)
		if err := h.d.Store.Users().BumpTaste(ctx, uid, targets, prev == nil); err != nil {
			log.Warn().Err(err).Str("user", uid).Str("item", item.ID).Msg("taste tags not updated")
		}
		if starsChanged {
			h.d.RatedAsync(uid, item.ActivityID, req.Stars, ratedTimeout)
		}
	}
	httpx.NoContent(w)
}

// tooCrowded reports the "Too crowded" rating tag.
func tooCrowded(tags []string) bool {
	return slices.ContainsFunc(tags, func(t string) bool { return strings.EqualFold(t, "Too crowded") })
}

// cleanTags trims, drops empty and repeated tags, keeping their order.
func cleanTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// tasteTargets is where one rating pulls the viewer's taste tags: with
// withStars, the trip types the rated activity speaks to (tasteKeys) and
// early_mornings for a stop that starts before 9 AM go toward (stars−1)/4;
// the "Too crowded" tag pulls big_crowds to 0.
func (h *H) tasteTargets(ctx context.Context, user *models.User, it *models.Itinerary, item *models.ItineraryItem, stars int, tags []string, withStars bool) map[string]float64 {
	target := float64(stars-1) / 4
	out := map[string]float64{}
	if tooCrowded(tags) {
		out["big_crowds"] = 0
	}
	if !withStars {
		return out
	}
	if item.ActivityID != "" {
		act, err := h.d.Store.Catalog().Activity(ctx, user.Catalog, item.ActivityID)
		switch {
		case err == nil:
			for _, key := range tasteKeys(act) {
				out[key] = target
			}
		case !errors.Is(err, store.ErrNotFound):
			log.Warn().Err(err).Str("activity", item.ActivityID).Msg("rated activity not read")
		}
	}
	if item.Start.In(httpx.Location(it.TZ)).Hour() < earlyMorningHour {
		out["early_mornings"] = target
	}
	if tooCrowded(tags) {
		out["big_crowds"] = 0
	}
	return out
}
