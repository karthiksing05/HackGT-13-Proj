package store

import (
	"context"
	"errors"
	"events/pkg/models"
	"testing"
	"time"
)

func TestMemoryStoreSeedingAndEvents(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	st.SeedDefaultEvents()

	// Check that 25 events are seeded
	events, err := st.ListEvents(ctx, "", "")
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 25 {
		t.Fatalf("expected 25 seeded events, got %d", len(events))
	}

	// Check a specific event
	jazz, err := st.GetEvent(ctx, "sunset-jazz-on-pier-nine")
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if jazz.Title != "Sunset Jazz on Pier Nine" {
		t.Errorf("expected Title 'Sunset Jazz on Pier Nine', got %s", jazz.Title)
	}
	if jazz.UnitCents != 1200 {
		t.Errorf("expected UnitCents 1200, got %d", jazz.UnitCents)
	}

	// Reserved slug must return ErrNotFound
	_, err = st.GetEvent(ctx, "api")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for reserved slug 'api', got %v", err)
	}
}

func TestStoreReserveAndReleaseInventory(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	st.SeedDefaultEvents()

	slug := "sunset-jazz-on-pier-nine"
	// Initial capacity is 60
	err := st.ReserveTickets(ctx, slug, 50)
	if err != nil {
		t.Fatalf("ReserveTickets failed: %v", err)
	}
	e, _ := st.GetEvent(ctx, slug)
	if e.Remaining != 10 {
		t.Fatalf("expected remaining 10, got %d", e.Remaining)
	}

	// Reserving more than remaining fails with ErrSoldOut
	err = st.ReserveTickets(ctx, slug, 15)
	if !errors.Is(err, ErrSoldOut) {
		t.Fatalf("expected ErrSoldOut, got %v", err)
	}

	// Releasing restores inventory
	err = st.ReleaseTickets(ctx, slug, 20)
	if err != nil {
		t.Fatalf("ReleaseTickets failed: %v", err)
	}
	e, _ = st.GetEvent(ctx, slug)
	if e.Remaining != 30 {
		t.Fatalf("expected remaining 30, got %d", e.Remaining)
	}
}

func TestStoreQuotesAndOrders(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	st.SeedDefaultEvents()

	// Quote
	quote := &models.Quote{
		QuoteID:        "q_test123",
		Event:          models.EventSummary{Slug: "sunset-jazz-on-pier-nine", Title: "Sunset Jazz on Pier Nine"},
		QuoteExpiresAt: time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       2,
		UnitCents:      1200,
		FeesCents:      310,
		TotalCents:     2710,
		ExpiresAt:      time.Now().Add(10 * time.Minute),
	}
	if err := st.SaveQuote(ctx, quote); err != nil {
		t.Fatal(err)
	}

	gotQ, err := st.GetQuote(ctx, "q_test123")
	if err != nil {
		t.Fatalf("GetQuote failed: %v", err)
	}
	if gotQ.TotalCents != 2710 {
		t.Errorf("expected 2710 total cents, got %d", gotQ.TotalCents)
	}

	// Order
	order := &models.OrderConfirmation{
		OrderID:          "SL-4F7K2",
		ConfirmationCode: "SL-4F7K2",
		Status:           "confirmed",
		Sandbox:          true,
		Quantity:         2,
		TotalCents:       2710,
		Ticket: models.TicketSummary{
			TicketID:  "tkt_1234567890abcdef",
			TicketURL: "https://events.sidequestz.tech/t/tkt_1234567890abcdef",
			Admit:     2,
		},
		CreatedAt: time.Now(),
	}
	if err := st.SaveOrder(ctx, order, "intent_abc"); err != nil {
		t.Fatal(err)
	}

	// Lookup by order ID
	byID, err := st.GetOrder(ctx, "SL-4F7K2")
	if err != nil || byID.OrderID != "SL-4F7K2" {
		t.Fatalf("GetOrder failed: %v", err)
	}

	// Lookup by ticket ID
	byTkt, err := st.GetOrderByTicketID(ctx, "tkt_1234567890abcdef")
	if err != nil || byTkt.OrderID != "SL-4F7K2" {
		t.Fatalf("GetOrderByTicketID failed: %v", err)
	}

	// Lookup by idempotency key
	byIdem, err := st.GetOrderByIdempotencyKey(ctx, "intent_abc")
	if err != nil || byIdem.OrderID != "SL-4F7K2" {
		t.Fatalf("GetOrderByIdempotencyKey failed: %v", err)
	}
}
