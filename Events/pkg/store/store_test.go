package store

import (
	"context"
	"errors"
	"events/pkg/models"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
	order.Ticket.Barcode = "SLT-TEST-1234"
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

	// Lookup by barcode
	byBarcode, err := st.GetOrderByBarcode(ctx, "SLT-TEST-1234")
	if err != nil || byBarcode.OrderID != "SL-4F7K2" {
		t.Fatalf("GetOrderByBarcode failed: %v", err)
	}

	// Lookup by idempotency key
	byIdem, err := st.GetOrderByIdempotencyKey(ctx, "intent_abc")
	if err != nil || byIdem.OrderID != "SL-4F7K2" {
		t.Fatalf("GetOrderByIdempotencyKey failed: %v", err)
	}
}

func TestMongoStoreOrdersAndUniqueBarcode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017"))
	if err != nil {
		t.Skip("local mongodb not reachable")
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skip("local mongodb not pingable")
	}

	dbName := "sidequestz_events_test"
	db := client.Database(dbName)
	defer func() { _ = db.Drop(context.Background()) }()

	mStore := NewMongoStore(db)
	if err := mStore.EnsureIndexesAndSeed(ctx); err != nil {
		t.Fatalf("EnsureIndexesAndSeed failed: %v", err)
	}

	// Save an order
	order1 := &models.OrderConfirmation{
		OrderID:          "SL-ORD01",
		ConfirmationCode: "SL-ORD01",
		Status:           "confirmed",
		Quantity:         1,
		TotalCents:       1500,
		Barcode:          "SLT-AAAA-1111",
		Ticket: models.TicketSummary{
			TicketID:  "tkt_abc001",
			TicketURL: "http://localhost:8085/t/tkt_abc001",
			Admit:     1,
			Barcode:   "SLT-AAAA-1111",
		},
		CreatedAt: time.Now(),
	}

	if err := mStore.SaveOrder(ctx, order1, "idem_test_1"); err != nil {
		t.Fatalf("SaveOrder failed: %v", err)
	}

	// Verify order is in "orders" collection
	var found bson.M
	err = db.Collection(CollOrders).FindOne(ctx, bson.M{"orderId": "SL-ORD01"}).Decode(&found)
	if err != nil {
		t.Fatalf("Expected order to be in 'orders' collection: %v", err)
	}
	if found["barcode"] != "SLT-AAAA-1111" {
		t.Fatalf("Expected top-level barcode in orders doc, got: %v", found["barcode"])
	}

	// Verify lookup by barcode works
	gotByBarcode, err := mStore.GetOrderByBarcode(ctx, "SLT-AAAA-1111")
	if err != nil || gotByBarcode.OrderID != "SL-ORD01" {
		t.Fatalf("GetOrderByBarcode failed: %v, got: %+v", err, gotByBarcode)
	}

	// Verify duplicate barcode with different idempotency key is handled or rejected
	order2 := &models.OrderConfirmation{
		OrderID:          "SL-ORD02",
		ConfirmationCode: "SL-ORD02",
		Status:           "confirmed",
		Quantity:         1,
		TotalCents:       1500,
		Barcode:          "SLT-AAAA-1111", // duplicate barcode!
		Ticket: models.TicketSummary{
			TicketID:  "tkt_abc002",
			TicketURL: "http://localhost:8085/t/tkt_abc002",
			Admit:     1,
			Barcode:   "SLT-AAAA-1111",
		},
		CreatedAt: time.Now(),
	}

	// SaveOrder automatically retries with a new unique barcode upon collision!
	err = mStore.SaveOrder(ctx, order2, "idem_test_2")
	if err != nil {
		t.Fatalf("Expected SaveOrder to retry and resolve barcode collision: %v", err)
	}
	if order2.Barcode == "SLT-AAAA-1111" {
		t.Fatalf("Expected barcode to be refreshed on collision, but stayed same")
	}
}

func TestLiveDBMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017"))
	if err != nil {
		t.Skip("local mongodb not reachable")
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skip("local mongodb not pingable")
	}

	db := client.Database("sidequestz_events")
	mStore := NewMongoStore(db)
	if err := mStore.EnsureIndexesAndSeed(ctx); err != nil {
		t.Fatalf("EnsureIndexesAndSeed failed: %v", err)
	}

	// Verify merchant_orders is dropped
	cols, err := db.ListCollectionNames(ctx, bson.M{"name": "merchant_orders"})
	if err != nil {
		t.Fatalf("ListCollectionNames failed: %v", err)
	}
	if len(cols) > 0 {
		t.Fatalf("Expected merchant_orders to be dropped, but still exists")
	}
}
