package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/travel"
	"context"
	"errors"
	"strings"
	"time"
)

// RouteInput is the app's RouteRequest in planner terms. Start, End,
// StartTime, BackBy, Ride and Modes override the pool's window when set.
type RouteInput struct {
	UserID    string // the caller; another user's pool reads as expired
	OptionID  string
	StopOrder []string
	Start     *Place
	End       *Place
	StartTime *time.Time
	BackBy    *time.Time
	Ride      string
	Modes     []string
}

// StopTime is one stop's scheduled interval after re-timing. Closed marks
// a flexible stop whose new time falls outside its opening hours.
type StopTime struct {
	StopID   string    `json:"stop_id"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Flexible bool      `json:"flexible"`
	Closed   bool      `json:"closed,omitempty"`
}

// RouteResult is the contract's RouteResult plus the additive fields.
// BrokenAt is the first stop, in the new order, that no longer works: a
// fixed-time event reached late or a place that would be closed; -1 when
// every stop still works.
type RouteResult struct {
	OptionID         string     `json:"option_id,omitempty"`
	Legs             []Leg      `json:"legs"`
	StopTimes        []StopTime `json:"stop_times"`
	Arrival          time.Time  `json:"arrival"`
	Depart           time.Time  `json:"depart"`
	MinutesLate      int        `json:"minutes_late"`
	BrokenAt         int        `json:"broken_at"`
	LateFlag         bool       `json:"late_flag"`
	TotalDurationMin int        `json:"total_duration_min"`
}

// UnknownStopError names a stop id that is neither the option's nor a
// suggested alternative.
type UnknownStopError struct{ ID string }

func (e *UnknownStopError) Error() string { return "Unknown stop " + e.ID }

// ErrUnknownOption means the option id is not in the pool.
var ErrUnknownOption = errors.New("unknown option")

// loadPool fetches the pool an option belongs to and the option itself. A
// pool owned by someone else is reported as not found, so ids leak nothing.
func (p *Planner) loadPool(ctx context.Context, userID, optionID string) (*PlanPool, *Option, error) {
	runID, ok := RunIDFromOption(optionID)
	if !ok {
		return nil, nil, ErrPoolNotFound
	}
	pool, err := p.Pools.GetPool(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	if userID != "" && pool.UserID != "" && pool.UserID != userID {
		return nil, nil, ErrPoolNotFound
	}
	for i := range pool.Options {
		if pool.Options[i].ID == optionID {
			return pool, &pool.Options[i], nil
		}
	}
	return pool, nil, ErrUnknownOption
}

// resolveStops maps a stop order onto the option's stops and the pool's
// alternatives. An empty order means the option as generated.
func resolveStops(pool *PlanPool, opt *Option, order []string) ([]Stop, error) {
	known := map[string]Stop{}
	for _, s := range opt.Stops {
		known[s.ID] = s
	}
	for id, s := range pool.Alternatives {
		known[id] = s
	}
	if len(order) == 0 {
		return append([]Stop(nil), opt.Stops...), nil
	}
	out := make([]Stop, 0, len(order))
	seen := map[string]bool{}
	for _, id := range order {
		s, ok := known[id]
		if !ok {
			return nil, &UnknownStopError{ID: id}
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		s.Order = len(out)
		out = append(out, s)
	}
	return out, nil
}

// poolWindow rebuilds the optimizer window from the pool.
func poolWindow(pool *PlanPool) itinerary.Window {
	w := pool.Window
	tz, err := time.LoadLocation(w.TZ)
	if err != nil {
		tz, _ = time.LoadLocation(defaultTZ)
	}
	return itinerary.Window{
		From: w.From, BackBy: w.BackBy, TZ: tz,
		Start: w.Start.Point(), End: w.End.Point(),
		Mode: travel.Mode(w.Mode), DriveLabel: w.DriveLabel, MaxLegKm: w.MaxLegKm,
		BudgetCents: w.BudgetCents, Pace: w.Pace,
	}
}

// applyOverrides moves the window to the request's start/end/times/modes.
func applyOverrides(w itinerary.Window, in RouteInput) itinerary.Window {
	if in.Start != nil && in.Start.HasCoord {
		p := *in.Start.Point()
		w.Start = &p
	}
	if in.End != nil && in.End.HasCoord {
		p := *in.End.Point()
		w.End = &p
	}
	if in.StartTime != nil && !in.StartTime.IsZero() {
		w.From = in.StartTime.UTC()
	}
	if in.BackBy != nil && !in.BackBy.IsZero() {
		w.BackBy = in.BackBy.UTC()
		if !w.BackBy.After(w.From) {
			w.BackBy = w.BackBy.AddDate(0, 0, 1)
		}
	}
	if in.Ride != "" || len(in.Modes) > 0 {
		w.Mode, w.DriveLabel = resolveMode(strings.ToLower(strings.TrimSpace(in.Ride)), in.Modes, false)
	}
	return w
}

func evalStops(stops []Stop) []itinerary.EvalStop {
	out := make([]itinerary.EvalStop, len(stops))
	for i, s := range stops {
		es := itinerary.EvalStop{Loc: travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}, DurationMin: s.DurationMinutes, Flexible: s.Flexible}
		if !s.Arrive.IsZero() && !s.Depart.IsZero() {
			a, d := s.Arrive, s.Depart
			es.Arrive, es.Depart = &a, &d
		}
		out[i] = es
	}
	return out
}

// Route re-times a stop order (§6.3): the option's stops and any
// alternatives, in the order given, against the pool's window with the
// request's overrides.
func (p *Planner) Route(ctx context.Context, in RouteInput) (RouteResult, error) {
	pool, opt, err := p.loadPool(ctx, in.UserID, in.OptionID)
	if err != nil {
		return RouteResult{}, err
	}
	stops, err := resolveStops(pool, opt, in.StopOrder)
	if err != nil {
		return RouteResult{}, err
	}
	w := applyOverrides(poolWindow(pool), in)
	return p.routeStops(ctx, w, in.OptionID, stops), nil
}

func (p *Planner) routeStops(ctx context.Context, w itinerary.Window, optionID string, stops []Stop) RouteResult {
	ev := itinerary.EvaluateStops(ctx, w, evalStops(stops), p.Travel)
	res := RouteResult{
		OptionID: optionID, Legs: []Leg{}, StopTimes: []StopTime{},
		Arrival: ev.Arrival.UTC(), Depart: ev.Depart.UTC(),
		MinutesLate: ev.MinutesLate, BrokenAt: ev.BrokenAt, LateFlag: ev.MinutesLate > 0,
		TotalDurationMin: int(ev.Arrival.Sub(ev.Depart).Minutes()),
	}
	for i, leg := range ev.Legs {
		res.Legs = append(res.Legs, renderLeg(leg, i, stops, w.DriveLabel))
	}
	for i, s := range stops {
		st := StopTime{StopID: s.ID, Start: ev.Starts[i].UTC(), End: ev.Ends[i].UTC(), Flexible: s.Flexible}
		if s.Flexible && len(s.OpenSlots) > 0 && !withinSlots(st.Start, st.End, s.OpenSlots) {
			st.Closed = true
			if res.BrokenAt < 0 || i < res.BrokenAt {
				res.BrokenAt = i
			}
		}
		res.StopTimes = append(res.StopTimes, st)
	}
	res.LateFlag = res.MinutesLate > 0 || res.BrokenAt >= 0
	return res
}

// StopDetail is what the save step needs about a stop id (the api.Planner
// seam's ResolveStop).
type StopDetail struct {
	ActivityID  string
	PriceCents  *int64
	WebsiteURL  string
	Bookable    bool
	DurationMin int
	Stop        *Stop
}

// ResolveStop finds a stop by id: in the pool of optionID when one is
// given (the option's stops and its suggested alternatives), else by the
// activity id the stop id carries, in the caller's catalog.
func (p *Planner) ResolveStop(ctx context.Context, userID, catalog, optionID, stopID string) (*StopDetail, error) {
	if optionID != "" {
		if pool, opt, err := p.loadPool(ctx, userID, optionID); err == nil {
			if opt != nil {
				for i := range opt.Stops {
					if opt.Stops[i].ID == stopID {
						return detailFromStop(&opt.Stops[i]), nil
					}
				}
			}
			if s, ok := pool.Alternatives[stopID]; ok {
				return detailFromStop(&s), nil
			}
		} else if !errors.Is(err, ErrPoolNotFound) && !errors.Is(err, ErrUnknownOption) {
			return nil, err
		}
	}
	actID, ok := ActivityIDFromStop(stopID)
	if !ok || p.Lookup == nil {
		return nil, &UnknownStopError{ID: stopID}
	}
	cat, ok := NormalizeCatalog(catalog)
	if !ok {
		return nil, &UnknownStopError{ID: stopID}
	}
	acts, err := p.Lookup.GetActivities(ctx, cat, []string{actID})
	if err != nil {
		return nil, err
	}
	if len(acts) != 1 {
		return nil, &UnknownStopError{ID: stopID}
	}
	s := stopFromActivity(&acts[0], nil)
	s.ID = stopID
	return detailFromStop(&s), nil
}

func detailFromStop(s *Stop) *StopDetail {
	d := &StopDetail{ActivityID: s.ActivityID, WebsiteURL: s.WebsiteURL, DurationMin: s.DurationMinutes, Stop: s}
	if s.PriceKnown && s.PriceCents != nil {
		v := *s.PriceCents
		d.PriceCents = &v
	}
	d.Bookable = s.TicketURL != "" || (s.PriceKnown && s.PriceCents != nil && *s.PriceCents > 0)
	return d
}
