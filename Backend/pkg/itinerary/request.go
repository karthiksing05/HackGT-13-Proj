package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"strings"
	"time"
	_ "time/tzdata" // the VPS may not ship zoneinfo
)

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

	// Busy are the user's busy blocks (their calendar), sorted and merged
	// (MergeBusy): no visit and no leg is put on top of one (busy.go).
	Busy []Interval
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
