package main

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	milesPerKm = 0.621371
	// hopMiles bounds how far each stop after the first may be from the one
	// before it (a walk).
	hopMiles = 1.5
	// leadTime: a plan that would start sooner than this moves a day later,
	// so every plan is still ahead when the seed has run.
	leadTime = 45 * time.Minute
	// walkNote is the description of every transit item (pkg/api/itineraries).
	walkNote = "Route options below. Times update live if you run late."
)

// catalog is the places of one catalog collection (kind place, with a
// location), read without their vectors.
type catalog struct {
	docs   int64 // every document, events included (for the report)
	places []*models.Activity
	byName map[string]*models.Activity // lowercase name → the place with the lowest id
}

func loadCatalog(ctx context.Context, st *store.Store, coll string) (*catalog, error) {
	c := &catalog{byName: map[string]*models.Activity{}}
	n, err := st.Collection(coll).CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("count %s: %w", coll, err)
	}
	c.docs = n
	cursor, err := st.Collection(coll).Find(ctx, bson.M{"kind": "place"}, options.Find().
		SetProjection(bson.M{"embedding": 0, "embeddingText": 0, "embeddingTextHash": 0, "embeddingMeta": 0, "embeddingTextMeta": 0}).
		SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", coll, err)
	}
	var all []*models.Activity
	if err := cursor.All(ctx, &all); err != nil {
		return nil, fmt.Errorf("read %s: %w", coll, err)
	}
	for _, a := range all {
		if len(a.Location.Coordinates) != 2 || strings.TrimSpace(a.Name) == "" {
			continue
		}
		c.places = append(c.places, a)
		if key := strings.ToLower(strings.TrimSpace(a.Name)); c.byName[key] == nil {
			c.byName[key] = a
		}
	}
	return c, nil
}

// point is where a catalog place is.
func point(a *models.Activity) travel.Point {
	return travel.Point{Lat: a.Location.Coordinates[1], Lng: a.Location.Coordinates[0]}
}

func miles(a, b travel.Point) float64 { return travel.HaversineKm(a, b) * milesPerKm }

// walkMinutes is the heuristic walk between two points, at least a minute.
func walkMinutes(a, b travel.Point) int {
	return max(int(travel.Estimate(a, b, travel.Walk).Duration.Minutes()), 1)
}

// openFor reports whether a place is open for the whole window, by its
// weekly hours in its own time zone (the planner's default hours for its
// category when it has none; never for a category without safe defaults).
func openFor(a *models.Activity, fallback *time.Location, from, to time.Time) bool {
	loc := fallback
	if a.Timezone != "" {
		if l, err := time.LoadLocation(a.Timezone); err == nil {
			loc = l
		}
	}
	intervals, ok := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, from, to)
	if !ok {
		return false
	}
	for _, iv := range intervals {
		if !iv.Start.After(from) && !iv.End.Before(to) {
			return true
		}
	}
	return false
}

// stopPick is one chosen stop: the place, when the group is there, the walk
// that led to it and how well it matched its spec (0 a named place, 1 a
// place of the spec's categories, 2 any open place).
type stopPick struct {
	place      *models.Activity
	start, end time.Time
	walk       int
	tier       int
}

