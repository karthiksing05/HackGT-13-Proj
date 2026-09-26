package planner

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func demoSave(t *testing.T) (*testPlanner, Option, SaveInput) {
	t.Helper()
	tp, b, spec := demoEvening(t)
	opt := b.Options[0]
	order := stopIDs(opt.Stops)
	route, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: order})
	if err != nil {
		t.Fatal(err)
	}
	in := SaveInput{
		UserID: "sandy", Catalog: "demo_activities",
		Plan: Request{Start: PlaceAt("Seaside Market Square", seasideMkt), End: PlaceAt("Home", seasideMkt),
			StartTime: spec.From, BackBy: spec.BackBy, Date: spec.From},
		Option: opt, StopOrder: order, Route: route, Visibility: "friends", TZ: ny, ItineraryID: "itin-1",
	}
	return tp, opt, in
}

func TestBuildItineraryFromRoute(t *testing.T) {
	tp, opt, in := demoSave(t)
	draft, err := tp.BuildItinerary(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	n := len(opt.Stops)
	if len(draft.Items) != 2*n+1 {
		t.Fatalf("%d items for %d stops", len(draft.Items), n)
	}
	if draft.Title != opt.Name || draft.PlanRunID == "" || draft.PoolMissing || draft.OptionID != opt.ID {
		t.Errorf("draft %+v", draft)
	}
	if !draft.Date.Equal(time.Date(2026, 9, 26, 0, 0, 0, 0, ny)) || draft.TZ != "America/New_York" {
		t.Errorf("date %v tz %s", draft.Date, draft.TZ)
	}
	prevEnd := draft.Items[0].Start
	for i, item := range draft.Items {
		if item.Start.Before(prevEnd) || item.End.Before(item.Start) {
			t.Errorf("item %d %s runs %v–%v after %v", i, item.Title, item.Start, item.End, prevEnd)
		}
		prevEnd = item.End
		if i%2 == 0 {
			if item.Kind != "transit" || item.Leg == nil || item.Description != transitDescription || !strings.Contains(item.Place.Name, " → ") {
				t.Errorf("item %d should be transit: %+v", i, item)
			}
			continue
		}
		s := opt.Stops[i/2]
		st := in.Route.StopTimes[i/2]
		if item.Kind != "sidequest" || item.ActivityID != s.ActivityID || item.StopID != s.ID || !item.Start.Equal(st.Start) || !item.End.Equal(st.End) {
			t.Errorf("item %d: %+v", i, item)
		}
		if item.Title != s.Title || item.Place != s.Place || item.Category != s.Category || item.Flexible != s.Flexible {
			t.Errorf("item %d copies the stop badly", i)
		}
		if (item.PriceCents == nil) == s.PriceKnown || (s.PriceKnown && *item.PriceCents != *s.PriceCents) {
			t.Errorf("item %d price %v, stop %v", i, item.PriceCents, s.PriceCents)
		}
		if want := "Walk to " + s.Title; draft.Items[i-1].Title != want && !strings.HasSuffix(draft.Items[i-1].Title, " to "+s.Title) {
			t.Errorf("leg title %q", draft.Items[i-1].Title)
		}
	}
	last := draft.Items[len(draft.Items)-1]
	if !last.End.Equal(in.Route.Arrival) || !strings.HasSuffix(last.Title, " to Home") || !strings.HasSuffix(last.Place.Name, "→ Home") {
		t.Errorf("last leg %+v (arrival %v)", last, in.Route.Arrival)
	}
	if first := draft.Items[0]; !strings.HasPrefix(first.Place.Name, "Seaside Market Square → ") {
		t.Errorf("first leg place %q", first.Place.Name)
	}
	run := tp.run(t, draft.PlanRunID)
	if run.Outcome == nil || run.Outcome.SavedOptionID != opt.ID || run.Outcome.ItineraryID != "itin-1" ||
		strings.Join(run.Outcome.StopOrder, ",") != strings.Join(in.StopOrder, ",") || len(run.Outcome.AlternativesUsed) != 0 {
		t.Errorf("outcome %+v", run.Outcome)
	}
}

func TestBuildItineraryGolden(t *testing.T) {
	tp, _, in := demoSave(t)
	draft, err := tp.BuildItinerary(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "itinerary_draft.json", draft)
}

// appStops is what the app sends back: only the contract's PlanStop keys.
func appStops(stops []Stop) []Stop {
	out := make([]Stop, len(stops))
	for i, s := range stops {
		out[i] = Stop{ID: s.ID, Title: s.Title, Subtitle: s.Subtitle, Place: s.Place, DurationMinutes: s.DurationMinutes}
	}
	return out
}

func TestBuildItineraryAfterThePoolExpired(t *testing.T) {
	tp, opt, in := demoSave(t)
	in.Option.Stops = appStops(opt.Stops)
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)

	draft, err := tp.BuildItinerary(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !draft.PoolMissing || draft.PlanRunID != "" || len(draft.Items) != 2*len(opt.Stops)+1 {
		t.Fatalf("draft %+v", draft)
	}
	for i, s := range opt.Stops {
		item := draft.Items[2*i+1]
		if item.ActivityID != s.ActivityID || item.Category != s.Category || item.Flexible != s.Flexible {
			t.Errorf("stop %d not filled in from the catalog: %+v", i, item)
		}
		if (item.PriceCents == nil) == s.PriceKnown {
			t.Errorf("stop %d price %v", i, item.PriceCents)
		}
		if !item.Start.Equal(in.Route.StopTimes[i].Start) {
			t.Errorf("stop %d keeps the route's time", i)
		}
	}

	// Without a catalog the stops stay as the app sent them, prices unknown.
	in.Catalog = ""
	draft, err = tp.BuildItinerary(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range opt.Stops {
		item := draft.Items[2*i+1]
		if item.ActivityID != opt.Stops[i].ActivityID || item.PriceCents != nil || item.Category != "" {
			t.Errorf("unenriched stop %d: %+v", i, item)
		}
	}

	// A route that does not fit the stops is recomputed.
	in.Route = RouteResult{}
	draft, err = tp.BuildItinerary(t.Context(), in)
	if err != nil || len(draft.Items) != 2*len(opt.Stops)+1 {
		t.Fatalf("re-evaluated: %v %d", err, len(draft.Items))
	}
	for i := 1; i < len(draft.Items); i++ {
		if draft.Items[i].Start.Before(draft.Items[i-1].End) {
			t.Errorf("items overlap at %d", i)
		}
	}
}

func TestBuildItineraryRejectsUnknownStops(t *testing.T) {
	tp, _, in := demoSave(t)
	in.StopOrder = append(in.StopOrder, "stop_ffffffffffffffffffffffff_9")
	var unknown *UnknownStopError
	if _, err := tp.BuildItinerary(t.Context(), in); !errors.As(err, &unknown) {
		t.Errorf("unknown stop with the pool: %v", err)
	}
	tp.clock.Advance(tp.Cfg.PoolTTL + time.Minute)
	if _, err := tp.BuildItinerary(t.Context(), in); !errors.As(err, &unknown) {
		t.Errorf("unknown stop without the pool: %v", err)
	}
	in.StopOrder = nil
	in.Option.Stops = nil
	if _, err := tp.BuildItinerary(t.Context(), in); !errors.Is(err, ErrNoStops) {
		t.Errorf("no stops: %v", err)
	}
}

func TestResolveStop(t *testing.T) {
	tp, opt, _ := demoSave(t)
	s := opt.Stops[0]
	d, err := tp.ResolveStop(t.Context(), "sandy", "demo_activities", opt.ID, s.ID)
	if err != nil || d.ActivityID != s.ActivityID || d.DurationMin != s.DurationMinutes || d.WebsiteURL != s.WebsiteURL {
		t.Fatalf("from the pool: %+v %v", d, err)
	}
	// Without the option (the api seam passes only the stop id): the catalog.
	d, err = tp.ResolveStop(t.Context(), "sandy", "demo_activities", "", s.ID)
	if err != nil || d.ActivityID != s.ActivityID || (d.PriceCents == nil) == s.PriceKnown {
		t.Fatalf("from the catalog: %+v %v", d, err)
	}
	var unknown *UnknownStopError
	if _, err := tp.ResolveStop(t.Context(), "sandy", "demo_activities", "", "opt-a-0"); !errors.As(err, &unknown) {
		t.Errorf("foreign stop id: %v", err)
	}
	if _, err := tp.ResolveStop(t.Context(), "sandy", "users", "", s.ID); !errors.As(err, &unknown) {
		t.Errorf("a catalog outside the allow-list: %v", err)
	}
}
