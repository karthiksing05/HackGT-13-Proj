package planner

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/democlock"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Sentences for the planning routes' user-facing errors.
const (
	MsgPlanExpired  = "This plan expired. Generate again."
	MsgPlanChanged  = "That plan changed. Go back and try again."
	MsgTooManyPicks = "Pick up to 10 must-see spots."
)

// Service is the api.Planner seam over the planner: contract shapes and
// *models.User in, contract shapes out. It emits only what pkg/contract
// defines; closed places, relaxations and costs of unknown prices stay in
// plan_runs.
type Service struct {
	P *Planner
}

var _ api.Planner = (*Service)(nil)

// NewService wraps a planner.
func NewService(p *Planner) *Service { return &Service{P: p} }

// Generate is POST /plans/generate. A request the planner cannot serve
// (the window already ended, times missing) is an empty batch whose reason
// starts with "invalid_request: ", which the app has its own sentence for;
// so are must-see picks that are unavailable or don't fit (picks.go). More
// than 10 picks is a 400. The window is judged at the account's business
// time (a demo account's demo date, pkg/democlock); pools and runs expire
// by the real clock.
func (s *Service) Generate(ctx context.Context, user *models.User, req contract.PlanRequest, tz *time.Location) (contract.PlanBatch, error) {
	now := s.now(ctx)
	uc := UserFromModel(user, now)
	tzName := ""
	if tz != nil {
		tzName = tz.String()
	}
	spec, err := BuildSpec(RequestFromContract(req), tzName, now, uc, s.P.Cfg)
	var invalid *RequestError
	switch {
	case errors.As(err, &invalid):
		return emptyBatch(invalid.Error()), nil
	case errors.Is(err, ErrTooManyPicks):
		return contract.PlanBatch{}, httpx.BadRequest(MsgTooManyPicks)
	case err != nil:
		return contract.PlanBatch{}, err
	}
	batch, err := s.P.Generate(ctx, uc, spec)
	if err != nil {
		return contract.PlanBatch{}, err
	}
	return BatchToContract(batch), nil
}

// More is POST /plans/generate/more; an unknown, expired or foreign cursor
// is the last, empty page.
func (s *Service) More(ctx context.Context, user *models.User, cursor string) (contract.PlanBatch, error) {
	batch, err := s.P.More(ctx, UserFromModel(user, s.now(ctx)), cursor)
	if err != nil {
		return contract.PlanBatch{}, err
	}
	return BatchToContract(batch), nil
}

// Route is POST /plans/route: the order re-timed against the request's
// start, end, times and ride.
func (s *Service) Route(ctx context.Context, user *models.User, req contract.RouteRequest) (contract.RouteResult, error) {
	in := RouteInput{UserID: userID(user), OptionID: req.OptionID, StopOrder: req.StopOrder, Ride: string(req.Ride)}
	for _, m := range req.Modes {
		in.Modes = append(in.Modes, string(m))
	}
	if p := PlaceFromContract(req.Start); p.HasCoord {
		in.Start = &p
	}
	if p := PlaceFromContract(req.End); p.HasCoord {
		in.End = &p
	}
	if !req.StartTime.IsZero() {
		t := req.StartTime.UTC()
		in.StartTime = &t
	}
	if !req.BackBy.IsZero() {
		t := req.BackBy.UTC()
		in.BackBy = &t
	}
	res, err := s.P.Route(ctx, in)
	if err != nil {
		return contract.RouteResult{}, planError(err)
	}
	return RouteToContract(res), nil
}

// Alternatives is POST /plans/alternatives.
func (s *Service) Alternatives(ctx context.Context, user *models.User, req contract.AlternativesRequest) ([]contract.PlanAlternative, error) {
	alts, err := s.P.Alternatives(ctx, AlternativesInput{UserID: userID(user), OptionID: req.OptionID, StopID: req.StopID, StopOrder: req.StopOrder})
	if err != nil {
		return nil, planError(err)
	}
	out := make([]contract.PlanAlternative, len(alts))
	for i, a := range alts {
		out[i] = contract.PlanAlternative{Stop: StopToContract(a.Stop), Reason: a.Reason}
	}
	return out, nil
}

