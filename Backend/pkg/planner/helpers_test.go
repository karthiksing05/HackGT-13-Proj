package planner

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"encoding/json"
	"flag"
	"fmt"
	"math"
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

// Vectors live in the catalog's embedding space. The Saltlight fixture
// carries each document's stored 1024-d Qwen3-Embedding-0.6B vector
// (testdata/make_fixture.py reads them from MongoDB), and everything else a
// test needs a vector for is built from those: a token ("kind:event",
// "cat:park", "outdoor") stands for the unit mean of the vectors of the
// fixture documents it describes, so Sandy's taste, a request's search
// vector and a synthetic activity sit in the same space as the catalog.
const testDim = ml.Dim

const saltlightFixture = "testdata/saltlight_2026-09-26.json"

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
	return loadFixture(t, saltlightFixture)
}

func atlanta(t *testing.T) []models.Activity {
	acts := loadFixture(t, "../itinerary/testdata/atlanta_2026-09-26.json")
	for i := range acts {
		acts[i].Score, acts[i].RerankScore = nil, nil // the planner scores, not the fixture
	}
	return acts
}

var (
	tokenOnce      sync.Once
	tokenCentroids map[string][]float64
	catalogMean    []float64
)

// tokenVector is the unit mean of the fixture vectors of the documents a
// token describes; a token no document has is the catalog's mean direction.
func tokenVector(token string) []float64 {
	tokenOnce.Do(loadTokenCentroids)
	if v, ok := tokenCentroids[strings.ToLower(token)]; ok {
		return v
	}
	return catalogMean
}

func loadTokenCentroids() {
	b, err := os.ReadFile(saltlightFixture)
	if err != nil {
		panic(fmt.Sprintf("the test vectors come from the Saltlight fixture: %v", err))
	}
	var acts []models.Activity
	if err := json.Unmarshal(b, &acts); err != nil {
		panic(fmt.Sprintf("decode %s: %v", saltlightFixture, err))
	}
	sums := map[string][]float64{}
	add := func(key string, v []float64) {
		sum, ok := sums[key]
		if !ok {
			sum = make([]float64, testDim)
			sums[key] = sum
		}
		for i, x := range v {
			sum[i] += x
		}
	}
	for _, a := range acts {
		if len(a.Embedding) != testDim {
			panic(fmt.Sprintf("%s has a %d-d vector; regenerate the fixture", a.Name, len(a.Embedding)))
		}
		add("", a.Embedding)
		add("kind:"+a.Kind, a.Embedding)
		add("cat:"+a.Category, a.Embedding)
		for _, tag := range a.Tags {
			add(strings.ToLower(tag), a.Embedding)
		}
	}
	tokenCentroids = map[string][]float64{}
	for k, sum := range sums {
		tokenCentroids[k] = l2norm(sum)
	}
	catalogMean = tokenCentroids[""]
}

// vectorFor is a document's vector: the stored one (every fixture document
// has it), else the mix of its kind, category and tags.
func vectorFor(a *models.Activity) []float64 {
	if len(a.Embedding) == testDim {
		return a.Embedding
	}
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

// preferenceScore is the fake classifier: closeness to Sandy's taste, on
// the real classifier's scale. Cosines in the catalog's space are
// compressed (0.58–0.83 against her taste vector across the fixture), so
// they are centred on the fixture's median and stretched through a
// logistic: 0.04–0.83 across the fixture, parks, trails and live music at
// the top, museums and tech workshops at the bottom, much as the real
// scores rank them (scenarios_test.go).
func preferenceScore(c *Candidate) float64 {
	v := c.Act.Embedding
	if len(v) == 0 {
		v = vectorFor(&c.Act)
	}
	return 1 / (1 + math.Exp(-20*(dot(sandyPositive, v)-0.745)))
}

func newTestPlanner(acts []models.Activity, cfg Config) *testPlanner {
	clock := NewFakeClock(testNow)
	src := &FakeSource{Activities: acts, VectorFor: vectorFor}
	scorer := &FakeScorer{Fn: preferenceScore}
	search := &FakeVectorizer{Fn: searchVectorFor}
	pools := NewMemPoolStore(clock)
	counter := 0
	tp := &testPlanner{source: src, scorer: scorer, search: search, pools: pools, clock: clock}
	pl, err := New(cfg, Deps{
		Source: src, Embeddings: src, Lookup: src, Scorer: scorer, Search: search, Pools: pools,
		Travel: travel.Heuristic{}, Clock: clock,
		NewID: func() string { counter++; return fmt.Sprintf("id-%04d", counter) },
	})
	if err != nil {
		panic(err) // the fakes above are all set
	}
	tp.Planner = pl
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

// guaranteeViolations is CheckOption over a catalog map.
func guaranteeViolations(spec PlanSpec, opt Option, catalog map[string]models.Activity) []string {
	return CheckOption(&spec, &opt, func(id string) (*models.Activity, bool) {
		a, ok := catalog[id]
		return &a, ok
	})
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
