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
// comedy) and class venues are left out: a visit only makes sense with a
// show or a session, and those arrive as events. Food and drink places are
// drop-in venues; each food category is its own, so the one-stop-per-
// category rule allows at most one meal and one coffee per plan. Bars,
// nightclubs, breweries and tours need real hours (no default).
var placeCategories = map[string]bool{
	"park": true, "hike": true, "bar": true, "landmark": true, "museum": true,
	"gallery": true, "garden": true, "shopping": true, "rec_venue": true,
	"zoo_aquarium": true, "market": true, "viewpoint": true, "tour": true,
	"restaurant": true, "cafe": true, "bakery": true, "dessert": true,
	"food_hall": true, "brewery": true, "nightclub": true,
}

// PlaceCategories lists the place categories the optimizer schedules, sorted,
// so a candidate query can restrict places to what could become a stop.
func PlaceCategories() []string {
	out := make([]string, 0, len(placeCategories))
	for c := range placeCategories {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// IsPlaceCategory reports whether places of this category can be scheduled.
func IsPlaceCategory(category string) bool {
	return placeCategories[category]
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
	if bonus := cfg.stopBonus(w.Pace); bonus > 0 {
		for i := range nodes {
			if nodes[i].Utility > 0 {
				nodes[i].Utility += bonus
			}
		}
	}

	nodes = assignSeries(nodes)
	nodes, capped := capSeries(nodes, cfg.seriesCap())
	for _, a := range capped {
		drops = append(drops, Drop{ActivityID: a, Reason: "series_cap"})
	}
	assignBits(nodes)
	sortNodes(nodes)
	return nodes, drops
}

func eventNodes(w Window, n Node, cfg Config) ([]Node, string) {
	st, ok := StayFor(n.Act, cfg)
	if !ok {
		return nil, "no_start_time"
	}
	switch st.Kind {
	case StayWindow:
		visit, _ := durations(n.Act, cfg)
		iv := Interval{Start: maxTime(st.Start, w.From), End: minTime(st.End, w.BackBy)}
		slots := slotNodes(w, n, []Interval{iv}, visit, st.MinStay, true, cfg)
		if len(slots) == 0 {
			return nil, "outside_window"
		}
		return slots, ""
	case StayClipped:
		stays := clippedNodes(w, n, st, cfg)
		if len(stays) == 0 {
			return nil, "outside_window"
		}
		return stays, ""
	}
	start, end := st.Start, st.End
	_, visit75 := durations(n.Act, cfg)
	if n.Act.End != nil && n.Act.End.After(start) {
		visit75 = end.Sub(start)
	}
	if start.Before(w.From) || end.After(w.BackBy) {
		return nil, "outside_window"
	}
	n.Start, n.End, n.P75End = start, end, start.Add(maxDur(visit75, end.Sub(start)))
	return []Node{n}, ""
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
	// The shortest acceptable visit is half the usual one, kept between
	// MinDuration and ShortVisit (a long hike can be a 30-minute walk); a
	// meal or a tour needs three quarters of its length.
	minVisit, short := clampDurTo(visit/2, cfg.MinDuration, ShortVisit), true
	if wholeVisitCategories[a.Category] {
		minVisit, short = maxDur(cfg.MinDuration, visit*3/4), false
	}
	if minVisit > visit {
		minVisit = visit
	}
	slots := slotNodes(w, n, open, visit, minVisit, short, cfg)
	if len(slots) == 0 {
		return nil, "closed_during_window"
	}
	return slots, ""
}

// Place categories visited (nearly) whole: a tour runs its route, a meal
// takes the time it takes. They never come in a short form and are cut to
// three quarters of their length at most.
var wholeVisitCategories = map[string]bool{"tour": true, "restaurant": true}

// slotNodes places visits of length `visit` inside the open intervals. The
// starts are, in order of priority up to cfg.MaxSlots: the earliest arrival
// from the start point, the latest starts that still reach the end point
// by back-by, the start of each interval, then the slot grid (:00 and :30
// by default). A visit that would run past the interval's end is cut there
// if that still leaves minVisit. With short set, a visit of an hour or
// more also comes in a short form (half, at least ShortVisit). A visit
// shorter than `visit` keeps 80% of the utility plus the rest in
// proportion to its length.
func slotNodes(w Window, n Node, open []Interval, visit, minVisit time.Duration, short bool, cfg Config) []Node {
	var arrive, leave time.Time
	if w.Start != nil {
		arrive = ceilTo(w.From.Add(travel.Estimate(*w.Start, n.Loc, w.Mode).Duration), anchorStep)
	}
	if w.End != nil {
		leave = w.BackBy.Add(-travel.Estimate(n.Loc, *w.End, w.Mode).Duration).Truncate(anchorStep)
	}
	lengths := []time.Duration{visit}
	if s := maxDur(ShortVisit, visit/2); short && visit >= 2*ShortVisit && s < visit && s >= minVisit {
		lengths = append(lengths, s)
	}
	var out []Node
	used := 0
	for _, iv := range open {
		if iv.End.Sub(iv.Start) < minVisit {
			continue
		}
		var starts []time.Time
		add := func(t time.Time) {
			if t.Before(iv.Start) || t.Add(minVisit).After(iv.End) {
				return
			}
			for _, u := range starts {
				if u.Equal(t) {
					return
				}
			}
			starts = append(starts, t)
		}
		if !arrive.IsZero() {
			add(arrive)
		}
		if !leave.IsZero() {
			for _, l := range lengths {
				add(leave.Add(-l))
			}
		}
		add(iv.Start)
		for t := iv.Start.Truncate(cfg.SlotStep).Add(cfg.SlotStep); !t.Add(minVisit).After(iv.End); t = t.Add(cfg.SlotStep) {
			add(t)
		}
		if room := cfg.MaxSlots - used; len(starts) > room {
			starts = starts[:maxInt(room, 0)]
		}
		used += len(starts)
		sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
		for _, t := range starts {
			var ends []time.Time
			for _, l := range lengths {
				e := minTime(t.Add(l), iv.End)
				if e.Sub(t) < minVisit || (len(ends) > 0 && e.Equal(ends[len(ends)-1])) {
					continue
				}
				ends = append(ends, e)
				s := n
				// A flexible visit can be cut short, so it never runs late.
				s.Start, s.End, s.P75End = t, e, e
				s.Flexible = true
				s.Utility = n.Utility * (0.8 + 0.2*math.Min(1, float64(e.Sub(t))/float64(visit)))
				out = append(out, s)
			}
		}
	}
	return out
}

func clampDurTo(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// ShortVisit is the shortest "short form" of a flexible visit.
const ShortVisit = 30 * time.Minute

// Anchored starts (arrival from the start point, departure to the end
// point) are rounded to this step.
const anchorStep = 5 * time.Minute

func ceilTo(t time.Time, step time.Duration) time.Time {
	r := t.Truncate(step)
	if r.Before(t) {
		r = r.Add(step)
	}
	return r
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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

// utility maps ranker output to 0..1: scores at or below the baseline Tau
// are worth nothing, and the range above it is stretched back to 0..1, so a
// higher bar cuts weak stops without shrinking good ones against travel and
// wait penalties. With cfg.Utility set, the caller supplies the base;
// otherwise the LLM rerank score (0-4) is preferred when present, the model
// score is used next, and unscored items get cfg.DefaultUtility.
func utility(a *models.Activity, cfg Config) float64 {
	base := cfg.DefaultUtility
	switch {
	case cfg.Utility != nil:
		base = cfg.Utility(a)
	case a.RerankScore != nil:
		base = *a.RerankScore / 4
	case a.Score != nil:
		base = *a.Score
	}
	if math.IsNaN(base) {
		base = cfg.DefaultUtility
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
// series in a 128-bit mask.
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
			if len(cats) >= maxMaskBits {
				continue
			}
			cats[c] = len(cats)
		}
		nodes[i].category = cats[c]
	}
}

// sortNodes orders by start, then end, then activity id and fixed-before-
// flexible, so the same candidates always give the same graph whatever
// order they arrived in.
func sortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := &nodes[i], &nodes[j]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if !a.End.Equal(b.End) {
			return a.End.Before(b.End)
		}
		if ai, bi := a.Act.ID.Hex(), b.Act.ID.Hex(); ai != bi {
			return ai < bi
		}
		return !a.Flexible && b.Flexible
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
