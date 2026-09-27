package planner

import (
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Must-see picks from the Saltlight fixture (a copy of the demo catalog);
// times are Saturday 26 Sep unless said, distances from Seaside Market.
const (
	jazzID     = "9bdbbc5e9aab712a5b769f72" // Sunset Jazz on Pier Nine: 18:30–21:00, live music, $12, 1.2 km
	standupID  = "e8af462b51ee8965c54eaba7" // Seasick Standup at the Crow's Nest: 21:00–22:40, comedy, 21+
	brunchID   = "74f8f86912cec7202ecab5ec" // Harbor Dumpling Brunch Crawl: Sunday morning
	cleanupID  = "44a3d2a124dc490b8c64e997" // Kelp Forest Kayak Cleanup: 9:00–12:00
	labID      = "82b634e6143f34fcaed7b3de" // The Lighthouse Laboratory: a museum 4.6 km out, Wed–Sun 11–19
	fallsID    = "9d5ff03a09a5e4ec4ad46352" // Lantern Falls Trail: 7 km out, 6–18
	captainsID = "eeaf196d1433e3afb2d1f020" // The Captain's Table: $70 and up, 17:30–22:00
	phoID      = "ab71e6cf41394229d8b4febd" // Pho Real Noodle House: a restaurant, 10:30–21:30
	rustyID    = "27909f9bdb2cf85d038e0198" // The Rusty Anchor: a bar
	cinemaID   = "77b4858e84b7392fa2a485c4" // Lanternfall Cinema: a venue plans never visit
)

func hasActivity(opt Option, id string) bool {
	for _, s := range opt.Stops {
		if s.ActivityID == id {
			return true
		}
	}
	return false
}

func stopOf(opt Option, id string) Stop {
	for _, s := range opt.Stops {
		if s.ActivityID == id {
			return s
		}
	}
	return Stop{}
}

// withPicks generates o (o.picks set) and checks every option on every
// page: the guarantees, with the picks' exemptions, and, on its own, that
// the option visits every pick. It returns the batch, the effective spec
// and every option.
func withPicks(t *testing.T, tp *testPlanner, user *UserContext, o reqOpts) (Batch, PlanSpec, []Option) {
	t.Helper()
	batch, spec := tp.generate(t, user, o)
	if len(batch.Options) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}
	eff := effectiveSpec(t, spec, tp.pool(t, batch.RunID), batch.Relaxed)
	catalog := catalogByID(tp.source.Activities)
	all := tp.allOptions(t, user, batch)
	for _, opt := range all {
		assertGuarantees(t, eff, opt, catalog)
		for _, id := range o.picks {
			if !hasActivity(opt, id) {
				t.Errorf("option %s (%s) skips the pick %s", opt.ID, opt.Name, catalog[id].Name)
			}
		}
	}
	if run := tp.run(t, batch.RunID); run.Final.Rejected != 0 {
		t.Errorf("the runtime check rejected %d options", run.Final.Rejected)
	}
	return batch, eff, all
}

func TestMustIncludeAnEvent(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.tags = []string{"Outdoors"}
	plain, _ := tp.generate(t, sandy(), o)
	missing := 0
	for _, opt := range tp.allOptions(t, sandy(), plain) {
		if !hasActivity(opt, jazzID) {
			missing++
		}
	}
	if missing == 0 {
		t.Fatal("every option has the jazz without asking; pick something else")
	}

	o.picks = []string{jazzID}
	batch, _, all := withPicks(t, tp, sandy(), o)
	jazz := catalogByID(tp.source.Activities)[jazzID]
	for _, opt := range all {
		s := stopOf(opt, jazzID)
		if s.Kind != "event" || s.Flexible || s.Arrive.Before(*jazz.Start) || s.Arrive.After(jazz.Start.Add(15*time.Minute)) {
			t.Errorf("%s: jazz at %v–%v (flexible %v)", opt.Name, s.Arrive.In(ny), s.Depart.In(ny), s.Flexible)
		}
	}
	run := tp.run(t, batch.RunID)
	if len(run.Spec.MustInclude) != 1 || run.Spec.MustInclude[0] != jazzID {
		t.Errorf("run spec picks %v", run.Spec.MustInclude)
	}
	logged := false
	for _, e := range run.Shortlist {
		logged = logged || (e.ID == jazzID && e.Source == pickSource)
	}
	if !logged {
		t.Error("the pick is not in the run's shortlist log as must_include")
	}
	t.Logf("%d options with the jazz, %d without it before", len(all), missing)
}

