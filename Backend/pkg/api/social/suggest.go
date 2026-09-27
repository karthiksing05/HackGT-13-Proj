package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/ml"
	"Backend/pkg/models"
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

// SuggestPeople is GET /people/suggested → [PersonSuggestion]: people whose
// taste best matches the viewer's (likes minus clashes, from
// the ML service), best first. Not the viewer, not their friends, not bots.
// A viewer without a taste profile gets []; the ML service being down is 503.
func (h *H) SuggestPeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if h.d.ML == nil {
		api.Fail(w, r, httpx.E(http.StatusServiceUnavailable, msgSuggestWarmingUp))
		return
	}
	if !ml.Usable(viewer.PositiveEmbedding, ml.Dim) {
		httpx.JSON(w, http.StatusOK, []contract.PersonSuggestion{})
		return
	}
	viewerID := viewer.ID.Hex()
	friendIDs, err := h.d.Store.Friends().IDs(ctx, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	people, err := h.d.Store.Users().Suggestable(ctx, append(friendIDs, viewerID), suggestPool)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	byID := make(map[string]*models.User, len(people))
	cands := make([]ml.UserCandidate, 0, len(people))
	for _, u := range people {
		id := u.ID.Hex()
		byID[id] = u
		cands = append(cands, ml.UserCandidate{ID: id, Positive: u.PositiveEmbedding, Negative: u.NegativeEmbedding})
	}
	matches, err := h.d.ML.UserCompatibility(ctx, tasteVectors(viewer), cands)
	switch {
	case errors.Is(err, ml.ErrNoUserEmbedding):
		httpx.JSON(w, http.StatusOK, []contract.PersonSuggestion{})
		return
	case err != nil:
		api.Fail(w, r, httpx.Wrap(http.StatusServiceUnavailable, msgSuggestWarmingUp, err))
		return
	}
	if len(matches) > suggestLimit {
		matches = matches[:suggestLimit]
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID)
	}
	relations, err := h.d.Store.Friends().Relations(ctx, viewerID, ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.PersonSuggestion, 0, len(matches))
	for _, m := range matches {
		u := byID[m.ID]
		if u == nil {
			continue
		}
		rel := relations[m.ID]
		row := contract.PersonSuggestion{
			Person:        view.PersonRef(u, h.d.Cfg.PublicBaseURL),
			Relation:      contract.FriendRelation(rel.Kind),
			Compatibility: min(100, max(0, m.Percent)),
		}
		if rel.RequestID != "" {
			id := rel.RequestID
			row.RequestID = &id
		}
		out = append(out, row)
	}
	httpx.JSON(w, http.StatusOK, out)
}
