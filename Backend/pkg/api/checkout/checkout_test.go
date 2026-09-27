package checkout_test

import (
	"Backend/pkg/api/checkout"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---- fixtures ---------------------------------------------------------------

// addPlan stores an itinerary with one bookable stop (and a walk leg before
// it) for the given members, the first being the host. priceCents nil means
// the price is unknown.
func addPlan(t *testing.T, srv *testutil.Server, members []string, title string, priceCents *int) (*models.Itinerary, string) {
	t.Helper()
	now := srv.Clock.Now().UTC()
	start := now.Add(24 * time.Hour).Truncate(time.Minute)
	lat, lng := 31.3660, -81.4283
	id := store.NewID()
	stopID := store.NewID()
	itin := &models.Itinerary{
		ID: id, HostID: members[0], MemberIDs: members, Title: "Evening out", DateKey: start.Format("2006-01-02"),
		TZ: testutil.TimeZone, Date: start.Truncate(24 * time.Hour), Start: start, BackBy: start.Add(3 * time.Hour),
		StartPlace: models.PlaceDoc{Name: "Seaside Market Square"}, EndPlace: models.PlaceDoc{Name: "Seaside Market Square"},
		Visibility: models.VisibilityJustMe, Status: models.ItineraryActive, CreatedAt: now, UpdatedAt: now,
		Items: []models.ItineraryItem{
			{ID: store.NewID(), Kind: models.ItemTransit, Title: "Walk to " + title, Start: start, End: start.Add(10 * time.Minute), LegMode: "walk", LegMinutes: 10},
			{ID: stopID, Kind: models.ItemStop, Title: title, Place: &models.PlaceDoc{Name: "Pier Nine", Lat: &lat, Lng: &lng},
				Start: start.Add(10 * time.Minute), End: start.Add(100 * time.Minute), Bookable: true, PriceCents: priceCents},
		},
	}
	if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(t.Context(), itin); err != nil {
		t.Fatal(err)
	}
	return itin, stopID
}

// addCard stores a saved card; later calls are younger.
func addCard(t *testing.T, srv *testutil.Server, userID, brand, last4 string, isDefault bool) *models.PaymentMethod {
	t.Helper()
	card := &models.PaymentMethod{
		ID: store.NewID(), UserID: userID, Brand: brand, Last4: last4, IsDefault: isDefault,
		DemoToken: "tok_" + strings.ToLower(brand) + "_" + last4, CreatedAt: time.Now().UTC(),
	}
	if _, err := srv.Store.Collection(store.CollPaymentMethods).InsertOne(t.Context(), card); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // distinct createdAt for ordering
	return card
}

// setInstant saves preferences with instant checkout on or off.
func setInstant(t *testing.T, srv *testutil.Server, userID string, on bool, limitCents int) {
	t.Helper()
	prefs := models.UserPrefs{
		Ratings: map[string]int{}, Company: "small_group", Pace: "balanced", Spend: "under_15",
		Flexibility: "bit_over_ok", SplitStyle: "equally", PreferFree: true, Answers: map[string]string{},
		InstantCheckout: on, InstantCheckoutLimitCents: limitCents,
	}
	if _, err := srv.Store.Users().Update(t.Context(), userID, bson.M{"prefs": prefs}); err != nil {
		t.Fatal(err)
	}
}

func create(t *testing.T, srv *testutil.Server, sess *testutil.Session, body contract.CreateCheckoutIntent) contract.CheckoutIntent {
	t.Helper()
	var intent contract.CheckoutIntent
	srv.Do(t, "POST", "/checkout/intents", body, sess).Expect(t, http.StatusCreated).JSON(t, &intent)
	return intent
}

func get(t *testing.T, srv *testutil.Server, sess *testutil.Session, id string) contract.CheckoutIntent {
	t.Helper()
	var intent contract.CheckoutIntent
	srv.Do(t, "GET", "/checkout/intents/"+id, nil, sess).Expect(t, http.StatusOK).JSON(t, &intent)
	return intent
}

// tick runs one agent pass and checks how many intents moved.
func tick(t *testing.T, agent *checkout.Agent, want int) {
	t.Helper()
	moved, err := agent.Tick(t.Context())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if moved != want {
		t.Fatalf("tick moved %d intents, want %d", moved, want)
	}
}

// statuses lists the checkout.status states sent to userID for one intent, in order.
func statuses(t *testing.T, srv *testutil.Server, userID, intentID string) []contract.CheckoutState {
	t.Helper()
	var out []contract.CheckoutState
	for _, e := range srv.Events.For(userID) {
		if e.Type != realtime.EventCheckoutStatus {
			continue
		}
		var data realtime.CheckoutStatusData
		testutil.EventData(t, e, &data)
		if data.IntentID == intentID {
			out = append(out, data.State)
		}
	}
	return out
}

func itemState(t *testing.T, srv *testutil.Server, userID, itemID string) *models.ItemState {
	t.Helper()
	var state models.ItemState
	err := srv.Store.Collection(store.CollItemStates).FindOne(t.Context(), bson.M{"_id": models.PairID(userID, itemID)}).Decode(&state)
	if err != nil {
		t.Fatalf("item state %s|%s: %v", userID, itemID, err)
	}
	return &state
}

func steps(texts ...string) []contract.CheckoutStep {
	out := make([]contract.CheckoutStep, 0, len(texts))
	for _, text := range texts {
		done := strings.HasPrefix(text, "+")
		out = append(out, contract.CheckoutStep{Text: strings.TrimPrefix(text, "+"), Done: done})
	}
	return out
}

var (
	preparingSteps = steps("Finding tickets on the official site", "Filling in your name and email", "Waiting for your approval")
	quotedSteps    = steps("+Found tickets on the official site", "+Filled in your name and email", "Waiting for your approval")
	instantSteps   = steps("+Found tickets on the official site", "+Filled in your name and email", "Paying instantly (within your limit)")
	confirmation   = regexp.MustCompile(`^SQ-[0-9A-HJKMNP-TV-Z]{5}$`)
)

func expectMessage(t *testing.T, res *testutil.Response, status int, message string) {
	t.Helper()
	res.Expect(t, status)
	if res.Message() != message {
		t.Fatalf("message = %q, want %q", res.Message(), message)
	}
}

// ---- flows -------------------------------------------------------------------

func TestCreateWithoutCardFails(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))

	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	if intent.State != contract.CheckoutFailed || intent.FailureReason == nil || *intent.FailureReason != checkout.MsgNoCard {
		t.Fatalf("no card must fail with %q: %+v", checkout.MsgNoCard, intent)
	}
	if intent.CardLast4 != "" || intent.CardBrand != "" || intent.PaymentMethodID != nil || len(intent.Steps) != 0 {
		t.Fatalf("a failed intent has no card and no steps: %+v", intent)
	}
	if intent.ItemTitle != "Sunset Jazz on Pier Nine" || intent.SubtotalCents == nil || *intent.SubtotalCents != 1200 {
		t.Fatalf("title/subtotal: %+v", intent)
	}
	if got := statuses(t, srv, a.UserID, intent.ID); !slices.Equal(got, []contract.CheckoutState{contract.CheckoutFailed}) {
		t.Fatalf("events = %v", got)
	}
	tick(t, checkout.NewAgent(srv.Deps), 0)
	// Nothing to cancel; it stays failed with its reason.
	srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/cancel", nil, a).Expect(t, http.StatusNoContent)
	if got := get(t, srv, a, intent.ID); got.State != contract.CheckoutFailed || got.FailureReason == nil {
		t.Fatalf("cancel changed a failed intent: %+v", got)
	}
}

