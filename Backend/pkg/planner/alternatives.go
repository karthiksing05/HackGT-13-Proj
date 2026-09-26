package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AlternativesInput is the app's AlternativesRequest.
type AlternativesInput struct {
	OptionID  string
	StopID    string
	StopOrder []string
}

// Alternative is a stop that could take another's place, best first.
type Alternative struct {
	Stop         Stop    `json:"stop"`
	Reason       string  `json:"reason"`
	FitScore     float64 `json:"fit_score"`
	DistanceKm   float64 `json:"distance_km"`
	SlotShiftMin int     `json:"slot_shift_min"`
}

const (
	altSlotPad      = 30 * time.Minute
	altFlexShiftMax = 30 * time.Minute
	altLimit        = 60
	altMaxResults   = 5
	altMinResults   = 3
)

// Alternatives is §6.4: the same kind of thing as stop S, reachable in S's
// slot between its neighbours, not already in the plan, scored against the
// pool's query vector.
func (p *Planner) Alternatives(ctx context.Context, in AlternativesInput) ([]Alternative, error) {
	pool, opt, err := p.loadPool(ctx, in.OptionID)
	if err != nil {
		return nil, err
	}
	stops, err := resolveStops(pool, opt, in.StopOrder)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, s := range stops {
		if s.ID == in.StopID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, &UnknownStopError{ID: in.StopID}
	}
	target := stops[idx]
	w := poolWindow(pool)
	tz := w.TZ

	// In-plan activities and series are excluded.
	exclude := map[string]bool{}
	series := map[string]bool{}
	for _, s := range stops {
		exclude[s.ActivityID] = true
		if s.SeriesKey != "" {
			series[s.SeriesKey] = true
		}
	}

	// The slot and the anchor.
	slot := TimeSlot{From: maxTime(target.Arrive.Add(-altSlotPad), w.From), To: minTime(target.Depart.Add(altSlotPad), w.BackBy)}
	if target.Arrive.IsZero() {
		slot = TimeSlot{From: w.From, To: w.BackBy}
	}
	anchor := travel.Point{Lat: target.Place.Lat, Lng: target.Place.Lng}
	if !target.Place.HasCoord {
		prev, next := neighbourPoints(stops, idx, w)
		anchor = travel.Point{Lat: (prev.Lat + next.Lat) / 2, Lng: (prev.Lng + next.Lng) / 2}
	}

	// Query: the pool's filters, centred on the anchor, matching the stop's
	// category or tags, inside the slot.
	spec := poolSpec(pool, w)
	q := baseQuery(&spec, p.Cfg, w.MaxLegKm)
	q.Center, q.RadiusKm = anchor, w.MaxLegKm
	q.From, q.To = slot.From, slot.To
	q.IncludeCategories = []string{target.Category}
	q.AnyTags = target.Tags
	q.LimitEvents, q.LimitPlaces = altLimit, altLimit
	for id := range exclude {
		q.ExcludeIDs = append(q.ExcludeIDs, id)
	}
	sort.Strings(q.ExcludeIDs)
	if !q.To.After(q.From) {
		return []Alternative{}, nil
	}

	events, places, err := p.Source.FindCandidates(ctx, q)
	if err != nil {
		return nil, err
	}
	drops := map[string]int{}
	cands := Feasible(&spec, &q, p.Cfg.Itinerary, append(events, places...), drops, feasibleOpts{minOpen: p.Cfg.Itinerary.MinDuration})
	var kept []*Candidate
	for _, c := range cands {
		if exclude[c.ID] {
			continue
		}
		// Tag matches need at least two shared tags unless the category matches.
		if c.Act.Category != target.Category && sharedTags(c.Act.Tags, target.Tags) < 2 {
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		return []Alternative{}, nil
	}

	run := &Run{Spec: spec, Cfg: p.Cfg, ItCfg: p.Cfg.Itinerary, Pool: NewPool(), User: &UserContext{ID: pool.UserID}, Log: PlanRun{Counts: CountLog{Drops: map[string]int{}}, Timings: map[string]int64{}}}
	if err := p.fetchEmbeddings(ctx, run, kept); err != nil {
		return nil, err
	}
	qv := QueryVector{Q: pool.QueryVector, Neg: pool.NegVector, HasNeg: pool.HasNeg, Source: pool.QuerySource}
	if len(qv.Q) == 0 {
		qv.Source = "none"
	}

	// Schedule each candidate at its earliest feasible grid start in the
	// slot, honouring the neighbours.
	prevEnd, nextStart, nextFlexible := neighbourTimes(stops, idx, w)
	prevPt, nextPt := neighbourPoints(stops, idx, w)
	type scored struct {
		c     *Candidate
		stop  Stop
		score float64
		shift int
		dist  float64
	}
	var results []scored
	for _, c := range kept {
		scoreForShortlist(c, qv, spec.Facets, p.Cfg)
		start, end, shift, ok := p.scheduleAlternative(ctx, c, slot, w, prevEnd, prevPt, nextStart, nextPt, nextFlexible)
		if !ok {
			continue
		}
		cos := clamp01(c.Cos / 0.6)
		if !c.HasVec {
			cos = c.Prior
		}
		fit := cos
		if ps, ok := pool.Scores[c.ID]; ok && ps.ML != nil {
			fit = 0.7*cos + 0.3*clamp01(*ps.ML)
		}
		s := stopFromActivity(&c.Act, tz)
		s.ID = AltStopID(c.ID, idx)
		s.Order = idx
		s.Arrive, s.Depart = start.UTC(), end.UTC()
		s.DurationMinutes = int(end.Sub(start).Minutes())
		s.Flexible = c.Kind == "place" || (c.Act.Attendance != nil && *c.Act.Attendance == "drop_in")
		s.SeriesKey = altSeriesKey(&c.Act)
		s.Utility = round5(fit)
		if series[s.SeriesKey] {
			continue
		}
		results = append(results, scored{c: c, stop: s, score: fit, shift: shift, dist: travel.HaversineKm(anchor, c.Point)})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].c.ID < results[j].c.ID
	})
	if len(results) > altMaxResults {
		results = results[:altMaxResults]
	}

	out := make([]Alternative, 0, len(results))
	var records []Stop
	for _, r := range results {
		out = append(out, Alternative{
			Stop:         r.stop,
			Reason:       alternativeReason(r.stop, target, r.dist, tz),
			FitScore:     round5(r.score),
			DistanceKm:   round2(r.dist),
			SlotShiftMin: r.shift,
		})
		records = append(records, r.stop)
	}
	if len(records) > 0 {
		if err := p.Pools.AddAlternatives(ctx, pool.ID, records); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

func sharedTags(a, b []string) int {
	n := 0
	for _, t := range a {
		if containsString(b, t) {
			n++
		}
	}
	return n
}

// poolSpec rebuilds enough of the spec for queries and feasibility.
func poolSpec(pool *PlanPool, w itinerary.Window) PlanSpec {
	return PlanSpec{
		UserID: pool.UserID, Catalog: pool.Catalog, City: pool.Spec.City, TZ: w.TZ,
		Start: w.Start, End: w.End, From: w.From, BackBy: w.BackBy,
		Mode: w.Mode, DriveLabel: w.DriveLabel, Range: pool.Spec.Range, MaxLegKm: w.MaxLegKm,
		Budget: pool.Spec.Budget, Pace: w.Pace, Who: pool.Spec.Who,
		MoodText: pool.Spec.MoodText, QuickPicks: pool.Spec.QuickPicks, Facets: pool.Spec.Facets,
		Hard: pool.Spec.Hard, AgeBracket: pool.Spec.AgeBracket, AvoidTags: pool.Spec.AvoidTags, Flexible: pool.Spec.Flexible,
	}
}

func neighbourPoints(stops []Stop, idx int, w itinerary.Window) (prev, next travel.Point) {
	prev, next = *w.Start, *w.End
	if idx > 0 {
		prev = travel.Point{Lat: stops[idx-1].Place.Lat, Lng: stops[idx-1].Place.Lng}
	}
	if idx+1 < len(stops) {
		next = travel.Point{Lat: stops[idx+1].Place.Lat, Lng: stops[idx+1].Place.Lng}
	}
	return prev, next
}

func neighbourTimes(stops []Stop, idx int, w itinerary.Window) (prevEnd, nextStart time.Time, nextFlexible bool) {
	prevEnd, nextStart, nextFlexible = w.From, w.BackBy, false
	if idx > 0 && !stops[idx-1].Depart.IsZero() {
		prevEnd = stops[idx-1].Depart
	}
	if idx+1 < len(stops) && !stops[idx+1].Arrive.IsZero() {
		nextStart, nextFlexible = stops[idx+1].Arrive, stops[idx+1].Flexible
	}
	return
}

// scheduleAlternative finds the earliest grid start inside the slot that
// the previous stop can reach and from which the next stop is reachable.
// A flexible next stop may shift by up to altFlexShiftMax.
func (p *Planner) scheduleAlternative(ctx context.Context, c *Candidate, slot TimeSlot, w itinerary.Window,
	prevEnd time.Time, prevPt travel.Point, nextStart time.Time, nextPt travel.Point, nextFlexible bool) (start, end time.Time, shiftMin int, ok bool) {
	itCfg := p.Cfg.Itinerary
	legs := lookupLegs(ctx, p.Travel, []travel.Pair{{From: prevPt, To: c.Point}, {From: c.Point, To: nextPt}}, w.Mode)
	inLeg := legs[travel.Pair{From: prevPt, To: c.Point}]
	outLeg := legs[travel.Pair{From: c.Point, To: nextPt}]
	earliest := maxTime(slot.From, prevEnd.Add(inLeg.Duration+itCfg.Buffer))
	if travel.HaversineKm(prevPt, c.Point) > w.MaxLegKm*2 || travel.HaversineKm(c.Point, nextPt) > w.MaxLegKm*2 {
		return start, end, 0, false
	}
	latestEnd := nextStart.Add(-outLeg.Duration - itCfg.Buffer)
	if nextFlexible {
		latestEnd = latestEnd.Add(altFlexShiftMax)
	}
	latestEnd = minTime(latestEnd, w.BackBy.Add(-outLeg.Duration))
	p75, median := visitLengths(&c.Act)
	visit := median
	if visit < itCfg.MinDuration {
		visit = itCfg.MinDuration
	}
	if visit > itCfg.MaxDuration {
		visit = itCfg.MaxDuration
	}
	_ = p75

	a := &c.Act
	if a.Kind == "event" && !(a.Attendance != nil && *a.Attendance == "drop_in") {
		start = a.Start.UTC()
		end = start.Add(visit)
		if a.End != nil && a.End.After(start) {
			end = a.End.UTC()
		}
		if start.Before(earliest) || end.After(latestEnd) {
			return start, end, 0, false
		}
	} else {
		intervals := []itinerary.Interval{{Start: slot.From, End: slot.To}}
		if a.Kind == "place" {
			loc := w.TZ
			if a.Timezone != "" {
				if l, err := time.LoadLocation(a.Timezone); err == nil {
					loc = l
				}
			}
			open, okHours := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, slot.From, slot.To)
			if !okHours {
				return start, end, 0, false
			}
			intervals = open
		} else {
			// drop-in event: its own span clipped to the slot
			s := a.Start.UTC()
			e := s.Add(visit)
			if a.End != nil && a.End.After(s) {
				e = a.End.UTC()
			}
			intervals = []itinerary.Interval{{Start: maxTime(s, slot.From), End: minTime(e, slot.To)}}
		}
		found := false
		for _, iv := range intervals {
			t := maxTime(iv.Start, earliest)
			if !t.Equal(iv.Start) {
				t = gridUp(t, itCfg.SlotStep)
			}
			v := visit
			if iv.End.Sub(t) < v {
				v = iv.End.Sub(t)
			}
			if v < itCfg.MinDuration {
				continue
			}
			start, end = t, t.Add(v)
			found = true
			break
		}
		if !found || end.After(latestEnd) {
			return start, end, 0, false
		}
	}
	if !nextFlexible {
		return start, end, 0, true
	}
	if over := end.Add(outLeg.Duration + itCfg.Buffer).Sub(nextStart); over > 0 {
		shiftMin = int(over.Minutes())
	}
	return start, end, shiftMin, true
}

