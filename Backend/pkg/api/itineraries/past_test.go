package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// pastClock is Saturday 2026-09-26 noon in New York.
var pastClock = time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

// pastStop is one ended stop of MockData.pastEvents().
type pastStop struct {
	id, title, place string
	start            time.Time
	company          []*testutil.Session // besides the host
}

// insertPast stores a one-stop itinerary (a walk, then the stop) for host
// and company, as it would look after the day.
func insertPast(t *testing.T, srv *testutil.Server, host *testutil.Session, s pastStop) {
	t.Helper()
	members := []string{host.UserID}
	for _, c := range s.company {
		members = append(members, c.UserID)
	}
	lat, lng := 33.77, -84.39
	local := s.start.In(ny)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, ny)
	doc := &models.Itinerary{
		HostID: host.UserID, MemberIDs: members, Title: s.title, DateKey: day.Format("2006-01-02"), TZ: testutil.TimeZone,
		Date: day, Start: s.start.Add(-15 * time.Minute), BackBy: s.start.Add(2 * time.Hour),
		StartPlace: models.PlaceDoc{Name: "Home", Lat: &lat, Lng: &lng}, EndPlace: models.PlaceDoc{Name: "Home"},
		Visibility: models.VisibilityJustMe,
		Items: []models.ItineraryItem{
			{ID: "leg-" + s.id, Kind: models.ItemTransit, Title: "Walk to " + s.title, Place: &models.PlaceDoc{Name: s.place},
				Start: s.start.Add(-15 * time.Minute), End: s.start, LegMode: "walk", LegMinutes: 15},
			{ID: s.id, Kind: models.ItemStop, Title: s.title, Place: &models.PlaceDoc{Name: s.place}, Start: s.start, End: s.start.Add(time.Hour)},
		},
	}
	if err := srv.Store.Itineraries().Insert(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
}

func rate(t *testing.T, srv *testutil.Server, sess *testutil.Session, itemID string, stars int, tags ...string) {
	t.Helper()
	if tags == nil {
		tags = []string{}
	}
	srv.Do(t, "PUT", "/ratings/"+itemID, contract.Rating{Stars: stars, Tags: tags}, sess).Expect(t, http.StatusNoContent)
}

// canonicalJSON re-encodes JSON with sorted keys.
func canonicalJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestPastEventsAndInsightsMatchTheMock replays MockData.pastEvents() with
// its three ratings: GET /me/insights must equal docs/api/examples/PastInsights.json
// and the first past events PastEvents.json.
func TestPastEventsAndInsightsMatchTheMock(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(pastClock))
	jordan := srv.Signup(t, "Jordan Lee")
	var others []*testutil.Session
	for _, name := range []string{"Maya Past", "Dev Past", "Ava Past", "Sam Past"} {
		others = append(others, srv.Signup(t, name))
	}
	// A plan still under way is not past.
	insertPast(t, srv, jordan, pastStop{id: "now1", title: "Lunch in progress", place: "Midtown", start: pastClock.Add(-30 * time.Minute)})
	for _, s := range []pastStop{
		{"x1", "Board game café", "Tech Square", at(9, 24, 19, 0), others[:3]},
		{"x2", "Eastside Trail walk", "BeltLine", at(9, 24, 16, 0), nil},
		{"x3", "Jazz night in Decatur", "Decatur Square", at(9, 19, 20, 0), others[:2]},
		{"x4", "Dinner before the show", "Decatur", at(9, 19, 18, 0), others[:2]},
		{"x5", "Atlanta Food Walk", "Downtown", at(9, 12, 12, 0), nil},
		{"x6", "Chattahoochee paddle", "Chattahoochee River", at(9, 6, 10, 0), others},
	} {
		insertPast(t, srv, jordan, s)
	}
	rate(t, srv, jordan, "x3", 5, "Great people", "Would go again")
	rate(t, srv, jordan, "x5", 4, "Good value")
	rate(t, srv, jordan, "x6", 3, "Hard to get to")

	res := srv.Do(t, "GET", "/me/insights", nil, jordan).Expect(t, http.StatusOK)
	if got, want := canonicalJSON(t, res.Body), canonicalJSON(t, readExample(t, "PastInsights")); got != want {
		t.Fatalf("insights differ from the mock's\n got %s\nwant %s", got, want)
	}

	var page contract.Page[contract.PastEvent]
	srv.Do(t, "GET", "/me/past-events?unrated=false", nil, jordan).Expect(t, http.StatusOK).JSON(t, &page)
	if page.NextCursor != nil || len(page.Items) != 6 {
		t.Fatalf("past events: %d, cursor %v", len(page.Items), page.NextCursor)
	}
	for i, id := range []string{"x1", "x2", "x3", "x4", "x5", "x6"} {
		if page.Items[i].ID != id {
			t.Fatalf("order: item %d is %s, want %s", i, page.Items[i].ID, id)
		}
	}
	firstTwo, _ := json.Marshal(page.Items[:2])
	if got, want := canonicalJSON(t, firstTwo), canonicalJSON(t, readExample(t, "PastEvents")); got != want {
		t.Fatalf("past events differ from the example\n got %s\nwant %s", got, want)
	}
	x3, x6 := page.Items[2], page.Items[5]
	if x3.Company != "with 2 others" || x3.Kind != contract.KindGroup || x3.Rating == nil || x3.Rating.Stars != 5 || len(x3.Rating.Tags) != 2 ||
		x6.Company != "with 4 others" || x6.Rating == nil || x6.Rating.Stars != 3 {
		t.Fatalf("rated events: %+v %+v", x3, x6)
	}

	srv.Do(t, "GET", "/me/past-events?unrated=true", nil, jordan).Expect(t, http.StatusOK).JSON(t, &page)
	if len(page.Items) != 3 || page.Items[0].ID != "x1" || page.Items[1].ID != "x2" || page.Items[2].ID != "x4" {
		t.Fatalf("unrated: %+v", page.Items)
	}

	// Company members see the shared stops as theirs; the solo ones are not.
	srv.Do(t, "GET", "/me/past-events", nil, others[3]).Expect(t, http.StatusOK).JSON(t, &page)
	if len(page.Items) != 1 || page.Items[0].ID != "x6" || page.Items[0].Rating != nil {
		t.Fatalf("Sam's past events: %+v", page.Items)
	}
}

