package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"math"
	"sort"
	"strings"
	"time"
)

// Node is one possible visit with a fixed start and end. A fixed-time event
// is one node; a drop-in event or a place becomes several nodes (one per
// start time) that share a series, so at most one of them is used.
type Node struct {
	Act       *models.Activity
	Start     time.Time
	End       time.Time
	P75End    time.Time // end if a fixed event with an estimated length runs long; End otherwise
	Loc       travel.Point
	Utility   float64
	CostCents int64
	Flexible  bool // the activity could have started at another time

	SeriesKey string
	series    int // bit index; every node has one
	category  int // bit index, or -1 when the category doesn't limit repeats
}

// Drop records why an activity produced no nodes.
type Drop struct {
	ActivityID string
	Reason     string
}

// Categories that don't identify a kind of outing, so several stops may share them.
var unlimitedCategories = map[string]bool{"": true, "other": true}

// Place categories we schedule. Venue types (theater, cinema, live_music,
// comedy, nightclub) and class venues are left out: a visit only makes sense
// with a show or a session, and those arrive as events. Food categories are
// separate, so the one-stop-per-category rule allows at most one meal and
// one coffee per plan.
var placeCategories = map[string]bool{
	"park": true, "hike": true, "bar": true, "landmark": true, "museum": true,
	"gallery": true, "garden": true, "shopping": true, "rec_venue": true,
	"zoo_aquarium": true, "market": true, "viewpoint": true, "tour": true,
	"restaurant": true, "cafe": true, "bakery": true, "dessert": true,
	"food_hall": true, "brewery": true,
}

