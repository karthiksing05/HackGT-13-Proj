package httpx

import (
	"fmt"
	"strings"
	"time"
)

// Formatting helpers for the labels the app shows verbatim. Callers pass
// times already converted with t.In(httpx.TZ(r)).

// Clock is "5:12 PM" / "6:00 PM" (minutes always shown: message times, "Locks 5:00 PM").
func Clock(t time.Time) string { return t.Format("3:04 PM") }

// ClockShort drops ":00": "6 PM", "6:30 PM" ("Free until 6:30 PM", "Locks Fri 9 PM").
func ClockShort(t time.Time) string {
	if t.Minute() == 0 {
		return t.Format("3 PM")
	}
	return t.Format("3:04 PM")
}

// TimeRange is "5:30–8 PM", "6–10 AM" or "11 AM–1 PM" (en dash, shared meridiem).
func TimeRange(start, end time.Time) string {
	if start.Format("PM") == end.Format("PM") {
		return clockNoMeridiem(start) + "–" + ClockShort(end)
	}
	return ClockShort(start) + "–" + ClockShort(end)
}

func clockNoMeridiem(t time.Time) string {
	if t.Minute() == 0 {
		return t.Format("3")
	}
	return t.Format("3:04")
}

// MonthDay is "Sep 25".
func MonthDay(t time.Time) string { return t.Format("Jan 2") }

// Weekday is "Thu".
func Weekday(t time.Time) string { return t.Format("Mon") }

// DayLabel is "Today", "Tomorrow", a weekday within the week, else "Oct 3"
// (forum "when", group subtitles). now and t are in the same zone.
func DayLabel(t, now time.Time) string {
	day := func(x time.Time) time.Time {
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location())
	}
	d := day(t)
	n := day(now)
	switch diff := int(d.Sub(n).Hours() / 24); {
	case diff == 0:
		return "Today"
	case diff == 1:
		return "Tomorrow"
	case diff > 1 && diff < 7:
		return Weekday(t)
	}
	return MonthDay(t)
}

// Dollars is "$9", "$9.50" or "-$4" for negative amounts.
func Dollars(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	if cents%100 == 0 {
		return fmt.Sprintf("%s$%d", sign, cents/100)
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

// Plural is "1 person" / "3 people".
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Miles is "0.4 mi".
func Miles(mi float64) string { return strings.TrimSuffix(fmt.Sprintf("%.1f", mi), ".0") + " mi" }