func TestMustIncludeAPlaceTakesItsCategory(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.tags, o.mood = []string{"Food"}, "dinner and a walk"
	o.picks = []string{phoID}
	_, _, all := withPicks(t, tp, sandy(), o)
	for _, opt := range all {
		for _, s := range opt.Stops {
			if s.Category == "restaurant" && s.ActivityID != phoID {
				t.Errorf("%s: %s is a second restaurant next to the pick", opt.Name, s.Title)
			}
		}
	}
}

func TestMustIncludeTwoPicks(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.picks = []string{standupID, jazzID} // any order: the plan puts them in time
	_, _, all := withPicks(t, tp, sandy(), o)
	for _, opt := range all {
		jazz, standup := stopOf(opt, jazzID), stopOf(opt, standupID)
		if jazz.Depart.After(standup.Arrive) {
			t.Errorf("%s: jazz until %v, standup from %v", opt.Name, jazz.Depart.In(ny), standup.Arrive.In(ny))
		}
	}
}

func TestMustIncludeBelowTheBarAndOutOfRange(t *testing.T) {
	acts := saltlight(t)
	lab := catalogByID(acts)[labID]
	if s := preferenceScore(&Candidate{Act: lab}); s >= DefaultConfig().Itinerary.Tau {
		t.Fatalf("premise: the laboratory scores %.2f, above the bar", s)
	}
	tp := newTestPlanner(acts, testConfig())
	o := defaultReq()
	o.from, o.backBy = localAt(12, 0), localAt(17, 0)
	o.picks = []string{labID}
	batch, eff, all := withPicks(t, tp, sandy(), o)
	labAt, _ := activityPoint(&lab)
	pool := tp.pool(t, batch.RunID)
	if d := travel.HaversineKm(seasideMkt, labAt); d <= pool.Spec.RadiusKm || d <= eff.MaxLegKm {
		t.Fatalf("premise: the laboratory is %.1f km out, radius %.1f, legs %.1f", d, pool.Spec.RadiusKm, eff.MaxLegKm)
	}
	// Only the pick's own legs may be longer than the range.
	noPicks := eff
	noPicks.MustInclude = nil
	for _, opt := range all {
		long := false
		for _, v := range guaranteeViolations(noPicks, opt, catalogByID(acts)) {
			long = long || strings.Contains(v, "km >")
		}
		if !long {
			t.Errorf("%s: no leg past the range, yet the laboratory is out of it", opt.Name)
		}
	}
	for _, e := range tp.run(t, batch.RunID).Shortlist {
		if e.ID == labID && (e.ML == nil || *e.ML >= 0.5 || e.Source != pickSource) {
			t.Errorf("the laboratory in the run: %+v", e)
		}
	}
}

func TestMustIncludeWithoutAFit(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.from, o.backBy = localAt(15, 0), localAt(17, 30) // the trail closes at 18:00, 7 km out
	o.picks = []string{fallsID}
	batch, _ := tp.generate(t, sandy(), o)
	if len(batch.Options) != 0 || !batch.Done || batch.Reason != ReasonPickNoFit {
		t.Fatalf("walking to a trail 7 km away and back in 2.5 hours: %+v", batch)
	}
	if run := tp.run(t, batch.RunID); run.Final.Reason != ReasonPickNoFit || len(run.Rounds) != 0 || len(tp.source.Calls) != 0 {
		t.Errorf("final %+v, %d rounds, %d catalog queries: nothing should run after the early answer", run.Final, len(run.Rounds), len(tp.source.Calls))
	}
	// By car it fits.
	o.rng, o.ride, o.modes = "anywhere", "drive", nil
	withPicks(t, tp, sandy(), o)
}

