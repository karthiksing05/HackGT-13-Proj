package planner

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func sandyModel() *models.User {
	id, _ := bson.ObjectIDFromHex("5f00000000000000000000aa")
	birth := time.Date(2003, 6, 14, 0, 0, 0, 0, time.UTC)
	return &models.User{
		ID: id, Catalog: "demo_activities", City: "saltlight", BirthDate: &birth,
		HomeBase:          &models.HomeBase{Name: "Seaside Market Square", Lat: seasideMkt.Lat, Lng: seasideMkt.Lng},
		Prefs:             models.UserPrefs{Company: "small_group", Pace: "balanced", Flexibility: "bit_over_ok", PreferFree: true},
		PositiveEmbedding: sandyPositive, NegativeEmbedding: vectorOf("cat:nightclub", "high_energy"),
		PositiveText: "Interests:\n- live music",
	}
}

func appPlanRequest(t *testing.T, o reqOpts) contract.PlanRequest {
	t.Helper()
	var req contract.PlanRequest
	if err := json.Unmarshal(appRequestJSON(o), &req); err != nil {
		t.Fatal(err)
	}
	return req
}

// strictDecode fails on any key the contract type does not define.
func strictDecode(t *testing.T, v, into any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("not the contract shape: %v\n%s", err, raw)
	}
}

