// Package candidates decides which activities are worth considering for a
// plan. It is the backend's version of the ML service's hard filters
// (ml/api/helpers/filters.py): max price, max distance, already over,
// availability window and excluded categories, plus what planning needs on
// top (both end points, place quality, age).
//
// Like the ML filters, a rule only applies when both sides have the data it
// needs: an activity with no price is never dropped for price. The ML
// service is then left to score, not filter.
//
// Query.EventFilter / PlaceFilter build the Mongo queries; Query.Keep is the
// same decision in Go, applied after the query and by the in-memory store.
package candidates

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Reasons an activity is rejected, for logging.
const (
	RejectNoLocation = "no_location"
	RejectTooFar     = "too_far"
	RejectNoStart    = "no_start_time"
	RejectOutside    = "outside_window"
	RejectOver       = "already_over"
	RejectPrice      = "over_budget"
	RejectAvoided    = "avoided"
	RejectAge        = "age_restricted"
	RejectCategory   = "category_excluded"
	RejectRating     = "low_rating"
)

// Query describes what a plan request can use.
type Query struct {
	// Activities must be within RadiusKm of at least one center (the start
	// and end points). No centers means no distance rule.
	Centers  []travel.Point
	RadiusKm float64

	// The plan window, and the current time. Events must overlap
	// [From, To) and not be over by Now.
	From, To time.Time
	Now      time.Time
	// An event with no published end is assumed to last at most this long.
	MaxEventSpan time.Duration

	BudgetCents int64    // 0 = no limit; applies per activity
	AvoidTags   []string // matched against category and tags, case-insensitively
	AgeBracket  string   // "13_17" | "18_20" | "21_plus"

	PlaceCategories []string // place categories that can be scheduled
	MinPlaceRating  float64  // places without a rating are kept only if hikes
}

// Keep reports whether an activity passes every rule, and the first rule it
// fails otherwise.
func (q Query) Keep(a *models.Activity) (bool, string) {
	if len(a.Location.Coordinates) < 2 {
		return false, RejectNoLocation
	}
	if len(q.Centers) > 0 && q.RadiusKm > 0 {
		p := travel.Point{Lat: a.Location.Coordinates[1], Lng: a.Location.Coordinates[0]}
		near := false
		for _, c := range q.Centers {
			if travel.HaversineKm(c, p) <= q.RadiusKm {
				near = true
				break
			}
		}
		if !near {
			return false, RejectTooFar
		}
	}

	if a.Kind == "place" {
		if !contains(q.PlaceCategories, a.Category) {
			return false, RejectCategory
		}
		if a.Rating == nil {
			if a.Category != "hike" && q.MinPlaceRating > 0 {
				return false, RejectRating
			}
		} else if *a.Rating < q.MinPlaceRating {
			return false, RejectRating
		}
	} else {
		if a.Start == nil {
			return false, RejectNoStart
		}
		finish := a.Start.Add(q.MaxEventSpan)
		if a.End != nil && a.End.After(*a.Start) {
			finish = *a.End
		}
		if !a.Start.Before(q.To) || !finish.After(q.From) {
			return false, RejectOutside
		}
		if !finish.After(q.Now) {
			return false, RejectOver
		}
	}

	if q.BudgetCents > 0 {
		if cents, ok := priceCents(a); ok && cents > q.BudgetCents {
			return false, RejectPrice
		}
	}
	if avoided(q.AvoidTags, a) {
		return false, RejectAvoided
	}
	if !ageAllowed(q.AgeBracket, a.Name) {
		return false, RejectAge
	}
	return true, ""
}

// EventFilter is the Mongo query for events. It matches everything Keep
// keeps (Keep re-checks the results).
func (q Query) EventFilter() bson.M {
	lower := q.From
	if q.Now.After(lower) {
		lower = q.Now
	}
	and := bson.A{
		bson.M{"kind": "event"},
		bson.M{"start": bson.M{"$lt": q.To}},
		// Overlaps the window and isn't over: either a published end after
		// the lower bound, or no end and a start recent enough.
		bson.M{"$or": bson.A{
			bson.M{"end": bson.M{"$gt": lower}},
			bson.M{"end": nil, "start": bson.M{"$gt": lower.Add(-q.MaxEventSpan)}},
		}},
	}
	return bson.M{"$and": append(and, q.common()...)}
}

// PlaceFilter is the Mongo query for places. Opening hours are checked later
// by the optimizer.
func (q Query) PlaceFilter() bson.M {
	and := bson.A{
		bson.M{"kind": "place"},
		bson.M{"category": bson.M{"$in": q.PlaceCategories}},
	}
	if q.MinPlaceRating > 0 {
		and = append(and, bson.M{"$or": bson.A{
			bson.M{"rating": bson.M{"$gte": q.MinPlaceRating}},
			bson.M{"rating": nil, "category": "hike"},
		}})
	}
	return bson.M{"$and": append(and, q.common()...)}
}

func (q Query) common() bson.A {
	var and bson.A
	if len(q.Centers) > 0 && q.RadiusKm > 0 {
		var near bson.A
		for _, c := range q.Centers {
			near = append(near, bson.M{"location": bson.M{"$geoWithin": bson.M{
				"$centerSphere": bson.A{bson.A{c.Lng, c.Lat}, q.RadiusKm / travel.EarthRadiusKm},
			}}})
		}
		and = append(and, bson.M{"$or": near})
	}
	if q.BudgetCents > 0 {
		dollars := float64(q.BudgetCents) / 100
		and = append(and, bson.M{"$or": bson.A{
			bson.M{"price.min": bson.M{"$lte": dollars}},
			bson.M{"price.min": nil},
		}})
	}
	if len(q.AvoidTags) > 0 {
		avoid := lowerAll(q.AvoidTags)
		and = append(and, bson.M{"category": bson.M{"$nin": avoid}}, bson.M{"tags": bson.M{"$nin": avoid}})
	}
	switch q.AgeBracket {
	case "13_17":
		and = append(and, bson.M{"name": bson.M{"$not": bson.M{"$regex": `(18\+|21\+)`, "$options": "i"}}})
	case "18_20":
		and = append(and, bson.M{"name": bson.M{"$not": bson.M{"$regex": `21\+`, "$options": "i"}}})
	}
	return and
}

// priceCents reads the lowest price. The ingested shape is {min, max} in
// whole units; seeded data may use cents.
func priceCents(a *models.Activity) (int64, bool) {
	if a.Price == nil {
		return 0, false
	}
	if a.Price.Min != nil {
		return int64(math.Round(*a.Price.Min * 100)), true
	}
	if a.Price.Cents > 0 || a.Price.IsFree {
		return a.Price.Cents, true
	}
	return 0, false
}

func avoided(avoid []string, a *models.Activity) bool {
	for _, t := range avoid {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if strings.ToLower(a.Category) == t {
			return true
		}
		for _, tag := range a.Tags {
			if strings.ToLower(tag) == t {
				return true
			}
		}
	}
	return false
}

func ageAllowed(bracket, name string) bool {
	n := strings.ToLower(name)
	switch bracket {
	case "13_17":
		return !strings.Contains(n, "18+") && !strings.Contains(n, "21+")
	case "18_20":
		return !strings.Contains(n, "21+")
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
