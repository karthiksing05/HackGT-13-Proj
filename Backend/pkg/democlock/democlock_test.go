package democlock

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewRefusesBadValues(t *testing.T) {
	for _, tc := range []struct{ date, tz, want string }{
		{"2026-9-24", "", "DEMO_DATE"},
		{"24/09/2026", "", "DEMO_DATE"},
		{"2026-02-30", "", "DEMO_DATE"},
		{"", "", "DEMO_DATE"},
		{"2026-09-24", "Mars/Olympus_Mons", "DEMO_TZ"},
	} {
		if _, err := New(tc.date, tc.tz); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("New(%q, %q) = %v, want an error naming %s", tc.date, tc.tz, err, tc.want)
		}
	}
	c, err := New(" 2026-09-24 ", "")
	if err != nil || c.Date() != "2026-09-24" || c.Location().String() != DefaultTZ {
		t.Fatalf("New: %v %+v", err, c)
	}
}

func TestShiftKeepsTheWallClock(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	c, err := New("2026-09-24", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ real, want time.Time }{
		// Later days move back, earlier ones forward, at the same wall clock.
		{time.Date(2026, 9, 27, 14, 30, 0, 0, ny), time.Date(2026, 9, 24, 14, 30, 0, 0, ny)},
		{time.Date(2026, 9, 20, 9, 5, 7, 123e6, ny), time.Date(2026, 9, 24, 9, 5, 7, 123e6, ny)},
		{time.Date(2026, 9, 24, 23, 59, 0, 0, ny), time.Date(2026, 9, 24, 23, 59, 0, 0, ny)},
		// Just after midnight in New York is still the previous day in UTC terms.
		{time.Date(2026, 9, 28, 4, 10, 0, 0, time.UTC), time.Date(2026, 9, 24, 0, 10, 0, 0, ny)},
		// Across the end of DST (Nov 1): 6 PM EST becomes 6 PM EDT.
		{time.Date(2026, 11, 3, 18, 0, 0, 0, ny), time.Date(2026, 9, 24, 18, 0, 0, 0, ny)},
	} {
		got := c.Shift(tc.real)
		if !got.Equal(tc.want) || got.Location() != tc.real.Location() {
			t.Errorf("Shift(%s) = %s, want %s in %s", tc.real, got, tc.want, tc.real.Location())
		}
	}
	// Into DST: a demo date in winter keeps the wall clock of a summer day.
	winter, _ := New("2026-12-01", "America/New_York")
	if got := winter.Shift(time.Date(2026, 9, 27, 18, 0, 0, 0, ny)); !got.Equal(time.Date(2026, 12, 1, 18, 0, 0, 0, ny)) {
		t.Errorf("winter shift %s", got)
	}
}

func TestContext(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	c, _ := New("2026-09-24", "America/New_York")
	real := time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)
	plain := context.Background()
	if From(plain) != nil || !Now(plain, real).Equal(real) || With(plain, nil) != plain {
		t.Fatal("a context without a clock is real time")
	}
	demo := With(plain, c)
	if From(demo) != c || !Now(demo, real).Equal(time.Date(2026, 9, 24, 14, 0, 0, 0, ny)) {
		t.Fatalf("demo now: %s", Now(demo, real))
	}
	until := Now(demo, real).Add(3 * time.Hour)
	if got := RealAt(demo, until, real); !got.Equal(real.Add(3 * time.Hour)) {
		t.Fatalf("RealAt = %s, want three real hours from now", got)
	}
	if got := RealAt(plain, until, real); !got.Equal(until) {
		t.Fatalf("RealAt without a clock = %s", got)
	}
}
