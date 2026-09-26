package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/testutil"
	"Backend/pkg/travel"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func notesOf(item contract.ItineraryItem) (string, string) {
	notes, scope := "", ""
	if item.Notes != nil {
		notes = *item.Notes
	}
	if item.NotesScope != nil {
		scope = string(*item.NotesScope)
	}
	return notes, scope
}

func TestNotesPrivateAndShared(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Notes")
	b := srv.Signup(t, "Bob Notes")
	c := srv.Signup(t, "Cara Notes")
	d := srv.Signup(t, "Dan Notes")
	it := create(t, srv, a, exampleRequest(t))
	addMember(t, srv, it.ID, b)
	addMember(t, srv, it.ID, c)
	stop := stops(it)[0]
	nested := "/itineraries/" + it.ID + "/items/" + stop.ID
	updatesTo := func(id string) int { return srv.EventCount(id, realtime.EventItineraryUpdated) }

	// Private: only Alice sees it, nobody is told.
	srv.Do(t, "PATCH", nested, map[string]any{"notes": "Bring cash", "notes_scope": "private"}, a).Expect(t, http.StatusNoContent)
	if n, s := notesOf(event(t, srv, a, stop.ID)); n != "Bring cash" || s != "private" {
		t.Fatalf("A's private note: %q %q", n, s)
	}
	if n, s := notesOf(event(t, srv, b, stop.ID)); n != "" || s != "" {
		t.Fatalf("B sees A's private note: %q %q", n, s)
	}
	if updatesTo(b.UserID) != 0 {
		t.Fatal("a private note is nobody else's news")
	}

	// Shared: everyone on the plan sees it; the others hear itinerary.updated.
	srv.Do(t, "PATCH", nested, map[string]any{"notes": "Meet at the fountain", "notes_scope": "shared"}, a).Expect(t, http.StatusNoContent)
	for _, sess := range []*testutil.Session{a, b, c} {
		if n, s := notesOf(event(t, srv, sess, stop.ID)); n != "Meet at the fountain" || s != "shared" {
			t.Fatalf("shared note for %s: %q %q", sess.User.Name, n, s)
		}
	}
	if updatesTo(a.UserID) != 0 || updatesTo(b.UserID) != 1 || updatesTo(c.UserID) != 1 {
		t.Fatalf("itinerary.updated after sharing: A %d B %d C %d", updatesTo(a.UserID), updatesTo(b.UserID), updatesTo(c.UserID))
	}
	bUpdate := itineraryEvents(t, srv, b.UserID, realtime.EventItineraryUpdated)
	if n, s := notesOf(stops(bUpdate[0])[0]); n != "Meet at the fountain" || s != "shared" || bUpdate[0].IsHost {
		t.Fatalf("B's itinerary.updated is rendered for B: %q %q host=%v", n, s, bUpdate[0].IsHost)
	}

	// B's own private note (scope omitted = private for a first note) wins for B only.
	srv.Do(t, "PATCH", "/events/"+stop.ID, map[string]any{"notes": "Bring a jacket"}, b).Expect(t, http.StatusNoContent)
	if n, s := notesOf(event(t, srv, b, stop.ID)); n != "Bring a jacket" || s != "" {
		t.Fatalf("B's own note: %q %q", n, s)
	}
	if n, _ := notesOf(event(t, srv, c, stop.ID)); n != "Meet at the fountain" {
		t.Fatalf("C still sees the shared note: %q", n)
	}

	// Editing the text without a scope keeps it shared.
	srv.Do(t, "PATCH", nested, map[string]any{"notes": "Meet at the north fountain"}, a).Expect(t, http.StatusNoContent)
	if n, s := notesOf(event(t, srv, c, stop.ID)); n != "Meet at the north fountain" || s != "shared" {
		t.Fatalf("shared note after an edit: %q %q", n, s)
	}

	// Switching to private takes the shared copy back.
	srv.Do(t, "PATCH", nested, map[string]any{"notes": "Only me now", "notes_scope": "private"}, a).Expect(t, http.StatusNoContent)
	if n, s := notesOf(event(t, srv, c, stop.ID)); n != "" || s != "" {
		t.Fatalf("C after A went private: %q %q", n, s)
	}
	if n, s := notesOf(event(t, srv, a, stop.ID)); n != "Only me now" || s != "private" {
		t.Fatalf("A after going private: %q %q", n, s)
	}
	// Clearing a note leaves nothing (the scope is remembered).
	srv.Do(t, "PATCH", nested, map[string]any{"notes": ""}, a).Expect(t, http.StatusNoContent)
	if n, s := notesOf(event(t, srv, a, stop.ID)); n != "" || s != "private" {
		t.Fatalf("A after clearing: %q %q", n, s)
	}

	srv.Do(t, "PATCH", nested, map[string]any{"notes": "x", "notes_scope": "everyone"}, a).Expect(t, http.StatusBadRequest)
	long := strings.Repeat("é", 2001)
	if res := srv.Do(t, "PATCH", nested, map[string]any{"notes": long, "notes_scope": "shared"}, a).Expect(t, http.StatusBadRequest); res.Message() != itineraries.MsgNoteTooLong {
		t.Fatalf("a long note: %s", res.Body)
	}
	srv.Do(t, "PATCH", nested, map[string]any{"notes": long[:2000*len("é")]}, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "PATCH", nested, map[string]any{"notes": "x"}, d).Expect(t, http.StatusNotFound)
	srv.Do(t, "PATCH", "/events/"+stop.ID, map[string]any{"notes": "x"}, d).Expect(t, http.StatusNotFound)
	srv.Do(t, "PATCH", "/itineraries/"+it.ID+"/items/nope", map[string]any{"notes": "x"}, a).Expect(t, http.StatusNotFound)
}

