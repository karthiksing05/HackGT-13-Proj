package planner

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// Days of the week of 20–26 Sep 2026 in Saltlight: Sunday 20 … Saturday 26.
func sepDay(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, ny) }

// weekRange is one range of weekly hours: open on weekday (0 = Sunday) at
// openHour:openMin for minutes.
func weekRange(weekday, openHour, openMin, minutes int) models.WeeklyHourRange {
	open := weekday*minutesPerDay + openHour*60 + openMin
	return models.WeeklyHourRange{Open: open, Close: (open + minutes) % minutesPerWeek}
}

func TestActivityDetailMapsAnEvent(t *testing.T) {
	start := time.Date(2026, 9, 26, 18, 30, 0, 0, ny)
	a := synthEvent("  Sunset Jazz on Pier Nine ", "live_music", seasideMkt, start, 150, []string{"music", " ", "outdoor "},
		&models.ActivityPrice{Min: f64p(12), Max: f64p(20), Currency: "USD", Tier: 1})
	a.VenueName = strp("Pier Nine Bandstand")
	a.End = timep(start.Add(150 * time.Minute))
	a.Summary = strp(" Brass and sunsets on the pier. ")
	a.Description = strp("")
	a.Address = &models.ActivityAddress{Street: strp("9 Pier Rd"), Locality: strp("Saltlight Harbor"), Region: strp("GA")}
	a.Rating, a.RatingCount = f64p(4.6), intp(1204)
	a.URL = strp("https://saltlight.example/jazz")
	a.TicketURL = strp(" https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets ")
	a.ImageURL = strp("javascript:alert(1)")

	d := ActivityDetail(&a, sepDay(26))
	if d.ID != a.ID.Hex() || d.Title != "Sunset Jazz on Pier Nine" || d.Kind != contract.StopKindEvent || d.Category != "live_music" ||
		d.CategoryLabel != "Live music" || strValue(d.Summary) != "Brass and sunsets on the pier." || d.Description != nil ||
		strValue(d.VenueName) != "Pier Nine Bandstand" || strValue(d.Address) != "9 Pier Rd, Saltlight Harbor, GA" {
		t.Errorf("text fields %+v", d)
	}
	if d.Place.Name != "Pier Nine Bandstand" || d.Place.Coordinate == nil || d.Place.Coordinate.Lat != seasideMkt.Lat {
		t.Errorf("place %+v", d.Place)
	}
	if d.Start == nil || !d.Start.Equal(start) || d.End == nil || !d.End.Equal(start.Add(150*time.Minute)) || d.HoursLine != nil {
		t.Errorf("times %v %v, hours %v", d.Start, d.End, d.HoursLine)
	}
	if d.PriceCents == nil || *d.PriceCents != 1200 || d.PriceLabel != "$12–$20" {
		t.Errorf("price %v %q", d.PriceCents, d.PriceLabel)
	}
	if d.Rating == nil || *d.Rating != 4.6 || d.RatingCount == nil || *d.RatingCount != 1204 {
		t.Errorf("rating %v %v", d.Rating, d.RatingCount)
	}
	// Links the app can open, trimmed; anything else is left out.
	if strValue(d.URL) != "https://saltlight.example/jazz" || strValue(d.TicketURL) != "https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets" ||
		d.ImageURL != nil {
		t.Errorf("links %v %v %v", d.URL, d.TicketURL, d.ImageURL)
	}
	if strings.Join(d.Tags, ",") != "music,outdoor" {
		t.Errorf("tags %q", d.Tags)
	}
}

