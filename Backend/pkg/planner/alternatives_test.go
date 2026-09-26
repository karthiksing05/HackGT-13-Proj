package planner

import (
	"Backend/pkg/travel"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAlternativesFitTheSlotAndSkipPlanStops(t *testing.T) {
	tp, b, spec := demoEvening(t)
	opt := b.Options[0]
	order := stopIDs(opt.Stops)
	inPlan := map[string]bool{}
	for _, s := range opt.Stops {
		inPlan[s.ActivityID] = true
	}
	most := 0
	for idx, target := range opt.Stops {
		alts, err := tp.Alternatives(t.Context(), AlternativesInput{UserID: "sandy", OptionID: opt.ID, StopID: target.ID, StopOrder: order})
		if err != nil {
			t.Fatal(err)
		}
		if len(alts) > 5 {
			t.Errorf("%d alternatives", len(alts))
		}
		most = max(most, len(alts))
		slotFrom := maxTime(target.Arrive.Add(-30*time.Minute), spec.From)
		slotTo := minTime(target.Depart.Add(30*time.Minute), spec.BackBy)
		for i, a := range alts {
			s := a.Stop
			if s.ID != AltStopID(s.ActivityID, idx) || inPlan[s.ActivityID] {
				t.Errorf("alternative %s (%s)", s.ID, s.Title)
			}
			if s.Arrive.Before(slotFrom) || s.Depart.After(slotTo) || !s.Depart.After(s.Arrive) {
				t.Errorf("%s at %v–%v is outside the slot %v–%v", s.Title, s.Arrive, s.Depart, slotFrom, slotTo)
			}
			if s.Category != target.Category && sharedTags(s.Tags, target.Tags) < 2 {
				t.Errorf("%s (%s) is not like %s (%s)", s.Title, s.Category, target.Title, target.Category)
			}
			if !strings.HasPrefix(a.Reason, "Also ") || !strings.Contains(a.Reason, " mi away") {
				t.Errorf("reason %q", a.Reason)
			}
			if i > 0 && a.FitScore > alts[i-1].FitScore {
				t.Error("alternatives must be best first")
			}
			// Reachable from the previous stop (or the start) and within the leg limits.
			here := travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}
			prevPt, prevEnd, prevLimit := seasideMkt, spec.From, 2*spec.MaxLegKm
			if idx > 0 {
				p := opt.Stops[idx-1]
				prevPt, prevEnd, prevLimit = travel.Point{Lat: p.Place.Lat, Lng: p.Place.Lng}, p.Depart, spec.MaxLegKm
			}
			if d := travel.HaversineKm(prevPt, here); d > prevLimit+1e-9 {
				t.Errorf("%s is %.2f km from the previous stop (limit %.1f)", s.Title, d, prevLimit)
			}
			if leg := travel.Estimate(prevPt, here, spec.Mode); prevEnd.Add(leg.Duration + 10*time.Minute).After(s.Arrive) {
				t.Errorf("%s at %v cannot be reached from the previous stop", s.Title, s.Arrive)
			}
			if idx+1 < len(opt.Stops) {
				n := opt.Stops[idx+1]
				if d := travel.HaversineKm(here, travel.Point{Lat: n.Place.Lat, Lng: n.Place.Lng}); d > spec.MaxLegKm+1e-9 {
					t.Errorf("%s is %.2f km from the next stop", s.Title, d)
				}
			}
		}
		pool := tp.pool(t, b.RunID)
		for _, a := range alts {
			if _, ok := pool.Alternatives[a.Stop.ID]; !ok {
				t.Errorf("%s was not remembered in the pool", a.Stop.ID)
			}
		}
	}
	if most < 3 {
		t.Errorf("Saltlight has six restaurants; expected at least three alternatives for some stop, got %d", most)
	}
	var unknown *UnknownStopError
	if _, err := tp.Alternatives(t.Context(), AlternativesInput{UserID: "sandy", OptionID: opt.ID, StopID: "stop_x_0", StopOrder: order}); !errors.As(err, &unknown) {
		t.Errorf("unknown stop: %v", err)
	}
	if _, err := tp.Alternatives(t.Context(), AlternativesInput{UserID: "mallory", OptionID: opt.ID, StopID: order[0], StopOrder: order}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("another user's option: %v", err)
	}
}

func TestAlternativeIDsWorkInRouteAndSave(t *testing.T) {
	tp, b, spec := demoEvening(t)
	opt := b.Options[0]
	order := stopIDs(opt.Stops)
	alts, err := tp.Alternatives(t.Context(), AlternativesInput{UserID: "sandy", OptionID: opt.ID, StopID: order[0], StopOrder: order})
	if err != nil || len(alts) == 0 {
		t.Fatalf("alternatives: %v (%d)", err, len(alts))
	}
	swapped := append([]string{alts[0].Stop.ID}, order[1:]...)
	res, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: swapped})
	if err != nil || len(res.StopTimes) != len(swapped) || res.StopTimes[0].StopID != alts[0].Stop.ID {
		t.Fatalf("route with the swap: %+v %v", res, err)
	}
	detail, err := tp.ResolveStop(t.Context(), "sandy", "demo_activities", opt.ID, alts[0].Stop.ID)
	if err != nil || detail.ActivityID != alts[0].Stop.ActivityID {
		t.Errorf("resolve alt: %+v %v", detail, err)
	}
	edited := opt
	edited.Stops = append([]Stop{alts[0].Stop}, opt.Stops[1:]...)
	draft, err := tp.BuildItinerary(t.Context(), SaveInput{UserID: "sandy", Catalog: "demo_activities",
		Plan:   Request{Start: PlaceAt("Start", seasideMkt), End: PlaceAt("End", seasideMkt), StartTime: spec.From, BackBy: spec.BackBy, Date: spec.From},
		Option: edited, StopOrder: swapped, Route: res, Visibility: "just_me", TZ: ny})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Items[1].ActivityID != alts[0].Stop.ActivityID || draft.Items[1].StopID != alts[0].Stop.ID {
		t.Errorf("first stop item %+v", draft.Items[1])
	}
	run := tp.run(t, b.RunID)
	if run.Outcome == nil || len(run.Outcome.AlternativesUsed) != 1 || run.Outcome.AlternativesUsed[0] != alts[0].Stop.ID {
		t.Errorf("outcome %+v", run.Outcome)
	}
}

func TestAlternativesGolden(t *testing.T) {
	tp, b, _ := demoEvening(t)
	opt := b.Options[0]
	alts, err := tp.Alternatives(t.Context(), AlternativesInput{UserID: "sandy", OptionID: opt.ID, StopID: opt.Stops[0].ID, StopOrder: stopIDs(opt.Stops)})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plan_alternatives.json", alts)
}
