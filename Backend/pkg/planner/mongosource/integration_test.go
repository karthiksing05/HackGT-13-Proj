//go:build integration

// Integration tests against a real MongoDB (MONGO_TEST_URI, default
// mongodb://127.0.0.1:27017). They copy freetime.demo_activities and
// freetime.activities into a throwaway sq_test_planner_* database (freetime
// is only read) and drop it at the end. Without a server they skip, unless
// CI=1.
//
//	go test -tags integration ./pkg/planner/mongosource/
package mongosource_test

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
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	setupOnce sync.Once
	setupErr  error
	client    *mongo.Client
	testDB    *mongo.Database
	extraDBs  []*mongo.Database
	catalogs  = map[string][]models.Activity{}

	ny, _      = time.LoadLocation("America/New_York")
	seasideMkt = travel.Point{Lat: 31.3680, Lng: -81.4250}
	techSquare = travel.Point{Lat: 33.7766, Lng: -84.3890}
)

func TestMain(m *testing.M) {
	code := m.Run()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, db := range append(extraDBs, testDB) {
		if db != nil {
			if err := db.Drop(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "drop %s: %v\n", db.Name(), err)
				code = 1
			}
		}
	}
	if client != nil {
		if err := client.Disconnect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "disconnect: %v\n", err)
		}
	}
	os.Exit(code)
}

func setup(t *testing.T) *mongo.Database {
	t.Helper()
	setupOnce.Do(func() { setupErr = connectAndSeed() })
	if setupErr != nil {
		if os.Getenv("CI") == "1" {
			t.Fatalf("mongo: %v", setupErr)
		}
		t.Skipf("mongo unavailable: %v", setupErr)
	}
	return testDB
}

func dbName(tag string) string {
	return fmt.Sprintf("sq_test_planner_%s%d_%d", tag, os.Getpid(), time.Now().UnixNano()%1_000_000)
}

// connectAndSeed copies both catalogs (read-only on freetime) and builds
// the indexes the store's EnsureIndexes creates on them in production.
func connectAndSeed() error {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(3 * time.Second))
	if err != nil {
		return err
	}
	if err := c.Ping(ctx, nil); err != nil {
		return err
	}
	client = c
	testDB = c.Database(dbName(""))
	for _, name := range []string{"demo_activities", "activities"} {
		cur, err := c.Database("freetime").Collection(name).Find(ctx, bson.D{})
		if err != nil {
			return err
		}
		var docs []bson.Raw
		if err := cur.All(ctx, &docs); err != nil {
			return err
		}
		if len(docs) == 0 {
			return fmt.Errorf("freetime.%s is empty; seed the local database first", name)
		}
		insert := make([]any, len(docs))
		acts := make([]models.Activity, len(docs))
		for i, d := range docs {
			insert[i] = d
			if err := bson.Unmarshal(d, &acts[i]); err != nil {
				return fmt.Errorf("decode %s: %w", name, err)
			}
		}
		if _, err := testDB.Collection(name).InsertMany(ctx, insert); err != nil {
			return err
		}
		if _, err := testDB.Collection(name).Indexes().CreateMany(ctx, []mongo.IndexModel{
			{Keys: bson.D{{Key: "location", Value: "2dsphere"}}},
			{Keys: bson.D{{Key: "city", Value: 1}, {Key: "kind", Value: 1}, {Key: "start", Value: 1}}},
		}); err != nil {
			return err
		}
		catalogs[name] = acts
	}
	return nil
}

func ids(acts []models.Activity) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.ID.Hex()] = a.Name
	}
	return out
}

func rating(v *float64) float64 {
	if v == nil {
		return math.Inf(-1)
	}
	return *v
}

