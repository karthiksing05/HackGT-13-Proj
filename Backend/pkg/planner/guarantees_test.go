package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
	"time"
)

// dropSet picks about a fifth of the ids, deterministically, for the fake
// classifier to leave out of its answer.
func dropSet(acts []models.Activity) map[string]bool {
	out := map[string]bool{}
	for _, a := range acts {
		h := fnv.New32a()
		_, _ = h.Write([]byte(a.ID.Hex()))
		if h.Sum32()%5 == 0 {
			out[a.ID.Hex()] = true
		}
	}
	return out
}

// TestGuaranteesAcrossTheMatrix runs Generate over both fixtures × travel
// modes × budgets × age brackets × vibes and checks §7 on every option of
// every page. The classifier drops a fifth of the ids; none may come back.
func TestGuaranteesAcrossTheMatrix(t *testing.T) {
	type world struct {
		name     string
		acts     []models.Activity
		user     func(age string) *UserContext
		start    travel.Point
		from, to time.Time
	}
	worlds := []world{
		{"saltlight", saltlight(t), func(age string) *UserContext {
			u := sandy()
			u.AgeBracket = age
			u.Prefs.AvoidTags = []string{"touristy"}
			return u
		}, seasideMkt, localAt(17, 0), localAt(23, 30)},
		{"atlanta", atlanta(t), func(age string) *UserContext {
			return &UserContext{ID: "jordan", Catalog: "activities", City: "atlanta", AgeBracket: age,
				PositiveEmbedding: vectorOf("music", "art", "food", "cat:live_music", "cat:gallery"), Prefs: UserPrefs{Pace: "balanced"}}
		}, techSquare, localAt(17, 0), localAt(23, 30)},
	}
	modes := []struct {
		name, rng, ride string
		modes           []string
	}{
		{"walk", "walkable", "none", []string{"walk"}},
		{"transit", "transit", "none", []string{"marta", "walk"}},
		{"drive", "anywhere", "drive", nil},
	}
	vibes := []struct {
		name  string
		picks []string
		mood  string
	}{
		{"plain", nil, ""},
		{"outside+food", []string{"Outdoors", "Food"}, "something chill outside, no bars"},
		{"night+family", []string{"Nightlife", "Music"}, "free stuff for the family"},
	}

	runs, withOptions, totalOptions := 0, 0, 0
	for _, w := range worlds {
		catalog := catalogByID(w.acts)
		drops := dropSet(w.acts)
		for _, m := range modes {
			for budget := 0; budget <= 3; budget++ {
				for _, age := range []string{"13_17", "18_20", "21_plus"} {
					for _, v := range vibes {
						name := fmt.Sprintf("%s/%s/budget%d/%s/%s", w.name, m.name, budget, age, v.name)
						t.Run(name, func(t *testing.T) {
							tp := newTestPlanner(w.acts, testConfig())
							tp.scorer.Drop = drops
							user := w.user(age)
							o := reqOpts{start: w.start, end: w.start, from: w.from, backBy: w.to, rng: m.rng, ride: m.ride,
								modes: m.modes, budget: budget, pace: "balanced", who: "friends", tags: v.picks, mood: v.mood}
							batch, spec := tp.generate(t, user, o)
							runs++
							if batch.Planner != "dag" || batch.RunID == "" {
								t.Fatalf("batch = %+v", batch)
							}
							if len(batch.Options) == 0 {
								if batch.Reason == "" || !batch.Done {
									t.Fatalf("empty batch without a reason: %+v", batch)
								}
								return
							}
							withOptions++
							pool := tp.pool(t, batch.RunID)
							eff := effectiveSpec(t, spec, pool, batch.Relaxed)
							all := tp.allOptions(t, user, batch)
							totalOptions += len(all)
							if len(all) != len(pool.Options) {
								t.Errorf("paged %d options, pool has %d", len(all), len(pool.Options))
							}
							seen := map[string]bool{}
							for n, opt := range all {
								if opt.ID != OptionID(batch.RunID, n) || seen[opt.ID] {
									t.Errorf("option %d id %s", n, opt.ID)
								}
								seen[opt.ID] = true
								assertGuarantees(t, eff, opt, catalog)
								for _, s := range opt.Stops {
									if drops[s.ActivityID] {
										t.Errorf("option %s uses %s, which the classifier dropped", opt.ID, s.ActivityID)
									}
								}
							}
							run := tp.run(t, batch.RunID)
							if run.ML.Mode != "classifier" {
								t.Errorf("ml mode %q", run.ML.Mode)
							}
							if run.Final.Rejected != 0 {
								t.Errorf("the runtime check rejected %d options", run.Final.Rejected)
							}
							for _, e := range run.Shortlist {
								if drops[e.ID] {
									t.Errorf("dropped id %s is in the pool (source %s)", e.ID, e.Source)
								}
							}
						})
					}
				}
			}
		}
	}
	t.Logf("%d runs, %d with options, %d options checked", runs, withOptions, totalOptions)
	if withOptions*2 < runs {
		t.Errorf("only %d of %d runs produced options; the matrix is too weak", withOptions, runs)
	}
}

