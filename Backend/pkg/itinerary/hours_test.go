package itinerary

import (
	"Backend/pkg/models"
	"testing"
	"time"
)

func TestOpenIntervalsWrapsPastEndOfWeek(t *testing.T) {
	// Saturday 17:00 to Sunday 01:00, stored with close < open.
	hours := []models.WeeklyHourRange{{Open: 6*1440 + 17*60, Close: 60}}
	from := at(22, 0) // Saturday
	to := from.Add(6 * time.Hour)
	got, ok := OpenIntervals(hours, "bar", ny, from, to)
	if !ok || len(got) != 1 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if !got[0].Start.Equal(from) || !got[0].End.Equal(time.Date(2026, 9, 27, 1, 0, 0, 0, ny)) {
		t.Errorf("interval %v - %v", got[0].Start.In(ny), got[0].End.In(ny))
	}
}

func TestOpenIntervalsAllWeekAndDefaults(t *testing.T) {
	from, to := at(20, 0), at(20, 0).Add(10*time.Hour)
	got, ok := OpenIntervals([]models.WeeklyHourRange{{Open: 0, Close: 10080}}, "bar", ny, from, to)
	if !ok || len(got) != 1 || !got[0].Start.Equal(from) || !got[0].End.Equal(to) {
		t.Errorf("24/7 place: %+v", got)
	}
	got, ok = OpenIntervals(nil, "park", ny, from, to)
	if !ok || len(got) != 1 || !got[0].End.Equal(at(22, 0)) {
		t.Errorf("park default hours: %+v", got)
	}
	if _, ok := OpenIntervals(nil, "bar", ny, from, to); ok {
		t.Error("a bar without hours should be rejected")
	}
}

func TestOpenIntervalsAcrossDST(t *testing.T) {
	// US clocks go back at 02:00 on 2026-11-01 (a Sunday).
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	got, ok := OpenIntervals(daily(9, 17), "museum", ny, from, from.Add(24*time.Hour))
	if !ok || len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Start.In(ny).Hour() != 9 || got[0].End.In(ny).Hour() != 17 {
		t.Errorf("DST day: %v - %v", got[0].Start.In(ny), got[0].End.In(ny))
	}
}
