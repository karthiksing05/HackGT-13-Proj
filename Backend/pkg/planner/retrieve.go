package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

// Radius of the phase-A search around the start point: enough for a plan
// of MaxStops legs, bounded to something a query can serve.
func radiusFor(spec *PlanSpec, cfg Config, maxLegKm float64) float64 {
	pace := cfg.Itinerary.Pace(spec.Pace)
	r := maxLegKm * (1 + 0.5*float64(pace.MaxStops))
	return math.Min(30, math.Max(3, r))
}

// baseQuery is the guaranteed pre-filter for the whole window.
func baseQuery(spec *PlanSpec, cfg Config, radiusKm float64) CandidateQuery {
	return CandidateQuery{
		Catalog:           spec.Catalog,
		City:              spec.City,
		Kinds:             []string{"event", "place"},
		Center:            *spec.Start,
		RadiusKm:          radiusKm,
		From:              spec.From,
		To:                spec.BackBy,
		MaxTier:           spec.Budget.Tier,
		FreeOnly:          spec.Budget.FreeOnly,
		AllowUnknownPrice: true,
		AgeBracket:        spec.AgeBracket,
		ExcludeCategories: append([]string(nil), spec.Hard.ExcludeCategories...),
		ExcludeTags:       append([]string(nil), spec.Hard.ExcludeTags...),
		PlaceCategories:   placeCategoriesMinus(spec.Hard.ExcludeCategories),
		LimitEvents:       cfg.PhaseAEvents,
		LimitPlaces:       cfg.PhaseAPlaces,
	}
}

func placeCategoriesMinus(excluded []string) []string {
	var out []string
	for _, c := range itinerary.PlaceCategories() {
		if !containsString(excluded, c) {
			out = append(out, c)
		}
	}
	return out
}

// feasibleOpts tunes the Go-side check for expansion slots.
type feasibleOpts struct {
	minOpen time.Duration // places must be open at least this long inside [q.From, q.To)
}

// Feasible re-checks, in Go, everything the query promised and everything
// Mongo cannot express (§4.2), before any vector or classifier work. Every
// drop is counted by reason.
func Feasible(spec *PlanSpec, q *CandidateQuery, itCfg itinerary.Config, acts []models.Activity, drops map[string]int, opts feasibleOpts) []*Candidate {
	drop := func(reason string) { drops[reason]++ }
	seen := map[string]bool{}
	var out []*Candidate
	for i := range acts {
		a := &acts[i]
		id := a.ID.Hex()
		if seen[id] {
			drop("duplicate")
			continue
		}
		seen[id] = true
		if _, ok := activityPoint(a); !ok {
			drop("no_location")
			continue
		}
		if isAddOnListing(a) {
			drop("add_on_listing")
			continue
		}
		c := newCandidate(*a)
		if q.RadiusKm > 0 && travel.HaversineKm(q.Center, c.Point) > q.RadiusKm {
			drop("out_of_radius")
			continue
		}
		if ageRules(spec.AgeBracket).blocks(a) {
			drop("age_gate")
			continue
		}
		if spec.Hard.excludesCategory(a.Category) || containsString(q.ExcludeCategories, a.Category) {
			drop("excluded_category")
			continue
		}
		if spec.Hard.excludesAnyTag(a.Tags) || tagsIntersect(a.Tags, q.ExcludeTags) {
			drop("excluded_tag")
			continue
		}
		if reason := priceDrop(c, spec.Budget); reason != "" {
			drop(reason)
			continue
		}
		var reason string
		if a.Kind == "event" {
			reason = eventFeasible(a, spec, q, itCfg)
		} else {
			reason = placeFeasible(a, spec, q, itCfg, opts)
		}
		if reason != "" {
			drop(reason)
			continue
		}
		if !MatchesQuery(a, q) {
			drop("query_mismatch")
			continue
		}
		out = append(out, c)
	}
	return out
}

func tagsIntersect(tags, list []string) bool {
	for _, t := range tags {
		if containsString(list, t) {
			return true
		}
	}
	return false
}

