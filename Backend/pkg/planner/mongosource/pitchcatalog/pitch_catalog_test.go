//go:build pitchcatalog

// Package pitchcatalog_test checks the Atlanta pitch catalog (dataingestion/pitch, the collection
// freetime.pitch_activities) against the real planner: the Mongo pre-filters and their Go re-check,
// full plan generation through the DAG solver, and every option's guarantees.
//
// The export is loaded into a throwaway database on PITCH_TEST_URI under the collection name
// "activities", because the planner's catalog allow-list (planner.NormalizeCatalog) only accepts
// activities and demo_activities. The test refuses any server that holds a "freetime" database, so
// it cannot run against a deployment, and drops its database at the end. The catalog has no vectors
// yet, so scoring runs the planner's documented fallback (the ML service rejects events without
// embeddings; the scorer here returns that error).
//
//	PITCH_TEST_URI=mongodb://127.0.0.1:27019 go test -tags pitchcatalog -count=1 -v ./pkg/planner/mongosource/pitchcatalog/
package pitchcatalog_test

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/travel"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	catalog       = "activities"      // the allow-listed name the pitch documents are loaded under
	vectorCatalog = "demo_activities" // the same documents plus a fixture vector, for the "classifier" runs
)

var (
	ny, _      = time.LoadLocation("America/New_York")
	techSquare = travel.Point{Lat: 33.7766, Lng: -84.3890} // Georgia Tech, the pitch's start
	piedmont   = travel.Point{Lat: 33.7851, Lng: -84.3738}
	decatur    = travel.Point{Lat: 33.7748, Lng: -84.2963}

	once    sync.Once
	onceErr error
	client  *mongo.Client
	testDB  *mongo.Database
	acts    []models.Activity
	byID    = map[string]*models.Activity{}
)

func TestMain(m *testing.M) {
	code := m.Run()
	if testDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := testDB.Drop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "drop %s: %v\n", testDB.Name(), err)
			code = 1
		}
		cancel()
	}
	os.Exit(code)
}

func exportPath() string {
	if p := os.Getenv("PITCH_EXPORT"); p != "" {
		return p
	}
	return filepath.Join("..", "..", "..", "..", "..", "dataingestion", "pitch", "out", "pitch_activities.json")
}

func load() error {
	uri := os.Getenv("PITCH_TEST_URI")
	if uri == "" {
		return errors.New("PITCH_TEST_URI is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(3 * time.Second))
	if err != nil {
		return err
	}
	names, err := c.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == "freetime" {
			return fmt.Errorf("refusing: %s holds a freetime database; point PITCH_TEST_URI at a throwaway mongod", uri)
		}
	}
	raw, err := os.ReadFile(exportPath())
	if err != nil {
		return err
	}
	var wrapped struct {
		Docs []bson.Raw `bson:"docs"`
	}
	if err := bson.UnmarshalExtJSON(append(append([]byte(`{"docs":`), raw...), '}'), true, &wrapped); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	insert := make([]any, len(wrapped.Docs))
	acts = make([]models.Activity, len(wrapped.Docs))
	for i, d := range wrapped.Docs {
		insert[i] = d
		if err := bson.Unmarshal(d, &acts[i]); err != nil {
			return fmt.Errorf("document %d does not decode into models.Activity: %w", i, err)
		}
	}
	for i := range acts {
		byID[acts[i].ID.Hex()] = &acts[i]
	}
	client = c
	testDB = c.Database(fmt.Sprintf("sq_pitch_%d", time.Now().UnixNano()))
	coll := testDB.Collection(catalog)
	if _, err := coll.InsertMany(ctx, insert); err != nil {
		return err
	}
	// The "classifier" runs need candidates with vectors (the planner skips the classifier
	// otherwise), so a second copy goes under the other allow-listed name with one fixed unit
	// vector on every document: a harness fixture, identical for all, never part of the dataset.
	fixture := make([]float64, 1024)
	fixture[0] = 1
	withVec := make([]any, len(wrapped.Docs))
	for i, d := range wrapped.Docs {
		var doc bson.D
		if err := bson.Unmarshal(d, &doc); err != nil {
			return err
		}
		withVec[i] = append(doc, bson.E{Key: "embedding", Value: fixture})
	}
	if _, err := testDB.Collection(vectorCatalog).InsertMany(ctx, withVec); err != nil {
		return err
	}
	for _, name := range []string{catalog, vectorCatalog} {
		if _, err := testDB.Collection(name).Indexes().CreateMany(ctx, []mongo.IndexModel{
			{Keys: bson.D{{Key: "location", Value: "2dsphere"}}},
			{Keys: bson.D{{Key: "city", Value: 1}, {Key: "kind", Value: 1}, {Key: "start", Value: 1}}},
			{Keys: bson.D{{Key: "sourceKeys", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "name", Value: 1}}},
		}); err != nil {
			return err
		}
	}
	return nil
}

func setup(t *testing.T) *mongo.Database {
	t.Helper()
	once.Do(func() { onceErr = load() })
	if onceErr != nil {
		if strings.Contains(onceErr.Error(), "not set") {
			t.Skip(onceErr)
		}
		t.Fatal(onceErr)
	}
	return testDB
}

func day(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, ny) }
func oct(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, ny) }

