package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"testing"
	"time"
)

func iv(from, to time.Time) Interval { return Interval{Start: from.UTC(), End: to.UTC()} }

func TestBusyHelpers(t *testing.T) {
	busy := MergeBusy([]Interval{
		iv(at(15, 0), at(16, 0)),
		iv(at(9, 0), at(10, 0)),
		iv(at(9, 30), at(10, 30)), // overlaps the 9:00 one
		iv(at(10, 30), at(11, 0)), // touches it
		iv(at(12, 0), at(12, 0)),  // empty
	})
	if len(busy) != 2 || !busy[0].Start.Equal(at(9, 0)) || !busy[0].End.Equal(at(11, 0)) || !busy[1].Start.Equal(at(15, 0)) {
		t.Fatalf("merged: %v", busy)
	}
	if !Clashes(busy, at(10, 59), at(12, 0)) || Clashes(busy, at(11, 0), at(15, 0)) {
		t.Error("clashes")
	}
	free := WithoutBusy([]Interval{iv(at(8, 0), at(13, 0)), iv(at(14, 0), at(17, 0))}, busy)
	want := []Interval{iv(at(8, 0), at(9, 0)), iv(at(11, 0), at(13, 0)), iv(at(14, 0), at(15, 0)), iv(at(16, 0), at(17, 0))}
	if len(free) != len(want) {
		t.Fatalf("free: %v", free)
	}
	for i := range want {
		if !free[i].Start.Equal(want[i].Start) || !free[i].End.Equal(want[i].End) {
			t.Errorf("free[%d] = %v, want %v", i, free[i], want[i])
		}
	}
	// A leg into a 12:00 visit sets off when the morning block ends; one
	// into a 17:00 visit after the 15:00 block, whatever came before.
	for _, c := range []struct {
		after, by, want time.Time
	}{
		{at(8, 0), at(8, 45), at(8, 0)},
		{at(8, 0), at(12, 0), at(11, 0)},
		{at(9, 15), at(12, 0), at(11, 0)}, // setting off inside a block waits for its end
		{at(11, 30), at(17, 0), at(16, 0)},
	} {
		if got := LegInto(busy, c.after, c.by); !got.Equal(c.want) {
			t.Errorf("LegInto(%v, %v) = %v, want %v", c.after.In(ny), c.by.In(ny), got.In(ny), c.want.In(ny))
		}
	}
	// The leg home sets off at the first free moment long enough for it.
	for _, c := range []struct {
		after time.Time
		leg   time.Duration
		want  time.Time
	}{
		{at(14, 30), 20 * time.Minute, at(14, 30)},
		{at(14, 45), 20 * time.Minute, at(16, 0)},
		{at(15, 10), 0, at(16, 0)},
		{at(8, 50), 5 * time.Minute, at(8, 50)},
	} {
		if got := LegAfter(busy, c.after, c.leg); !got.Equal(c.want) {
			t.Errorf("LegAfter(%v, %v) = %v, want %v", c.after.In(ny), c.leg, got.In(ny), c.want.In(ny))
		}
	}
	if got := FreeBetween(busy, at(8, 0), at(16, 30)); got != 8*time.Hour+30*time.Minute-3*time.Hour {
		t.Errorf("free between: %v", got)
	}
	if got := LongestFree(at(8, 0), at(17, 0), busy); got != 4*time.Hour {
		t.Errorf("longest free: %v", got)
	}
	if got := LongestFree(at(9, 0), at(11, 0), busy); got != 0 {
		t.Errorf("a busy window: %v", got)
	}
}

