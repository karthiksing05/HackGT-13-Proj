package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// dayItems maps each calendar day id to its item titles.
func dayItems(days []contract.CalendarDay) map[string][]string {
	out := map[string][]string{}
	for _, d := range days {
		titles := []string{}
		for _, item := range d.Items {
			titles = append(titles, item.Title)
		}
		out[d.ID] = titles
	}
	return out
}

func TestCalendarDays(t *testing.T) {
	saturdayNoon := time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)
	srv := testutil.New(t, testutil.WithNow(saturdayNoon))
	a := srv.Signup(t, "Alice Calendar")
	b := srv.Signup(t, "Bob Calendar")
	// 11:30 PM Saturday in New York is already Sunday in UTC.
	late := create(t, srv, a, plan("Late show", contract.VisibilityJustMe,
		stopSpec{id: "l0", title: "Late show", start: at(9, 26, 23, 30), minutes: 60}))
	create(t, srv, a, plan("Midnight snack", contract.VisibilityJustMe,
		stopSpec{id: "m0", title: "Midnight snack", start: at(9, 27, 0, 30), minutes: 30}))
	brunch := create(t, srv, a, plan("Brunch", contract.VisibilityFriends,
		stopSpec{id: "b0", title: "Brunch", start: at(9, 28, 10, 0), minutes: 90},
		stopSpec{id: "b1", title: "Walk it off", start: at(9, 28, 11, 45), minutes: 45}))
	addMember(t, srv, brunch.ID, b)
	create(t, srv, a, plan("Next week", contract.VisibilityJustMe,
		stopSpec{id: "n0", title: "Farmers market", start: at(10, 5, 9, 0), minutes: 60}))

	var days []contract.CalendarDay
	srv.Do(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	if len(days) != 3 || days[0].ID != "2026-09-26" || days[2].ID != "2026-09-28" ||
		!days[0].Date.Equal(time.Date(2026, 9, 26, 4, 0, 0, 0, time.UTC)) || !days[2].Date.Equal(time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("days: %+v", days)
	}
	got := dayItems(days)
	if len(got["2026-09-26"]) != 1 || got["2026-09-26"][0] != "Late show" ||
		len(got["2026-09-27"]) != 1 || got["2026-09-27"][0] != "Midnight snack" ||
		len(got["2026-09-28"]) != 2 || got["2026-09-28"][0] != "Brunch" || got["2026-09-28"][1] != "Walk it off" {
		t.Fatalf("items by New York day: %v", got)
	}
	show := days[0].Items[0]
	if show.Kind != contract.KindSidequest || show.ItineraryID == nil || *show.ItineraryID != late.ID || len(show.People) != 0 ||
		!show.Start.Equal(time.Date(2026, 9, 27, 3, 30, 0, 0, time.UTC)) {
		t.Fatalf("late show block: %+v", show)
	}
	group := days[2].Items[0]
	if group.Kind != contract.KindGroup || len(group.People) != 2 || group.People[0].ID != a.UserID || group.People[1].ID != b.UserID ||
		group.ItineraryID == nil || *group.ItineraryID != brunch.ID {
		t.Fatalf("group block: %+v", group)
	}

	// The same plans seen from Berlin fall on Berlin days.
	srv.DoRaw(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, map[string]string{
		"Authorization": a.Bearer(), "X-Time-Zone": "Europe/Berlin",
	}).Expect(t, http.StatusOK).JSON(t, &days)
	got = dayItems(days)
	if len(got["2026-09-26"]) != 0 || len(got["2026-09-27"]) != 2 || got["2026-09-27"][0] != "Late show" ||
		!days[0].Date.Equal(time.Date(2026, 9, 25, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("Berlin days: %v (%s)", got, days[0].Date)
	}

	// Default range: today and the next 13 days, empty days included.
	srv.Do(t, "GET", "/calendar/days", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	if len(days) != 14 || days[0].ID != "2026-09-26" || days[13].ID != "2026-10-09" || days[3].Items == nil {
		t.Fatalf("default range: %d days %s…%s", len(days), days[0].ID, days[len(days)-1].ID)
	}
	if got := dayItems(days); len(got["2026-10-05"]) != 1 || len(got["2026-09-29"]) != 0 {
		t.Fatalf("default range items: %v", got)
	}

	// B, a member of the brunch only, sees just that.
	srv.Do(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, b).Expect(t, http.StatusOK).JSON(t, &days)
	if got := dayItems(days); len(got["2026-09-26"]) != 0 || len(got["2026-09-27"]) != 0 || len(got["2026-09-28"]) != 2 {
		t.Fatalf("B's calendar: %v", got)
	}

	// Days across the end of daylight saving time stay whole local days.
	srv.Do(t, "GET", "/calendar/days?from=2026-10-31&to=2026-11-02", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	if len(days) != 3 || days[1].ID != "2026-11-01" || !days[1].Date.Equal(time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)) ||
		!days[2].Date.Equal(time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("DST days: %+v", days)
	}

	srv.Do(t, "GET", "/calendar/days?from=2026-09-26&to=2026-11-26", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	if len(days) != 62 {
		t.Fatalf("62-day range: %d", len(days))
	}
	for q, msg := range map[string]string{
		"from=2026-09-26&to=2026-11-27": itineraries.MsgCalendarRange, // 63 days
		"from=2026-09-28&to=2026-09-26": itineraries.MsgCalendarRange,
		"from=soon":                     httpx.GenericBadRequest,
		"to=2026-13-01":                 httpx.GenericBadRequest,
	} {
		if res := srv.Do(t, "GET", "/calendar/days?"+q, nil, a).Expect(t, http.StatusBadRequest); res.Message() != msg {
			t.Errorf("%s: %q, want %q", q, res.Message(), msg)
		}
	}
}

// busyEvent puts a block on a user's calendar, as a connected calendar or
// the showcase seed would.
func busyEvent(t *testing.T, srv *testutil.Server, userID, title string, start, end time.Time) *models.CalendarEvent {
	t.Helper()
	ev := &models.CalendarEvent{UserID: userID, Title: title, Start: start.UTC(), End: end.UTC(), Location: "Klaus Advanced Computing Building",
		Source: "seed", SeriesID: "series-" + title, Seed: "calendar-v1"}
	if err := srv.Store.CalendarEvents().Insert(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestCalendarDaysListTheViewersBusyBlocks(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)))
	a := srv.Signup(t, "Alice Busy")
	b := srv.Signup(t, "Bob Busy")
	math := busyEvent(t, srv, a.UserID, "MATH 3012", at(9, 28, 9, 30), at(9, 28, 10, 45))
	busyEvent(t, srv, a.UserID, "CS 3510 lecture", at(9, 28, 13, 0), at(9, 28, 13, 50))
	busyEvent(t, srv, a.UserID, "Night shift", at(9, 26, 22, 0), at(9, 27, 6, 30))
	busyEvent(t, srv, a.UserID, "Next month", at(10, 20, 9, 0), at(10, 20, 10, 0))
	busyEvent(t, srv, b.UserID, "Bob's lab", at(9, 28, 11, 0), at(9, 28, 12, 0))
	create(t, srv, a, plan("Lunch break", contract.VisibilityJustMe,
		stopSpec{id: "l0", title: "Tacos", start: at(9, 28, 11, 15), minutes: 60}))

	var days []contract.CalendarDay
	srv.Do(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	got := dayItems(days)
	want := map[string][]string{
		"2026-09-26": {"Night shift"}, // on both days it runs into
		"2026-09-27": {"Night shift"},
		"2026-09-28": {"MATH 3012", "Tacos", "CS 3510 lecture"}, // in time order with the stops
	}
	for day, titles := range want {
		if strings.Join(got[day], "|") != strings.Join(titles, "|") {
			t.Errorf("%s: %v, want %v", day, got[day], titles)
		}
	}
	block := days[2].Items[0]
	if block.ID != math.ID || block.Kind != contract.KindBusy || block.ItineraryID != nil || block.People == nil || len(block.People) != 0 ||
		block.Interested == nil || !block.Start.Equal(math.Start) || !block.End.Equal(math.End) {
		t.Fatalf("busy block: %+v", block)
	}

	// Seen from Berlin, the night shift is one Sunday morning block.
	srv.DoRaw(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, map[string]string{
		"Authorization": a.Bearer(), "X-Time-Zone": "Europe/Berlin",
	}).Expect(t, http.StatusOK).JSON(t, &days)
	if got := dayItems(days); len(got["2026-09-26"]) != 0 || strings.Join(got["2026-09-27"], "|") != "Night shift" {
		t.Errorf("Berlin days: %v", got)
	}

	// Bob sees his own lab and nothing of Alice's.
	srv.Do(t, "GET", "/calendar/days?from=2026-09-26&to=2026-09-28", nil, b).Expect(t, http.StatusOK).JSON(t, &days)
	if got := dayItems(days); len(got["2026-09-26"]) != 0 || len(got["2026-09-27"]) != 0 || strings.Join(got["2026-09-28"], "|") != "Bob's lab" {
		t.Errorf("Bob's calendar: %v", got)
	}

	// Tapping the block opens it: what it is, where, and the viewer's own
	// note, which stays theirs.
	item := event(t, srv, a, math.ID)
	if item.Kind != contract.KindBusy || item.Title != "MATH 3012" || item.Place == nil || item.Place.Name != "Klaus Advanced Computing Building" ||
		item.Description == nil || *item.Description != "From your calendar. SideQuests plans around it." || item.Notes != nil ||
		!item.Start.Equal(math.Start) || item.People == nil {
		t.Fatalf("event: %+v", item)
	}
	shared := contract.NotesShared
	srv.Do(t, "PATCH", "/events/"+math.ID, contract.ItemNotesPatch{Notes: "Bring the problem set", NotesScope: &shared}, a).Expect(t, http.StatusNoContent)
	item = event(t, srv, a, math.ID)
	if item.Notes == nil || *item.Notes != "Bring the problem set" || item.NotesScope == nil || *item.NotesScope != contract.NotesPrivate {
		t.Fatalf("notes: %+v", item)
	}
	srv.Do(t, "GET", "/events/"+math.ID, nil, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "PATCH", "/events/"+math.ID, contract.ItemNotesPatch{Notes: "mine now"}, b).Expect(t, http.StatusNotFound)
}

// kindsAndTitles is an itinerary's items as "kind:title".
func kindsAndTitles(it contract.Itinerary) []string {
	out := []string{}
	for _, item := range it.Items {
		out = append(out, string(item.Kind)+":"+item.Title)
	}
	return out
}

func TestSavedPlansKeepTheBusyBlocksTheyWorkAround(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)))
	a := srv.Signup(t, "Alice Host")
	b := srv.Signup(t, "Bob Member")
	lecture := busyEvent(t, srv, a.UserID, "CS 3510 lecture", at(9, 28, 13, 0), at(9, 28, 13, 50))
	busyEvent(t, srv, a.UserID, "Office hours", at(9, 28, 15, 0), at(9, 28, 15, 30))
	busyEvent(t, srv, a.UserID, "Morning lab", at(9, 28, 9, 0), at(9, 28, 11, 0))    // before the plan's window
	busyEvent(t, srv, b.UserID, "Bob's seminar", at(9, 28, 12, 0), at(9, 28, 16, 0)) // someone else's
	lat, lng := coord(33.7766, -84.389)
	// 11:50–16:00: coffee, the lecture, a gallery, office hours, the way back.
	req := plan("Monday around class", contract.VisibilityOpen,
		stopSpec{id: "c0", title: "Coffee", lat: lat, lng: lng, start: at(9, 28, 12, 0), minutes: 45},
		stopSpec{id: "g0", title: "Gallery", lat: lat, lng: lng, start: at(9, 28, 14, 10), minutes: 50})
	it := create(t, srv, a, req)

	want := []string{"transit:Walk to Coffee", "sidequest:Coffee", "busy:CS 3510 lecture", "transit:Walk to Gallery",
		"sidequest:Gallery", "busy:Office hours", "transit:Walk to Home"}
	if got := kindsAndTitles(it); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("items:\n%v\nwant\n%v", got, want)
	}
	class := it.Items[2]
	if !class.Start.Equal(lecture.Start) || !class.End.Equal(lecture.End) || class.Place == nil || class.Place.Name != "Klaus Advanced Computing Building" ||
		class.Description == nil || *class.Description != "From your calendar. SideQuests plans around it." || class.ID == lecture.ID {
		t.Fatalf("busy item: %+v", class)
	}
	// Legs keep off the blocks: into the gallery once the lecture is over,
	// home once office hours are.
	for _, c := range []struct {
		i     int
		start time.Time
	}{{0, at(9, 28, 11, 50)}, {3, at(9, 28, 13, 50)}, {6, at(9, 28, 15, 30)}} {
		if leg := it.Items[c.i]; !leg.Start.Equal(c.start) || !leg.End.Equal(c.start.Add(10*time.Minute)) {
			t.Errorf("leg %d runs %v–%v, want from %v", c.i, leg.Start.In(ny), leg.End.In(ny), c.start.In(ny))
		}
	}

	// The calendar lists each block once, from the calendar itself.
	var days []contract.CalendarDay
	srv.Do(t, "GET", "/calendar/days?from=2026-09-28&to=2026-09-28", nil, a).Expect(t, http.StatusOK).JSON(t, &days)
	if got := dayItems(days)["2026-09-28"]; strings.Join(got, "|") != "Morning lab|Coffee|CS 3510 lecture|Gallery|Office hours" {
		t.Errorf("Alice's Monday: %v", got)
	}
	for _, item := range days[0].Items {
		if item.Kind == contract.KindBusy && (item.ItineraryID != nil || item.ID == class.ID) {
			t.Errorf("a plan's copy of a block is on the calendar: %+v", item)
		}
	}

	// A member sees the plan without the host's calendar in it.
	addMember(t, srv, it.ID, b)
	for _, item := range getItinerary(t, srv, b, it.ID).Items {
		if item.Kind == contract.KindBusy {
			t.Errorf("Bob sees Alice's %q", item.Title)
		}
	}
	srv.Do(t, "GET", "/events/"+class.ID, nil, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/itineraries/"+it.ID+"/items/"+class.ID+"/transit", nil, b).Expect(t, http.StatusNotFound)
	if got := event(t, srv, a, class.ID); got.Kind != contract.KindBusy || got.Title != "CS 3510 lecture" {
		t.Errorf("Alice opens her block: %+v", got)
	}
	srv.Do(t, "GET", "/calendar/days?from=2026-09-28&to=2026-09-28", nil, b).Expect(t, http.StatusOK).JSON(t, &days)
	if got := dayItems(days)["2026-09-28"]; strings.Join(got, "|") != "Bob's seminar|Coffee|Gallery" {
		t.Errorf("Bob's Monday: %v", got)
	}
	// The forum counts the plan's stops, not the host's classes.
	var page contract.Page[contract.ForumPost]
	srv.Do(t, "GET", "/forum/posts?lat=33.7766&lng=-84.389", nil, b).Expect(t, http.StatusOK).JSON(t, &page)
	posted := false
	for _, post := range page.Items {
		if post.ID == it.ID {
			posted = true
			if post.Route == nil || !strings.HasPrefix(*post.Route, "2 stops ·") {
				t.Errorf("forum route %v", post.Route)
			}
		}
	}
	if !posted {
		t.Errorf("the plan is not on Bob's forum: %d posts", len(page.Items))
	}
	// Nor does searching for Alice's class find it.
	var found contract.SearchResults
	srv.Do(t, "GET", "/search?q=3510", nil, b).Expect(t, http.StatusOK).JSON(t, &found)
	if len(found.Sidequests) != 0 {
		t.Errorf("Bob finds the plan by Alice's class: %d", len(found.Sidequests))
	}

	// Re-timed, the blocks stay where they are and the stops step around them.
	later := contract.NewTime(at(9, 28, 12, 20))
	var moved contract.Itinerary
	srv.Do(t, "PATCH", "/itineraries/"+it.ID, contract.ItineraryUpdate{Start: &later}, a).Expect(t, http.StatusOK).JSON(t, &moved)
	var blocks []contract.ItineraryItem
	for _, item := range moved.Items {
		if item.Kind == contract.KindBusy {
			blocks = append(blocks, item)
		}
	}
	if len(blocks) != 2 || !blocks[0].Start.Equal(lecture.Start) {
		t.Fatalf("blocks after the edit: %v", kindsAndTitles(moved))
	}
	for _, item := range moved.Items {
		for _, block := range blocks {
			if item.Kind != contract.KindBusy && item.Start.Before(block.End.Time) && item.End.After(block.Start.Time) {
				t.Errorf("%s %v–%v is on top of %s", item.Title, item.Start.In(ny), item.End.In(ny), block.Title)
			}
		}
	}
}
