package itinerary

import (
	"Backend/pkg/models"
	"math"
	"testing"
	"time"
)

func TestFixedEventNodes(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 0))
	noEnd := event("No end", techSquare, at(19, 0), 150, 0.8)
	withEndTime := withEnd(event("With end", techSquare, at(19, 0), 150, 0.8), at(20, 0))
	early := event("Too early", techSquare, at(17, 0), 60, 0.8)
	late := event("Runs late", techSquare, at(22, 0), 120, 0.8)
	absurd := event("Bad data", techSquare, at(18, 0), 60, 0.8)
	absurd.Duration.MedianMin = 5.5e16

	nodes, drops := BuildNodes(w, []models.Activity{noEnd, withEndTime, early, late, absurd}, cfg)
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (drops: %+v)", len(nodes), drops)
	}
	for _, n := range nodes {
		switch n.Act.Name {
		case "No end":
			if n.End.Sub(n.Start) != 150*time.Minute {
				t.Errorf("missing end should use the median: %v", n.End.Sub(n.Start))
			}
		case "With end":
			if !n.End.Equal(at(20, 0)) {
				t.Errorf("published end ignored: %v", n.End.In(ny))
			}
		}
	}
	// 5.5e16 minutes clamps to 6 h, which overruns 23:00, so it's dropped.
	reasons := map[string]int{}
	for _, d := range drops {
		reasons[d.Reason]++
	}
	if reasons["outside_window"] != 3 {
		t.Errorf("drops = %+v", drops)
	}
}

func TestDropInSlotsAreClippedAndShareASeries(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(22, 0))
	club := dropIn(event("Club night", techSquare, at(16, 0), 120, 0.8), at(23, 0)) // 16:00-23:00, 2 h visits
	nodes, _ := BuildNodes(w, []models.Activity{club}, cfg)
	if len(nodes) == 0 {
		t.Fatal("expected slots")
	}
	for _, n := range nodes {
		if n.Start.Before(w.From) || n.End.After(w.BackBy) {
			t.Errorf("slot %v-%v outside window", n.Start.In(ny), n.End.In(ny))
		}
		if n.series != nodes[0].series || !n.Flexible {
			t.Error("slots should share one series and be flexible")
		}
	}
	// 18:00, 18:30, 19:00, 19:30, 20:00 fit a 2 h visit before 22:00; the
	// short form (1 h) fits every start to 21:00; a visit from 20:30 is cut
	// at 22:00 (90 min, above the hour it must last).
	full, short, cut := 0, 0, 0
	for _, n := range nodes {
		switch d := n.End.Sub(n.Start); {
		case d == 2*time.Hour:
			full++
		case d == time.Hour:
			short++
		case n.End.Equal(w.BackBy) && d >= time.Hour:
			cut++
		default:
			t.Errorf("slot of %v", d)
		}
	}
	if full != 5 || short != 7 || cut != 1 {
		t.Errorf("got %d full, %d short and %d cut slots, want 5, 7 and 1", full, short, cut)
	}
}

func TestTimedEntrySlotsShareASeries(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(12, 0), at(22, 0))
	var acts []models.Activity
	for m := range 4 {
		a := withEnd(event("Balloon Museum", techSquare, at(13, m*15), 15, 0.9), at(13, m*15+15))
		a.VenueName = str("Balloon Museum NYC")
		acts = append(acts, a)
	}
	nodes, _ := BuildNodes(w, acts, cfg)
	for _, n := range nodes {
		if n.series != nodes[0].series {
			t.Fatal("timed-entry slots of one exhibition must share a series")
		}
	}
}

func TestPlaceNodes(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(22, 0))
	museum := place("Museum", "museum", techSquare, daily(10, 17), 0.9) // closed all evening
	bar := place("Bar", "bar", techSquare, daily(17, 26), 0.9)          // wraps past midnight
	noHoursBar := place("Mystery bar", "bar", techSquare, nil, 0.9)
	park := place("Park", "park", techSquare, nil, 0.9) // default 6-22
	cinema := place("Cinema", "cinema", techSquare, daily(10, 23), 0.9)

	nodes, drops := BuildNodes(w, []models.Activity{museum, bar, noHoursBar, park, cinema}, cfg)
	reasons := map[string]string{}
	for _, d := range drops {
		reasons[d.ActivityID] = d.Reason
	}
	if reasons[museum.ID.Hex()] != "closed_during_window" || reasons[noHoursBar.ID.Hex()] != "no_hours" ||
		reasons[cinema.ID.Hex()] != "category_excluded" {
		t.Errorf("drops = %+v", drops)
	}
	got := map[string]int{}
	for _, n := range nodes {
		got[n.Act.Name]++
		if n.Utility >= utility(&bar, cfg) {
			t.Error("place utility should be scaled down by PlaceWeight")
		}
	}
	if got["Bar"] == 0 || got["Park"] == 0 {
		t.Errorf("nodes by place = %v", got)
	}
}