// TestMongoAndGoFiltersAgree: for a matrix of queries, the Mongo query and
// the Go re-check (planner.MatchesQuery) select exactly the same documents,
// and Mongo returns them in the documented order.
func TestMongoAndGoFiltersAgree(t *testing.T) {
	db := setup(t)
	s := mongosource.New(db, nil)
	ctx := context.Background()
	worlds := []struct {
		catalog, city string
		center        travel.Point
	}{
		{"demo_activities", "saltlight", seasideMkt},
		{"activities", "atlanta", techSquare},
	}
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, ny) }
	windows := [][2]time.Time{
		{day(26, 18), day(26, 23)}, {day(26, 12), day(26, 18)}, {day(27, 10), day(27, 16)},
		{time.Date(2026, 10, 2, 17, 0, 0, 0, ny), time.Date(2026, 10, 3, 1, 0, 0, 0, ny)},
	}
	budgets := []struct {
		free    bool
		tier    int
		unknown bool
	}{{true, 0, false}, {false, 1, true}, {false, 2, false}, {false, 3, true}}
	variants := []func(q *planner.CandidateQuery, acts []models.Activity){
		func(q *planner.CandidateQuery, acts []models.Activity) {},
		func(q *planner.CandidateQuery, acts []models.Activity) {
			q.ExcludeCategories, q.ExcludeTags = []string{"bar", "museum"}, []string{"outdoor"}
		},
		func(q *planner.CandidateQuery, acts []models.Activity) {
			q.IncludeCategories, q.AnyTags = []string{"restaurant", "cafe", "market"}, []string{"food"}
			for i := 0; i < 5 && i < len(acts); i++ {
				q.ExcludeIDs = append(q.ExcludeIDs, acts[i*7%len(acts)].ID.Hex())
			}
		},
	}
	queries, matched := 0, 0
	for _, w := range worlds {
		acts := catalogs[w.catalog]
		for _, win := range windows {
			for _, radius := range []float64{2, 6, 15} {
				for _, b := range budgets {
					for _, age := range []string{"21_plus", "18_20", "13_17"} {
						for vi, variant := range variants {
							q := planner.CandidateQuery{
								Catalog: w.catalog, City: w.city, Kinds: []string{"event", "place"},
								Center: w.center, RadiusKm: radius, From: win[0].UTC(), To: win[1].UTC(),
								MaxTier: b.tier, FreeOnly: b.free, AllowUnknownPrice: b.unknown, AgeBracket: age,
								PlaceCategories: itinerary.PlaceCategories(), LimitEvents: 5000, LimitPlaces: 5000,
							}
							variant(&q, acts)
							events, places, err := s.FindCandidates(ctx, q)
							if err != nil {
								t.Fatal(err)
							}
							queries++
							want := map[string]string{}
							for i := range acts {
								if planner.MatchesQuery(&acts[i], &q) {
									want[acts[i].ID.Hex()] = acts[i].Kind + " " + acts[i].Name
								}
							}
							got := ids(append(append([]models.Activity(nil), events...), places...))
							matched += len(got)
							name := fmt.Sprintf("%s %s r=%.0f budget=%+v %s variant %d", w.catalog, win[0].Format("Mon 15:04"), radius, b, age, vi)
							for id, n := range want {
								if _, ok := got[id]; !ok {
									t.Errorf("%s: Go keeps %s, Mongo does not", name, n)
								}
							}
							for id, n := range got {
								if _, ok := want[id]; !ok {
									t.Errorf("%s: Mongo returns %s, Go rejects it", name, n)
								}
							}
							for _, e := range events {
								if e.Kind != "event" {
									t.Errorf("%s: %s in the event list", name, e.Name)
								}
							}
							if !sort.SliceIsSorted(events, func(i, j int) bool {
								if !events[i].Start.Equal(*events[j].Start) {
									return events[i].Start.Before(*events[j].Start)
								}
								return events[i].ID.Hex() < events[j].ID.Hex()
							}) {
								t.Errorf("%s: events out of order", name)
							}
							if !sort.SliceIsSorted(places, func(i, j int) bool {
								if ri, rj := rating(places[i].Rating), rating(places[j].Rating); ri != rj {
									return ri > rj
								}
								if pi, pj := rating(places[i].Popularity), rating(places[j].Popularity); pi != pj {
									return pi > pj
								}
								return places[i].ID.Hex() < places[j].ID.Hex()
							}) {
								t.Errorf("%s: places out of order", name)
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d queries, %d documents matched in total", queries, matched)
	if matched < queries {
		t.Errorf("the matrix is too sparse to mean anything (%d matches over %d queries)", matched, queries)
	}
}

// TestFindCandidatesSaltlightEvening is planner.md §8's check: Saturday
// 18:00–23:00, walkable from Seaside Market.
func TestFindCandidatesSaltlightEvening(t *testing.T) {
	db := setup(t)
	s := mongosource.New(db, nil)
	from, to := time.Date(2026, 9, 26, 18, 0, 0, 0, ny).UTC(), time.Date(2026, 9, 26, 23, 0, 0, 0, ny).UTC()
	base := planner.CandidateQuery{
		Catalog: "demo_activities", City: "saltlight", Kinds: []string{"event", "place"}, Center: seasideMkt, RadiusKm: 5,
		From: from, To: to, MaxTier: 3, AgeBracket: "21_plus", PlaceCategories: itinerary.PlaceCategories(),
		LimitEvents: 400, LimitPlaces: 600,
	}
	find := func(q planner.CandidateQuery) ([]models.Activity, []models.Activity) {
		ev, pl, err := s.FindCandidates(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		return ev, pl
	}
	events, places := find(base)
	if len(events) == 0 || len(places) == 0 {
		t.Fatalf("%d events, %d places", len(events), len(places))
	}
	for _, a := range append(events, places...) {
		p := travel.Point{Lat: a.Location.Coordinates[1], Lng: a.Location.Coordinates[0]}
		if d := travel.HaversineKm(seasideMkt, p); d > 5 {
			t.Errorf("%s is %.2f km away", a.Name, d)
		}
		if a.Kind != "event" {
			continue
		}
		dropIn := a.Attendance != nil && *a.Attendance == "drop_in"
		if !dropIn && (a.Start.Before(from) || a.Start.After(to.Add(-15*time.Minute))) {
			t.Errorf("%s starts %v, outside the window", a.Name, a.Start.In(ny))
		}
		if dropIn && (!a.Start.Before(to) || (a.End != nil && !a.End.After(from))) {
			t.Errorf("drop-in %s does not overlap the window", a.Name)
		}
	}
	adult := func(a models.Activity) bool {
		for _, tag := range a.Tags {
			if tag == "21_plus" {
				return true
			}
		}
		return a.Category == "bar" || a.Category == "nightclub" || strings.Contains(a.Name, "21+")
	}
	hadAdult := false
	for _, a := range append(events, places...) {
		hadAdult = hadAdult || adult(a)
	}
	under21 := base
	under21.AgeBracket = "18_20"
	ev, pl := find(under21)
	for _, a := range append(ev, pl...) {
		if adult(a) {
			t.Errorf("18_20 sees %s", a.Name)
		}
	}
	if !hadAdult || len(ev)+len(pl) >= len(events)+len(places) {
		t.Error("the age variant should remove something on a Saturday night")
	}
	free := base
	free.FreeOnly, free.MaxTier = true, 0
	ev, pl = find(free)
	for _, a := range append(ev, pl...) {
		if a.Price != nil && !a.Price.IsFree && (a.Price.Min == nil || *a.Price.Min != 0) {
			t.Errorf("free-only returns %s at %+v", a.Name, *a.Price)
		}
	}
	if len(ev) == 0 {
		t.Error("Saltlight has free events on Saturday evening")
	}
}

// sandy is the demo user; her profile vector stands in for the ML service:
// the normalised mean of the catalog's outdoor + music/nature vectors.
func sandy(t *testing.T) *planner.UserContext {
	var sum []float64
	n := 0
	for _, a := range catalogs["demo_activities"] {
		tags := strings.Join(a.Tags, ",")
		if len(a.Embedding) == 0 || !strings.Contains(tags, "outdoor") || !(strings.Contains(tags, "music") || strings.Contains(tags, "nature")) {
			continue
		}
		if sum == nil {
			sum = make([]float64, len(a.Embedding))
		}
		for i, x := range a.Embedding {
			sum[i] += x
		}
		n++
	}
	if n == 0 {
		t.Fatal("no vectors in demo_activities")
	}
	norm := 0.0
	for _, x := range sum {
		norm += x * x
	}
	for i := range sum {
		sum[i] /= math.Sqrt(norm)
	}
	return &planner.UserContext{ID: "sandy", Catalog: "demo_activities", City: "saltlight", AgeBracket: "21_plus",
		PositiveEmbedding: sum, Prefs: planner.UserPrefs{Pace: "balanced", Flexible: true}}
}

func newPlanner(store *mongosource.Store, clock planner.Clock, user *planner.UserContext) *planner.Planner {
	cfg := planner.DefaultConfig()
	cfg.Jev = "off"
	scorer := &planner.FakeScorer{Fn: func(c *planner.Candidate) float64 {
		s := 0.0
		for i, x := range c.Act.Embedding {
			if i < len(user.PositiveEmbedding) {
				s += x * user.PositiveEmbedding[i]
			}
		}
		return math.Max(0, math.Min(1, 0.5+0.5*s))
	}}
	return planner.New(cfg, planner.Deps{Source: store, Embeddings: store, Lookup: store, Pools: store, Scorer: scorer,
		Travel: travel.Heuristic{}, Clock: clock})
}

// TestGenerateSurvivesARestart: a run generated through one Store is paged,
// re-routed, given alternatives and saved through fresh Store and Planner
// instances on the same database, and every option meets §7.
func TestGenerateSurvivesARestart(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	clock := planner.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, ny))
	user := sandy(t)
	storeA := mongosource.New(db, clock)
	if err := storeA.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"start":      map[string]any{"name": "Seaside Market Square", "coordinate": map[string]float64{"lat": seasideMkt.Lat, "lng": seasideMkt.Lng}},
		"end":        map[string]any{"name": "Seaside Market Square", "coordinate": map[string]float64{"lat": seasideMkt.Lat, "lng": seasideMkt.Lng}},
		"date":       "2026-09-26T22:00:00Z",
		"start_time": "2026-09-26T22:00:00Z",
		"back_by":    "2026-09-27T03:00:00Z",
		"range":      "walkable", "ride": "none", "budget": 2, "who": "friends", "pace": "balanced",
		"modes": []string{"walk"}, "tags": []string{"Outdoors", "Food"}, "mood_text": "something chill outside, then food",
	})
	a := newPlanner(storeA, clock, user)
	spec, err := planner.ParsePlanRequest(body, "America/New_York", clock.Now(), user, a.Cfg)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := a.Generate(ctx, user, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Options) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}

	// Restart 1: page, re-route and ask for alternatives.
	storeB := mongosource.New(db, clock)
	b := newPlanner(storeB, clock, user)
	pool, err := storeB.GetPool(ctx, batch.RunID)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(batch.Options)
	stored, _ := json.Marshal(pool.Options[:len(batch.Options)])
	if string(first) != string(stored) {
		t.Errorf("options changed on the way through Mongo:\n%s\n%s", first, stored)
	}
	all := append([]planner.Option(nil), batch.Options...)
	for cursor := batch.Cursor; cursor != ""; {
		page, err := b.More(ctx, user, cursor)
		if err != nil || len(page.Options) == 0 {
			t.Fatalf("more %s: %+v %v", cursor, page, err)
		}
		all = append(all, page.Options...)
		cursor = page.Cursor
	}
	if len(all) != len(pool.Options) {
		t.Errorf("paged %d of %d options", len(all), len(pool.Options))
	}
	eff := spec
	eff.MaxLegKm, eff.Budget = pool.Window.MaxLegKm, pool.Spec.Budget
	byID := map[string]models.Activity{}
	for _, act := range catalogs["demo_activities"] {
		byID[act.ID.Hex()] = act
	}
	lookup := func(id string) (*models.Activity, bool) {
		act, ok := byID[id]
		return &act, ok
	}
	for _, opt := range all {
		for _, v := range planner.CheckOption(&eff, &opt, lookup) {
			t.Errorf("%s: %s", opt.ID, v)
		}
	}
	opt := all[0]
	order := make([]string, len(opt.Stops))
	for i, s := range opt.Stops {
		order[i] = s.ID
	}
	if _, err := b.Route(ctx, planner.RouteInput{UserID: user.ID, OptionID: opt.ID, StopOrder: order}); err != nil {
		t.Fatal(err)
	}
	alts, err := b.Alternatives(ctx, planner.AlternativesInput{UserID: user.ID, OptionID: opt.ID, StopID: order[0], StopOrder: order})
	if err != nil || len(alts) == 0 {
		t.Fatalf("alternatives: %v (%d)", err, len(alts))
	}
	for _, alt := range alts {
		for _, s := range opt.Stops {
			if alt.Stop.ActivityID == s.ActivityID {
				t.Errorf("alternative %s repeats a stop in the plan", alt.Stop.Title)
			}
		}
	}

	// Restart 2: the alternative is known to a fresh instance; save.
	storeC := mongosource.New(db, clock)
	c := newPlanner(storeC, clock, user)
	swapped := append([]string{alts[0].Stop.ID}, order[1:]...)
	route, err := c.Route(ctx, planner.RouteInput{UserID: user.ID, OptionID: opt.ID, StopOrder: swapped})
	if err != nil {
		t.Fatalf("route with the alternative after a restart: %v", err)
	}
	edited := opt
	edited.Stops = append([]planner.Stop{alts[0].Stop}, opt.Stops[1:]...)
	draft, err := c.BuildItinerary(ctx, planner.SaveInput{UserID: user.ID, Catalog: "demo_activities",
		Plan:   planner.Request{Start: planner.PlaceAt("Seaside Market Square", seasideMkt), End: planner.PlaceAt("Seaside Market Square", seasideMkt), StartTime: spec.From, BackBy: spec.BackBy, Date: spec.From},
		Option: edited, StopOrder: swapped, Route: route, Visibility: "just_me", TZ: ny, ItineraryID: "itin-restart"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.PoolMissing || draft.Items[1].ActivityID != alts[0].Stop.ActivityID {
		t.Errorf("draft %+v", draft.Items[1])
	}

	var run planner.PlanRun
	if err := db.Collection(mongosource.RunsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: batch.RunID}}).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if o := run.Outcome; o == nil || o.SavedOptionID != opt.ID || o.ItineraryID != "itin-restart" ||
		len(o.AlternativesUsed) != 1 || o.AlternativesUsed[0] != alts[0].Stop.ID || strings.Join(o.StopOrder, ",") != strings.Join(swapped, ",") {
		t.Errorf("outcome %+v", run.Outcome)
	}
	if len(run.Rounds) == 0 || run.Counts.Feasible == 0 || run.ML.Mode != "classifier" || len(run.Shortlist) == 0 || run.UserID != user.ID {
		t.Errorf("run log: %d rounds, counts %+v, ml %+v", len(run.Rounds), run.Counts, run.ML)
	}
	var poolDoc planner.PlanPool
	if err := db.Collection(mongosource.PoolsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: batch.RunID}}).Decode(&poolDoc); err != nil {
		t.Fatal(err)
	}
	if run.ExpiresAt.Sub(run.CreatedAt) != 72*time.Hour || poolDoc.ExpiresAt.Sub(poolDoc.CreatedAt) != 6*time.Hour {
		t.Errorf("expiry: run %v, pool %v", run.ExpiresAt.Sub(run.CreatedAt), poolDoc.ExpiresAt.Sub(poolDoc.CreatedAt))
	}
	if len(poolDoc.Alternatives) != len(alts) || len(poolDoc.QueryVector) != len(user.PositiveEmbedding) {
		t.Errorf("pool: %d alternatives, %d-d query vector", len(poolDoc.Alternatives), len(poolDoc.QueryVector))
	}
	// The TTL monitor only removes documents whose expiresAt is a BSON date.
	for _, coll := range []string{mongosource.RunsCollection, mongosource.PoolsCollection} {
		raw, err := db.Collection(coll).FindOne(ctx, bson.D{{Key: "_id", Value: batch.RunID}}).Raw()
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"expiresAt", "createdAt"} {
			if v := raw.Lookup(field); v.Type != bson.TypeDateTime {
				t.Errorf("%s.%s is %v, not a date", coll, field, v.Type)
			}
		}
		if v := raw.Lookup("userId"); v.Type != bson.TypeString || v.StringValue() != user.ID {
			t.Errorf("%s.userId = %v", coll, v)
		}
	}

	// Past its expiry a pool reads as gone before the TTL monitor runs.
	clock.Advance(6*time.Hour + time.Minute)
	if _, err := storeC.GetPool(ctx, batch.RunID); !errors.Is(err, planner.ErrPoolNotFound) {
		t.Errorf("expired pool: %v", err)
	}
	if page, err := c.More(ctx, user, batch.Cursor); err != nil || len(page.Options) != 0 || !page.Done {
		t.Errorf("expired cursor: %+v %v", page, err)
	}
	if err := storeC.AddAlternatives(ctx, batch.RunID, nil); err != nil {
		t.Errorf("adding nothing is a no-op: %v", err)
	}
	if err := storeC.AddAlternatives(ctx, batch.RunID, []planner.Stop{alts[0].Stop}); !errors.Is(err, planner.ErrPoolNotFound) {
		t.Errorf("add to an expired pool: %v", err)
	}
	if err := storeC.AddAlternatives(ctx, "whatever", []planner.Stop{{ID: "a.b"}}); err == nil {
		t.Error("a stop id with a dot must be refused")
	}
	if err := storeC.PatchRun(ctx, "no-such-run", bson.M{"outcome": bson.M{}}); !errors.Is(err, planner.ErrPoolNotFound) {
		t.Errorf("patch a missing run: %v", err)
	}
}

