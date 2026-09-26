package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"testing"
	"time"
)

func TestFromRequestParsesLocationsAndWindow(t *testing.T) {
	cfg := DefaultConfig()
	now := at(12, 0)
	req := models.PlanGenerateRequest{
		StartLocation: "33.7766,-84.3890",
		EndLocation:   "33.7710,-84.3918",
		Date:          "2026-09-26",
		StartTime:     "18:00",
		BackByTime:    "22:30",
		TravelModes:   []string{"walk", "marta"},
		Pace:          "packed",
	}
	cands := []models.Activity{event("Show", techSquare, at(19, 0), 90, 0.8)}
	w, err := FromRequest(req, cands, nil, now, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if *w.Start != techSquare {
		t.Errorf("start = %+v", *w.Start)
	}
	if w.End == nil || w.End.Lat != 33.7710 {
		t.Errorf("end = %+v", w.End)
	}
	if !w.From.Equal(at(18, 0)) || !w.BackBy.Equal(at(22, 30)) {
		t.Errorf("window = %v .. %v", w.From.In(ny), w.BackBy.In(ny))
	}
	if w.Mode != travel.Transit || w.MaxLegKm != 10 {
		t.Errorf("mode %s, max leg %.0f km", w.Mode, w.MaxLegKm)
	}
}

func TestFromRequestBackByAfterMidnight(t *testing.T) {
	req := models.PlanGenerateRequest{StartLocation: "33.7766,-84.3890", EndLocation: "33.7766,-84.3890",
		Date: "2026-09-26", StartTime: "21:00", BackByTime: "02:00"}
	w, err := FromRequest(req, nil, nil, at(12, 0), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := w.BackBy.Sub(w.From); got != 5*time.Hour {
		t.Errorf("window length = %v, want 5h", got)
	}
}

func TestFromRequestTimezoneFromNearestCandidate(t *testing.T) {
	berlin := travel.Point{Lat: 52.52, Lng: 13.405}
	b := event("Club", berlin, time.Date(2026, 9, 26, 23, 0, 0, 0, time.UTC), 120, 0.5)
	b.Timezone = "Europe/Berlin"
	a := event("Show", techSquare, at(19, 0), 90, 0.5)
	req := models.PlanGenerateRequest{StartLocation: "52.52,13.405", EndLocation: "52.52,13.405",
		Date: "2026-09-26", StartTime: "20:00", BackByTime: "23:00"}
	w, err := FromRequest(req, []models.Activity{a, b}, nil, at(0, 0), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if w.TZ.String() != "Europe/Berlin" {
		t.Fatalf("tz = %s, want Europe/Berlin", w.TZ)
	}
	if w.From.Hour() != 18 { // 20:00 CEST = 18:00 UTC
		t.Errorf("from = %v UTC", w.From)
	}
}

func TestFromRequestFallbacks(t *testing.T) {
	user := &models.User{LastLocation: &models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-84.3890, 33.7766}}}
	req := models.PlanGenerateRequest{StartLocation: "Midtown Atlanta", Date: "2026-09-26"}
	w, err := FromRequest(req, nil, user, at(12, 0), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if w.Start == nil || *w.Start != techSquare {
		t.Errorf("start should fall back to last location, got %+v", w.Start)
	}
	if w.End != w.Start {
		t.Errorf("end should default to start")
	}
	if !w.From.Equal(at(18, 0)) || !w.BackBy.Equal(at(22, 0)) {
		t.Errorf("default window = %v .. %v", w.From.In(ny), w.BackBy.In(ny))
	}

	if _, err := FromRequest(models.PlanGenerateRequest{Date: "2026-09-25"}, nil, nil, at(12, 0), DefaultConfig()); err == nil {
		t.Error("a window that already ended should be an error")
	}
	if _, err := FromRequest(models.PlanGenerateRequest{Date: "tomorrow"}, nil, nil, at(12, 0), DefaultConfig()); err == nil {
		t.Error("a malformed date should be an error")
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		ride  string
		modes []string
		want  travel.Mode
		label string
	}{
		{"", nil, travel.Walk, "drive"},
		{"rideshare", nil, travel.Drive, "rideshare"},
		{"", []string{"walk", "drive"}, travel.Drive, "drive"},
		{"none", []string{"marta"}, travel.Transit, "drive"},
	}
	for _, c := range cases {
		m, l := ResolveMode(c.ride, c.modes)
		if m != c.want || l != c.label {
			t.Errorf("ResolveMode(%q, %v) = %s/%s, want %s/%s", c.ride, c.modes, m, l, c.want, c.label)
		}
	}
}
