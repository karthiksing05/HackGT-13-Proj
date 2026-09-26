package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"math"
	"math/rand"
	"testing"
	"time"
)

// checkFeasible asserts the basic promises every itinerary must keep.
func checkFeasible(t *testing.T, w Window, it Itinerary, cfg Config) {
	t.Helper()
	if len(it.Legs) != len(it.Stops)+1 {
		t.Fatalf("%d legs for %d stops", len(it.Legs), len(it.Stops))
	}
	if it.Depart.Before(w.From) || it.Arrival.After(w.BackBy) {
		t.Errorf("%s: leaves %v, back %v, window %v-%v", names(it), it.Depart.In(ny), it.Arrival.In(ny), w.From.In(ny), w.BackBy.In(ny))
	}
	seen := map[string]bool{}
	for i, s := range it.Stops {
		if seen[s.Node.SeriesKey] {
			t.Errorf("%s: series %s visited twice", names(it), s.Node.SeriesKey)
		}
		seen[s.Node.SeriesKey] = true
		if i > 0 {
			prev := it.Stops[i-1].Node
			if prev.End.Add(it.Legs[i].Duration).After(s.Node.Start) {
				t.Errorf("%s: can't reach stop %d in time", names(it), i)
			}
		}
	}
}

func TestOverlappingEventsAreNeverChained(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 30))
	acts := []models.Activity{
		event("A", offset(0.3, 0), at(18, 30), 90, 0.9),  // 18:30-20:00
		event("B", offset(0.5, 0), at(19, 30), 90, 0.95), // overlaps A
		event("C", offset(0.4, 0.2), at(20, 30), 60, 0.8),
	}
	acts[2].Category = "comedy"
	its, _ := plan(w, acts, cfg)
	if len(its) == 0 {
		t.Fatal("no itineraries")
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
	}
	if got := names(its[0]); got != "A@18:30 -> C@20:30" {
		t.Errorf("best = %s", got)
	}
}

func TestRangeCapBlocksFarLegs(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 30))
	near := event("Near", offset(0.5, 0), at(18, 30), 60, 0.9)
	far := event("Far", offset(8, 0), at(21, 0), 60, 0.9)
	far.Category = "comedy"
	its, _ := plan(w, []models.Activity{near, far}, cfg)
	for _, it := range its {
		if len(it.Stops) > 1 {
			t.Errorf("walking 8 km with a 3 km cap: %s", names(it))
		}
	}
}

func TestSeriesUsedOnce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Paces["balanced"] = PaceProfile{MaxStops: 5, MaxWait: 3 * time.Hour, LambdaWait: 0}
	w := window(at(12, 0), at(18, 0))
	var acts []models.Activity
	for h := 13; h < 17; h++ {
		a := withEnd(event("Exhibit", techSquare, at(h, 0), 30, 0.9), at(h, 30))
		a.VenueName = str("Museum")
		a.Category = "other"
		acts = append(acts, a)
	}
	its, _ := plan(w, acts, cfg)
	if len(its) == 0 {
		t.Fatal("no itineraries")
	}
	for _, it := range its {
		if len(it.Stops) != 1 {
			t.Errorf("repeated a timed-entry exhibit: %s", names(it))
		}
	}
}

func TestOneStopPerCategoryAndBudget(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 30))
	w.BudgetCents = 3000
	a := event("Gig 1", offset(0.2, 0), at(18, 30), 60, 0.9)
	b := event("Gig 2", offset(0.3, 0), at(20, 0), 60, 0.9) // same category as Gig 1
	c := event("Comedy", offset(0.2, 0.2), at(21, 30), 60, 0.9)
	c.Category = "comedy"
	a.Price = &models.ActivityPrice{Min: f64(20)}
	c.Price = &models.ActivityPrice{Min: f64(20)} // Gig 1 + Comedy = $40 > $30
	its, _ := plan(w, []models.Activity{a, b, c}, cfg)
	for _, it := range its {
		cats := map[string]bool{}
		for _, s := range it.Stops {
			if cats[s.Node.Act.Category] {
				t.Errorf("category repeated: %s", names(it))
			}
			cats[s.Node.Act.Category] = true
		}
		if it.CostCents > w.BudgetCents {
			t.Errorf("over budget: %s costs %d", names(it), it.CostCents)
		}
	}
	if got := names(its[0]); got != "Gig 2@20:00 -> Comedy@21:30" {
		t.Errorf("best = %s", got)
	}
}

func TestReturnLegMustFitBackBy(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(21, 5))
	// Ends 21:00 about 1.5 km away: the walk home doesn't fit by 21:05.
	a := event("Late show", offset(1.5, 0), at(19, 30), 90, 0.9)
	its, _ := plan(w, []models.Activity{a}, cfg)
	if len(its) != 0 {
		t.Errorf("expected nothing feasible, got %s", names(its[0]))
	}
}

