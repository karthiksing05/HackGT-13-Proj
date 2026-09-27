package planner_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// calendarServer is the planning HTTP surface over the real planner with
// the calendar wired (as wire.Planner does), and Sandy as the demo seed
// leaves her.
func calendarServer(t *testing.T) (*testutil.Server, *testutil.Session) {
	t.Helper()
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 26 Sep 2026, noon in New York
	ctx := context.Background()
	db := srv.Store.DB()
	acts := seedDemoCatalog(t, db)
	profile := profileVector(t, acts)
	fake := &fakeML{search: profile}
	mlSrv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(mlSrv.Close)
	cfg := planner.DefaultConfig()
	cfg.PoolTTL, cfg.RunTTL = 24*365*time.Hour, 24*365*time.Hour
	st := mongosource.New(db, srv.Clock)
	if err := st.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	scorer := planner.NewMLScorer(ml.NewClient(mlSrv.URL))
	p, err := planner.New(cfg, planner.Deps{Source: st, Embeddings: st, Lookup: st, Pools: st, Calendar: st, Scorer: scorer, Search: scorer, Clock: srv.Clock})
	if err != nil {
		t.Fatal(err)
	}
	srv.Deps.Planner = planner.NewService(p)
	sandy := srv.Signup(t, "Sandy Byte")
	if _, err := srv.Store.Users().Update(ctx, sandy.UserID, bson.M{
		"email": testutil.UniqueEmail("demo"), "city": "saltlight", "roles": []string{"demo"},
		"homeBase":          models.HomeBase{Name: seaside.Name, Lat: seaside.Coordinate.Lat, Lng: seaside.Coordinate.Lng},
		"positiveEmbedding": profile, "positiveText": "Interests:\n- outdoor recreation\n- live music",
	}); err != nil {
		t.Fatal(err)
	}
	return srv, sandy
}

func addBusy(t *testing.T, srv *testutil.Server, userID, title string, start, end time.Time) {
	t.Helper()
	if err := srv.Store.CalendarEvents().Insert(context.Background(), &models.CalendarEvent{
		UserID: userID, Title: title, Start: start, End: end, Source: "seed", Seed: "calendar-v1",
	}); err != nil {
		t.Fatal(err)
	}
}

// allPages is a run's options, every page.
func allPages(t *testing.T, srv *testutil.Server, sess *testutil.Session, req contract.PlanRequest) (contract.PlanBatch, []contract.PlanOption) {
	t.Helper()
	var first contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", req, sess).Expect(t, http.StatusOK), &first)
	all := append([]contract.PlanOption(nil), first.Options...)
	for cursor := first.Cursor; cursor != nil; {
		var page contract.PlanBatch
		decodeStrict(t, srv.Do(t, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: *cursor}, sess).Expect(t, http.StatusOK), &page)
		all = append(all, page.Options...)
		cursor = page.Cursor
	}
	return first, all
}

// onTop lists the stops of options that overlap [from, to).
func onTop(opts []contract.PlanOption, from, to time.Time) []string {
	var out []string
	for _, o := range opts {
		for _, s := range o.Stops {
			if s.ArriveTime.Before(to) && s.DepartTime.After(from) {
				out = append(out, o.ID+" "+s.Title)
			}
		}
	}
	return out
}

