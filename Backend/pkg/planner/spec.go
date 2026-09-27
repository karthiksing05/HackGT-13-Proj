package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // the VPS may not ship zoneinfo
)

const defaultTZ = "America/New_York"

// ParsePlanRequest decodes a request body (the app's PlanRequest or the
// legacy shape) and normalises it. tzHeader is the X-Time-Zone value, used
// when the city's zone is unknown.
func ParsePlanRequest(raw []byte, tzHeader string, now time.Time, user *UserContext, cfg Config) (PlanSpec, error) {
	req, err := DecodeRequest(raw)
	if err != nil {
		return PlanSpec{}, err
	}
	return BuildSpec(req, tzHeader, now, user, cfg)
}

// wire shapes -------------------------------------------------------------

type wirePlace struct {
	Name       string `json:"name"`
	Coordinate *struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"coordinate"`
}

func (p wirePlace) place() Place {
	out := Place{Name: strings.TrimSpace(p.Name)}
	if p.Coordinate != nil && validCoord(p.Coordinate.Lat, p.Coordinate.Lng) {
		out.Lat, out.Lng, out.HasCoord = p.Coordinate.Lat, p.Coordinate.Lng, true
	}
	return out
}

func validCoord(lat, lng float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lng) || (lat == 0 && lng == 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

type appRequest struct {
	Start     wirePlace `json:"start"`
	End       wirePlace `json:"end"`
	Date      string    `json:"date"`
	StartTime string    `json:"start_time"`
	BackBy    string    `json:"back_by"`
	Range     string    `json:"range"`
	Ride      string    `json:"ride"`
	OpenSeats *int      `json:"open_seats"`
	MoodText  string    `json:"mood_text"`
	Tags      []string  `json:"tags"`
	Budget    *int      `json:"budget"`
	Who       string    `json:"who"`
	Pace      string    `json:"pace"`
	Modes     []string  `json:"modes"`
	// MustInclude is additive (MUSTSEE §2).
	MustInclude []string `json:"must_include"`
}

type legacyRequest struct {
	StartLocation string   `json:"start_location"`
	EndLocation   string   `json:"end_location"`
	Date          string   `json:"date"`
	StartTime     string   `json:"start_time"`
	BackByTime    string   `json:"back_by_time"`
	RangeKm       float64  `json:"range_km"`
	RideChoice    string   `json:"ride_choice"`
	OpenSeats     int      `json:"open_seats"`
	MoodText      string   `json:"mood_text"`
	Tags          []string `json:"tags"`
	BudgetCents   int64    `json:"budget_cents"`
	Pace          string   `json:"pace"`
	TravelModes   []string `json:"travel_modes"`
}

// DecodeRequest reads either wire shape. The app shape is recognised by
// `start` being an object.
func DecodeRequest(raw []byte) (Request, error) {
	var probe struct {
		Start json.RawMessage `json:"start"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Request{}, &RequestError{Reason: "malformed JSON"}
	}
	if bytes.HasPrefix(bytes.TrimSpace(probe.Start), []byte("{")) {
		return decodeAppRequest(raw)
	}
	return decodeLegacyRequest(raw)
}

func decodeAppRequest(raw []byte) (Request, error) {
	var a appRequest
	if err := json.Unmarshal(raw, &a); err != nil {
		return Request{}, &RequestError{Reason: "malformed plan request"}
	}
	req := Request{
		Start: a.Start.place(), End: a.End.place(),
		Range: strings.ToLower(strings.TrimSpace(a.Range)), Ride: strings.ToLower(strings.TrimSpace(a.Ride)),
		OpenSeats: a.OpenSeats, MoodText: a.MoodText, Tags: a.Tags,
		Who: strings.ToLower(strings.TrimSpace(a.Who)), Pace: strings.ToLower(strings.TrimSpace(a.Pace)),
		Modes: a.Modes, MustInclude: a.MustInclude, Raw: json.RawMessage(append([]byte(nil), raw...)),
	}
	if a.Budget != nil {
		req.Budget = *a.Budget
	} else {
		req.Budget = 3
	}
	var err error
	if req.StartTime, err = parseInstant(a.StartTime); err != nil {
		return req, &RequestError{Reason: "start_time is not a valid time"}
	}
	if req.BackBy, err = parseInstant(a.BackBy); err != nil {
		return req, &RequestError{Reason: "back_by is not a valid time"}
	}
	if a.Date != "" {
		if req.Date, err = parseInstant(a.Date); err != nil {
			return req, &RequestError{Reason: "date is not a valid time"}
		}
	}
	return req, nil
}

