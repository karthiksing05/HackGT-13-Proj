package planner

import (
	"Backend/pkg/models"
	"slices"
	"testing"
	"time"
)

// Places rated below MinPlaceRating are dropped, and unrated ones unless
// they are hikes; the query and the Go check agree.
func TestPlaceRatingRule(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	spec := tp.spec(t, sandy(), defaultReq())
	q := baseQuery(&spec, tp.Cfg, radiusFor(&spec, tp.Cfg, spec.MaxLegKm))
	near := offsetKm(seasideMkt, 0.3, 0)
	rated := func(name, cat string, r *float64) models.Activity {
		a := synthPlace(name, cat, near, dailyHours(0, 24), []string{"outdoor"}, priceOf(0))
		a.Rating = r
		return a
	}
	good, poor := rated("Good park", "park", f64p(4.0)), rated("Poor park", "park", f64p(3.9))
	trail, unrated := rated("Trail", "hike", nil), rated("Unrated park", "park", nil)
	drops := map[string]int{}
	got := Feasible(&spec, &q, tp.Cfg.Itinerary, []models.Activity{good, poor, trail, unrated}, drops, feasibleOpts{})
	if len(got) != 2 || got[0].ID != good.ID.Hex() || got[1].ID != trail.ID.Hex() || drops["low_rating"] != 2 {
		t.Errorf("kept %d, drops %v", len(got), drops)
	}
	for _, a := range []models.Activity{poor, unrated} {
		if MatchesQuery(&a, &q) {
			t.Errorf("%s: the query must drop it too", a.Name)
		}
	}
}

// Avoided tags are matched without case, against tags and categories.
func TestAvoidTagsCoverCategories(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	user := sandy()
	user.Prefs.AvoidTags = []string{"LIVE_MUSIC", " Crowded "}
	spec := tp.spec(t, user, defaultReq())
	if !slices.Contains(spec.Hard.ExcludeCategories, "live_music") || !slices.Contains(spec.Hard.ExcludeTags, "crowded") ||
		!slices.Contains(spec.AvoidTags, "live_music") {
		t.Errorf("hard %+v avoid %v", spec.Hard, spec.AvoidTags)
	}
	gig := synthEvent("Gig", "live_music", offsetKm(seasideMkt, 0.5, 0), localAt(19, 0), 60, nil, priceOf(5))
	q := baseQuery(&spec, tp.Cfg, 5)
	drops := map[string]int{}
	if got := Feasible(&spec, &q, tp.Cfg.Itinerary, []models.Activity{gig}, drops, feasibleOpts{}); len(got) != 0 || drops["excluded_category"] != 1 {
		t.Errorf("gig kept: %d, drops %v", len(got), drops)
	}
}

// Timed-entry slots of one exhibition take one ranker slot and share its
// scores; every slot still reaches the pool.
func TestSeriesShareOneRankerSlot(t *testing.T) {
	var acts []models.Activity
	for i := range 4 {
		a := synthEvent("Balloon Museum", "gallery", offsetKm(seasideMkt, 0.5, 0), localAt(18, 30*i), 30, []string{"art"}, priceOf(10))
		a.VenueName = strp("Balloon Hall")
		a.End = timep(localAt(18, 30*i).Add(30 * time.Minute))
		acts = append(acts, a)
	}
	acts = append(acts, synthPlace("Park", "park", offsetKm(seasideMkt, 0, 0.5), dailyHours(0, 24), []string{"outdoor"}, priceOf(0)))
	tp := newTestPlanner(acts, testConfig())
	batch, _ := tp.generate(t, sandy(), defaultReq())
	run := tp.run(t, batch.RunID)
	if run.Counts.SeriesSiblings != 3 || run.Counts.Shortlist != 2 {
		t.Errorf("siblings %d, shortlist %d", run.Counts.SeriesSiblings, run.Counts.Shortlist)
	}
	scored := 0
	for _, c := range tp.scorer.Calls[0].Candidates {
		if c.Act.Name == "Balloon Museum" {
			scored++
		}
	}
	if scored != 1 {
		t.Errorf("the ranker saw %d slots of one exhibition", scored)
	}
	pool := tp.pool(t, batch.RunID)
	var ml []float64
	for _, a := range acts[:4] {
		ps, ok := pool.Scores[a.ID.Hex()]
		if !ok || ps.ML == nil {
			t.Fatalf("slot %s missing from the pool", a.Start.In(ny).Format("15:04"))
		}
		ml = append(ml, *ps.ML)
	}
	if ml[0] != ml[1] || ml[1] != ml[2] || ml[2] != ml[3] {
		t.Errorf("slots scored apart: %v", ml)
	}
}