// checkOffBusy fails an itinerary that puts a visit, or a leg as the saved
// plan lays it out, on top of a busy block.
func checkOffBusy(t *testing.T, w Window, it Itinerary) {
	t.Helper()
	if Clashes(w.Busy, it.Depart, it.Depart.Add(it.Legs[0].Duration)) || it.Depart.Add(it.Legs[0].Duration).After(it.Stops[0].Node.Start) {
		t.Errorf("%s: the first leg (from %v) runs into a busy block", names(it), it.Depart.In(ny))
	}
	for i, s := range it.Stops {
		if Clashes(w.Busy, s.Node.Start, s.Node.End) {
			t.Errorf("%s: stop %d is on top of a busy block", names(it), i)
		}
		if i > 0 {
			setOff := LegInto(w.Busy, it.Stops[i-1].Node.End, s.Node.Start)
			if Clashes(w.Busy, setOff, s.Node.Start) || setOff.Add(it.Legs[i].Duration).After(s.Node.Start) {
				t.Errorf("%s: the leg into stop %d (from %v) runs into a busy block", names(it), i, setOff.In(ny))
			}
		}
	}
	home := it.Legs[len(it.Legs)-1].Duration
	setOff := LegAfter(w.Busy, it.Stops[len(it.Stops)-1].Node.End, home)
	if Clashes(w.Busy, setOff, setOff.Add(home)) || !setOff.Add(home).Equal(it.Arrival) || it.Arrival.After(w.BackBy) {
		t.Errorf("%s: the way back %v–%v, arrival %v", names(it), setOff.In(ny), setOff.Add(home).In(ny), it.Arrival.In(ny))
	}
}

func overlapsAny(it Itinerary, b Interval) bool {
	for _, s := range it.Stops {
		if s.Node.Start.Before(b.End) && s.Node.End.After(b.Start) {
			return true
		}
	}
	return false
}

// A class in the middle of a Saturday: the Atlanta fixture's plans that
// would have used that time now keep off it, some on both sides of it.
func TestPlansKeepOffABusyBlock(t *testing.T) {
	acts := loadFixture(t, "atlanta_2026-09-26.json")
	cfg := DefaultConfig()
	class := iv(at(14, 0), at(15, 30))
	w := window(at(12, 0), at(19, 0))
	w.Pace = "packed"

	free, _ := plan(w, acts, cfg)
	clashed := 0
	for _, it := range free {
		if overlapsAny(it, class) {
			clashed++
		}
	}
	if clashed == 0 {
		t.Fatal("without the class no plan uses its time; the fixture no longer tests anything")
	}

	w.Busy = MergeBusy([]Interval{class})
	its, drops := plan(w, acts, cfg)
	if len(its) == 0 {
		t.Fatal("no itineraries around the class")
	}
	around := 0
	for _, it := range its {
		checkFeasible(t, w, it, cfg)
		checkOffBusy(t, w, it)
		if first, last := it.Stops[0].Node, it.Stops[len(it.Stops)-1].Node; !first.Start.After(class.Start) && !last.Start.Before(class.End) {
			around++
		}
		// Re-timed as it is, nothing moves and nothing is late.
		ev := Evaluate(context.Background(), w, planStops(it), travel.Heuristic{})
		if ev.BrokenAt != -1 || ev.MinutesLate != 0 {
			t.Errorf("%s re-times as late: %+v", names(it), ev)
		}
		for i, s := range it.Stops {
			if !ev.Starts[i].Equal(s.Node.Start) {
				t.Errorf("%s: stop %d re-timed %v → %v", names(it), i, s.Node.Start.In(ny), ev.Starts[i].In(ny))
			}
		}
	}
	t.Logf("%d plans (%d without the class used its time), %d on both sides of it; best %s; drops %v",
		len(its), clashed, around, names(its[0]), countReasons(drops))
	if around == 0 {
		t.Error("no plan works around the class (stops before and after it)")
	}
	if countReasons(drops)["busy"] == 0 {
		t.Error("no activity was dropped for the class")
	}

	// The same inputs give the same plans.
	again, _ := plan(w, acts, cfg)
	if len(again) != len(its) {
		t.Fatalf("second solve: %d plans, first %d", len(again), len(its))
	}
	for i := range its {
		if names(again[i]) != names(its[i]) {
			t.Fatalf("plan %d: %s, then %s", i, names(its[i]), names(again[i]))
		}
	}
}

func TestABusyWindowHasNothingToVisit(t *testing.T) {
	acts := loadFixture(t, "atlanta_2026-09-26.json")
	w := window(at(13, 0), at(16, 0))
	w.Busy = MergeBusy([]Interval{iv(at(12, 0), at(14, 30)), iv(at(14, 30), at(17, 0))})
	nodes, drops := BuildNodes(w, acts, DefaultConfig())
	if len(nodes) != 0 {
		t.Fatalf("%d nodes in a busy window", len(nodes))
	}
	if countReasons(drops)["busy"] == 0 {
		t.Errorf("drops %v", countReasons(drops))
	}
}

