package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	ny, _      = time.LoadLocation("America/New_York")
	techSquare = travel.Point{Lat: 33.7766, Lng: -84.3890}
)

var idCounter uint32

func newID() bson.ObjectID {
	idCounter++
	var id bson.ObjectID
	id[8] = byte(idCounter >> 24)
	id[9] = byte(idCounter >> 16)
	id[10] = byte(idCounter >> 8)
	id[11] = byte(idCounter)
	return id
}

func str(s string) *string   { return &s }
func f64(v float64) *float64 { return &v }

// at returns a time on 2026-09-26 (a Saturday) in New York.
func at(hour, min int) time.Time {
	return time.Date(2026, 9, 26, hour, min, 0, 0, ny)
}

// offset returns a point roughly dx km east and dy km north of Tech Square.
func offset(dxKm, dyKm float64) travel.Point {
	return travel.Point{Lat: techSquare.Lat + dyKm/111.0, Lng: techSquare.Lng + dxKm/92.5}
}

func event(name string, loc travel.Point, start time.Time, minutes int, score float64) models.Activity {
	s := start.UTC()
	return models.Activity{
		ID:         newID(),
		Kind:       "event",
		Name:       name,
		Category:   "theater", // attended whole; clipped stays have their own tests
		VenueName:  str(name + " Hall"),
		Location:   models.GeoJSONPoint{Type: "Point", Coordinates: []float64{loc.Lng, loc.Lat}},
		Start:      &s,
		Attendance: str("fixed_start"),
		Timezone:   "America/New_York",
		Duration:   &models.ActivityDuration{MedianMin: float64(minutes), P75Min: float64(minutes)},
		Score:      f64(score),
	}
}

func withEnd(a models.Activity, end time.Time) models.Activity {
	e := end.UTC()
	a.End = &e
	return a
}

func dropIn(a models.Activity, end time.Time) models.Activity {
	a = withEnd(a, end)
	a.Attendance = str("drop_in")
	return a
}

func place(name, category string, loc travel.Point, hours []models.WeeklyHourRange, score float64) models.Activity {
	return models.Activity{
		ID:          newID(),
		Kind:        "place",
		Name:        name,
		Category:    category,
		Location:    models.GeoJSONPoint{Type: "Point", Coordinates: []float64{loc.Lng, loc.Lat}},
		Timezone:    "America/New_York",
		WeeklyHours: hours,
		Duration:    &models.ActivityDuration{MedianMin: 60, P75Min: 75},
		Score:       f64(score),
	}
}

// Every day, open to close, as minutes of the week.
func daily(openHour, closeHour int) []models.WeeklyHourRange {
	var out []models.WeeklyHourRange
	for d := range 7 {
		out = append(out, models.WeeklyHourRange{Open: d*1440 + openHour*60, Close: d*1440 + closeHour*60})
	}
	return out
}

func window(from, backBy time.Time) Window {
	p := techSquare
	return Window{
		From: from.UTC(), BackBy: backBy.UTC(), TZ: ny,
		Start: &p, End: &p,
		Mode: travel.Walk, DriveLabel: "drive", MaxLegKm: 3, Pace: "balanced",
	}
}

func plan(w Window, acts []models.Activity, cfg Config) ([]Itinerary, []Drop) {
	nodes, drops := BuildNodes(w, acts, cfg)
	g := BuildGraph(nil, w, nodes, travel.Heuristic{}, cfg)
	return Solve(g, w, cfg), drops
}

func names(it Itinerary) string {
	s := ""
	for i, st := range it.Stops {
		if i > 0 {
			s += " -> "
		}
		s += fmt.Sprintf("%s@%s", st.Node.Act.Name, st.Node.Start.In(ny).Format("15:04"))
	}
	return s
}
