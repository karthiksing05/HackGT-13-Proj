package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var checkoutNow = time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

func checkoutIntent(userID, state string, due *time.Time) *models.CheckoutIntent {
	return &models.CheckoutIntent{
		UserID: userID, ItemID: "item-1", ItineraryID: "itin-1", ItemTitle: "Sunset Jazz", Quantity: 1,
		CardBrand: "Visa", CardLast4: "4242", State: state, Steps: []models.CheckoutStep{{Text: "Waiting for your approval"}},
		NextTransitionAt: due,
	}
}

func TestCheckoutIntentTransitions(t *testing.T) {
	s := testutil.Store(t, func() time.Time { return checkoutNow })
	ctx := context.Background()
	intents := s.CheckoutIntents()

	in := checkoutIntent("user-1", models.CheckoutAwaitingApproval, nil)
	if err := intents.Insert(ctx, in); err != nil {
		t.Fatal(err)
	}
	if in.ID == "" || !in.CreatedAt.Equal(checkoutNow) {
		t.Fatalf("Insert did not fill id/timestamps: %+v", in)
	}
	if _, err := intents.Get(ctx, "user-2", in.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("someone else's intent must be ErrNotFound: %v", err)
	}
	card := &models.PaymentMethod{ID: "pm-2", Brand: "Mastercard", Last4: "5454"}
	if _, err := intents.SetCard(ctx, "user-2", in.ID, card); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetCard by another user: %v", err)
	}
	got, err := intents.SetCard(ctx, "user-1", in.ID, card)
	if err != nil || got.CardLast4 != "5454" || got.PaymentMethodID == nil || *got.PaymentMethodID != "pm-2" {
		t.Fatalf("SetCard: %v %+v", err, got)
	}
	due := checkoutNow.Add(time.Second)
	got, err = intents.Approve(ctx, "user-1", in.ID, "tk-1", "SQ-4F7K2", due)
	if err != nil || got.State != models.CheckoutProcessing || got.TicketID != "tk-1" || got.NextTransitionAt == nil || !got.NextTransitionAt.Equal(due) {
		t.Fatalf("Approve: %v %+v", err, got)
	}
	// Past awaiting approval every guard refuses with the current state.
	var conflict *store.CheckoutStateError
	for name, call := range map[string]func() (*models.CheckoutIntent, error){
		"approve": func() (*models.CheckoutIntent, error) {
			return intents.Approve(ctx, "user-1", in.ID, "tk-2", "SQ-XXXXX", due)
		},
		"cancel":  func() (*models.CheckoutIntent, error) { return intents.Cancel(ctx, "user-1", in.ID) },
		"setcard": func() (*models.CheckoutIntent, error) { return intents.SetCard(ctx, "user-1", in.ID, card) },
	} {
		current, err := call()
		if !errors.As(err, &conflict) || conflict.State != models.CheckoutProcessing || !errors.Is(err, store.ErrConflict) {
			t.Fatalf("%s while processing: %v", name, err)
		}
		if current == nil || current.TicketID != "tk-1" {
			t.Fatalf("%s must return the unchanged intent: %+v", name, current)
		}
	}
	if _, err := intents.Cancel(ctx, "user-1", "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Cancel unknown: %v", err)
	}

	// Cancel clears the pending step.
	pending := checkoutIntent("user-1", models.CheckoutPreparing, &due)
	if err := intents.Insert(ctx, pending); err != nil {
		t.Fatal(err)
	}
	got, err = intents.Cancel(ctx, "user-1", pending.ID)
	if err != nil || got.State != models.CheckoutCancelled || got.NextTransitionAt != nil {
		t.Fatalf("Cancel: %v %+v", err, got)
	}
}

