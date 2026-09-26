package httpx

import (
	"net/http"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the VPS may not ship zoneinfo
)

// DefaultTimeZone applies when the app sends no (or an unknown) X-Time-Zone.
const DefaultTimeZone = "America/New_York"

var (
	tzCache   sync.Map // IANA name → *time.Location
	defaultTZ = mustLocation(DefaultTimeZone)
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("httpx: " + err.Error())
	}
	return loc
}

// Location resolves an IANA zone name, caching successes; unknown names fall
// back to the default zone.
func Location(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return defaultTZ
	}
	if cached, ok := tzCache.Load(name); ok {
		return cached.(*time.Location)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return defaultTZ
	}
	tzCache.Store(name, loc)
	return loc
}

// TZ is the request's zone from X-Time-Zone (default America/New_York). Use it
// for anything day-based: calendar days, "today", plan dates, "Free until 6:30 PM".
func TZ(r *http.Request) *time.Location {
	return Location(r.Header.Get("X-Time-Zone"))
}

// DayKey is the YYYY-MM-DD id of t's local day in loc.
func DayKey(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// LocalMidnight is the start of t's local day in loc.
func LocalMidnight(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// ParseDay reads a "YYYY-MM-DD" query value as local midnight in loc.
func ParseDay(s string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(s), loc)
}
