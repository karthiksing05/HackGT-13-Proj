package itinerary

import (
	"Backend/pkg/models"
	"sort"
	"time"
)

const minutesPerWeek = 7 * 24 * 60

// Interval is a half-open time range [Start, End).
type Interval struct {
	Start time.Time
	End   time.Time
}

// Opening hours assumed for places with no weeklyHours, as local
// [open, close) minutes of the day, one entry per category. Categories
// missing here (bars, nightclubs, breweries, tours) are dropped instead:
// guessing their hours is too risky.
var defaultDailyHours = map[string][2]int{
	"park":         {6 * 60, 22 * 60},
	"hike":         {6 * 60, 20 * 60},
	"landmark":     {0, 24 * 60},
	"viewpoint":    {0, 24 * 60},
	"museum":       {10 * 60, 17 * 60},
	"gallery":      {11 * 60, 18 * 60},
	"garden":       {8 * 60, 19 * 60},
	"shopping":     {10 * 60, 21 * 60},
	"market":       {8 * 60, 14 * 60},
	"rec_venue":    {10 * 60, 22 * 60},
	"zoo_aquarium": {9 * 60, 17 * 60},
	"restaurant":   {11 * 60, 22 * 60},
	"cafe":         {7 * 60, 18 * 60},
	"bakery":       {7 * 60, 17 * 60},
	"dessert":      {12 * 60, 22 * 60},
	"food_hall":    {10 * 60, 21 * 60},
}

// OpenIntervals returns when a place is open inside [from, to).
//
// weeklyHours holds minutes from the start of the week in the place's local
// time, with day 0 = Sunday (Google Places' convention). A range whose close
// is before its open wraps past the end of the week, e.g. Saturday 17:00 to
// Sunday 01:00. ok is false when the place has no hours and its category has
// no safe default.
func OpenIntervals(hours []models.WeeklyHourRange, category string, loc *time.Location, from, to time.Time) ([]Interval, bool) {
	if len(hours) == 0 {
		daily, ok := defaultDailyHours[category]
		if !ok {
			return nil, false
		}
		for day := range 7 {
			hours = append(hours, models.WeeklyHourRange{
				Open:  day*24*60 + daily[0],
				Close: day*24*60 + daily[1],
			})
		}
	}

	local := from.In(loc)
	// Sunday 00:00 of the week containing `from`.
	weekStart := time.Date(local.Year(), local.Month(), local.Day()-int(local.Weekday()), 0, 0, 0, 0, loc)

	var out []Interval
	// The previous week covers ranges that wrap into this one; the next week
	// covers windows that cross Saturday night.
	for week := -1; week <= 1; week++ {
		for _, r := range hours {
			open, close := r.Open, r.Close
			if close < open {
				close += minutesPerWeek
			}
			if close == open {
				continue
			}
			// time.Date normalizes minute overflow and stays correct across DST.
			s := time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day()+week*7, 0, open, 0, 0, loc)
			e := time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day()+week*7, 0, close, 0, 0, loc)
			if s.Before(from) {
				s = from
			}
			if e.After(to) {
				e = to
			}
			if e.After(s) {
				out = append(out, Interval{Start: s.UTC(), End: e.UTC()})
			}
		}
	}
	return mergeIntervals(out), true
}

func mergeIntervals(in []Interval) []Interval {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start.Before(in[j].Start) })
	out := []Interval{in[0]}
	for _, iv := range in[1:] {
		last := &out[len(out)-1]
		if !iv.Start.After(last.End) {
			if iv.End.After(last.End) {
				last.End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}