func isFree(a *models.Activity) bool {
	return a.Price != nil && (a.Price.IsFree || (a.Price.Min != nil && *a.Price.Min == 0))
}

// The documents decode into the planner's model, and the planner's own price reading gives
// the 150 paid / 100 free split; the free-only query keeps exactly the free ones.
func TestPitchDecodesAndPrices(t *testing.T) {
	setup(t)
	if len(acts) != 250 {
		t.Fatalf("%d documents, want 250", len(acts))
	}
	paid, free, kinds := 0, 0, map[string]int{}
	for i := range acts {
		a := &acts[i]
		kinds[a.Kind]++
		switch {
		case itinerary.CostCents(a) > 0:
			paid++
		case isFree(a):
			free++
		default:
			t.Errorf("%s: price neither paid nor free: %+v", a.Name, a.Price)
		}
		if a.City != "atlanta" || a.Timezone != "America/New_York" || len(a.Location.Coordinates) != 2 {
			t.Errorf("%s: city %q tz %q", a.Name, a.City, a.Timezone)
		}
		if len(a.Embedding) != 0 {
			t.Errorf("%s: carries a vector; vectors are deferred", a.Name)
		}
	}
	t.Logf("kinds %v; paid %d, free %d (itinerary.CostCents)", kinds, paid, free)
	if paid != 150 || free != 100 {
		t.Errorf("paid/free %d/%d, want 150/100", paid, free)
	}
	q := planner.CandidateQuery{
		Catalog: catalog, City: "atlanta", Kinds: []string{"event", "place"}, From: day(27, 0, 0).UTC(), To: oct(11, 0, 0).UTC(),
		FreeOnly: true, AgeBracket: "21_plus", PlaceCategories: itinerary.PlaceCategories(), MinPlaceRating: 4,
	}
	matched := 0
	for i := range acts {
		a := &acts[i]
		if planner.MatchesQuery(a, &q) {
			matched++
			if !isFree(a) {
				t.Errorf("free-only keeps %s (%+v)", a.Name, a.Price)
			}
		} else if isFree(a) {
			t.Errorf("free-only drops free %s", a.Name)
		}
	}
	t.Logf("free-only query over the whole range keeps %d documents", matched)
}

