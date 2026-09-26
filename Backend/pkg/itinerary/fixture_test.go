package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Fixtures are real activities from the ingestion output for 26 Sep 2026
// (Atlanta: events plus the 60 best-rated places within 6 km of Tech Square;
// NYC: events only). "score" is a deterministic stand-in for ranker output.
// Regenerate with the extractor described in testdata/README.md.
func loadFixture(t *testing.T, name string) []models.Activity {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var acts []models.Activity
	if err := json.Unmarshal(b, &acts); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return acts
}

func TestFixtures(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		start    travel.Point
		from, to time.Time
		mode     travel.Mode
		maxLegKm float64
		pace     string
	}{
		{"atlanta evening walk", "atlanta_2026-09-26.json", techSquare, at(17, 0), at(23, 30), travel.Walk, 3, "balanced"},
		{"atlanta afternoon transit", "atlanta_2026-09-26.json", techSquare, at(12, 0), at(23, 59), travel.Transit, 10, "packed"},
		{"nyc evening transit", "nyc_2026-09-26.json", travel.Point{Lat: 40.7580, Lng: -73.9855}, at(17, 0), at(23, 59), travel.Transit, 10, "balanced"},
		{"nyc day packed", "nyc_2026-09-26.json", travel.Point{Lat: 40.7580, Lng: -73.9855}, at(10, 0), at(23, 59), travel.Transit, 10, "packed"},
	}
	cfg := DefaultConfig()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acts := loadFixture(t, c.file)
			start := c.start
			w := Window{From: c.from.UTC(), BackBy: c.to.UTC(), TZ: ny, Start: &start, End: &start,
				Mode: c.mode, DriveLabel: "drive", MaxLegKm: c.maxLegKm, Pace: c.pace}

			began := time.Now()
			nodes, drops := BuildNodes(w, acts, cfg)
			g := BuildGraph(context.Background(), w, nodes, travel.Heuristic{}, cfg)
			pool := Diverse(Solve(g, w, cfg), cfg.Mu)
			took := time.Since(began)

			edges := 0
			for _, in := range g.In {
				edges += len(in)
			}
			t.Logf("%d candidates -> %d nodes, %d edges, %d itineraries in %v; drops %v",
				len(acts), len(nodes), edges, len(pool), took, countReasons(drops))
			if len(pool) == 0 {
				t.Fatal("no itineraries")
			}
			if took > 250*time.Millisecond {
				t.Errorf("optimizer took %v", took)
			}
			for i, it := range pool {
				checkFeasible(t, w, it, cfg)
				ev := Evaluate(context.Background(), w, planStops(it), travel.Heuristic{})
				if ev.BrokenAt != -1 || ev.MinutesLate != 0 {
					t.Errorf("%s re-times as late: %+v", names(it), ev)
				}
				if i < 3 {
					t.Logf("#%d u=%.3f travel=%dm wait=%dm late_risk=%v  %s", i+1, it.Utility, it.TravelMin, it.WaitMin, it.LateRisk, names(it))
				}
			}
		})
	}
}

func countReasons(drops []Drop) map[string]int {
	out := map[string]int{}
	for _, d := range drops {
		out[d.Reason]++
	}
	return out
}

// planStops converts an itinerary the way the handler does, for Evaluate.
func planStops(it Itinerary) []models.PlanStop {
	out := make([]models.PlanStop, len(it.Stops))
	for i, s := range it.Stops {
		start, end := s.Node.Start, s.Node.End
		out[i] = models.PlanStop{
			Lat: s.Node.Loc.Lat, Lng: s.Node.Loc.Lng,
			ArriveTime: &start, DepartTime: &end,
			DurationMin: int(end.Sub(start).Minutes()),
			Flexible:    s.Node.Flexible,
		}
	}
	return out
}
