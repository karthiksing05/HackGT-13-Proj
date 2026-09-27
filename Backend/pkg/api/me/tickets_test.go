package me_test

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var ny = mustZone(testutil.TimeZone)

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// ticketStop is one stop of a hand-made plan.
type ticketStop struct {
	title, place string
	start        time.Time
	minutes      int
}

// insertPlan stores a plan hosted by host with the others on it too (the
// store keeps plans in the past as they are, which POST /itineraries
// doesn't allow). Stops sit at Ponce City Market.
func insertPlan(t *testing.T, srv *testutil.Server, title string, host *testutil.Session, others []*testutil.Session, stops ...ticketStop) *models.Itinerary {
	t.Helper()
	members := []string{host.UserID}
	for _, s := range others {
		members = append(members, s.UserID)
	}
	items := make([]models.ItineraryItem, 0, len(stops))
	for _, s := range stops {
		lat, lng := 33.7726, -84.3655
		items = append(items, models.ItineraryItem{
			ID: store.NewID(), Kind: models.ItemStop, Title: s.title, Place: &models.PlaceDoc{Name: s.place, Lat: &lat, Lng: &lng},
			Start: s.start, End: s.start.Add(time.Duration(s.minutes) * time.Minute), Bookable: true,
		})
	}
	first, last := items[0].Start.In(ny), items[len(items)-1].End
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, ny)
	it := &models.Itinerary{
		HostID: host.UserID, MemberIDs: members, Title: title, DateKey: day.Format("2006-01-02"), TZ: testutil.TimeZone,
		Date: day, Start: first.Add(-15 * time.Minute), BackBy: last.Add(30 * time.Minute),
		StartPlace: models.PlaceDoc{Name: "Home"}, EndPlace: models.PlaceDoc{Name: "Home"},
		Visibility: models.VisibilityFriends, Items: items,
	}
	if err := srv.Store.Itineraries().Insert(t.Context(), it); err != nil {
		t.Fatal(err)
	}
	return it
}

// book saves a ticket on sess's state for the item, as the checkout agent does.
func book(t *testing.T, srv *testutil.Server, sess *testutil.Session, it *models.Itinerary, item models.ItineraryItem, id string, quantity int) {
	t.Helper()
	total, code, url := 1200*quantity, "SQ-"+id, "http://api.test/tickets/"+id
	ticket := models.ItemTicket{ID: id, Quantity: quantity, TotalCents: &total, Confirmation: &code, URL: &url}
	if err := srv.Store.CheckoutReads().SaveTicket(t.Context(), sess.UserID, it.ID, item.ID, ticket); err != nil {
		t.Fatal(err)
	}
}

