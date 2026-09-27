package planner

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Minutes of weeklyHours: counted from Sunday 00:00 local time.
const (
	minutesPerDay  = 24 * 60
	minutesPerWeek = 7 * minutesPerDay
)

// ActivityDetail is GET /activities/{id} for one catalog document: what
// Create › Review's stop pane shows beyond the stop itself. The labels are
// the ones stops use (categoryLabel, priceLabel); day is the plan's local
// day and only the hours line reads it.
func ActivityDetail(a *models.Activity, day time.Time) contract.ActivityDetail {
	d := contract.ActivityDetail{
		ID: a.ID.Hex(), Title: strings.TrimSpace(a.Name), Kind: contract.StopKindPlace,
		Category: a.Category, CategoryLabel: categoryLabel(a.Category),
		Summary: nonEmpty(a.Summary), Description: nonEmpty(a.Description), VenueName: nonEmpty(a.VenueName),
		HoursLine: hoursLine(a, day), PriceLabel: priceLabel(a),
		URL: webURL(a.URL), TicketURL: webURL(a.TicketURL), ImageURL: webURL(a.ImageURL),
		Tags: []string{},
	}
	if a.Kind == "event" {
		d.Kind = contract.StopKindEvent
		if a.Start != nil {
			d.Start = contract.Ptr(a.Start.UTC())
			if a.End != nil && a.End.After(*a.Start) {
				d.End = contract.Ptr(a.End.UTC())
			}
		}
	}
	if address := activityAddress(a); address != "" {
		d.Address = &address
	}
	if pt, ok := activityPoint(a); ok {
		d.Place = PlaceToContract(PlaceAt(placeName(a), pt))
	} else {
		d.Place = contract.Place{Name: placeName(a)}
	}
	if cents, known := activityCost(a); known {
		v := int(cents)
		d.PriceCents = &v
	}
	// A rating is out of 5 (NaN fails both tests); its count only goes with it.
	if r := a.Rating; r != nil && *r > 0 && *r <= 5 {
		v := *r
		d.Rating = &v
		if n := a.RatingCount; n != nil && *n > 0 {
			count := *n
			d.RatingCount = &count
		}
	}
	for _, tag := range a.Tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			d.Tags = append(d.Tags, tag)
		}
	}
	return d
}

// hoursLine is the opening hours on day, read the way Google lists them: a
// range belongs to the day it opens, so a bar open Saturday 5 PM to Sunday
// 2 AM is "Open 5 PM–2 AM" on Saturday. Ranges join with commas ("Open
// 11 AM–2 PM, 5–10 PM"); a whole day or more (a whole week is how 24/7 is
// stored) is "Open 24 hours", and a day no range opens on is "Closed that
// day". The weekly hours are in the activity's own time zone, and a date is
// the same weekday everywhere, so day's weekday picks the ranges. Unknown
// hours are nil: no weekly hours, or hours an ingester assumed (hoursSource
// "default").
func hoursLine(a *models.Activity, day time.Time) *string {
	if len(a.WeeklyHours) == 0 || strValue(a.HoursSource) == "default" {
		return nil
	}
	type span struct{ open, length int } // minutes of the day, minutes long
	weekday := int(day.Weekday())
	var spans []span
	for _, r := range a.WeeklyHours {
		length := floorMod(r.Close-r.Open, minutesPerWeek)
		if length == 0 && r.Close != r.Open {
			length = minutesPerWeek // {0, 10080}: open all week
		}
		switch {
		case length == 0:
			continue
		case length >= minutesPerWeek:
			return strPtr("Open 24 hours")
		}
		if open := floorMod(r.Open, minutesPerWeek); open/minutesPerDay == weekday {
			spans = append(spans, span{open % minutesPerDay, length})
		}
	}
	if len(spans) == 0 {
		return strPtr("Closed that day")
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].open < spans[j].open })
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.open <= last.open+last.length {
			last.length = max(last.length, s.open+s.length-last.open)
			continue
		}
		merged = append(merged, s)
	}
	parts := make([]string, 0, len(merged))
	for _, s := range merged {
		// 12 AM–11:59 PM is how some sources write a whole day.
		if s.length >= minutesPerDay-1 {
			return strPtr("Open 24 hours")
		}
		parts = append(parts, httpx.TimeRange(clockAt(s.open), clockAt(s.open+s.length)))
	}
	return strPtr("Open " + strings.Join(parts, ", "))
}

// clockAt is a minute of the day as a time, for the clock labels.
func clockAt(minute int) time.Time {
	return time.Date(2000, 1, 1, 0, minute%minutesPerDay, 0, 0, time.UTC)
}

func floorMod(v, m int) int { return (v%m + m) % m }

// nonEmpty is s trimmed, nil when that leaves nothing.
func nonEmpty(s *string) *string {
	if v := strings.TrimSpace(strValue(s)); v != "" {
		return &v
	}
	return nil
}

// webURL keeps a link the app can open in its browser: an absolute http(s)
// URL, trimmed.
func webURL(s *string) *string {
	v := strings.TrimSpace(strValue(s))
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	return &v
}

func strPtr(s string) *string { return &s }