func TestApproveFlowBooksTicket(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	card := addCard(t, srv, a.UserID, "Visa", "4242", true)
	itin, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))
	// A note saved earlier must survive the booking.
	note := "bring a blanket"
	_, err := srv.Store.Collection(store.CollItemStates).InsertOne(t.Context(), models.ItemState{
		ID: models.PairID(a.UserID, itemID), UserID: a.UserID, ItemID: itemID, ItineraryID: itin.ID,
		Notes: &note, NotesScope: "private", TransitMode: "walk", UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := checkout.NewAgent(srv.Deps)

	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 2})
	if intent.State != contract.CheckoutPreparing || intent.Instant || !slices.Equal(intent.Steps, preparingSteps) {
		t.Fatalf("created: %+v", intent)
	}
	if intent.SubtotalCents == nil || *intent.SubtotalCents != 2400 || intent.FeesCents != nil || intent.TotalCents != nil {
		t.Fatalf("while preparing only the subtotal is known: %+v", intent)
	}
	if intent.PaymentMethodID == nil || *intent.PaymentMethodID != card.ID || intent.CardBrand != "Visa" || intent.CardLast4 != "4242" || intent.Quantity != 2 {
		t.Fatalf("card/quantity: %+v", intent)
	}
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a), http.StatusBadRequest, checkout.MsgPreparing)

	tick(t, agent, 1)
	quoted := get(t, srv, a, intent.ID)
	if quoted.State != contract.CheckoutAwaitingApproval || !slices.Equal(quoted.Steps, quotedSteps) {
		t.Fatalf("after the first step: %+v", quoted)
	}
	if quoted.FeesCents == nil || *quoted.FeesCents != 0 || quoted.TotalCents == nil || *quoted.TotalCents != 2400 {
		t.Fatalf("quote: %+v", quoted)
	}
	tick(t, agent, 0) // awaiting approval waits for the user

	var approved contract.CheckoutIntent
	srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a).Expect(t, http.StatusOK).JSON(t, &approved)
	if approved.State != contract.CheckoutProcessing || approved.Instant {
		t.Fatalf("approve: %+v", approved)
	}
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a), http.StatusBadRequest, checkout.MsgFinished)
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/cancel", nil, a), http.StatusConflict, checkout.MsgWentThrough)
	expectMessage(t, srv.Do(t, "PATCH", "/checkout/intents/"+intent.ID, contract.CheckoutPatch{PaymentMethodID: &card.ID}, a),
		http.StatusConflict, checkout.MsgWentThrough)

	tick(t, agent, 1)
	booked := get(t, srv, a, intent.ID)
	if booked.State != contract.CheckoutBooked || !slices.Equal(booked.Steps, steps("+Found tickets on the official site", "+Filled in your name and email", "+Waiting for your approval")) {
		t.Fatalf("booked: %+v", booked)
	}
	state := itemState(t, srv, a.UserID, itemID)
	ticket := state.Ticket
	if ticket == nil || len(ticket.ID) < 20 || ticket.Quantity != 2 || ticket.TotalCents == nil || *ticket.TotalCents != 2400 {
		t.Fatalf("ticket on the item: %+v", ticket)
	}
	if ticket.Confirmation == nil || !confirmation.MatchString(*ticket.Confirmation) {
		t.Fatalf("confirmation: %v", ticket.Confirmation)
	}
	if ticket.URL == nil || *ticket.URL != "http://api.test/tickets/"+ticket.ID {
		t.Fatalf("ticket url: %v", ticket.URL)
	}
	if state.Notes == nil || *state.Notes != note || state.TransitMode != "walk" || state.ItineraryID != itin.ID {
		t.Fatalf("the booking clobbered the item state: %+v", state)
	}
	want := []contract.CheckoutState{contract.CheckoutPreparing, contract.CheckoutAwaitingApproval, contract.CheckoutProcessing, contract.CheckoutBooked}
	if got := statuses(t, srv, a.UserID, intent.ID); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a), http.StatusBadRequest, checkout.MsgFinished)
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/cancel", nil, a), http.StatusConflict, checkout.MsgWentThrough)
	tick(t, agent, 0)

	// The ticket page is public and says what it is.
	page := srv.Do(t, "GET", "/tickets/"+ticket.ID, nil, nil).Expect(t, http.StatusOK)
	body := string(page.Body)
	for _, want := range []string{"Sunset Jazz on Pier Nine", *ticket.Confirmation, "Demo ticket", "$24.00", "Visa •••• 4242", "Pier Nine", "nothing was bought or charged"} {
		if !strings.Contains(body, want) {
			t.Errorf("ticket page lacks %q:\n%s", want, body)
		}
	}
	if ct := page.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	if page.Header.Get("Cache-Control") != "no-store" || page.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("ticket page headers: %v", page.Header)
	}
	missing := srv.Do(t, "GET", "/tickets/not-a-ticket", nil, nil).Expect(t, http.StatusNotFound)
	if !strings.Contains(string(missing.Body), "Ticket not found") {
		t.Errorf("404 page: %s", missing.Body)
	}
}

