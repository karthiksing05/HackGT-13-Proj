package itineraries_test

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// hitDoc is a catalog entry for the must-see search: an event with its
// times, or a place open daily from open to close (hours).
type hitDoc struct {
	kind, name, venue, category string
	tags                        []string
	lat, lng                    float64
	start, end                  time.Time
	open, close                 int
	rating                      float64
	dollars                     *float64
}

func dollars(v float64) *float64 { return &v }

func seedHits(t *testing.T, srv *testutil.Server, coll, city string, docs []hitDoc) {
	t.Helper()
	var rows []any
	for _, d := range docs {
		doc := models.Activity{ID: bson.NewObjectID(), Kind: d.kind, City: city, Name: d.name, Category: d.category, Tags: d.tags,
			Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{d.lng, d.lat}}, Timezone: testutil.TimeZone}
		if doc.Tags == nil {
			doc.Tags = []string{}
		}
		if d.venue != "" {
			doc.VenueName = &d.venue
		}
		if d.kind == "event" {
			start, end := d.start.UTC(), d.end.UTC()
			doc.Start, doc.End, doc.Attendance = &start, &end, str("fixed_start")
		} else {
			for day := range 7 {
				doc.WeeklyHours = append(doc.WeeklyHours, models.WeeklyHourRange{Open: day*1440 + d.open*60, Close: day*1440 + d.close*60})
			}
		}
		if d.rating > 0 {
			doc.Rating = &d.rating
		}
		if d.dollars != nil {
			doc.Price = &models.ActivityPrice{Min: d.dollars, Max: d.dollars, Currency: "USD", IsFree: *d.dollars == 0}
		}
		rows = append(rows, doc)
	}
	if _, err := srv.Store.Collection(coll).InsertMany(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
}

func str(s string) *string { return &s }

// Saturday 26 Sep 2026 around Tech Square (the test clock: Saturday noon).
var atlantaHits = []hitDoc{
	{kind: "event", name: "Jazz Night at Symphony Hall", venue: "Symphony Hall", category: "live_music", lat: 33.7890, lng: -84.3860,
		start: at(9, 26, 19, 0), end: at(9, 26, 21, 0), dollars: dollars(25)},
	{kind: "event", name: "Sunset Jazz Walk", venue: "Piedmont Park Gate", category: "tour", lat: 33.7851, lng: -84.3738,
		start: at(9, 26, 18, 0), end: at(9, 26, 19, 30), dollars: dollars(0)},
	{kind: "event", name: "Morning Jazz Brunch", venue: "Brunch Hall", category: "restaurant", lat: 33.7800, lng: -84.3850,
		start: at(9, 26, 10, 0), end: at(9, 26, 11, 30)}, // over by noon
	{kind: "event", name: "Jazz Picnic", venue: "Piedmont Park", category: "community_event", lat: 33.7851, lng: -84.3738,
		start: at(9, 27, 12, 0), end: at(9, 27, 15, 0)}, // Sunday
	{kind: "event", name: "Late Jazz Lounge Set", venue: "The Lounge", category: "bar", tags: []string{"21_plus", "music"}, lat: 33.7700, lng: -84.3860,
		start: at(9, 26, 22, 0), end: at(9, 26, 23, 30)},
	{kind: "event", name: "Bluegrass Night", venue: "Jazz Alley", category: "live_music", lat: 33.7750, lng: -84.3700,
		start: at(9, 26, 20, 0), end: at(9, 26, 22, 0)},
	{kind: "place", name: "Jazz Record Shop", category: "shopping", lat: 33.7780, lng: -84.3880, open: 10, close: 21, rating: 4.5},
	{kind: "place", name: "Jazzy's Bar", category: "bar", lat: 33.7700, lng: -84.3950, open: 16, close: 24, rating: 4.6},
	{kind: "place", name: "Blue Note Jazz Club", category: "live_music", lat: 33.7710, lng: -84.3890, open: 18, close: 24, rating: 4.9}, // a venue: shows come as events
	{kind: "place", name: "Piedmont Park", category: "park", lat: 33.7851, lng: -84.3738, open: 6, close: 22, rating: 4.8},
	{kind: "place", name: "Centennial Olympic Park", category: "park", lat: 33.7603, lng: -84.3932, open: 6, close: 22, rating: 4.6},
	{kind: "place", name: "Stone Mountain Park", category: "park", lat: 33.8053, lng: -84.1455, open: 6, close: 22, rating: 4.9}, // 22 km out
}

var saltlightHits = []hitDoc{
	{kind: "event", name: "Sunset Jazz on Pier Nine", venue: "Pier Nine Bandstand", category: "live_music", lat: 31.376524, lng: -81.41746,
		start: at(9, 26, 18, 30), end: at(9, 26, 21, 0), dollars: dollars(12)},
	{kind: "place", name: "Seaside Market Hall", category: "market", lat: 31.365972, lng: -81.428348, open: 8, close: 19, rating: 4.7},
}

