package itineraries_test

import (
	"Backend/pkg/api"
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

// seedCatalogDoc puts one catalog document in coll and returns its id.
func seedCatalogDoc(t *testing.T, srv *testutil.Server, coll string, doc models.Activity) string {
	t.Helper()
	doc.ID = bson.NewObjectID()
	if doc.Tags == nil {
		doc.Tags = []string{}
	}
	if _, err := srv.Store.Collection(coll).InsertOne(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	return doc.ID.Hex()
}

// openOn is weekly hours from openHour for hours on each weekday (0 = Sunday).
func openOn(openHour, hours int, weekdays ...int) []models.WeeklyHourRange {
	var out []models.WeeklyHourRange
	for _, d := range weekdays {
		open := d*1440 + openHour*60
		out = append(out, models.WeeklyHourRange{Open: open, Close: (open + hours*60) % (7 * 1440)})
	}
	return out
}

// recordShop is open Monday to Saturday 10–6 (Google's hours) and closed on Sundays.
func recordShop() models.Activity {
	return models.Activity{
		Kind: "place", City: "atlanta", Name: "Jazz Record Shop", Category: "shopping", Tags: []string{"music", "indoor"},
		Summary:     str("Crates of vinyl and a listening booth."),
		Description: str("Crates of vinyl, a listening booth in the back, and staff picks by the register."),
		Location:    models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-84.3880, 33.7780}},
		Address:     &models.ActivityAddress{Formatted: str("88 5th St NW, Atlanta, GA 30308")},
		VenueName:   str("Jazz Record Shop"),
		Timezone:    testutil.TimeZone,
		WeeklyHours: openOn(10, 8, 1, 2, 3, 4, 5, 6),
		HoursSource: str("google"),
		Price:       &models.ActivityPrice{Tier: 1, Currency: "USD"},
		Rating:      dollars(4.5),
		RatingCount: intPtr(1204),
		URL:         str("https://jazzrecords.example"),
	}
}

// jazzNight is Saturday's show at Symphony Hall, 7–9 PM, $25–$40.
func jazzNight() models.Activity {
	start, end := at(9, 26, 19, 0).UTC(), at(9, 26, 21, 0).UTC()
	return models.Activity{
		Kind: "event", City: "atlanta", Name: "Jazz Night at Symphony Hall", Category: "live_music",
		Location:  models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-84.3860, 33.7890}},
		Address:   &models.ActivityAddress{Street: str("1280 Peachtree St NE"), Locality: str("Atlanta"), Region: str("GA")},
		VenueName: str("Symphony Hall"), Start: &start, End: &end, Attendance: str("fixed_start"), Timezone: testutil.TimeZone,
		Price:     &models.ActivityPrice{Min: dollars(25), Max: dollars(40), Currency: "USD", Tier: 2},
		TicketURL: str("https://tickets.example/jazz-night"), ImageURL: str("not a link"),
	}
}

// rustyAnchor is a Saltlight pub open 3 PM–2 AM Monday to Saturday.
func rustyAnchor() models.Activity {
	return models.Activity{
		Kind: "place", City: "saltlight", Name: "The Rusty Anchor", Category: "bar",
		Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-81.421732, 31.374148}},
		Timezone: testutil.TimeZone, WeeklyHours: openOn(15, 11, 1, 2, 3, 4, 5, 6), HoursSource: str("google"),
		Price: &models.ActivityPrice{Min: dollars(6), Max: dollars(12), Currency: "USD", Tier: 1},
	}
}

func intPtr(n int) *int { return &n }