// One solve sees the best SolverEvents event series and SolverPlaces place
// series by utility.
func TestSolverCaps(t *testing.T) {
	cfg := testConfig()
	cfg.SolverEvents, cfg.SolverPlaces = 1, 2
	run := &Run{Cfg: cfg, Pool: NewPool()}
	score := map[string]float64{}
	add := func(a models.Activity, s float64) {
		run.Pool.Add(newCandidate(a))
		score[a.ID.Hex()] = s
	}
	near := offsetKm(seasideMkt, 0.3, 0)
	add(synthEvent("E1", "comedy", near, localAt(19, 0), 60, nil, nil), 0.6)
	add(synthEvent("E2", "theater", near, localAt(20, 0), 60, nil, nil), 0.9)
	add(synthPlace("P1", "park", near, nil, nil, nil), 0.7)
	add(synthPlace("P2", "garden", near, nil, nil, nil), 0.8)
	add(synthPlace("P3", "hike", near, nil, nil, nil), 0.5)
	acts, cut := run.solverActivities(func(a *models.Activity) float64 { return score[a.ID.Hex()] })
	var names []string
	for _, a := range acts {
		names = append(names, a.Name)
	}
	if cut != 2 || len(names) != 3 || names[0] != "E2" || names[1] != "P1" || names[2] != "P2" {
		t.Errorf("kept %v, cut %d", names, cut)
	}
}

// Bakeries, dessert shops and food halls are Food; breweries are
// Nightlife (and age-gated like bars).
func TestNewCategoriesJoinTheirFacets(t *testing.T) {
	food, _ := FacetByName("Food")
	for _, c := range []string{"bakery", "dessert", "food_hall"} {
		if !slices.Contains(food.Cats, c) {
			t.Errorf("Food misses %s", c)
		}
	}
	night, _ := FacetByName("Nightlife")
	if !slices.Contains(night.Cats, "brewery") {
		t.Error("Nightlife misses brewery")
	}
	brewery := models.Activity{Kind: "place", Category: "brewery"}
	if !AgeRulesFor("18_20").Blocks(&brewery) || AgeRulesFor("21_plus").Blocks(&brewery) {
		t.Error("breweries are for 21+")
	}
}

// Walking radius: a round trip's stops are at most ceil(MaxStops/2) legs
// from home, now that the home legs are within the range too.
func TestRadiusFollowsTheHomeLegs(t *testing.T) {
	cfg := DefaultConfig()
	spec := PlanSpec{Pace: "balanced", Start: &seasideMkt, End: &seasideMkt}
	if r := radiusFor(&spec, cfg, 2); r != 4 {
		t.Errorf("walkable balanced: %v", r)
	}
	spec.Pace = "packed"
	if r := radiusFor(&spec, cfg, 10); r != 30 {
		t.Errorf("transit packed: %v", r)
	}
	end := offsetKm(seasideMkt, 3, 0)
	spec.Pace, spec.End = "balanced", &end
	if r := radiusFor(&spec, cfg, 2); r < 5.49 || r > 5.51 {
		t.Errorf("a separate end point adds half its distance: %v", r)
	}
}