func TestMustIncludeUnavailable(t *testing.T) {
	teen := sandy()
	teen.AgeBracket = "18_20"
	monday := defaultReq()
	monday.from, monday.backBy = time.Date(2026, 9, 28, 12, 0, 0, 0, ny), time.Date(2026, 9, 28, 17, 0, 0, 0, ny)
	afternoon := defaultReq()
	afternoon.from, afternoon.backBy = localAt(12, 0), localAt(17, 0)
	for _, c := range []struct {
		name   string
		user   *UserContext
		o      reqOpts
		picks  []string
		reason string
	}{
		{"an event on another day", sandy(), defaultReq(), []string{brunchID}, "Harbor Dumpling Brunch Crawl"},
		{"an event already over", sandy(), afternoon, []string{cleanupID}, "Kelp Forest Kayak Cleanup"},
		{"an event the age rules out", teen, defaultReq(), []string{standupID}, "Seasick Standup at the Crow's Nest"},
		{"a place closed that day", sandy(), monday, []string{labID}, "The Lighthouse Laboratory"},
		{"a venue plans never visit", sandy(), defaultReq(), []string{cinemaID}, "Lanternfall Cinema"},
		{"an id not in the catalog", sandy(), defaultReq(), []string{"ffffffffffffffffffffffff"}, "a pick"},
		{"a malformed id", sandy(), defaultReq(), []string{"pier-nine"}, "a pick"},
		{"the first that fails, in order", sandy(), defaultReq(), []string{jazzID, brunchID, "ffffffffffffffffffffffff"}, "Harbor Dumpling Brunch Crawl"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tp := newTestPlanner(saltlight(t), testConfig())
			c.o.picks = c.picks
			batch, _ := tp.generate(t, c.user, c.o)
			want := "must_include_unavailable: " + c.reason
			if len(batch.Options) != 0 || !batch.Done || batch.Reason != want {
				t.Fatalf("batch %+v, want %q", batch, want)
			}
			if run := tp.run(t, batch.RunID); run.Final.Reason != want || len(tp.source.Calls) != 0 {
				t.Errorf("final %+v, %d catalog queries", run.Final, len(tp.source.Calls))
			}
		})
	}
}

