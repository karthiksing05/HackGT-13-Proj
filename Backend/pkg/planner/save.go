package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// SaveInput is the app's CreateItineraryRequest in planner terms. Option is
// the option as edited on Review (its stops carry at least id, title,
// subtitle, place and duration); Route is what /plans/route returned.
type SaveInput struct {
	UserID       string
	Catalog      string // the user's catalog: fills in stops the pool no longer has
	Plan         Request
	Option       Option
	StopOrder    []string
	Route        RouteResult
	Visibility   string
	LockAt       *time.Time
	MaxGroupSize *int
	TZ           *time.Location
	ItineraryID  string // the id the store will use, for the run's outcome
}

// LegInfo is the transit item's leg.
type LegInfo struct {
	Mode       string
	Minutes    int
	DistanceKm float64
}

// ItemDraft is one itinerary item before it becomes a document.
type ItemDraft struct {
	ID          string
	Kind        string // sidequest | transit
	Title       string
	Place       Place
	Start       time.Time
	End         time.Time
	Description string
	WebsiteURL  string
	ImageURL    string
	Bookable    bool
	PriceCents  *int64
	PriceKnown  bool
	ActivityID  string
	Category    string
	Tags        []string
	Flexible    bool
	Leg         *LegInfo
	StopID      string
	DurationMin int
}

// ItineraryDraft is the materialised itinerary (§6.5) for the store to
// persist and the view layer to render.
type ItineraryDraft struct {
	Title        string
	Date         time.Time // local midnight of the plan's day, in TZ
	Start        time.Time
	BackBy       time.Time
	TZ           string
	StartPlace   Place
	EndPlace     Place
	Visibility   string
	LockAt       *time.Time
	MaxGroupSize *int
	Items        []ItemDraft
	PlanRunID    string
	OptionID     string
	StopOrder    []string
	PoolMissing  bool
}

const transitDescription = "Route options below. Times update live if you run late."

// ErrNoStops means the stop order resolved to nothing.
var ErrNoStops = errors.New("A sidequest needs at least one stop.")

