package api_test

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// stubProbe answers GET on a route registered with api.Stub, so the 501
// shape stays pinned however many area routes are built.
func stubProbe(t *testing.T, srv *testutil.Server, sess *testutil.Session) *testutil.Response {
	t.Helper()
	r := mux.NewRouter()
	api.Stub(r, srv.Deps, "GET", "/stub-probe", true)
	req := httptest.NewRequest("GET", "/stub-probe", nil)
	if sess != nil {
		req.Header.Set("Authorization", sess.Bearer())
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return &testutil.Response{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}
}

// scopePlan is a one-stop plan starting two hours from the server's clock.
func scopePlan(srv *testutil.Server) contract.CreateItineraryRequest {
	start := srv.Clock.Now().Add(2 * time.Hour).Truncate(time.Minute)
	arrive, leave := start.Add(10*time.Minute), start.Add(70*time.Minute)
	return contract.CreateItineraryRequest{
		Plan: contract.PlanRequest{
			Start: contract.Place{Name: "Home"}, End: contract.Place{Name: "Home"},
			Date: contract.NewTime(start), StartTime: contract.NewTime(start), BackBy: contract.NewTime(start.Add(3 * time.Hour)),
			Range: contract.RangeWalkable, Ride: contract.RideNone, Budget: 1, Who: contract.VisibilityFriends, Pace: contract.PaceBalanced,
		},
		Option: contract.PlanOption{ID: "opt-scope", Name: "Scope plan", Tag: "Best match", Stops: []contract.PlanStop{
			{ID: "s0", Title: "Scope Park", Subtitle: "Park · Free", Place: contract.Place{Name: "Scope Park"}, DurationMinutes: 60},
		}},
		StopOrder: []string{"s0"},
		Route: contract.RouteResult{
			Legs:      []contract.Leg{{Mode: contract.ModeWalk, Minutes: 10}, {Mode: contract.ModeWalk, Minutes: 10}},
			StopTimes: []contract.StopWindow{{Start: contract.NewTime(arrive), End: contract.NewTime(leave)}},
			Arrival:   contract.NewTime(leave.Add(10 * time.Minute)), BrokenAt: -1,
		},
		Visibility: contract.VisibilityFriends,
	}
}

func createScopeItinerary(t *testing.T, srv *testutil.Server, a *testutil.Session) string {
	t.Helper()
	var it contract.Itinerary
	srv.Do(t, "POST", "/itineraries", scopePlan(srv), a).Expect(t, http.StatusCreated).JSON(t, &it)
	return it.ID
}

// createItineraryForA: A's plan is invisible to B.
func createItineraryForA(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
	return "GET", "/itineraries/" + createScopeItinerary(t, srv, a), nil
}

// joinAsMemberThenPatch: B is on A's plan but only the host may edit it.
func joinAsMemberThenPatch(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
	id := createScopeItinerary(t, srv, a)
	others, err := srv.Store.Users().Search(t.Context(), "Bob Scope", a.UserID, 1)
	if err != nil || len(others) != 1 {
		t.Fatalf("find B: %v %d", err, len(others))
	}
	if _, err := srv.Store.Itineraries().AddMember(t.Context(), id, others[0].ID.Hex()); err != nil {
		t.Fatal(err)
	}
	return "PATCH", "/itineraries/" + id, map[string]string{"title": "Mine now"}
}