func searchHits(t *testing.T, srv *testutil.Server, sess *testutil.Session, query string) []contract.ActivityHit {
	t.Helper()
	var hits []contract.ActivityHit
	srv.Do(t, "GET", "/activities/search?"+query, nil, sess).Expect(t, http.StatusOK).JSON(t, &hits)
	return hits
}

func hitNames(hits []contract.ActivityHit) string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Title)
	}
	return strings.Join(out, " | ")
}

const nearTechSquare = "near=33.7766,-84.389"

func TestActivitySearchMatchesAndRanks(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	seedHits(t, srv, store.DefaultCatalog, "atlanta", atlantaHits)
	seedHits(t, srv, otherActivityCollection(), "saltlight", saltlightHits)
	a := srv.Signup(t, "Alice Picks")

	// Names starting with "jazz", then names with a word that does, then
	// the other matches (a venue); events by start before places by
	// distance. The brunch is over, the picnic is tomorrow, and the jazz
	// club is a venue, which plans never visit.
	hits := searchHits(t, srv, a, "q=JAZZ&"+nearTechSquare)
	want := "Jazz Night at Symphony Hall | Jazz Record Shop | Jazzy's Bar | Sunset Jazz Walk | Late Jazz Lounge Set | Bluegrass Night"
	if got := hitNames(hits); got != want {
		t.Fatalf("jazz:\n got %s\nwant %s", got, want)
	}
	night := hits[0]
	if night.Kind != contract.StopKindEvent || night.Category != "live_music" || night.Place.Name != "Symphony Hall" || night.Place.Coordinate == nil ||
		night.Start == nil || !night.Start.Equal(at(9, 26, 19, 0)) || night.End == nil || !night.End.Equal(at(9, 26, 21, 0)) ||
		night.PriceCents == nil || *night.PriceCents != 2500 || night.DistanceMi == nil || *night.DistanceMi != 0.9 ||
		night.Subtitle != "Live music · 7:00 PM · 0.9 mi" {
		t.Errorf("event hit %+v", night)
	}
	if shop := hits[1]; shop.Kind != contract.StopKindPlace || shop.Start != nil || shop.End != nil || shop.PriceCents != nil ||
		shop.Subtitle != "Shopping · 0.1 mi" {
		t.Errorf("place hit %+v", shop)
	}
	if walk := hits[3]; walk.PriceCents == nil || *walk.PriceCents != 0 {
		t.Errorf("a free event says so: %+v", walk)
	}

	// Without near: no distances; the same order otherwise, places by rating.
	hits = searchHits(t, srv, a, "q=jazz")
	if hitNames(hits) != "Jazz Night at Symphony Hall | Jazzy's Bar | Jazz Record Shop | Sunset Jazz Walk | Late Jazz Lounge Set | Bluegrass Night" {
		t.Errorf("without near: %s", hitNames(hits))
	}
	for _, h := range hits {
		if h.DistanceMi != nil || strings.HasSuffix(h.Subtitle, " mi") {
			t.Errorf("a distance without near: %+v", h)
		}
	}
	// A category or a tag, with a space for the underscore.
	if got := hitNames(searchHits(t, srv, a, "q=live%20music&"+nearTechSquare)); got != "Jazz Night at Symphony Hall | Bluegrass Night" {
		t.Errorf("live music: %s", got)
	}
	if got := searchHits(t, srv, a, "q=bossa%20nova"); len(got) != 0 {
		t.Errorf("no match: %s", hitNames(got))
	}
	if got := searchHits(t, srv, a, "q=jazz&limit=2"); len(got) != 2 {
		t.Errorf("limit 2: %s", hitNames(got))
	}
}

func TestActivitySuggestions(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	seedHits(t, srv, store.DefaultCatalog, "atlanta", atlantaHits)
	a := srv.Signup(t, "Alice Suggest")
	// The day's events that are not over, by start; then the places, the
	// best-rated within 5 km first, the far park last.
	want := "Sunset Jazz Walk | Jazz Night at Symphony Hall | Bluegrass Night | Late Jazz Lounge Set | " +
		"Piedmont Park | Jazzy's Bar | Centennial Olympic Park | Jazz Record Shop | Stone Mountain Park"
	if got := hitNames(searchHits(t, srv, a, "q=&"+nearTechSquare)); got != want {
		t.Errorf("suggestions:\n got %s\nwant %s", got, want)
	}
	// Another day: its own events.
	if got := hitNames(searchHits(t, srv, a, "q=jazz&date=2026-09-27")); got != "Jazz Picnic | Jazzy's Bar | Jazz Record Shop" {
		t.Errorf("Sunday: %s", got)
	}
}

