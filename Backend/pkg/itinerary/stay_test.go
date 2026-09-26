package itinerary

import (
	"Backend/pkg/models"
	"testing"
	"time"
)

func withTags(a models.Activity, category string, tags ...string) models.Activity {
	a.Category, a.Tags = category, tags
	return a
}

func TestClippable(t *testing.T) {
	ev := event("x", techSquare, at(19, 0), 60, 0.8)
	cases := []struct {
		act  models.Activity
		want bool
	}{
		{withTags(ev, "live_music", "music"), true},
		{withTags(ev, "market", "food"), true},
		{withTags(ev, "sports_event", "group"), true},
		{withTags(ev, "community_event", "active", "group"), false},        // a dive, a round-robin
		{withTags(ev, "community_event", "active", "solo_friendly"), true}, // open play
		{withTags(ev, "tour", "outdoor"), false},
		{withTags(ev, "class_workshop"), false},
		{withTags(ev, "theater"), false},
		{withTags(ev, "cinema"), false},
		{withTags(place("Bar", "bar", techSquare, nil, 0.8), "bar"), false},
	}
	for _, c := range cases {
		if got := Clippable(&c.act); got != c.want {
			t.Errorf("%s %v: clippable %v, want %v", c.act.Category, c.act.Tags, got, c.want)
		}
	}
}

func TestStayFor(t *testing.T) {
	cfg := DefaultConfig()
	jazz := withEnd(withTags(event("Jazz", techSquare, at(18, 30), 150, 0.8), "live_music"), at(21, 0))
	if st, _ := StayFor(&jazz, cfg); st.Kind != StayClipped || !st.End.Equal(at(21, 0).UTC()) || st.MinStay != time.Hour {
		t.Errorf("jazz: %+v", st)
	}
	short := withEnd(withTags(event("Poetry", techSquare, at(19, 30), 75, 0.8), "community_event"), at(20, 45))
	if st, _ := StayFor(&short, cfg); st.MinStay != 37*time.Minute+30*time.Second {
		t.Errorf("a 75-minute event needs half of it: %v", st.MinStay)
	}
	noEnd := withTags(event("Set", techSquare, at(20, 0), 90, 0.8), "live_music")
	noEnd.Duration.P75Min = 120
	if st, _ := StayFor(&noEnd, cfg); !st.End.Equal(at(22, 0).UTC()) {
		t.Errorf("no published end: the p75 length, got %v", st.End.In(ny))
	}
	play := event("Play", techSquare, at(19, 0), 150, 0.8)
	if st, _ := StayFor(&play, cfg); st.Kind != StayWhole || st.MinStay != 0 || !st.End.Equal(at(21, 30).UTC()) {
		t.Errorf("play: %+v", st)
	}
	market := dropIn(withTags(event("Market", techSquare, at(12, 0), 60, 0.8), "market"), at(18, 0))
	if st, _ := StayFor(&market, cfg); st.Kind != StayWindow || st.MinStay != time.Hour {
		t.Errorf("market: %+v", st)
	}
	if _, ok := StayFor(&models.Activity{Kind: "event"}, cfg); ok {
		t.Error("no start, no stay")
	}
}

// A set that ends at back-by is joined and left early enough to get home;
// the stays share one series, keep their fixed start and never run late.
func TestClippedStayEndsInTimeToGetHome(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 30), at(21, 0))
	jazz := withEnd(withTags(event("Jazz", offset(1.2, 0), at(18, 30), 150, 0.8), "live_music"), at(21, 0))
	nodes, drops := BuildNodes(w, []models.Activity{jazz}, cfg)
	if len(nodes) == 0 {
		t.Fatalf("no stays: %+v", drops)
	}
	starts := map[time.Time]bool{}
	for _, n := range nodes {
		starts[n.Start] = true
		if n.Flexible || !n.P75End.Equal(n.End) || n.series != nodes[0].series {
			t.Errorf("stay %v–%v: flexible %v, p75 %v", n.Start.In(ny), n.End.In(ny), n.Flexible, n.P75End.In(ny))
		}
		if n.End.Sub(n.Start) < time.Hour || n.End.After(at(21, 0)) {
			t.Errorf("stay %v–%v", n.Start.In(ny), n.End.In(ny))
		}
	}
	if len(starts) != 2 || !starts[at(18, 30).UTC()] || !starts[at(18, 45).UTC()] {
		t.Errorf("starts %v, want 18:30 and 18:45", starts)
	}
	its, _ := plan(w, []models.Activity{jazz}, cfg)
	if len(its) == 0 {
		t.Fatal("the set alone should make a plan")
	}
	best := its[0]
	if best.Arrival.After(w.BackBy) || best.LateRisk {
		t.Errorf("home %v (late risk %v)", best.Arrival.In(ny), best.LateRisk)
	}
	if end := best.Stops[0].Node.End; end.Before(at(20, 30)) {
		t.Errorf("stays until %v: the last stay home in time is later", end.In(ny))
	}
}

