package checkout

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Sentences the app shows verbatim.
const (
	MsgNoCard      = "Add a card in Account first."
	MsgNoItem      = "Pick something to get tickets for."
	MsgQuantity    = "You can get 1 to 10 tickets at a time."
	MsgPickCard    = "Pick a card to pay with."
	MsgFinished    = "This checkout already finished."
	MsgWentThrough = "This checkout already went through."
	MsgPreparing   = "The agent is still getting the quote. Try again in a moment."
)

// defaultItemTitle names an item that has no title of its own.
const defaultItemTitle = "Tickets"

// Create is POST /checkout/intents (CreateCheckoutIntent) → 201
// CheckoutIntent: preparing, processing (instant checkout within the
// user's limit) or failed (no card to pay with).
func (h *H) Create(w http.ResponseWriter, r *http.Request) {
	var req contract.CreateCheckoutIntent
	if !httpx.Decode(w, r, &req) {
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	quantity := req.Quantity
	if quantity == 0 {
		quantity = 1 // absent: one ticket, as the app's model defaults
	}
	switch {
	case itemID == "":
		httpx.Error(w, http.StatusBadRequest, MsgNoItem)
		return
	case quantity < 1 || quantity > maxQuantity:
		httpx.Error(w, http.StatusBadRequest, MsgQuantity)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	reads := h.d.Store.CheckoutReads()
	itin, item, err := reads.MemberItem(ctx, user.ID.Hex(), itemID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	cards, err := reads.Cards(ctx, user.ID.Hex())
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	intent := h.newIntent(user, itin, item, quantity, pickCard(cards, req.PaymentMethodID), req.Instant)
	if err := h.d.Store.CheckoutIntents().Insert(ctx, intent); err != nil {
		api.Fail(w, r, err)
		return
	}
	publish(h.d, intent)
	httpx.JSON(w, http.StatusCreated, render(intent))
}

// newIntent starts a checkout for item. Without a card it has failed
// already; with instant checkout asked for, switched on in the user's
// preferences and a known subtotal within their limit, the agent pays
// straight away; otherwise it prepares a quote for approval.
func (h *H) newIntent(user *models.User, itin *models.Itinerary, item *models.ItineraryItem, quantity int, card *models.PaymentMethod, instant bool) *models.CheckoutIntent {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = defaultItemTitle
	}
	intent := &models.CheckoutIntent{
		ID:          store.NewID(),
		UserID:      user.ID.Hex(),
		ItemID:      item.ID,
		ItineraryID: itin.ID,
		ItemTitle:   title,
		Quantity:    quantity,
		Steps:       []models.CheckoutStep{},
	}
	if item.PriceCents != nil {
		subtotal := *item.PriceCents * quantity
		intent.SubtotalCents = &subtotal
	}
	if card == nil {
		reason := MsgNoCard
		intent.State, intent.FailureReason = models.CheckoutFailed, &reason
		return intent
	}
	cardID := card.ID
	intent.PaymentMethodID, intent.CardBrand, intent.CardLast4 = &cardID, card.Brand, card.Last4
	due := h.due()
	intent.NextTransitionAt = &due
	prefs := user.Prefs
	if instant && prefs.InstantCheckout && intent.SubtotalCents != nil && *intent.SubtotalCents <= prefs.InstantCheckoutLimitCents {
		intent.State, intent.Instant, intent.Steps = models.CheckoutProcessing, true, instantSteps()
		quote(intent)
		intent.TicketID, intent.Confirmation = newTicketID(), newConfirmation()
		return intent
	}
	intent.State, intent.Steps = models.CheckoutPreparing, preparingSteps()
	return intent
}

// due is when a step started now finishes (CHECKOUT_STEP_DELAY; 0 in tests).
func (h *H) due() time.Time {
	return h.d.Clock().Add(h.d.Cfg.CheckoutStepDelay).UTC().Truncate(time.Millisecond)
}

// Get is GET /checkout/intents/{id} → CheckoutIntent (the owner's only).
func (h *H) Get(w http.ResponseWriter, r *http.Request) {
	intent, err := h.d.Store.CheckoutIntents().Get(r.Context(), api.UserID(r), mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, render(intent))
}

// Patch is PATCH /checkout/intents/{id} ({payment_method_id}, Checkout ›
// "Change") → CheckoutIntent, until the agent starts paying.
func (h *H) Patch(w http.ResponseWriter, r *http.Request) {
	var req contract.CheckoutPatch
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.PaymentMethodID == nil || strings.TrimSpace(*req.PaymentMethodID) == "" {
		httpx.Error(w, http.StatusBadRequest, MsgPickCard)
		return
	}
	ctx := r.Context()
	userID, id := api.UserID(r), mux.Vars(r)["id"]
	if _, err := h.d.Store.CheckoutIntents().Get(ctx, userID, id); err != nil {
		api.Fail(w, r, err)
		return
	}
	card, err := h.d.Store.CheckoutReads().Card(ctx, userID, strings.TrimSpace(*req.PaymentMethodID))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	intent, err := h.d.Store.CheckoutIntents().SetCard(ctx, userID, id, card)
	var conflict *store.CheckoutStateError
	if errors.As(err, &conflict) {
		if paying(conflict.State) {
			httpx.Error(w, http.StatusConflict, MsgWentThrough)
		} else {
			httpx.Error(w, http.StatusBadRequest, MsgFinished)
		}
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, render(intent))
}

// Approve is POST /checkout/intents/{id}/approve → CheckoutIntent in
// processing; the agent books it after the step delay. The only call that
// "spends" (Simulated: nothing is charged).
func (h *H) Approve(w http.ResponseWriter, r *http.Request) {
	intent, err := h.d.Store.CheckoutIntents().Approve(r.Context(), api.UserID(r), mux.Vars(r)["id"],
		newTicketID(), newConfirmation(), h.due())
	var conflict *store.CheckoutStateError
	if errors.As(err, &conflict) {
		if conflict.State == models.CheckoutPreparing {
			httpx.Error(w, http.StatusBadRequest, MsgPreparing)
		} else {
			httpx.Error(w, http.StatusBadRequest, MsgFinished)
		}
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	publish(h.d, intent)
	httpx.JSON(w, http.StatusOK, render(intent))
}

// Cancel is POST /checkout/intents/{id}/cancel → 204 while nothing is paid
// (an intent already cancelled or failed stays as it is); 409 once the
// agent is paying or has booked.
func (h *H) Cancel(w http.ResponseWriter, r *http.Request) {
	intent, err := h.d.Store.CheckoutIntents().Cancel(r.Context(), api.UserID(r), mux.Vars(r)["id"])
	var conflict *store.CheckoutStateError
	if errors.As(err, &conflict) {
		if paying(conflict.State) {
			httpx.Error(w, http.StatusConflict, MsgWentThrough)
		} else {
			httpx.NoContent(w)
		}
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	publish(h.d, intent)
	httpx.NoContent(w)
}

// paying reports the states in which the purchase went (or is going) through.
func paying(state string) bool {
	return state == models.CheckoutProcessing || state == models.CheckoutBooked
}
