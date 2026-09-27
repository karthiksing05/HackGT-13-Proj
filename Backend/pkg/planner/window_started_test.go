package planner

import (
	"errors"
	"testing"
	"time"
)

// seaShanty is the Saltlight fixture's Sea Shanty Singalong at The Rusty
// Anchor, Sunday 27 Sep 17:00–19:00.
const seaShanty = "99a9922c81d6260b3b5be433"

// A request whose window started earlier (the app sent its start a while
// ago, or the user picked a start in the past) is planned from now: no stop
// is scheduled in the past and an event that is already over is not offered.
func TestWindowThatAlreadyStartedIsPlannedFromNow(t *testing.T) {
	sunday := func(h, m, s int) time.Time { return time.Date(2026, 9, 27, h, m, s, 0, ny) }
	tp := newTestPlanner(saltlight(t), testConfig())
	tp.clock.T = sunday(19, 5, 20) // the singalong ended at 19:00
	o := defaultReq()
	o.from, o.backBy = sunday(17, 0, 0), sunday(22, 30, 0)
	o.tags, o.mood = []string{"Music"}, "sea shanties and live music"

	batch, spec := tp.generate(t, sandy(), o)
	if want := sunday(19, 6, 0).UTC(); !spec.From.Equal(want) {
		t.Fatalf("window starts at %s, want now rounded up to the minute (%s)", spec.From, want)
	}
	if spec.LocalDate != "2026-09-27" {
		t.Errorf("local date %s", spec.LocalDate)
	}
	if len(batch.Options) == 0 {
		t.Fatalf("no options for the rest of the evening: %s", batch.Reason)
	}
	for _, opt := range tp.allOptions(t, sandy(), batch) {
		for _, s := range opt.Stops {
			if s.Arrive.Before(tp.clock.T) {
				t.Errorf("option %s: %s at %s is in the past (now %s)", opt.ID, s.Title, s.Arrive.In(ny).Format("15:04"), tp.clock.T.Format("15:04"))
			}
			if s.ActivityID == seaShanty {
				t.Errorf("option %s offers the singalong that ended at 19:00", opt.ID)
			}
		}
	}

	// Re-timing an option with the original start (what the app sends from
	// Review) is timed from now too.
	opt := batch.Options[0]
	order := make([]string, len(opt.Stops))
	for i, st := range opt.Stops {
		order[len(order)-1-i] = st.ID
	}
	from, backBy := o.from.UTC(), o.backBy.UTC()
	tp.clock.T = sunday(19, 20, 0)
	route, err := tp.Route(t.Context(), RouteInput{UserID: "sandy", OptionID: opt.ID, StopOrder: order, StartTime: &from, BackBy: &backBy})
	if err != nil {
		t.Fatal(err)
	}
	if route.Depart.Before(tp.clock.T) {
		t.Errorf("the route departs at %s, before now", route.Depart.In(ny).Format("15:04"))
	}
	for i, st := range route.StopTimes {
		if st.Start.Before(tp.clock.T) {
			t.Errorf("route stop %d starts at %s, before now", i, st.Start.In(ny).Format("15:04"))
		}
	}

	// A window with less than a minute left has ended.
	tp.clock.T = sunday(22, 29, 30)
	_, err = ParsePlanRequest(appRequestJSON(o), "America/New_York", tp.clock.Now(), sandy(), tp.Cfg)
	var re *RequestError
	if !errors.As(err, &re) || re.Reason != "window already ended" {
		t.Errorf("30 s left: %v", err)
	}
	// A window that has not started keeps its start.
	tp.clock.T = sunday(12, 0, 0)
	spec = tp.spec(t, sandy(), o)
	if !spec.From.Equal(sunday(17, 0, 0).UTC()) {
		t.Errorf("a future window moved to %s", spec.From)
	}
}
