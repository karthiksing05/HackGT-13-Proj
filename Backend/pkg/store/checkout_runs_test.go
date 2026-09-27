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

	"go.mongodb.org/mongo-driver/v2/bson"
)

func newRun(budget int) (*models.CheckoutRun, []*models.CheckoutIntent) {
	run := &models.CheckoutRun{UserID: "user-1", ItineraryID: "itin-1", State: models.RunRunning, BudgetCents: budget, Currency: "usd"}
	intents := []*models.CheckoutIntent{
		{UserID: "user-1", ItemID: "item-a", ItineraryID: "itin-1", State: models.CheckoutProcessing, Quantity: 1, Steps: []models.CheckoutStep{}},
		{UserID: "user-1", ItemID: "item-b", ItineraryID: "itin-1", State: models.CheckoutProcessing, Quantity: 2, Steps: []models.CheckoutStep{}},
	}
	return run, intents
}

func TestCheckoutRunBudgetIsNeverExceeded(t *testing.T) {
	st := testutil.Store(t, nil)
	ctx := context.Background()
	runs := st.CheckoutRuns()
	run, intents := newRun(5000)
	if err := runs.Create(ctx, run, intents); err != nil {
		t.Fatal(err)
	}
	// A second run for the same plan while this one runs is refused.
	again, againIntents := newRun(1000)
	if err := runs.Create(ctx, again, againIntents); !errors.Is(err, store.ErrRunActive) {
		t.Fatalf("second active run: %v", err)
	}

	// Twenty concurrent $10 reservations against a $50 budget: exactly five win.
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := runs.Reserve(ctx, run.ID, 1000)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 5 {
		t.Fatalf("%d reservations won, want 5", won)
	}
	// Committing one at a lower final amount frees the difference.
	if err := runs.Commit(ctx, run.ID, 1000, 800); err != nil {
		t.Fatal(err)
	}
	if ok, _ := runs.Reserve(ctx, run.ID, 200); !ok {
		t.Fatal("the $2 freed by the commit could not be reserved")
	}
	if ok, _ := runs.Reserve(ctx, run.ID, 1); ok {
		t.Fatal("reserved past the budget")
	}
	got, _ := runs.ByID(ctx, run.ID)
	if got.SpentCents != 800 || got.ReservedCents != 4200 || len(got.IntentIDs) != 2 {
		t.Fatalf("accounting: %+v", got)
	}

	// A cancelled run takes no more reservations.
	if err := runs.Release(ctx, run.ID, 4200); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Cancel(ctx, "user-1", run.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := runs.Reserve(ctx, run.ID, 100); ok {
		t.Fatal("a cancelled run reserved budget")
	}
}

func TestCheckoutRunLeaseAndIntents(t *testing.T) {
	st := testutil.Store(t, nil)
	ctx := context.Background()
	runs, intents := st.CheckoutRuns(), st.CheckoutIntents()
	run, list := newRun(5000)
	if err := runs.Create(ctx, run, list); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if stale, _ := runs.Stale(ctx, now); len(stale) != 1 || stale[0] != run.ID {
		t.Fatalf("a new run is resumable: %v", stale)
	}
	if _, ok, _ := runs.Claim(ctx, run.ID, now, now.Add(time.Minute)); !ok {
		t.Fatal("first claim failed")
	}
	if _, ok, _ := runs.Claim(ctx, run.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("a held lease was claimed again")
	}
	if stale, _ := runs.Stale(ctx, now); len(stale) != 0 {
		t.Fatalf("a leased run is stale: %v", stale)
	}
	// After the lease lapses a new runner takes over with nothing reserved.
	_, _ = runs.Reserve(ctx, run.ID, 700)
	later := now.Add(2 * time.Minute)
	claimed, ok, _ := runs.Claim(ctx, run.ID, later, later.Add(time.Minute))
	if !ok || claimed.ReservedCents != 0 {
		t.Fatalf("takeover: %v %+v", ok, claimed)
	}

	got, err := intents.ByRun(ctx, run)
	if err != nil || len(got) != 2 || got[0].ItemID != "item-a" || got[1].ItemID != "item-b" || got[0].RunID != run.ID {
		t.Fatalf("ByRun: %v %+v", err, got)
	}
	a, ok, err := intents.RunUpdate(ctx, got[0].ID, []string{models.CheckoutProcessing},
		bson.M{"state": models.CheckoutBooked, "orderRef": "SL-1"}, models.CheckoutStep{Text: "Booked", Done: true})
	if err != nil || !ok || a.State != models.CheckoutBooked || len(a.Steps) != 1 {
		t.Fatalf("RunUpdate: %v %v %+v", err, ok, a)
	}
	if _, ok, _ := intents.RunUpdate(ctx, got[0].ID, []string{models.CheckoutProcessing}, bson.M{"state": models.CheckoutFailed}); ok {
		t.Fatal("a booked intent was failed")
	}
	if err := intents.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = intents.ByRun(ctx, run)
	if got[0].State != models.CheckoutBooked || got[1].State != models.CheckoutCancelled {
		t.Fatalf("CancelRun: %s %s", got[0].State, got[1].State)
	}
	fin, err := runs.Finish(ctx, run.ID, "1 booked", "")
	if err != nil || fin.State != models.RunDone || fin.Summary != "1 booked" || fin.FinishedAt == nil || fin.LeaseUntil != nil {
		t.Fatalf("Finish: %v %+v", err, fin)
	}
}
