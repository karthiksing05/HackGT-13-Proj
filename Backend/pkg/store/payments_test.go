package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPaymentsDefaultAndPromotion(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	payments := s.Payments()

	visa, err := payments.Add(ctx, "user-a", "Visa", "4242", "tok_visa_4242")
	if err != nil || !visa.IsDefault || visa.ID == "" || !visa.CreatedAt.Equal(testNow) || visa.DemoToken != "tok_visa_4242" {
		t.Fatalf("first card: %v %+v", err, visa)
	}
	mc, err := payments.Add(ctx, "user-a", "Mastercard", "5454", "tok_mastercard_5454")
	if err != nil || mc.IsDefault {
		t.Fatalf("second card must not be default: %v %+v", err, mc)
	}
	third, _ := payments.Add(ctx, "user-a", "Visa", "1881", "tok_visa_1881")
	other, _ := payments.Add(ctx, "user-b", "Amex", "0005", "tok_amex_0005")
	if !other.IsDefault {
		t.Fatal("each account has its own default")
	}

	list, err := payments.List(ctx, "user-a")
	if err != nil || len(list) != 3 || list[0].ID != visa.ID || list[1].ID != mc.ID || list[2].ID != third.ID {
		t.Fatalf("List order: %v %+v", err, list)
	}
	if n, err := payments.Count(ctx, "user-a"); err != nil || n != 3 {
		t.Fatalf("Count: %d %v", n, err)
	}

	// Another account's card is not found and survives.
	if err := payments.Delete(ctx, "user-b", visa.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-account delete: %v", err)
	}
	// Deleting a spare leaves the default alone.
	if err := payments.Delete(ctx, "user-a", third.ID); err != nil {
		t.Fatal(err)
	}
	// Deleting the default promotes the oldest card left.
	if err := payments.Delete(ctx, "user-a", visa.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = payments.List(ctx, "user-a")
	if len(list) != 1 || list[0].ID != mc.ID || !list[0].IsDefault {
		t.Fatalf("promotion: %+v", list)
	}
	if err := payments.Delete(ctx, "user-a", mc.ID); err != nil {
		t.Fatalf("deleting the last card: %v", err)
	}
	if list, _ := payments.List(ctx, "user-a"); len(list) != 0 {
		t.Fatalf("cards left: %+v", list)
	}
	if again, _ := payments.Add(ctx, "user-a", "Visa", "4242", "tok_visa_4242"); !again.IsDefault {
		t.Fatal("a card added to an empty account is the default")
	}

	// The index refuses a second default for one account.
	dup := models.PaymentMethod{ID: store.NewID(), UserID: "user-b", Brand: "Visa", Last4: "4242", IsDefault: true, CreatedAt: testNow}
	if _, err := s.Collection(store.CollPaymentMethods).InsertOne(ctx, dup); !store.IsDuplicate(err) {
		t.Fatalf("second default accepted: %v", err)
	}
}

func TestPaymentsConcurrentFirstCardsKeepOneDefault(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	payments := s.Payments()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := payments.Add(ctx, "user-race", "Visa", "4242", "tok_visa_4242")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent add: %v", err)
		}
	}
	list, err := payments.List(ctx, "user-race")
	if err != nil || len(list) != 6 {
		t.Fatalf("List: %v %d", err, len(list))
	}
	defaults := 0
	for _, card := range list {
		if card.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("%d defaults after concurrent adds, want 1", defaults)
	}
}

func TestPaymentsPromoteOldestByCreation(t *testing.T) {
	now := testNow
	s := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	payments := s.Payments()
	first, _ := payments.Add(ctx, "u", "Visa", "4242", "tok_visa_4242")
	now = now.Add(time.Minute)
	second, _ := payments.Add(ctx, "u", "Mastercard", "5454", "tok_mastercard_5454")
	now = now.Add(time.Minute)
	if _, err := payments.Add(ctx, "u", "Visa", "1881", "tok_visa_1881"); err != nil {
		t.Fatal(err)
	}
	if err := payments.Delete(ctx, "u", first.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := payments.List(ctx, "u")
	if len(list) != 2 || list[0].ID != second.ID || !list[0].IsDefault || list[1].IsDefault {
		t.Fatalf("the next oldest card must be promoted: %+v", list)
	}
}