// A start the walk cannot make is joined up to 15 minutes late, never
// later; an event that began before that is not a candidate.
func TestClippedStayLateArrival(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 0), at(21, 0))
	shanty := withEnd(withTags(event("Shanty", offset(0.75, 0), at(17, 0), 120, 0.8), "live_music"), at(19, 0))
	its, _ := plan(w, []models.Activity{shanty}, cfg)
	if len(its) == 0 || !its[0].Stops[0].Node.Start.Equal(at(17, 15).UTC()) {
		t.Fatalf("want the 17:15 stay, got %v", its)
	}
	early := withEnd(withTags(event("Early", offset(0.2, 0), at(16, 40), 120, 0.8), "live_music"), at(18, 40))
	if nodes, drops := BuildNodes(w, []models.Activity{early}, cfg); len(nodes) != 0 || drops[0].Reason != "outside_window" {
		t.Errorf("began 20 minutes before the window: %d nodes, %+v", len(nodes), drops)
	}
}

func TestStopBonusOnlyAboveTheBar(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(22, 0))
	good := withTags(event("Good", techSquare, at(19, 0), 60, 0.9), "comedy")
	weak := withTags(event("Weak", techSquare, at(20, 30), 60, 0.4), "theater")
	nodes, _ := BuildNodes(w, []models.Activity{good, weak}, cfg)
	for _, n := range nodes {
		switch n.Act.Name {
		case "Good": // (0.9 − 0.5) / 0.5 + the balanced bonus
			if !approxEq(n.Utility, 0.8+cfg.Paces["balanced"].StopBonus) {
				t.Errorf("good stop utility %v", n.Utility)
			}
		case "Weak":
			if n.Utility != 0 {
				t.Errorf("a stop below the bar gets no bonus: %v", n.Utility)
			}
		}
	}
	cfg.ExtraStopBonus = 0.05
	w.Pace = "packed"
	nodes, _ = BuildNodes(w, []models.Activity{good}, cfg)
	if want := 0.8 + cfg.Paces["packed"].StopBonus + 0.05; !approxEq(nodes[0].Utility, want) {
		t.Errorf("packed with an extra bonus: %v, want %v", nodes[0].Utility, want)
	}
}

func approxEq(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// Flexible visits start on arrival from the start point as well as on the
// grid, come in a short form when an hour or longer, and a meal is cut to
// three quarters of its length at most, never halved.
func TestSlotAnchorsAndShortVisits(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(17, 30), at(21, 0))
	park := place("Park", "park", offset(0.85, 0), daily(6, 22), 0.9)
	diner := place("Diner", "restaurant", offset(0.5, 0), daily(11, 19), 0.9)
	diner.Duration = &models.ActivityDuration{MedianMin: 80, P75Min: 90}
	nodes, _ := BuildNodes(w, []models.Activity{park, diner}, cfg)
	arrival := false
	lengths := map[string]map[time.Duration]bool{"Park": {}, "Diner": {}}
	for _, n := range nodes {
		d := n.End.Sub(n.Start)
		lengths[n.Act.Name][d] = true
		if n.Act.Name == "Park" && n.Start.Equal(at(17, 45).UTC()) {
			arrival = true
		}
		if n.Act.Name == "Diner" && d < 60*time.Minute {
			t.Errorf("diner visit of %v", d)
		}
	}
	if !arrival {
		t.Error("no park visit starting on arrival (17:30 plus the walk, rounded to 17:45)")
	}
	if !lengths["Park"][time.Hour] || !lengths["Park"][30*time.Minute] {
		t.Errorf("park lengths %v, want 1 h and the 30 min short form", lengths["Park"])
	}
	if !lengths["Diner"][80*time.Minute] || !lengths["Diner"][60*time.Minute] {
		t.Errorf("diner lengths %v: whole visits, and one cut at the 19:00 close", lengths["Diner"])
	}
}

func TestDominantKeepsTheBestOfEachVisitedSet(t *testing.T) {
	var a, b mask
	a = a.with(1)
	b = b.with(2)
	ls := []*label{
		{utility: 1, series: a, stops: 1},
		{utility: 2, series: a, stops: 1}, // same set, better
		{utility: 0.5, series: b, stops: 1},
		{utility: 3, series: a, stops: 1, cost: 500}, // costs more: not comparable
	}
	got := dominant(ls)
	if len(got) != 3 || got[0].utility != 2 || got[1].utility != 0.5 || got[2].utility != 3 {
		for _, l := range got {
			t.Logf("%+v", *l)
		}
		t.Errorf("dominant kept %d labels", len(got))
	}
}
