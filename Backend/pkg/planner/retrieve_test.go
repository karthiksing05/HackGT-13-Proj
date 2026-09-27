package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// violator is an activity the pre-filter must reject, with the reason.
type violator struct {
	act    models.Activity
	reason string
}

func TestFeasibleDropsEachViolatorWithItsReason(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	o := defaultReq()
	o.mood = "no museums, no sports"
	spec := tp.spec(t, sandy(), o)
	if !slices.Contains(spec.Hard.ExcludeCategories, "gallery") || !slices.Contains(spec.Hard.ExcludeTags, "art") {
		t.Fatalf("mood did not exclude art: %+v", spec.Hard)
	}
	near := offsetKm(seasideMkt, 0.5, 0)
	valid := synthEvent("Valid gig", "live_music", near, localAt(19, 0), 90, []string{"music"}, priceOf(10))
	park := synthPlace("Valid park", "park", offsetKm(seasideMkt, -0.4, 0.3), nil, []string{"outdoor"}, nil)

	noStart := synthEvent("No start", "comedy", near, localAt(19, 0), 60, nil, priceOf(5))
	noStart.Start = nil
	nowhere := synthEvent("Nowhere", "comedy", near, localAt(19, 0), 60, nil, priceOf(5))
	nowhere.Location.Coordinates = []float64{0, 0}
	shortDropIn := synthEvent("Short drop-in", "festival", near, localAt(22, 30), 90, nil, priceOf(0))
	shortDropIn.Attendance, shortDropIn.End = strp("drop_in"), timep(localAt(23, 30))
	shortDropIn.Duration = &models.ActivityDuration{MedianMin: 90, P75Min: 90}

	violators := []violator{
		{synthEvent("Too early", "comedy", near, localAt(17, 0), 60, nil, priceOf(5)), "outside_window"},
		{synthEvent("Runs late", "comedy", near, localAt(22, 0), 120, nil, priceOf(5)), "outside_window"},
		{synthEvent("Far away", "comedy", offsetKm(seasideMkt, 10, 0), localAt(19, 0), 60, nil, priceOf(5)), "out_of_radius"},
		{nowhere, "no_location"},
		{synthEvent("Parking: Valid gig", "live_music", near, localAt(19, 0), 60, nil, priceOf(5)), "add_on_listing"},
		{synthEvent("Pricey gala", "theater", near, localAt(19, 0), 60, nil, priceOf(80)), "over_budget"},
		{synthEvent("Tier three", "theater", near, localAt(19, 0), 60, nil, priceOf(45)), "over_tier"},
		{synthEvent("Paint night", "class_workshop", near, localAt(19, 0), 60, []string{"art"}, priceOf(5)), "excluded_tag"},
		{synthPlace("Gallery", "gallery", near, dailyHours(10, 22), nil, priceOf(0)), "excluded_category"},
		{synthPlace("Arcade", "rec_venue", near, dailyHours(10, 22), nil, priceOf(5)), "excluded_category"},
		{synthPlace("Morning cafe", "cafe", near, dailyHours(7, 15), []string{"food"}, priceOf(5)), "closed_during_window"},
		{synthPlace("Cinema", "cinema", near, dailyHours(10, 23), nil, priceOf(12)), "category_excluded"},
		{synthPlace("Club", "nightclub", near, nil, nil, priceOf(10)), "no_hours"},
		{shortDropIn, "too_short_overlap"},
		{noStart, "no_start_time"},
		{valid, "duplicate"}, // the same id a second time
	}
	acts := []models.Activity{valid, park}
	want := map[string]int{}
	for _, v := range violators {
		acts = append(acts, v.act)
		want[v.reason]++
	}
	q := baseQuery(&spec, tp.Cfg, radiusFor(&spec, tp.Cfg, spec.MaxLegKm))
	drops := map[string]int{}
	got := Feasible(&spec, &q, tp.Cfg.Itinerary, acts, drops, feasibleOpts{})
	if len(got) != 2 || got[0].ID != valid.ID.Hex() || got[1].ID != park.ID.Hex() {
		var names []string
		for _, c := range got {
			names = append(names, c.Act.Name)
		}
		t.Fatalf("survivors = %v", names)
	}
	for reason, n := range want {
		if drops[reason] != n {
			t.Errorf("drops[%s] = %d, want %d (all: %v)", reason, drops[reason], n, drops)
		}
	}
	for _, c := range got {
		if !MatchesQuery(&c.Act, &q) {
			t.Errorf("%s survived but fails the query", c.Act.Name)
		}
		if c.PriceKnown != (c.Act.Price != nil) {
			t.Errorf("%s price known = %v", c.Act.Name, c.PriceKnown)
		}
	}
}

