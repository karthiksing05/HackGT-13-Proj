package planner

import (
	"Backend/pkg/models"
	"sync"
	"testing"
	"time"
)

// splitByFacet separates the activities that cover a facet.
func splitByFacet(acts []models.Activity, f Facet) (kept, covering []models.Activity) {
	for _, a := range acts {
		if f.Covers(&a) {
			covering = append(covering, a)
		} else {
			kept = append(kept, a)
		}
	}
	return kept, covering
}

func hasIssue(issues []IssueLog, kind, facet string) bool {
	for _, is := range issues {
		if is.Kind == kind && is.Facet == facet {
			return true
		}
	}
	return false
}

func TestLoopExpandsForAnUncoveredFacet(t *testing.T) {
	food, _ := FacetByName("Food")
	visible, covering := splitByFacet(saltlight(t), food)
	var pho models.Activity
	for _, a := range covering {
		if a.ID.Hex() == "ab71e6cf41394229d8b4febd" { // Pho Real Noodle House, open 10:30–21:30
			pho = a
		}
	}
	if pho.Name == "" {
		t.Fatal("fixture changed: Pho Real Noodle House missing")
	}
	tp := newTestPlanner(visible, testConfig())
	tp.source.Hidden = []models.Activity{pho} // only an expansion query can find it
	o := defaultReq()
	o.tags = []string{"Food"}
	batch, spec := tp.generate(t, sandy(), o)
	run := tp.run(t, batch.RunID)

	if len(run.Rounds) < 2 {
		t.Fatalf("expected a second round, got %d (stop %q)", len(run.Rounds), run.Rounds[0].Stop)
	}
	r0 := run.Rounds[0]
	if !hasIssue(r0.Issues, "uncovered_facet", "Food") || r0.Boosts["Food"] != 0 {
		t.Errorf("round 0 issues %+v boosts %v", r0.Issues, r0.Boosts)
	}
	found := false
	for _, e := range r0.Expansions {
		if e.Kind == "uncovered_facet" && e.Facet == "Food" && e.Added >= 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("no Food expansion added anything: %+v", r0.Expansions)
	}
	if run.Rounds[1].Boosts["Food"] != 0.10 {
		t.Errorf("round 1 boosts %v", run.Rounds[1].Boosts)
	}
	for _, e := range run.Shortlist {
		if e.ID == pho.ID.Hex() && (e.Source != "expand:uncovered_facet:Food" || e.Round != 1) {
			t.Errorf("pho logged as %s round %d", e.Source, e.Round)
		}
	}
	covered := false
	for _, top := range run.Rounds[1].Top3 {
		if containsString(top.Metrics.CoveredFacets, "Food") {
			covered = true
		}
	}
	if !covered {
		t.Errorf("round 1 top plans still miss Food: %+v", run.Rounds[1].Top3)
	}
	uses := false
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		assertGuarantees(t, spec, opt, catalogByID(append(visible, pho)))
		for _, s := range opt.Stops {
			uses = uses || s.ActivityID == pho.ID.Hex()
		}
	}
	if !uses {
		t.Error("no option uses the restaurant the expansion found")
	}
	for i := 1; i < len(run.Rounds); i++ {
		if run.Rounds[i].Top3Sum < run.Rounds[i-1].Top3Sum {
			t.Errorf("top-3 sum fell from %.5f to %.5f in round %d", run.Rounds[i-1].Top3Sum, run.Rounds[i].Top3Sum, i)
		}
	}
	// One classifier call for retrieval, then one per round that expanded.
	want := 1
	for _, r := range run.Rounds {
		for _, e := range r.Expansions {
			if e.Feasible > 0 {
				want++
				break
			}
		}
	}
	if tp.scorer.CallCount() != want {
		t.Errorf("%d classifier calls, want %d", tp.scorer.CallCount(), want)
	}
}

