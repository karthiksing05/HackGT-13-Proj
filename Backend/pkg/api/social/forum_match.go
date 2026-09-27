package social

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"context"
	"errors"

	"github.com/rs/zerolog/log"
)

// scorePlans sets each plan post's Compatibility: the mean compatibility
// score of its stops for the viewer (one ML call for the whole feed). Best
// effort: without a taste profile, stop vectors or the ML service the posts
// simply have no match, and the feed still answers.
func (h *H) scorePlans(ctx context.Context, viewer *models.User, items []feedItem) {
	if h.d.ML == nil || !ml.Usable(viewer.PositiveEmbedding, ml.Dim) {
		return
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.activityIDs...)
	}
	if len(ids) == 0 {
		return
	}
	vectors, err := h.d.Store.Catalog().Embeddings(ctx, viewer.Catalog, ids)
	if err != nil {
		log.Warn().Err(err).Msg("forum match: loading stop vectors failed")
		return
	}
	plans := make([]ml.ItineraryStops, 0, len(items))
	for _, it := range items {
		if len(it.activityIDs) == 0 {
			continue
		}
		stops := make(map[string][]float64, len(it.activityIDs))
		for _, id := range it.activityIDs {
			if v, ok := vectors[id]; ok {
				stops[id] = v
			}
		}
		plans = append(plans, ml.ItineraryStops{ID: it.post.ID, Stops: stops})
	}
	matches, err := h.d.ML.ItineraryCompatibility(ctx, tasteVectors(viewer), plans)
	if err != nil {
		if !errors.Is(err, ml.ErrNoUserEmbedding) {
			log.Warn().Err(err).Msg("forum match: ML service failed")
		}
		return
	}
	percent := make(map[string]int, len(matches))
	for _, m := range matches {
		percent[m.ID] = min(100, max(0, m.Percent))
	}
	for i := range items {
		if p, ok := percent[items[i].post.ID]; ok {
			items[i].post.Compatibility = &p
		}
	}
}
