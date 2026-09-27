package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
)

const (
	// suggestPool caps how many accounts are scored per request; suggestLimit
	// is how many come back.
	suggestPool  = 300
	suggestLimit = 20

	msgSuggestWarmingUp = "Suggestions are warming up. Try again in a moment."
)

// tasteVectors is a user's stored likes and dislikes vectors for matching.
func tasteVectors(u *models.User) ml.UserVectors {
	return ml.UserVectors{Positive: u.PositiveEmbedding, Negative: u.NegativeEmbedding}
}

// SuggestPeople is GET /people/suggested → [PersonSuggestion]: people in the
// viewer's catalog whose taste best matches theirs (likes minus clashes, from
// the ML service), best first, then (without a match percent) recently
// active people to fill the list, so a viewer without a taste profile, or
// with few matches, still has people to add. Not the viewer, not their
// friends, not bots. The demo cast (whose catalog holds only bots besides
// them) gets []; the ML service being down is 503.
func (h *H) SuggestPeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if store.IsDemoCast(viewer) {
		httpx.JSON(w, http.StatusOK, []contract.PersonSuggestion{})
		return
	}
	viewerID := viewer.ID.Hex()
	friendIDs, err := h.d.Store.Friends().IDs(ctx, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	exclude := append(friendIDs, viewerID)
	matches, byID, err := h.tasteMatches(ctx, viewer, exclude)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if len(matches) > suggestLimit {
		matches = matches[:suggestLimit]
	}
	ids := make([]string, 0, suggestLimit)
	percent := make(map[string]int, len(matches))
	for _, m := range matches {
		if byID[m.ID] != nil {
			ids = append(ids, m.ID)
			percent[m.ID] = min(100, max(0, m.Percent))
		}
	}
	if len(ids) < suggestLimit {
		recent, err := h.d.Store.Users().RecentlyActive(ctx, append(exclude, ids...), suggestLimit-len(ids))
		if err != nil {
			api.Fail(w, r, err)
			return
		}
		for _, u := range recent {
			byID[u.ID.Hex()] = u
			ids = append(ids, u.ID.Hex())
		}
	}
	relations, err := h.d.Store.Friends().Relations(ctx, viewerID, ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.PersonSuggestion, 0, len(ids))
	for _, id := range ids {
		rel := relations[id]
		row := contract.PersonSuggestion{
			Person:   view.PersonRef(byID[id], h.d.Cfg.PublicBaseURL),
			Relation: contract.FriendRelation(rel.Kind),
		}
		if p, ok := percent[id]; ok {
			row.Compatibility = &p
		}
		if rel.RequestID != "" {
			id := rel.RequestID
			row.RequestID = &id
		}
		out = append(out, row)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// tasteMatches scores the suggestable people (not in exclude) against the
// viewer's taste, best first, with the scored users by id. A viewer without
// a taste profile has no matches and needs no ML service; otherwise the
// service missing or failing is 503.
func (h *H) tasteMatches(ctx context.Context, viewer *models.User, exclude []string) ([]ml.Match, map[string]*models.User, error) {
	byID := map[string]*models.User{}
	if !ml.Usable(viewer.PositiveEmbedding, ml.Dim) {
		return nil, byID, nil
	}
	if h.d.ML == nil {
		return nil, nil, httpx.E(http.StatusServiceUnavailable, msgSuggestWarmingUp)
	}
	people, err := h.d.Store.Users().Suggestable(ctx, exclude, suggestPool)
	if err != nil {
		return nil, nil, err
	}
	cands := make([]ml.UserCandidate, 0, len(people))
	for _, u := range people {
		id := u.ID.Hex()
		byID[id] = u
		cands = append(cands, ml.UserCandidate{ID: id, Positive: u.PositiveEmbedding, Negative: u.NegativeEmbedding})
	}
	matches, err := h.d.ML.UserCompatibility(ctx, tasteVectors(viewer), cands)
	switch {
	case errors.Is(err, ml.ErrNoUserEmbedding):
		return nil, byID, nil
	case err != nil:
		return nil, nil, httpx.Wrap(http.StatusServiceUnavailable, msgSuggestWarmingUp, err)
	}
	return matches, byID, nil
}