func TestLoopStopRules(t *testing.T) {
	t.Run("converged", func(t *testing.T) {
		tp := newTestPlanner(saltlight(t), testConfig())
		o := defaultReq()
		o.tags, o.backBy = []string{"Food"}, localAt(22, 0)
		batch, _ := tp.generate(t, sandy(), o)
		run := tp.run(t, batch.RunID)
		if len(run.Rounds) != 1 || run.Rounds[0].Stop != "converged" || len(run.Rounds[0].Issues) != 0 {
			t.Errorf("rounds %d, stop %q, issues %+v", len(run.Rounds), run.Rounds[0].Stop, run.Rounds[0].Issues)
		}
	})
	t.Run("last round", func(t *testing.T) {
		cfg := testConfig()
		cfg.Rounds = 1
		tp := newTestPlanner(saltlight(t), cfg)
		o := defaultReq()
		o.tags = []string{"Art", "Nerdy", "Active"}
		batch, _ := tp.generate(t, sandy(), o)
		run := tp.run(t, batch.RunID)
		if len(run.Rounds) != 1 || run.Rounds[0].Stop != "last_round" {
			t.Errorf("rounds %d, stop %q", len(run.Rounds), run.Rounds[0].Stop)
		}
	})
	t.Run("soft budget", func(t *testing.T) {
		cfg := testConfig()
		cfg.SoftBudget = time.Millisecond
		tp := newTestPlanner(saltlight(t), cfg)
		// Every reading moves the clock a second on (the planner reads it
		// from several goroutines, hence the lock).
		var mu sync.Mutex
		tick := testNow
		tp.Planner.Clock = ClockFunc(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			tick = tick.Add(time.Second)
			return tick
		})
		o := defaultReq()
		o.tags = []string{"Art", "Nerdy", "Active"}
		batch, _ := tp.generate(t, sandy(), o)
		run := tp.run(t, batch.RunID)
		if len(run.Rounds) != 1 || run.Rounds[0].Stop != "soft_budget" {
			t.Errorf("rounds %d, stop %q", len(run.Rounds), run.Rounds[0].Stop)
		}
		if len(batch.Options) == 0 {
			t.Error("the budget ends the loop, not the answer")
		}
	})
	t.Run("nothing to add", func(t *testing.T) {
		// Four open-all-day places and a request for art, which the city
		// does not have: round 0 boosts Art, round 1 has nothing new to try.
		var acts []models.Activity
		for i, c := range []string{"park", "viewpoint", "landmark", "garden"} {
			acts = append(acts, synthPlace(c, c, offsetKm(seasideMkt, 0.3*float64(i+1), 0.2), dailyHours(0, 24), []string{"outdoor"}, priceOf(0)))
		}
		tp := newTestPlanner(acts, testConfig())
		o := defaultReq()
		o.tags, o.pace = []string{"Art"}, "relaxed"
		o.backBy = localAt(21, 0)
		batch, _ := tp.generate(t, sandy(), o)
		run := tp.run(t, batch.RunID)
		if len(run.Rounds) != 2 || run.Rounds[1].Stop != "nothing_to_add" {
			t.Fatalf("rounds %d, stops %q / %q, issues %+v / %+v", len(run.Rounds), run.Rounds[0].Stop, run.Rounds[len(run.Rounds)-1].Stop,
				run.Rounds[0].Issues, run.Rounds[len(run.Rounds)-1].Issues)
		}
		if !containsString(run.Rounds[0].Adapted, "boost:Art") || len(run.Rounds[1].Adapted) != 0 {
			t.Errorf("adapted %v / %v", run.Rounds[0].Adapted, run.Rounds[1].Adapted)
		}
		if len(batch.Options) == 0 {
			t.Error("options expected")
		}
	})
}