func indexSpecs(t *testing.T, coll *mongo.Collection) map[string]mongo.IndexSpecification {
	t.Helper()
	specs, err := coll.Indexes().ListSpecifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]mongo.IndexSpecification{}
	for _, s := range specs {
		out[s.Name] = s
	}
	return out
}

// TestEnsureIndexes: the TTL and lookup indexes exist with the shared names
// and options, a second call (or the foundation's identical indexes) is a
// no-op, and a same-named index with other options is an error.
func TestEnsureIndexes(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	s := mongosource.New(db, nil)
	for i := 0; i < 2; i++ {
		if err := s.EnsureIndexes(ctx); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	check := func(coll, name, keys string, ttl bool) {
		spec, ok := indexSpecs(t, db.Collection(coll))[name]
		if !ok {
			t.Fatalf("%s has no index %s", coll, name)
		}
		if spec.KeysDocument.String() != keys {
			t.Errorf("%s.%s keys %s", coll, name, spec.KeysDocument)
		}
		if ttl != (spec.ExpireAfterSeconds != nil && *spec.ExpireAfterSeconds == 0) {
			t.Errorf("%s.%s ttl %v", coll, name, spec.ExpireAfterSeconds)
		}
	}
	check(mongosource.PoolsCollection, mongosource.ExpiresIndex, `{"expiresAt": {"$numberInt":"1"}}`, true)
	check(mongosource.RunsCollection, mongosource.ExpiresIndex, `{"expiresAt": {"$numberInt":"1"}}`, true)
	check(mongosource.RunsCollection, mongosource.RunsByUserIndex, `{"userId": {"$numberInt":"1"},"createdAt": {"$numberInt":"-1"}}`, false)

	// The foundation's store.EnsureIndexes got there first: no conflict.
	first := client.Database(dbName("idx_"))
	extraDBs = append(extraDBs, first)
	ttlOpts := options.Index().SetName(mongosource.ExpiresIndex).SetExpireAfterSeconds(0)
	for _, coll := range []string{mongosource.PoolsCollection, mongosource.RunsCollection} {
		if _, err := first.Collection(coll).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: ttlOpts}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.Collection(mongosource.RunsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName(mongosource.RunsByUserIndex)}); err != nil {
		t.Fatal(err)
	}
	if err := mongosource.New(first, nil).EnsureIndexes(ctx); err != nil {
		t.Errorf("after the foundation's identical indexes: %v", err)
	}

	// Same name, other options: refuse rather than silently keep a wrong TTL.
	conflict := client.Database(dbName("conflict_"))
	extraDBs = append(extraDBs, conflict)
	if _, err := conflict.Collection(mongosource.PoolsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName(mongosource.ExpiresIndex).SetExpireAfterSeconds(60)}); err != nil {
		t.Fatal(err)
	}
	if err := mongosource.New(conflict, nil).EnsureIndexes(ctx); err == nil {
		t.Error("a conflicting TTL index must be reported")
	}
}