func decodeLegacyRequest(raw []byte) (Request, error) {
	var l legacyRequest
	if err := json.Unmarshal(raw, &l); err != nil {
		return Request{}, &RequestError{Reason: "malformed plan request"}
	}
	req := Request{
		Legacy:       true,
		LegacyDate:   strings.TrimSpace(l.Date),
		LegacyStart:  strings.TrimSpace(l.StartTime),
		LegacyBackBy: strings.TrimSpace(l.BackByTime),
		RangeKm:      l.RangeKm,
		Ride:         strings.ToLower(strings.TrimSpace(l.RideChoice)),
		MoodText:     l.MoodText,
		Tags:         l.Tags,
		BudgetCents:  l.BudgetCents,
		Budget:       3,
		Pace:         strings.ToLower(strings.TrimSpace(l.Pace)),
		Modes:        l.TravelModes,
		Raw:          json.RawMessage(append([]byte(nil), raw...)),
	}
	if l.OpenSeats > 0 {
		seats := l.OpenSeats
		req.OpenSeats = &seats
	}
	if p, ok := travel.ParsePoint(l.StartLocation); ok {
		req.Start = PlaceAt("", p)
	} else {
		req.Start = Place{Name: strings.TrimSpace(l.StartLocation)}
	}
	if p, ok := travel.ParsePoint(l.EndLocation); ok {
		req.End = PlaceAt("", p)
	} else {
		req.End = Place{Name: strings.TrimSpace(l.EndLocation)}
	}
	return req, nil
}

// parseInstant accepts RFC 3339 (with or without fractions) and a bare
// date, which means UTC midnight.
func parseInstant(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}