func TestBudgetAndPrice(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 0))
	w.BudgetCents = 2000
	cheap := event("Cheap", techSquare, at(19, 0), 60, 0.8)
	cheap.Price = &models.ActivityPrice{Min: f64(10)}
	pricey := event("Pricey", techSquare, at(19, 0), 60, 0.8)
	pricey.Price = &models.ActivityPrice{Min: f64(45)}
	nodes, drops := BuildNodes(w, []models.Activity{cheap, pricey}, cfg)
	if len(nodes) != 1 || nodes[0].CostCents != 1000 {
		t.Fatalf("nodes %+v drops %+v", nodes, drops)
	}
	if drops[0].Reason != "over_budget" {
		t.Errorf("drop reason %s", drops[0].Reason)
	}
}

func TestUtility(t *testing.T) {
	cfg := DefaultConfig() // baseline 0.5
	a := event("x", techSquare, at(19, 0), 60, 0.9)
	cases := []struct {
		name   string
		score  *float64
		rerank *float64
		want   float64
	}{
		{"model 0.9", f64(0.9), nil, 0.8},
		{"model 1.0 is the top", f64(1.0), nil, 1.0},
		{"model 0.45, below the baseline", f64(0.45), nil, 0},
		{"rerank 3.5/4 wins over the model", f64(0.2), f64(3.5), 0.75},
		{"rerank 2/4 is the baseline", f64(0.9), f64(2), 0},
		{"rerank 1.66/4 adds nothing", f64(0.9), f64(1.66), 0},
		{"unscored", nil, nil, 0.2},
	}
	for _, c := range cases {
		a.Score, a.RerankScore = c.score, c.rerank
		if u := utility(&a, cfg); math.Abs(u-c.want) > 1e-9 {
			t.Errorf("%s: utility %.3f, want %.3f", c.name, u, c.want)
		}
	}
}

func TestDuplicateListingsShareASeries(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(18, 0), at(23, 0))
	a := event("Michelle Malone Band w/ Trina Meade & Co.", offset(1, 0), at(19, 0), 150, 0.8)
	b := event("Michelle Malone", offset(1.02, 0), at(19, 0), 150, 0.8) // same venue, other name
	c := event("Little Big Town", offset(0, 1), at(19, 30), 150, 0.8)
	d := event("Little Big Town", offset(0.9, 1), at(19, 30), 150, 0.8) // same name, geocoded elsewhere
	e := event("Michelle Malone", offset(1, 0), at(21, 0), 60, 0.8)     // later show: a different event
	f := event("Wicked", offset(0, -1), at(19, 0), 150, 0.8)
	g := event("Hamilton", offset(0, -1), at(19, 0), 150, 0.8) // theatres sharing a coordinate
	nodes, _ := BuildNodes(w, []models.Activity{a, b, c, d, e, f, g}, cfg)
	key := map[string]string{}
	for _, n := range nodes {
		key[n.Act.ID.Hex()] = n.SeriesKey
	}
	if key[a.ID.Hex()] != key[b.ID.Hex()] {
		t.Error("same venue, same start: should be one event")
	}
	if key[c.ID.Hex()] != key[d.ID.Hex()] {
		t.Error("same name, same start: should be one event")
	}
	if key[e.ID.Hex()] == key[b.ID.Hex()] {
		t.Error("a later show is a different event")
	}
	if key[f.ID.Hex()] == key[g.ID.Hex()] {
		t.Error("different shows at one coordinate are different events")
	}
}

func TestFoodPlacesAreScheduled(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(8, 0), at(21, 0))
	cafe := place("Coffee", "cafe", techSquare, daily(7, 16), 0.9)
	diner := place("Diner", "restaurant", techSquare, nil, 0.9) // no hours: default 11:00-22:00
	tour := place("Ferry loop", "tour", techSquare, daily(10, 18), 0.9)
	workshop := place("Makerspace", "class_workshop", techSquare, daily(9, 21), 0.9)
	nodes, drops := BuildNodes(w, []models.Activity{cafe, diner, tour, workshop}, cfg)
	got := map[string]int{}
	for _, n := range nodes {
		got[n.Act.Name]++
		if n.Act.Name == "Diner" && n.Start.Before(at(11, 0)) {
			t.Errorf("diner scheduled at %v, before its default opening", n.Start.In(ny))
		}
	}
	if got["Coffee"] == 0 || got["Diner"] == 0 || got["Ferry loop"] == 0 {
		t.Errorf("food and tour places should get slots, got %v (drops %+v)", got, drops)
	}
	if got["Makerspace"] != 0 {
		t.Error("class venues need a session and should stay excluded")
	}
}
