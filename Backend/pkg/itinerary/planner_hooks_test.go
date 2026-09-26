package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"testing"
	"time"
)

// Eighty distinct series in one solve: the mask has to reach past bit 63
// and the cap must leave them all in.
func TestSeriesCapAbove64(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Paces["balanced"] = PaceProfile{MaxStops: 100, MaxWait: 60 * time.Minute, LambdaWait: 0}
	w := window(at(0, 0), at(0, 0).Add(48*time.Hour))
	var acts []models.Activity
	for i := 0; i < 80; i++ {
		a := event("E", techSquare, at(1, 0).Add(time.Duration(i)*30*time.Minute), 15, 0.9)
		a.Name = a.ID.Hex()
		a.VenueName = str(a.Name)
		a.Category = "other"
		acts = append(acts, a)
	}
	nodes, drops := BuildNodes(w, acts, cfg)
	if len(nodes) != 80 || len(drops) != 0 {
		t.Fatalf("%d nodes, drops %+v", len(nodes), countReasons(drops))
	}
	maxSeries := 0
	for _, n := range nodes {
		if n.series > maxSeries {
			maxSeries = n.series
		}
	}
	if maxSeries < 64 {
		t.Fatalf("series indices only reach %d", maxSeries)
	}
	g := BuildGraph(context.Background(), w, nodes, travel.Heuristic{}, cfg)
	its := Solve(g, w, cfg)
	if len(its) == 0 || len(its[0].Stops) != 80 {
		t.Fatalf("expected one itinerary through all 80 series, got %d stops", len(its[0].Stops))
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
	}

	cfg.SeriesCap = 64
	_, drops = BuildNodes(w, acts, cfg)
	if countReasons(drops)["series_cap"] != 16 {
		t.Errorf("SeriesCap 64 over 80 series: drops %v", countReasons(drops))
	}
}

func TestRestaurantPlaces(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(20, 0), at(22, 30))
	diner := place("Diner", "restaurant", techSquare, nil, 0.9) // default 11-22
	cafe := place("Cafe", "cafe", techSquare, nil, 0.9)         // default 7-19: closed
	club := place("Club", "nightclub", techSquare, nil, 0.9)    // needs real hours
	openClub := place("Open club", "nightclub", techSquare, daily(21, 27), 0.9)

	nodes, drops := BuildNodes(w, []models.Activity{diner, cafe, club, openClub}, cfg)
	reasons := map[string]string{}
	for _, d := range drops {
		reasons[d.ActivityID] = d.Reason
	}
	if reasons[cafe.ID.Hex()] != "closed_during_window" || reasons[club.ID.Hex()] != "no_hours" {
		t.Errorf("drops = %+v", drops)
	}
	got := map[string]int{}
	for _, n := range nodes {
		got[n.Act.Name]++
		if n.Act.Name == "Diner" && n.End.After(time.Date(2026, 9, 26, 22, 0, 0, 0, ny)) {
			t.Errorf("diner slot %v-%v runs past the default closing time", n.Start.In(ny), n.End.In(ny))
		}
	}
	if got["Diner"] == 0 || got["Open club"] == 0 {
		t.Errorf("nodes by place = %v", got)
	}
	for _, c := range []string{"restaurant", "cafe", "bar", "nightclub"} {
		if !IsPlaceCategory(c) {
			t.Errorf("%s should be schedulable", c)
		}
	}
	if IsPlaceCategory("cinema") || len(PlaceCategories()) != len(placeCategories) {
		t.Error("PlaceCategories mismatch")
	}
}

func TestSortNodesIsDeterministic(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(22, 0))
	a := place("A", "park", techSquare, daily(6, 22), 0.8)
	b := place("B", "garden", techSquare, daily(6, 22), 0.8)
	fixed := event("Fixed", techSquare, at(18, 0), 60, 0.8)
	fixed.Category = "comedy"

	order := func(acts []models.Activity) []string {
		nodes, _ := BuildNodes(w, acts, cfg)
		out := make([]string, len(nodes))
		for i, n := range nodes {
			out[i] = n.Act.ID.Hex() + "@" + n.Start.Format("15:04")
		}
		return out
	}
	first := order([]models.Activity{b, fixed, a})
	second := order([]models.Activity{a, b, fixed})
	if len(first) != len(second) {
		t.Fatal("different node counts")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order depends on input order at %d: %s vs %s", i, first[i], second[i])
		}
	}
	// Nodes with the same start and end are ordered by activity id.
	nodes, _ := BuildNodes(w, []models.Activity{fixed, b, a}, cfg)
	for i := 1; i < len(nodes); i++ {
		p, n := nodes[i-1], nodes[i]
		if p.Start.Equal(n.Start) && p.End.Equal(n.End) && p.Act.ID.Hex() > n.Act.ID.Hex() {
			t.Errorf("nodes %d/%d at %s: %s sorted before %s", i-1, i, n.Start.In(ny).Format("15:04"), p.Act.ID.Hex(), n.Act.ID.Hex())
		}
	}
}

func TestUtilityHook(t *testing.T) {
	cfg := DefaultConfig()
	a := event("x", techSquare, at(19, 0), 60, 0.1)
	cfg.Utility = func(act *models.Activity) float64 {
		if act.ID == a.ID {
			return 0.9
		}
		return 0
	}
	if u := utility(&a, cfg); u < 0.59 || u > 0.61 {
		t.Errorf("hook 0.9 -> %.2f, want 0.6", u)
	}
	other := event("y", techSquare, at(19, 0), 60, 0.9)
	if u := utility(&other, cfg); u != 0 {
		t.Errorf("hook 0 -> %.2f, want 0", u)
	}
}