// BuildSpec normalises a decoded request for a user (§3 of the design).
func BuildSpec(req Request, tzHeader string, now time.Time, user *UserContext, cfg Config) (PlanSpec, error) {
	if user == nil {
		user = &UserContext{}
	}
	spec := PlanSpec{UserID: user.ID, Raw: req.Raw, Now: now, MustInclude: normalizePicks(req.MustInclude)}
	if len(spec.MustInclude) > MaxMustInclude {
		return spec, ErrTooManyPicks
	}

	catalog, ok := NormalizeCatalog(user.Catalog)
	if !ok {
		catalog = store.ActivityCollection
	}
	spec.Catalog = catalog

	// Start point: request → home base → last known location → the city's
	// default. The name always describes the point actually used.
	home := homeBase(user)
	if p := req.Start.Point(); p != nil {
		spec.Start, spec.StartName = p, req.Start.Name
	} else if home != nil {
		p := home.Point()
		spec.Start, spec.StartName = p, home.Name
	} else if user.LastLocation != nil && validCoord(user.LastLocation.Lat, user.LastLocation.Lng) {
		p := *user.LastLocation
		spec.Start, spec.StartName = &p, "Current location"
	}

	// City: the user's, else the nearest to the start (unknown when none is
	// within CitySnapKm); with no start at all, the catalog's own city,
	// whose default point becomes the start.
	var city City
	if c, ok := CityBySlug(user.City); ok {
		city = c
	} else if spec.Start != nil {
		city, _ = NearestCity(*spec.Start, cfg.CitySnapKm)
	} else {
		city, _ = CityBySlug(DefaultCityForCatalog(catalog))
	}
	spec.City = city.Slug
	if spec.Start == nil {
		p := city.Start
		spec.Start, spec.StartName = &p, city.StartName
	}

	// End: the request's, else back to the start.
	if p := req.End.Point(); p != nil {
		spec.End, spec.EndName = p, req.End.Name
	} else {
		p := *spec.Start
		spec.End, spec.EndName = &p, spec.StartName
	}

	// Demo snap: a phone far from the user's catalog city plans from the
	// user's home base when it is in that city, else the city's default point.
	if _, ok := CityBySlug(user.City); ok && travel.HaversineKm(*spec.Start, city.Center) > cfg.CitySnapKm {
		p, name := city.Start, city.StartName
		if home != nil && travel.HaversineKm(*home.Point(), city.Center) <= cfg.CitySnapKm {
			p, name = *home.Point(), home.Name
		}
		spec.Start, spec.End = &p, &p
		spec.StartName, spec.EndName = name, name
		spec.SnappedStart = true
	}

	// Time zone: the city's, else the header's, else the default.
	spec.TZ = loadTZ(city.TZ, tzHeader)

	// Window.
	if req.Legacy {
		from, backBy, err := legacyWindow(req, spec.TZ, now)
		if err != nil {
			return spec, err
		}
		spec.From, spec.BackBy = from, backBy
	} else {
		if req.StartTime.IsZero() || req.BackBy.IsZero() {
			return spec, &RequestError{Reason: "start_time and back_by are required"}
		}
		spec.From = req.StartTime.UTC()
		spec.BackBy = req.BackBy.UTC()
		if !spec.BackBy.After(spec.From) {
			spec.BackBy = spec.BackBy.AddDate(0, 0, 1)
		}
	}
	if !spec.BackBy.After(now) {
		return spec, &RequestError{Reason: "window already ended"}
	}
	// A window that already started is planned from now (the next whole
	// minute): nothing is scheduled in the past and an event that is over,
	// or too far along to drop in on, is not offered.
	if spec.From.Before(now) {
		spec.From = ceilMinute(now.UTC())
		if !spec.BackBy.After(spec.From) {
			return spec, &RequestError{Reason: "window already ended"}
		}
	}
	spec.LocalDate = spec.From.In(spec.TZ).Format("2006-01-02")

	// Travel mode and per-leg range.
	spec.Mode, spec.DriveLabel = resolveMode(req.Ride, req.Modes, req.Legacy)
	spec.Range = req.Range
	switch {
	case req.Legacy && req.RangeKm > 0:
		spec.MaxLegKm = req.RangeKm
	default:
		if km, ok := cfg.RangeKm[spec.Range]; ok {
			spec.MaxLegKm = km
		} else {
			spec.MaxLegKm = cfg.Itinerary.DefaultMaxLegKm[string(spec.Mode)]
			spec.Range = rangeLabel(spec.MaxLegKm, cfg)
		}
	}
	if spec.MaxLegKm <= 0 {
		spec.MaxLegKm = cfg.RangeKm["walkable"]
	}
	if spec.Mode == travel.Walk && cfg.WalkLegCapKm > 0 && spec.MaxLegKm > cfg.WalkLegCapKm {
		spec.MaxLegKm = cfg.WalkLegCapKm
	}

	// Budget.
	switch {
	case req.Legacy && req.BudgetCents > 0:
		spec.Budget = Budget{Tier: 3, TotalCents: req.BudgetCents}
	case req.Legacy:
		spec.Budget = Budget{Tier: 3}
	default:
		spec.Budget = BudgetForLevel(req.Budget)
	}

	spec.Pace = normalizePace(req.Pace, user.Prefs.Pace)
	spec.Who = req.Who
	if req.OpenSeats != nil && *req.OpenSeats > 0 {
		spec.OpenSeats = *req.OpenSeats
	}
	spec.MoodText = strings.TrimSpace(req.MoodText)
	spec.QuickPicks = uniqueStrings(trimAll(req.Tags))
	spec.Hard, spec.Facets = ExtractMoodConstraints(spec.MoodText, spec.QuickPicks)
	if spec.Budget.FreeOnly {
		spec.Hard.FreeOnly = true
	} else if spec.Hard.FreeOnly {
		spec.Budget = BudgetForLevel(0)
	}
	// Avoided tags match tags and categories, ignoring case ("LIVE_MUSIC"
	// avoids the live_music category); the catalog's vocabulary is lower case.
	spec.AvoidTags = uniqueStrings(lowerAll(trimAll(user.Prefs.AvoidTags)))
	spec.Hard.ExcludeTags = uniqueStrings(append(spec.Hard.ExcludeTags, spec.AvoidTags...))
	spec.Hard.ExcludeCategories = uniqueStrings(append(spec.Hard.ExcludeCategories, spec.AvoidTags...))
	slices.Sort(spec.Hard.ExcludeTags)
	slices.Sort(spec.Hard.ExcludeCategories)
	spec.AgeBracket = NormalizeAgeBracket(user.AgeBracket)
	spec.Flexible = user.Prefs.Flexible
	return spec, nil
}

