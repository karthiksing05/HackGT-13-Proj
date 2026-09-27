package planner_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/itinerary"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// The HTTP surface of the planner, end to end: the router and handlers of
// pkg/api/planning over the real planner, the real Mongo store (a test
// database seeded with freetime's demo catalog) and the real ML client
// talking to a fake ML service.

var seaside = contract.Place{Name: "Seaside Market Square", Coordinate: &contract.Coordinate{Lat: 31.368, Lng: -81.425}}

// fakeML answers /healthz, /v1/search-profile and /v1/events/rank: every
// candidate scores 0.5 + 0.5·cos(user, activity).
type fakeML struct {
	mu       sync.Mutex
	search   []float64
	ranks    []ml.RankEventsRequest
	searches int
}

func (f *fakeML) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/healthz":
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "jev": false,
			"ranking": map[string]any{"model_version": "fake-1"}, "embedding": map[string]any{"model": ml.Model, "dim": ml.Dim}})
	case "/v1/search-profile":
		f.searches++
		_ = json.NewEncoder(w).Encode(map[string]any{"search_text": "Interests:\n- outdoor recreation",
			"search_embedding": f.search, "model": ml.Model, "dim": ml.Dim, "provider": "fake"})
	case "/v1/events/rank":
		var req ml.RankEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.ranks = append(f.ranks, req)
		events := []map[string]any{}
		for _, e := range req.Events {
			cos := 0.0
			for i, x := range e.Embedding {
				cos += x * req.User.PositiveEmbedding[i]
			}
			events = append(events, map[string]any{"event_id": e.ID, "score": 0.5 + 0.5*cos, "rerank_score": nil})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": events, "model_version": "fake-1", "reranked": false})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeML) rankCalls() []ml.RankEventsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ml.RankEventsRequest(nil), f.ranks...)
}

