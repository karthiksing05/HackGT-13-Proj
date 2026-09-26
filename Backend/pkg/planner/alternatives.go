package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"sort"
	"time"
)

// AlternativesInput is the app's AlternativesRequest.
type AlternativesInput struct {
	UserID    string // the caller; another user's pool reads as expired
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
)

// Alternatives is §6.4: the same kind of thing as stop S, reachable in S's
// slot between its neighbours, not already in the plan, scored against the
// pool's query vector. It returns up to five, best first; fewer (or none)
// when the catalog has nothing else that fits.
func (p *Planner) Alternatives(ctx context.Context, in AlternativesInput) ([]Alternative, error) {
	pool, opt, err := p.loadPool(ctx, in.UserID, in.OptionID)
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
	prev, next := neighbours(stops, idx, w)
	anchor := travel.Point{Lat: target.Place.Lat, Lng: target.Place.Lng}
	if !target.Place.HasCoord {
		anchor = travel.Point{Lat: (prev.pt.Lat + next.pt.Lat) / 2, Lng: (prev.pt.Lng + next.pt.Lng) / 2}
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
		start, end, shift, ok := p.scheduleAlternative(ctx, c, slot, w, prev, next)
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
		if s.Flexible {
			s.OpenSlots = openSlotsFor(&c.Act, tz, w.From, w.BackBy)
		}
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

// neighbour is the stop (or depot) on one side of the stop being replaced.
type neighbour struct {
	pt       travel.Point
	at       time.Time // previous: when it ends; next: when it starts
	flexible bool
}

// neighbours finds what comes before and after stops[idx].
func neighbours(stops []Stop, idx int, w itinerary.Window) (prev, next neighbour) {
	prev = neighbour{pt: *w.Start, at: w.From}
	next = neighbour{pt: *w.End, at: w.BackBy}
	if idx > 0 {
		s := stops[idx-1]
		prev = neighbour{pt: travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}, at: s.Depart, flexible: s.Flexible}
		if s.Depart.IsZero() {
			prev.at = w.From
		}
	}
	if idx+1 < len(stops) {
		s := stops[idx+1]
		next = neighbour{pt: travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}, at: s.Arrive, flexible: s.Flexible}
		if s.Arrive.IsZero() {
			next.at = w.BackBy
		}
	}
	return prev, next
}

// scheduleAlternative finds the earliest grid start inside the slot that
// the previous stop can reach and from which the next stop is reachable,
// within the leg limits. A flexible visit is shortened to fit (never below
// MinDuration); a flexible next stop may shift by up to altFlexShiftMax.
func (p *Planner) scheduleAlternative(ctx context.Context, c *Candidate, slot TimeSlot, w itinerary.Window, prev, next neighbour) (start, end time.Time, shiftMin int, ok bool) {
	itCfg := p.Cfg.Itinerary
	// Every leg, to and from the user's start and end points too, is
	// within MaxLegKm (§7).
	if travel.HaversineKm(prev.pt, c.Point) > w.MaxLegKm || travel.HaversineKm(c.Point, next.pt) > w.MaxLegKm {
		return start, end, 0, false
	}
	legs := lookupLegs(ctx, p.Travel, []travel.Pair{{From: prev.pt, To: c.Point}, {From: c.Point, To: next.pt}}, w.Mode)
	inLeg := legs[travel.Pair{From: prev.pt, To: c.Point}]
	outLeg := legs[travel.Pair{From: c.Point, To: next.pt}]
	earliest := maxTime(slot.From, prev.at.Add(inLeg.Duration+itCfg.Buffer))
	latestEnd := next.at.Add(-outLeg.Duration - itCfg.Buffer)
	if next.flexible {
		latestEnd = latestEnd.Add(altFlexShiftMax)
	}
	latestEnd = minTime(latestEnd, w.BackBy.Add(-outLeg.Duration))
	_, median := visitLengths(&c.Act)
	visit := median
	if visit < itCfg.MinDuration {
		visit = itCfg.MinDuration
	}
	if visit > itCfg.MaxDuration {
		visit = itCfg.MaxDuration
	}

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
			if t.After(iv.Start) {
				t = gridUp(t, itCfg.SlotStep)
			}
			v := minDuration(visit, minTime(iv.End, latestEnd).Sub(t))
			if v < itCfg.MinDuration {
				continue
			}
			start, end = t, t.Add(v)
			found = true
			break
		}
		if !found {
			return start, end, 0, false
		}
	}
	if !next.flexible {
		return start, end, 0, true
	}
	if over := end.Add(outLeg.Duration + itCfg.Buffer).Sub(next.at); over > 0 {
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

// altSeriesKey is the optimizer's series key, so an alternative never
// repeats an in-plan series.
func altSeriesKey(a *models.Activity) string {
	return itinerary.SeriesKey(a)
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
