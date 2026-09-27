package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Radius of the phase-A search around the start point. Every leg, the
// first and last included, is at most maxLegKm, so on a round trip a stop
// is at most ceil(MaxStops/2) legs from home; half the distance to a
// separate end point is added. Bounded to something a query can serve.
func radiusFor(spec *PlanSpec, cfg Config, maxLegKm float64) float64 {
	pace := cfg.Itinerary.Pace(spec.Pace)
	r := maxLegKm * math.Ceil(float64(pace.MaxStops)/2)
	if spec.Start != nil && spec.End != nil {
		r += travel.HaversineKm(*spec.Start, *spec.End) / 2
	}
	return math.Min(30, math.Max(3, r))
}

// baseQuery is the guaranteed pre-filter for the whole window. The
// must-see picks are left out: they join the pool on their own terms.
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
		MinPlaceRating:    cfg.MinPlaceRating,
		ExcludeIDs:        append([]string(nil), spec.MustInclude...),
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
		if AgeRulesFor(spec.AgeBracket).Blocks(a) {
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
		if !ratingAllowed(a, q.MinPlaceRating) {
			drop("low_rating")
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

// eventFeasible is the Go-side time check of an event against the window
// and, for an expansion, its slot. A whole event must start inside the
// window and end (at its p75 length) by back-by; a clipped stay needs
// MinStay between its start (up to LateArrival late) and its end; a window
// event needs MinStay of overlap.
func eventFeasible(a *models.Activity, spec *PlanSpec, q *CandidateQuery, itCfg itinerary.Config) string {
	st, ok := itinerary.StayFor(a, itCfg)
	if !ok {
		return "no_start_time"
	}
	sliced := q.From != spec.From || q.To != spec.BackBy
	switch st.Kind {
	case itinerary.StayWindow:
		if minTime(st.End, spec.BackBy).Sub(maxTime(st.Start, spec.From)) < st.MinStay {
			return "too_short_overlap"
		}
		if sliced && minTime(st.End, q.To).Sub(maxTime(st.Start, q.From)) < minDuration(st.MinStay, 30*time.Minute) {
			return "outside_slot"
		}
		return ""
	case itinerary.StayClipped:
		if !st.ClippedFits(spec.From, spec.BackBy) {
			return "outside_window"
		}
		if sliced && (st.Start.Add(itinerary.LateArrival).Before(q.From) || st.Start.After(q.To.Add(-EventStartMargin))) {
			return "outside_slot"
		}
		return ""
	}
	start := st.Start
	p75, _ := visitLengths(a)
	if p75 > itCfg.MaxDuration {
		p75 = itCfg.MaxDuration
	}
	if start.Before(spec.From) {
		return "outside_window"
	}
	if start.Add(maxDuration(st.End.Sub(start), p75)).After(spec.BackBy) {
		return "outside_window"
	}
	if start.Before(q.From) || start.After(q.To.Add(-EventStartMargin)) {
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
// call, then fills the pool. The must-see picks skip phase A's filters and
// the shortlist but are embedded and scored with the rest.
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
	if len(cands) < cfg.MinCandidates && run.relaxRange() {
		// Too few fit: relax the range once (twice the radius, longer
		// legs). The counts then describe the relaxed query, which is the
		// one that fed the pool.
		q = baseQuery(spec, cfg, run.RadiusKm)
		t := p.Clock.Now()
		events, places, findErr = p.Source.FindCandidates(ctx, q)
		run.Log.Timings["phase_a_relaxed_ms"] = p.msSince(t)
		if findErr != nil {
			return findErr
		}
		run.Log.Counts.EventsA, run.Log.Counts.PlacesA = len(events), len(places)
		run.Log.Counts.Drops = map[string]int{}
		cands = Feasible(spec, &q, run.ItCfg, append(events, places...), run.Log.Counts.Drops, feasibleOpts{})
	}
	run.Log.Counts.Feasible = len(cands)
	run.Log.Filters.RadiusKm = run.RadiusKm
	if len(cands) == 0 && len(run.Picks) == 0 {
		return nil
	}

	// One representative per series is embedded, shortlisted and scored:
	// every timed-entry slot of an exhibition costs one ranker slot and
	// shares its scores.
	reps, siblings := seriesRepresentatives(cands)
	run.Log.Counts.SeriesSiblings = len(cands) - len(reps)

	t := p.Clock.Now()
	if err := p.fetchEmbeddings(ctx, run, reps); err != nil {
		return err
	}
	run.Log.Timings["phase_b_ms"] = p.msSince(t)

	run.QV = BuildQueryVector(run.User, run.SearchEmb, cfg)
	short := Shortlist(reps, run.QV, spec, cfg)
	run.Log.Counts.Shortlist = len(short)
	if len(run.Picks) > 0 {
		// The picks get their vectors and cosine scores outside the
		// shortlist, on the expansions' scale, and ride the same
		// classifier call.
		if err := p.fetchEmbeddings(ctx, run, run.Picks); err != nil {
			return err
		}
		for _, c := range run.Picks {
			scoreForShortlist(c, run.QV, spec.Facets, cfg)
			c.CosRank = expansionCosRank(c)
		}
		short = append(short, run.Picks...)
	}

	t = p.Clock.Now()
	kept, mode := p.scoreCandidates(ctx, run, short, cfg.MLTimeout)
	run.Log.Timings["classifier_ms"] = p.msSince(t)
	run.Log.ML.Mode = mode
	run.Log.Counts.Ranked = len(kept)
	if mode == "classifier" && p.rerankable(run) && cfg.Jev == "sync" {
		t = p.Clock.Now()
		p.rerankSync(ctx, run, kept)
		run.Log.Timings["jev_ms"] = p.msSince(t)
	}
	for _, c := range kept {
		c.Source, c.Round = "retrieval", 0
		if run.isPick(c.ID) {
			c.Source = pickSource
		}
		run.Pool.Add(c)
		run.Log.Shortlist = append(run.Log.Shortlist, shortlistEntry(c))
		for _, sib := range siblings[c.ID] {
			sib.inherit(c)
			run.Pool.Add(sib)
		}
	}
	return nil
}

// seriesRepresentatives keeps the first candidate of each series (events
// arrive by start, so the earliest listing) and files the others under its
// id.
func seriesRepresentatives(cands []*Candidate) ([]*Candidate, map[string][]*Candidate) {
	repOf := map[string]*Candidate{}
	siblings := map[string][]*Candidate{}
	var reps []*Candidate
	for _, c := range cands {
		key := itinerary.SeriesKey(&c.Act)
		if rep, ok := repOf[key]; ok {
			siblings[rep.ID] = append(siblings[rep.ID], c)
			continue
		}
		repOf[key] = c
		reps = append(reps, c)
	}
	return reps, siblings
}

// jevEnabled is true when the scorer can rerank and the knob allows it.
func (p *Planner) jevEnabled() bool {
	if p.Scorer == nil || p.Cfg.Jev == "off" || p.Cfg.Jev == "" {
		return false
	}
	jc, ok := p.Scorer.(JevCapable)
	return ok && jc.JevAvailable()
}

// rerankSync (PLANNER_JEV=sync) waits for the reranker on the best
// JevTopK of the shortlist so its scores steer the solve. A failure only
// leaves the classifier scores in place.
func (p *Planner) rerankSync(ctx context.Context, run *Run, kept []*Candidate) {
	var send []*Candidate
	for _, c := range kept {
		if c.ML != nil && len(send) < run.Cfg.JevTopK {
			send = append(send, c) // scored by the classifier, so its vector was usable
		}
	}
	if len(send) == 0 {
		return
	}
	jctx, cancel := context.WithTimeout(ctx, run.Cfg.JevTimeout)
	defer cancel()
	p.attachTexts(jctx, run, send)
	res, err := p.Scorer.Score(jctx, p.scoreRequest(run, send, true))
	now := p.Clock.Now()
	jl := &JevLog{Requested: true, CompletedAt: &now, Scores: map[string]float64{}}
	if err != nil {
		jl.Err = err.Error()
	}
	for _, c := range send {
		if j, ok := res.Rerank[c.ID]; ok && err == nil {
			j = math.Max(0, math.Min(4, j))
			c.Jev = &j
			jl.Scores[c.ID] = j
		}
	}
	run.Log.ML.Jev = jl
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
		if e, ok := embs[c.ID]; ok && usableVec(e, len(e)) {
			c.Act.Embedding = e
			got++
		}
	}
	run.Log.Counts.Embedded += got
	return nil
}

// scoreCandidates is the classifier policy: send the candidates that have
// a vector, with the user's positive vector (else the search vector; with
// neither the call is skipped). Ids the service dropped stay dropped, but
// for the must-see picks, which stay unscored. On error everyone keeps the
// cosine/prior blend and the mode says why.
func (p *Planner) scoreCandidates(ctx context.Context, run *Run, cands []*Candidate, timeout time.Duration) ([]*Candidate, string) {
	if len(run.Dropped) > 0 {
		var fresh []*Candidate
		for _, c := range cands {
			if !run.Dropped[c.ID] {
				fresh = append(fresh, c)
			}
		}
		cands = fresh
	}
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
		if usableVec(c.Act.Embedding, len(pos)) {
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
		if !usableVec(c.Act.Embedding, len(pos)) {
			kept = append(kept, c) // never sent: unscored, not dropped
			continue
		}
		s, ok := res.Scores[c.ID]
		if !ok && run.isPick(c.ID) {
			kept = append(kept, c)
			continue
		}
		if !ok {
			run.Log.Counts.MLDropped++
			if run.Dropped == nil {
				run.Dropped = map[string]bool{}
			}
			run.Dropped[c.ID] = true
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
	maxKm := run.RadiusKm
	for _, c := range cands {
		if run.Spec.Start != nil {
			maxKm = math.Max(maxKm, travel.HaversineKm(*run.Spec.Start, c.Point))
		}
	}
	req := ScoreRequest{
		User: run.User, PositiveEmbedding: pos, NegativeEmbedding: run.User.NegativeEmbedding,
		Candidates: cands, Query: run.QV, SearchText: run.SearchText, SearchEmbedding: run.SearchEmb,
		Rerank: rerank, RerankTopK: run.Cfg.JevTopK,
		From: run.Spec.From, BackBy: run.Spec.BackBy, Center: run.Spec.Start, MaxDistanceKm: maxKm + 0.05,
		ExcludedCategories: run.Spec.Hard.ExcludeCategories,
	}
	switch {
	case run.Spec.Budget.FreeOnly:
		zero := int64(0)
		req.MaxPriceCents = &zero
	case run.Spec.Budget.TotalCents > 0:
		total := run.Spec.Budget.TotalCents
		req.MaxPriceCents = &total
	}
	return req
}

// usableVec: dim finite values, not all zero (what the ranker accepts).
func usableVec(v []float64, dim int) bool {
	if dim == 0 || len(v) != dim {
		return false
	}
	nonZero := false
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
		nonZero = nonZero || x != 0
	}
	return nonZero
}

// attachTexts fetches the embedding texts of the rerank candidates when the
// source can; without them the reranker still gets the vectors.
func (p *Planner) attachTexts(ctx context.Context, run *Run, cands []*Candidate) {
	ts, ok := p.Embeddings.(TextSource)
	if !ok || len(cands) == 0 {
		return
	}
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.ID
	}
	texts, err := ts.FetchTexts(ctx, run.Spec.Catalog, ids)
	if err != nil {
		log.Warn().Err(err).Str("run", run.ID).Msg("planner: rerank texts")
		return
	}
	for _, c := range cands {
		c.Text = texts[c.ID]
	}
}

// rerankable: the reranker judges the candidate against the user's
// profile text, so it needs a user who has one.
func (p *Planner) rerankable(run *Run) bool {
	return p.jevEnabled() && strings.TrimSpace(run.User.PositiveText) != ""
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
