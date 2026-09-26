package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"time"
)

// Fixed-start events must begin this long before the window closes.
const eventStartMargin = 15 * time.Minute

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
	if len(q.Kinds) > 0 && !containsString(q.Kinds, a.Kind) {
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
			if start.Before(q.From) || start.After(q.To.Add(-eventStartMargin)) {
				return false
			}
		}
	} else if len(q.PlaceCategories) > 0 && !containsString(q.PlaceCategories, a.Category) {
		return false
	}
	if ageRules(q.AgeBracket).blocks(a) {
		return false
	}
	if containsString(q.ExcludeCategories, a.Category) {
		return false
	}
	for _, t := range a.Tags {
		if containsString(q.ExcludeTags, t) {
			return false
		}
	}
	if len(q.IncludeCategories)+len(q.AnyTags) > 0 {
		hit := containsString(q.IncludeCategories, a.Category)
		for _, t := range a.Tags {
			if hit {
				break
			}
			hit = containsString(q.AnyTags, t)
		}
		if !hit {
			return false
		}
	}
	if !priceAllowed(a, q) {
		return false
	}
	return !containsString(q.ExcludeIDs, a.ID.Hex())
}

// priceAllowed is the price clause: null price means unknown, never free.
func priceAllowed(a *models.Activity, q *CandidateQuery) bool {
	if q.FreeOnly {
		if a.Price == nil {
			return freeIfUnknownCategories[a.Category]
		}
		return a.Price.IsFree || (a.Price.Min != nil && *a.Price.Min == 0)
	}
	if q.MaxTier >= 3 {
		return true
	}
	if a.Price == nil {
		return q.AllowUnknownPrice
	}
	if a.Price.Tier > 0 && a.Price.Tier <= q.MaxTier {
		return true
	}
	if a.Price.Min != nil && *a.Price.Min <= tierBounds[q.MaxTier] {
		return true
	}
	if a.Price.Tier == 0 && a.Price.Min == nil {
		// A price object that says nothing about the amount.
		return q.AllowUnknownPrice || a.Price.IsFree
	}
	return a.Price.IsFree
}

// tierBound is the upper price (whole units) of a tier for the query.
func tierBound(tier int) float64 {
	if tier < 0 {
		return 0
	}
	if tier >= len(tierBounds) {
		return tierBounds[len(tierBounds)-1]
	}
	return tierBounds[tier]
}