// priceDrop applies the budget: known cost within the total, known tier
// within the budget's tier, and for free-only plans a known free price or
// an unknown one in a normally-free category.
func priceDrop(c *Candidate, b Budget) string {
	if b.FreeOnly {
		if c.PriceKnown && c.CostCents == 0 {
			return ""
		}
		if !c.PriceKnown && c.Act.Price == nil && freeIfUnknownCategories[c.Act.Category] {
			return ""
		}
		return "not_free"
	}
	if b.TotalCents > 0 && c.PriceKnown && c.CostCents > b.TotalCents {
		return "over_budget"
	}
	if b.Tier < 3 && c.TierKnown && c.Tier > b.Tier {
		return "over_tier"
	}
	return ""
}

// visitLengths returns the p75 visit length (falling back to the median,
// then an hour) and the median.
func visitLengths(a *models.Activity) (p75, median time.Duration) {
	median, p75 = time.Hour, 0
	if d := a.Duration; d != nil {
		if d.MedianMin > 0 && !math.IsInf(d.MedianMin, 0) && !math.IsNaN(d.MedianMin) {
			median = time.Duration(math.Min(d.MedianMin, 1e6) * float64(time.Minute))
		}
		if d.P75Min > 0 && !math.IsInf(d.P75Min, 0) && !math.IsNaN(d.P75Min) {
			p75 = time.Duration(math.Min(d.P75Min, 1e6) * float64(time.Minute))
		}
	}
	if p75 < median {
		p75 = median
	}
	return p75, median
}

func eventFeasible(a *models.Activity, spec *PlanSpec, q *CandidateQuery, itCfg itinerary.Config) string {
	if a.Start == nil {
		return "no_start_time"
	}
	start := a.Start.UTC()
	p75, _ := visitLengths(a)
	if p75 > itCfg.MaxDuration {
		p75 = itCfg.MaxDuration
	}
	end := start.Add(p75)
	if a.End != nil && a.End.UTC().After(start) {
		end = a.End.UTC()
	}
	isDropIn := a.Attendance != nil && *a.Attendance == "drop_in"
	if a.End != nil && a.End.Sub(start) > itCfg.MaxDuration {
		isDropIn = true // a multi-day span is something to drop into
	}
	if isDropIn {
		overlapStart := maxTime(start, spec.From)
		overlapEnd := minTime(end, spec.BackBy)
		if overlapEnd.Sub(overlapStart) < p75 {
			return "too_short_overlap"
		}
		if q.From != spec.From || q.To != spec.BackBy {
			if minTime(end, q.To).Sub(maxTime(start, q.From)) < minDuration(p75, 30*time.Minute) {
				return "outside_slot"
			}
		}
		return ""
	}
	if start.Before(spec.From) {
		return "outside_window"
	}
	if start.Add(maxDuration(end.Sub(start), p75)).After(spec.BackBy) {
		return "outside_window"
	}
	if start.Before(q.From) || start.After(q.To.Add(-eventStartMargin)) {
		return "outside_slot"
	}
	return ""
}

func placeFeasible(a *models.Activity, spec *PlanSpec, q *CandidateQuery, itCfg itinerary.Config, opts feasibleOpts) string {
	if !itinerary.IsPlaceCategory(a.Category) {
		return "category_excluded"
	}
	loc := spec.TZ
	if a.Timezone != "" {
		if l, err := time.LoadLocation(a.Timezone); err == nil {
			loc = l
		}
	}
	open, ok := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, spec.From, spec.BackBy)
	if !ok {
		return "no_hours"
	}
	if longestInterval(open) < itCfg.MinDuration {
		return "closed_during_window"
	}
	if opts.minOpen > 0 {
		slot, _ := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, q.From, q.To)
		if longestInterval(slot) < opts.minOpen {
			return "closed_during_slot"
		}
	}
	return ""
}

func longestInterval(ivs []itinerary.Interval) time.Duration {
	var best time.Duration
	for _, iv := range ivs {
		if d := iv.End.Sub(iv.Start); d > best {
			best = d
		}
	}
	return best
}

// --- round 0 ---------------------------------------------------------------