func TestSingleStopAndSeparateEnd(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 0))
	end := offset(0, 1)
	w.End = &end
	its, _ := plan(w, []models.Activity{event("Only", offset(0, 0.5), at(19, 0), 90, 0.9)}, cfg)
	if len(its) != 1 || len(its[0].Stops) != 1 {
		t.Fatalf("got %d itineraries", len(its))
	}
	it := its[0]
	checkFeasible(t, w, it, cfg)
	if it.Legs[1].DistanceKm < 0.4 {
		t.Errorf("last leg should go to the separate end point, got %.2f km", it.Legs[1].DistanceKm)
	}
	if !it.Stops[0].Arrive.Before(it.Stops[0].Node.Start) {
		t.Error("should arrive before the event starts")
	}
}

func TestPlacesFillGaps(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 0), at(23, 0))
	show := event("Show", offset(0.5, 0), at(20, 0), 90, 0.9) // 20:00-21:30
	bar := place("Bar", "bar", offset(0.4, 0.1), daily(16, 24), 0.8)
	its, _ := plan(w, []models.Activity{show, bar}, cfg)
	if len(its) == 0 {
		t.Fatal("no itineraries")
	}
	if len(its[0].Stops) != 2 {
		t.Errorf("best should combine the bar and the show, got %s", names(its[0]))
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
	}
}

func TestDiversePutsBestFirstAndVaries(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 0), at(23, 30))
	acts := []models.Activity{
		event("A", offset(0.2, 0), at(17, 30), 60, 0.95),
		event("B", offset(0.3, 0), at(19, 0), 60, 0.9),
		event("C", offset(0.3, 0.1), at(19, 0), 60, 0.85),
		event("D", offset(0.1, 0.2), at(21, 0), 60, 0.8),
	}
	cats := []string{"live_music", "comedy", "gallery", "theater"}
	for i := range acts {
		acts[i].Category = cats[i]
	}
	its, _ := plan(w, acts, cfg)
	ordered := Diverse(its, cfg.Mu)
	if len(ordered) != len(its) || ordered[0].Utility != its[0].Utility {
		t.Fatal("Diverse must keep every itinerary and start with the best")
	}
	if len(ordered) > 1 && jaccard(seriesSet(ordered[0]), seriesSet(ordered[1])) == 1 {
		t.Error("second choice duplicates the first")
	}
}

func TestEvaluateFlagsLateReorder(t *testing.T) {
	w := window(at(18, 0), at(23, 0))
	mk := func(lat, lng float64, start, end time.Time) models.PlanStop {
		s, e := start.UTC(), end.UTC()
		return models.PlanStop{Lat: lat, Lng: lng, ArriveTime: &s, DepartTime: &e, DurationMin: int(e.Sub(s).Minutes())}
	}
	p1, p2 := offset(0.5, 0), offset(0.6, 0.2)
	a := mk(p1.Lat, p1.Lng, at(19, 0), at(20, 0))
	b := mk(p2.Lat, p2.Lng, at(20, 30), at(21, 30))

	ok := Evaluate(context.Background(), w, []models.PlanStop{a, b}, travel.Heuristic{})
	if ok.BrokenAt != -1 || ok.MinutesLate != 0 || len(ok.Legs) != 3 {
		t.Errorf("original order: %+v", ok)
	}
	swapped := Evaluate(context.Background(), w, []models.PlanStop{b, a}, travel.Heuristic{})
	if swapped.BrokenAt != 1 || swapped.MinutesLate <= 0 {
		t.Errorf("swapped order should be late at stop 1: %+v", swapped)
	}
}

// --- oracle -----------------------------------------------------------------

// bruteForce enumerates every time-ordered subset of nodes, checks it with
// the same graph edges and path rules as Solve, and returns the best utility.
func bruteForce(g *Graph, w Window, cfg Config) float64 {
	n := len(g.Nodes)
	edge := map[[2]int]*Edge{}
	for j := 0; j <= n; j++ {
		for ei := range g.In[j] {
			e := &g.In[j][ei]
			edge[[2]int{e.From, j}] = e
		}
	}
	pace := cfg.Pace(w.Pace)
	best := math.Inf(-1)
	for mask := 1; mask < 1<<n; mask++ {
		var seq []int
		for i := 0; i < n; i++ {
			if mask&(1<<i) != 0 {
				seq = append(seq, i)
			}
		}
		if len(seq) > pace.MaxStops {
			continue
		}
		u, prev, ok := 0.0, Source, true
		var series, cats uint64
		var cost int64
		for _, j := range seq {
			e := edge[[2]int{prev, j}]
			node := g.Nodes[j]
			sBit := uint64(1) << uint(node.series)
			if e == nil || series&sBit != 0 {
				ok = false
				break
			}
			series |= sBit
			if node.category >= 0 {
				cBit := uint64(1) << uint(node.category)
				if cats&cBit != 0 {
					ok = false
					break
				}
				cats |= cBit
			}
			cost += node.CostCents
			u += node.Utility - e.Penalty
			prev = j
		}
		if !ok || (w.BudgetCents > 0 && cost > w.BudgetCents) {
			continue
		}
		e := edge[[2]int{prev, n}]
		if e == nil {
			continue
		}
		if u -= e.Penalty; u > best {
			best = u
		}
	}
	return best
}