func TestFeasibleAgeGates(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	near := offsetKm(seasideMkt, 0.5, 0)
	drinks := synthEvent("Beer tasting", "class_workshop", near, localAt(19, 0), 60, []string{"drinks"}, priceOf(10))
	eighteen := synthEvent("Glow Party (18+)", "festival", near, localAt(19, 0), 60, nil, priceOf(10))
	twentyOne := synthEvent("Jazz Night 21+", "live_music", near, localAt(19, 0), 60, nil, priceOf(10))
	tagged := synthEvent("Late set", "live_music", near, localAt(20, 0), 60, []string{"21_plus"}, priceOf(10))
	bar := synthPlace("Harbor bar", "bar", near, dailyHours(16, 26), nil, priceOf(8))
	ok := synthEvent("All ages show", "comedy", near, localAt(19, 0), 60, nil, priceOf(10))
	acts := []models.Activity{drinks, eighteen, twentyOne, tagged, bar, ok}

	survivors := func(age string) []string {
		u := sandy()
		u.AgeBracket = age
		spec := tp.spec(t, u, defaultReq())
		q := baseQuery(&spec, tp.Cfg, radiusFor(&spec, tp.Cfg, spec.MaxLegKm))
		var names []string
		for _, c := range Feasible(&spec, &q, tp.Cfg.Itinerary, acts, map[string]int{}, feasibleOpts{}) {
			names = append(names, c.Act.Name)
		}
		sort.Strings(names)
		return names
	}
	for age, want := range map[string]string{
		"21_plus": "All ages show|Beer tasting|Glow Party (18+)|Harbor bar|Jazz Night 21+|Late set",
		"18_20":   "All ages show|Beer tasting|Glow Party (18+)",
		"13_17":   "All ages show",
	} {
		if got := strings.Join(survivors(age), "|"); got != want {
			t.Errorf("%s: %s, want %s", age, got, want)
		}
	}
}

func TestFreeOnlyAndUnknownPrices(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	near := offsetKm(seasideMkt, 0.5, 0)
	freeEvent := synthEvent("Free show", "comedy", near, localAt(19, 0), 60, nil, priceOf(0))
	paid := synthEvent("Paid show", "theater", near, localAt(19, 0), 60, nil, priceOf(5))
	unknownPark := synthPlace("Mystery park", "park", near, nil, nil, nil)
	unknownGig := synthEvent("Mystery gig", "live_music", near, localAt(20, 0), 60, nil, nil)
	tierOnly := synthEvent("Tier only", "festival", near, localAt(20, 0), 60, nil, &models.ActivityPrice{Tier: 1, Currency: "USD"})
	acts := []models.Activity{freeEvent, paid, unknownPark, unknownGig, tierOnly}

	names := func(budget int) string {
		o := defaultReq()
		o.budget = budget
		spec := tp.spec(t, sandy(), o)
		q := baseQuery(&spec, tp.Cfg, radiusFor(&spec, tp.Cfg, spec.MaxLegKm))
		var out []string
		for _, c := range Feasible(&spec, &q, tp.Cfg.Itinerary, acts, map[string]int{}, feasibleOpts{}) {
			out = append(out, c.Act.Name)
			if !MatchesQuery(&c.Act, &q) {
				t.Errorf("%s fails the query", c.Act.Name)
			}
		}
		sort.Strings(out)
		return strings.Join(out, "|")
	}
	// Free: known-free, or no price at all in a normally free category.
	// A price object with only a tier is not "free".
	if got := names(0); got != "Free show|Mystery park" {
		t.Errorf("free: %s", got)
	}
	// $: unknown prices stay (counted as unknown, not free).
	if got := names(1); got != "Free show|Mystery gig|Mystery park|Paid show|Tier only" {
		t.Errorf("$: %s", got)
	}
	cost, known := activityCost(&unknownGig)
	if known || cost != 0 {
		t.Error("a null price is unknown")
	}
	if _, known := activityCost(&tierOnly); known {
		t.Error("a tier without an amount is not a known cost")
	}
	if tier, known := activityTier(&tierOnly); !known || tier != 1 {
		t.Errorf("tier-only tier = %d %v", tier, known)
	}
	if isKnownFree(&unknownPark) || !isKnownFree(&freeEvent) {
		t.Error("isKnownFree")
	}
}

