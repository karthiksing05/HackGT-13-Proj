package social_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Places around Midtown Atlanta (the app's default forum area).
var (
	midtown    = contract.Coordinate{Lat: 33.7838, Lng: -84.3833}
	techSquare = contract.Coordinate{Lat: 33.7766, Lng: -84.3890} // ~0.6 mi from Midtown
	downtown   = contract.Coordinate{Lat: 33.7621, Lng: -84.3833} // ~1.5 mi
	krog       = contract.Coordinate{Lat: 33.7571, Lng: -84.3640} // ~2.2 mi
	airport    = contract.Coordinate{Lat: 33.6407, Lng: -84.4277} // ~10 mi
)

var ny = mustZone("America/New_York")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// befriend makes a and b friends through the API (a asks, b accepts).
func befriend(t *testing.T, srv *testutil.Server, a, b *testutil.Session) {
	t.Helper()
	var req contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: b.UserID}, a).Expect(t, http.StatusCreated).JSON(t, &req)
	srv.Do(t, "POST", "/friends/requests/"+req.ID+"/accept", nil, b).Expect(t, http.StatusNoContent)
}

// setUser writes user fields directly (home base, status, …).
func setUser(t *testing.T, srv *testutil.Server, s *testutil.Session, set bson.M) {
	t.Helper()
	if _, err := srv.Store.Users().Update(context.Background(), s.UserID, set); err != nil {
		t.Fatal(err)
	}
}

func home(c contract.Coordinate) bson.M {
	return bson.M{"homeBase": models.HomeBase{Name: "Home", Lat: c.Lat, Lng: c.Lng}}
}

func f64(v float64) *float64 { return &v }

// plan builds an itinerary hosted by host starting at start (2 stops with a
// walk before each), open to everyone around at; mods adjust it.
func plan(host string, start time.Time, at contract.Coordinate, mods ...func(*models.Itinerary)) *models.Itinerary {
	lat, lng := at.Lat, at.Lng
	stopA := models.PlaceDoc{Name: "Stop A", Lat: &lat, Lng: &lng}
	stopB := models.PlaceDoc{Name: "Stop B"}
	it := &models.Itinerary{
		ID: store.NewID(), HostID: host, MemberIDs: []string{host}, Title: "Sunset walk",
		DateKey: start.In(ny).Format("2006-01-02"), TZ: "America/New_York",
		Date:  time.Date(start.In(ny).Year(), start.In(ny).Month(), start.In(ny).Day(), 0, 0, 0, 0, ny),
		Start: start, BackBy: start.Add(150 * time.Minute),
		StartPlace: models.PlaceDoc{Name: "Meet here", Lat: &lat, Lng: &lng}, EndPlace: models.PlaceDoc{Name: "Home"},
		Visibility: models.VisibilityOpen,
		Items: []models.ItineraryItem{
			{ID: store.NewID(), Kind: models.ItemTransit, Title: "Walk to Stop A", Start: start, End: start.Add(15 * time.Minute), LegMode: "walk", LegMinutes: 15},
			{ID: store.NewID(), Kind: models.ItemStop, Title: "Stop A", Place: &stopA, Start: start.Add(15 * time.Minute), End: start.Add(75 * time.Minute), Description: "First stop."},
			{ID: store.NewID(), Kind: models.ItemTransit, Title: "Walk to Stop B", Start: start.Add(75 * time.Minute), End: start.Add(90 * time.Minute), LegMode: "walk", LegMinutes: 15},
			{ID: store.NewID(), Kind: models.ItemStop, Title: "Stop B", Place: &stopB, Start: start.Add(90 * time.Minute), End: start.Add(150 * time.Minute)},
		},
		Plan:   &models.PlanSnapshot{Range: "walkable", Ride: "none", Budget: 1, Who: "friends", Pace: "balanced", Tags: []string{"Outdoors", "Food"}},
		Status: models.ItineraryActive, CreatedAt: start.Add(-2 * time.Hour), UpdatedAt: start.Add(-2 * time.Hour),
	}
	for _, mod := range mods {
		mod(it)
	}
	return it
}

func insertPlan(t *testing.T, srv *testutil.Server, it *models.Itinerary) *models.Itinerary {
	t.Helper()
	if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	return it
}

func loadPlan(t *testing.T, srv *testutil.Server, id string) *models.Itinerary {
	t.Helper()
	plans, err := srv.Store.Forum().Plans(context.Background(), []string{id})
	if err != nil || plans[id] == nil {
		t.Fatalf("plan %s: %v", id, err)
	}
	return plans[id]
}

// group makes a group thread over a plan with the given members (the host first).
func group(t *testing.T, srv *testutil.Server, members ...*testutil.Session) *models.Thread {
	t.Helper()
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	it := insertPlan(t, srv, plan(ids[0], srv.Clock.Now().Add(-48*time.Hour), midtown, func(it *models.Itinerary) {
		it.MemberIDs = ids
		it.Title = "Krog St dinner crew"
		it.Status = models.ItineraryPast
	}))
	th, _, err := srv.Store.Threads().EnsureGroup(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// addCard stores a simulated card directly (payments belong to backend-A).
func addCard(t *testing.T, srv *testutil.Server, s *testutil.Session, brand, last4 string, isDefault bool) *models.PaymentMethod {
	t.Helper()
	card := &models.PaymentMethod{ID: store.NewID(), UserID: s.UserID, Brand: brand, Last4: last4, IsDefault: isDefault,
		DemoToken: "tok_" + last4, CreatedAt: srv.Clock.Now()}
	if _, err := srv.Store.Collection(store.CollPaymentMethods).InsertOne(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	return card
}

// events of one type addressed to a user, decoded as the app would.
func eventsFor[T any](t *testing.T, srv *testutil.Server, userID, typ string) []T {
	t.Helper()
	var out []T
	for _, e := range srv.Events.For(userID) {
		if e.Type == typ {
			var v T
			testutil.EventData(t, e, &v)
			out = append(out, v)
		}
	}
	return out
}

// canonical re-encodes JSON with sorted keys so two documents compare by value.
func canonical(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("canonical %s: %v", raw, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// broadcasts counts recorded broadcasts of a type.
func broadcasts(srv *testutil.Server, typ string) int {
	n := 0
	for _, e := range srv.Events.Events() {
		if e.Broadcast && e.Type == typ {
			n++
		}
	}
	return n
}

// direct counts one user's own (non-broadcast) events of a type.
func direct(srv *testutil.Server, userID, typ string) int {
	n := 0
	for _, e := range srv.Events.Events() {
		if !e.Broadcast && e.UserID == userID && e.Type == typ {
			n++
		}
	}
	return n
}