func TestMustIncludeLimitAndDuplicates(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	svc := NewService(tp.Planner)
	req := appPlanRequest(t, defaultReq())
	for i := 0; i < 11; i++ {
		req.MustInclude = append(req.MustInclude, fmt.Sprintf("%024x", i+1))
	}
	_, err := svc.Generate(t.Context(), sandyModel(), req, ny)
	var status *httpx.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusBadRequest || status.Message != MsgTooManyPicks {
		t.Fatalf("eleven picks: %v", err)
	}
	// Repeats count once: eleven entries, one pick.
	req.MustInclude = []string{jazzID, strings.ToUpper(jazzID), " " + jazzID + " "}
	for len(req.MustInclude) < 11 {
		req.MustInclude = append(req.MustInclude, jazzID)
	}
	batch, err := svc.Generate(t.Context(), sandyModel(), req, ny)
	if err != nil || len(batch.Options) == 0 || batch.Reason != nil {
		t.Fatalf("one pick sent eleven times: %v %+v", err, batch)
	}
	for _, opt := range batch.Options {
		n := 0
		for _, s := range opt.Stops {
			if s.ActivityID != nil && *s.ActivityID == jazzID {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s visits the jazz %d times", opt.Name, n)
		}
	}
}

func TestMustIncludeKeepsTheBudgetHonest(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.budget = 1 // $, $25 in all; the Captain's Table is $70 and up
	o.picks = []string{captainsID}
	batch, _, all := withPicks(t, tp, sandy(), o)
	if w := tp.pool(t, batch.RunID).Window.BudgetCents; w != 7000 {
		t.Errorf("solver budget %d, want the pick's 7000", w)
	}
	for _, opt := range all {
		if !opt.CostKnown || opt.TotalCostCents < 7000 {
			// A stop without a known price makes the total a floor, but
			// the pick's own $70 is always in it.
			if opt.TotalCostCents < 7000 {
				t.Errorf("%s totals %d", opt.Name, opt.TotalCostCents)
			}
		}
		for _, s := range opt.Stops {
			if s.ActivityID != captainsID && s.PriceKnown && *s.PriceCents > 0 {
				t.Errorf("%s: %s costs %d on top of a pick over the budget", opt.Name, s.Title, *s.PriceCents)
			}
		}
	}
}

func TestMustIncludeOverridesTheMoodForThePickOnly(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.mood, o.picks = "no bars tonight", []string{rustyID}
	_, _, all := withPicks(t, tp, sandy(), o)
	for _, opt := range all {
		for _, s := range opt.Stops {
			if s.Category == "bar" && s.ActivityID != rustyID {
				t.Errorf("%s: %s is a bar, and only the picked one may be", opt.Name, s.Title)
			}
		}
	}
}

// TestMustIncludeAcrossModesAndBudgets checks the guarantees, picks
// included, on every page for each way of travelling and every budget.
func TestMustIncludeAcrossModesAndBudgets(t *testing.T) {
	acts := saltlight(t)
	for _, m := range []struct {
		name, rng, ride string
		modes           []string
	}{
		{"walk", "walkable", "none", []string{"walk"}},
		{"transit", "transit", "none", []string{"marta", "walk"}},
		{"drive", "anywhere", "drive", nil},
	} {
		for budget := 0; budget <= 3; budget++ {
			for _, picks := range [][]string{{jazzID}, {phoID}, {jazzID, standupID}, {labID}} {
				t.Run(fmt.Sprintf("%s/budget%d/%d picks %s", m.name, budget, len(picks), picks[0][:6]), func(t *testing.T) {
					tp := newTestPlanner(acts, testConfig())
					o := defaultReq()
					o.rng, o.ride, o.modes, o.budget, o.picks = m.rng, m.ride, m.modes, budget, picks
					o.from, o.backBy = localAt(16, 0), localAt(23, 30)
					withPicks(t, tp, sandy(), o)
				})
			}
		}
	}
}

// TestPickProblemRules covers the rule the search and the planner share.
func TestPickProblemRules(t *testing.T) {
	day, next := localAt(0, 0), localAt(0, 0).AddDate(0, 0, 1)
	now := localAt(12, 0)
	cat := catalogByID(saltlight(t))
	bar := cat[rustyID]
	noHours := bar
	noHours.WeeklyHours = nil
	addOn := cat[jazzID]
	addOn.Name = "Pier Nine Parking"
	noWhere := cat[phoID]
	noWhere.Location = models.GeoJSONPoint{}
	for _, c := range []struct {
		name string
		a    models.Activity
		age  string
		want string
	}{
		{"an evening event", cat[jazzID], "21_plus", ""},
		{"an event that ended at noon", cat[cleanupID], "21_plus", "over"},
		{"tomorrow's event", cat[brunchID], "21_plus", "not_on_this_day"},
		{"a 21+ show for a teen", cat[standupID], "13_17", "age_gate"},
		{"a bar with hours", bar, "21_plus", ""},
		{"a bar without hours", noHours, "21_plus", "no_hours"},
		{"a cinema venue", cat[cinemaID], "21_plus", "category_excluded"},
		{"an add-on listing", addOn, "21_plus", "add_on_listing"},
		{"no location", noWhere, "21_plus", "no_location"},
	} {
		if got := PickProblem(&c.a, day, next, now, ny, c.age); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