func TestCheckoutDueAndAdvance(t *testing.T) {
	s := testutil.Store(t, func() time.Time { return checkoutNow })
	ctx := context.Background()
	intents := s.CheckoutIntents()
	past, future := checkoutNow.Add(-time.Second), checkoutNow.Add(time.Minute)
	ready := checkoutIntent("user-1", models.CheckoutPreparing, &past)
	paying := checkoutIntent("user-1", models.CheckoutProcessing, &checkoutNow)
	later := checkoutIntent("user-1", models.CheckoutPreparing, &future)
	waiting := checkoutIntent("user-1", models.CheckoutAwaitingApproval, nil)
	for _, in := range []*models.CheckoutIntent{ready, paying, later, waiting} {
		if err := intents.Insert(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	due, err := intents.Due(ctx, checkoutNow, 10)
	if err != nil || len(due) != 2 || due[0].ID != ready.ID || due[1].ID != paying.ID {
		t.Fatalf("Due = %v %+v, want the ready and paying intents, oldest first", err, due)
	}
	got, ok, err := intents.Advance(ctx, ready.ID, models.CheckoutPreparing, checkoutNow, bson.M{"state": models.CheckoutAwaitingApproval})
	if err != nil || !ok || got.State != models.CheckoutAwaitingApproval || got.NextTransitionAt != nil {
		t.Fatalf("Advance: %v %v %+v", err, ok, got)
	}
	// The same step again, a step from the wrong state and a step not due yet all miss.
	for name, try := range map[string]func() (bool, error){
		"twice": func() (bool, error) {
			_, ok, err := intents.Advance(ctx, ready.ID, models.CheckoutPreparing, checkoutNow, bson.M{"state": models.CheckoutAwaitingApproval})
			return ok, err
		},
		"wrong state": func() (bool, error) {
			_, ok, err := intents.Advance(ctx, paying.ID, models.CheckoutPreparing, checkoutNow, bson.M{"state": models.CheckoutAwaitingApproval})
			return ok, err
		},
		"not due": func() (bool, error) {
			_, ok, err := intents.Advance(ctx, later.ID, models.CheckoutPreparing, checkoutNow, bson.M{"state": models.CheckoutAwaitingApproval})
			return ok, err
		},
	} {
		if ok, err := try(); ok || err != nil {
			t.Fatalf("Advance %s: ok=%v err=%v", name, ok, err)
		}
	}
	if _, err := intents.ByTicket(ctx, "tk-9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByTicket before booking: %v", err)
	}
	if _, ok, err := intents.Advance(ctx, paying.ID, models.CheckoutProcessing, checkoutNow,
		bson.M{"state": models.CheckoutBooked, "ticketId": "tk-9", "confirmation": "SQ-4F7K2"}); !ok || err != nil {
		t.Fatalf("book: %v %v", ok, err)
	}
	if got, err := intents.ByTicket(ctx, "tk-9"); err != nil || got.ID != paying.ID {
		t.Fatalf("ByTicket: %v %+v", err, got)
	}
	if _, err := intents.ByTicket(ctx, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByTicket empty id: %v", err)
	}
}

func TestCheckoutReads(t *testing.T) {
	s := testutil.Store(t, func() time.Time { return checkoutNow })
	ctx := context.Background()
	db := s.DB()
	reads := s.CheckoutReads()

	for i, card := range []models.PaymentMethod{
		{ID: "pm-b", UserID: "user-1", Brand: "Visa", Last4: "4242", IsDefault: true, CreatedAt: checkoutNow.Add(time.Minute)},
		{ID: "pm-a", UserID: "user-1", Brand: "Mastercard", Last4: "5454", CreatedAt: checkoutNow},
		{ID: "pm-c", UserID: "user-2", Brand: "Visa", Last4: "1881", IsDefault: true, CreatedAt: checkoutNow},
	} {
		if _, err := db.Collection(store.CollPaymentMethods).InsertOne(ctx, card); err != nil {
			t.Fatalf("card %d: %v", i, err)
		}
	}
	cards, err := reads.Cards(ctx, "user-1")
	if err != nil || len(cards) != 2 || cards[0].ID != "pm-a" || cards[1].ID != "pm-b" {
		t.Fatalf("Cards = %v %+v, want user-1's oldest first", err, cards)
	}
	if _, err := reads.Card(ctx, "user-1", "pm-c"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("someone else's card: %v", err)
	}

	itin := models.Itinerary{ID: "itin-1", HostID: "user-1", MemberIDs: []string{"user-1", "user-2"}, Title: "Evening out",
		Status: models.ItineraryActive, Items: []models.ItineraryItem{{ID: "leg-1", Kind: models.ItemTransit}, {ID: "stop-1", Kind: models.ItemStop, Title: "Sunset Jazz"}}}
	gone := models.Itinerary{ID: "itin-2", HostID: "user-1", MemberIDs: []string{"user-1"}, Status: models.ItineraryDeleted,
		Items: []models.ItineraryItem{{ID: "stop-2", Kind: models.ItemStop}}}
	for _, doc := range []models.Itinerary{itin, gone} {
		if _, err := db.Collection(store.CollItineraries).InsertOne(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	if gotItin, item, err := reads.MemberItem(ctx, "user-2", "stop-1"); err != nil || gotItin.ID != "itin-1" || item.Title != "Sunset Jazz" {
		t.Fatalf("MemberItem for a member: %v", err)
	}
	for name, try := range map[string][2]string{
		"not a member": {"user-3", "stop-1"},
		"deleted plan": {"user-1", "stop-2"},
		"unknown item": {"user-1", "stop-9"},
	} {
		if _, _, err := reads.MemberItem(ctx, try[0], try[1]); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("MemberItem %s: %v", name, err)
		}
	}
	if _, item, err := reads.Item(ctx, "itin-2", "stop-2"); err != nil || item.ID != "stop-2" {
		t.Fatalf("Item ignores membership and status: %v", err)
	}

	// SaveTicket creates the state, keeps what else is there, and is repeatable.
	note := "bring cash"
	if _, err := db.Collection(store.CollItemStates).InsertOne(ctx, models.ItemState{
		ID: models.PairID("user-2", "stop-1"), UserID: "user-2", ItemID: "stop-1", ItineraryID: "itin-1", Notes: &note, TransitMode: "walk",
	}); err != nil {
		t.Fatal(err)
	}
	total, code, url := 2400, "SQ-4F7K2", "http://api.test/tickets/tk-1"
	ticket := models.ItemTicket{ID: "tk-1", Quantity: 2, TotalCents: &total, Confirmation: &code, URL: &url}
	for _, user := range []string{"user-1", "user-2", "user-2"} {
		if err := reads.SaveTicket(ctx, user, "itin-1", "stop-1", ticket); err != nil {
			t.Fatal(err)
		}
	}
	n, err := db.Collection(store.CollItemStates).CountDocuments(ctx, bson.M{})
	if err != nil || n != 2 {
		t.Fatalf("item states = %d (%v), want one per user", n, err)
	}
	var created, kept models.ItemState
	if err := db.Collection(store.CollItemStates).FindOne(ctx, bson.M{"_id": models.PairID("user-1", "stop-1")}).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.UserID != "user-1" || created.ItemID != "stop-1" || created.ItineraryID != "itin-1" || created.Ticket == nil || created.Ticket.ID != "tk-1" {
		t.Fatalf("created state: %+v", created)
	}
	if err := db.Collection(store.CollItemStates).FindOne(ctx, bson.M{"_id": models.PairID("user-2", "stop-1")}).Decode(&kept); err != nil {
		t.Fatal(err)
	}
	if kept.Notes == nil || *kept.Notes != note || kept.TransitMode != "walk" || kept.Ticket == nil || *kept.Ticket.TotalCents != 2400 {
		t.Fatalf("existing state: %+v", kept)
	}
}

func TestCheckoutTicketIndex(t *testing.T) {
	s := testutil.Store(t, func() time.Time { return checkoutNow })
	cursor, err := s.DB().Collection(store.CollCheckoutIntents).Indexes().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var indexes []struct {
		Name   string `bson:"name"`
		Sparse bool   `bson:"sparse"`
	}
	if err := cursor.All(context.Background(), &indexes); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name == "ticketId" && index.Sparse {
			return
		}
	}
	t.Fatalf("no sparse ticketId index: %+v", indexes)
}