func TestTransitOptionsAndChoice(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Transit")
	b := srv.Signup(t, "Bob Transit")
	it := create(t, srv, a, exampleRequest(t))
	rooftop := stops(it)[0]
	from, to := it.StartPlace.Coordinate, rooftop.Place.Coordinate
	pa, pb := travel.Point{Lat: from.Lat, Lng: from.Lng}, travel.Point{Lat: to.Lat, Lng: to.Lng}
	minutes := func(mode travel.Mode) int { return int(travel.Estimate(pa, pb, mode).Duration / time.Minute) }

	check := func(path string) {
		t.Helper()
		var options []contract.TransitOption
		srv.Do(t, "GET", path, nil, a).Expect(t, http.StatusOK).JSON(t, &options)
		if len(options) != 3 ||
			options[0].Mode != contract.ModeWalk || options[0].Minutes != minutes(travel.Walk) || options[0].CostCents == nil || *options[0].CostCents != 0 ||
			options[1].Mode != contract.ModeMarta || options[1].Minutes != minutes(travel.Transit) || options[1].CostCents == nil || *options[1].CostCents != 250 ||
			options[2].Mode != contract.ModeRideshare || options[2].Minutes != minutes(travel.Drive) || options[2].CostCents != nil {
			t.Fatalf("%s: %+v", path, options)
		}
	}
	// To the first stop from the plan's start, by stop or by the leg that leads there.
	check("/itineraries/" + it.ID + "/items/" + rooftop.ID + "/transit")
	check("/events/" + rooftop.ID + "/transit")
	check("/events/" + it.Items[0].ID + "/transit")
	if minutes(travel.Walk) <= minutes(travel.Transit) {
		t.Fatalf("fixture should be far enough to ride: walk %d transit %d", minutes(travel.Walk), minutes(travel.Transit))
	}

	// Without coordinates: the mock's fixed 18 / 12 / 8.
	blind := create(t, srv, a, plan("No map", contract.VisibilityJustMe,
		stopSpec{id: "n0", title: "Somewhere", start: at(9, 25, 15, 0), minutes: 30}))
	var options []contract.TransitOption
	srv.Do(t, "GET", "/events/"+stops(blind)[0].ID+"/transit", nil, a).Expect(t, http.StatusOK).JSON(t, &options)
	raw, _ := json.Marshal(options)
	if string(raw) != `[{"mode":"walk","minutes":18,"cost_cents":0},{"mode":"marta","minutes":12,"cost_cents":250},{"mode":"rideshare","minutes":8}]` {
		t.Fatalf("defaults: %s", raw)
	}

	// The choice comes back on the item for the chooser only.
	srv.Do(t, "PUT", "/itineraries/"+it.ID+"/items/"+rooftop.ID+"/transit", map[string]string{"mode": "marta"}, a).Expect(t, http.StatusNoContent)
	if got := event(t, srv, a, rooftop.ID); got.TransitMode == nil || *got.TransitMode != contract.ModeMarta {
		t.Fatalf("transit_mode: %+v", got.TransitMode)
	}
	srv.Do(t, "PUT", "/events/"+rooftop.ID+"/transit", map[string]string{"mode": "rideshare"}, a).Expect(t, http.StatusNoContent)
	if got := stops(getItinerary(t, srv, a, it.ID))[0]; got.TransitMode == nil || *got.TransitMode != contract.ModeRideshare {
		t.Fatalf("transit_mode on the itinerary: %+v", got.TransitMode)
	}
	srv.Do(t, "PUT", "/events/"+rooftop.ID+"/transit", map[string]string{"mode": "jetpack"}, a).Expect(t, http.StatusBadRequest)
	srv.Do(t, "PUT", "/events/"+rooftop.ID+"/transit", map[string]string{"mode": "walk"}, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/events/"+rooftop.ID+"/transit", nil, b).Expect(t, http.StatusNotFound)
}

func TestEventDetail(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Event")
	b := srv.Signup(t, "Bob Event")
	it := create(t, srv, a, exampleRequest(t))
	want := it.Items[3]
	got := event(t, srv, a, want.ID)
	gotRaw, _ := json.Marshal(got)
	wantRaw, _ := json.Marshal(want)
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("GET /events/{id} differs from the itinerary's item\n got %s\nwant %s", gotRaw, wantRaw)
	}
	srv.Do(t, "GET", "/events/"+want.ID, nil, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/events/nope", nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/events/"+want.ID, nil, nil).Expect(t, http.StatusUnauthorized)
}