// ResolveStop tells POST /itineraries what the planner knows about a stop
// id it generated: from the newest live pool that holds it, else from the
// catalog activity the id names. An unknown id is store.ErrNotFound.
func (s *Service) ResolveStop(ctx context.Context, stopID string) (*api.StopDetail, error) {
	d, err := s.P.ResolveStopByID(ctx, stopID)
	var unknown *UnknownStopError
	if errors.As(err, &unknown) {
		return nil, fmt.Errorf("%w: %v", store.ErrNotFound, err)
	}
	if err != nil {
		return nil, err
	}
	out := &api.StopDetail{ActivityID: d.ActivityID, Bookable: d.Bookable, DurationMin: d.DurationMin}
	if d.PriceCents != nil {
		v := int(*d.PriceCents)
		out.PriceCents = &v
	}
	if d.WebsiteURL != "" {
		v := d.WebsiteURL
		out.WebsiteURL = &v
	}
	return out, nil
}

// planError maps the planner's errors to the app's sentences: a pool that
// expired (or is not the caller's) is 404, a stop id the plan does not know
// is 400.
func planError(err error) error {
	var unknown *UnknownStopError
	switch {
	case errors.Is(err, ErrPoolNotFound), errors.Is(err, ErrUnknownOption):
		return httpx.NotFound(MsgPlanExpired)
	case errors.As(err, &unknown):
		return httpx.BadRequest(MsgPlanChanged)
	}
	return err
}

// now is the planner clock at the request's business time: Route,
// Alternatives and ResolveStop read no clock of their own (a pool's window
// is fixed and its expiry is real time), so only Generate and More need it.
func (s *Service) now(ctx context.Context) time.Time { return democlock.Now(ctx, s.P.Clock.Now()) }

func emptyBatch(reason string) contract.PlanBatch {
	return contract.PlanBatch{Options: []contract.PlanOption{}, Done: true, Reason: &reason}
}

func userID(u *models.User) string {
	if u == nil {
		return ""
	}
	return u.ID.Hex()
}

// --- users --------------------------------------------------------------------

// UserFromModel is what the planner reads from the user document. The
// profile vectors pass through as stored: an empty or all-zero one means
// no profile, and one whose size differs from the catalog's vectors is
// never compared or sent (the planner checks dimensions itself). The
// legacy `embedding` field is never read.
func UserFromModel(u *models.User, now time.Time) *UserContext {
	if u == nil {
		return &UserContext{AgeBracket: string(contract.AgeAdult)}
	}
	uc := &UserContext{
		ID: u.ID.Hex(), Catalog: u.Catalog, City: u.City,
		AgeBracket:        string(view.AgeBracket(u.BirthDate, now)),
		Prefs:             UserPrefs{Pace: u.Prefs.Pace, Flexible: u.Prefs.Flexible(), PreferFree: u.Prefs.PreferFree, AvoidTags: u.Taste.AvoidTags},
		PositiveEmbedding: u.PositiveEmbedding, NegativeEmbedding: u.NegativeEmbedding,
		PositiveText: u.PositiveText, NegativeText: u.NegativeText,
	}
	if hb := u.HomeBase; hb != nil {
		uc.HomeBase = &Place{Name: hb.Name, Lat: hb.Lat, Lng: hb.Lng, HasCoord: validCoord(hb.Lat, hb.Lng)}
	}
	if ll := u.LastLocation; ll != nil && len(ll.Coordinates) >= 2 && validCoord(ll.Coordinates[1], ll.Coordinates[0]) {
		uc.LastLocation = &travel.Point{Lat: ll.Coordinates[1], Lng: ll.Coordinates[0]}
	}
	return uc
}

// --- contract shapes -------------------------------------------------------------

