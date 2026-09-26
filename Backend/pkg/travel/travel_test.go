package travel

import (
	"context"
	"testing"
	"time"
)

var (
	techSquare = Point{Lat: 33.7766, Lng: -84.3890}
	gwcc       = Point{Lat: 33.758301, Lng: -84.398201} // ~2.2 km away
	decatur    = Point{Lat: 33.7748, Lng: -84.2963}     // ~8.6 km away
)

func TestParsePoint(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"33.7766,-84.3890", true},
		{" 33.7766 , -84.3890 ", true},
		{"Midtown Atlanta", false},
		{"33.7766", false},
		{"91,0", false},
		{"0,181", false},
		{"", false},
	}
	for _, c := range cases {
		p, ok := ParsePoint(c.in)
		if ok != c.ok {
			t.Errorf("ParsePoint(%q) ok=%v, want %v", c.in, ok, c.ok)
		}
		if ok && c.in == "33.7766,-84.3890" && p != techSquare {
			t.Errorf("ParsePoint(%q) = %+v", c.in, p)
		}
	}
}

func TestHaversine(t *testing.T) {
	d := HaversineKm(techSquare, gwcc)
	if d < 2.0 || d > 2.4 {
		t.Fatalf("Tech Square -> GWCC = %.2f km, want ~2.2", d)
	}
	if HaversineKm(techSquare, techSquare) != 0 {
		t.Fatal("distance to self should be 0")
	}
}

func TestLowerBoundNeverExceedsEstimate(t *testing.T) {
	for _, mode := range []Mode{Walk, Transit, Drive} {
		for _, b := range []Point{techSquare, gwcc, decatur} {
			lb := LowerBound(techSquare, b, mode)
			est := Estimate(techSquare, b, mode).Duration
			// Estimates are rounded to the minute.
			if lb > est+30*time.Second {
				t.Errorf("mode %s to %+v: lower bound %v > estimate %v", mode, b, lb, est)
			}
		}
	}
}

func TestEstimateModes(t *testing.T) {
	if leg := Estimate(techSquare, decatur, Drive); leg.Mode != Drive {
		t.Errorf("long drive leg should drive, got %s", leg.Mode)
	}
	if leg := Estimate(techSquare, Point{Lat: 33.7790, Lng: -84.3890}, Drive); leg.Mode != Walk {
		t.Errorf("0.3 km leg should be walked, got %s", leg.Mode)
	}
	walk := Estimate(techSquare, decatur, Walk).Duration
	transit := Estimate(techSquare, decatur, Transit).Duration
	if transit >= walk {
		t.Errorf("transit (%v) should beat walking (%v) over 8 km", transit, walk)
	}
}

func TestHeuristicProvider(t *testing.T) {
	pairs := []Pair{{techSquare, gwcc}, {gwcc, techSquare}}
	legs, err := Heuristic{}.Legs(context.Background(), pairs, Walk)
	if err != nil || len(legs) != 2 {
		t.Fatalf("got %d legs, err %v", len(legs), err)
	}
	if legs[pairs[0]].Duration <= 0 {
		t.Fatal("expected a positive duration")
	}
}
