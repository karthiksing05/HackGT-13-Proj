package itinerary

import (
	"Backend/pkg/models"
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
	// 18:00, 18:30, 19:00, 19:30, 20:00 fit a 2 h visit before 22:00.
	if len(nodes) != 5 {
		t.Errorf("got %d slots, want 5", len(nodes))
	}
}

func TestTimedEntrySlotsShareASeries(t *testing.T) {
	cfg := DefaultConfig()
	w := window(at(12, 0), at(22, 0))
	var acts []models.Activity
	for m := 0; m < 4; m++ {
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
	cfg := DefaultConfig()
	a := event("x", techSquare, at(19, 0), 60, 0.9)
	if u := utility(&a, cfg); u < 0.59 || u > 0.61 {
		t.Errorf("score 0.9 -> %.2f, want 0.6", u)
	}
	a.RerankScore = f64(2)
	if u := utility(&a, cfg); u < 0.19 || u > 0.21 {
		t.Errorf("rerank 2/4 -> %.2f, want 0.2", u)
	}
	a.RerankScore, a.Score = nil, nil
	if u := utility(&a, cfg); u < 0.09 || u > 0.11 {
		t.Errorf("unscored -> %.2f, want 0.1", u)
	}
}
