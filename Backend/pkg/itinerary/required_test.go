package itinerary

import (
	"Backend/pkg/models"
	"testing"
)

// Required visits (Config.Required, the planner's must-see picks): every
// itinerary makes them, whatever the soft rules would say.

func requiring(cfg Config, acts ...models.Activity) Config {
	for _, a := range acts {
		cfg.Required = append(cfg.Required, a.ID.Hex())
	}
	return cfg
}

// visits reports whether the itinerary has a stop at the activity.
func visits(it Itinerary, a models.Activity) bool {
	for _, s := range it.Stops {
		if s.Node.Act.ID == a.ID {
			return true
		}
	}
	return false
}

func TestRequiredVisitBeyondTheRange(t *testing.T) {
	w := window(at(18, 0), at(23, 30)) // walking, 3 km legs
	near := event("Near", offset(0.5, 0), at(21, 30), 60, 0.9)
	far := event("Far expo", offset(0, 6), at(20, 0), 60, 0.2) // 6 km out, below the bar
	far.Category = "comedy"
	if its, _ := plan(w, []models.Activity{near, far}, DefaultConfig()); len(its) == 0 || visits(its[0], far) {
		t.Fatal("without the requirement the far expo is out of range")
	}
	cfg := requiring(DefaultConfig(), far)
	its, _ := plan(w, []models.Activity{near, far}, cfg)
	if len(its) == 0 {
		t.Fatal("a required visit beyond the range should be walked to")
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
		if !visits(it, far) {
			t.Errorf("%s skips the required expo", names(it))
		}
		if it.Legs[0].DistanceKm < 5.9 || it.Legs[len(it.Legs)-1].DistanceKm < 5.9 {
			t.Errorf("%s: legs %+v should run to the expo and back", names(it), it.Legs)
		}
	}
}

func TestRequiredVisitsWithoutAFit(t *testing.T) {
	w := window(at(18, 0), at(23, 30))
	gig := event("Gig", offset(0.5, 0), at(19, 0), 60, 0.9)
	early := event("Matinee", offset(0.2, 0), at(14, 0), 60, 0.9) // before the window
	early.Category = "comedy"
	clash := event("Clash", offset(0.3, 0.1), at(19, 30), 60, 0.9) // overlaps the gig
	clash.Category = "cinema"
	cases := []struct {
		name     string
		acts     []models.Activity
		required []models.Activity
	}{
		{"a required visit outside the window", []models.Activity{gig, early}, []models.Activity{early}},
		{"two required visits at the same time", []models.Activity{gig, clash}, []models.Activity{gig, clash}},
		{"a required id the solve was not given", []models.Activity{gig}, []models.Activity{gig, clash}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if its, _ := plan(w, c.acts, requiring(DefaultConfig(), c.required...)); len(its) != 0 {
				t.Errorf("expected nothing, got %s", names(its[0]))
			}
		})
	}
}

func TestRequiredSeriesIsVisitedOnce(t *testing.T) {
	w := window(at(12, 0), at(18, 0))
	var slots []models.Activity
	for h := 13; h < 16; h++ {
		a := withEnd(event("Exhibit", techSquare, at(h, 0), 30, 0.9), at(h, 30))
		a.VenueName = str("Museum")
		slots = append(slots, a)
	}
	// The 14:00 slot is required: the other slots of the exhibition go.
	cfg := requiring(DefaultConfig(), slots[1])
	its, drops := plan(w, slots, cfg)
	if len(its) == 0 {
		t.Fatal("no itinerary")
	}
	for _, it := range its {
		if len(it.Stops) != 1 || !visits(it, slots[1]) {
			t.Errorf("%s: want the 14:00 slot alone", names(it))
		}
	}
	if got := countReasons(drops)["required_series"]; got != 2 {
		t.Errorf("drops %v", countReasons(drops))
	}
	// Two slots of one series can't both be visited.
	if its, _ := plan(w, slots, requiring(DefaultConfig(), slots[0], slots[2])); len(its) != 0 {
		t.Errorf("two required slots of one exhibition: %s", names(its[0]))
	}
}

