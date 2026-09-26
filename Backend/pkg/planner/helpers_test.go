package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test time: Saturday 26 Sep 2026, noon in Saltlight (America/New_York).
var (
	ny, _      = time.LoadLocation("America/New_York")
	testNow    = time.Date(2026, 9, 26, 12, 0, 0, 0, ny)
	seasideMkt = travel.Point{Lat: 31.3680, Lng: -81.4250}
	techSquare = travel.Point{Lat: 33.7766, Lng: -84.3890}
)

const testDim = 32

var (
	fixtureMu    sync.Mutex
	fixtureCache = map[string][]models.Activity{}
)

// loadFixture reads a plain-JSON activity list; the Saltlight one lives here,
// the Atlanta one is the optimizer's.
func loadFixture(t *testing.T, path string) []models.Activity {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if acts, ok := fixtureCache[path]; ok {
		return append([]models.Activity(nil), acts...)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var acts []models.Activity
	if err := json.Unmarshal(b, &acts); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	fixtureCache[path] = acts
	return append([]models.Activity(nil), acts...)
}

func saltlight(t *testing.T) []models.Activity {
	return loadFixture(t, "testdata/saltlight_2026-09-26.json")
}

func atlanta(t *testing.T) []models.Activity {
	acts := loadFixture(t, "../itinerary/testdata/atlanta_2026-09-26.json")
	for i := range acts {
		acts[i].Score, acts[i].RerankScore = nil, nil // the planner scores, not the fixture
	}
	return acts
}

// tokenVector is a deterministic pseudo-random unit vector per token, so
// documents sharing categories or tags are close in cosine space.
func tokenVector(token string) []float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(token)))
	r := rand.New(rand.NewSource(int64(h.Sum64())))
	v := make([]float64, testDim)
	for i := range v {
		v[i] = r.NormFloat64()
	}
	return l2norm(v)
}

// vectorFor is the fake embedding of a document: its kind, category and tags.
func vectorFor(a *models.Activity) []float64 {
	tokens := append([]string{"kind:" + a.Kind, "cat:" + a.Category}, a.Tags...)
	return vectorOf(tokens...)
}

func vectorOf(tokens ...string) []float64 {
	sum := make([]float64, testDim)
	for _, tok := range tokens {
		for i, x := range tokenVector(tok) {
			sum[i] += x
		}
	}
	return l2norm(sum)
}

// searchVectorFor mirrors the ML search profile: quick picks map to their
// facet vocabulary, plus a few mood words.
func searchVectorFor(in SearchInput) []float64 {
	var tokens []string
	for _, tag := range in.Tags {
		if f, ok := FacetByName(tag); ok {
			tokens = append(tokens, f.Tags...)
			for _, c := range f.Cats {
				tokens = append(tokens, "cat:"+c)
			}
		}
	}
	for _, w := range strings.Fields(strings.ToLower(in.MoodText)) {
		switch w {
		case "outside", "outdoors", "nature":
			tokens = append(tokens, "outdoor", "nature")
		case "food", "eat", "dinner":
			tokens = append(tokens, "food", "cat:restaurant")
		case "music", "concert":
			tokens = append(tokens, "music", "cat:live_music")
		case "chill", "relax":
			tokens = append(tokens, "low_energy")
		}
	}
	if len(tokens) == 0 {
		return nil
	}
	return vectorOf(tokens...)
}

// sandy is the demo user: Saltlight catalog, likes the outdoors, walks and
// live music, dislikes crowds and clubs.
func sandy() *UserContext {
	return &UserContext{
		ID: "sandy", Catalog: "demo_activities", City: "saltlight", AgeBracket: "21_plus",
		HomeBase:          &Place{Name: "Seaside Market Square", Lat: seasideMkt.Lat, Lng: seasideMkt.Lng, HasCoord: true},
		Prefs:             UserPrefs{Pace: "balanced", Flexible: true, PreferFree: true},
		PositiveEmbedding: vectorOf("outdoor", "nature", "music", "food", "low_energy", "cat:park", "cat:live_music", "cat:market"),
		NegativeEmbedding: vectorOf("cat:nightclub", "high_energy", "late_night"),
	}
}

// appRequestJSON builds an app-shape body for the Saltlight Saturday evening.
type reqOpts struct {
	start, end   travel.Point
	from, backBy time.Time
	rng, ride    string
	budget       int
	mood         string
	tags         []string
	pace, who    string
	modes        []string
}