func TestRelaxLadderWhenNoItineraryFits(t *testing.T) {
	// One park 1.5 km away and a 40-minute window: it passes the filters
	// (open, in range) but no visit plus the walk there and back fits.
	// (MinCandidates is off: the retrieval's own range step is tested below.)
	park := synthPlace("Far park", "park", offsetKm(seasideMkt, 0, 1.5), dailyHours(0, 24), []string{"outdoor"}, priceOf(0))
	cfg := testConfig()
	cfg.MinCandidates = 0
	tp := newTestPlanner([]models.Activity{park}, cfg)
	o := defaultReq()
	o.backBy = localAt(18, 40)
	batch, _ := tp.generate(t, sandy(), o)
	if len(batch.Options) != 0 || batch.Reason != "no_feasible_itinerary" || !batch.Done {
		t.Fatalf("batch = %+v", batch)
	}
	if len(batch.Relaxed) != 1 || batch.Relaxed[0] != "range" {
		t.Errorf("relaxed = %v", batch.Relaxed)
	}
	run := tp.run(t, batch.RunID)
	if len(run.Rounds) != 3 || run.Rounds[2].Stop != "last_round" {
		t.Fatalf("rounds %d", len(run.Rounds))
	}
	if !hasIssue(run.Rounds[0].Issues, "no_plans", "") {
		t.Errorf("issues %+v", run.Rounds[0].Issues)
	}
	if !containsString(run.Rounds[0].Adapted, "k:32,slots:18") || !containsString(run.Rounds[1].Adapted, "range") {
		t.Errorf("ladder: %v then %v", run.Rounds[0].Adapted, run.Rounds[1].Adapted)
	}
	if run.Rounds[1].K != 32 || run.Final.Reason != "no_feasible_itinerary" {
		t.Errorf("k %d, final %+v", run.Rounds[1].K, run.Final)
	}
}

func TestRangeRelaxedOnceWhenTooFewFit(t *testing.T) {
	// Walkable range: legs of 2 km (to and from home too), a 4 km search.
	// Three places 2.5 km out fit the search but no leg: fewer than
	// MinCandidates, so the range is relaxed once (legs of 3 km, a search
	// twice as wide) and they become reachable.
	var acts []models.Activity
	for i, c := range []string{"park", "landmark", "viewpoint"} {
		acts = append(acts, synthPlace(c, c, offsetKm(seasideMkt, 0.2*float64(i), 2.5), dailyHours(0, 24), []string{"outdoor"}, priceOf(0)))
	}
	tp := newTestPlanner(acts, testConfig())
	batch, spec := tp.generate(t, sandy(), defaultReq())
	if len(batch.Options) == 0 {
		t.Fatalf("no options after relaxing: %s", batch.Reason)
	}
	if len(batch.Relaxed) != 1 || batch.Relaxed[0] != "range" {
		t.Errorf("relaxed = %v", batch.Relaxed)
	}
	pool := tp.pool(t, batch.RunID)
	if pool.Window.MaxLegKm != 3 || pool.Spec.RadiusKm != 8 {
		t.Errorf("leg %.2f radius %.2f", pool.Window.MaxLegKm, pool.Spec.RadiusKm)
	}
	eff := effectiveSpec(t, spec, pool, batch.Relaxed)
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		assertGuarantees(t, eff, opt, catalogByID(acts))
	}
	if n := len(tp.source.Calls); n < 2 || tp.source.Calls[0].RadiusKm != 4 || tp.source.Calls[1].RadiusKm != 8 {
		t.Errorf("%d queries; the second should use the relaxed radius", n)
	}

	// Nothing even after relaxing: an empty batch with the reason.
	far := []models.Activity{synthPlace("Very far", "park", offsetKm(seasideMkt, 0, 40), dailyHours(0, 24), nil, priceOf(0))}
	tp = newTestPlanner(far, testConfig())
	batch, _ = tp.generate(t, sandy(), defaultReq())
	if len(batch.Options) != 0 || batch.Reason != "no_candidates_fit_window" || len(batch.Relaxed) != 1 {
		t.Errorf("batch = %+v", batch)
	}
	if run := tp.run(t, batch.RunID); run.Final.Reason != "no_candidates_fit_window" || len(run.Rounds) != 0 {
		t.Errorf("run final %+v rounds %d", run.Final, len(run.Rounds))
	}
}

func TestSnappedStartIsReported(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.start, o.end = techSquare, techSquare // Sandy's phone is in Atlanta
	batch, spec := tp.generate(t, sandy(), o)
	if !spec.SnappedStart || !containsString(batch.Relaxed, "snapped_start") || len(batch.Options) == 0 {
		t.Errorf("snapped=%v relaxed=%v options=%d", spec.SnappedStart, batch.Relaxed, len(batch.Options))
	}
}
