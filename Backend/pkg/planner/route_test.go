package planner

import (
	"Backend/pkg/models"
	"errors"
	"testing"
	"time"
)

func reversed(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[len(ids)-1-i] = id
	}
	return out
}

func TestRouteKeepsTheGeneratedTimes(t *testing.T) {
	tp, b, _ := demoEvening(t)
	for _, opt := range tp.allOptions(t, sandy(), b) {
		res, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: stopIDs(opt.Stops)})
		if err != nil {
			t.Fatal(err)
		}
		if res.BrokenAt != -1 || res.MinutesLate != 0 || res.LateFlag || len(res.Legs) != len(opt.Stops)+1 {
			t.Fatalf("%s: %+v", opt.ID, res)
		}
		if !res.Arrival.Equal(opt.Arrival) {
			t.Errorf("%s arrival %v, option says %v", opt.ID, res.Arrival, opt.Arrival)
		}
		for i, st := range res.StopTimes {
			s := opt.Stops[i]
			if st.StopID != s.ID || !st.Start.Equal(s.Arrive) || !st.End.Equal(s.Depart) || st.Flexible != s.Flexible {
				t.Errorf("%s stop %d: %+v vs %s–%s", opt.ID, i, st, s.Arrive, s.Depart)
			}
		}
		for i, l := range res.Legs {
			if l != opt.Legs[i] {
				t.Errorf("%s leg %d: %+v vs %+v", opt.ID, i, l, opt.Legs[i])
			}
		}
	}
}

func TestRouteReorderMakesFixedEventsLate(t *testing.T) {
	a := synthEvent("Early set", "live_music", offsetKm(seasideMkt, 0.5, 0), localAt(18, 30), 60, []string{"music"}, priceOf(5))
	b := synthEvent("Late show", "comedy", offsetKm(seasideMkt, 0.5, 0.4), localAt(20, 0), 60, nil, priceOf(5))
	tp := newTestPlanner([]models.Activity{a, b}, testConfig())
	batch, _ := tp.generate(t, sandy(), defaultReq())
	var opt Option
	for _, o := range tp.allOptions(t, sandy(), batch) {
		if len(o.Stops) == 2 {
			opt = o
		}
	}
	if len(opt.Stops) != 2 || opt.Stops[0].ActivityID != a.ID.Hex() {
		t.Fatalf("expected Early set → Late show, got %+v", opt.Stops)
	}
	res, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: reversed(stopIDs(opt.Stops))})
	if err != nil {
		t.Fatal(err)
	}
	if res.BrokenAt != 1 || res.MinutesLate <= 0 || !res.LateFlag {
		t.Errorf("swapped fixed events: %+v", res)
	}
	// A stop left out is removed.
	res, err = tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: []string{opt.Stops[1].ID}})
	if err != nil || len(res.Legs) != 2 || len(res.StopTimes) != 1 || res.BrokenAt != -1 {
		t.Errorf("one stop: %+v %v", res, err)
	}
}

func TestRouteOverridesAndErrors(t *testing.T) {
	tp, b, spec := demoEvening(t)
	opt := b.Options[0]
	ids := stopIDs(opt.Stops)
	ctx := t.Context()

	res, err := tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: ids, Ride: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res.Legs {
		if l.Mode != "walk" && l.Mode != "rideshare" {
			t.Errorf("cover → %s", l.Mode)
		}
	}
	far := PlaceAt("Lighthouse", offsetKm(seasideMkt, 0, 3))
	later := spec.From.Add(30 * time.Minute)
	res, err = tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: ids, Start: &far, StartTime: &later})
	if err != nil || res.Legs[0].DistanceKm < 2 || res.Depart.Before(later) {
		t.Errorf("start override: %+v %v", res.Legs[0], err)
	}

	var unknown *UnknownStopError
	if _, err := tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: []string{"stop_nope_0"}}); !errors.As(err, &unknown) || unknown.Error() != "Unknown stop stop_nope_0" {
		t.Errorf("unknown stop: %v", err)
	}
	if _, err := tp.Route(ctx, RouteInput{UserID: "someone-else", OptionID: opt.ID, StopOrder: ids}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("another user's option: %v", err)
	}
	if _, err := tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: "opt-a", StopOrder: ids}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("foreign option id: %v", err)
	}
	if _, err := tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: b.RunID + "-99", StopOrder: ids}); !errors.Is(err, ErrUnknownOption) {
		t.Errorf("unknown option: %v", err)
	}
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)
	if _, err := tp.Route(ctx, RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: ids}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("expired pool: %v", err)
	}
}

func TestRouteGolden(t *testing.T) {
	tp, b, _ := demoEvening(t)
	opt := b.Options[0]
	res, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: reversed(stopIDs(opt.Stops))})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "route_result.json", res)
}

func TestMorePagesAndCursors(t *testing.T) {
	tp, b, _ := demoEvening(t)
	ctx := t.Context()
	pool := tp.pool(t, b.RunID)
	if len(b.Options) != 3 || b.Cursor != EncodeCursor(b.RunID, 3) || b.Done {
		t.Fatalf("first page: %d options, cursor %q", len(b.Options), b.Cursor)
	}
	page, err := tp.More(ctx, sandy(), b.Cursor)
	if err != nil || len(page.Options) != 2 || page.Options[0].ID != pool.Options[3].ID {
		t.Fatalf("second page: %+v %v", page, err)
	}
	if all := tp.allOptions(t, sandy(), b); len(all) != len(pool.Options) {
		t.Errorf("paged %d of %d", len(all), len(pool.Options))
	}
	last, _ := tp.More(ctx, sandy(), EncodeCursor(b.RunID, len(pool.Options)-1))
	if len(last.Options) != 1 || !last.Done || last.Cursor != "" {
		t.Errorf("last page: %+v", last)
	}
	for _, c := range []string{"", "batch-0", "dag_unknown_3", EncodeCursor(b.RunID, 999)} {
		p, err := tp.More(ctx, sandy(), c)
		if err != nil || len(p.Options) != 0 || !p.Done {
			t.Errorf("cursor %q: %+v %v", c, p, err)
		}
	}
	if p, _ := tp.More(ctx, &UserContext{ID: "someone-else"}, b.Cursor); len(p.Options) != 0 || !p.Done {
		t.Error("another user's cursor must read as exhausted")
	}
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)
	if p, _ := tp.More(ctx, sandy(), b.Cursor); len(p.Options) != 0 || !p.Done {
		t.Error("an expired pool reads as exhausted")
	}
}

// A reorder that moves a place outside its opening hours breaks the plan
// there, even though places have no fixed start.
func TestRouteFlagsClosedPlaces(t *testing.T) {
	tp, b, _ := demoEvening(t)
	opt := b.Options[0] // Brine and Bivalve (closes 23:00 Saturday), then the Crow's Nest
	res, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: reversed(stopIDs(opt.Stops))})
	if err != nil {
		t.Fatal(err)
	}
	if res.BrokenAt != 1 || !res.StopTimes[1].Closed || res.StopTimes[0].Closed || !res.LateFlag {
		t.Errorf("reversed: broken_at %d, closed %v/%v", res.BrokenAt, res.StopTimes[0].Closed, res.StopTimes[1].Closed)
	}
	for _, s := range opt.Stops {
		if len(s.OpenSlots) == 0 || !withinSlots(s.Arrive, s.Depart, s.OpenSlots) {
			t.Errorf("%s: generated visit %v–%v outside its slots %+v", s.Title, s.Arrive, s.Depart, s.OpenSlots)
		}
	}
}