func defaultReq() reqOpts {
	return reqOpts{
		start: seasideMkt, end: seasideMkt,
		from: time.Date(2026, 9, 26, 18, 0, 0, 0, ny), backBy: time.Date(2026, 9, 26, 23, 0, 0, 0, ny),
		rng: "walkable", ride: "none", budget: 2, pace: "balanced", who: "friends", modes: []string{"walk"},
	}
}

func appRequestJSON(o reqOpts) []byte {
	body := map[string]any{
		"start":      map[string]any{"name": "Start", "coordinate": map[string]float64{"lat": o.start.Lat, "lng": o.start.Lng}},
		"end":        map[string]any{"name": "End", "coordinate": map[string]float64{"lat": o.end.Lat, "lng": o.end.Lng}},
		"date":       o.from.UTC().Format(time.RFC3339),
		"start_time": o.from.UTC().Format(time.RFC3339),
		"back_by":    o.backBy.UTC().Format(time.RFC3339),
		"range":      o.rng,
		"ride":       o.ride,
		"mood_text":  o.mood,
		"tags":       o.tags,
		"budget":     o.budget,
		"who":        o.who,
		"pace":       o.pace,
		"modes":      o.modes,
	}
	if body["tags"] == nil {
		body["tags"] = []string{}
	}
	b, _ := json.Marshal(body)
	return b
}

// testPlanner wires fakes around a fixture with a fixed clock and ids.
type testPlanner struct {
	*Planner
	source *FakeSource
	scorer *FakeScorer
	search *FakeVectorizer
	pools  *MemPoolStore
	clock  *FakeClock
	jobs   []func()
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.Jev = "off"
	return cfg
}

func newTestPlanner(acts []models.Activity, cfg Config) *testPlanner {
	clock := NewFakeClock(testNow)
	src := &FakeSource{Activities: acts, VectorFor: vectorFor}
	scorer := &FakeScorer{Fn: func(c *Candidate) float64 {
		// Prefer what Sandy likes, deterministically.
		return clamp01(0.5 + 0.5*dot(sandy().PositiveEmbedding, vectorFor(&c.Act)))
	}}
	search := &FakeVectorizer{Fn: searchVectorFor}
	pools := NewMemPoolStore(clock)
	counter := 0
	tp := &testPlanner{source: src, scorer: scorer, search: search, pools: pools, clock: clock}
	tp.Planner = New(cfg, Deps{
		Source: src, Embeddings: src, Lookup: src, Scorer: scorer, Search: search, Pools: pools,
		Travel: travel.Heuristic{}, Clock: clock,
		NewID: func() string { counter++; return fmt.Sprintf("id-%04d", counter) },
	})
	tp.Planner.Background = func(f func()) { tp.jobs = append(tp.jobs, f) }
	return tp
}

func (tp *testPlanner) spec(t *testing.T, user *UserContext, o reqOpts) PlanSpec {
	t.Helper()
	spec, err := ParsePlanRequest(appRequestJSON(o), "America/New_York", tp.clock.Now(), user, tp.Cfg)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	return spec
}