func myTickets(t *testing.T, srv *testutil.Server, sess *testutil.Session) []contract.MyTicket {
	t.Helper()
	var out []contract.MyTicket
	srv.Do(t, "GET", "/me/tickets", nil, sess).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func ticketIDs(tickets []contract.MyTicket) []string {
	ids := []string{}
	for _, tk := range tickets {
		ids = append(ids, tk.Ticket.ID)
	}
	return ids
}

// TestMyTicketsAreYourOwn: the whole ticket and its stop, for the buyer
// only. Another member's ticket on the same stop (which the plan shows the
// group) is not yours to list.
func TestMyTicketsAreYourOwn(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	alex, blair, casey, drew := srv.Signup(t, "Alex Tix"), srv.Signup(t, "Blair Tix"), srv.Signup(t, "Casey Tix"), srv.Signup(t, "Drew Tix")
	evening := time.Date(2026, 9, 26, 19, 0, 0, 0, ny)
	group := insertPlan(t, srv, "Friday night out", alex, []*testutil.Session{blair, drew},
		ticketStop{"Sunset Jazz", "Pier Nine Bandstand", evening, 120}, ticketStop{"Harbor walk", "Harbor Park", evening.Add(150 * time.Minute), 45})
	solo := insertPlan(t, srv, "Saturday reset", alex, nil, ticketStop{"Botanical Garden", "Piedmont Park", evening.Add(24 * time.Hour), 90})
	jazz := group.Items[0]
	book(t, srv, alex, group, jazz, "tk-alex", 2)
	book(t, srv, blair, group, jazz, "tk-blair", 1)
	book(t, srv, alex, solo, solo.Items[0], "tk-garden", 1)

	got := myTickets(t, srv, alex)
	if ids := ticketIDs(got); !slices.Equal(ids, []string{"tk-alex", "tk-garden"}) {
		t.Fatalf("Alex's tickets %v, want tk-alex then tk-garden", ids)
	}
	mine := got[0]
	want := contract.Ticket{ID: "tk-alex", Quantity: 2, TotalCents: testutil.Ptr(2400), Confirmation: testutil.Ptr("SQ-tk-alex"),
		URL: testutil.Ptr("http://api.test/tickets/tk-alex"), Mine: true}
	gotTicket, _ := json.Marshal(mine.Ticket)
	wantTicket, _ := json.Marshal(want)
	if string(gotTicket) != string(wantTicket) {
		t.Errorf("ticket %s, want %s", gotTicket, wantTicket)
	}
	if mine.ItemID != jazz.ID || mine.ItineraryID != group.ID || mine.ItineraryTitle == nil || *mine.ItineraryTitle != "Friday night out" ||
		mine.Title != "Sunset Jazz" || !mine.Start.Equal(jazz.Start) || !mine.End.Equal(jazz.End) || mine.Kind != contract.KindGroup {
		t.Errorf("the stop: %+v", mine)
	}
	if mine.Place.Name != "Pier Nine Bandstand" || mine.Place.Coordinate == nil || mine.Place.Coordinate.Lat != 33.7726 {
		t.Errorf("place %+v", mine.Place)
	}
	if got[1].Kind != contract.KindSidequest || got[1].Ticket.Quantity != 1 {
		t.Errorf("a solo plan's stop is a sidequest: %+v", got[1])
	}

	if ids := ticketIDs(myTickets(t, srv, blair)); !slices.Equal(ids, []string{"tk-blair"}) {
		t.Errorf("Blair's tickets %v", ids)
	}
	// Drew is on the plan and sees a ticket on the stop (the group's), but bought none.
	var plan contract.Itinerary
	srv.Do(t, "GET", "/itineraries/"+group.ID, nil, drew).Expect(t, http.StatusOK).JSON(t, &plan)
	if tk := plan.Items[0].Ticket; tk == nil || tk.Mine {
		t.Fatalf("Drew should see the group's ticket on the stop: %+v", tk)
	}
	if body := srv.Do(t, "GET", "/me/tickets", nil, drew).Expect(t, http.StatusOK).Body; string(body) != "[]\n" && string(body) != "[]" {
		t.Errorf("Drew's tickets: %s", body)
	}
	if got := myTickets(t, srv, casey); len(got) != 0 {
		t.Errorf("Casey has no tickets: %+v", got)
	}
}

// TestMyTicketsOnTheAccountClock: upcoming (still going or ahead) first by
// start, then past, latest first. The demo cast lives on the demo date, so
// the same stops sort differently for them than for a real-time account.
func TestMyTicketsOnTheAccountClock(t *testing.T) {
	realNow := time.Date(2026, 9, 27, 12, 0, 0, 0, ny) // Sunday noon; the demo date is Thursday
	srv := testutil.New(t, testutil.WithNow(realNow.UTC()), testutil.WithConfig(func(c *config.Config) { c.DemoDate = "2026-09-24" }))
	sandy, pat := srv.Signup(t, "Sandy Tix"), srv.Signup(t, "Pat Tix")
	if _, err := srv.Store.Users().Update(t.Context(), sandy.UserID, bson.M{"roles": []string{"demo"}, "city": "saltlight"}); err != nil {
		t.Fatal(err)
	}
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 9, day, hour, minute, 0, 0, ny) }
	stops := []struct {
		id string
		ticketStop
	}{
		{"market", ticketStop{"Morning market", "Seaside Market Hall", at(24, 9, 0), 60}},  // over by noon
		{"cruise", ticketStop{"Lunch cruise", "Harbor Pier", at(24, 11, 30), 60}},          // still going at noon
		{"jazz", ticketStop{"Evening jazz", "Pier Nine Bandstand", at(24, 19, 0), 120}},    // tonight
		{"gallery", ticketStop{"Gallery opening", "Old Customs House", at(25, 14, 0), 60}}, // tomorrow
		{"show", ticketStop{"Last week's show", "Lighthouse Theater", at(17, 20, 0), 120}}, // a week ago
	}
	for _, sess := range []*testutil.Session{sandy, pat} {
		for _, s := range stops {
			it := insertPlan(t, srv, "Plan "+s.id, sess, nil, s.ticketStop)
			book(t, srv, sess, it, it.Items[0], s.id+"-"+sess.UserID, 1)
		}
	}
	strip := func(ids []string, sess *testutil.Session) []string {
		out := []string{}
		for _, id := range ids {
			out = append(out, id[:len(id)-len(sess.UserID)-1])
		}
		return out
	}
	// Sandy's noon is Thursday's: the cruise is still on and tonight's jazz and tomorrow's gallery are ahead.
	if got := strip(ticketIDs(myTickets(t, srv, sandy)), sandy); !slices.Equal(got, []string{"cruise", "jazz", "gallery", "market", "show"}) {
		t.Errorf("demo account order %v", got)
	}
	// For everyone else it's Sunday: all of them are over, latest first.
	if got := strip(ticketIDs(myTickets(t, srv, pat)), pat); !slices.Equal(got, []string{"gallery", "jazz", "cruise", "market", "show"}) {
		t.Errorf("real-time account order %v", got)
	}
}