// PlaceCategories lists the place categories the optimizer can schedule.
func PlaceCategories() []string {
	out := make([]string, 0, len(placeCategories))
	for c := range placeCategories {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// SeriesKey groups activities that are one experience: every timed-entry
// slot of an exhibition (same venue and name) shares a key; each place is
// its own. BuildNodes refines this further for duplicate listings.
func SeriesKey(a *models.Activity) string {
	if a.Kind == "place" {
		return "place|" + a.ID.Hex()
	}
	v := norm(ptrStr(a.VenueName))
	if v == "" {
		if p, ok := activityPoint(*a); ok {
			v = travel.LocKey(p)
		}
	}
	return "event|" + v + "|" + norm(a.Name)
}

// Two activities within this distance, where the place's name matches the
// event's venue, are the same venue.
const sameVenueKm = 0.05

// Two event listings starting at the same time within this distance are
// the same event.
const duplicateListingKm = 0.1

// BuildNodes turns candidates into nodes that fit the window. Activities
// that can't fit are returned as drops with a reason; they could never be
// part of any itinerary.
func BuildNodes(w Window, acts []models.Activity, cfg Config) ([]Node, []Drop) {
	var nodes []Node
	var drops []Drop
	drop := func(a *models.Activity, reason string) {
		drops = append(drops, Drop{ActivityID: a.ID.Hex(), Reason: reason})
	}

	seen := map[string]bool{}
	for i := range acts {
		a := &acts[i]
		if id := a.ID.Hex(); seen[id] {
			continue
		} else {
			seen[id] = true
		}
		loc, ok := activityPoint(*a)
		if !ok {
			drop(a, "no_location")
			continue
		}
		base := Node{
			Act:       a,
			Loc:       loc,
			Utility:   utility(a, cfg),
			CostCents: costCents(a),
		}
		if w.BudgetCents > 0 && base.CostCents > w.BudgetCents {
			drop(a, "over_budget")
			continue
		}

		var made []Node
		var reason string
		if a.Kind == "place" {
			made, reason = placeNodes(w, base, cfg)
		} else {
			made, reason = eventNodes(w, base, cfg)
		}
		if len(made) == 0 {
			drop(a, reason)
			continue
		}
		nodes = append(nodes, made...)
	}

	nodes = assignSeries(nodes)
	nodes, capped := capSeries(nodes, 64)
	for _, a := range capped {
		drops = append(drops, Drop{ActivityID: a, Reason: "series_cap"})
	}
	assignBits(nodes)
	sortNodes(nodes)
	return nodes, drops
}

func eventNodes(w Window, n Node, cfg Config) ([]Node, string) {
	a := n.Act
	if a.Start == nil {
		return nil, "no_start_time"
	}
	start := a.Start.UTC()
	visit, visit75 := durations(a, cfg)

	// A long published span is a window the user can drop into, not a
	// visit length (e.g. a 31-hour convention listed as fixed_start).
	isWindow := a.Attendance != nil && *a.Attendance == "drop_in"
	if a.End != nil && a.End.Sub(start) > cfg.MaxDuration {
		isWindow = true
	}

	if !isWindow {
		end := start.Add(visit)
		if a.End != nil && a.End.After(start) {
			end = a.End.UTC()
			visit75 = end.Sub(start)
		}
		if start.Before(w.From) || end.After(w.BackBy) {
			return nil, "outside_window"
		}
		n.Start, n.End, n.P75End = start, end, start.Add(maxDur(visit75, end.Sub(start)))
		return []Node{n}, ""
	}

	winEnd := start.Add(visit)
	if a.End != nil && a.End.After(start) {
		winEnd = a.End.UTC()
	}
	iv := Interval{Start: maxTime(start, w.From), End: minTime(winEnd, w.BackBy)}
	slots := slotNodes(n, []Interval{iv}, visit, cfg)
	if len(slots) == 0 {
		return nil, "outside_window"
	}
	return slots, ""
}

func placeNodes(w Window, n Node, cfg Config) ([]Node, string) {
	a := n.Act
	if !placeCategories[a.Category] {
		return nil, "category_excluded"
	}
	loc := w.TZ
	if a.Timezone != "" {
		if l, err := time.LoadLocation(a.Timezone); err == nil {
			loc = l
		}
	}
	open, ok := OpenIntervals(a.WeeklyHours, a.Category, loc, w.From, w.BackBy)
	if !ok {
		return nil, "no_hours"
	}
	visit, _ := durations(a, cfg)
	n.Utility *= cfg.PlaceWeight
	slots := slotNodes(n, open, visit, cfg)
	if len(slots) == 0 {
		return nil, "closed_during_window"
	}
	return slots, ""
}

// slotNodes places visits of length `visit` inside the open intervals: one
// at the start of each interval, then on the slot grid (:00 and :30 by
// default), up to cfg.MaxSlots in total. A visit that doesn't fit whole
// shrinks to the interval if that still leaves MinDuration.
func slotNodes(n Node, open []Interval, visit time.Duration, cfg Config) []Node {
	var out []Node
	for _, iv := range open {
		length := iv.End.Sub(iv.Start)
		v := visit
		if length < v {
			if length < cfg.MinDuration {
				continue
			}
			v = length
		}
		t := iv.Start
		for !t.Add(v).After(iv.End) && len(out) < cfg.MaxSlots {
			s := n
			// A flexible visit can be cut short, so it never runs late.
			s.Start, s.End, s.P75End = t, t.Add(v), t.Add(v)
			s.Flexible = true
			out = append(out, s)
			next := t.Truncate(cfg.SlotStep).Add(cfg.SlotStep)
			if !next.After(t) {
				next = t.Add(cfg.SlotStep)
			}
			t = next
		}
	}
	return out
}

func durations(a *models.Activity, cfg Config) (visit, visit75 time.Duration) {
	visit, visit75 = cfg.DefaultDuration, cfg.DefaultDuration
	if a.Duration != nil {
		if m := a.Duration.MedianMin; m > 0 && !math.IsInf(m, 0) && !math.IsNaN(m) {
			visit = clampDur(time.Duration(math.Min(m, 1e6)*float64(time.Minute)), cfg)
		}
		visit75 = visit
		if p := a.Duration.P75Min; p > 0 && !math.IsInf(p, 0) && !math.IsNaN(p) {
			visit75 = clampDur(time.Duration(math.Min(p, 1e6)*float64(time.Minute)), cfg)
		}
	}
	return visit, maxDur(visit, visit75)
}

func clampDur(d time.Duration, cfg Config) time.Duration {
	if d < cfg.MinDuration {
		return cfg.MinDuration
	}
	if d > cfg.MaxDuration {
		return cfg.MaxDuration
	}
	return d
}

// utility maps ranker output to 0..1: scores at or below the baseline are
// worth nothing, and the range above it is stretched back to 0..1, so a
// higher bar cuts weak stops without shrinking good ones against travel and
// wait penalties. The LLM rerank score (0-4) is preferred when present; the
// model score is used otherwise; unscored items get cfg.DefaultUtility.
func utility(a *models.Activity, cfg Config) float64 {
	base := cfg.DefaultUtility
	switch {
	case a.RerankScore != nil:
		base = *a.RerankScore / 4
	case a.Score != nil:
		base = *a.Score
	}
	base = math.Max(0, math.Min(1, base))
	if cfg.Tau >= 1 {
		return 0
	}
	return math.Max(0, (base-cfg.Tau)/(1-cfg.Tau))
}

func costCents(a *models.Activity) int64 {
	if a.Price == nil {
		return 0
	}
	if a.Price.Min != nil && *a.Price.Min > 0 {
		return int64(math.Round(*a.Price.Min * 100))
	}
	return a.Price.Cents
}

// assignSeries gives each node its series key: venue + name for events, so
// every timed-entry slot of one exhibition collides; the place's own id for
// places, unless the place is the venue of an event, in which case it takes
// that event's key.
func assignSeries(nodes []Node) []Node {
	type venue struct {
		loc  travel.Point
		name string
		key  string
	}
	type listing struct {
		start time.Time
		loc   travel.Point
		venue string
		name  string
		key   string
	}
	var venues []venue
	var listings []listing
	for i := range nodes {
		a := nodes[i].Act
		if a.Kind == "place" {
			continue
		}
		key := SeriesKey(a)
		// The same event is often listed twice ("Michelle Malone" and
		// "Michelle Malone Band w/ …" at 19:00 in one venue, or one tour
		// geocoded to two points). Listings with the same start time are the
		// same event if they have the same name, or are at the same venue
		// (same venue name, or within 100 m) and one name extends the other.
		// Different shows sharing a coordinate (Broadway theatres) stay
		// separate.
		if a.Start != nil {
			name := norm(a.Name)
			for _, l := range listings {
				if !l.start.Equal(*a.Start) {
					continue
				}
				sameSpot := (l.venue != "" && l.venue == norm(ptrStr(a.VenueName))) ||
					travel.HaversineKm(l.loc, nodes[i].Loc) <= duplicateListingKm
				related := strings.HasPrefix(l.name, name) || strings.HasPrefix(name, l.name)
				if l.name == name || (sameSpot && related) {
					key = l.key
					break
				}
			}
			listings = append(listings, listing{start: *a.Start, loc: nodes[i].Loc, venue: norm(ptrStr(a.VenueName)), name: name, key: key})
		}
		nodes[i].SeriesKey = key
		venues = append(venues, venue{loc: nodes[i].Loc, name: norm(ptrStr(a.VenueName)), key: key})
	}
	for i := range nodes {
		a := nodes[i].Act
		if a.Kind != "place" {
			continue
		}
		nodes[i].SeriesKey = SeriesKey(a)
		name := norm(a.Name)
		for _, v := range venues {
			if v.name != "" && v.name == name && travel.HaversineKm(v.loc, nodes[i].Loc) <= sameVenueKm {
				nodes[i].SeriesKey = v.key
				break
			}
		}
	}
	return nodes
}

// capSeries keeps the `limit` series with the best utility; paths track
// series in a 64-bit mask.
func capSeries(nodes []Node, limit int) ([]Node, []string) {
	best := map[string]float64{}
	for _, n := range nodes {
		if u, ok := best[n.SeriesKey]; !ok || n.Utility > u {
			best[n.SeriesKey] = n.Utility
		}
	}
	if len(best) <= limit {
		return nodes, nil
	}
	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if best[keys[i]] != best[keys[j]] {
			return best[keys[i]] > best[keys[j]]
		}
		return keys[i] < keys[j]
	})
	keep := map[string]bool{}
	for _, k := range keys[:limit] {
		keep[k] = true
	}
	var out []Node
	droppedIDs := map[string]bool{}
	for _, n := range nodes {
		if keep[n.SeriesKey] {
			out = append(out, n)
		} else {
			droppedIDs[n.Act.ID.Hex()] = true
		}
	}
	var dropped []string
	for id := range droppedIDs {
		dropped = append(dropped, id)
	}
	sort.Strings(dropped)
	return out, dropped
}

func assignBits(nodes []Node) {
	series := map[string]int{}
	cats := map[string]int{}
	for i := range nodes {
		k := nodes[i].SeriesKey
		if _, ok := series[k]; !ok {
			series[k] = len(series)
		}
		nodes[i].series = series[k]

		c := strings.ToLower(nodes[i].Act.Category)
		nodes[i].category = -1
		if unlimitedCategories[c] {
			continue
		}
		if _, ok := cats[c]; !ok {
			if len(cats) >= 64 {
				continue
			}
			cats[c] = len(cats)
		}
		nodes[i].category = cats[c]
	}
}

func sortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if !nodes[i].Start.Equal(nodes[j].Start) {
			return nodes[i].Start.Before(nodes[j].Start)
		}
		return nodes[i].End.Before(nodes[j].End)
	})
}

func norm(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
