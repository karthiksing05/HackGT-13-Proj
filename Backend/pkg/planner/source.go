package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"slices"
	"time"
)

// EventStartMargin: fixed-start events must begin at least this long before
// the window (or slot) closes.
const EventStartMargin = 15 * time.Minute

// EventLateStart: a fixed-start event may have begun this long before the
// window (or slot) opens and still be a candidate; the Go-side check keeps
// it only when a late arrival is fine for it (itinerary.Clippable).
const EventLateStart = itinerary.LateArrival

// CandidateQuery is a guaranteed pre-filter: every field is a hard
// condition the store applies before anything is scored. MatchesQuery is
// the same conditions in Go, used by the fake source and by the planner's
// own re-check.
type CandidateQuery struct {
	Catalog string
	City    string   // omitted from the query when empty
	Kinds   []string // "event", "place"; empty means both

	Center   travel.Point
	RadiusKm float64 // 0 means no geo filter

	From, To time.Time // the window, or an expansion slot

	MaxTier           int  // 3 or more means no tier filter
	FreeOnly          bool // budget 0
	AllowUnknownPrice bool // tiers 1–2 keep documents without a price

	AgeBracket string

	ExcludeCategories []string
	ExcludeTags       []string
	IncludeCategories []string // with AnyTags: category ∈ these OR tags ∩ AnyTags ≠ ∅
	AnyTags           []string
	ExcludeIDs        []string // hex ids

	PlaceCategories []string // place categories that can become a stop
	// MinPlaceRating keeps places rated at least this, and unrated hikes;
	// 0 means no rating rule.
	MinPlaceRating float64

	LimitEvents int
	LimitPlaces int
}

// CandidateSource runs the guaranteed pre-filter against a catalog. Events
// come back sorted by start, places by rating then popularity (ties by id).
type CandidateSource interface {
	FindCandidates(ctx context.Context, q CandidateQuery) (events, places []models.Activity, err error)
}

// EmbeddingSource fetches stored activity vectors for the ids that
// survived the Go-side feasibility check.
type EmbeddingSource interface {
	FetchEmbeddings(ctx context.Context, catalog string, ids []string) (map[string][]float64, error)
}

// ActivityLookup fetches full documents by id (for resolving a saved stop).
type ActivityLookup interface {
	GetActivities(ctx context.Context, catalog string, ids []string) ([]models.Activity, error)
}

// MatchesQuery applies the query's conditions to one document.
func MatchesQuery(a *models.Activity, q *CandidateQuery) bool {
	if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, a.Kind) {
		return false
	}
	if q.City != "" && a.City != q.City {
		return false
	}
	if a.Kind != "event" && a.Kind != "place" {
		return false
	}
	p, ok := activityPoint(a)
	if !ok {
		return false
	}
	if q.RadiusKm > 0 && travel.HaversineKm(q.Center, p) > q.RadiusKm {
		return false
	}
	if a.Kind == "event" {
		if a.Start == nil {
			return false
		}
		start := a.Start.UTC()
		if a.Attendance != nil && *a.Attendance == "drop_in" {
			if !start.Before(q.To) {
				return false
			}
			if a.End != nil && !a.End.UTC().After(q.From) {
				return false
			}
		} else {
			if start.Before(q.From.Add(-EventLateStart)) || start.After(q.To.Add(-EventStartMargin)) {
				return false
			}
		}
	} else {
		if len(q.PlaceCategories) > 0 && !slices.Contains(q.PlaceCategories, a.Category) {
			return false
		}
		if !ratingAllowed(a, q.MinPlaceRating) {
			return false
		}
	}
	if AgeRulesFor(q.AgeBracket).Blocks(a) {
		return false
	}
	if slices.Contains(q.ExcludeCategories, a.Category) {
		return false
	}
	for _, t := range a.Tags {
		if slices.Contains(q.ExcludeTags, t) {
			return false
		}
	}
	if len(q.IncludeCategories)+len(q.AnyTags) > 0 {
		hit := slices.Contains(q.IncludeCategories, a.Category)
		for _, t := range a.Tags {
			if hit {
				break
			}
			hit = slices.Contains(q.AnyTags, t)
		}
		if !hit {
			return false
		}
	}
	if !priceAllowed(a, q) {
		return false
	}
	return !slices.Contains(q.ExcludeIDs, a.ID.Hex())
}

// ratingAllowed is the place-quality rule: rated at least min, or an
// unrated hike (trails rarely have ratings). It mirrors the store's query
// (mongosource.ratingClause).
func ratingAllowed(a *models.Activity, min float64) bool {
	if min <= 0 || a.Kind != "place" {
		return true
	}
	if a.Rating == nil {
		return a.Category == "hike"
	}
	return *a.Rating >= min
}

// priceAllowed is the price clause: a null price is unknown, never free.
// It mirrors the store's query exactly (mongosource.priceClause).
func priceAllowed(a *models.Activity, q *CandidateQuery) bool {
	p := a.Price
	if q.FreeOnly {
		if p == nil {
			return freeIfUnknownCategories[a.Category]
		}
		return p.IsFree || (p.Min != nil && *p.Min == 0)
	}
	if q.MaxTier >= 3 {
		return true
	}
	if p == nil {
		return q.AllowUnknownPrice
	}
	switch {
	case p.Tier >= 1 && p.Tier <= q.MaxTier:
		return true
	case p.Min != nil && *p.Min <= TierBound(q.MaxTier):
		return true
	case p.IsFree:
		return true
	case p.Tier == 0 && p.Min == nil:
		return q.AllowUnknownPrice // a price object that states no amount
	}
	return false
}

// TierBound is the upper price, in whole currency units, of a price tier
// (0 → 0, 1 → 15, 2 → 40, 3 → 80).
func TierBound(tier int) float64 {
	if tier < 0 {
		return 0
	}
	if tier >= len(tierBounds) {
		return tierBounds[len(tierBounds)-1]
	}
	return tierBounds[tier]
}