func TestShortlistQuotasAndCaps(t *testing.T) {
	cfg := testConfig()
	cfg.ShortlistN, cfg.FacetQuota, cfg.CategoryCap = 80, 15, 25
	food, _ := FacetByName("Food")
	spec := &PlanSpec{Facets: []Facet{food}}
	q := vectorOf("outdoor", "cat:park")
	var cands []*Candidate
	add := func(n int, kind, cat string, vec []float64, tags []string) {
		for range n {
			a := synthPlace("x", cat, seasideMkt, nil, tags, nil)
			a.Kind = kind
			a.Embedding = vec
			cands = append(cands, newCandidate(a))
		}
	}
	add(60, "place", "park", vectorOf("outdoor", "cat:park"), []string{"outdoor"})
	add(30, "event", "live_music", vectorOf("music", "cat:live_music"), []string{"music"})
	add(20, "place", "restaurant", vectorOf("food", "cat:restaurant"), []string{"food"})

	out := Shortlist(cands, QueryVector{Q: q, Source: "user"}, spec, cfg)
	count := map[string]int{}
	for _, c := range out {
		count[c.Act.Category]++
	}
	// The cap is per category, events included: 30 gigs → 25.
	if count["park"] != 25 || count["restaurant"] != 20 || count["live_music"] != 25 || len(out) != 70 {
		t.Errorf("counts %v (total %d)", count, len(out))
	}
	// Tight N: the facet and event quotas come first, the cap still holds.
	cfg.ShortlistN = 50
	out = Shortlist(cands, QueryVector{Q: q, Source: "user"}, spec, cfg)
	count = map[string]int{}
	for _, c := range out {
		count[c.Act.Category]++
	}
	if len(out) != 50 || count["restaurant"] < 15 || count["live_music"] < 15 || count["park"] > 25 {
		t.Errorf("tight counts %v (total %d)", count, len(out))
	}
	// Output order: best key first, ids ascending on ties; CosRank 1 → 0.
	for i := 1; i < len(out); i++ {
		a, b := out[i-1], out[i]
		ka := a.Cos + 0.05*float64(a.coversCount(spec.Facets))
		kb := b.Cos + 0.05*float64(b.coversCount(spec.Facets))
		if ka < kb-1e-12 || (math.Abs(ka-kb) < 1e-12 && a.ID > b.ID) {
			t.Fatalf("order broken at %d", i)
		}
	}
	if out[0].CosRank != 1 || out[len(out)-1].CosRank != 0 {
		t.Errorf("cos ranks %v … %v", out[0].CosRank, out[len(out)-1].CosRank)
	}
}

func TestShortlistUsesPriorsWithoutVectors(t *testing.T) {
	cfg := testConfig()
	qv := BuildQueryVector(&UserContext{ID: "novec"}, nil, cfg)
	if qv.Source != "none" || qv.Q != nil || qv.HasNeg {
		t.Fatalf("query vector = %+v", qv)
	}
	mk := func(rating, pop *float64) *Candidate {
		a := synthPlace("p", "park", seasideMkt, nil, nil, nil)
		a.Rating, a.Popularity = rating, pop
		a.Embedding = vectorOf("outdoor") // present, but there is nothing to compare it with
		return newCandidate(a)
	}
	best := mk(f64p(4.9), f64p(0.9))
	mid := mk(f64p(4.0), f64p(0.5))
	unknown := mk(nil, nil)
	low := mk(f64p(3.0), f64p(0.1))
	out := Shortlist([]*Candidate{low, unknown, mid, best}, qv, &PlanSpec{}, cfg)
	if out[0] != best || out[1] != mid || out[2] != unknown || out[3] != low {
		t.Errorf("prior order: %.3f %.3f %.3f %.3f", out[0].Prior, out[1].Prior, out[2].Prior, out[3].Prior)
	}
	for _, c := range out {
		if c.HasVec || c.Cos != 0 || c.Dislike != 0 {
			t.Errorf("no query vector, yet cos=%v hasVec=%v", c.Cos, c.HasVec)
		}
	}
	if !approx(unknown.Prior, 0.5*0.4+0.2*0.3+0.3*0.5, 1e-9) {
		t.Errorf("neutral prior = %v", unknown.Prior)
	}
}