func gridUp(t time.Time, step time.Duration) time.Time {
	r := t.Truncate(step)
	if r.Before(t) {
		r = r.Add(step)
	}
	return r
}

func lookupLegs(ctx context.Context, tp travel.Provider, pairs []travel.Pair, mode travel.Mode) map[travel.Pair]travel.Leg {
	var legs map[travel.Pair]travel.Leg
	if tp != nil {
		legs, _ = tp.Legs(ctx, pairs, mode)
	}
	if legs == nil {
		legs = map[travel.Pair]travel.Leg{}
	}
	for _, p := range pairs {
		if _, ok := legs[p]; !ok {
			legs[p] = travel.Estimate(p.From, p.To, mode)
		}
	}
	return legs
}

func normSpace(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// altSeriesKey mirrors the optimizer's series keys well enough to keep an
// alternative from repeating an in-plan series.
func altSeriesKey(a *models.Activity) string {
	if a.Kind == "place" {
		return "place|" + a.ID.Hex()
	}
	v := ""
	if a.VenueName != nil {
		v = normSpace(*a.VenueName)
	}
	if v == "" {
		if p, ok := activityPoint(a); ok {
			v = travel.LocKey(p)
		}
	}
	return "event|" + v + "|" + normSpace(a.Name)
}

// Phrases for the reason line, by category.
var reasonPhrases = map[string]string{
	"gallery": "art to see", "museum": "art to see",
	"park": "time outside", "garden": "time outside", "hike": "time outside", "viewpoint": "time outside",
	"restaurant": "good food", "cafe": "good food", "market": "good food",
	"bar": "drinks", "nightclub": "late-night music", "live_music": "live music", "comedy": "laughs",
	"rec_venue": "games", "class_workshop": "something hands-on", "tour": "sights", "landmark": "sights",
	"community_event": "people to meet", "festival": "people to meet",
}

// alternativeReason is "Also {phrase} · {miles} mi away" plus the price
// when the tier differs and the start time for events.
func alternativeReason(alt, target Stop, distKm float64, tz *time.Location) string {
	phrase, ok := reasonPhrases[alt.Category]
	if !ok {
		phrase = "a similar vibe"
	}
	s := fmt.Sprintf("Also %s · %.1f mi away", phrase, distKm/kmPerMile)
	if alt.TierKnown && (!target.TierKnown || alt.Tier != target.Tier) {
		s += " · " + tierSymbol(alt.Tier)
	}
	if alt.Kind == "event" && !alt.Arrive.IsZero() && tz != nil {
		s += " · starts " + alt.Arrive.In(tz).Format("3:04 PM")
	}
	return s
}
