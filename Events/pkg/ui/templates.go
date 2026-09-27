// Package ui renders the ticket website: the event listings, each event's
// page, checkout, the ticket, and plain message pages (not found, payment
// problems).
package ui

import (
	"embed"
	"encoding/json"
	"events/pkg/models"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // Saltlight Harbor's clock, even on hosts without tzdata
)

//go:embed templates/*
var templateFS embed.FS

// SiteName is the website's name.
const SiteName = "Saltlight Tickets"

// Harbor is the city's time zone: every date and time on the site is local.
var Harbor = func() *time.Location {
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		return loc
	}
	return time.FixedZone("EDT", -4*3600)
}()

// MaxTicketsPerOrder caps the quantity picker.
const MaxTicketsPerOrder = 8

// Meta is what every page's <head> needs.
type Meta struct {
	Title       string
	Description string
	JSONLD      template.JS // schema.org JSON-LD, or empty
}

// Category is one filter chip on the listings.
type Category struct {
	Key    string
	Label  string
	Active bool
}

// Day is the listings' events on one local date.
type Day struct {
	Label  string // "Saturday, September 26"
	Events []*models.Event
}

// HomeView is the listings page.
type HomeView struct {
	Meta
	Days       []Day
	Categories []Category
	Category   string
	Search     string
	Count      int
}

// EventView is one event's page.
type EventView struct {
	Meta
	Event   *models.Event
	SoldOut bool
	FewLeft bool
}

// CheckoutView is the checkout page (and the form again after a mistake).
type CheckoutView struct {
	Meta
	Event    *models.Event
	SoldOut  bool
	MaxQty   int
	Quantity int
	Name     string
	Email    string
	Error    string
}

// TicketView is the ticket page.
type TicketView struct {
	Meta
	Order *models.OrderConfirmation
	Start time.Time
	Bars  []Bar
}

// MessageView is a plain page with one message and a way back.
type MessageView struct {
	Meta
	Heading   string
	Body      string
	LinkURL   string
	LinkLabel string
}

// Bar is one bar of the ticket's barcode.
type Bar struct {
	X, W int
}

var funcMap = template.FuncMap{
	"site":  func() string { return SiteName },
	"money": Money,
	"local": func(t time.Time) time.Time { return t.In(Harbor) },
	"date": func(t time.Time) string {
		return t.In(Harbor).Format("Mon, Jan 2")
	},
	"longDate": func(t time.Time) string {
		return t.In(Harbor).Format("Monday, January 2, 2006")
	},
	"clock": func(t time.Time) string {
		return t.In(Harbor).Format("3:04 PM")
	},
	"weekday": func(t time.Time) string { return strings.ToUpper(t.In(Harbor).Format("Mon")) },
	"monthDay": func(t time.Time) string {
		return strings.ToUpper(t.In(Harbor).Format("Jan")) + " " + t.In(Harbor).Format("2")
	},
	"category": CategoryLabel,
	"brand":    BrandName,
	"fees":     models.CalculateFees,
	"mul":      func(a, b int) int { return a * b },
	"add":      func(a, b int) int { return a + b },
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
}

