package planner

import (
	"Backend/pkg/contract"
	"strings"
	"testing"
	"time"
)

// hitSearch is the must-see search on Saturday 26 Sep at noon from
// Seaside Market, for an adult.
func hitSearch(q string) HitSearch {
	day := localAt(0, 0)
	near := seasideMkt
	return HitSearch{Query: q, Near: &near, From: day, To: day.AddDate(0, 0, 1), Now: localAt(12, 0), TZ: ny,
		AgeBracket: "21_plus", Limit: 50}
}

func hitTitles(hits []contract.ActivityHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Title
	}
	return out
}

func indexOf(titles []string, title string) int {
	for i, t := range titles {
		if t == title {
			return i
		}
	}
	return -1
}

func TestActivityHitsMatchAndRank(t *testing.T) {
	acts := saltlight(t)

	// A word of the name: the jazz, with every field the app shows.
	hits := ActivityHits(acts, hitSearch("JAZZ"))
	if len(hits) != 1 {
		t.Fatalf("jazz: %v", hitTitles(hits))
	}
	jazz := hits[0]
	if jazz.ID != jazzID || jazz.Kind != contract.StopKindEvent || jazz.Category != "live_music" ||
		jazz.Subtitle != "Live music · 6:30 PM · 0.7 mi" || jazz.Place.Name != "Pier Nine Bandstand" || jazz.Place.Coordinate == nil ||
		jazz.Start == nil || !jazz.Start.Equal(time.Date(2026, 9, 26, 22, 30, 0, 0, time.UTC)) || jazz.End == nil ||
		jazz.PriceCents == nil || *jazz.PriceCents != 1200 || jazz.DistanceMi == nil || *jazz.DistanceMi != 0.7 {
		t.Errorf("jazz hit %+v", jazz)
	}

	// Names that start with q, then names with a word that does, then the
	// rest (venue, category, tag); events by start before places by distance.
	titles := hitTitles(ActivityHits(acts, hitSearch("sea")))
	want := []string{
		"Seasick Standup at the Crow's Nest", "Seaside Market Hall", "Sea Cliff Staircase Challenge", // names starting with "sea": the event, then places by distance
		"Saturday Seaside Market", "Oracle of the Deep: Late-Night Data Seance", "Sweet Seaweed Vegan Kitchen", // a word starting with it: events by start, then the place
	}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("sea:\n got %v\nwant %v", titles, want)
	}
	// Only the category matches: after the names.
	titles = hitTitles(ActivityHits(acts, hitSearch("park")))
	if len(titles) < 3 || titles[0] != "Anchor Park" || indexOf(titles, "Tidepool Discovery Beach") < 1 || indexOf(titles, "Kelp Hollow Dog Beach") < 1 {
		t.Errorf("park: %v", titles)
	}

	// A category or tag, with spaces for underscores: every live_music
	// listing of the day and the places tagged music, never tomorrow's.
	titles = hitTitles(ActivityHits(acts, hitSearch("live music")))
	if len(titles) == 0 || titles[0] != "Sunset Jazz on Pier Nine" || indexOf(titles, "Sea Shanty Singalong at The Rusty Anchor") >= 0 {
		t.Errorf("live music: %v", titles)
	}
	if hits := ActivityHits(acts, hitSearch("zzzz")); len(hits) != 0 {
		t.Errorf("nothing matches zzzz: %v", hitTitles(hits))
	}
}

func TestActivityHitsSuggestions(t *testing.T) {
	acts := saltlight(t)
	hits := ActivityHits(acts, hitSearch(""))
	titles := hitTitles(hits)
	// The day's events not over at noon, by start (the 9–12 cleanup is
	// over), then places, the best-rated near Seaside Market first.
	wantEvents := []string{"Saturday Seaside Market", "Sunset Jazz on Pier Nine", "Oracle of the Deep: Late-Night Data Seance",
		"Seasick Standup at the Crow's Nest", "Barnacle Bash Silent Disco"}
	if len(titles) < len(wantEvents)+3 || strings.Join(titles[:len(wantEvents)], "|") != strings.Join(wantEvents, "|") {
		t.Fatalf("suggestions: %v", titles)
	}
	places := hits[len(wantEvents):]
	for i, h := range places {
		if h.Kind != contract.StopKindPlace || h.Start != nil || h.DistanceMi == nil {
			t.Errorf("place hit %+v", h)
		}
		if i > 0 && *places[i-1].DistanceMi*1.609344 <= suggestRadiusKm && *h.DistanceMi*1.609344 <= suggestRadiusKm {
			prev, cur := catalogByID(acts)[places[i-1].ID], catalogByID(acts)[h.ID]
			if floatOrNeg(prev.Rating) < floatOrNeg(cur.Rating) {
				t.Errorf("near places by rating: %s (%v) before %s (%v)", prev.Name, prev.Rating, cur.Name, cur.Rating)
			}
		}
	}
	// Places plans never visit are not suggested, nor closed ones.
	for _, title := range []string{"Lanternfall Cinema", "The Shipyard Makerspace", "Kelp Forest Kayak Cleanup"} {
		if indexOf(titles, title) >= 0 {
			t.Errorf("%s suggested", title)
		}
	}

	// Limit, and no distance without near.
	s := hitSearch("")
	s.Near, s.Limit = nil, 3
	if hits := ActivityHits(acts, s); len(hits) != 3 || hits[0].DistanceMi != nil || strings.Contains(hits[0].Subtitle, "mi") {
		t.Errorf("three without near: %+v", hits)
	}
}

func TestActivityHitsFollowTheRules(t *testing.T) {
	acts := saltlight(t)
	// Under 21: no 21+ show, no bar, no nightclub.
	s := hitSearch("")
	s.AgeBracket = "18_20"
	for _, h := range ActivityHits(acts, s) {
		if h.Category == "bar" || h.Category == "nightclub" || h.ID == standupID {
			t.Errorf("an 18-year-old is offered %s", h.Title)
		}
	}
	// Another day: Sunday's events, not Saturday's.
	s = hitSearch("")
	s.From, s.To = s.From.AddDate(0, 0, 1), s.To.AddDate(0, 0, 1)
	titles := hitTitles(ActivityHits(acts, s))
	if indexOf(titles, "Sunset Jazz on Pier Nine") >= 0 || indexOf(titles, "Harbor Dumpling Brunch Crawl") < 0 {
		t.Errorf("Sunday: %v", titles)
	}
	// Late on Saturday the jazz is over.
	s = hitSearch("jazz")
	s.Now = localAt(21, 30)
	if hits := ActivityHits(acts, s); len(hits) != 0 {
		t.Errorf("the jazz ended at 21:00: %v", hitTitles(hits))
	}
}
