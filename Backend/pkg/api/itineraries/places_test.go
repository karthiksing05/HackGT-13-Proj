package itineraries_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// catalogDoc is a catalog entry shaped like freetime's activities.
type catalogDoc struct {
	kind, name, venue, category string
	lat, lng                    float64
}

var atlanta = []catalogDoc{
	{"place", "Piedmont Park", "", "park", 33.7851, -84.3738},
	{"place", "Ponce City Market", "", "market", 33.7726, -84.3655},
	{"place", "Krog Street Market", "", "market", 33.7571, -84.3640},
	{"place", "Centennial Olympic Park", "", "park", 33.7603, -84.3932},
	{"place", "Historic Fourth Ward Park", "", "park", 33.7667, -84.3639},
	{"place", "Tech Square", "", "landmark", 33.7766, -84.3890},
	{"event", "Jazz at the Park", "Symphony Hall", "live_music", 33.7890, -84.3860},
	{"event", "Park Tavern Trivia", "Park Tavern", "bar", 33.7856, -84.3710},
	{"event", "Movie night", "Piedmont Park", "community_event", 33.7852, -84.3737},
}

var saltlight = []catalogDoc{
	{"place", "Seaside Market Square", "", "market", 31.3680, -81.4250},
	{"place", "The Lighthouse Laboratory", "", "museum", 31.4007, -81.395612},
}

func seedCatalog(t *testing.T, srv *testutil.Server, coll string, docs []catalogDoc) {
	t.Helper()
	var rows []any
	for _, d := range docs {
		doc := models.Activity{ID: bson.NewObjectID(), Kind: d.kind, City: "atlanta", Name: d.name, Category: d.category, Tags: []string{},
			Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{d.lng, d.lat}}, Timezone: testutil.TimeZone}
		if d.venue != "" {
			venue := d.venue
			doc.VenueName = &venue
		}
		rows = append(rows, doc)
	}
	if _, err := srv.Store.Collection(coll).InsertMany(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
}

func setHome(t *testing.T, srv *testutil.Server, sess *testutil.Session, name string, lat, lng float64) {
	t.Helper()
	if _, err := srv.Store.Users().Update(context.Background(), sess.UserID, bson.M{"homeBase": models.HomeBase{Name: name, Lat: lat, Lng: lng}}); err != nil {
		t.Fatal(err)
	}
}

func placeNames(places []contract.Place) []string {
	var out []string
	for _, p := range places {
		out = append(out, p.Name)
	}
	return out
}

func placesSearch(t *testing.T, srv *testutil.Server, sess *testutil.Session, query string) []contract.Place {
	t.Helper()
	var places []contract.Place
	srv.Do(t, "GET", "/places/search?"+query, nil, sess).Expect(t, http.StatusOK).JSON(t, &places)
	return places
}