// TestMyTicketsOutliveThePlan: a ticket stays yours when you leave the plan
// or the host deletes it, without the plan's title (you can't open it any
// more). A stop removed from its plan takes its time and place along, so
// its ticket has nothing to list.
func TestMyTicketsOutliveThePlan(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	host, guest := srv.Signup(t, "Hana Host"), srv.Signup(t, "Gus Guest")
	evening := time.Date(2026, 9, 26, 18, 0, 0, 0, ny)
	joined := insertPlan(t, srv, "Pier crawl", host, []*testutil.Session{guest}, ticketStop{"Harbor cruise", "Harbor Pier", evening, 90})
	deleted := insertPlan(t, srv, "Rooftop night", host, nil, ticketStop{"Rooftop cinema", "Skyline Park", evening.Add(3 * time.Hour), 120})
	edited := insertPlan(t, srv, "Gallery hop", host, nil,
		ticketStop{"Print fair", "Old Customs House", evening.Add(24 * time.Hour), 60}, ticketStop{"Late gallery", "North Wing", evening.Add(26 * time.Hour), 60})
	book(t, srv, guest, joined, joined.Items[0], "tk-guest", 2)
	book(t, srv, host, deleted, deleted.Items[0], "tk-rooftop", 1)
	book(t, srv, host, edited, edited.Items[0], "tk-print", 1)
	book(t, srv, host, edited, edited.Items[1], "tk-late", 1)

	srv.Do(t, "POST", "/itineraries/"+joined.ID+"/leave", nil, guest).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", "/itineraries/"+deleted.ID, nil, host).Expect(t, http.StatusNoContent)
	srv.Do(t, "PATCH", "/itineraries/"+edited.ID, contract.ItineraryUpdate{StopOrder: []string{edited.Items[1].ID}}, host).Expect(t, http.StatusOK)

	var raw []map[string]json.RawMessage
	srv.Do(t, "GET", "/me/tickets", nil, guest).Expect(t, http.StatusOK).JSON(t, &raw)
	if len(raw) != 1 || string(raw[0]["title"]) != `"Harbor cruise"` || string(raw[0]["itinerary_id"]) != `"`+joined.ID+`"` {
		t.Fatalf("after leaving: %v", raw)
	}
	if title, ok := raw[0]["itinerary_title"]; ok {
		t.Errorf("a plan you left has no title for you: %s", title)
	}

	got := myTickets(t, srv, host)
	if ids := ticketIDs(got); !slices.Equal(ids, []string{"tk-rooftop", "tk-late"}) {
		t.Fatalf("host's tickets %v, want the deleted plan's and the stop still on its plan", ids)
	}
	if got[0].ItineraryTitle != nil || got[0].Title != "Rooftop cinema" {
		t.Errorf("deleted plan: %+v", got[0])
	}
	if got[1].ItineraryTitle == nil || *got[1].ItineraryTitle != "Gallery hop" {
		t.Errorf("edited plan keeps its title: %+v", got[1])
	}
}

func TestMyTicketsNeedASession(t *testing.T) {
	srv := testutil.New(t)
	srv.Do(t, "GET", "/me/tickets", nil, nil).Expect(t, http.StatusUnauthorized)
}