func TestInstantCheckout(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	setInstant(t, srv, a.UserID, true, 3000)
	_, priced := addPlan(t, srv, []string{a.UserID}, "Karaoke Night", testutil.Ptr(1200))
	_, unpriced := addPlan(t, srv, []string{a.UserID}, "Mystery Show", nil)
	agent := checkout.NewAgent(srv.Deps)

	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: priced, Quantity: 2, Instant: true})
	if intent.State != contract.CheckoutProcessing || !intent.Instant || !slices.Equal(intent.Steps, instantSteps) {
		t.Fatalf("instant within the limit: %+v", intent)
	}
	if intent.TotalCents == nil || *intent.TotalCents != 2400 || intent.FeesCents == nil || *intent.FeesCents != 0 {
		t.Fatalf("instant quote: %+v", intent)
	}
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/cancel", nil, a), http.StatusConflict, checkout.MsgWentThrough)
	tick(t, agent, 1)
	if got := get(t, srv, a, intent.ID); got.State != contract.CheckoutBooked || !got.Instant {
		t.Fatalf("instant booked: %+v", got)
	}
	if ticket := itemState(t, srv, a.UserID, priced).Ticket; ticket == nil || ticket.Quantity != 2 || *ticket.TotalCents != 2400 {
		t.Fatalf("instant ticket: %+v", ticket)
	}
	want := []contract.CheckoutState{contract.CheckoutProcessing, contract.CheckoutBooked}
	if got := statuses(t, srv, a.UserID, intent.ID); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}

	// Over the limit, an unknown price or instant not asked for: the agent asks first.
	for name, body := range map[string]contract.CreateCheckoutIntent{
		"over the limit": {ItemID: priced, Quantity: 3, Instant: true},
		"unknown price":  {ItemID: unpriced, Quantity: 1, Instant: true},
		"not asked for":  {ItemID: priced, Quantity: 2},
	} {
		if got := create(t, srv, a, body); got.State != contract.CheckoutPreparing || got.Instant {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// Asked for, but switched off in preferences.
	setInstant(t, srv, a.UserID, false, 3000)
	if got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: priced, Quantity: 1, Instant: true}); got.State != contract.CheckoutPreparing {
		t.Fatalf("instant off in preferences: %+v", got)
	}
	// Exactly at the limit counts as within it.
	setInstant(t, srv, a.UserID, true, 2400)
	if got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: priced, Quantity: 2, Instant: true}); got.State != contract.CheckoutProcessing {
		t.Fatalf("at the limit: %+v", got)
	}
}

