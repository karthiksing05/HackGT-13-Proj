package itineraries

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"fmt"
	"testing"
	"time"
)

var newYork = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return loc
}()

func local(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, newYork)
}

func sequentialIDs() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("new-%d", n) }
}

// The mock's retimed steps a leg and its stop past a calendar block they would overlap.
func TestRetimeStepsAroundBusyBlocks(t *testing.T) {
	it := &models.Itinerary{
		Start:      local(9, 26, 13, 0),
		StartPlace: models.PlaceDoc{Name: "Home"},
		Items: []models.ItineraryItem{
			{ID: "s1", Kind: models.ItemStop, Title: "Museum", Place: &models.PlaceDoc{Name: "Museum"}, Start: local(9, 26, 13, 20), End: local(9, 26, 14, 20)},
			{ID: "busy", Kind: kindBusy, Title: "Call with family", Start: local(9, 26, 14, 0), End: local(9, 26, 15, 0)},
			{ID: "s2", Kind: models.ItemStop, Title: "Coffee", Start: local(9, 26, 14, 30), End: local(9, 26, 15, 0)},
		},
	}
	got := retime(it, nil, sequentialIDs())
	want := []struct {
		id, title, place string
		start, end       time.Time
	}{
		{"busy", "Call with family", "", local(9, 26, 14, 0), local(9, 26, 15, 0)},
		{"new-1", "Walk to Museum", "Home → Museum", local(9, 26, 15, 0), local(9, 26, 15, 15)},
		{"s1", "Museum", "Museum", local(9, 26, 15, 15), local(9, 26, 16, 15)},
		{"new-2", "Walk to Coffee", "Museum → Coffee", local(9, 26, 16, 15), local(9, 26, 16, 30)},
		{"s2", "Coffee", "", local(9, 26, 16, 30), local(9, 26, 17, 0)},
	}
	if len(got) != len(want) {
		t.Fatalf("%d items: %+v", len(got), got)
	}
	for i, w := range want {
		place := ""
		if got[i].Place != nil {
			place = got[i].Place.Name
		}
		if got[i].ID != w.id || got[i].Title != w.title || place != w.place || !got[i].Start.Equal(w.start) || !got[i].End.Equal(w.end) {
			t.Errorf("item %d = %s %q %q %s–%s, want %+v", i, got[i].ID, got[i].Title, place, got[i].Start, got[i].End, w)
		}
	}
	if got[1].Kind != models.ItemTransit || got[1].LegMode != "walk" || got[1].LegMinutes != defaultLegMinutes || got[1].Description != walkNote {
		t.Errorf("leg: %+v", got[1])
	}
	// Retiming again keeps the legs between the same stops.
	it.Items = got
	again := retime(it, nil, sequentialIDs())
	if again[1].ID != "new-1" || again[3].ID != "new-2" {
		t.Errorf("leg ids not kept: %s %s", again[1].ID, again[3].ID)
	}
}