func TestPlacesSearch(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	seedCatalog(t, srv, store.DefaultCatalog, atlanta)
	seedCatalog(t, srv, otherActivityCollection(), saltlight)
	a := srv.Signup(t, "Alice Places")
	techSquare := travel.Point{Lat: 33.7766, Lng: -84.3890}

	// Names (or an event's venue) containing q, nearest first, one per name.
	found := placesSearch(t, srv, a, "q=park&near=33.7766,-84.389")
	if got := strings.Join(placeNames(found), " | "); got != "Piedmont Park | Centennial Olympic Park | Park Tavern | Historic Fourth Ward Park" {
		t.Fatalf("park near Tech Square: %s", got)
	}
	last := 0.0
	for _, p := range found {
		d := travel.HaversineKm(techSquare, travel.Point{Lat: p.Coordinate.Lat, Lng: p.Coordinate.Lng})
		if d < last {
			t.Fatalf("not nearest first: %v", placeNames(found))
		}
		last = d
	}
	// Without a reference point: by name.
	if got := strings.Join(placeNames(placesSearch(t, srv, a, "q=MARKET")), " | "); got != "Krog Street Market | Ponce City Market" {
		t.Fatalf("market by name: %s", got)
	}
	// No query and nowhere to start from: nothing to suggest.
	if got := placesSearch(t, srv, a, "q="); len(got) != 0 {
		t.Fatalf("empty query without home base: %v", placeNames(got))
	}
	if got := placesSearch(t, srv, a, "near=33.7766,-84.389"); len(got) != 4 || got[0].Name != "Tech Square" {
		t.Fatalf("four nearest: %v", placeNames(got))
	}

	// With a home base: it comes first, then the four nearest to it (without repeating it).
	setHome(t, srv, a, "Tech Square", 33.7766, -84.3890)
	home := placesSearch(t, srv, a, "q=")
	if len(home) != 5 || home[0].Name != "Tech Square" || home[0].Coordinate == nil {
		t.Fatalf("home base first: %v", placeNames(home))
	}
	for _, p := range home[1:] {
		if p.Name == "Tech Square" {
			t.Fatalf("home base repeated: %v", placeNames(home))
		}
	}
	if got := placesSearch(t, srv, a, "q=park"); got[0].Name != "Piedmont Park" {
		t.Fatalf("q near the home base: %v", placeNames(got))
	}

	// Changing the email does not switch the configured collection.
	before := strings.Join(placeNames(placesSearch(t, srv, a, "q=a")), " | ")
	email := strings.Replace(testutil.UniqueEmail("student"), "@example.test", "@gatech.edu", 1)
	if _, err := srv.Store.Users().Update(t.Context(), a.UserID, bson.M{"email": email, "catalog": otherActivityCollection()}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(placeNames(placesSearch(t, srv, a, "q=a")), " | "); got != before {
		t.Fatalf("email changed the activity collection: %s, want %s", got, before)
	}
	srv.Do(t, "GET", "/places/search?q=park&near=north", nil, a).Expect(t, http.StatusBadRequest)
	srv.Do(t, "GET", "/places/search?q=park", nil, nil).Expect(t, http.StatusUnauthorized)
}

func TestReverseGeocode(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	seedCatalog(t, srv, store.DefaultCatalog, atlanta)
	a := srv.Signup(t, "Alice Pin")
	var place contract.Place
	// About 15 m from Tech Square.
	srv.Do(t, "GET", "/places/reverse?lat=33.7767&lng=-84.3891", nil, a).Expect(t, http.StatusOK).JSON(t, &place)
	if place.Name != "Tech Square" || place.Coordinate == nil || place.Coordinate.Lat != 33.7766 {
		t.Fatalf("near Tech Square: %+v", place)
	}
	// An event names its venue.
	srv.Do(t, "GET", "/places/reverse?lat=33.7890&lng=-84.3860", nil, a).Expect(t, http.StatusOK).JSON(t, &place)
	if place.Name != "Symphony Hall" {
		t.Fatalf("an event's venue: %+v", place)
	}
	srv.Do(t, "GET", "/places/reverse?lat=33.8&lng=-84.3", nil, a).Expect(t, http.StatusOK).JSON(t, &place)
	if place.Name != "Dropped pin" || place.Coordinate == nil || place.Coordinate.Lat != 33.8 || place.Coordinate.Lng != -84.3 {
		t.Fatalf("far from everything: %+v", place)
	}
	for _, q := range []string{"lat=abc&lng=-84.3", "lat=95&lng=0", "lng=-84.3"} {
		srv.Do(t, "GET", "/places/reverse?"+q, nil, a).Expect(t, http.StatusBadRequest)
	}
}

// TestPlacesOnRealCatalogDocs copies Atlanta activities from the local
// freetime seed (read-only) and checks every copied place names itself.
func TestPlacesOnRealCatalogDocs(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	ctx := context.Background()
	source := srv.Store.DB().Client().Database("freetime").Collection(store.CollActivities)
	cursor, err := source.Find(ctx, bson.M{"kind": "place"}, options.Find().SetLimit(40).
		SetProjection(bson.M{"embedding": 0, "embeddingText": 0, "embeddingTextHash": 0}))
	if err != nil {
		t.Fatal(err)
	}
	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Skip("no freetime seed on this server")
	}
	rows := make([]any, 0, len(docs))
	for _, d := range docs {
		rows = append(rows, d)
	}
	// Alice is a regular account, so she reads store.DefaultCatalog (store.CatalogFor).
	if _, err := srv.Store.Collection(store.DefaultCatalog).InsertMany(ctx, rows); err != nil {
		t.Fatal(err)
	}
	a := srv.Signup(t, "Alice Real")
	for _, d := range docs[:10] {
		var act models.Activity
		raw, _ := bson.Marshal(d)
		if err := bson.Unmarshal(raw, &act); err != nil {
			t.Fatal(err)
		}
		lng, lat := act.Location.Coordinates[0], act.Location.Coordinates[1]
		var place contract.Place
		srv.Do(t, "GET", fmt.Sprintf("/places/reverse?lat=%v&lng=%v", lat, lng), nil, a).Expect(t, http.StatusOK).JSON(t, &place)
		if place.Name == "Dropped pin" || place.Name == "" {
			t.Errorf("a pin on %q found nothing", act.Name)
		}
		var found []contract.Place
		word := strings.Fields(act.Name)[0]
		srv.Do(t, "GET", fmt.Sprintf("/places/search?q=%s&near=%v,%v", url.QueryEscape(word), lat, lng), nil, a).Expect(t, http.StatusOK).JSON(t, &found)
		if len(found) == 0 || len(found) > 10 || !strings.Contains(strings.ToLower(found[0].Name), strings.ToLower(word)) {
			t.Errorf("searching %q near %q: %v", word, act.Name, placeNames(found))
		}
	}
}