func TestInsightsNudgeWithoutFavorites(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(pastClock))
	a := srv.Signup(t, "Alice Nudge")
	nudge := `{"based_on":0,"headline":"` + itineraries.MsgInsightsNudge + `","highlights":[],"top_tags":[]}`
	res := srv.Do(t, "GET", "/me/insights", nil, a).Expect(t, http.StatusOK)
	if got := canonicalJSON(t, res.Body); got != canonicalJSON(t, []byte(nudge)) {
		t.Fatalf("no history: %s", got)
	}
	insertPast(t, srv, a, pastStop{id: "meh", title: "Okay museum", place: "Downtown", start: at(9, 20, 14, 0)})
	rate(t, srv, a, "meh", 3)
	res = srv.Do(t, "GET", "/me/insights", nil, a).Expect(t, http.StatusOK)
	if got := canonicalJSON(t, res.Body); got != canonicalJSON(t, []byte(nudge)) {
		t.Fatalf("nothing rated 4+: %s", got)
	}
}

func TestPastEventsPages(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(pastClock))
	a := srv.Signup(t, "Alice Pages")
	for i := 0; i < 55; i++ {
		insertPast(t, srv, a, pastStop{id: fmt.Sprintf("p%02d", i), title: fmt.Sprintf("Stop %d", i), place: "Somewhere",
			start: at(9, 20, 8, 0).Add(time.Duration(i) * 10 * time.Minute)})
	}
	var first, second contract.Page[contract.PastEvent]
	srv.Do(t, "GET", "/me/past-events", nil, a).Expect(t, http.StatusOK).JSON(t, &first)
	if len(first.Items) != 50 || first.NextCursor == nil || first.Items[0].ID != "p54" || first.Items[49].ID != "p05" {
		t.Fatalf("page 1: %d items, cursor %v", len(first.Items), first.NextCursor)
	}
	srv.Do(t, "GET", "/me/past-events?cursor="+*first.NextCursor, nil, a).Expect(t, http.StatusOK).JSON(t, &second)
	if len(second.Items) != 5 || second.NextCursor != nil || second.Items[0].ID != "p04" || second.Items[4].ID != "p00" {
		t.Fatalf("page 2: %+v cursor %v", second.Items, second.NextCursor)
	}
	var small contract.Page[contract.PastEvent]
	srv.Do(t, "GET", "/me/past-events?limit=20", nil, a).Expect(t, http.StatusOK).JSON(t, &small)
	if len(small.Items) != 20 || small.NextCursor == nil {
		t.Fatalf("limit 20: %d", len(small.Items))
	}
	srv.Do(t, "GET", "/me/past-events?cursor=bm9wZQ", nil, a).Expect(t, http.StatusBadRequest)
}

