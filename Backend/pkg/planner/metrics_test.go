package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"testing"
	"time"
)

func TestPlanScoreWeights(t *testing.T) {
	w := DefaultConfig().Weights
	perfect := PlanMetrics{Fit: 1, Fill: 1, Coverage: 1, Variety: 1, PaceFit: 1}
	if s := planScore(perfect, w); !approx(s, 1, 1e-12) {
		t.Errorf("perfect plan = %v, want 1", s)
	}
	late := perfect
	late.LateRisk = true
	if s := planScore(late, w); !approx(s, 0.95, 1e-12) {
		t.Errorf("late risk = %v", s)
	}
	spent := perfect
	spent.BudgetUse = 1.0 // 10 % over the 0.9 threshold costs 0.05
	if s := planScore(spent, w); !approx(s, 0.95, 1e-12) {
		t.Errorf("budget use 1.0 = %v", s)
	}
	busy := perfect
	busy.TravelShare, busy.IdleShare = 0.5, 0.2
	if s := planScore(busy, w); !approx(s, 1-0.075-0.01, 1e-12) {
		t.Errorf("travel/idle = %v", s)
	}
	weak := perfect
	weak.WeakStops = 2
	if s := planScore(weak, w); !approx(s, 0.8, 1e-12) {
		t.Errorf("two weak stops = %v", s)
	}
	if s := planScore(PlanMetrics{LateRisk: true, BudgetUse: 5, TravelShare: 1, IdleShare: 1}, w); s != 0 {
		t.Errorf("score must clamp at 0, got %v", s)
	}
}

// plan builds a ScoredPlan over the given series keys.
func plan(score float64, keys ...string) ScoredPlan {
	var it itinerary.Itinerary
	for _, k := range keys {
		it.Stops = append(it.Stops, itinerary.Stop{Node: &itinerary.Node{SeriesKey: k}})
	}
	return ScoredPlan{It: it, Score: score, Signature: signature(it)}
}

func TestMergeBestAndTop3(t *testing.T) {
	best := []ScoredPlan{plan(0.7, "a", "b"), plan(0.6, "c")}
	fresh := []ScoredPlan{plan(0.75, "b", "a"), plan(0.65, "a", "d"), plan(0.5, "e")}
	got := mergeBest(best, fresh, 10, 0.5)
	if len(got) != 4 {
		t.Fatalf("%d plans, want 4 (a|b deduped)", len(got))
	}
	if got[0].Signature != "a|b" || got[0].Score != 0.75 {
		t.Errorf("best = %s %.2f", got[0].Signature, got[0].Score)
	}
	// Diversity: "c" (0.6, no overlap) beats "a|d" (0.65 − 0.5·⅓).
	if got[1].Signature != "c" {
		t.Errorf("second = %s", got[1].Signature)
	}
	if s := top3Sum(got); !approx(s, 0.75+0.65+0.6, 1e-12) {
		t.Errorf("top3 = %v (the three best scores, not the display order)", s)
	}
	if got := mergeBest(got, nil, 2, 0.5); len(got) != 2 || got[0].Signature != "a|b" {
		t.Errorf("pool size cap: %d", len(got))
	}
	if signature(plan(0, "x", "y").It) != signature(plan(0, "y", "x").It) {
		t.Error("signatures ignore order")
	}
}

func TestEvaluateMetrics(t *testing.T) {
	cfg := testConfig()
	food, _ := FacetByName("Food")
	art, _ := FacetByName("Art")
	diner := synthPlace("Diner", "restaurant", offsetKm(seasideMkt, 0.4, 0), dailyHours(0, 24), []string{"food"}, priceOf(10))
	park := synthPlace("Park", "park", offsetKm(seasideMkt, 0, 0.5), dailyHours(0, 24), []string{"outdoor"}, priceOf(0))
	spec := PlanSpec{
		Start: &seasideMkt, End: &seasideMkt, TZ: ny, From: localAt(18, 0).UTC(), BackBy: localAt(22, 0).UTC(),
		Mode: travel.Walk, DriveLabel: "drive", MaxLegKm: 2, Pace: "balanced",
		Budget: Budget{Tier: 2, TotalCents: 7000}, Facets: []Facet{food, art},
	}
	run := &Run{Spec: spec, Cfg: cfg, ItCfg: cfg.Itinerary, Pool: NewPool(), Window: spec.Window()}
	for _, a := range []models.Activity{diner, park} {
		c := newCandidate(a)
		ml := map[string]float64{"Diner": 0.8, "Park": 0.6}[a.Name]
		c.ML = &ml
		run.Pool.Add(c)
	}
	itCfg := cfg.Itinerary
	itCfg.Utility = run.utilityFn()
	nodes, _ := itinerary.BuildNodes(run.Window, run.Pool.Activities(), itCfg)
	g := itinerary.BuildGraph(context.Background(), run.Window, nodes, travel.Heuristic{}, itCfg)
	var two itinerary.Itinerary
	for _, it := range itinerary.Solve(g, run.Window, itCfg) {
		if len(it.Stops) == 2 {
			two = it
			break
		}
	}
	if len(two.Stops) != 2 {
		t.Fatal("no two-stop itinerary")
	}
	sp := run.evaluate(two)
	m := sp.Metrics
	if !approx(m.Fit, 0.7, 1e-12) || m.Coverage != 0.5 || m.Variety != 1 || !approx(m.PaceFit, 2.0/3, 1e-12) {
		t.Errorf("fit %v coverage %v variety %v pace %v", m.Fit, m.Coverage, m.Variety, m.PaceFit)
	}
	if len(m.CoveredFacets) != 1 || m.CoveredFacets[0] != "Food" || len(m.MissingFacets) != 1 || m.MissingFacets[0] != "Art" {
		t.Errorf("facets %v / %v", m.CoveredFacets, m.MissingFacets)
	}
	if !approx(m.BudgetUse, 1000.0/7000, 1e-12) || m.Stops != 2 {
		t.Errorf("budget use %v stops %d", m.BudgetUse, m.Stops)
	}
	span := two.Arrival.Sub(two.Depart).Minutes()
	if !approx(m.TravelShare, float64(two.TravelMin)/span, 1e-12) || !approx(m.IdleShare, float64(two.WaitMin)/240, 1e-12) {
		t.Errorf("travel %v idle %v", m.TravelShare, m.IdleShare)
	}
	if !approx(sp.Score, planScore(m, cfg.Weights), 1e-12) {
		t.Error("score must be planScore(metrics)")
	}
	// Fit ignores facet boosts; the solver's utility does not.
	run.Boosts = map[string]float64{"Food": 0.1}
	if run.evaluate(two).Metrics.Fit != m.Fit {
		t.Error("boosts must not change fit")
	}
	diner0 := run.Pool.Get(diner.ID.Hex())
	if u := run.utilityFn()(&diner0.Act); !approx(u, 0.9, 1e-12) {
		t.Errorf("boosted utility %v", u)
	}
	run.Boosts = map[string]float64{"Food": 0.3}
	if u := run.utilityFn()(&diner0.Act); !approx(u, 0.95, 1e-12) {
		t.Errorf("boost cap: %v", u)
	}
	_ = time.Minute
}
