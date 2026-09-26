package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

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
		switch strings.Trim(w, ",.!") {
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

var sandyPositive = vectorOf("outdoor", "nature", "music", "food", "low_energy", "cat:park", "cat:live_music", "cat:market")

// sandy is the demo user: Saltlight catalog, likes the outdoors, walks and
// live music, dislikes crowds and clubs.
func sandy() *UserContext {
	return &UserContext{
		ID: "sandy", Catalog: "demo_activities", City: "saltlight", AgeBracket: "21_plus",
		HomeBase:          &Place{Name: "Seaside Market Square", Lat: seasideMkt.Lat, Lng: seasideMkt.Lng, HasCoord: true},
		Prefs:             UserPrefs{Pace: "balanced", Flexible: true, PreferFree: true},
		PositiveEmbedding: sandyPositive,
		NegativeEmbedding: vectorOf("cat:nightclub", "high_energy", "late_night"),
	}
}

// reqOpts builds an app-shape body.
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

// defaultReq is the Saltlight Saturday evening, walking, $$.
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
	if o.tags == nil {
		body["tags"] = []string{}
	}
	if o.modes == nil {
		body["modes"] = []string{}
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

// preferenceScore is the fake classifier: closeness to what Sandy likes,
// mapped onto 0.5..1.
func preferenceScore(c *Candidate) float64 {
	v := c.Act.Embedding
	if len(v) == 0 {
		v = vectorFor(&c.Act)
	}
	return clamp01(0.5 + 0.5*dot(sandyPositive, v))
}

func newTestPlanner(acts []models.Activity, cfg Config) *testPlanner {
	clock := NewFakeClock(testNow)
	src := &FakeSource{Activities: acts, VectorFor: vectorFor}
	scorer := &FakeScorer{Fn: preferenceScore}
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
		if len(b.Options) == 0 {
			t.Fatalf("cursor %s gave an empty page", cursor)
		}
		out = append(out, b.Options...)
		if b.Done != (b.Cursor == "") {
			t.Fatalf("done=%v with cursor %q", b.Done, b.Cursor)
		}
		cursor = b.Cursor
	}
	return out
}

func (tp *testPlanner) run(t *testing.T, id string) *PlanRun {
	t.Helper()
	r, ok := tp.pools.GetRun(id)
	if !ok {
		t.Fatalf("run %s not saved", id)
	}
	return r
}

func (tp *testPlanner) pool(t *testing.T, id string) *PlanPool {
	t.Helper()
	p, err := tp.pools.GetPool(t.Context(), id)
	if err != nil {
		t.Fatalf("pool %s: %v", id, err)
	}
	return p
}

var appLegModes = map[string]bool{"walk": true, "marta": true, "drive": true, "rideshare": true}

// effectiveSpec is the spec the options must satisfy: the request's, with
// the range and budget relaxations the run applied (and reported).
func effectiveSpec(t *testing.T, spec PlanSpec, pool *PlanPool, relaxed []string) PlanSpec {
	t.Helper()
	eff := spec
	if pool.Window.MaxLegKm != spec.MaxLegKm {
		if !containsString(relaxed, "range") || pool.Window.MaxLegKm < spec.MaxLegKm {
			t.Errorf("leg range changed %.2f → %.2f without a range relaxation (%v)", spec.MaxLegKm, pool.Window.MaxLegKm, relaxed)
		}
		eff.MaxLegKm = pool.Window.MaxLegKm
	}
	if pool.Spec.Budget != spec.Budget {
		if !containsString(relaxed, "budget") || spec.Budget.FreeOnly {
			t.Errorf("budget changed %+v → %+v without a budget relaxation (%v)", spec.Budget, pool.Spec.Budget, relaxed)
		}
		eff.Budget = pool.Spec.Budget
	}
	return eff
}

// assertGuarantees reports every §7 violation of one option.
func assertGuarantees(t *testing.T, spec PlanSpec, opt Option, catalog map[string]models.Activity) {
	t.Helper()
	for _, v := range guaranteeViolations(spec, opt, catalog) {
		t.Errorf("option %s (%s): %s", opt.ID, opt.Name, v)
	}
}

// guaranteeViolations is the §7 checklist for one option.
func guaranteeViolations(spec PlanSpec, opt Option, catalog map[string]models.Activity) []string {
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}
	if len(opt.Stops) == 0 {
		fail("no stops")
		return out
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
		if s.ID != StopID(s.ActivityID, i) {
			fail("stop %d id %s", i, s.ID)
		}
		if s.Arrive.Before(spec.From) {
			fail("stop %d arrives %v before %v", i, s.Arrive, spec.From)
		}
		if s.Depart.After(spec.BackBy) {
			fail("stop %d departs %v after %v", i, s.Depart, spec.BackBy)
		}
		dropIn := a.Attendance != nil && *a.Attendance == "drop_in"
		if a.Kind == "event" && !dropIn && a.Start != nil && !(a.End != nil && a.End.Sub(*a.Start) > 6*time.Hour) {
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
		} else {
			if s.PriceCents != nil {
				fail("stop %d has price_cents without a known price", i)
			}
			if spec.Budget.FreeOnly && (a.Price != nil || !freeIfUnknownCategories[a.Category]) {
				fail("stop %d has an unknown price on a free-only plan", i)
			}
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
			if s.Arrive.Before(prev.Depart) {
				fail("stop %d starts before stop %d ends", i, i-1)
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
	if opt.Depart.Before(spec.From) || opt.Arrival.After(spec.BackBy) {
		fail("leaves %v, back %v, window %v–%v", opt.Depart, opt.Arrival, spec.From, spec.BackBy)
	}
	return out
}

func catalogByID(acts []models.Activity) map[string]models.Activity {
	out := map[string]models.Activity{}
	for _, a := range acts {
		out[a.ID.Hex()] = a
	}
	return out
}

func approx(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// --- synthetic activities ---------------------------------------------------

var synthCounter uint32 = 0x10000

func synthID() bson.ObjectID {
	synthCounter++
	var id bson.ObjectID
	id[0] = 0xAA
	id[8] = byte(synthCounter >> 24)
	id[9] = byte(synthCounter >> 16)
	id[10] = byte(synthCounter >> 8)
	id[11] = byte(synthCounter)
	return id
}

// offsetKm is a point dx km east and dy km north of p.
func offsetKm(p travel.Point, dxKm, dyKm float64) travel.Point {
	return travel.Point{Lat: p.Lat + dyKm/111.0, Lng: p.Lng + dxKm/(111.0*math.Cos(p.Lat*math.Pi/180))}
}

func strp(s string) *string   { return &s }
func f64p(v float64) *float64 { return &v }
func timep(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func priceOf(min float64) *models.ActivityPrice {
	return &models.ActivityPrice{Min: f64p(min), Max: f64p(min), Currency: "USD", Tier: tierForAmount(min), IsFree: min == 0}
}

func synthEvent(name, category string, at travel.Point, start time.Time, minutes int, tags []string, price *models.ActivityPrice) models.Activity {
	return models.Activity{
		ID: synthID(), Kind: "event", City: "saltlight", Name: name, Category: category, Tags: tags,
		Location:  models.GeoJSONPoint{Type: "Point", Coordinates: []float64{at.Lng, at.Lat}},
		VenueName: strp(name + " Hall"), Start: timep(start), Attendance: strp("fixed_start"), Timezone: "America/New_York",
		Duration: &models.ActivityDuration{MedianMin: float64(minutes), P75Min: float64(minutes)}, Price: price,
	}
}

func synthPlace(name, category string, at travel.Point, hours []models.WeeklyHourRange, tags []string, price *models.ActivityPrice) models.Activity {
	return models.Activity{
		ID: synthID(), Kind: "place", City: "saltlight", Name: name, Category: category, Tags: tags,
		Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{at.Lng, at.Lat}},
		Timezone: "America/New_York", WeeklyHours: hours,
		Duration: &models.ActivityDuration{MedianMin: 60, P75Min: 75}, Price: price,
		Rating: f64p(4.5), Popularity: f64p(0.8),
	}
}

// dailyHours is open→close every day, as minutes of the week.
func dailyHours(openHour, closeHour int) []models.WeeklyHourRange {
	var out []models.WeeklyHourRange
	for d := 0; d < 7; d++ {
		out = append(out, models.WeeklyHourRange{Open: d*1440 + openHour*60, Close: d*1440 + closeHour*60})
	}
	return out
}

func localAt(hour, min int) time.Time { return time.Date(2026, 9, 26, hour, min, 0, 0, ny) }

// --- golden files -------------------------------------------------------------

// golden compares v's indented JSON with testdata/golden/<name>; -update
// rewrites the file.
func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run with -update): %v", name, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the golden file; run with -update and review.\n--- got ---\n%s", name, got)
	}
}
