package itineraries_test

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// examplesDir holds the app's contract dumps (docs/api/examples).
const examplesDir = "../../../../docs/api/examples"

// exampleClock is Friday 2026-09-25 8 AM in New York, before the example plan.
var exampleClock = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// ny is the X-Time-Zone every testutil request carries.
var ny = mustZone(testutil.TimeZone)

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at is a New York wall-clock time.
func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, ny)
}

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(examplesDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// exampleRequest is docs/api/examples/CreateItineraryRequest.json without its
// invite_user_ids: those are the demo's people, nobody's friends here (tests
// that bring friends add their own).
func exampleRequest(t *testing.T) contract.CreateItineraryRequest {
	t.Helper()
	var req contract.CreateItineraryRequest
	if err := json.Unmarshal(readExample(t, "CreateItineraryRequest"), &req); err != nil {
		t.Fatal(err)
	}
	req.InviteUserIDs = nil
	return req
}

func create(t *testing.T, srv *testutil.Server, sess *testutil.Session, req any) contract.Itinerary {
	t.Helper()
	var it contract.Itinerary
	srv.Do(t, "POST", "/itineraries", req, sess).Expect(t, http.StatusCreated).JSON(t, &it)
	return it
}

// stopSpec is one stop of a hand-made plan.
type stopSpec struct {
	id, title string
	lat, lng  *float64
	start     time.Time
	minutes   int
}

func coord(lat, lng float64) (*float64, *float64) { return &lat, &lng }

// plan is a Create request for stops at fixed times: a 10-minute walk
// before each stop and one back to the end.
func plan(name string, visibility contract.Visibility, stops ...stopSpec) contract.CreateItineraryRequest {
	start := stops[0].start.Add(-10 * time.Minute)
	last := stops[len(stops)-1]
	back := last.start.Add(time.Duration(last.minutes+60) * time.Minute)
	req := contract.CreateItineraryRequest{
		Plan: contract.PlanRequest{
			Start: contract.Place{Name: "Home"}, End: contract.Place{Name: "Home"},
			Date: contract.NewTime(start), StartTime: contract.NewTime(start), BackBy: contract.NewTime(back),
			Range: contract.RangeWalkable, Ride: contract.RideNone, Budget: 1, Who: contract.Visibility(visibility), Pace: contract.PaceBalanced,
		},
		Option:     contract.PlanOption{ID: "opt-" + stops[0].id, Name: name, Tag: "Best match"},
		Visibility: visibility,
		Route:      contract.RouteResult{BrokenAt: -1},
	}
	for _, s := range stops {
		place := contract.Place{Name: s.title}
		if s.lat != nil {
			place.Coordinate = &contract.Coordinate{Lat: *s.lat, Lng: *s.lng}
		}
		req.Option.Stops = append(req.Option.Stops, contract.PlanStop{ID: s.id, Title: s.title, Subtitle: "Stop", Place: place, DurationMinutes: s.minutes})
		req.StopOrder = append(req.StopOrder, s.id)
		req.Route.Legs = append(req.Route.Legs, contract.Leg{Mode: contract.ModeWalk, Minutes: 10})
		req.Route.StopTimes = append(req.Route.StopTimes, contract.StopWindow{
			Start: contract.NewTime(s.start), End: contract.NewTime(s.start.Add(time.Duration(s.minutes) * time.Minute)),
		})
	}
	req.Route.Legs = append(req.Route.Legs, contract.Leg{Mode: contract.ModeWalk, Minutes: 10})
	req.Route.Arrival = contract.NewTime(back)
	return req
}

// addMember puts sess on an itinerary directly (joins belong to the social area).
func addMember(t *testing.T, srv *testutil.Server, itineraryID string, sess *testutil.Session) {
	t.Helper()
	if _, err := srv.Store.Itineraries().AddMember(context.Background(), itineraryID, sess.UserID); err != nil {
		t.Fatal(err)
	}
}

// stops are the non-transit items.
func stops(it contract.Itinerary) []contract.ItineraryItem {
	var out []contract.ItineraryItem
	for _, item := range it.Items {
		if item.Kind != contract.KindTransit {
			out = append(out, item)
		}
	}
	return out
}

func getItinerary(t *testing.T, srv *testutil.Server, sess *testutil.Session, id string) contract.Itinerary {
	t.Helper()
	var it contract.Itinerary
	srv.Do(t, "GET", "/itineraries/"+id, nil, sess).Expect(t, http.StatusOK).JSON(t, &it)
	return it
}

func event(t *testing.T, srv *testutil.Server, sess *testutil.Session, itemID string) contract.ItineraryItem {
	t.Helper()
	var item contract.ItineraryItem
	srv.Do(t, "GET", "/events/"+itemID, nil, sess).Expect(t, http.StatusOK).JSON(t, &item)
	return item
}

// itineraryEvents is the itinerary.updated payloads a user received.
func itineraryEvents(t *testing.T, srv *testutil.Server, userID, typ string) []contract.Itinerary {
	t.Helper()
	var out []contract.Itinerary
	for _, e := range srv.Events.Of(typ) {
		if e.UserID != userID {
			continue
		}
		var it contract.Itinerary
		testutil.EventData(t, e, &it)
		out = append(out, it)
	}
	return out
}

func forumUpdates(srv *testutil.Server) int { return len(srv.Events.Of("forum.update")) }

// fakePlanner answers ResolveStop from a table; nothing else is used here.
type fakePlanner struct{ details map[string]*api.StopDetail }

var errUnused = errors.New("not used by these tests")

func (fakePlanner) Generate(context.Context, *models.User, contract.PlanRequest, *time.Location) (contract.PlanBatch, error) {
	return contract.PlanBatch{}, errUnused
}
func (fakePlanner) More(context.Context, *models.User, string) (contract.PlanBatch, error) {
	return contract.PlanBatch{}, errUnused
}
func (fakePlanner) Route(context.Context, *models.User, contract.RouteRequest) (contract.RouteResult, error) {
	return contract.RouteResult{}, errUnused
}
func (fakePlanner) Alternatives(context.Context, *models.User, contract.AlternativesRequest) ([]contract.PlanAlternative, error) {
	return nil, errUnused
}
func (p fakePlanner) ResolveStop(_ context.Context, _ *models.User, _, stopID string) (*api.StopDetail, error) {
	if d, ok := p.details[stopID]; ok {
		return d, nil
	}
	return nil, errors.New("unknown stop " + stopID)
}

// otherActivityCollection is a catalog signed-up (non-demo) accounts never read.
func otherActivityCollection() string { return store.CollDemoActivities }