// pickStop chooses the place of one stop. The first stop starts at at,
// within the world's reach of the Forum's center (a fallback nearest the
// plan's anchor); every later one follows a walk from prev. used are the
// places other plans of the world already have (a fallback avoids them),
// inPlan this plan's.
func (c *catalog) pickStop(w *worldSpec, s stopSpec, anchor travel.Point, prev *models.Activity, at time.Time,
	loc *time.Location, used, inPlan map[string]bool) (stopPick, bool) {
	fits := func(a *models.Activity) (stopPick, bool) {
		id := a.ID.Hex()
		if inPlan[id] {
			return stopPick{}, false
		}
		p := point(a)
		start, walk := at, 0
		if prev == nil {
			if miles(w.center, p) > w.maxFromCenter {
				return stopPick{}, false
			}
		} else {
			if miles(point(prev), p) > hopMiles {
				return stopPick{}, false
			}
			walk = walkMinutes(point(prev), p)
			start = at.Add(time.Duration(walk) * time.Minute)
		}
		end := start.Add(time.Duration(s.minutes) * time.Minute)
		if !openFor(a, loc, start, end) {
			return stopPick{}, false
		}
		return stopPick{place: a, start: start, end: end, walk: walk}, true
	}
	for _, name := range s.names {
		if a := c.byName[strings.ToLower(name)]; a != nil {
			if pick, ok := fits(a); ok {
				return pick, true
			}
		}
	}
	ref := anchor
	if prev != nil {
		ref = point(prev)
	}
	// Rank the other open places: its categories before anything else, then
	// places no other plan has, then the nearest. Places are sorted by id,
	// so a tie keeps the lowest id and every run picks the same.
	var best stopPick
	var bestKey []float64
	for _, a := range c.places {
		pick, ok := fits(a)
		if !ok {
			continue
		}
		pick.tier = 2
		if hasAny([]string{a.Category}, s.categories) {
			pick.tier = 1
		}
		reused := 0.0
		if used[a.ID.Hex()] {
			reused = 1
		}
		key := []float64{float64(pick.tier), reused, miles(ref, point(a))}
		if bestKey == nil || slices.Compare(key, bestKey) < 0 {
			best, bestKey = pick, key
		}
	}
	return best, bestKey != nil
}

// planStart is when a plan starts: its day and time in the world's zone,
// a day later when that is less than leadTime from now.
func planStart(spec *planSpec, now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day()+spec.day, spec.start[0], spec.start[1], 0, 0, loc)
	if start.Before(now.Add(leadTime)) {
		start = time.Date(local.Year(), local.Month(), local.Day()+spec.day+1, spec.start[0], spec.start[1], 0, 0, loc)
	}
	return start
}

// laidOut is a plan's stops in the catalog, in order.
type laidOut struct {
	start, backBy time.Time
	stops         []stopPick
	exact         bool // every stop matched its spec (a named place or its categories)
}

// layOut picks a plan's stops from the catalog; ok is false when not even
// its first stop fits.
func (c *catalog) layOut(w *worldSpec, spec *planSpec, now time.Time, loc *time.Location, used map[string]bool) (laidOut, bool) {
	out := laidOut{start: planStart(spec, now, loc), exact: true}
	inPlan := map[string]bool{}
	cursor := out.start
	var prev *models.Activity
	for _, s := range spec.stops {
		pick, ok := c.pickStop(w, s, spec.anchor, prev, cursor, loc, used, inPlan)
		if !ok {
			if prev == nil {
				return laidOut{}, false
			}
			out.exact = false
			continue
		}
		if pick.tier > 1 {
			out.exact = false
		}
		out.stops = append(out.stops, pick)
		inPlan[pick.place.ID.Hex()] = true
		cursor, prev = pick.end, pick.place
	}
	for _, s := range out.stops {
		used[s.place.ID.Hex()] = true
	}
	out.backBy = quarterCeil(cursor)
	return out, true
}

// quarterCeil rounds a time up to the next quarter hour.
func quarterCeil(t time.Time) time.Time {
	q := t.Truncate(15 * time.Minute)
	if q.Before(t) {
		q = q.Add(15 * time.Minute)
	}
	return q
}

// placeName is how a stop's place reads: its venue, else its own name.
func placeName(a *models.Activity) string {
	if a.VenueName != nil && strings.TrimSpace(*a.VenueName) != "" {
		return strings.TrimSpace(*a.VenueName)
	}
	return a.Name
}

func placeDoc(a *models.Activity) models.PlaceDoc {
	p := point(a)
	lat, lng := p.Lat, p.Lng
	return models.PlaceDoc{Name: placeName(a), Lat: &lat, Lng: &lng}
}