// keysOf collects the keys of every object at path (options, options.stops).
func keysOf(t *testing.T, doc any, path ...string) map[string]bool {
	t.Helper()
	raw, _ := json.Marshal(doc)
	var v any
	_ = json.Unmarshal(raw, &v)
	objs := []any{v}
	for _, p := range path {
		var next []any
		for _, o := range objs {
			m, _ := o.(map[string]any)
			if arr, ok := m[p].([]any); ok {
				next = append(next, arr...)
			}
		}
		objs = next
	}
	out := map[string]bool{}
	for _, o := range objs {
		for k := range o.(map[string]any) {
			out[k] = true
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestServiceGenerateSpeaksTheContract(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	svc := NewService(tp.Planner)
	o := defaultReq()
	o.tags, o.mood = []string{"Outdoors", "Food"}, "something chill outside, then food"
	batch, err := svc.Generate(t.Context(), sandyModel(), appPlanRequest(t, o), ny)
	if err != nil {
		t.Fatal(err)
	}
	var back contract.PlanBatch
	strictDecode(t, batch, &back)
	if len(batch.Options) != 3 || batch.Done || batch.Cursor == nil || batch.Reason != nil {
		t.Fatalf("first page: %d options, done %v, cursor %v, reason %v", len(batch.Options), batch.Done, batch.Cursor, batch.Reason)
	}
	// Every key we write is one the app's own example documents.
	raw, err := os.ReadFile("../../../docs/api/examples/PlanBatch.dag.json")
	if err != nil {
		t.Skipf("contract example missing: %v", err)
	}
	var example any
	_ = json.Unmarshal(raw, &example)
	for _, path := range [][]string{{"options"}, {"options", "stops"}} {
		ours, theirs := keysOf(t, batch, path...), keysOf(t, example, path...)
		for k := range ours {
			if !theirs[k] {
				t.Errorf("%s: key %q is not in PlanBatch.dag.json (%v)", strings.Join(path, "."), k, sortedKeys(theirs))
			}
		}
	}
	for _, opt := range batch.Options {
		known := false
		for _, s := range opt.Stops {
			if s.ArriveTime == nil || s.DepartTime == nil || !s.Kind.Valid() || s.Flexible == nil || s.ActivityID == nil {
				t.Errorf("stop %s lacks its planner extras", s.ID)
			}
			if s.Place.Coordinate == nil || s.DurationMinutes <= 0 || s.Title == "" || s.Subtitle == "" {
				t.Errorf("stop %s: %+v", s.ID, s)
			}
		}
		runID, _ := RunIDFromOption(opt.ID)
		pool := tp.pool(t, runID)
		for _, po := range pool.Options {
			if po.ID == opt.ID {
				for _, s := range po.Stops {
					known = known || s.PriceKnown
				}
			}
		}
		if known != (opt.TotalCostCents != nil) {
			t.Errorf("%s: total_cost_cents %v with known prices %v", opt.ID, opt.TotalCostCents, known)
		}
	}

	// The next page, then an unknown cursor: the last, empty page.
	more, err := svc.More(t.Context(), sandyModel(), *batch.Cursor)
	if err != nil || len(more.Options) != 2 {
		t.Fatalf("more: %+v %v", more, err)
	}
	strictDecode(t, more, &back)
	empty, err := svc.More(t.Context(), sandyModel(), "batch-0")
	if err != nil || len(empty.Options) != 0 || !empty.Done || empty.Options == nil {
		t.Errorf("unknown cursor: %+v %v", empty, err)
	}
}

func TestServiceInvalidAndEmptyRequests(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	svc := NewService(tp.Planner)
	past := defaultReq()
	past.from, past.backBy = localAt(8, 0), localAt(10, 0) // the clock says noon
	batch, err := svc.Generate(t.Context(), sandyModel(), appPlanRequest(t, past), ny)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Reason == nil || *batch.Reason != "invalid_request: window already ended" || len(batch.Options) != 0 || batch.Options == nil || !batch.Done {
		t.Errorf("expired window: %+v", batch)
	}
	var back contract.PlanBatch
	strictDecode(t, batch, &back)

	far := defaultReq()
	far.start = offsetKm(seasideMkt, -30, 0) // inside the city's 60 km, 20+ km from every activity
	far.end = far.start
	batch, err = svc.Generate(t.Context(), sandyModel(), appPlanRequest(t, far), ny)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Reason == nil || *batch.Reason != "no_candidates_fit_window" || len(batch.Options) != 0 || !batch.Done || batch.Cursor != nil {
		t.Errorf("nothing in range: %+v", batch)
	}
}

func TestServiceRouteAlternativesAndErrors(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	svc := NewService(tp.Planner)
	user := sandyModel()
	o := defaultReq()
	o.tags, o.mood = []string{"Outdoors", "Food"}, "something chill outside, then food"
	batch, err := svc.Generate(t.Context(), user, appPlanRequest(t, o), ny)
	if err != nil {
		t.Fatal(err)
	}
	opt := batch.Options[0]
	order := make([]string, len(opt.Stops))
	for i, s := range opt.Stops {
		order[i] = s.ID
	}
	req := contract.RouteRequest{OptionID: opt.ID, StopOrder: order,
		Start:     contract.Place{Name: "Start", Coordinate: &contract.Coordinate{Lat: seasideMkt.Lat, Lng: seasideMkt.Lng}},
		End:       contract.Place{Name: "End", Coordinate: &contract.Coordinate{Lat: seasideMkt.Lat, Lng: seasideMkt.Lng}},
		StartTime: contract.NewTime(localAt(18, 0)), BackBy: contract.NewTime(localAt(23, 0)),
		Ride: contract.RideNone, Modes: contract.TravelModes{contract.ModeWalk}}
	res, err := svc.Route(t.Context(), user, req)
	if err != nil {
		t.Fatal(err)
	}
	var backRoute contract.RouteResult
	strictDecode(t, res, &backRoute)
	if res.BrokenAt != -1 || res.MinutesLate != 0 || len(res.Legs) != len(order)+1 || len(res.StopTimes) != len(order) {
		t.Errorf("same order: %+v", res)
	}
	for i, st := range res.StopTimes {
		if !st.Start.Equal(opt.Stops[i].ArriveTime.Time) || !st.End.Equal(opt.Stops[i].DepartTime.Time) {
			t.Errorf("stop %d re-timed: %v–%v", i, st.Start, st.End)
		}
	}
	// Reversed: the oyster bar lands after its 23:00 close (the golden case),
	// folded into broken_at and minutes_late.
	req.StopOrder = reversed(order)
	res, err = svc.Route(t.Context(), user, req)
	if err != nil || res.BrokenAt != 1 || res.MinutesLate != 94 {
		t.Errorf("reversed: %+v %v", res, err)
	}

	alts, err := svc.Alternatives(t.Context(), user, contract.AlternativesRequest{OptionID: opt.ID, StopID: order[0], StopOrder: order})
	if err != nil || len(alts) == 0 {
		t.Fatalf("alternatives: %v %d", err, len(alts))
	}
	var backAlts []contract.PlanAlternative
	strictDecode(t, alts, &backAlts)
	for _, a := range alts {
		if !strings.HasPrefix(a.Stop.ID, "alt_") || a.Reason == "" || a.Stop.ActivityID == nil {
			t.Errorf("alternative %+v", a)
		}
	}

	status := func(err error) (int, string) {
		var se *httpx.StatusError
		if !errors.As(err, &se) {
			return 0, ""
		}
		return se.Status, se.Message
	}
	bad := req
	bad.StopOrder = []string{"stop_ffffffffffffffffffffffff_0"}
	if code, msg := status(func() error { _, err := svc.Route(t.Context(), user, bad); return err }()); code != http.StatusBadRequest || msg != MsgPlanChanged {
		t.Errorf("unknown stop: %d %q", code, msg)
	}
	bad = req
	bad.OptionID = "opt-a"
	if code, msg := status(func() error { _, err := svc.Route(t.Context(), user, bad); return err }()); code != http.StatusNotFound || msg != MsgPlanExpired {
		t.Errorf("unknown option: %d %q", code, msg)
	}
	other := sandyModel()
	other.ID, _ = bson.ObjectIDFromHex("5f00000000000000000000bb")
	if code, _ := status(func() error { _, err := svc.Route(t.Context(), other, req); return err }()); code != http.StatusNotFound {
		t.Errorf("another user's option: %d", code)
	}
	if code, _ := status(func() error {
		_, err := svc.Alternatives(t.Context(), other, contract.AlternativesRequest{OptionID: opt.ID, StopID: order[0], StopOrder: order})
		return err
	}()); code != http.StatusNotFound {
		t.Errorf("another user's alternatives: %d", code)
	}
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)
	if code, msg := status(func() error { _, err := svc.Route(t.Context(), user, req); return err }()); code != http.StatusNotFound || msg != MsgPlanExpired {
		t.Errorf("expired: %d %q", code, msg)
	}
}

func TestServiceResolveStop(t *testing.T) {
	tp := newTestPlanner(saltlight(t), testConfig())
	svc := NewService(tp.Planner)
	o := defaultReq()
	batch, err := svc.Generate(t.Context(), sandyModel(), appPlanRequest(t, o), ny)
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := RunIDFromOption(batch.Options[0].ID)
	pool := tp.pool(t, runID)
	var stop Stop
	for _, s := range pool.Options[0].Stops {
		if s.PriceKnown {
			stop = s
			break
		}
	}
	if stop.ID == "" {
		stop = pool.Options[0].Stops[0]
	}
	d, err := svc.ResolveStop(t.Context(), stop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.ActivityID != stop.ActivityID || d.DurationMin != stop.DurationMinutes || (d.PriceCents == nil) == stop.PriceKnown {
		t.Errorf("from the pool: %+v vs %+v", d, stop)
	}
	if stop.WebsiteURL != "" && (d.WebsiteURL == nil || *d.WebsiteURL != stop.WebsiteURL) {
		t.Errorf("website %v", d.WebsiteURL)
	}
	// Once the pool expired, the activity the id names still answers.
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)
	d, err = svc.ResolveStop(t.Context(), stop.ID)
	if err != nil || d.ActivityID != stop.ActivityID {
		t.Errorf("from the catalog: %+v %v", d, err)
	}
	for _, id := range []string{"opt-a-0", "stop_ffffffffffffffffffffffff_0", ""} {
		if _, err := svc.ResolveStop(t.Context(), id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestUserFromModel(t *testing.T) {
	now := time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)
	u := sandyModel()
	u.LastLocation = &models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-81.43, 31.38}}
	u.Taste.AvoidTags = []string{"touristy"}
	u.Embedding = []float64{9, 9, 9} // the legacy field: never read
	uc := UserFromModel(u, now)
	if uc.ID != u.ID.Hex() || uc.Catalog != "demo_activities" || uc.City != "saltlight" || uc.AgeBracket != "adult" {
		t.Errorf("user %+v", uc)
	}
	if uc.HomeBase == nil || !uc.HomeBase.HasCoord || uc.HomeBase.Name != "Seaside Market Square" {
		t.Errorf("home base %+v", uc.HomeBase)
	}
	if uc.LastLocation == nil || *uc.LastLocation != (travel.Point{Lat: 31.38, Lng: -81.43}) {
		t.Errorf("last location %+v", uc.LastLocation)
	}
	if !uc.Prefs.Flexible || uc.Prefs.Pace != "balanced" || len(uc.Prefs.AvoidTags) != 1 || uc.PositiveText == "" {
		t.Errorf("prefs %+v", uc.Prefs)
	}
	if len(uc.PositiveEmbedding) != testDim || len(uc.NegativeEmbedding) != testDim {
		t.Error("the profile vectors pass through")
	}
	for birth, want := range map[time.Time]string{
		time.Date(2007, 1, 1, 0, 0, 0, 0, time.UTC): "18_20",
		time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC): "13_17",
		time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC): "13_17",
	} {
		b := birth
		u.BirthDate = &b
		if got := NormalizeAgeBracket(UserFromModel(u, now).AgeBracket); got != want {
			t.Errorf("born %s: %s, want %s", birth.Format("2006"), got, want)
		}
	}
	if uc := UserFromModel(nil, now); uc.ID != "" || NormalizeAgeBracket(uc.AgeBracket) != "21_plus" {
		t.Errorf("nil user %+v", uc)
	}
}
