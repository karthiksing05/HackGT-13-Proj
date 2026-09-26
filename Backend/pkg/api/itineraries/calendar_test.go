package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/testutil"
	"net/http"
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