func TestBuildQueryVector(t *testing.T) {
	cfg := testConfig()
	pos, neg, search := vectorOf("a"), vectorOf("b"), vectorOf("c")
	u := &UserContext{PositiveEmbedding: pos, NegativeEmbedding: neg}

	qv := BuildQueryVector(u, search, cfg)
	want := make([]float64, len(pos))
	for i := range pos {
		want[i] = 0.4*pos[i] + 0.6*search[i]
	}
	want = l2norm(want)
	if qv.Source != "user+search" || !qv.HasNeg || !approx(dot(qv.Q, want), 1, 1e-12) {
		t.Errorf("blend: %s neg=%v cos=%v", qv.Source, qv.HasNeg, dot(qv.Q, want))
	}
	if qv = BuildQueryVector(u, nil, cfg); qv.Source != "user" || !approx(dot(qv.Q, pos), 1, 1e-12) {
		t.Errorf("user only: %s", qv.Source)
	}
	if qv = BuildQueryVector(&UserContext{}, search, cfg); qv.Source != "search" || qv.HasNeg {
		t.Errorf("search only: %s", qv.Source)
	}
	zero := make([]float64, testDim)
	if qv = BuildQueryVector(&UserContext{PositiveEmbedding: zero, NegativeEmbedding: neg}, nil, cfg); qv.Source != "none" || qv.Q != nil {
		t.Errorf("an all-zero profile is no profile: %+v", qv)
	}
	if qv = BuildQueryVector(u, []float64{1, 0, 0}, cfg); qv.Source != "user" {
		t.Errorf("a search vector of another dimension is ignored: %s", qv.Source)
	}
}

func TestGenerateFetchesEmbeddingsForSurvivorsOnly(t *testing.T) {
	acts := saltlight(t)
	far := synthEvent("Far away", "comedy", offsetKm(seasideMkt, 12, 0), localAt(19, 0), 60, nil, priceOf(5))
	early := synthEvent("Too early", "comedy", seasideMkt, localAt(16, 0), 60, nil, priceOf(5))
	tp := newTestPlanner(append(acts, far, early), testConfig())
	batch, _ := tp.generate(t, sandy(), defaultReq())
	run := tp.run(t, batch.RunID)
	fetched := map[string]bool{}
	for _, call := range tp.source.EmbedCalls {
		for _, id := range call {
			if fetched[id] {
				t.Errorf("%s fetched twice", id)
			}
			fetched[id] = true
		}
	}
	if fetched[far.ID.Hex()] || fetched[early.ID.Hex()] {
		t.Error("vectors were fetched for activities that failed the pre-filter")
	}
	if run.Counts.Embedded != len(fetched) || len(fetched) < run.Counts.Feasible {
		t.Errorf("embedded %d, fetched %d, feasible %d", run.Counts.Embedded, len(fetched), run.Counts.Feasible)
	}
	// Every fetched id passed the filter: it is in the pool or was an
	// expansion candidate that failed nothing but the cosine cut.
	for id := range fetched {
		a := catalogByID(append(acts, far, early))[id]
		if a.Kind == "event" && a.Start != nil && a.Start.Before(localAt(18, 0)) {
			t.Errorf("%s starts before the window but was embedded", a.Name)
		}
	}
}

func TestClassifierFallbackKeepsPlanning(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	tp.scorer.Err = errors.New("ranker down")
	batch, spec := tp.generate(t, sandy(), defaultReq())
	if len(batch.Options) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}
	run := tp.run(t, batch.RunID)
	if run.ML.Mode != "fallback:ranker down" {
		t.Errorf("ml mode = %q", run.ML.Mode)
	}
	for _, e := range run.Shortlist {
		if e.ML != nil {
			t.Error("no classifier scores after a failure")
		}
	}
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		assertGuarantees(t, spec, opt, catalogByID(saltlight(t)))
	}
}