// priceCents is the entry price the way the planner reads it: the minimum
// of a range in whole dollars, free, or stored cents; known is false when
// the catalog does not say.
func priceCents(a *models.Activity) (int, bool) {
	p := a.Price
	switch {
	case p == nil:
		return 0, false
	case p.Min != nil && !math.IsNaN(*p.Min) && !math.IsInf(*p.Min, 0):
		return max(int(math.Round(*p.Min*100)), 0), true
	case p.IsFree:
		return 0, true
	case p.Cents > 0:
		return int(p.Cents), true
	}
	return 0, false
}

func nonEmpty(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

// stopItem is a plan's n-th stop as the itinerary stores it, with what the
// planner would know about the activity (its id, price, links, bookable).
func stopItem(planID string, n int, s stopPick) models.ItineraryItem {
	a := s.place
	pd := placeDoc(a)
	item := models.ItineraryItem{
		ID: fmt.Sprintf("%s-stop-%d", planID, n), Kind: models.ItemStop, Title: a.Name, Place: &pd,
		Start: s.start.UTC(), End: s.end.UTC(), WebsiteURL: nonEmpty(a.URL), TicketURL: nonEmpty(a.TicketURL),
		ActivityID: a.ID.Hex(), DurationMin: int(s.end.Sub(s.start).Minutes()),
	}
	if a.Summary != nil {
		item.Description = strings.TrimSpace(*a.Summary)
	}
	if cents, known := priceCents(a); known {
		item.PriceCents = &cents
	}
	item.Bookable = item.TicketURL != nil || (item.PriceCents != nil && *item.PriceCents > 0)
	return item
}

// items are a plan's timeline: each stop, with the walk to it before it.
func items(planID string, stops []stopPick) []models.ItineraryItem {
	out := []models.ItineraryItem{}
	for i, s := range stops {
		if i > 0 {
			from := stops[i-1]
			out = append(out, models.ItineraryItem{
				ID: fmt.Sprintf("%s-leg-%d", planID, i), Kind: models.ItemTransit, Title: "Walk to " + s.place.Name,
				Place: &models.PlaceDoc{Name: placeName(from.place) + " → " + placeName(s.place)},
				Start: from.end.UTC(), End: s.start.UTC(), Description: walkNote,
				LegMode: "walk", LegMinutes: s.walk, DurationMin: s.walk,
			})
		}
		out = append(out, stopItem(planID, i+1, s))
	}
	return out
}

// Forum interest tags (the app's ForumQuery.interestTags) and what speaks to them.
var (
	interestTags  = []string{"Outdoors", "Food", "Art", "Music", "Active", "Games", "Shopping"}
	tagCategories = map[string][]string{
		"Outdoors": {"park", "garden", "hike", "viewpoint", "zoo_aquarium"},
		"Food":     {"restaurant", "cafe", "market", "food_hall", "bakery", "dessert"},
		"Art":      {"museum", "gallery", "landmark", "theater"},
		"Music":    {"live_music", "festival"},
		"Active":   {"hike", "sports_event"},
		"Games":    {"rec_venue"},
		"Shopping": {"shopping", "market"},
	}
	tagTags = map[string][]string{
		"Outdoors": {"outdoor", "outdoors", "nature"},
		"Food":     {"food"},
		"Art":      {"art"},
		"Music":    {"music"},
		"Active":   {"active"},
	}
)

// planTags are the Forum tags a plan's stops speak to, in the app's order.
func planTags(stops []stopPick) []string {
	var out []string
	for _, tag := range interestTags {
		for _, s := range stops {
			if hasAny([]string{s.place.Category}, tagCategories[tag]) || hasAny(s.place.Tags, tagTags[tag]) {
				out = append(out, tag)
				break
			}
		}
	}
	return out
}

func hasAny(have, want []string) bool {
	for _, a := range have {
		for _, b := range want {
			if a == b {
				return true
			}
		}
	}
	return false
}

// planBudget is the dearest stop's price tier (0 free … 3 $$$).
func planBudget(stops []stopPick) int {
	budget := 0
	for _, s := range stops {
		if s.place.Price != nil {
			budget = max(budget, min(s.place.Price.Tier, 3))
		}
	}
	return budget
}