func TestUnknownPriceQuoteStaysUnknown(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Mystery Show", nil)
	agent := checkout.NewAgent(srv.Deps)
	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	tick(t, agent, 1)
	quoted := get(t, srv, a, intent.ID)
	if quoted.State != contract.CheckoutAwaitingApproval || quoted.SubtotalCents != nil || quoted.FeesCents != nil || quoted.TotalCents != nil {
		t.Fatalf("unknown price must stay unknown: %+v", quoted)
	}
	srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a).Expect(t, http.StatusOK)
	tick(t, agent, 1)
	if ticket := itemState(t, srv, a.UserID, itemID).Ticket; ticket == nil || ticket.TotalCents != nil {
		t.Fatalf("ticket without a price: %+v", ticket)
	}
}

func TestCancelAndChangeCard(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	b := srv.Signup(t, "Theo Park")
	visa := addCard(t, srv, a.UserID, "Visa", "4242", true)
	master := addCard(t, srv, a.UserID, "Mastercard", "5454", false)
	theirs := addCard(t, srv, b.UserID, "Visa", "1881", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))
	agent := checkout.NewAgent(srv.Deps)

	// Cancel while preparing: the agent never touches it again.
	first := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	srv.Do(t, "POST", "/checkout/intents/"+first.ID+"/cancel", nil, a).Expect(t, http.StatusNoContent)
	if got := get(t, srv, a, first.ID); got.State != contract.CheckoutCancelled {
		t.Fatalf("cancelled: %+v", got)
	}
	tick(t, agent, 0)
	srv.Do(t, "POST", "/checkout/intents/"+first.ID+"/cancel", nil, a).Expect(t, http.StatusNoContent)
	if got := statuses(t, srv, a.UserID, first.ID); !slices.Equal(got, []contract.CheckoutState{contract.CheckoutPreparing, contract.CheckoutCancelled}) {
		t.Fatalf("events = %v (a second cancel must not emit)", got)
	}

	// Change the card while awaiting approval.
	second := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	var switched contract.CheckoutIntent
	srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{PaymentMethodID: &master.ID}, a).Expect(t, http.StatusOK).JSON(t, &switched)
	if switched.State != contract.CheckoutPreparing || switched.CardBrand != "Mastercard" || switched.CardLast4 != "5454" || *switched.PaymentMethodID != master.ID {
		t.Fatalf("switch while preparing: %+v", switched)
	}
	tick(t, agent, 1)
	srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{PaymentMethodID: &visa.ID}, a).Expect(t, http.StatusOK).JSON(t, &switched)
	if switched.State != contract.CheckoutAwaitingApproval || switched.CardLast4 != "4242" || *switched.PaymentMethodID != visa.ID {
		t.Fatalf("switch while awaiting approval: %+v", switched)
	}
	unknown := "pm-nope"
	srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{PaymentMethodID: &unknown}, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{PaymentMethodID: &theirs.ID}, a).Expect(t, http.StatusNotFound)
	expectMessage(t, srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{}, a), http.StatusBadRequest, checkout.MsgPickCard)
	if got := get(t, srv, a, second.ID); got.CardLast4 != "4242" {
		t.Fatalf("a refused switch changed the card: %+v", got)
	}
	srv.Do(t, "POST", "/checkout/intents/"+second.ID+"/cancel", nil, a).Expect(t, http.StatusNoContent)
	expectMessage(t, srv.Do(t, "PATCH", "/checkout/intents/"+second.ID, contract.CheckoutPatch{PaymentMethodID: &master.ID}, a),
		http.StatusBadRequest, checkout.MsgFinished)
	expectMessage(t, srv.Do(t, "POST", "/checkout/intents/"+second.ID+"/approve", nil, a), http.StatusBadRequest, checkout.MsgFinished)
}