func TestUserWithoutVectorsTakesThePriorsPath(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	u := sandy()
	u.PositiveEmbedding, u.NegativeEmbedding = nil, nil
	batch, spec := tp.generate(t, u, defaultReq())
	if len(batch.Options) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}
	if len(tp.search.Calls) != 0 {
		t.Error("no mood and no picks: the search profile is skipped")
	}
	if tp.scorer.CallCount() != 0 {
		t.Error("no positive vector: the classifier is skipped")
	}
	run := tp.run(t, batch.RunID)
	pool := tp.pool(t, batch.RunID)
	if run.ML.Mode != "skipped:no_positive_vector" || pool.QuerySource != "none" || len(pool.QueryVector) != 0 {
		t.Errorf("ml %q, query source %q, %d floats", run.ML.Mode, pool.QuerySource, len(pool.QueryVector))
	}
	for _, opt := range tp.allOptions(t, u, batch) {
		assertGuarantees(t, spec, opt, catalogByID(saltlight(t)))
	}

	// With mood text the search vector stands in for the profile.
	tp = newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.mood, o.tags = "something outside with food", []string{"Outdoors"}
	batch, _ = tp.generate(t, u, o)
	if len(tp.search.Calls) != 1 || tp.scorer.CallCount() == 0 {
		t.Fatalf("search calls %d, classifier calls %d", len(tp.search.Calls), tp.scorer.CallCount())
	}
	sent := tp.scorer.Calls[0]
	if !approx(dot(sent.PositiveEmbedding, searchVectorFor(tp.search.Calls[0])), 1, 1e-9) || sent.Rerank {
		t.Error("the classifier should get the search vector as the positive vector, without rerank")
	}
	if pool := tp.pool(t, batch.RunID); pool.QuerySource != "search" {
		t.Errorf("query source %q", pool.QuerySource)
	}
	in := tp.search.Calls[0]
	if in.MoodText != o.mood || in.Budget != 2 || in.Timezone != "America/New_York" || !in.StartTime.Equal(o.from) {
		t.Errorf("search input %+v", in)
	}
}

func TestDroppedIDsAreNotReappended(t *testing.T) {
	acts := saltlight(t)
	tp := newTestPlanner(acts, testConfig())
	first, _ := tp.generate(t, sandy(), defaultReq())
	if len(first.Options) == 0 {
		t.Fatal("no options")
	}
	victim := first.Options[0].Stops[0].ActivityID

	tp = newTestPlanner(acts, testConfig())
	tp.scorer.Drop = map[string]bool{victim: true}
	batch, _ := tp.generate(t, sandy(), defaultReq())
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		for _, s := range opt.Stops {
			if s.ActivityID == victim {
				t.Fatalf("%s was dropped by the classifier but is in %s", victim, opt.ID)
			}
		}
	}
	run := tp.run(t, batch.RunID)
	if run.Counts.MLDropped != 1 {
		t.Errorf("ml dropped = %d", run.Counts.MLDropped)
	}
	for _, q := range tp.source.Calls[1:] {
		if len(q.IncludeCategories)+len(q.AnyTags) > 0 || q.From != tp.source.Calls[0].From {
			if !slices.Contains(q.ExcludeIDs, victim) {
				t.Error("an expansion query could bring the dropped id back")
			}
		}
	}
}

func TestMatchesQuerySlotsAndIncludes(t *testing.T) {
	near := offsetKm(seasideMkt, 0.3, 0)
	dropIn := synthEvent("Market", "market", near, localAt(12, 0), 60, []string{"food"}, priceOf(0))
	dropIn.Attendance, dropIn.End = strp("drop_in"), timep(localAt(19, 0))
	q := CandidateQuery{City: "saltlight", Center: seasideMkt, RadiusKm: 2, From: localAt(18, 0).UTC(), To: localAt(23, 0).UTC(), MaxTier: 3}
	if !MatchesQuery(&dropIn, &q) {
		t.Error("a drop-in that overlaps the window matches")
	}
	q.From = localAt(19, 0).UTC()
	if MatchesQuery(&dropIn, &q) {
		t.Error("a drop-in that ended at the window's start does not")
	}
	fixed := synthEvent("Gig", "live_music", near, localAt(22, 50), 60, nil, priceOf(0))
	if MatchesQuery(&fixed, &q) {
		t.Error("a fixed event must start 15 minutes before the window closes")
	}
	q.IncludeCategories, q.AnyTags = []string{"museum"}, []string{"food"}
	cafe := synthPlace("Cafe", "cafe", near, nil, []string{"food"}, priceOf(5))
	park := synthPlace("Park", "park", near, nil, []string{"outdoor"}, nil)
	if !MatchesQuery(&cafe, &q) || MatchesQuery(&park, &q) {
		t.Error("include categories OR any tags")
	}
	q.ExcludeIDs = []string{cafe.ID.Hex()}
	if MatchesQuery(&cafe, &q) {
		t.Error("excluded ids")
	}
	q = CandidateQuery{City: "atlanta", From: q.From, To: q.To, MaxTier: 3}
	if MatchesQuery(&park, &q) {
		t.Error("city filter")
	}
	_ = travel.Point{}
	_ = time.Minute
}