func tasteOf(t *testing.T, srv *testutil.Server, userID string) models.UserTaste {
	t.Helper()
	u, err := srv.Store.Users().ByID(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return u.Taste
}

func near(a, b float64) bool { return a-b < 1e-6 && b-a < 1e-6 }

// seedActivity puts one catalog activity in the test database's activities.
func seedActivity(t *testing.T, srv *testutil.Server, category string, tags ...string) string {
	t.Helper()
	id := bson.NewObjectID()
	summary := "A catalog stop"
	doc := models.Activity{ID: id, Kind: "place", City: "atlanta", Name: "Catalog " + category, Summary: &summary, Category: category, Tags: tags,
		Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-84.39, 33.77}}, Timezone: testutil.TimeZone}
	if _, err := srv.Store.Collection(store.CollActivities).InsertOne(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	return id.Hex()
}

// ratedPlan is a plan with one stop at start that points at activityID ("" for none).
func ratedPlan(t *testing.T, srv *testutil.Server, sess *testutil.Session, activityID string, start time.Time) string {
	t.Helper()
	req := plan("Rated", contract.VisibilityJustMe, stopSpec{id: "r0", title: "Rated stop", start: start, minutes: 60})
	if activityID != "" {
		req.Option.Stops[0].ActivityID = &activityID
	}
	return stops(create(t, srv, sess, req))[0].ID
}

func TestRatingsMoveTasteAndProfiles(t *testing.T) {
	profiles := &testutil.ProfilesRecorder{}
	srv := testutil.New(t, testutil.WithNow(pastClock), testutil.WithProfiles(profiles))
	a := srv.Signup(t, "Alice Rates")
	b := srv.Signup(t, "Bob Rates")
	hike := seedActivity(t, srv, "hike", "outdoors", "hiking")
	sunrise := ratedPlan(t, srv, a, hike, at(9, 27, 7, 30)) // before 9 AM: early_mornings too

	rate(t, srv, a, sunrise, 5)
	taste := tasteOf(t, srv, a.UserID)
	// No saved preferences: tags start at 0.5 and step 0.3 of the way to 1.
	for _, key := range []string{"outdoors", "long_walks", "early_mornings"} {
		if !near(taste.Tags[key], 0.65) {
			t.Errorf("taste %s = %v, want 0.65", key, taste.Tags[key])
		}
	}
	if len(taste.Tags) != 3 || taste.RatingCount != 1 {
		t.Fatalf("taste after one rating: %+v", taste)
	}
	calls := profiles.WaitRated(t, 1, time.Second)
	if calls[0].UserID != a.UserID || calls[0].ActivityID != hike || calls[0].Stars != 5 {
		t.Fatalf("Rated call: %+v", calls[0])
	}
	if got := event(t, srv, a, sunrise); got.Rating == nil || got.Rating.Stars != 5 || got.Rating.Tags == nil {
		t.Fatalf("rating on the item: %+v", got.Rating)
	}

	// Saving the same rating again changes nothing.
	rate(t, srv, a, sunrise, 5)
	time.Sleep(100 * time.Millisecond)
	if again := tasteOf(t, srv, a.UserID); !near(again.Tags["outdoors"], 0.65) || again.RatingCount != 1 || len(profiles.RatedCalls()) != 1 {
		t.Fatalf("re-saving moved the taste: %+v, %d calls", again, len(profiles.RatedCalls()))
	}

	// A change of heart steps toward the new stars; "Too crowded" pulls big_crowds down.
	note := "packed trail"
	srv.Do(t, "PUT", "/ratings/"+sunrise, contract.Rating{Stars: 1, Tags: []string{"Too crowded", " Too crowded "}, Note: &note}, a).Expect(t, http.StatusNoContent)
	taste = tasteOf(t, srv, a.UserID)
	if !near(taste.Tags["outdoors"], 0.455) || !near(taste.Tags["big_crowds"], 0.35) || taste.RatingCount != 1 {
		t.Fatalf("after re-rating: %+v", taste)
	}
	if calls := profiles.WaitRated(t, 2, time.Second); calls[1].Stars != 1 {
		t.Fatalf("second Rated call: %+v", calls[1])
	}
	if got := event(t, srv, a, sunrise).Rating; got.Stars != 1 || len(got.Tags) != 1 || got.Note == nil || *got.Note != note {
		t.Fatalf("stored rating: %+v", got)
	}

	// Saved preferences seed a tag the first time it moves.
	if _, err := srv.Store.Users().Update(context.Background(), a.UserID, bson.M{"prefs.ratings": bson.M{"food": 5}}); err != nil {
		t.Fatal(err)
	}
	lunch := ratedPlan(t, srv, a, seedActivity(t, srv, "restaurant", "food"), at(9, 27, 12, 30))
	rate(t, srv, a, lunch, 3)
	if taste = tasteOf(t, srv, a.UserID); !near(taste.Tags["food"], 0.85) || taste.RatingCount != 2 {
		t.Fatalf("food from a saved 5: %+v", taste)
	}

	// A stop that is not a catalog activity still counts, without a vector update.
	plain := ratedPlan(t, srv, a, "", at(9, 27, 15, 0))
	before := len(profiles.WaitRated(t, 3, time.Second))
	rate(t, srv, a, plain, 4)
	time.Sleep(100 * time.Millisecond)
	if taste = tasteOf(t, srv, a.UserID); taste.RatingCount != 3 || len(profiles.RatedCalls()) != before {
		t.Fatalf("rating without an activity: %+v, calls %d → %d", taste, before, len(profiles.RatedCalls()))
	}

	for _, stars := range []int{0, 6} {
		res := srv.Do(t, "PUT", "/ratings/"+sunrise, contract.Rating{Stars: stars, Tags: []string{}}, a).Expect(t, http.StatusBadRequest)
		if res.Message() != itineraries.MsgStars {
			t.Errorf("%d stars: %q", stars, res.Message())
		}
	}
	srv.Do(t, "PUT", "/ratings/"+sunrise, contract.Rating{Stars: 4, Tags: []string{}}, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "PUT", "/ratings/nope", contract.Rating{Stars: 4, Tags: []string{}}, a).Expect(t, http.StatusNotFound)
}