func TestCardChoice(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	older := addCard(t, srv, a.UserID, "Mastercard", "5454", false)
	def := addCard(t, srv, a.UserID, "Visa", "4242", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))

	for name, tc := range map[string]struct {
		requested *string
		want      string
	}{
		"null is the default": {nil, def.ID},
		"the one asked for":   {&older.ID, older.ID},
		"unknown → default":   {testutil.Ptr("pm-gone"), def.ID},
	} {
		got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1, PaymentMethodID: tc.requested})
		if got.PaymentMethodID == nil || *got.PaymentMethodID != tc.want {
			t.Errorf("%s: paid with %v, want %s", name, got.PaymentMethodID, tc.want)
		}
	}
	// Without a default, the oldest card.
	if _, err := srv.Store.Collection(store.CollPaymentMethods).UpdateMany(t.Context(), bson.M{"userId": a.UserID}, bson.M{"$set": bson.M{"isDefault": false}}); err != nil {
		t.Fatal(err)
	}
	if got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1}); *got.PaymentMethodID != older.ID {
		t.Fatalf("no default: paid with %s, want the oldest %s", *got.PaymentMethodID, older.ID)
	}
}

func TestValidationAndScoping(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	b := srv.Signup(t, "Theo Park")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	addCard(t, srv, b.UserID, "Visa", "1881", true)
	itin, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))

	expectMessage(t, srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{Quantity: 1}, a), http.StatusBadRequest, checkout.MsgNoItem)
	for _, q := range []int{-1, 11} {
		expectMessage(t, srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: itemID, Quantity: q}, a),
			http.StatusBadRequest, checkout.MsgQuantity)
	}
	if got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID}); got.Quantity != 1 || *got.SubtotalCents != 1200 {
		t.Fatalf("absent quantity is one ticket: %+v", got)
	}
	if got := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 10}); got.Quantity != 10 || *got.SubtotalCents != 12000 {
		t.Fatalf("ten tickets: %+v", got)
	}
	srv.DoRaw(t, "POST", "/checkout/intents", strings.NewReader("{"), map[string]string{"Authorization": a.Bearer()}).Expect(t, http.StatusBadRequest)
	srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1}, nil).Expect(t, http.StatusUnauthorized)
	srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: "no-such-item", Quantity: 1}, a).Expect(t, http.StatusNotFound)

	// B can neither buy A's item nor see or touch A's intent.
	srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1}, b).Expect(t, http.StatusNotFound)
	mine := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	cardB := "any"
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/checkout/intents/" + mine.ID, nil},
		{"PATCH", "/checkout/intents/" + mine.ID, contract.CheckoutPatch{PaymentMethodID: &cardB}},
		{"POST", "/checkout/intents/" + mine.ID + "/approve", nil},
		{"POST", "/checkout/intents/" + mine.ID + "/cancel", nil},
		{"GET", "/checkout/intents/no-such-intent", nil},
	} {
		if res := srv.Do(t, req.method, req.path, req.body, b); res.Status != http.StatusNotFound {
			t.Errorf("B %s %s = %d, want 404: %s", req.method, req.path, res.Status, res.Body)
		}
	}
	if got := get(t, srv, a, mine.ID); got.State != contract.CheckoutPreparing {
		t.Fatalf("B's calls changed A's intent: %+v", got)
	}
	if n := srv.EventCount(b.UserID, realtime.EventCheckoutStatus); n != 0 {
		t.Fatalf("B received %d of A's checkout events", n)
	}

	// A member of a group plan can buy for its items; a deleted plan's items are gone.
	_, groupItem := addPlan(t, srv, []string{a.UserID, b.UserID}, "Karaoke Night", testutil.Ptr(800))
	if got := create(t, srv, b, contract.CreateCheckoutIntent{ItemID: groupItem, Quantity: 1}); got.CardLast4 != "1881" {
		t.Fatalf("member checkout: %+v", got)
	}
	if _, err := srv.Store.Collection(store.CollItineraries).UpdateOne(t.Context(), bson.M{"_id": itin.ID}, bson.M{"$set": bson.M{"status": models.ItineraryDeleted}}); err != nil {
		t.Fatal(err)
	}
	srv.Do(t, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1}, a).Expect(t, http.StatusNotFound)
}

