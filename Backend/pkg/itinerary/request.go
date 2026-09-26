package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // the VPS may not ship zoneinfo
)

const defaultTimezone = "America/New_York"

// Window is a plan request translated into the optimizer's terms.
type Window struct {
	From   time.Time // UTC
	BackBy time.Time // UTC
	TZ     *time.Location

	Start *travel.Point // nil when unknown
	End   *travel.Point // defaults to Start

	Mode       travel.Mode
	DriveLabel string // how drive legs are shown: "drive" or "rideshare"
	MaxLegKm   float64

	BudgetCents int64 // 0 = no budget
	Pace        string
}

// FromRequest reads the plan request. start_location and end_location are
// "lat,lng" strings; if the start can't be parsed, the user's last known
// location is used. date, start_time and back_by_time are local to the
// candidates' time zone. A back-by time at or before the start time means
// the next day.
func FromRequest(req models.PlanGenerateRequest, cands []models.Activity, user *models.User, now time.Time, cfg Config) (Window, error) {
	var w Window
	w.Start, w.End = Endpoints(req, user)

	w.TZ = resolveTimezone(cands, w.Start)

	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = now.In(w.TZ).Format("2006-01-02")
	}
	startClock := defaultIfEmpty(req.StartTime, "18:00")
	backClock := defaultIfEmpty(req.BackByTime, "22:00")

	from, err := time.ParseInLocation("2006-01-02 15:04", date+" "+startClock, w.TZ)
	if err != nil {
		return w, fmt.Errorf("bad date or start_time: %w", err)
	}
	backBy, err := time.ParseInLocation("2006-01-02 15:04", date+" "+backClock, w.TZ)
	if err != nil {
		return w, fmt.Errorf("bad back_by_time: %w", err)
	}
	if !backBy.After(from) {
		backBy = backBy.AddDate(0, 0, 1)
	}
	if !backBy.After(now) {
		return w, errors.New("the requested window has already ended")
	}
	w.From = from.UTC()
	w.BackBy = backBy.UTC()

	w.Mode, w.DriveLabel = ResolveMode(req.RideChoice, req.TravelModes)
	w.MaxLegKm = req.RangeKm
	if w.MaxLegKm <= 0 {
		w.MaxLegKm = cfg.DefaultMaxLegKm[string(w.Mode)]
	}
	w.BudgetCents = req.BudgetCents
	w.Pace = req.Pace
	return w, nil
}

// Endpoints parses start_location and end_location ("lat,lng"). An
// unparseable start falls back to the user's last known location; a missing
// end means a round trip. Either can be nil when nothing is known.
func Endpoints(req models.PlanGenerateRequest, user *models.User) (start, end *travel.Point) {
	if p, ok := travel.ParsePoint(req.StartLocation); ok {
		start = &p
	} else if user != nil && user.LastLocation != nil && len(user.LastLocation.Coordinates) >= 2 {
		p := travel.Point{Lat: user.LastLocation.Coordinates[1], Lng: user.LastLocation.Coordinates[0]}
		start = &p
	}
	if p, ok := travel.ParsePoint(req.EndLocation); ok {
		end = &p
	} else {
		end = start
	}
	return start, end
}

func defaultIfEmpty(s, def string) string {
	if s = strings.TrimSpace(s); s == "" {
		return def
	}
	return s
}

// ResolveMode picks the main travel mode from ride_choice and travel_modes.
// Any driving or rideshare choice wins, then transit, then walking. The
// label is how drive legs are shown: "drive" or "rideshare".
func ResolveMode(rideChoice string, modes []string) (travel.Mode, string) {
	all := append([]string{rideChoice}, modes...)
	hasTransit := false
	for _, m := range all {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "drive":
			return travel.Drive, "drive"
		case "rideshare", "uber", "lyft", "cover":
			return travel.Drive, "rideshare"
		case "marta", "transit", "bus", "train", "subway":
			hasTransit = true
		}
	}
	if hasTransit {
		return travel.Transit, "drive"
	}
	return travel.Walk, "drive"
}

// resolveTimezone uses the time zone of the candidate nearest the start
// point, else the most common one, else America/New_York.
func resolveTimezone(cands []models.Activity, start *travel.Point) *time.Location {
	name := ""
	if start != nil {
		best := -1.0
		for _, a := range cands {
			p, ok := activityPoint(a)
			if !ok || a.Timezone == "" {
				continue
			}
			if d := travel.HaversineKm(*start, p); best < 0 || d < best {
				best, name = d, a.Timezone
			}
		}
	}
	if name == "" {
		counts := map[string]int{}
		for _, a := range cands {
			if a.Timezone != "" {
				counts[a.Timezone]++
				if counts[a.Timezone] > counts[name] {
					name = a.Timezone
				}
			}
		}
	}
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func activityPoint(a models.Activity) (travel.Point, bool) {
	if len(a.Location.Coordinates) < 2 {
		return travel.Point{}, false
	}
	lng, lat := a.Location.Coordinates[0], a.Location.Coordinates[1]
	if lat == 0 && lng == 0 {
		return travel.Point{}, false
	}
	return travel.Point{Lat: lat, Lng: lng}, true
}