// Scores are drawn above the baseline, so every stop is worth something and
// the optimum usually has several stops.
func randomInstance(r *rand.Rand, n int, constrained bool) (Window, []models.Activity) {
	tau := DefaultConfig().Tau
	w := window(at(14, 0), at(23, 0))
	w.MaxLegKm = 4
	cats := []string{"live_music", "comedy", "gallery"}
	var acts []models.Activity
	for i := 0; i < n; i++ {
		start := at(14, 0).Add(time.Duration(r.Intn(15*30)) * time.Minute)
		a := event("E", offset(r.Float64()*3-1.5, r.Float64()*3-1.5), start, 30+r.Intn(4)*30, tau+0.05+(0.95-tau)*r.Float64())
		a.Name = a.ID.Hex()
		a.VenueName = str(a.Name)
		a.Category = "other"
		if constrained {
			a.Category = cats[r.Intn(len(cats))]
			a.Price = &models.ActivityPrice{Min: f64(float64(r.Intn(4) * 10))}
		}
		acts = append(acts, a)
	}
	if constrained {
		w.BudgetCents = 4000
	}
	return w, acts
}

func TestSolveMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	compared, multiStop := 0, 0
	for trial := 0; trial < 200; trial++ {
		constrained := trial%2 == 1
		cfg := DefaultConfig()
		if !constrained {
			cfg.Paces["balanced"] = PaceProfile{MaxStops: 20, MaxWait: 2 * time.Hour, LambdaWait: 0.004}
		}
		w, acts := randomInstance(r, 3+r.Intn(8), constrained)
		nodes, _ := BuildNodes(w, acts, cfg)
		g := BuildGraph(context.Background(), w, nodes, travel.Heuristic{}, cfg)
		its := Solve(g, w, cfg)
		want := bruteForce(g, w, cfg)
		if math.IsInf(want, -1) {
			if len(its) != 0 {
				t.Fatalf("trial %d: brute force found nothing, DP found %s", trial, names(its[0]))
			}
			continue
		}
		if len(its) == 0 {
			t.Fatalf("trial %d: DP found nothing, brute force %.4f", trial, want)
		}
		got := its[0].Utility
		compared++
		if len(its[0].Stops) > 1 {
			multiStop++
		}
		for _, it := range its {
			checkFeasible(t, w, it, cfg)
		}
		if !constrained && math.Abs(got-want) > 1e-9 {
			t.Fatalf("trial %d (unconstrained): DP %.6f, brute force %.6f", trial, got, want)
		}
		if constrained && want > 0 && got < 0.98*want {
			t.Fatalf("trial %d (constrained): DP %.6f < 98%% of %.6f", trial, got, want)
		}
	}
	t.Logf("compared %d instances, %d with multi-stop optima", compared, multiStop)
	if compared < 100 || multiStop < 50 {
		t.Fatalf("oracle too weak: %d compared, %d multi-stop", compared, multiStop)
	}
}

func TestDiverseCollapsesTimeShiftedCopies(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(15, 0), at(20, 0))
	expo := dropIn(event("Expo", offset(0.5, 0), at(10, 0), 90, 0.9), at(19, 0))
	its, _ := plan(w, []models.Activity{expo}, cfg)
	if len(its) < 2 {
		t.Fatalf("expected several time-shifted copies before dedupe, got %d", len(its))
	}
	if got := Diverse(its, cfg.Mu); len(got) != 1 {
		t.Errorf("same stop at different times should collapse to one, got %d", len(got))
	}
}

func TestHomeLegsRespectRange(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(15, 0), at(23, 0))
	w.MaxLegKm = 3
	far := event("Far expo", offset(0, 4), at(17, 0), 90, 0.9) // 4 km from home
	if its, _ := plan(w, []models.Activity{far}, cfg); len(its) != 0 {
		t.Errorf("a stop beyond range_km from home should be unreachable, got %s", names(its[0]))
	}
}