func TestActivityDetailLeavesOutWhatIsUnknown(t *testing.T) {
	a := synthPlace("Anchor Park", "park", seasideMkt, nil, nil, nil)
	a.Rating, a.RatingCount = nil, intp(40) // a count without a rating
	a.Start = timep(time.Date(2026, 9, 26, 9, 0, 0, 0, ny))
	d := ActivityDetail(&a, sepDay(26))
	if d.Kind != contract.StopKindPlace || d.Start != nil || d.End != nil || d.CategoryLabel != "Park" || d.Place.Name != "Anchor Park" ||
		d.Summary != nil || d.Address != nil || d.VenueName != nil || d.HoursLine != nil || d.PriceCents != nil ||
		d.PriceLabel != "Price unknown" || d.Rating != nil || d.RatingCount != nil || d.URL != nil {
		t.Errorf("unknowns %+v", d)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"tags":[]`, `"price_label":"Price unknown"`, `"place":{"name":"Anchor Park","coordinate":`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s missing from %s", key, raw)
		}
	}
	for _, key := range []string{"summary", "hours_line", "price_cents", "rating", "start", "url"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("%s written for an unknown: %s", key, raw)
		}
	}

	// Ratings are out of 5; a document without a location still names its place.
	for _, r := range []float64{0, 5.5, math.NaN()} {
		a.Rating = f64p(r)
		if got := ActivityDetail(&a, sepDay(26)).Rating; got != nil {
			t.Errorf("rating %v kept as %v", r, *got)
		}
	}
	a.Location = models.GeoJSONPoint{}
	if p := ActivityDetail(&a, sepDay(26)).Place; p.Name != "Anchor Park" || p.Coordinate != nil {
		t.Errorf("no location: %+v", p)
	}
}

func TestHoursLine(t *testing.T) {
	line := func(hours []models.WeeklyHourRange, source string, day int) string {
		a := synthPlace("Somewhere", "shopping", seasideMkt, hours, nil, nil)
		if source != "" {
			a.HoursSource = &source
		}
		if s := hoursLine(&a, sepDay(day)); s != nil {
			return *s
		}
		return "<nil>"
	}
	// Monday to Saturday 10–6, closed on Sundays.
	shop := []models.WeeklyHourRange{}
	for d := 1; d <= 6; d++ {
		shop = append(shop, weekRange(d, 10, 0, 8*60))
	}
	lunchAndDinner := []models.WeeklyHourRange{weekRange(5, 17, 0, 5*60), weekRange(5, 11, 0, 3*60)}
	// A bar: every night 5 PM–2 AM except Sunday, Saturday's wrapping past the end of the week.
	bar := []models.WeeklyHourRange{}
	for d := 1; d <= 6; d++ {
		bar = append(bar, weekRange(d, 17, 0, 9*60))
	}
	for _, c := range []struct {
		name   string
		hours  []models.WeeklyHourRange
		source string
		day    int
		want   string
	}{
		{"open", shop, "google", 26, "Open 10 AM–6 PM"},
		{"closed", shop, "google", 20, "Closed that day"},
		{"any source but default", shop, "", 22, "Open 10 AM–6 PM"},
		{"assumed hours aren't known", shop, "default", 26, "<nil>"},
		{"no hours", nil, "google", 26, "<nil>"},
		{"minutes", []models.WeeklyHourRange{weekRange(6, 10, 30, 7*60+30)}, "osm", 26, "Open 10:30 AM–6 PM"},
		{"two ranges in order", lunchAndDinner, "google", 25, "Open 11 AM–2 PM, 5–10 PM"},
		{"past midnight", bar, "google", 25, "Open 5 PM–2 AM"},
		{"wraps past the week", bar, "google", 26, "Open 5 PM–2 AM"},
		{"Saturday's night isn't Sunday's", bar, "google", 20, "Closed that day"},
		{"24/7", []models.WeeklyHourRange{{Open: 0, Close: minutesPerWeek}}, "google", 23, "Open 24 hours"},
		{"a whole day", []models.WeeklyHourRange{weekRange(1, 0, 0, minutesPerDay)}, "google", 21, "Open 24 hours"},
		{"a whole day, other days closed", []models.WeeklyHourRange{weekRange(1, 0, 0, minutesPerDay)}, "google", 22, "Closed that day"},
		{"to 11:59 PM", []models.WeeklyHourRange{weekRange(3, 0, 0, minutesPerDay-1)}, "google", 23, "Open 24 hours"},
		{"overlaps merge", []models.WeeklyHourRange{weekRange(4, 9, 0, 3*60), weekRange(4, 11, 0, 3*60), weekRange(4, 9, 0, 60)}, "google", 24, "Open 9 AM–2 PM"},
		{"empty range", []models.WeeklyHourRange{{Open: 600, Close: 600}}, "google", 20, "Closed that day"},
	} {
		if got := line(c.hours, c.source, c.day); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// The date is the same weekday wherever it's read.
	a := synthPlace("Somewhere", "shopping", seasideMkt, shop, nil, nil)
	if got := hoursLine(&a, time.Date(2026, 9, 20, 0, 0, 0, 0, time.FixedZone("Pacific/Kiritimati", 14*3600))); got == nil || *got != "Closed that day" {
		t.Errorf("Sunday in another zone: %v", got)
	}
}

func TestPriceLabel(t *testing.T) {
	price := func(p *models.ActivityPrice) string {
		a := synthPlace("Somewhere", "shopping", seasideMkt, nil, nil, p)
		return priceLabel(&a)
	}
	for _, c := range []struct {
		name  string
		price *models.ActivityPrice
		want  string
	}{
		{"unknown", nil, "Price unknown"},
		{"a tier only", &models.ActivityPrice{Tier: 2}, "$$"},
		{"nothing stated", &models.ActivityPrice{Currency: "USD"}, "Price unknown"},
		{"free", &models.ActivityPrice{IsFree: true}, "Free"},
		{"zero", priceOf(0), "Free"},
		{"free even with a max", &models.ActivityPrice{Min: f64p(0), Max: f64p(10), IsFree: true}, "Free"},
		{"up to", &models.ActivityPrice{Min: f64p(0), Max: f64p(20)}, "Up to $20"},
		{"one price", priceOf(18), "$18"},
		{"cents", priceOf(12.5), "$12.50"},
		{"a range", &models.ActivityPrice{Min: f64p(6), Max: f64p(12), Tier: 1}, "$6–$12"},
		{"a bad max", &models.ActivityPrice{Min: f64p(6), Max: f64p(math.NaN())}, "$6"},
		{"the legacy seed's cents", &models.ActivityPrice{Cents: 1500}, "$15"},
	} {
		if got := price(c.price); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func intp(v int) *int { return &v }