func TestRequiredVisitsRaiseTheStopCapAndShareCategories(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(12, 0), at(23, 0))
	w.Pace = "chill" // at most 3 stops
	var shows []models.Activity
	for i, h := range []int{13, 15, 17, 19} {
		name := []string{"Hamlet", "Macbeth", "Othello", "Lear"}[i] // four theater shows
		shows = append(shows, event(name, offset(0.1*float64(i), 0), at(h, 0), 60, 0.9))
	}
	its, _ := plan(w, shows, requiring(cfg, shows...))
	if len(its) == 0 || len(its[0].Stops) != 4 {
		t.Fatalf("four required theater shows at a chill pace: %v", len(its))
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
	}
	// A required show takes its category: no other theater joins it.
	extra := event("Other play", offset(0.2, 0.1), at(21, 0), 60, 0.95)
	its, _ = plan(w, []models.Activity{shows[0], extra}, requiring(cfg, shows[0]))
	for _, it := range its {
		if visits(it, extra) {
			t.Errorf("%s: a second theater next to the required one", names(it))
		}
	}
}

func TestRequiredVisitsMayWaitForEachOther(t *testing.T) {
	cfg := DefaultConfig() // balanced: 60 minutes' longest wait
	w := window(at(12, 0), at(23, 0))
	lunch := event("Lunch show", offset(0.3, 0), at(13, 0), 60, 0.9)
	late := event("Late show", offset(0.4, 0), at(18, 0), 60, 0.9)
	late.Category = "comedy"
	if its, _ := plan(w, []models.Activity{lunch, late}, cfg); len(its) > 0 && len(its[0].Stops) == 2 {
		t.Fatal("four idle hours between two stops should not pass without the requirement")
	}
	its, _ := plan(w, []models.Activity{lunch, late}, requiring(cfg, lunch, late))
	if len(its) != 1 || len(its[0].Stops) != 2 || its[0].WaitMin < 200 {
		t.Fatalf("required shows four hours apart: %d itineraries", len(its))
	}
}

func TestDiverseIgnoresTheRequiredVisit(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 0), at(23, 30))
	pick := event("Pick", offset(0.2, 0), at(20, 0), 60, 0.6)
	pick.Category = "comedy"
	a := event("A", offset(0.3, 0), at(18, 0), 60, 0.95)
	b := event("B", offset(0.3, 0.1), at(18, 0), 60, 0.9)
	b.Category = "cinema"
	its, _ := plan(w, []models.Activity{pick, a, b}, requiring(cfg, pick))
	ordered := Diverse(its, cfg.Mu)
	if len(ordered) < 2 {
		t.Fatalf("got %d itineraries", len(ordered))
	}
	// {Pick, A} then {Pick, B}: sharing only the required visit is no
	// overlap, so the second keeps its place ahead of {Pick} alone.
	if !visits(ordered[0], a) || !visits(ordered[1], b) || len(ordered[1].Stops) != 2 {
		t.Errorf("order: %s | %s", names(ordered[0]), names(ordered[1]))
	}
	if o := jaccard(seriesSet(ordered[0]), seriesSet(ordered[1])); o != 0 {
		t.Errorf("overlap %.2f counts the required visit", o)
	}
}

func TestRequiredPlaceIsVisitedWhenItFits(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 0), at(23, 0))
	show := event("Show", offset(0.5, 0), at(19, 0), 90, 0.95)         // 19:00–20:30
	cafe := place("Cafe", "cafe", offset(0.4, 0.1), daily(7, 18), 0.3) // closes at 18:00, below the bar
	its, _ := plan(w, []models.Activity{show, cafe}, requiring(cfg, cafe))
	if len(its) == 0 {
		t.Fatal("the cafe fits before it closes")
	}
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
		if !visits(it, cafe) {
			t.Errorf("%s skips the required cafe", names(it))
		}
		for _, s := range it.Stops {
			if s.Node.Act.ID == cafe.ID && s.Node.End.After(at(18, 0)) {
				t.Errorf("cafe visit runs to %v, after closing", s.Node.End.In(ny))
			}
		}
	}
	if !visits(its[0], show) {
		t.Errorf("best %s should still take the show after the cafe", names(its[0]))
	}
}