func TestActivitySearchAgeAndCatalog(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	seedHits(t, srv, store.DefaultCatalog, "atlanta", atlantaHits)
	seedHits(t, srv, otherActivityCollection(), "saltlight", saltlightHits)

	// Under 21: no 21+ set, no bar.
	req := testutil.SignupRequest("Young Picker")
	born := contract.NewTime(time.Date(2007, 3, 1, 0, 0, 0, 0, time.UTC))
	req.DateOfBirth = &born
	young := srv.SignupWith(t, req)
	if got := hitNames(searchHits(t, srv, young, "q=jazz&"+nearTechSquare)); got != "Jazz Night at Symphony Hall | Jazz Record Shop | Sunset Jazz Walk | Bluegrass Night" {
		t.Errorf("19-year-old: %s", got)
	}

	// An account outside the demo searches the default catalog, whatever a
	// legacy catalog field says.
	a := srv.Signup(t, "Alice Atlanta")
	if _, err := srv.Store.Users().Update(t.Context(), a.UserID, bson.M{"catalog": store.CollDemoActivities}); err != nil {
		t.Fatal(err)
	}
	for _, h := range searchHits(t, srv, a, "q=a&limit=50") {
		if strings.Contains(h.Title, "Pier Nine") || h.Title == "Seaside Market Hall" {
			t.Errorf("an Atlanta account finds %s", h.Title)
		}
	}
	sandy := srv.Signup(t, "Sandy Picks")
	if _, err := srv.Store.Users().Update(context.Background(), sandy.UserID, bson.M{"email": strings.Replace(testutil.UniqueEmail("demo"), "@example.test", "@gatech.edu", 1), "catalog": otherActivityCollection(), "city": "saltlight"}); err != nil {
		t.Fatal(err)
	}
	if got, want := hitNames(searchHits(t, srv, sandy, "q=jazz")), hitNames(searchHits(t, srv, a, "q=jazz")); got != want {
		t.Errorf("email or legacy catalog changed the search: %s, want %s", got, want)
	}
	// The demo account searches Saltlight.
	if _, err := srv.Store.Users().Update(context.Background(), sandy.UserID, bson.M{"roles": []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	demo, atlanta := hitNames(searchHits(t, srv, sandy, "q=a&limit=50")), hitNames(searchHits(t, srv, a, "q=a&limit=50"))
	if demo == "" || demo == atlanta {
		t.Errorf("the demo account should search Saltlight: %q (Atlanta: %q)", demo, atlanta)
	}
}

func TestActivitySearchOnTheDemoDate(t *testing.T) {
	// DEMO_DATE Thursday 24 Sep while it is really Sunday 27 Sep at noon: a
	// demo account's "today" is Thursday, in business time.
	real := time.Date(2026, 9, 27, 12, 0, 0, 0, ny)
	srv := testutil.New(t, testutil.WithNow(real.UTC()), testutil.WithConfig(func(c *config.Config) { c.DemoDate = "2026-09-24" }))
	seedHits(t, srv, store.CollDemoActivities, "saltlight", []hitDoc{
		{kind: "event", name: "Thursday Coffee Hour", venue: "Driftwood", category: "community_event", lat: 31.3707, lng: -81.4192,
			start: at(9, 24, 10, 0), end: at(9, 24, 11, 0)}, // over by Thursday noon
		{kind: "event", name: "Thursday Trivia Night", venue: "The Rusty Anchor", category: "community_event", lat: 31.3741, lng: -81.4217,
			start: at(9, 24, 19, 0), end: at(9, 24, 21, 0)},
		{kind: "event", name: "Sunday Shanty", venue: "The Rusty Anchor", category: "live_music", lat: 31.3741, lng: -81.4217,
			start: at(9, 27, 19, 0), end: at(9, 27, 21, 0)},
	})
	sandy := srv.Signup(t, "Sandy Demo")
	if _, err := srv.Store.Users().Update(context.Background(), sandy.UserID, bson.M{"email": testutil.UniqueEmail("demo"), "city": "saltlight", "roles": []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	if got := hitNames(searchHits(t, srv, sandy, "q=")); got != "Thursday Trivia Night" {
		t.Errorf("the demo account's today: %s", got)
	}
	if got := hitNames(searchHits(t, srv, sandy, "q=&date=2026-09-27")); got != "Sunday Shanty" {
		t.Errorf("the demo account on Sunday: %s", got)
	}
}

func TestActivitySearchErrors(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	seedHits(t, srv, store.DefaultCatalog, "atlanta", atlantaHits)
	a := srv.Signup(t, "Alice Errors")
	for query, msg := range map[string]string{
		"q=jazz&near=north":       "Check the location and try again.",
		"q=jazz&near=95,0":        "Check the location and try again.",
		"q=jazz&date=tomorrow":    "Check the date and try again.",
		"q=jazz&date=2026-13-01":  "Check the date and try again.",
		"q=jazz&limit=0":          "Check how many results to show and try again.",
		"q=jazz&limit=twenty":     "Check how many results to show and try again.",
		"q=jazz&limit=-3&near=1,": "Check the location and try again.",
	} {
		if got := srv.Do(t, "GET", "/activities/search?"+query, nil, a).Expect(t, http.StatusBadRequest).Message(); got != msg {
			t.Errorf("%s: %q, want %q", query, got, msg)
		}
	}
	// Above 50 is 50.
	if got := searchHits(t, srv, a, "q=&limit=500"); len(got) == 0 || len(got) > 50 {
		t.Errorf("limit 500: %d", len(got))
	}
	srv.Do(t, "GET", "/activities/search?q=jazz", nil, nil).Expect(t, http.StatusUnauthorized)
}