// For a matrix of windows, places, radii, budgets and ages, the Mongo query and the Go re-check
// select exactly the same documents (the planner's own consistency rule, on this catalog).
func TestPitchFiltersAgree(t *testing.T) {
	db := setup(t)
	s := mongosource.New(db, nil)
	ctx := context.Background()
	windows := [][2]time.Time{
		{day(27, 8, 0), day(27, 12, 0)}, {day(27, 13, 0), day(27, 17, 0)}, {day(27, 18, 0), day(27, 23, 0)},
		{day(27, 22, 0), day(28, 3, 0)}, {day(30, 17, 30), day(30, 22, 0)}, {oct(3, 8, 0), oct(3, 13, 0)},
		{oct(2, 21, 0), oct(3, 2, 0)}, {oct(10, 12, 0), oct(10, 18, 0)},
	}
	budgets := []struct {
		free    bool
		tier    int
		unknown bool
	}{{true, 0, false}, {false, 1, true}, {false, 2, true}, {false, 3, true}}
	queries, matched := 0, 0
	for _, center := range []travel.Point{techSquare, piedmont, decatur} {
		for _, win := range windows {
			for _, radius := range []float64{2, 10, 25} {
				for _, b := range budgets {
					for _, age := range []string{"21_plus", "18_20"} {
						q := planner.CandidateQuery{
							Catalog: catalog, City: "atlanta", Kinds: []string{"event", "place"}, Center: center, RadiusKm: radius,
							From: win[0].UTC(), To: win[1].UTC(), MaxTier: b.tier, FreeOnly: b.free, AllowUnknownPrice: b.unknown,
							AgeBracket: age, PlaceCategories: itinerary.PlaceCategories(), MinPlaceRating: 4,
							LimitEvents: 5000, LimitPlaces: 5000,
						}
						events, places, err := s.FindCandidates(ctx, q)
						if err != nil {
							t.Fatal(err)
						}
						queries++
						got := map[string]bool{}
						for _, a := range append(events, places...) {
							got[a.ID.Hex()] = true
						}
						matched += len(got)
						for i := range acts {
							want := planner.MatchesQuery(&acts[i], &q)
							if want != got[acts[i].ID.Hex()] {
								t.Errorf("%s %s r=%.0f %+v %s: Mongo %v, Go %v", win[0].Format("Mon 15:04"), acts[i].Name, radius, b, age, got[acts[i].ID.Hex()], want)
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d queries agree; %d documents matched in total", queries, matched)
}

type scenario struct {
	name       string
	from, to   time.Time
	start      travel.Point
	startName  string
	rangeName  string
	modes      []string
	budget     int
	pace       string
	tags       []string
	mood       string
	wantFree   bool
	wantEvents bool
}

var scenarios = []scenario{
	{"pitch demo: Sunday 1-5 PM from Tech Square, walkable, $$, outdoors + food", day(27, 13, 0), day(27, 17, 0), techSquare, "Tech Square",
		"walkable", []string{"walk"}, 2, "balanced", []string{"Outdoors", "Food"}, "something chill outside, then food", false, false},
	{"Sunday afternoon, free only, by transit", day(27, 13, 0), day(27, 17, 0), techSquare, "Tech Square",
		"transit", []string{"walk", "marta"}, 0, "balanced", []string{"Outdoors", "Art"}, "", true, false},
	{"Sunday morning brunch and a walk, transit, $$", day(27, 9, 30), day(27, 13, 0), techSquare, "Tech Square",
		"transit", []string{"walk", "marta"}, 2, "relaxed", []string{"Food"}, "brunch then a walk", false, false},
	{"Sunday evening music and nightlife, transit, $$$", day(27, 18, 0), day(27, 23, 0), techSquare, "Tech Square",
		"transit", []string{"walk", "marta"}, 3, "balanced", []string{"Music", "Nightlife"}, "live music and dancing", false, true},
	{"Sunday late night, transit, $$", day(27, 21, 30), day(28, 2, 0), piedmont, "Piedmont Park",
		"transit", []string{"walk", "marta"}, 2, "relaxed", []string{"Nightlife"}, "", false, true},
	{"Friday night out past midnight, anywhere, $$$", oct(2, 20, 0), oct(3, 2, 0), techSquare, "Tech Square",
		"anywhere", []string{"walk", "marta"}, 3, "balanced", []string{"Nightlife", "Music"}, "", false, true},
	{"Wednesday after work, transit, $", day(30, 17, 30), day(30, 21, 30), techSquare, "Tech Square",
		"transit", []string{"walk", "marta"}, 1, "balanced", []string{"Meet people"}, "", false, false},
	{"Saturday morning outdoors, anywhere, free", oct(3, 8, 0), oct(3, 12, 30), decatur, "Decatur Square",
		"anywhere", []string{"walk", "marta"}, 0, "packed", []string{"Outdoors", "Active"}, "", true, false},
	{"Saturday afternoon classes and culture, transit, $$", oct(10, 12, 0), oct(10, 18, 0), piedmont, "Piedmont Park",
		"transit", []string{"walk", "marta"}, 2, "balanced", []string{"Nerdy", "Art"}, "learn something", false, false},
}

// Full plans through the real planner and DAG solver, twice per scenario:
//   - "no vectors": the catalog as loaded today. No document has a vector, so the planner has no
//     query vector and ranks by its prior (rating, popularity, facet match); the ML call is skipped.
//   - "classifier": the user has a profile vector and every candidate, event or place, is scored by
//     one facet rule (0.85 when it covers a requested quick pick, else 0.6), standing in for the ML
//     classifier once the catalog's vectors exist.
//
// Every option in both modes must meet the planner's own guarantees (planner.CheckOption).
func TestPitchPlans(t *testing.T) {
	db := setup(t)
	for _, mode := range []string{"no vectors", "classifier"} {
		for _, sc := range scenarios {
			t.Run(mode+"/"+sc.name, func(t *testing.T) { runScenario(t, db, sc, mode) })
		}
	}
}

func runScenario(t *testing.T, db *mongo.Database, sc scenario, mode string) {
	ctx := context.Background()
	lookup := func(id string) (*models.Activity, bool) { a, ok := byID[id]; return a, ok }
	clock := planner.NewFakeClock(sc.from.Add(-45 * time.Minute))
	store := mongosource.New(db, clock)
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := planner.DefaultConfig()
	cfg.Jev = "off"
	user := &planner.UserContext{ID: "pitch-harness", Catalog: catalog, City: "atlanta", AgeBracket: "21_plus",
		Prefs: planner.UserPrefs{Pace: sc.pace, Flexible: true}}
	scorer := &planner.FakeScorer{Err: errors.New("ml: /v1/events/rank rejects events without embeddings (vectors pending)")}
	if mode == "classifier" {
		var facets []planner.Facet
		for _, tag := range sc.tags {
			if f, ok := planner.FacetByName(tag); ok {
				facets = append(facets, f)
			}
		}
		scorer = &planner.FakeScorer{Fn: func(c *planner.Candidate) float64 {
			for _, f := range facets {
				if f.Covers(&c.Act) {
					return 0.85
				}
			}
			return 0.6
		}}
		v := make([]float64, 1024)
		v[0] = 1
		user.PositiveEmbedding = v
		user.Catalog = vectorCatalog
	}
	p, err := planner.New(cfg, planner.Deps{Source: store, Embeddings: store, Lookup: store, Pools: store, Scorer: scorer,
		Travel: travel.Heuristic{}, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	place := map[string]any{"name": sc.startName, "coordinate": map[string]float64{"lat": sc.start.Lat, "lng": sc.start.Lng}}
	body, _ := json.Marshal(map[string]any{
		"start": place, "end": place, "date": sc.from.UTC().Format(time.RFC3339),
		"start_time": sc.from.UTC().Format(time.RFC3339), "back_by": sc.to.UTC().Format(time.RFC3339),
		"range": sc.rangeName, "ride": "none", "budget": sc.budget, "who": "friends", "pace": sc.pace,
		"modes": sc.modes, "tags": sc.tags, "mood_text": sc.mood,
	})
	spec, err := planner.ParsePlanRequest(body, "America/New_York", clock.Now(), user, p.Cfg)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := p.Generate(ctx, user, spec)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]planner.Option(nil), batch.Options...)
	for cursor := batch.Cursor; cursor != ""; {
		page, err := p.More(ctx, user, cursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Options...)
		cursor = page.Cursor
	}
	if len(all) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}
	var run struct {
		ML struct {
			Mode string `bson:"mode"`
		} `bson:"ml"`
		Counts    planner.CountLog         `bson:"counts"`
		Shortlist []planner.ShortlistEntry `bson:"shortlist"`
	}
	if err := db.Collection(mongosource.RunsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: batch.RunID}}).Decode(&run); err != nil {
		t.Fatal(err)
	}
	shortEvents := 0
	for _, e := range run.Shortlist {
		if e.Kind == "event" {
			shortEvents++
		}
	}
	var lines []string
	events, paidStops, stops := 0, 0, 0
	for i := range all {
		opt := &all[i]
		for _, issue := range planner.CheckOption(&spec, opt, lookup) {
			t.Errorf("option %s breaks a guarantee: %s", opt.Name, issue)
		}
		if i < 4 {
			lines = append(lines, fmt.Sprintf("  [%s] %s | %s | score %.2f", opt.Tag, opt.Name, opt.Meta, opt.Score))
		}
		for _, s := range opt.Stops {
			stops++
			a := byID[s.ActivityID]
			if a == nil {
				t.Errorf("stop %s is not a catalog document", s.Title)
				continue
			}
			if a.Kind == "event" {
				events++
			}
			price := "free"
			if itinerary.CostCents(a) > 0 {
				paidStops++
				price = fmt.Sprintf("$%.0f", float64(itinerary.CostCents(a))/100)
			}
			if sc.wantFree && !isFree(a) {
				t.Errorf("a free-only plan includes %s (%s)", a.Name, price)
			}
			if i < 4 {
				lines = append(lines, fmt.Sprintf("      %s-%s  %-44s %-14s %-5s %s", s.Arrive.In(ny).Format("Mon 15:04"),
					s.Depart.In(ny).Format("15:04"), s.Title, s.Category, a.Kind, price))
			}
		}
	}
	if !sc.wantFree && paidStops == 0 {
		t.Errorf("a paid-inclusive request produced only free stops")
	}
	if sc.wantEvents && events == 0 && mode == "classifier" {
		t.Errorf("no event stops in an evening request")
	}
	t.Logf("%s: %d options (%d stops: %d events, %d paid); retrieved %d events + %d places, %d feasible, drops %v, "+
		"shortlist %d (%d events); ml=%q; reason=%q\n%s", mode, len(all), stops, events, paidStops, run.Counts.EventsA,
		run.Counts.PlacesA, run.Counts.Feasible, run.Counts.Drops, len(run.Shortlist), shortEvents, run.ML.Mode, batch.Reason,
		strings.Join(lines, "\n"))
}

// Fixed-start and drop-in eligibility at the edges of a window.
func TestPitchEventEligibility(t *testing.T) {
	db := setup(t)
	s := mongosource.New(db, nil)
	find := func(from, to time.Time) map[string]bool {
		q := planner.CandidateQuery{
			Catalog: catalog, City: "atlanta", Kinds: []string{"event"}, Center: techSquare, RadiusKm: 30,
			From: from.UTC(), To: to.UTC(), MaxTier: 3, AgeBracket: "21_plus", PlaceCategories: itinerary.PlaceCategories(),
			LimitEvents: 5000,
		}
		events, _, err := s.FindCandidates(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range events {
			out[e.Name] = true
		}
		return out
	}
	// 19:10 on the pitch day: a concert that started at 19:00 can be joined late (live music is
	// clippable); the drop-in salsa social (17:00-20:00) is still open; the 18:00 dinner is not.
	got := find(day(27, 19, 10), day(27, 23, 0))
	for name, want := range map[string]bool{
		"Soul & Funk Revue": true, "Sunset Salsa Social": true, "Sunday Supper: Beer Pairing Dinner": false,
		"Amapiano Sunset Session": true, "Sunday Soul & Disco Night": true,
	} {
		if got[name] != want {
			t.Errorf("window 19:10-23:00: %s retrieved=%v, want %v", name, got[name], want)
		}
	}
	// A window that closes before a fixed start (less the 15-minute margin) leaves it out.
	got = find(day(27, 17, 0), day(27, 19, 10))
	if got["Soul & Funk Revue"] {
		t.Error("a 19:00 show should not be offered in a window ending at 19:10")
	}
	if !got["Sunset Salsa Social"] {
		t.Error("the drop-in salsa social overlaps 17:00-19:10")
	}
}