// A day move keeps local wall-clock times across the end of daylight saving time.
func TestEditMovesDaysAcrossDST(t *testing.T) {
	lock := local(10, 31, 17, 0)
	it := &models.Itinerary{
		Date: local(10, 31, 0, 0), DateKey: "2026-10-31", Start: local(10, 31, 18, 0), BackBy: local(10, 31, 21, 0), LockAt: &lock,
		StartPlace: models.PlaceDoc{Name: "Home"},
		Items: []models.ItineraryItem{
			{ID: "leg", Kind: models.ItemTransit, Title: "Walk to Show", Start: local(10, 31, 18, 0), End: local(10, 31, 18, 15)},
			{ID: "show", Kind: models.ItemStop, Title: "Show", Start: local(10, 31, 18, 15), End: local(10, 31, 20, 0)},
		},
	}
	set, err := edit(it, contract.ItineraryUpdate{Date: contract.Ptr(local(11, 2, 0, 0))}, newYork, local(10, 30, 12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !set["date"].(time.Time).Equal(local(11, 2, 0, 0)) || set["dateKey"] != "2026-11-02" ||
		!set["start"].(time.Time).Equal(local(11, 2, 18, 0)) || !set["backBy"].(time.Time).Equal(local(11, 2, 21, 0)) ||
		!set["lockAt"].(time.Time).Equal(local(11, 2, 17, 0)) || set["status"] != models.ItineraryActive {
		t.Fatalf("moved across DST: %v", set)
	}
	items := set["items"].([]models.ItineraryItem)
	if !items[1].Start.Equal(local(11, 2, 18, 15)) || !items[1].End.Equal(local(11, 2, 20, 0)) || items[0].ID != "leg" {
		t.Fatalf("items after the move: %+v", items)
	}
	// The same day is not a move; only the re-time happens.
	set, err = edit(it, contract.ItineraryUpdate{Date: contract.Ptr(local(10, 31, 0, 0))}, newYork, local(10, 30, 12, 0))
	if err != nil || set["date"] != nil || !set["start"].(time.Time).Equal(it.Start) {
		t.Fatalf("same day: %v %v", set, err)
	}
	// A title alone is not a re-time, so the status stays as it is.
	title := "Old show"
	set, err = edit(it, contract.ItineraryUpdate{Title: &title}, newYork, local(11, 5, 0, 0))
	if err != nil || set["title"] != "Old show" || set["status"] != nil || set["items"] != nil {
		t.Fatalf("a title edit does not re-time: %v %v", set, err)
	}
	// A re-time that ends before now makes it past.
	set, err = edit(it, contract.ItineraryUpdate{Start: contract.Ptr(local(10, 31, 17, 30))}, newYork, local(11, 5, 0, 0))
	if err != nil || set["status"] != models.ItineraryPast {
		t.Fatalf("re-timed into the past: %v %v", set, err)
	}
}

func TestLocalDay(t *testing.T) {
	for in, want := range map[string]string{
		"2026-09-26":           "2026-09-26", // day-only: that date anywhere
		"2026-09-26T04:00:00Z": "2026-09-26", // the app's local midnight
		"2026-09-27T03:30:00Z": "2026-09-26", // 11:30 PM in New York
		"2026-09-26T14:10:00Z": "2026-09-26",
	} {
		parsed, err := contract.ParseTime(in)
		if err != nil {
			t.Fatal(err)
		}
		y, m, d := localDay(parsed, newYork)
		if got := fmt.Sprintf("%04d-%02d-%02d", y, m, d); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

func TestInsightsTieBreaks(t *testing.T) {
	five := &contract.Rating{Stars: 5, Tags: []string{"b", "a"}}
	four := &contract.Rating{Stars: 4, Tags: []string{"a"}}
	past := []contract.PastEvent{
		{ID: "1", Title: "First five", Place: "Zoo", Company: "solo", Date: contract.NewTime(local(9, 20, 9, 0)), Rating: five},
		{ID: "2", Title: "Second five", Place: "Art", Company: "with 1 other", Date: contract.NewTime(local(9, 19, 19, 0)), Rating: five},
		{ID: "3", Title: "A four", Place: "Zoo", Company: "with 2 others", Date: contract.NewTime(local(9, 18, 19, 0)), Rating: four},
		{ID: "4", Title: "Unrated", Place: "Zoo", Company: "solo", Date: contract.NewTime(local(9, 17, 19, 0))},
	}
	got := insights(past, newYork)
	if got.Headline != "You like evening sidequests with friends." || got.BasedOn != 3 ||
		got.Highlights[0].Value != "Evenings" || *got.Highlights[0].Detail != "2 of your 3 favorites" ||
		got.Highlights[2].Value != "Zoo" || got.Highlights[3].Value != "First five" || *got.Highlights[3].Detail != "5 stars" ||
		fmt.Sprint(got.TopTags) != "[a b]" {
		t.Fatalf("insights: %+v", got)
	}
	if one := insights(past[:1], newYork); *one.Highlights[0].Detail != "1 of your 1 favorite" || one.Headline != "You like morning sidequests on your own." {
		t.Fatalf("one favorite: %+v", one)
	}
}