// seedDemoCatalog copies freetime.demo_activities (read only) into db.
func seedDemoCatalog(t *testing.T, db *mongo.Database) []models.Activity {
	t.Helper()
	ctx := context.Background()
	cur, err := db.Client().Database("freetime").Collection("demo_activities").Find(ctx, bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	var raws []bson.Raw
	if err := cur.All(ctx, &raws); err != nil {
		t.Fatal(err)
	}
	if len(raws) == 0 {
		if os.Getenv("CI") == "1" {
			t.Fatal("freetime.demo_activities is empty; seed the local database")
		}
		t.Skip("freetime.demo_activities is empty")
	}
	docs := make([]any, len(raws))
	acts := make([]models.Activity, len(raws))
	for i, raw := range raws {
		docs[i] = raw
		if err := bson.Unmarshal(raw, &acts[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Collection(store.ActivityCollection).InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	return acts
}

// profileVector stands in for Sandy's taste vector: the normalised mean of
// the catalog's outdoor + music/nature activities.
func profileVector(t *testing.T, acts []models.Activity) []float64 {
	sum := make([]float64, ml.Dim)
	n := 0
	for _, a := range acts {
		tags := strings.Join(a.Tags, ",")
		if len(a.Embedding) != ml.Dim || !strings.Contains(tags, "outdoor") || !(strings.Contains(tags, "music") || strings.Contains(tags, "nature")) {
			continue
		}
		for i, x := range a.Embedding {
			sum[i] += x
		}
		n++
	}
	if n == 0 {
		t.Fatal("no activity vectors in the demo catalog")
	}
	norm := 0.0
	for _, x := range sum {
		norm += x * x
	}
	for i := range sum {
		sum[i] /= math.Sqrt(norm)
	}
	return sum
}

// decodeStrict reads a body into a contract type, refusing unknown keys.
func decodeStrict(t *testing.T, res *testutil.Response, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(res.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("body is not the contract shape: %v\n%s", err, res.Body)
	}
}

func planRequest(start contract.Place, from, backBy time.Time) contract.PlanRequest {
	return contract.PlanRequest{
		Start: start, End: start, Date: contract.NewTime(from), StartTime: contract.NewTime(from), BackBy: contract.NewTime(backBy),
		Range: contract.RangeWalkable, Ride: contract.RideNone, MoodText: "something chill outside, then food",
		Tags: []string{"Outdoors", "Food"}, Budget: 2, Who: contract.VisibilityFriends, Pace: contract.PaceBalanced,
		Modes: contract.TravelModes{contract.ModeWalk},
	}
}

func TestPlanningOverHTTP(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 26 Sep 2026, noon in New York
	ctx := context.Background()
	db := srv.Store.DB()
	acts := seedDemoCatalog(t, db)
	catalog := map[string]models.Activity{}
	for _, a := range acts {
		catalog[a.ID.Hex()] = a
	}
	profile := profileVector(t, acts)

	fake := &fakeML{search: profile}
	mlSrv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(mlSrv.Close)
	cfg := planner.DefaultConfig()
	cfg.PoolTTL, cfg.RunTTL = 24*365*time.Hour, 24*365*time.Hour // the TTL monitor uses the wall clock, the test clock is frozen
	st := mongosource.New(db, srv.Clock)
	if err := st.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	scorer := planner.NewMLScorer(ml.NewClient(mlSrv.URL))
	p, err := planner.New(cfg, planner.Deps{Source: st, Embeddings: st, Lookup: st, Pools: st, Scorer: scorer, Search: scorer, Clock: srv.Clock})
	if err != nil {
		t.Fatal(err)
	}
	svc := planner.NewService(p)
	srv.Deps.Planner = svc

	// Sandy: a Saltlight home base and taste vector, as the demo seed sets
	// them (the app cannot change them yet).
	sandy := srv.Signup(t, "Sandy Byte")
	if _, err := srv.Store.Users().Update(ctx, sandy.UserID, bson.M{
		"email": testutil.UniqueEmail("demo"), "city": "saltlight",
		"homeBase":          models.HomeBase{Name: seaside.Name, Lat: seaside.Coordinate.Lat, Lng: seaside.Coordinate.Lng},
		"positiveEmbedding": profile, "positiveText": "Interests:\n- outdoor recreation\n- live music",
	}); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC) // 18:00–23:00 in Saltlight
	backBy := from.Add(5 * time.Hour)

	// Generate: options with their schedule, no reason, a cursor.
	var batch contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", planRequest(seaside, from, backBy), sandy).Expect(t, http.StatusOK), &batch)
	if len(batch.Options) == 0 || len(batch.Options) > 3 || batch.Reason != nil {
		t.Fatalf("generate: %d options, reason %v", len(batch.Options), batch.Reason)
	}
	all := append([]contract.PlanOption(nil), batch.Options...)
	for _, opt := range batch.Options {
		for _, s := range opt.Stops {
			if s.ArriveTime == nil || s.DepartTime == nil || s.ActivityID == nil || s.Flexible == nil || !s.Kind.Valid() {
				t.Fatalf("stop %s lacks its schedule: %+v", s.ID, s)
			}
			if _, ok := catalog[*s.ActivityID]; !ok {
				t.Errorf("stop %s is not from Sandy's catalog", s.ID)
			}
			if s.ArriveTime.Before(from) || s.DepartTime.After(backBy) {
				t.Errorf("stop %s runs %v–%v outside the window", s.ID, s.ArriveTime, s.DepartTime)
			}
		}
	}
	ranks := fake.rankCalls()
	if len(ranks) == 0 || fake.searches == 0 {
		t.Fatal("the planner never asked the ML service")
	}
	if u := ranks[0].User; u.MaxPrice == nil || *u.MaxPrice != 70 || u.MaxDistanceMiles == nil || u.AvailableStart == nil || !u.AvailableStart.Equal(from) {
		t.Errorf("rank constraints %+v", u)
	}

	// More: the rest of the run, two at a time.
	for cursor := batch.Cursor; cursor != nil; {
		var page contract.PlanBatch
		decodeStrict(t, srv.Do(t, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: *cursor}, sandy).Expect(t, http.StatusOK), &page)
		if len(page.Options) == 0 || len(page.Options) > 2 || page.Done != (page.Cursor == nil) {
			t.Fatalf("more: %+v", page)
		}
		all = append(all, page.Options...)
		cursor = page.Cursor
	}

	route := func(sess *testutil.Session, opt contract.PlanOption, order []string) *testutil.Response {
		return srv.Do(t, "POST", "/plans/route", contract.RouteRequest{OptionID: opt.ID, StopOrder: order, Start: seaside, End: seaside,
			StartTime: contract.NewTime(from), BackBy: contract.NewTime(backBy), Ride: contract.RideNone,
			Modes: contract.TravelModes{contract.ModeWalk}}, sess)
	}
	ids := func(opt contract.PlanOption) []string {
		out := make([]string, len(opt.Stops))
		for i, s := range opt.Stops {
			out[i] = s.ID
		}
		return out
	}

	// Route, same order: nothing breaks and the times are the planner's.
	best := all[0]
	var result contract.RouteResult
	decodeStrict(t, route(sandy, best, ids(best)).Expect(t, http.StatusOK), &result)
	if result.BrokenAt != -1 || result.MinutesLate != 0 || len(result.Legs) != len(best.Stops)+1 || len(result.StopTimes) != len(best.Stops) {
		t.Fatalf("same order: %+v", result)
	}
	for i, w := range result.StopTimes {
		if !w.Start.Equal(best.Stops[i].ArriveTime.Time) || !w.End.Equal(best.Stops[i].DepartTime.Time) {
			t.Errorf("stop %d re-timed to %v–%v", i, w.Start, w.End)
		}
	}

	// broken_at is the first stop that no longer works: a fixed start
	// reached late, or a place visited outside its hours. The expectation
	// is computed independently, from the catalog's opening hours.
	opensFor := func(id string, start, end time.Time) bool {
		a := catalog[id]
		if a.Kind != "place" {
			return true
		}
		loc, _ := time.LoadLocation(a.Timezone)
		ivs, ok := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, start.Add(-time.Hour), end.Add(time.Hour))
		for _, iv := range ivs {
			if ok && !start.Before(iv.Start) && !end.After(iv.End) {
				return true
			}
		}
		return false
	}
	// An option with a place and, later, a fixed start; the checks below
	// route just those two stops (a stop left out is removed).
	var place, event contract.PlanStop
	var mixed contract.PlanOption
	for _, opt := range all {
		for i := 0; i < len(opt.Stops) && mixed.ID == ""; i++ {
			for j := i + 1; j < len(opt.Stops); j++ {
				if *opt.Stops[i].Flexible && opt.Stops[i].Kind == contract.StopKindPlace &&
					opt.Stops[j].Kind == contract.StopKindEvent && !*opt.Stops[j].Flexible {
					mixed, place, event = opt, opt.Stops[i], opt.Stops[j]
					break
				}
			}
		}
		if mixed.ID != "" {
			break
		}
	}
	if mixed.ID == "" {
		t.Fatal("no option with a place and then a fixed start")
	}
	// The show alone, leaving ten minutes after it starts: its fixed start
	// is missed, so broken_at is 0 and the minutes late are at least ten.
	decodeStrict(t, srv.Do(t, "POST", "/plans/route", contract.RouteRequest{OptionID: mixed.ID, StopOrder: []string{event.ID},
		Start: seaside, End: seaside, StartTime: contract.NewTime(event.ArriveTime.Add(10 * time.Minute)), BackBy: contract.NewTime(backBy),
		Ride: contract.RideNone, Modes: contract.TravelModes{contract.ModeWalk}}, sandy).Expect(t, http.StatusOK), &result)
	if result.BrokenAt != 0 || result.MinutesLate < 10 || len(result.Legs) != 2 {
		t.Errorf("missed start: %+v", result)
	}
	// Leaving five minutes before the show: the place still takes its time,
	// so the fixed start is missed (unless the place was already closed).
	late := srv.Do(t, "POST", "/plans/route", contract.RouteRequest{OptionID: mixed.ID, StopOrder: []string{place.ID, event.ID}, Start: seaside, End: seaside,
		StartTime: contract.NewTime(event.ArriveTime.Add(-5 * time.Minute)), BackBy: contract.NewTime(backBy), Ride: contract.RideNone,
		Modes: contract.TravelModes{contract.ModeWalk}}, sandy).Expect(t, http.StatusOK)
	decodeStrict(t, late, &result)
	wantBroken := 1
	if !opensFor(*place.ActivityID, result.StopTimes[0].Start.Time, result.StopTimes[0].End.Time) {
		wantBroken = 0
	}
	t.Logf("late start: %s then %s → broken_at %d, minutes_late %d", place.Title, event.Title, result.BrokenAt, result.MinutesLate)
	if result.BrokenAt != wantBroken || result.MinutesLate <= 0 || !result.StopTimes[1].Start.After(event.ArriveTime.Time) {
		t.Errorf("late start: broken_at %d (want %d), minutes_late %d, show at %v", result.BrokenAt, wantBroken, result.MinutesLate, result.StopTimes[1].Start)
	}
	// Show first, then the place: the show is on time, the place moves
	// after it and breaks the plan only if it is closed by then.
	decodeStrict(t, route(sandy, mixed, []string{event.ID, place.ID}).Expect(t, http.StatusOK), &result)
	if !result.StopTimes[0].Start.Equal(event.ArriveTime.Time) {
		t.Errorf("the show keeps its start: %v", result.StopTimes[0].Start)
	}
	wantBroken = -1
	if !opensFor(*place.ActivityID, result.StopTimes[1].Start.Time, result.StopTimes[1].End.Time) {
		wantBroken = 1
	}
	t.Logf("reordered: %s then %s → broken_at %d, minutes_late %d", event.Title, place.Title, result.BrokenAt, result.MinutesLate)
	if result.BrokenAt != wantBroken || (wantBroken == 1 && result.MinutesLate <= 0) {
		t.Errorf("reordered: broken_at %d (want %d), minutes_late %d, place at %v–%v",
			result.BrokenAt, wantBroken, result.MinutesLate, result.StopTimes[1].Start, result.StopTimes[1].End)
	}

	// Alternatives for the first stop, then a route with the swap.
	var alts []contract.PlanAlternative
	decodeStrict(t, srv.Do(t, "POST", "/plans/alternatives", contract.AlternativesRequest{OptionID: best.ID, StopID: best.Stops[0].ID, StopOrder: ids(best)}, sandy).Expect(t, http.StatusOK), &alts)
	if len(alts) == 0 {
		t.Fatal("no alternatives for the first stop")
	}
	for _, a := range alts {
		if !strings.HasPrefix(a.Stop.ID, "alt_") || !strings.HasPrefix(a.Reason, "Also ") || a.Stop.ActivityID == nil {
			t.Errorf("alternative %+v", a)
		}
		for _, s := range best.Stops {
			if *a.Stop.ActivityID == *s.ActivityID {
				t.Errorf("alternative %s repeats a stop in the plan", a.Stop.Title)
			}
		}
	}
	swapped := append([]string{alts[0].Stop.ID}, ids(best)[1:]...)
	decodeStrict(t, route(sandy, best, swapped).Expect(t, http.StatusOK), &result)
	if len(result.StopTimes) != len(swapped) {
		t.Errorf("route with the swap: %+v", result)
	}

	// ResolveStop (POST /itineraries' enrichment) on stop ids it returned.
	user, err := srv.Store.Users().ByID(ctx, sandy.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{best.Stops[0].ID, alts[0].Stop.ID} {
		d, err := svc.ResolveStop(ctx, user, best.ID, id)
		if err != nil || d.ActivityID == "" || d.DurationMin <= 0 {
			t.Errorf("resolve %s: %+v %v", id, d, err)
		}
	}
	if d, _ := svc.ResolveStop(ctx, user, best.ID, best.Stops[0].ID); d != nil && d.ActivityID != *best.Stops[0].ActivityID {
		t.Errorf("resolve names %s, the stop says %s", d.ActivityID, *best.Stops[0].ActivityID)
	}
	if _, err := svc.ResolveStop(ctx, user, best.ID, "stop_ffffffffffffffffffffffff_0"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown stop: %v", err)
	}

	// Someone else's option reads as expired; an unknown stop id is a 400.
	theo := srv.Signup(t, "Theo Park")
	if res := route(theo, best, ids(best)).Expect(t, http.StatusNotFound); res.Message() != planner.MsgPlanExpired {
		t.Errorf("foreign option: %q", res.Message())
	}
	if res := route(sandy, best, []string{"stop_ffffffffffffffffffffffff_0"}).Expect(t, http.StatusBadRequest); res.Message() != planner.MsgPlanChanged {
		t.Errorf("unknown stop: %q", res.Message())
	}

	// Nothing within reach: an empty batch with the reason.
	far := contract.Place{Name: "Marsh road", Coordinate: &contract.Coordinate{Lat: 31.368, Lng: -81.74}}
	var empty contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", planRequest(far, from, backBy), sandy).Expect(t, http.StatusOK), &empty)
	if len(empty.Options) != 0 || !empty.Done || empty.Reason == nil || *empty.Reason != "no_candidates_fit_window" || empty.Cursor != nil {
		t.Errorf("empty: %+v", empty)
	}
	// A window that already ended.
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", planRequest(seaside, from.Add(-24*time.Hour), backBy.Add(-24*time.Hour)), sandy).Expect(t, http.StatusOK), &empty)
	if empty.Reason == nil || *empty.Reason != "invalid_request: window already ended" || len(empty.Options) != 0 {
		t.Errorf("expired window: %+v", empty)
	}
}