func TestRatingSucceedsWhenProfilesFail(t *testing.T) {
	profiles := &testutil.ProfilesRecorder{Err: errors.New("ml service down")}
	srv := testutil.New(t, testutil.WithNow(pastClock), testutil.WithProfiles(profiles))
	a := srv.Signup(t, "Alice Offline")
	stop := ratedPlan(t, srv, a, seedActivity(t, srv, "park", "outdoor"), at(9, 27, 10, 0))
	rate(t, srv, a, stop, 4)
	profiles.WaitRated(t, 1, time.Second)
	if taste := tasteOf(t, srv, a.UserID); taste.RatingCount != 1 || !near(taste.Tags["outdoors"], 0.575) {
		t.Fatalf("taste with the ML service down: %+v", taste)
	}
}

// TestRatingsMoveTheTasteBars: the tags a rating writes are the ones
// GET /me/taste-profile reads (Outdoors ← outdoors, Social ← social).
func TestRatingsMoveTheTasteBars(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(pastClock))
	a := srv.Signup(t, "Alice Bars")
	b := srv.Signup(t, "Bob Bars")
	bars := func() map[string]float64 {
		t.Helper()
		var profile contract.TasteProfile
		srv.Do(t, "GET", "/me/taste-profile", nil, a).Expect(t, http.StatusOK).JSON(t, &profile)
		out := map[string]float64{}
		for _, bar := range profile.Bars {
			out[bar.Label] = bar.Value
		}
		return out
	}
	before := bars()
	hike := seedActivity(t, srv, "hike", "outdoors")
	req := plan("Group hike", contract.VisibilityFriends, stopSpec{id: "g0", title: "Group hike", start: at(9, 27, 10, 0), minutes: 120})
	req.Option.Stops[0].ActivityID = &hike
	it := create(t, srv, a, req)
	addMember(t, srv, it.ID, b)
	stop := stops(it)[0].ID

	rate(t, srv, a, stop, 4)
	taste := tasteOf(t, srv, a.UserID)
	if !near(taste.Tags["outdoors"], 0.575) || !near(taste.Tags["social"], 0.575) || taste.RatingCount != 1 {
		t.Fatalf("a 4-star group hike: %+v", taste)
	}
	// Adding "Great people" later pulls social toward 1 without re-counting the stars.
	rate(t, srv, a, stop, 4, "Great people")
	taste = tasteOf(t, srv, a.UserID)
	if !near(taste.Tags["outdoors"], 0.575) || !near(taste.Tags["social"], 0.7025) || taste.RatingCount != 1 {
		t.Fatalf("after adding Great people: %+v", taste)
	}
	rate(t, srv, a, stop, 4, "Great people")
	if again := tasteOf(t, srv, a.UserID); !near(again.Tags["social"], 0.7025) {
		t.Fatalf("re-saving moved social: %+v", again)
	}
	after := bars()
	if after["Outdoors"] <= before["Outdoors"] || after["Social"] <= before["Social"] ||
		after["Food"] != before["Food"] || after["Nightlife"] != before["Nightlife"] {
		t.Fatalf("taste bars before %v after %v", before, after)
	}
}