// Money is cents as dollars: "$12" for whole dollars, "$12.96" otherwise.
func Money(cents int) string {
	if cents%100 == 0 {
		return fmt.Sprintf("$%d", cents/100)
	}
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// BrandName is a card brand as people write it.
func BrandName(brand string) string {
	switch strings.ToLower(brand) {
	case "visa":
		return "Visa"
	case "mastercard":
		return "Mastercard"
	case "amex", "american_express":
		return "American Express"
	case "discover":
		return "Discover"
	case "":
		return "Card"
	}
	return brand
}

var categoryLabels = map[string]string{
	"live_music":      "Live music",
	"nightclub":       "Nightlife",
	"bar":             "Bars",
	"comedy":          "Comedy",
	"theater":         "Theater",
	"festival":        "Festivals",
	"sports_event":    "Sports",
	"tour":            "Tours",
	"class_workshop":  "Classes",
	"community_event": "Community",
	"rec_venue":       "Games",
	"restaurant":      "Food & drink",
}

// CategoryLabel is a category's name on the site.
func CategoryLabel(key string) string {
	if label, ok := categoryLabels[key]; ok {
		return label
	}
	return strings.ReplaceAll(key, "_", " ")
}

// Categories are the chips for every category in events, by name.
func Categories(events []*models.Event, active string) []Category {
	seen := map[string]bool{}
	var out []Category
	for _, e := range events {
		if e.Category == "" || seen[e.Category] {
			continue
		}
		seen[e.Category] = true
		out = append(out, Category{Key: e.Category, Label: CategoryLabel(e.Category), Active: e.Category == active})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// GroupByDay sorts events by start and groups them by local date.
func GroupByDay(events []*models.Event) []Day {
	sorted := append([]*models.Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var days []Day
	for _, e := range sorted {
		label := e.Start.In(Harbor).Format("Monday, January 2")
		if len(days) == 0 || days[len(days)-1].Label != label {
			days = append(days, Day{Label: label})
		}
		days[len(days)-1].Events = append(days[len(days)-1].Events, e)
	}
	return days
}

// Barcode draws code as bars: the same code always gives the same bars.
func Barcode(code string) []Bar {
	h := fnv.New64a()
	var bars []Bar
	x := 0
	for i := 0; x < 280; i++ {
		_, _ = h.Write([]byte{byte(i)})
		_, _ = h.Write([]byte(code))
		v := h.Sum64()
		w := int(v%3) + 1
		gap := int((v>>8)%3) + 1
		bars = append(bars, Bar{X: x, W: w * 2})
		x += w*2 + gap*2
	}
	return bars
}

func page(name string) *template.Template {
	return template.Must(template.New(name).Funcs(funcMap).ParseFS(templateFS, "templates/layout.gohtml", "templates/"+name))
}

var (
	homeTmpl     = page("home.gohtml")
	eventTmpl    = page("event.gohtml")
	checkoutTmpl = page("checkout.gohtml")
	ticketTmpl   = page("ticket.gohtml")
	messageTmpl  = page("message.gohtml")
)

// RenderHome writes the listings page.
func RenderHome(w io.Writer, v HomeView) error { return homeTmpl.Execute(w, v) }

// RenderEvent writes an event's page.
func RenderEvent(w io.Writer, v EventView) error { return eventTmpl.Execute(w, v) }

// RenderCheckout writes the checkout page.
func RenderCheckout(w io.Writer, v CheckoutView) error { return checkoutTmpl.Execute(w, v) }

// RenderTicket writes the ticket page.
func RenderTicket(w io.Writer, v TicketView) error { return ticketTmpl.Execute(w, v) }

// RenderMessage writes a message page.
func RenderMessage(w io.Writer, v MessageView) error { return messageTmpl.Execute(w, v) }

// BuildJSONLDEvent is the schema.org Event for an event's pages.
func BuildJSONLDEvent(e *models.Event, baseURL string) string {
	availability := "https://schema.org/InStock"
	if e.Remaining <= 0 {
		availability = "https://schema.org/SoldOut"
	}
	obj := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Event",
		"name":        e.Title,
		"description": e.Description,
		"startDate":   e.Start.UTC().Format(time.RFC3339),
		"endDate":     e.End.UTC().Format(time.RFC3339),
		"image":       e.ImageURL,
		"location": map[string]any{
			"@type":   "Place",
			"name":    e.Venue,
			"address": e.Address,
		},
		"offers": map[string]any{
			"@type":         "Offer",
			"price":         fmt.Sprintf("%.2f", float64(e.UnitCents)/100.0),
			"priceCurrency": "USD",
			"availability":  availability,
			"url":           fmt.Sprintf("%s/%s/tickets", strings.TrimRight(baseURL, "/"), e.Slug),
		},
	}
	b, _ := json.MarshalIndent(obj, "", "  ")
	return string(b)
}

// BuildJSONLDReservation is the schema.org EventReservation for a ticket.
func BuildJSONLDReservation(order *models.OrderConfirmation) string {
	obj := map[string]any{
		"@context":          "https://schema.org",
		"@type":             "EventReservation",
		"reservationNumber": order.ConfirmationCode,
		"reservationStatus": "https://schema.org/ReservationConfirmed",
		"underName": map[string]any{
			"@type": "Person",
			"name":  order.Buyer.Name,
		},
		"reservationFor": map[string]any{
			"@type":     "Event",
			"name":      order.Event.Title,
			"startDate": order.Event.StartsAt,
			"location": map[string]any{
				"@type": "Place",
				"name":  order.Event.Venue,
			},
		},
		"numSeats":      order.Quantity,
		"totalPrice":    fmt.Sprintf("%.2f", float64(order.TotalCents)/100.0),
		"priceCurrency": "USD",
		"ticket": map[string]any{
			"@type":        "Ticket",
			"ticketNumber": order.Ticket.Barcode,
			"ticketToken":  order.Ticket.TicketID,
		},
	}
	b, _ := json.MarshalIndent(obj, "", "  ")
	return string(b)
}