// retrieve runs phase A (candidates and the search vector in parallel), the
// Go-side check, the range relax step when nothing fits, phase B
// (embeddings for survivors only), the cosine shortlist and one classifier
// call, then fills the pool.
func (p *Planner) retrieve(ctx context.Context, run *Run) error {
	cfg := run.Cfg
	spec := &run.Spec

	var (
		wg       sync.WaitGroup
		sv       SearchVector
		svErr    error
		events   []models.Activity
		places   []models.Activity
		findErr  error
		findMs   int64
		searchMs int64
	)
	q := baseQuery(spec, cfg, run.RadiusKm)
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := p.Clock.Now()
		events, places, findErr = p.Source.FindCandidates(ctx, q)
		findMs = p.msSince(t)
	}()
	if p.Search != nil && (spec.MoodText != "" || len(spec.QuickPicks) > 0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := p.Clock.Now()
			sctx, cancel := context.WithTimeout(ctx, cfg.SearchTimeout)
			defer cancel()
			sv, svErr = p.Search.SearchVector(sctx, SearchInput{
				MoodText: spec.MoodText, Tags: spec.QuickPicks, Who: spec.Who, Pace: spec.Pace,
				Budget: spec.Budget.Tier, StartTime: spec.From, BackBy: spec.BackBy, Timezone: spec.TZ.String(),
			})
			searchMs = p.msSince(t)
		}()
	}
	wg.Wait()
	run.Log.Timings["phase_a_ms"] = findMs
	run.Log.Timings["search_ms"] = searchMs
	if findErr != nil {
		return findErr
	}
	if svErr != nil {
		run.Log.ML.SearchErr = svErr.Error()
	} else if len(sv.Embedding) > 0 && !isZeroVec(sv.Embedding) {
		run.SearchEmb = sv.Embedding
		run.SearchText = sv.Text
	}
	run.Log.Counts.EventsA, run.Log.Counts.PlacesA = len(events), len(places)

	cands := Feasible(spec, &q, run.ItCfg, append(events, places...), run.Log.Counts.Drops, feasibleOpts{})
	if len(cands) == 0 {
		// Relax the range once: wider radius and longer legs.
		run.relaxRange()
		q = baseQuery(spec, cfg, run.RadiusKm)
		t := p.Clock.Now()
		events, places, findErr = p.Source.FindCandidates(ctx, q)
		run.Log.Timings["phase_a_relaxed_ms"] = p.msSince(t)
		if findErr != nil {
			return findErr
		}
		run.Log.Counts.EventsA, run.Log.Counts.PlacesA = len(events), len(places)
		cands = Feasible(spec, &q, run.ItCfg, append(events, places...), run.Log.Counts.Drops, feasibleOpts{})
	}
	run.Log.Counts.Feasible = len(cands)
	run.Log.Filters.RadiusKm = run.RadiusKm
	if len(cands) == 0 {
		return nil
	}

	t := p.Clock.Now()
	if err := p.fetchEmbeddings(ctx, run, cands); err != nil {
		return err
	}
	run.Log.Timings["phase_b_ms"] = p.msSince(t)

	run.QV = BuildQueryVector(run.User, run.SearchEmb, cfg)
	short := Shortlist(cands, run.QV, spec, cfg)
	run.Log.Counts.Shortlist = len(short)

	t = p.Clock.Now()
	kept, mode := p.scoreCandidates(ctx, run, short, cfg.MLTimeout)
	run.Log.Timings["classifier_ms"] = p.msSince(t)
	run.Log.ML.Mode = mode
	run.Log.Counts.Ranked = len(kept)
	for _, c := range kept {
		c.Source, c.Round = "retrieval", 0
		run.Pool.Add(c)
		run.Log.Shortlist = append(run.Log.Shortlist, shortlistEntry(c))
	}
	return nil
}

func shortlistEntry(c *Candidate) ShortlistEntry {
	return ShortlistEntry{ID: c.ID, Kind: c.Kind, Category: c.Act.Category, Cos: round5(c.Cos), Dislike: round5(c.Dislike),
		Prior: round5(c.Prior), ML: c.ML, Source: c.Source, Round: c.Round}
}