// RequestFromContract is the app's PlanRequest in planner terms; the body
// is kept, re-encoded, for plan_runs.
func RequestFromContract(req contract.PlanRequest) Request {
	r := Request{
		Start: PlaceFromContract(req.Start), End: PlaceFromContract(req.End),
		Date: req.Date.UTC(), StartTime: req.StartTime.UTC(), BackBy: req.BackBy.UTC(),
		Range: string(req.Range), Ride: string(req.Ride), OpenSeats: req.OpenSeats,
		MoodText: req.MoodText, Tags: req.Tags, Budget: req.Budget, Who: string(req.Who), Pace: string(req.Pace),
		MustInclude: req.MustInclude,
	}
	for _, m := range req.Modes {
		r.Modes = append(r.Modes, string(m))
	}
	if raw, err := json.Marshal(req); err == nil {
		r.Raw = raw
	}
	return r
}

// PlaceFromContract reads the app's Place (the coordinate is optional).
func PlaceFromContract(p contract.Place) Place {
	out := Place{Name: p.Name}
	if c := p.Coordinate; c != nil && validCoord(c.Lat, c.Lng) {
		out.Lat, out.Lng, out.HasCoord = c.Lat, c.Lng, true
	}
	return out
}

// PlaceToContract writes the app's Place.
func PlaceToContract(p Place) contract.Place {
	out := contract.Place{Name: p.Name}
	if p.HasCoord {
		out.Coordinate = &contract.Coordinate{Lat: p.Lat, Lng: p.Lng}
	}
	return out
}

// BatchToContract is a page of options; the reason only comes with an
// empty page.
func BatchToContract(b Batch) contract.PlanBatch {
	out := contract.PlanBatch{Options: make([]contract.PlanOption, 0, len(b.Options)), Done: b.Done}
	for _, o := range b.Options {
		out.Options = append(out.Options, OptionToContract(o))
	}
	if b.Cursor != "" {
		c := b.Cursor
		out.Cursor = &c
	}
	if b.Reason != "" && len(out.Options) == 0 {
		r := b.Reason
		out.Reason = &r
	}
	return out
}

// OptionToContract is one option; total_cost_cents is the sum of the known
// prices, left out when no stop's price is known.
func OptionToContract(o Option) contract.PlanOption {
	out := contract.PlanOption{ID: o.ID, Name: o.Name, Tag: o.Tag, Meta: o.Meta,
		Stops: make([]contract.PlanStop, 0, len(o.Stops)), LateFlag: o.LateFlag}
	known := false
	for _, s := range o.Stops {
		out.Stops = append(out.Stops, StopToContract(s))
		known = known || s.PriceKnown
	}
	if known {
		v := int(o.TotalCostCents)
		out.TotalCostCents = &v
	}
	return out
}

// StopToContract is one stop with the planner extras the contract defines.
func StopToContract(s Stop) contract.PlanStop {
	out := contract.PlanStop{ID: s.ID, Title: s.Title, Subtitle: s.Subtitle, Place: PlaceToContract(s.Place),
		DurationMinutes: s.DurationMinutes}
	if !s.Arrive.IsZero() {
		out.ArriveTime = contract.Ptr(s.Arrive)
	}
	if !s.Depart.IsZero() {
		out.DepartTime = contract.Ptr(s.Depart)
	}
	if k := contract.PlanStopKind(s.Kind); k.Valid() {
		out.Kind = k
	}
	flexible := s.Flexible
	out.Flexible = &flexible
	if s.ActivityID != "" {
		id := s.ActivityID
		out.ActivityID = &id
	}
	return out
}

// RouteToContract is the re-timed order: a closed place is already folded
// into broken_at and minutes_late.
func RouteToContract(r RouteResult) contract.RouteResult {
	out := contract.RouteResult{
		Legs: make([]contract.Leg, 0, len(r.Legs)), StopTimes: make([]contract.StopWindow, 0, len(r.StopTimes)),
		Arrival: contract.NewTime(r.Arrival), MinutesLate: r.MinutesLate, BrokenAt: r.BrokenAt,
	}
	for _, l := range r.Legs {
		out.Legs = append(out.Legs, contract.Leg{Mode: contract.TravelMode(l.Mode), Minutes: l.Minutes})
	}
	for _, st := range r.StopTimes {
		out.StopTimes = append(out.StopTimes, contract.StopWindow{Start: contract.NewTime(st.Start), End: contract.NewTime(st.End)})
	}
	return out
}