// TestGuaranteeChecklistCatchesViolations proves the checklist is not
// vacuous: each §7 rule, broken on purpose, is reported.
func TestGuaranteeChecklistCatchesViolations(t *testing.T) {
	acts := saltlight(t)
	tp := newTestPlanner(acts, testConfig())
	o := defaultReq()
	o.rng, o.modes = "transit", []string{"marta", "walk"}
	batch, spec := tp.generate(t, sandy(), o)
	var base Option
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		if len(opt.Stops) >= 2 {
			base = opt
			break
		}
	}
	if len(base.Stops) < 2 {
		t.Fatal("need a two-stop option")
	}
	catalog := catalogByID(acts)
	if v := guaranteeViolations(spec, base, catalog); len(v) != 0 {
		t.Fatalf("the unmodified option already fails: %v", v)
	}
	clone := func() Option {
		c := base
		c.Stops = append([]Stop(nil), base.Stops...)
		c.Legs = append([]Leg(nil), base.Legs...)
		return c
	}
	cloneCatalog := func() map[string]models.Activity {
		out := map[string]models.Activity{}
		for k, v := range catalog {
			out[k] = v
		}
		return out
	}
	s0, s1 := base.Stops[0], base.Stops[1]
	cases := []struct {
		name string
		want string
		mut  func(o *Option, sp *PlanSpec, cat map[string]models.Activity)
	}{
		{"leg mode outside the app enum", "not in the app enum", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) { o.Legs[0].Mode = "transit" }},
		{"missing return leg", "legs for", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) { o.Legs = o.Legs[:len(o.Legs)-1] }},
		{"runs past back-by", "departs", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			o.Stops[1].Depart = sp.BackBy.Add(time.Minute)
		}},
		{"starts before the window", "arrives", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			o.Stops[0].Arrive = sp.From.Add(-time.Minute)
		}},
		{"repeated category", "category", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			a := cat[s1.ActivityID]
			a.Category = cat[s0.ActivityID].Category
			cat[s1.ActivityID] = a
		}},
		{"repeated series", "series", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) { o.Stops[1].SeriesKey = s0.SeriesKey }},
		{"age gate", "age-gated", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.AgeBracket = "18_20"
			a := cat[s0.ActivityID]
			a.Tags = append(append([]string(nil), a.Tags...), "21_plus")
			cat[s0.ActivityID] = a
		}},
		{"paid stop on a free plan", "free-only", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.Budget = BudgetForLevel(0)
			cents := int64(500)
			o.Stops[0].PriceCents, o.Stops[0].PriceKnown = &cents, true
		}},
		{"over the total", "exceed", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.Budget = Budget{Tier: 3, TotalCents: 100}
			cents := int64(500)
			o.Stops[0].PriceCents, o.Stops[0].PriceKnown = &cents, true
		}},
		{"leg too long", "km >", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			p := offsetKm(travel.Point{Lat: s0.Place.Lat, Lng: s0.Place.Lng}, 40, 0)
			o.Stops[1].Place.Lat, o.Stops[1].Place.Lng = p.Lat, p.Lng
		}},
		{"excluded category", "exclusion", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.Hard.ExcludeCategories = []string{cat[s0.ActivityID].Category}
		}},
		{"outside the catalog", "not in the user's catalog", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			delete(cat, s1.ActivityID)
		}},
		{"a must-see pick left out", "is not in the option", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.MustInclude = []string{s0.ActivityID, jazzID}
		}},
		{"a stop sharing a pick's category", "category", func(o *Option, sp *PlanSpec, cat map[string]models.Activity) {
			sp.MustInclude = []string{s0.ActivityID}
			a := cat[s1.ActivityID]
			a.Category = cat[s0.ActivityID].Category
			cat[s1.ActivityID] = a
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, sp, cat := clone(), spec, cloneCatalog()
			c.mut(&o, &sp, cat)
			found := false
			for _, v := range guaranteeViolations(sp, o, cat) {
				if strings.Contains(v, c.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("violation %q not reported: %v", c.want, guaranteeViolations(sp, o, cat))
			}
		})
	}
}

// TestGuaranteeChecklistExemptsPicks: what a must-see pick bypasses (its
// own legs' range, the per-stop price rules, the exclusions, sharing a
// category with another pick, a total over the budget it alone causes) is
// not reported for it, and still is for the other stops.
func TestGuaranteeChecklistExemptsPicks(t *testing.T) {
	acts := saltlight(t)
	tp := newTestPlanner(acts, testConfig())
	o := defaultReq()
	o.rng, o.modes = "transit", []string{"marta", "walk"}
	batch, spec := tp.generate(t, sandy(), o)
	var base Option
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		if len(opt.Stops) >= 2 {
			base = opt
			break
		}
	}
	if len(base.Stops) < 2 {
		t.Fatal("need a two-stop option")
	}
	catalog := catalogByID(acts)
	s0, s1 := base.Stops[0], base.Stops[1]
	o0 := base
	o0.Stops = append([]Stop(nil), base.Stops...)
	sp := spec
	sp.MustInclude = []string{s0.ActivityID, s1.ActivityID}
	sp.Budget = Budget{Tier: 0, FreeOnly: true}
	sp.Hard.ExcludeCategories = []string{catalog[s0.ActivityID].Category}
	a1 := catalog[s1.ActivityID]
	a1.Category = catalog[s0.ActivityID].Category
	catalog[s1.ActivityID] = a1
	far := offsetKm(travel.Point{Lat: s0.Place.Lat, Lng: s0.Place.Lng}, 40, 0)
	o0.Stops[1].Place.Lat, o0.Stops[1].Place.Lng = far.Lat, far.Lng
	cents := int64(9000)
	o0.Stops[0].PriceCents, o0.Stops[0].PriceKnown, o0.Stops[0].TierKnown, o0.Stops[0].Tier = &cents, true, true, 3
	if v := guaranteeViolations(sp, o0, catalog); len(v) != 0 {
		t.Errorf("picks bending only what they may: %v", v)
	}
	// The same option without the picks breaks every one of those rules.
	sp.MustInclude = nil
	v := strings.Join(guaranteeViolations(sp, o0, catalog), "; ")
	for _, want := range []string{"free-only", "tier", "exclusion", "category", "km >"} {
		if !strings.Contains(v, want) {
			t.Errorf("without picks %q is not reported: %s", want, v)
		}
	}
}
