package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Sentences the Create and Edit flows show.
const (
	MsgPlanChanged = "That plan changed. Go back and try again."
	MsgNoStops     = "A sidequest needs at least one stop."
	MsgWindow      = "Back by has to be after the start."
	MsgGroupSize   = "A group needs room for at least 2 people."
)

// Create is POST /itineraries (CreateItineraryRequest) → 201 Itinerary.
func (h *H) Create(w http.ResponseWriter, r *http.Request) {
	var req contract.CreateItineraryRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	doc, err := h.materialize(ctx, user.ID.Hex(), req, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Itineraries().Insert(ctx, doc); err != nil {
		api.Fail(w, r, err)
		return
	}
	if doc.Visibility != models.VisibilityJustMe {
		realtime.ForumUpdate(h.d.Publish())
	}
	out, err := View(ctx, h.d, doc, doc.HostID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// materialize turns a chosen plan option into an itinerary, the port of the
// mock server's createItinerary: the stops in stop_order; for each route
// leg a transit item "<Mode> to <destination>" from the running cursor
// (starting at the plan's start time) for the leg's minutes, followed by
// the stop at its route time. The planner, when wired, adds what it knows
// about each stop (activity, price, website, bookable); otherwise the
// option's own data stands.
func (h *H) materialize(ctx context.Context, hostID string, req contract.CreateItineraryRequest, tz *time.Location) (*models.Itinerary, error) {
	plan := req.Plan
	if !req.Visibility.Valid() || !plan.Range.Valid() || !plan.Ride.Valid() || !plan.Who.Valid() || !plan.Pace.Valid() ||
		!plan.Modes.Valid() || plan.Budget < 0 || plan.Budget > 3 {
		return nil, httpx.BadRequest(httpx.GenericBadRequest)
	}
	if len(req.StopOrder) == 0 {
		return nil, httpx.BadRequest(MsgNoStops)
	}
	if !req.Plan.BackBy.After(req.Plan.StartTime.Time) {
		return nil, httpx.BadRequest(MsgWindow)
	}
	if req.MaxGroupSize != nil && *req.MaxGroupSize < 2 {
		return nil, httpx.BadRequest(MsgGroupSize)
	}
	byID := make(map[string]contract.PlanStop, len(req.Option.Stops))
	for _, s := range req.Option.Stops {
		byID[s.ID] = s
	}
	stops := make([]contract.PlanStop, 0, len(req.StopOrder))
	used := map[string]bool{}
	for _, id := range req.StopOrder {
		s, ok := byID[id]
		if !ok || used[id] {
			return nil, httpx.BadRequest(MsgPlanChanged)
		}
		used[id] = true
		stops = append(stops, s)
	}
	legs, times := req.Route.Legs, req.Route.StopTimes
	if len(legs) < len(stops) || len(times) < len(stops) {
		return nil, httpx.BadRequest(MsgPlanChanged)
	}
	for _, w := range times {
		if w.End.Before(w.Start.Time) {
			return nil, httpx.BadRequest(MsgPlanChanged)
		}
	}
	for _, leg := range legs {
		if !leg.Mode.Valid() || leg.Minutes < 0 {
			return nil, httpx.BadRequest(MsgPlanChanged)
		}
	}

	details := h.resolveStops(ctx, stops)
	places := make([]string, 0, len(stops)+1)
	for _, s := range stops {
		places = append(places, s.Place.Name)
	}
	places = append(places, req.Plan.End.Name)

	items := []models.ItineraryItem{}
	cursor := req.Plan.StartTime.Time
	for i, leg := range legs {
		legEnd := cursor.Add(time.Duration(leg.Minutes) * time.Minute)
		destination := req.Plan.End.Name
		if i < len(places) {
			destination = places[i]
		}
		items = append(items, models.ItineraryItem{
			ID:          store.NewID(),
			Kind:        models.ItemTransit,
			Title:       leg.Mode.Label() + " to " + destination,
			Place:       &models.PlaceDoc{Name: destination},
			Start:       cursor,
			End:         legEnd,
			Description: walkNote,
			LegMode:     string(leg.Mode),
			LegMinutes:  leg.Minutes,
		})
		cursor = legEnd
		if i < len(stops) {
			items = append(items, stopItem(stops[i], times[i], details[i]))
			cursor = times[i].End.Time
		}
	}

	title := strings.TrimSpace(req.Option.Name)
	if title == "" {
		title = stops[0].Title
	}
	y, m, d := localDay(req.Plan.Date.Time, tz)
	date := time.Date(y, m, d, 0, 0, 0, 0, tz)
	modes := make([]string, 0, len(req.Plan.Modes))
	for _, mode := range req.Plan.Modes {
		modes = append(modes, string(mode))
	}
	return &models.Itinerary{
		ID:           store.NewID(),
		HostID:       hostID,
		MemberIDs:    []string{hostID},
		Title:        title,
		DateKey:      date.Format("2006-01-02"),
		TZ:           tz.String(),
		Date:         date,
		Start:        req.Plan.StartTime.Time,
		BackBy:       req.Plan.BackBy.Time,
		StartPlace:   placeIn(req.Plan.Start),
		EndPlace:     placeIn(req.Plan.End),
		Visibility:   string(req.Visibility),
		LockAt:       req.LockAt.StdPtr(),
		MaxGroupSize: req.MaxGroupSize,
		Items:        items,
		Plan: &models.PlanSnapshot{
			Range: string(req.Plan.Range), Ride: string(req.Plan.Ride), OpenSeats: req.Plan.OpenSeats,
			MoodText: req.Plan.MoodText, Tags: req.Plan.Tags, Budget: req.Plan.Budget,
			Who: string(req.Plan.Who), Pace: string(req.Plan.Pace), Modes: modes,
		},
		OptionID:  req.Option.ID,
		RunID:     runID(req.Option.ID),
		RouteMode: routeMode(legs),
	}, nil
}

// resolveStops asks the planner (when wired) what it knows about each
// stop. A stop it cannot resolve (an error, e.g. wrapping ErrNotFound) keeps
// the option's own data; the save never fails because of it.
func (h *H) resolveStops(ctx context.Context, stops []contract.PlanStop) []*api.StopDetail {
	out := make([]*api.StopDetail, len(stops))
	if h.d.Planner == nil {
		return out
	}
	for i, s := range stops {
		detail, err := h.d.Planner.ResolveStop(ctx, s.ID)
		if err != nil {
			log.Info().Err(err).Str("stop", s.ID).Msg("stop not resolved; saving the option's own data")
			continue
		}
		out[i] = detail
	}
	return out
}

// stopItem is one saved stop: the option's title, place, subtitle and visit
// length at its route time, plus what the planner knows about the activity
// (id, price, website, bookable) when there is anything. Stop ids are not
// unique per user, so the planner never decides the stop's time.
func stopItem(s contract.PlanStop, window contract.StopWindow, detail *api.StopDetail) models.ItineraryItem {
	place := placeIn(s.Place)
	item := models.ItineraryItem{
		ID:          store.NewID(),
		Kind:        models.ItemStop,
		Title:       s.Title,
		Place:       &place,
		Start:       window.Start.Time,
		End:         window.End.Time,
		Description: s.Subtitle,
		StopID:      s.ID,
		DurationMin: s.DurationMinutes,
	}
	if s.ActivityID != nil {
		item.ActivityID = *s.ActivityID
	}
	if detail != nil {
		if detail.ActivityID != "" {
			item.ActivityID = detail.ActivityID
		}
		item.PriceCents = detail.PriceCents
		if detail.WebsiteURL != nil && *detail.WebsiteURL != "" {
			item.WebsiteURL = detail.WebsiteURL
		}
		if detail.TicketURL != nil && *detail.TicketURL != "" {
			item.TicketURL = detail.TicketURL
		}
		item.Bookable = detail.Bookable
	}
	return item
}

// localDay is the calendar day a wire date names in tz. The app sends
// instants (local midnight, or a time on that day); a day-only "2026-09-26"
// decodes as UTC midnight and names that date wherever it is read.
func localDay(t time.Time, tz *time.Location) (int, time.Month, int) {
	if u := t.UTC(); u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
		return u.Date()
	}
	return t.In(tz).Date()
}

// optionIDPattern is the planner's "<runId>-<n>" option id.
var optionIDPattern = regexp.MustCompile(`^(.+)-(\d+)$`)

// runID is the planner run an option came from ("" for other ids).
func runID(optionID string) string {
	if m := optionIDPattern.FindStringSubmatch(optionID); m != nil {
		return m[1]
	}
	return ""
}

// routeMode is the mode that carries the most minutes of the route (walk
// without legs); the forum's "N stops · walking|MARTA|driving" reads it.
func routeMode(legs []contract.Leg) string {
	minutes := map[contract.TravelMode]int{}
	best := contract.ModeWalk
	for _, leg := range legs {
		minutes[leg.Mode] += leg.Minutes
		if minutes[leg.Mode] > minutes[best] {
			best = leg.Mode
		}
	}
	return string(best)
}