func TestPlansWorkAroundTheCalendar(t *testing.T) {
	srv, sandy := calendarServer(t)
	ctx := context.Background()
	from := time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC) // 18:00–23:00 in Saltlight
	backBy := from.Add(5 * time.Hour)
	classFrom, classTo := from.Add(90*time.Minute), from.Add(150*time.Minute) // 19:30–20:30
	// Someone else's whole evening is busy: it never reaches Sandy's plans.
	theo := srv.Signup(t, "Theo Park")
	addBusy(t, srv, theo.UserID, "Theo's shift", from.Add(-time.Hour), backBy.Add(time.Hour))

	_, free := allPages(t, srv, sandy, planRequest(seaside, from, backBy))
	if len(free) == 0 || len(onTop(free, classFrom, classTo)) == 0 {
		t.Fatalf("with a free evening no option uses 19:30–20:30 (%d options); the test no longer bites", len(free))
	}

	// A class in the middle of the window: no option puts a stop on it.
	addBusy(t, srv, sandy.UserID, "CS 3510 lecture", classFrom, classTo)
	first, busy := allPages(t, srv, sandy, planRequest(seaside, from, backBy))
	if len(busy) == 0 || first.Reason != nil {
		t.Fatalf("around the class: %d options, reason %v", len(busy), first.Reason)
	}
	if clash := onTop(busy, classFrom, classTo); len(clash) > 0 {
		t.Errorf("stops on top of the class: %v", clash)
	}
	around := 0
	for _, o := range busy {
		if o.Stops[0].ArriveTime.Before(classFrom) && !o.Stops[len(o.Stops)-1].ArriveTime.Before(classTo) {
			around++
		}
	}
	t.Logf("%d options around the class (%d with stops on both sides); best %q", len(busy), around, busy[0].Name)

	// The run knew the class and no option failed the guarantees (which
	// check the legs too).
	runID, _ := planner.RunIDFromOption(busy[0].ID)
	var run planner.PlanRun
	if err := srv.Store.DB().Collection(mongosource.RunsCollection).FindOne(ctx, bson.M{"_id": runID}).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if len(run.Spec.Busy) != 1 || !run.Spec.Busy[0].From.Equal(classFrom) || run.Final.Rejected != 0 {
		t.Errorf("run: busy %v, rejected %d", run.Spec.Busy, run.Final.Rejected)
	}

	// Re-routed as generated, nothing moves and nothing breaks.
	best := busy[0]
	order := make([]string, len(best.Stops))
	for i, s := range best.Stops {
		order[i] = s.ID
	}
	var route contract.RouteResult
	decodeStrict(t, srv.Do(t, "POST", "/plans/route", contract.RouteRequest{OptionID: best.ID, StopOrder: order, Start: seaside, End: seaside,
		StartTime: contract.NewTime(from), BackBy: contract.NewTime(backBy), Ride: contract.RideNone,
		Modes: contract.TravelModes{contract.ModeWalk}}, sandy).Expect(t, http.StatusOK), &route)
	if route.BrokenAt != -1 || route.MinutesLate != 0 {
		t.Errorf("route: %+v", route)
	}
	for i, w := range route.StopTimes {
		if !w.Start.Equal(best.Stops[i].ArriveTime.Time) || (w.Start.Before(classTo) && w.End.After(classFrom)) {
			t.Errorf("stop %d re-timed to %v–%v", i, w.Start, w.End)
		}
	}

	// A window the calendar fills: an empty batch the app has a sentence for.
	tomorrow := from.Add(16 * time.Hour) // Sunday 10:00 in Saltlight
	addBusy(t, srv, sandy.UserID, "Brunch shift", tomorrow.Add(-30*time.Minute), tomorrow.Add(80*time.Minute))
	addBusy(t, srv, sandy.UserID, "Study group", tomorrow.Add(90*time.Minute), tomorrow.Add(3*time.Hour))
	var full contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", planRequest(seaside, tomorrow, tomorrow.Add(2*time.Hour)), sandy).Expect(t, http.StatusOK), &full)
	if len(full.Options) != 0 || !full.Done || full.Reason == nil || *full.Reason != planner.ReasonCalendarFull {
		t.Errorf("a full calendar: %+v", full)
	}
	// Theo, whose shift is his own, plans that window.
	if _, err := srv.Store.Users().Update(ctx, theo.UserID, bson.M{"city": "saltlight", "roles": []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	var theirs contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", planRequest(seaside, tomorrow, tomorrow.Add(2*time.Hour)), theo).Expect(t, http.StatusOK), &theirs)
	if theirs.Reason != nil && *theirs.Reason == planner.ReasonCalendarFull {
		t.Errorf("Theo's plans read Sandy's calendar: %+v", theirs)
	}
}