func activityDetail(t *testing.T, srv *testutil.Server, sess *testutil.Session, path string) contract.ActivityDetail {
	t.Helper()
	var out contract.ActivityDetail
	srv.Do(t, "GET", path, nil, sess).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

// useDemoCatalog moves an account to the demo catalog (Sandy and her bots).
func useDemoCatalog(t *testing.T, srv *testutil.Server, sess *testutil.Session) {
	t.Helper()
	if _, err := srv.Store.Users().Update(context.Background(), sess.UserID, bson.M{"catalog": store.CollDemoActivities, "city": "saltlight"}); err != nil {
		t.Fatal(err)
	}
}

func TestActivityDetailMapsTheCatalog(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 26 Sep, noon in New York
	shop := seedCatalogDoc(t, srv, store.CollActivities, recordShop())
	jazz := seedCatalogDoc(t, srv, store.CollActivities, jazzNight())
	a := srv.Signup(t, "Alice Details")

	// A place, with the hours of the viewer's today (a Saturday)…
	got := activityDetail(t, srv, a, "/activities/"+shop)
	if got.ID != shop || got.Title != "Jazz Record Shop" || got.Kind != contract.StopKindPlace || got.Category != "shopping" ||
		got.CategoryLabel != "Shopping" || got.Summary == nil || *got.Summary != "Crates of vinyl and a listening booth." ||
		got.Description == nil || !strings.HasPrefix(*got.Description, "Crates of vinyl, a listening booth") ||
		got.VenueName == nil || *got.VenueName != "Jazz Record Shop" || got.Address == nil || *got.Address != "88 5th St NW, Atlanta, GA 30308" ||
		got.Place.Name != "Jazz Record Shop" || got.Place.Coordinate == nil || got.Place.Coordinate.Lat != 33.7780 ||
		got.Start != nil || got.End != nil || got.HoursLine == nil || *got.HoursLine != "Open 10 AM–6 PM" ||
		got.PriceCents != nil || got.PriceLabel != "$" || got.Rating == nil || *got.Rating != 4.5 ||
		got.RatingCount == nil || *got.RatingCount != 1204 || got.URL == nil || *got.URL != "https://jazzrecords.example" ||
		got.TicketURL != nil || strings.Join(got.Tags, ",") != "music,indoor" {
		t.Errorf("place detail %+v", got)
	}
	// …or of the plan's day.
	if sunday := activityDetail(t, srv, a, "/activities/"+shop+"?date=2026-09-27"); sunday.HoursLine == nil || *sunday.HoursLine != "Closed that day" {
		t.Errorf("Sunday: %v", sunday.HoursLine)
	}

	// An event: its times and venue, a price range, no hours line; a link that isn't one is left out.
	ev := activityDetail(t, srv, a, "/activities/"+jazz)
	if ev.Kind != contract.StopKindEvent || ev.CategoryLabel != "Live music" || ev.Place.Name != "Symphony Hall" ||
		ev.Start == nil || !ev.Start.Equal(at(9, 26, 19, 0)) || ev.End == nil || !ev.End.Equal(at(9, 26, 21, 0)) ||
		ev.HoursLine != nil || ev.PriceCents == nil || *ev.PriceCents != 2500 || ev.PriceLabel != "$25–$40" ||
		ev.Address == nil || *ev.Address != "1280 Peachtree St NE, Atlanta, GA" || ev.Summary != nil || ev.Rating != nil ||
		ev.TicketURL == nil || *ev.TicketURL != "https://tickets.example/jazz-night" || ev.ImageURL != nil || ev.Tags == nil {
		t.Errorf("event detail %+v", ev)
	}
	// Unknowns stay off the wire; tags is [] when there are none.
	res := srv.Do(t, "GET", "/activities/"+jazz, nil, a).Expect(t, http.StatusOK)
	if body := string(res.Body); !strings.Contains(body, `"tags":[]`) || strings.Contains(body, `"rating"`) || strings.Contains(body, `"summary"`) {
		t.Errorf("wire shape: %s", body)
	}
}

func TestActivityDetailStaysInTheViewersCatalog(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	shop := seedCatalogDoc(t, srv, store.CollActivities, recordShop())
	pub := seedCatalogDoc(t, srv, store.CollDemoActivities, rustyAnchor())
	a := srv.Signup(t, "Alice Catalog")
	sandy := srv.Signup(t, "Sandy Catalog")
	useDemoCatalog(t, srv, sandy)

	// Each account reads its own catalog…
	if got := activityDetail(t, srv, sandy, "/activities/"+pub); got.Title != "The Rusty Anchor" || got.PriceLabel != "$6–$12" ||
		got.HoursLine == nil || *got.HoursLine != "Open 3 PM–2 AM" {
		t.Errorf("the demo catalog's pub: %+v", got)
	}
	// …and the other catalog's ids, malformed ids and unknown ones are 404 with the usual body.
	for _, c := range []struct {
		who  *testutil.Session
		id   string
		name string
	}{
		{a, pub, "the demo catalog's pub for an Atlanta account"},
		{sandy, shop, "an Atlanta shop for the demo account"},
		{a, "nope", "a malformed id"},
		{a, strings.ToUpper(bson.NewObjectID().Hex()), "an unknown id"},
	} {
		res := srv.Do(t, "GET", "/activities/"+c.id, nil, c.who).Expect(t, http.StatusNotFound)
		if res.Message() != api.MsgNotFound || !strings.Contains(string(res.Body), `"error":"not_found"`) {
			t.Errorf("%s: %s", c.name, res.Body)
		}
	}
	// A token is needed; a date must be a date.
	srv.Do(t, "GET", "/activities/"+shop, nil, nil).Expect(t, http.StatusUnauthorized)
	for _, date := range []string{"tomorrow", "2026-13-01"} {
		if got := srv.Do(t, "GET", "/activities/"+shop+"?date="+date, nil, a).Expect(t, http.StatusBadRequest).Message(); got != "Check the date and try again." {
			t.Errorf("date=%s: %q", date, got)
		}
	}
	// The must-see search keeps its path.
	srv.Do(t, "GET", "/activities/search?q=jazz", nil, a).Expect(t, http.StatusOK)
}

func TestActivityDetailOnTheDemoDate(t *testing.T) {
	// DEMO_DATE Sunday 27 Sep while it is really Thursday 24 Sep at noon: the demo account's
	// "today" is Sunday, when the pub is closed; everyone else's is Thursday.
	real := time.Date(2026, 9, 24, 12, 0, 0, 0, ny)
	srv := testutil.New(t, testutil.WithNow(real.UTC()), testutil.WithConfig(func(c *config.Config) { c.DemoDate = "2026-09-27" }))
	pub := seedCatalogDoc(t, srv, store.CollDemoActivities, rustyAnchor())
	shop := seedCatalogDoc(t, srv, store.CollActivities, recordShop())
	sandy := srv.Signup(t, "Sandy Demo Day")
	useDemoCatalog(t, srv, sandy)
	a := srv.Signup(t, "Alice Real Day")

	if got := activityDetail(t, srv, sandy, "/activities/"+pub); got.HoursLine == nil || *got.HoursLine != "Closed that day" {
		t.Errorf("the demo account's Sunday: %v", got.HoursLine)
	}
	if got := activityDetail(t, srv, sandy, "/activities/"+pub+"?date=2026-09-26"); got.HoursLine == nil || *got.HoursLine != "Open 3 PM–2 AM" {
		t.Errorf("the demo account on Saturday: %v", got.HoursLine)
	}
	if got := activityDetail(t, srv, a, "/activities/"+shop); got.HoursLine == nil || *got.HoursLine != "Open 10 AM–6 PM" {
		t.Errorf("an account on the real clock: %v", got.HoursLine)
	}
}
