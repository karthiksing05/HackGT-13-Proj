package ui

import (
	"bytes"
	"events/pkg/models"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestRenderAllGoHTMLTemplates(t *testing.T) {
	sampleEvent := &models.Event{
		Slug:        "test-event",
		Title:       "Test Concert at the Marina",
		Description: "A wonderful concert by the harbor.",
		Category:    "live_music",
		Venue:       "Marina Stage",
		Address:     "123 Ocean Ave",
		Start:       time.Now(),
		End:         time.Now().Add(2 * time.Hour),
		UnitCents:   1500,
		Capacity:    60,
		Remaining:   45,
		ImageURL:    "https://example.com/test.jpg",
	}

	// 1. Test HomeTemplate
	var bufHome bytes.Buffer
	err := RenderHome(&bufHome, HomeView{
		Events:   []*models.Event{sampleEvent},
		Category: "live_music",
		Search:   "Concert",
	})
	if err != nil {
		t.Fatalf("RenderHome failed: %v", err)
	}
	if !strings.Contains(bufHome.String(), "Test Concert at the Marina") {
		t.Errorf("RenderHome output missing event title")
	}

	// 2. Test EventTemplate
	var bufEvent bytes.Buffer
	err = RenderEvent(&bufEvent, EventView{
		Event:   sampleEvent,
		JSONLD:  template.JS(`{"@type":"Event"}`),
		Host:    "events.sidequestz.tech",
		BaseURL: "http://localhost:8085",
	})
	if err != nil {
		t.Fatalf("RenderEvent failed: %v", err)
	}
	if !strings.Contains(bufEvent.String(), "Test Concert at the Marina") {
		t.Errorf("RenderEvent output missing event title")
	}

	// 3. Test TicketsTemplate
	var bufTickets bytes.Buffer
	err = RenderTickets(&bufTickets, TicketsView{
		Event:   sampleEvent,
		JSONLD:  template.JS(`{"@type":"Offer"}`),
		Host:    "events.sidequestz.tech",
		BaseURL: "http://localhost:8085",
	})
	if err != nil {
		t.Fatalf("RenderTickets failed: %v", err)
	}
	if !strings.Contains(bufTickets.String(), "rel=\"agent-checkout\"") {
		t.Errorf("RenderTickets output missing rel=agent-checkout link")
	}

	// 4. Test TicketPassTemplate
	sampleOrder := &models.OrderConfirmation{
		OrderID:          "SL-TEST1",
		ConfirmationCode: "SL-TEST1",
		Status:           "confirmed",
		Quantity:         2,
		TotalCents:       3310,
		Event:            sampleEvent.SummaryView(),
		Buyer:            models.BuyerInfo{Name: "Sandy Byte", Email: "demo@sidequestz.tech"},
		Ticket: models.TicketSummary{
			TicketID:  "tkt_12345",
			TicketURL: "http://localhost:8085/t/tkt_12345",
			Admit:     2,
			Barcode:   "SLT-1234-5678",
		},
		Payment: models.PaymentSummary{
			Scheme: "visa_agent_token",
			Last4:  "1881",
			AuthID: "sbx_auth_123",
		},
	}
	var bufPass bytes.Buffer
	err = RenderTicketPass(&bufPass, TicketPassView{
		Found:         true,
		Order:         sampleOrder,
		FormattedDate: "Sat, Sep 26, 2026",
		FormattedTime: "8:00 PM EDT",
		JSONLD:        template.JS(`{"@type":"EventReservation"}`),
	})
	if err != nil {
		t.Fatalf("RenderTicketPass failed: %v", err)
	}
	if !strings.Contains(bufPass.String(), "SL-TEST1") || !strings.Contains(bufPass.String(), "Sandy Byte") {
		t.Errorf("RenderTicketPass output missing confirmation code or buyer name")
	}

	// 5. Test DashboardTemplate
	var bufDash bytes.Buffer
	err = RenderDashboard(&bufDash, DashboardView{
		DemoKey:         "test-demo-key",
		Scenario:        "normal",
		Orders:          []*models.OrderConfirmation{sampleOrder},
		Rejected:        []models.RejectedRequest{},
		TotalOrders:     1,
		TotalGrossCents: 3310,
		Host:            "events.sidequestz.tech",
	})
	if err != nil {
		t.Fatalf("RenderDashboard failed: %v", err)
	}
	if !strings.Contains(bufDash.String(), "Merchant Booth Screen") {
		t.Errorf("RenderDashboard output missing title")
	}
}