// BuildItinerary is §6.5: resolve the stop order against the pool (falling
// back to the body's stops when the pool expired), time the stops from the
// route (or by re-evaluating), and lay out transit + sidequest items.
func (p *Planner) BuildItinerary(ctx context.Context, in SaveInput) (ItineraryDraft, error) {
	tz := in.TZ
	if tz == nil {
		tz, _ = time.LoadLocation(defaultTZ)
	}
	draft := ItineraryDraft{
		Title: strings.TrimSpace(in.Option.Name), Visibility: in.Visibility, LockAt: in.LockAt, MaxGroupSize: in.MaxGroupSize,
		OptionID: in.Option.ID, TZ: tz.String(), StopOrder: append([]string(nil), in.StopOrder...),
	}
	if draft.Title == "" {
		draft.Title = "Sidequest"
	}

	// The window from the plan, or from the pool when the plan is bare.
	draft.Start, draft.BackBy = in.Plan.StartTime.UTC(), in.Plan.BackBy.UTC()
	draft.StartPlace, draft.EndPlace = in.Plan.Start, in.Plan.End

	var pool *PlanPool
	var opt *Option
	if runID, ok := RunIDFromOption(in.Option.ID); ok {
		pl, err := p.Pools.GetPool(ctx, runID)
		if err == nil && in.UserID != "" && pl.UserID != "" && pl.UserID != in.UserID {
			err = ErrPoolNotFound // someone else's plan: use only what the body says
		}
		if err == nil {
			pool = pl
			for i := range pool.Options {
				if pool.Options[i].ID == in.Option.ID {
					opt = &pool.Options[i]
				}
			}
		} else if !errors.Is(err, ErrPoolNotFound) {
			return draft, err
		}
	}
	if pool != nil {
		draft.PlanRunID = pool.ID
		if draft.Start.IsZero() {
			draft.Start, draft.BackBy = pool.Window.From, pool.Window.BackBy
		}
		if !draft.StartPlace.HasCoord {
			draft.StartPlace = pool.Window.Start
		}
		if !draft.EndPlace.HasCoord {
			draft.EndPlace = pool.Window.End
		}
	} else {
		draft.PoolMissing = true
		log.Warn().Str("option", in.Option.ID).Msg("planner: pool_missing on save; using the body's stops")
	}
	if !draft.BackBy.After(draft.Start) {
		draft.BackBy = draft.BackBy.AddDate(0, 0, 1)
	}
	date := draft.Start
	if !in.Plan.Date.IsZero() {
		date = in.Plan.Date
	}
	ld := date.In(tz)
	draft.Date = time.Date(ld.Year(), ld.Month(), ld.Day(), 0, 0, 0, 0, tz)

	// Stops: pool records first, the body's stops as the fallback (filled
	// in from the user's catalog where the ids allow it).
	stops, fromBody, err := saveStops(pool, opt, in)
	if err != nil {
		return draft, err
	}
	if len(stops) == 0 {
		return draft, ErrNoStops
	}
	p.enrichFromCatalog(ctx, in.Catalog, stops, fromBody)

	// Times: the route's when it is consistent, else a fresh evaluation.
	var starts, ends []time.Time
	var legs []Leg
	if routeConsistent(in.Route, len(stops)) {
		for _, st := range in.Route.StopTimes {
			starts = append(starts, st.Start.UTC())
			ends = append(ends, st.End.UTC())
		}
		legs = in.Route.Legs
	} else {
		w := draftWindow(draft, pool, in)
		res := p.routeStops(ctx, w, in.Option.ID, stops)
		for _, st := range res.StopTimes {
			starts = append(starts, st.Start)
			ends = append(ends, st.End)
		}
		legs = res.Legs
	}

	// Items: transit → sidequest per stop, then the final transit item.
	cursor := draft.Start
	if len(legs) > 0 && len(starts) > 0 {
		if leave := starts[0].Add(-time.Duration(legs[0].Minutes) * time.Minute); leave.After(cursor) {
			cursor = leave
		}
	}
	prevName := draft.StartPlace.Name
	if prevName == "" {
		prevName = "Start"
	}
	var altsUsed []string
	for i, s := range stops {
		if strings.HasPrefix(s.ID, "alt_") {
			altsUsed = append(altsUsed, s.ID)
		}
		if i < len(legs) {
			leg := legs[i]
			legEnd := cursor.Add(time.Duration(leg.Minutes) * time.Minute)
			draft.Items = append(draft.Items, ItemDraft{
				ID: p.NewID(), Kind: "transit", Title: legTitle(leg.Mode, s.Title),
				Place: Place{Name: prevName + " → " + s.Place.Name}, Start: cursor, End: legEnd,
				Description: transitDescription, Leg: &LegInfo{Mode: leg.Mode, Minutes: leg.Minutes, DistanceKm: leg.DistanceKm},
				DurationMin: leg.Minutes,
			})
			cursor = legEnd
		}
		start, end := cursor, cursor.Add(time.Duration(s.DurationMinutes)*time.Minute)
		if i < len(starts) {
			start, end = starts[i], ends[i]
			if start.Before(cursor) {
				start = cursor
			}
			if !end.After(start) {
				end = start.Add(time.Duration(s.DurationMinutes) * time.Minute)
			}
		}
		item := ItemDraft{
			ID: p.NewID(), Kind: "sidequest", Title: s.Title, Place: s.Place, Start: start, End: end,
			Description: stopDescription(s), WebsiteURL: s.WebsiteURL, ImageURL: s.ImageURL,
			ActivityID: s.ActivityID, Category: s.Category, Tags: s.Tags, Flexible: s.Flexible,
			StopID: s.ID, DurationMin: int(end.Sub(start).Minutes()),
		}
		if s.PriceKnown && s.PriceCents != nil {
			v := *s.PriceCents
			item.PriceCents, item.PriceKnown = &v, true
		}
		item.Bookable = s.TicketURL != "" || (item.PriceCents != nil && *item.PriceCents > 0)
		draft.Items = append(draft.Items, item)
		cursor = end
		prevName = s.Place.Name
	}
	if len(legs) > len(stops) {
		leg := legs[len(stops)]
		endName := draft.EndPlace.Name
		if endName == "" {
			endName = "End"
		}
		legEnd := cursor.Add(time.Duration(leg.Minutes) * time.Minute)
		if !in.Route.Arrival.IsZero() && routeConsistent(in.Route, len(stops)) {
			legEnd = in.Route.Arrival.UTC()
			if legEnd.Before(cursor) {
				legEnd = cursor
			}
		}
		draft.Items = append(draft.Items, ItemDraft{
			ID: p.NewID(), Kind: "transit", Title: legTitle(leg.Mode, endName),
			Place: Place{Name: prevName + " → " + endName}, Start: cursor, End: legEnd,
			Description: transitDescription, Leg: &LegInfo{Mode: leg.Mode, Minutes: leg.Minutes, DistanceKm: leg.DistanceKm},
			DurationMin: leg.Minutes,
		})
	}

	if pool != nil {
		order := make([]string, len(stops))
		for i, s := range stops {
			order[i] = s.ID
		}
		if altsUsed == nil {
			altsUsed = []string{}
		}
		outcome := OutcomeLog{SavedOptionID: in.Option.ID, SavedAt: p.Clock.Now(), StopOrder: order, AlternativesUsed: altsUsed, ItineraryID: in.ItineraryID}
		if err := p.Pools.PatchRun(ctx, pool.ID, bson.M{"outcome": outcome}); err != nil {
			log.Warn().Err(err).Str("run", pool.ID).Msg("planner: patch run outcome")
		}
	}
	return draft, nil
}

