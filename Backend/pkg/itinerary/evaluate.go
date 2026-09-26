package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"time"
)

// Evaluation is the timing of a stop order the user chose.
type Evaluation struct {
	Legs        []travel.Leg // len(stops)+1
	Starts      []time.Time
	Ends        []time.Time
	Depart      time.Time
	Arrival     time.Time
	MinutesLate int // late arrivals at fixed-time stops plus lateness past back-by
	BrokenAt    int // index of the first stop reached late, or -1
}

// EvalStop is what Evaluate needs to know about a stop: where it is, when
// it was planned (nil for a stop with no scheduled time) and whether that
// time can move.
type EvalStop struct {
	Loc         travel.Point
	Arrive      *time.Time
	Depart      *time.Time
	DurationMin int
	Flexible    bool
}

// Evaluate times a given stop order, e.g. after the user drags stops around.
// Fixed-time stops keep their scheduled times and count as late when the
// user can't get there in time. Flexible stops start on arrival, but never
// before their planned time, which is known to be open.
func Evaluate(ctx context.Context, w Window, stops []models.PlanStop, tp travel.Provider) Evaluation {
	es := make([]EvalStop, len(stops))
	for i, s := range stops {
		es[i] = EvalStop{
			Loc:         travel.Point{Lat: s.Lat, Lng: s.Lng},
			Arrive:      s.ArriveTime,
			Depart:      s.DepartTime,
			DurationMin: s.DurationMin,
			Flexible:    s.Flexible,
		}
	}
	return EvaluateStops(ctx, w, es, tp)
}

// EvaluateStops is Evaluate over the optimizer's own stop type.
func EvaluateStops(ctx context.Context, w Window, stops []EvalStop, tp travel.Provider) Evaluation {
	ev := Evaluation{BrokenAt: -1}

	points := make([]*travel.Point, 0, len(stops)+2)
	points = append(points, w.Start)
	for i := range stops {
		points = append(points, &stops[i].Loc)
	}
	points = append(points, w.End)

	pairs := map[travel.Pair]bool{}
	for i := 0; i+1 < len(points); i++ {
		if points[i] != nil && points[i+1] != nil {
			pairs[travel.Pair{From: *points[i], To: *points[i+1]}] = true
		}
	}
	legs := lookupLegs(ctx, tp, pairs, w.Mode)
	legAt := func(i int) travel.Leg {
		if points[i] == nil || points[i+1] == nil {
			return travel.Leg{Mode: travel.Walk, Source: "unknown"}
		}
		return legs[travel.Pair{From: *points[i], To: *points[i+1]}]
	}

	t := w.From
	if len(stops) > 0 && stops[0].Arrive != nil {
		if leave := stops[0].Arrive.Add(-legAt(0).Duration); leave.After(t) {
			t = leave
		}
	}
	ev.Depart = t

	for i, s := range stops {
		leg := legAt(i)
		ev.Legs = append(ev.Legs, leg)
		arrive := t.Add(leg.Duration)

		start := arrive
		if s.Arrive != nil && s.Arrive.After(start) {
			start = *s.Arrive
		}
		dur := time.Duration(s.DurationMin) * time.Minute
		if s.Arrive != nil && s.Depart != nil {
			dur = s.Depart.Sub(*s.Arrive)
		}
		end := start.Add(dur)

		if !s.Flexible && s.Arrive != nil && arrive.After(*s.Arrive) {
			ev.MinutesLate += int(arrive.Sub(*s.Arrive).Minutes())
			if ev.BrokenAt < 0 {
				ev.BrokenAt = i
			}
			start = arrive
			if s.Depart != nil {
				end = *s.Depart // the event ends when it ends
				if end.Before(start) {
					end = start
				}
			}
		}
		ev.Starts = append(ev.Starts, start)
		ev.Ends = append(ev.Ends, end)
		t = end
	}

	last := legAt(len(stops))
	ev.Legs = append(ev.Legs, last)
	ev.Arrival = t.Add(last.Duration)
	if ev.Arrival.After(w.BackBy) {
		ev.MinutesLate += int(ev.Arrival.Sub(w.BackBy).Minutes())
	}
	return ev
}