// homeBase is the user's home base when it has a usable coordinate.
func homeBase(user *UserContext) *Place {
	hb := user.HomeBase
	if hb == nil || !hb.HasCoord || !validCoord(hb.Lat, hb.Lng) {
		return nil
	}
	out := *hb
	if strings.TrimSpace(out.Name) == "" {
		out.Name = "Home base"
	}
	return &out
}

// ceilMinute rounds t up to a whole minute.
func ceilMinute(t time.Time) time.Time {
	if m := t.Truncate(time.Minute); m.Before(t) {
		return m.Add(time.Minute)
	}
	return t
}

func loadTZ(cityTZ, header string) *time.Location {
	for _, name := range []string{cityTZ, strings.TrimSpace(header), defaultTZ} {
		if name == "" {
			continue
		}
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.UTC
}

func legacyWindow(req Request, tz *time.Location, now time.Time) (time.Time, time.Time, error) {
	date := req.LegacyDate
	if date == "" {
		date = now.In(tz).Format("2006-01-02")
	}
	start := req.LegacyStart
	if start == "" {
		start = "18:00"
	}
	backBy := req.LegacyBackBy
	if backBy == "" {
		backBy = "22:00"
	}
	from, err := time.ParseInLocation("2006-01-02 15:04", date+" "+start, tz)
	if err != nil {
		return time.Time{}, time.Time{}, &RequestError{Reason: "bad date or start_time"}
	}
	to, err := time.ParseInLocation("2006-01-02 15:04", date+" "+backBy, tz)
	if err != nil {
		return time.Time{}, time.Time{}, &RequestError{Reason: "bad back_by_time"}
	}
	if !to.After(from) {
		to = to.AddDate(0, 0, 1)
	}
	return from.UTC(), to.UTC(), nil
}

// resolveMode picks the main travel mode. The app's ride answer wins:
// drive → driving legs, cover → rideshare legs, none → transit when a
// transit mode was ticked, else walking. Legacy requests keep the old
// rule where any driving entry in travel_modes counts.
func resolveMode(ride string, modes []string, legacy bool) (travel.Mode, string) {
	switch ride {
	case "drive":
		return travel.Drive, "drive"
	case "cover", "rideshare", "uber", "lyft":
		return travel.Drive, "rideshare"
	case "none":
		if hasTransitMode(modes) {
			return travel.Transit, "drive"
		}
		return travel.Walk, "drive"
	}
	if legacy {
		return itinerary.ResolveMode(ride, modes)
	}
	if hasTransitMode(modes) {
		return travel.Transit, "drive"
	}
	return travel.Walk, "drive"
}

func hasTransitMode(modes []string) bool {
	for _, m := range modes {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "marta", "transit", "bus", "train", "subway":
			return true
		}
	}
	return false
}

func rangeLabel(km float64, cfg Config) string {
	best, bestDiff := "", math.Inf(1)
	for _, name := range []string{"walkable", "transit", "anywhere"} {
		if d := math.Abs(cfg.RangeKm[name] - km); d < bestDiff {
			best, bestDiff = name, d
		}
	}
	return best
}

func normalizePace(requested, usual string) string {
	for _, p := range []string{requested, usual} {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "relaxed", "chill":
			return "chill"
		case "balanced":
			return "balanced"
		case "packed":
			return "packed"
		}
	}
	return "balanced"
}

// paceTarget is the stop count a pace aims for (§5.2).
func paceTarget(pace string) int {
	switch pace {
	case "chill":
		return 2
	case "packed":
		return 4
	}
	return 3
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