// A clipped stay (a set at a bar) ends as the next busy block starts.
func TestClippedStaysEndBeforeABusyBlock(t *testing.T) {
	cfg := DefaultConfig()
	set := withEnd(event("Set", offset(0.4, 0), at(19, 0), 180, 0.9), at(22, 0))
	set.Category = "live_music"
	w := window(at(18, 0), at(23, 30))
	w.Busy = MergeBusy([]Interval{iv(at(21, 0), at(22, 30))})
	nodes, _ := BuildNodes(w, []models.Activity{set}, cfg)
	if len(nodes) == 0 {
		t.Fatal("no stays before the block")
	}
	for _, n := range nodes {
		if n.End.After(at(21, 0)) || n.Start.Before(at(19, 0)) {
			t.Errorf("stay %v–%v runs into the block", n.Start.In(ny), n.End.In(ny))
		}
	}
	// Starting inside a block is no stay at all.
	w.Busy = MergeBusy([]Interval{iv(at(18, 30), at(19, 30))})
	if nodes, drops := BuildNodes(w, []models.Activity{set}, cfg); len(nodes) != 0 || countReasons(drops)["busy"] != 1 {
		t.Errorf("a set that starts in a block: %d nodes, drops %v", len(nodes), countReasons(drops))
	}
}

func TestEvaluateStepsAroundBusyBlocks(t *testing.T) {
	w := window(at(12, 0), at(18, 0))
	w.Busy = MergeBusy([]Interval{iv(at(13, 0), at(14, 0)), iv(at(17, 0), at(17, 30))})
	a, b := offset(0.5, 0), offset(0.5, 0.5)
	planned := func(from, to time.Time) (*time.Time, *time.Time) { return &from, &to }
	walkAB := travel.Estimate(a, b, travel.Walk).Duration

	// A flexible stop that would run into the block moves after it; the leg
	// into it sets off when the block ends.
	aFrom, aTo := planned(at(12, 0), at(12, 45))
	bFrom, bTo := planned(at(12, 50), at(13, 50))
	ev := Evaluate(context.Background(), w, []EvalStop{
		{Loc: a, Arrive: aFrom, Depart: aTo, DurationMin: 45, Flexible: true},
		{Loc: b, Arrive: bFrom, Depart: bTo, DurationMin: 60, Flexible: true},
	}, travel.Heuristic{})
	if ev.BrokenAt != -1 || ev.MinutesLate != 0 || !ev.Starts[1].Equal(at(14, 0).Add(walkAB)) || !ev.Ends[1].Equal(ev.Starts[1].Add(time.Hour)) {
		t.Errorf("flexible stop: %v–%v, %+v", ev.Starts[1].In(ny), ev.Ends[1].In(ny), ev)
	}

	// A fixed start just after the block can't be made: the leg waits for
	// the block to end.
	cFrom, cTo := planned(at(14, 0).Add(walkAB-time.Minute), at(15, 30))
	ev = Evaluate(context.Background(), w, []EvalStop{
		{Loc: a, Arrive: aFrom, Depart: aTo, DurationMin: 45, Flexible: true},
		{Loc: b, Arrive: cFrom, Depart: cTo, DurationMin: 90},
	}, travel.Heuristic{})
	if ev.BrokenAt != 1 || ev.MinutesLate != 1 {
		t.Errorf("fixed stop behind the block: broken_at %d, %d min late", ev.BrokenAt, ev.MinutesLate)
	}

	// The way back waits for the 17:00 block.
	dFrom, dTo := planned(at(16, 0), at(16, 55))
	ev = Evaluate(context.Background(), w, []EvalStop{{Loc: a, Arrive: dFrom, Depart: dTo, DurationMin: 55, Flexible: true}}, travel.Heuristic{})
	home := travel.Estimate(a, techSquare, travel.Walk).Duration
	if !ev.Arrival.Equal(at(17, 30).Add(home)) || ev.MinutesLate != 0 {
		t.Errorf("way back: arrival %v, %+v", ev.Arrival.In(ny), ev)
	}
}
