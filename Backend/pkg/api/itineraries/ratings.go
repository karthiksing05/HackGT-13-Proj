package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// Rating limits and their sentences.
const (
	MsgStars     = "Pick 1 to 5 stars."
	MsgRatingTag = "Pick up to 12 tags."
	maxTags      = 12
)

// ratedTimeout bounds the background taste-vector update after a rating.
const ratedTimeout = 10 * time.Second

// Rate is PUT /ratings/{itemId} (Rating) → 204 for an item of a plan the
// viewer is on (404 otherwise). A new or changed rating of a stop moves the
// viewer's taste tags (users.taste.tags, which GET /me/taste-profile reads:
// see starTargets and tagTargets) and, when the stop is a catalog activity,
// folds into their taste vectors in the background (api.Profiles; a
// failing ML service never fails the rating).
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
	if len(tags) > maxTags {
		httpx.Error(w, http.StatusBadRequest, MsgRatingTag)
		return
	}
	if req.Note != nil && utf8.RuneCountInString(*req.Note) > maxNoteRunes {
		httpx.Error(w, http.StatusBadRequest, MsgNoteTooLong)
		return
	}
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
	if item.Kind == models.ItemTransit {
		httpx.NoContent(w)
		return
	}
	// Re-saving must not step the taste twice: the stars count again only
	// when they changed, a rating tag only when it is new.
	starsChanged := prev == nil || prev.Stars != req.Stars
	targets := map[string]float64{}
	if starsChanged {
		targets = h.starTargets(ctx, user, it, &item, req.Stars)
	}
	var before []string
	if prev != nil {
		before = prev.Tags
	}
	maps.Copy(targets, tagTargets(tags, before))
	if len(targets) > 0 || prev == nil {
		if err := h.d.Store.Users().BumpTaste(ctx, uid, targets, prev == nil); err != nil {
			log.Warn().Err(err).Str("user", uid).Str("item", item.ID).Msg("taste tags not updated")
		}
	}
	if starsChanged {
		h.d.RatedAsync(uid, item.ActivityID, req.Stars, ratedTimeout)
	}
	httpx.NoContent(w)
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

// starTargets is where the stars pull the taste tags, toward (stars−1)/4:
// the trip types the rated catalog activity speaks to (tasteKeys),
// early_mornings for a stop that starts before 9 AM, and social for a stop
// shared with company.
func (h *H) starTargets(ctx context.Context, user *models.User, it *models.Itinerary, item *models.ItineraryItem, stars int) map[string]float64 {
	target := float64(stars-1) / 4
	out := map[string]float64{}
	if item.ActivityID != "" {
		act, err := h.d.Store.Catalog().Activity(ctx, store.CatalogFor(user), item.ActivityID)
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
	if len(it.MemberIDs) > 1 {
		out["social"] = target
	}
	return out
}

// tagTargets is what the rating's own tags say whatever the stars
// (ratingTagTaste), for the tags not already in before.
func tagTargets(tags, before []string) map[string]float64 {
	out := map[string]float64{}
	for _, tag := range tags {
		signal, ok := ratingTagTaste[strings.ToLower(tag)]
		if ok && !slices.ContainsFunc(before, func(b string) bool { return strings.EqualFold(b, tag) }) {
			out[signal.key] = signal.target
		}
	}
	return out
}