func round5(v float64) float64 { return math.Round(v*1e5) / 1e5 }

// fetchEmbeddings is phase B: vectors for the survivors only, capped, the
// best priors first when over the cap.
func (p *Planner) fetchEmbeddings(ctx context.Context, run *Run, cands []*Candidate) error {
	if p.Embeddings == nil || len(cands) == 0 {
		return nil
	}
	ordered := append([]*Candidate(nil), cands...)
	for _, c := range ordered {
		c.Prior = Prior(c, run.Spec.Facets)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Prior != ordered[j].Prior {
			return ordered[i].Prior > ordered[j].Prior
		}
		return ordered[i].ID < ordered[j].ID
	})
	if cap := run.Cfg.EmbedFetchCap; cap > 0 && len(ordered) > cap {
		ordered = ordered[:cap]
	}
	ids := make([]string, len(ordered))
	for i, c := range ordered {
		ids[i] = c.ID
	}
	embs, err := p.Embeddings.FetchEmbeddings(ctx, run.Spec.Catalog, ids)
	if err != nil {
		return err
	}
	got := 0
	for _, c := range ordered {
		if e, ok := embs[c.ID]; ok && len(e) > 0 {
			c.Act.Embedding = e
			got++
		}
	}
	run.Log.Counts.Embedded += got
	return nil
}

// scoreCandidates is the classifier policy: send the candidates that have
// a vector, with the user's positive vector (else the search vector; with
// neither the call is skipped). Ids the service dropped stay dropped. On
// error everyone keeps the cosine/prior blend and the mode says why.
func (p *Planner) scoreCandidates(ctx context.Context, run *Run, cands []*Candidate, timeout time.Duration) ([]*Candidate, string) {
	if p.Scorer == nil {
		return cands, "skipped:no_scorer"
	}
	pos := run.User.PositiveEmbedding
	if isZeroVec(pos) {
		pos = run.SearchEmb
	}
	if isZeroVec(pos) {
		return cands, "skipped:no_positive_vector"
	}
	var send []*Candidate
	for _, c := range cands {
		if len(c.Act.Embedding) > 0 {
			send = append(send, c)
		}
	}
	if len(send) == 0 {
		return cands, "skipped:no_candidate_vectors"
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := p.Scorer.Score(sctx, p.scoreRequest(run, send, false))
	if err != nil {
		return cands, "fallback:" + err.Error()
	}
	if res.ModelVersion != "" {
		run.Log.ML.Model = res.ModelVersion
	}
	kept := make([]*Candidate, 0, len(cands))
	for _, c := range cands {
		if len(c.Act.Embedding) == 0 {
			kept = append(kept, c)
			continue
		}
		s, ok := res.Scores[c.ID]
		if !ok {
			run.Log.Counts.MLDropped++
			continue
		}
		s = clamp01(s)
		c.ML = &s
		if j, ok := res.Rerank[c.ID]; ok {
			j = math.Max(0, math.Min(4, j))
			c.Jev = &j
		}
		kept = append(kept, c)
	}
	return kept, "classifier"
}

func (p *Planner) scoreRequest(run *Run, cands []*Candidate, rerank bool) ScoreRequest {
	pos := run.User.PositiveEmbedding
	if isZeroVec(pos) {
		pos = run.SearchEmb
	}
	req := ScoreRequest{
		User: run.User, PositiveEmbedding: pos, NegativeEmbedding: run.User.NegativeEmbedding,
		Candidates: cands, Query: run.QV, SearchText: run.SearchText, SearchEmbedding: run.SearchEmb,
		Rerank: rerank, RerankTopK: run.Cfg.JevTopK,
		From: run.Spec.From, BackBy: run.Spec.BackBy, Center: run.Spec.Start, MaxDistanceKm: run.RadiusKm,
		ExcludedCategories: run.Spec.Hard.ExcludeCategories,
	}
	if run.Spec.Budget.TotalCents > 0 {
		total := run.Spec.Budget.TotalCents
		req.MaxPriceCents = &total
	}
	return req
}

func (p *Planner) msSince(t time.Time) int64 {
	return p.Clock.Now().Sub(t).Milliseconds()
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

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