// saveStops resolves the order: the pool's records (the option's stops and
// the alternatives suggested for it) first, the body's stops for anything
// the pool does not have. Unknown ids are an error. fromBody marks the stops
// that came from the body.
func saveStops(pool *PlanPool, opt *Option, in SaveInput) (stops []Stop, fromBody []bool, err error) {
	known := map[string]Stop{}
	pooled := map[string]bool{}
	for _, s := range in.Option.Stops {
		known[s.ID] = s
	}
	if pool != nil {
		for id, s := range pool.Alternatives {
			known[id], pooled[id] = s, true
		}
		if opt != nil {
			for _, s := range opt.Stops {
				known[s.ID], pooled[s.ID] = s, true
			}
		}
	}
	order := in.StopOrder
	if len(order) == 0 {
		src := in.Option.Stops
		if opt != nil {
			src = opt.Stops
		}
		for _, s := range src {
			order = append(order, s.ID)
		}
	}
	seen := map[string]bool{}
	for _, id := range order {
		if seen[id] {
			continue
		}
		seen[id] = true
		s, ok := known[id]
		if !ok {
			return nil, nil, &UnknownStopError{ID: id}
		}
		if s.ActivityID == "" {
			s.ActivityID, _ = ActivityIDFromStop(id)
		}
		if s.DurationMinutes <= 0 {
			s.DurationMinutes = 60
		}
		s.Order = len(stops)
		stops = append(stops, s)
		fromBody = append(fromBody, !pooled[id])
	}
	return stops, fromBody, nil
}

// enrichFromCatalog fills in what the app does not send back for a stop
// (category, price, links, whether its time can move) from the user's own
// catalog. What the user saw (title, place, duration) is kept.
func (p *Planner) enrichFromCatalog(ctx context.Context, catalog string, stops []Stop, fromBody []bool) {
	cat, ok := NormalizeCatalog(catalog)
	if p.Lookup == nil || !ok || catalog == "" {
		return
	}
	var ids []string
	for i, s := range stops {
		if fromBody[i] && s.ActivityID != "" {
			ids = append(ids, s.ActivityID)
		}
	}
	if len(ids) == 0 {
		return
	}
	acts, err := p.Lookup.GetActivities(ctx, cat, ids)
	if err != nil {
		log.Warn().Err(err).Msg("planner: enrich saved stops")
		return
	}
	byID := map[string]*models.Activity{}
	for i := range acts {
		byID[acts[i].ID.Hex()] = &acts[i]
	}
	for i := range stops {
		a := byID[stops[i].ActivityID]
		if !fromBody[i] || a == nil {
			continue
		}
		full := stopFromActivity(a, nil)
		s := &stops[i]
		s.Kind, s.Category, s.Tags = full.Kind, full.Category, full.Tags
		s.PriceCents, s.PriceKnown, s.Tier, s.TierKnown = full.PriceCents, full.PriceKnown, full.Tier, full.TierKnown
		s.WebsiteURL, s.TicketURL, s.ImageURL, s.Summary, s.Address = full.WebsiteURL, full.TicketURL, full.ImageURL, full.Summary, full.Address
		s.Flexible = a.Kind == "place" || (a.Attendance != nil && *a.Attendance == "drop_in")
		if !s.Place.HasCoord {
			s.Place = full.Place
		}
	}
}

func routeConsistent(r RouteResult, stops int) bool {
	if len(r.StopTimes) != stops || len(r.Legs) != stops+1 {
		return false
	}
	for i, st := range r.StopTimes {
		if st.Start.IsZero() || st.End.Before(st.Start) {
			return false
		}
		if i > 0 && st.Start.Before(r.StopTimes[i-1].End) {
			return false
		}
	}
	return true
}

func draftWindow(d ItineraryDraft, pool *PlanPool, in SaveInput) itinerary.Window {
	var w itinerary.Window
	if pool != nil {
		w = poolWindow(pool)
	} else {
		w = poolWindow(&PlanPool{Window: PoolWindow{TZ: d.TZ, Mode: "walk", DriveLabel: "drive", MaxLegKm: 2, Pace: "balanced"}})
		w.Mode, w.DriveLabel = resolveMode(in.Plan.Ride, in.Plan.Modes, false)
	}
	w.From, w.BackBy = d.Start, d.BackBy
	w.Start, w.End = d.StartPlace.Point(), d.EndPlace.Point()
	return w
}

func legTitle(mode, destination string) string {
	return legVerb(mode) + " to " + destination
}

// stopDescription is the stop's summary, else its subtitle.
func stopDescription(s Stop) string {
	if strings.TrimSpace(s.Summary) != "" {
		return strings.TrimSpace(s.Summary)
	}
	return s.Subtitle
}
