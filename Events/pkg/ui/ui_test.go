package ui

import (
	"bytes"
	"events/pkg/models"
	"html/template"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sampleEvent(start time.Time) *models.Event {
	return &models.Event{
		Slug: "test-event", Title: "Test Concert at the Marina", Description: "A wonderful concert by the harbor.",
		Summary: "A concert.", Category: "live_music", Tags: []string{"outdoor", "low_energy"}, Venue: "Marina Stage",
		Address: "123 Ocean Ave", Start: start, End: start.Add(2 * time.Hour), UnitCents: 1500, Capacity: 60,
		Remaining: 12, ImageURL: "https://example.com/test.jpg",
	}
}

// The site reads as an ordinary ticket website: none of the machinery shows.
func assertOrdinary(t *testing.T, page, html string) {
	t.Helper()
	lower := strings.ToLower(html)
	for _, word := range []string{"agent", "sandbox", "muse", "spt", "sidequestz", "demo", "tap signature", "shared payment"} {
		if strings.Contains(lower, word) {
			t.Errorf("%s mentions %q", page, word)
		}
	}
}

func TestPagesRender(t *testing.T) {
	start := time.Date(2026, 9, 26, 23, 0, 0, 0, time.UTC) // 7:00 PM in Saltlight Harbor
	ev := sampleEvent(start)
	ld := template.JS(BuildJSONLDEvent(ev, "https://tickets.example"))

	var home bytes.Buffer
	if err := RenderHome(&home, HomeView{
		Meta: Meta{Title: SiteName}, Days: GroupByDay([]*models.Event{ev}),
		Categories: Categories([]*models.Event{ev}, "live_music"), Category: "live_music", Search: "Concert", Count: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Test Concert at the Marina", "Saturday, September 26", "7:00 PM", "$15", "Live music"} {
		if !strings.Contains(home.String(), want) {
			t.Errorf("home is missing %q", want)
		}
	}
	assertOrdinary(t, "home", home.String())

	var event bytes.Buffer
	if err := RenderEvent(&event, EventView{Meta: Meta{Title: ev.Title, JSONLD: ld}, Event: ev, FewLeft: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Get tickets", "/test-event/tickets", "Only 12 left", "application/ld+json", "7:00 PM – 9:00 PM"} {
		if !strings.Contains(event.String(), want) {
			t.Errorf("event page is missing %q", want)
		}
	}
	assertOrdinary(t, "event", event.String())

	var checkout bytes.Buffer
	if err := RenderCheckout(&checkout, CheckoutView{Meta: Meta{Title: "Checkout", JSONLD: ld}, Event: ev, MaxQty: 8, Quantity: 2,
		Name: "Sandy Byte", Error: "Enter a valid email address for your tickets."}); err != nil {
		t.Fatal(err)
	}
	// 2 × $15 = $30, fees $2.40 + $1.00 = $3.40, total $33.40.
	for _, want := range []string{`method="post"`, `action="/test-event/tickets"`, "$30", "$3.40", "$33.40", `value="2" selected`,
		`value="Sandy Byte"`, "Enter a valid email address", "Continue to payment"} {
		if !strings.Contains(checkout.String(), want) {
			t.Errorf("checkout is missing %q", want)
		}
	}
	assertOrdinary(t, "checkout", checkout.String())

	order := &models.OrderConfirmation{
		OrderID: "SL-TEST1", ConfirmationCode: "SL-TEST1", Status: "confirmed", Quantity: 2, SubtotalCents: 3000,
		FeesCents: 340, TotalCents: 3340, Event: ev.SummaryView(),
		Buyer:   models.BuyerInfo{Name: "Sandy Byte", Email: "sandy@example.com"},
		Ticket:  models.TicketSummary{TicketID: "tkt_12345", Admit: 2, Barcode: "SLT-1234-5678"},
		Payment: models.PaymentSummary{Scheme: models.PaymentSchemeStripeSPT, Brand: "visa", Last4: "4242"},
	}
	var ticket bytes.Buffer
	if err := RenderTicket(&ticket, TicketView{Meta: Meta{Title: "Your tickets", JSONLD: template.JS(BuildJSONLDReservation(order))},
		Order: order, Start: start, Bars: Barcode(order.Ticket.Barcode)}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SL-TEST1", "Sandy Byte", "SLT-1234-5678", "Visa •••• 4242", "$33.40", "<rect"} {
		if !strings.Contains(ticket.String(), want) {
			t.Errorf("ticket is missing %q", want)
		}
	}
	assertOrdinary(t, "ticket", ticket.String())

	var msg bytes.Buffer
	if err := RenderMessage(&msg, MessageView{Meta: Meta{Title: "Page not found"}, Heading: "Page not found", Body: "Nope.", LinkURL: "/", LinkLabel: "Browse events"}); err != nil {
		t.Fatal(err)
	}
	assertOrdinary(t, "message", msg.String())
}

func TestGroupByDayUsesHarborTime(t *testing.T) {
	late := sampleEvent(time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)) // 10 PM Sep 26 local
	early := sampleEvent(time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC))
	next := sampleEvent(time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC))
	days := GroupByDay([]*models.Event{next, late, early})
	if len(days) != 2 || days[0].Label != "Saturday, September 26" || len(days[0].Events) != 2 || days[0].Events[0] != early {
		t.Fatalf("days: %+v", days)
	}
}

func TestBarcodeIsStable(t *testing.T) {
	a, b := Barcode("SLT-1234-5678"), Barcode("SLT-1234-5678")
	if !reflect.DeepEqual(a, b) || reflect.DeepEqual(a, Barcode("SLT-0000-0000")) || len(a) < 20 {
		t.Fatalf("barcode not stable or not distinct (%d bars)", len(a))
	}
}

func TestMoney(t *testing.T) {
	for cents, want := range map[int]string{1200: "$12", 1296: "$12.96", 5: "$0.05"} {
		if got := Money(cents); got != want {
			t.Errorf("Money(%d) = %s, want %s", cents, got, want)
		}
	}
}