// TestTicketsAreSharedWithTheGroup: a ticket one member booked shows for
// everyone on the plan ("The group sees it too"); your own ticket wins;
// a member who leaves takes theirs along.
func TestTicketsAreSharedWithTheGroup(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Ticket")
	b := srv.Signup(t, "Bob Ticket")
	c := srv.Signup(t, "Cara Ticket")
	d := srv.Signup(t, "Dan Ticket")
	it := create(t, srv, a, exampleRequest(t))
	addMember(t, srv, it.ID, b)
	addMember(t, srv, it.ID, c)
	rooftop := stops(it)[0]
	book := func(sess *testutil.Session, id string, quantity int) {
		t.Helper()
		total, confirmation, url := 1800*quantity, "SQ-"+id, "http://api.test/tickets/"+id
		ticket := models.ItemTicket{ID: id, Quantity: quantity, TotalCents: &total, Confirmation: &confirmation, URL: &url}
		if err := srv.Store.CheckoutReads().SaveTicket(context.Background(), sess.UserID, it.ID, rooftop.ID, ticket); err != nil {
			t.Fatal(err)
		}
	}
	ticketFor := func(sess *testutil.Session) (string, string) {
		t.Helper()
		onPlan, onEvent := stops(getItinerary(t, srv, sess, it.ID))[0].Ticket, event(t, srv, sess, rooftop.ID).Ticket
		id := func(tk *contract.Ticket) string {
			if tk == nil {
				return ""
			}
			return tk.ID
		}
		return id(onPlan), id(onEvent)
	}

	book(a, "TA", 2)
	for _, sess := range []*testutil.Session{a, b, c} {
		if plan, ev := ticketFor(sess); plan != "TA" || ev != "TA" {
			t.Fatalf("%s sees %q / %q, want A's ticket", sess.User.Name, plan, ev)
		}
	}
	full := event(t, srv, b, rooftop.ID).Ticket
	if full.Quantity != 2 || full.TotalCents == nil || *full.TotalCents != 3600 || full.Confirmation == nil || *full.Confirmation != "SQ-TA" ||
		full.URL == nil || *full.URL != "http://api.test/tickets/TA" {
		t.Fatalf("the shared ticket is the whole ticket: %+v", full)
	}
	if other := stops(getItinerary(t, srv, b, it.ID))[1]; other.Ticket != nil {
		t.Fatalf("an item nobody booked has no ticket: %+v", other.Ticket)
	}
	srv.Do(t, "GET", "/itineraries/"+it.ID, nil, d).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/events/"+rooftop.ID, nil, d).Expect(t, http.StatusNotFound)
	// Notes and travel choices land on the same state the checkout agent wrote.
	srv.Do(t, "PATCH", "/events/"+rooftop.ID, map[string]any{"notes": "Tickets are in my wallet"}, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "PUT", "/events/"+rooftop.ID+"/transit", map[string]string{"mode": "walk"}, a).Expect(t, http.StatusNoContent)
	if mine := event(t, srv, a, rooftop.ID); mine.Ticket == nil || mine.Ticket.ID != "TA" || mine.Notes == nil || mine.TransitMode == nil {
		t.Fatalf("A's state after notes and transit: %+v", mine)
	}

	// B books too, later: B and C see B's; A keeps their own.
	srv.Clock.Advance(time.Minute)
	book(b, "TB", 1)
	for sess, want := range map[*testutil.Session]string{a: "TA", b: "TB", c: "TB"} {
		if plan, ev := ticketFor(sess); plan != want || ev != want {
			t.Errorf("%s sees %q / %q, want %s", sess.User.Name, plan, ev, want)
		}
	}
	// Once B leaves, their ticket is no longer the group's.
	srv.Do(t, "POST", "/itineraries/"+it.ID+"/leave", nil, b).Expect(t, http.StatusNoContent)
	if plan, ev := ticketFor(c); plan != "TA" || ev != "TA" {
		t.Errorf("after B left, C sees %q / %q, want A's", plan, ev)
	}
}
