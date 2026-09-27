package planner

import (
	"Backend/pkg/itinerary"
	"context"
	"time"
)

// CalendarSource reads a user's busy blocks: the events on their calendar
// (mongosource.Store over calendar_events) overlapping [from, to).
// Optional: without one, a plan avoids only the busy blocks its spec
// already carries.
type CalendarSource interface {
	BusyBlocks(ctx context.Context, userID string, from, to time.Time) ([]itinerary.Interval, error)
}

// ReasonCalendarFull is the empty batch's reason when the user's calendar
// leaves no stretch of the window long enough for the shortest visit.
const ReasonCalendarFull = "calendar_full"

// busyPad widens the busy blocks read around the window, so a re-route
// that moves the start or back-by (Route's overrides) still steps around
// the ones it runs into.
const busyPad = 12 * time.Hour

// loadBusy adds the user's calendar around the window to spec.Busy, merged
// (itinerary.MergeBusy). Nothing a plan offers overlaps one (the optimizer
// keeps every visit and leg off them, CheckOption verifies it).
func (p *Planner) loadBusy(ctx context.Context, userID string, spec *PlanSpec) error {
	blocks := spec.Busy
	if p.Calendar != nil && userID != "" {
		read, err := p.Calendar.BusyBlocks(ctx, userID, spec.From.Add(-busyPad), spec.BackBy.Add(busyPad))
		if err != nil {
			return err
		}
		blocks = append(append([]itinerary.Interval(nil), blocks...), read...)
	}
	spec.Busy = itinerary.MergeBusy(blocks)
	return nil
}

// calendarFull reports whether the busy blocks leave no stretch of the
// window long enough for the shortest visit.
func calendarFull(spec *PlanSpec, cfg itinerary.Config) bool {
	return len(spec.Busy) > 0 && itinerary.LongestFree(spec.From, spec.BackBy, spec.Busy) < cfg.MinDuration
}

// slotsOf and intervalsOf convert between the pool's stored busy blocks
// and the optimizer's.
func slotsOf(ivs []itinerary.Interval) []TimeSlot {
	if len(ivs) == 0 {
		return nil
	}
	out := make([]TimeSlot, len(ivs))
	for i, iv := range ivs {
		out[i] = TimeSlot{From: iv.Start.UTC(), To: iv.End.UTC()}
	}
	return out
}

func intervalsOf(slots []TimeSlot) []itinerary.Interval {
	if len(slots) == 0 {
		return nil
	}
	out := make([]itinerary.Interval, len(slots))
	for i, s := range slots {
		out[i] = itinerary.Interval{Start: s.From.UTC(), End: s.To.UTC()}
	}
	return itinerary.MergeBusy(out)
}
