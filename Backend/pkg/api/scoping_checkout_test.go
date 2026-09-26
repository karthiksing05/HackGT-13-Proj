package api_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"net/http"
	"testing"
	"time"
)

// backend-D's rows of the scoping table, added from here so the shared
// table in scoping_test.go needs no edit.
func init() {
	scopes = append(scopes,
		scope{"GET /checkout/intents/{id} of A", checkoutIntentForA("GET", ""), http.StatusNotFound},
		scope{"POST /checkout/intents/{id}/approve of A", checkoutIntentForA("POST", "/approve"), http.StatusNotFound},
		scope{"POST /checkout/intents/{id}/cancel of A", checkoutIntentForA("POST", "/cancel"), http.StatusNotFound},
	)
}

// checkoutIntentForA gives A a plan with a priced stop and a card, starts a
// checkout for it and points B's request at A's intent.
func checkoutIntentForA(method, suffix string) func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
	return func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
		t.Helper()
		now := srv.Clock.Now().UTC()
		price := 1500
		itemID := store.NewID()
		itin := models.Itinerary{
			ID: store.NewID(), HostID: a.UserID, MemberIDs: []string{a.UserID}, Title: "Scoping plan",
			Start: now.Add(time.Hour), BackBy: now.Add(3 * time.Hour), Visibility: models.VisibilityJustMe, Status: models.ItineraryActive,
			Items: []models.ItineraryItem{{ID: itemID, Kind: models.ItemStop, Title: "Show", Start: now.Add(time.Hour),
				End: now.Add(2 * time.Hour), Bookable: true, PriceCents: &price}},
			CreatedAt: now, UpdatedAt: now,
		}
		card := models.PaymentMethod{ID: store.NewID(), UserID: a.UserID, Brand: "Visa", Last4: "4242", IsDefault: true, CreatedAt: now}
		if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(t.Context(), itin); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.Store.Collection(store.CollPaymentMethods).InsertOne(t.Context(), card); err != nil {
			t.Fatal(err)
		}
		var intent contract.CheckoutIntent
		srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1}, a).
			Expect(t, http.StatusCreated).JSON(t, &intent)
		return method, "/checkout/intents/" + intent.ID + suffix, nil
	}
}