func TestStepDelay(t *testing.T) {
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) { c.CheckoutStepDelay = 1500 * time.Millisecond }))
	a := srv.Signup(t, "Sandy Byte")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))
	agent := checkout.NewAgent(srv.Deps)

	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	tick(t, agent, 0)
	srv.Clock.Advance(1499 * time.Millisecond)
	tick(t, agent, 0)
	srv.Clock.Advance(time.Millisecond)
	tick(t, agent, 1)
	srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a).Expect(t, http.StatusOK)
	tick(t, agent, 0)
	srv.Clock.Advance(1500 * time.Millisecond)
	tick(t, agent, 1)
	if got := get(t, srv, a, intent.ID); got.State != contract.CheckoutBooked {
		t.Fatalf("after two delays: %+v", got)
	}
}

// TestAgentRuns drives the real goroutine: the intent moves on its own and
// the owner hears about each step.
func TestAgentRuns(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	checkout.StartAgent(ctx, srv.Deps)

	intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
	waitState(t, srv, a, intent.ID, contract.CheckoutAwaitingApproval)
	srv.Do(t, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, a).Expect(t, http.StatusOK)
	waitState(t, srv, a, intent.ID, contract.CheckoutBooked)
	want := []contract.CheckoutState{contract.CheckoutPreparing, contract.CheckoutAwaitingApproval, contract.CheckoutProcessing, contract.CheckoutBooked}
	if got := statuses(t, srv, a.UserID, intent.ID); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func waitState(t *testing.T, srv *testutil.Server, sess *testutil.Session, id string, state contract.CheckoutState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := get(t, srv, sess, id)
		if got.State == state {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("intent %s stuck in %s, want %s", id, got.State, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestApproveRacesCancel: whichever lands first wins, the other is refused,
// and the intent moves exactly once.
func TestApproveRacesCancel(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Sandy Byte")
	addCard(t, srv, a.UserID, "Visa", "4242", true)
	_, itemID := addPlan(t, srv, []string{a.UserID}, "Sunset Jazz on Pier Nine", testutil.Ptr(1200))
	agent := checkout.NewAgent(srv.Deps)
	// post runs off the test goroutine, so it reports failures as a status.
	post := func(path string) int {
		req, err := http.NewRequest("POST", srv.URL(path), nil)
		if err != nil {
			return -1
		}
		req.Header.Set("Authorization", a.Bearer())
		req.Header.Set("X-Time-Zone", testutil.TimeZone)
		res, err := srv.HTTP.Client().Do(req)
		if err != nil {
			return -1
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	for range 6 {
		intent := create(t, srv, a, contract.CreateCheckoutIntent{ItemID: itemID, Quantity: 1})
		tick(t, agent, 1)
		var wg sync.WaitGroup
		var approve, cancel int
		wg.Add(2)
		go func() { defer wg.Done(); approve = post("/checkout/intents/" + intent.ID + "/approve") }()
		go func() { defer wg.Done(); cancel = post("/checkout/intents/" + intent.ID + "/cancel") }()
		wg.Wait()
		final := get(t, srv, a, intent.ID)
		switch {
		case approve == http.StatusOK && cancel == http.StatusConflict:
			if final.State != contract.CheckoutProcessing {
				t.Fatalf("approve won but the intent is %s", final.State)
			}
			tick(t, agent, 1) // book it before the next round
		case cancel == http.StatusNoContent && approve == http.StatusBadRequest:
			if final.State != contract.CheckoutCancelled {
				t.Fatalf("cancel won but the intent is %s", final.State)
			}
		default:
			t.Fatalf("approve %d / cancel %d: both or neither won", approve, cancel)
		}
		got := statuses(t, srv, a.UserID, intent.ID)
		if len(got) < 3 || got[2] != final.State {
			t.Fatalf("events = %v, want preparing, awaiting_approval and one winner (%s)", got, final.State)
		}
	}
}
