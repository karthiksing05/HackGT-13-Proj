package ui

import (
	"embed"
	"encoding/json"
	"events/pkg/models"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

// View structs for template rendering

type HomeView struct {
	Events   []*models.Event
	Category string
	Search   string
}

type EventView struct {
	Event   *models.Event
	JSONLD  template.JS
	Host    string
	BaseURL string
}

type TicketsView struct {
	Event   *models.Event
	JSONLD  template.JS
	Host    string
	BaseURL string
}

type TicketPassView struct {
	Found         bool
	Order         *models.OrderConfirmation
	FormattedDate string
	FormattedTime string
	JSONLD        template.JS
}

type DashboardView struct {
	DemoKey         string
	Scenario        string
	Orders          []*models.OrderConfirmation
	Rejected        []models.RejectedRequest
	TotalOrders     int
	TotalGrossCents int
	Host            string
}

var baseCSS = func() template.CSS {
	b, err := templateFS.ReadFile("templates/base.css")
	if err != nil {
		return ""
	}
	return template.CSS(b)
}()

var funcMap = template.FuncMap{
	"baseCSS": func() template.CSS {
		return baseCSS
	},
	"centsToDollars": func(cents int) string {
		return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
	},
	"formatDate": func(t time.Time) string {
		return t.Format("Mon, Jan 2, 2006")
	},
	"formatTime": func(t time.Time) string {
		return t.Format("3:04 PM")
	},
	"formatDateTime": func(t time.Time) string {
		return t.Format("Mon, Jan 2 · 3:04 PM MST")
	},
	"categoryTitle": func(cat string) string {
		switch cat {
		case "live_music":
			return "Live Music"
		case "nightclub":
			return "Nightlife & Clubs"
		case "comedy":
			return "Comedy"
		case "sports_event":
			return "Sports & Games"
		case "tour":
			return "Tours & Food"
		case "class_workshop":
			return "Workshops & Classes"
		case "community_event":
			return "Community"
		default:
			return strings.ReplaceAll(cat, "_", " ")
		}
	},
}

// Parsed templates loaded from external gohtml files
var (
	homeTmpl       = template.Must(template.New("home.gohtml").Funcs(funcMap).ParseFS(templateFS, "templates/home.gohtml"))
	eventTmpl      = template.Must(template.New("event.gohtml").Funcs(funcMap).ParseFS(templateFS, "templates/event.gohtml"))
	ticketsTmpl    = template.Must(template.New("tickets.gohtml").Funcs(funcMap).ParseFS(templateFS, "templates/tickets.gohtml"))
	ticketPassTmpl = template.Must(template.New("ticket_pass.gohtml").Funcs(funcMap).ParseFS(templateFS, "templates/ticket_pass.gohtml"))
	dashboardTmpl  = template.Must(template.New("dashboard.gohtml").Funcs(funcMap).ParseFS(templateFS, "templates/dashboard.gohtml"))
)

func RenderHome(w io.Writer, view HomeView) error {
	return homeTmpl.Execute(w, view)
}

func RenderEvent(w io.Writer, view EventView) error {
	return eventTmpl.Execute(w, view)
}

func RenderTickets(w io.Writer, view TicketsView) error {
	return ticketsTmpl.Execute(w, view)
}

func RenderTicketPass(w io.Writer, view TicketPassView) error {
	return ticketPassTmpl.Execute(w, view)
}

func RenderDashboard(w io.Writer, view DashboardView) error {
	return dashboardTmpl.Execute(w, view)
}

// BuildJSONLDEvent generates valid Schema.org Event JSON-LD string.
func BuildJSONLDEvent(e *models.Event, baseURL string) string {
	obj := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Event",
		"name":        e.Title,
		"description": e.Description,
		"startDate":   e.Start.UTC().Format(time.RFC3339),
		"endDate":     e.End.UTC().Format(time.RFC3339),
		"location": map[string]any{
			"@type":   "Place",
			"name":    e.Venue,
			"address": e.Address,
		},
		"offers": map[string]any{
			"@type":         "Offer",
			"price":         fmt.Sprintf("%.2f", float64(e.UnitCents)/100.0),
			"priceCurrency": "USD",
			"availability":  "https://schema.org/InStock",
			"url":           fmt.Sprintf("%s/%s/tickets", baseURL, e.Slug),
		},
	}
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}

// BuildJSONLDReservation generates Schema.org EventReservation JSON-LD.
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
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}