func (tp *testPlanner) generate(t *testing.T, user *UserContext, o reqOpts) (Batch, PlanSpec) {
	t.Helper()
	spec := tp.spec(t, user, o)
	batch, err := tp.Generate(t.Context(), user, spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return batch, spec
}

// allOptions pages through a run until done.
func (tp *testPlanner) allOptions(t *testing.T, user *UserContext, first Batch) []Option {
	t.Helper()
	out := append([]Option(nil), first.Options...)
	cursor := first.Cursor
	for cursor != "" {
		b, err := tp.More(t.Context(), user, cursor)
		if err != nil {
			t.Fatalf("more: %v", err)
		}
		out = append(out, b.Options...)
		cursor = b.Cursor
		if b.Done && cursor != "" {
			t.Fatalf("done with a cursor: %+v", b)
		}
	}
	return out
}

var appLegModes = map[string]bool{"walk": true, "marta": true, "drive": true, "rideshare": true}

// assertGuarantees is the §7 checklist for one option.
func assertGuarantees(t *testing.T, spec PlanSpec, user *UserContext, opt Option, catalog map[string]models.Activity) {
	t.Helper()
	fail := func(format string, args ...any) {
		t.Helper()
		t.Errorf("option %s (%s): %s", opt.ID, opt.Name, fmt.Sprintf(format, args...))
	}
	if len(opt.Stops) == 0 {
		fail("no stops")
		return
	}
	if len(opt.Legs) != len(opt.Stops)+1 {
		fail("%d legs for %d stops", len(opt.Legs), len(opt.Stops))
	} else {
		if opt.Legs[0].FromStopID != "start" || opt.Legs[len(opt.Legs)-1].ToStopID != "end" {
			fail("legs must run start → … → end: %+v", opt.Legs)
		}
		for i, l := range opt.Legs {
			if !appLegModes[l.Mode] {
				fail("leg %d mode %q not in the app enum", i, l.Mode)
			}
			if i > 0 && l.FromStopID != opt.Stops[i-1].ID {
				fail("leg %d from %s, want %s", i, l.FromStopID, opt.Stops[i-1].ID)
			}
			if i < len(opt.Stops) && l.ToStopID != opt.Stops[i].ID {
				fail("leg %d to %s, want %s", i, l.ToStopID, opt.Stops[i].ID)
			}
		}
	}
	age := ageRules(spec.AgeBracket)
	series := map[string]bool{}
	cats := map[string]bool{}
	var known int64
	for i, s := range opt.Stops {
		a, ok := catalog[s.ActivityID]
		if !ok {
			fail("stop %d activity %s is not in the user's catalog", i, s.ActivityID)
			continue
		}
		if s.Arrive.Before(spec.From) {
			fail("stop %d arrives %v before %v", i, s.Arrive, spec.From)
		}
		if s.Depart.After(spec.BackBy) {
			fail("stop %d departs %v after %v", i, s.Depart, spec.BackBy)
		}
		if a.Kind == "event" && a.Attendance != nil && *a.Attendance != "drop_in" {
			p75, _ := visitLengths(&a)
			if a.Start.Add(maxDuration(s.Depart.Sub(s.Arrive), p75)).After(spec.BackBy) && !opt.LateFlag {
				fail("stop %d could run past back-by without late_flag", i)
			}
		}
		if s.PriceKnown {
			known += *s.PriceCents
			if spec.Budget.FreeOnly && *s.PriceCents != 0 {
				fail("stop %d costs %d on a free-only plan", i, *s.PriceCents)
			}
		} else if spec.Budget.FreeOnly && !freeIfUnknownCategories[a.Category] {
			fail("stop %d has an unknown price on a free-only plan", i)
		}
		if spec.Budget.Tier < 3 && s.TierKnown && s.Tier > spec.Budget.Tier {
			fail("stop %d tier %d over budget tier %d", i, s.Tier, spec.Budget.Tier)
		}
		if age.blocks(&a) {
			fail("stop %d %q is age-gated for %s", i, a.Name, spec.AgeBracket)
		}
		if spec.Hard.excludesCategory(a.Category) || spec.Hard.excludesAnyTag(a.Tags) || tagsIntersect(a.Tags, spec.AvoidTags) {
			fail("stop %d %q hits an exclusion", i, a.Name)
		}
		if series[s.SeriesKey] {
			fail("series %s repeated", s.SeriesKey)
		}
		series[s.SeriesKey] = true
		c := strings.ToLower(a.Category)
		if c != "" && c != "other" {
			if cats[c] {
				fail("category %s repeated", c)
			}
			cats[c] = true
		}
		if i > 0 {
			prev := opt.Stops[i-1]
			if d := travel.HaversineKm(travel.Point{Lat: prev.Place.Lat, Lng: prev.Place.Lng}, travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}); d > spec.MaxLegKm+1e-9 {
				fail("leg into stop %d is %.2f km > %.2f", i, d, spec.MaxLegKm)
			}
		}
	}
	if spec.Budget.TotalCents > 0 && known > spec.Budget.TotalCents {
		fail("known costs %d exceed %d", known, spec.Budget.TotalCents)
	}
	first, last := opt.Stops[0], opt.Stops[len(opt.Stops)-1]
	if d := travel.HaversineKm(*spec.Start, travel.Point{Lat: first.Place.Lat, Lng: first.Place.Lng}); d > 2*spec.MaxLegKm+1e-9 {
		fail("first leg %.2f km > 2×%.2f", d, spec.MaxLegKm)
	}
	if d := travel.HaversineKm(travel.Point{Lat: last.Place.Lat, Lng: last.Place.Lng}, *spec.End); d > 2*spec.MaxLegKm+1e-9 {
		fail("last leg %.2f km > 2×%.2f", d, spec.MaxLegKm)
	}
}

func catalogByID(acts []models.Activity) map[string]models.Activity {
	out := map[string]models.Activity{}
	for _, a := range acts {
		out[a.ID.Hex()] = a
	}
	return out
}

func approx(a, b, eps float64) bool { return math.Abs(a-b) <= eps }
