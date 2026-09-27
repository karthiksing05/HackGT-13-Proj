package checkout

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// Agentic checkout (checkout runs): the buyer approves a budget once and the
// agent (pkg/agent) buys every listed ticket from its merchant, never
// spending more than that budget in total.

// Sentences the app shows verbatim.
const (
	MsgAgentOff       = "Agentic checkout isn't set up on this server yet."
	MsgRunBudget      = "Set a budget between $1 and $1,000."
	MsgRunNoItems     = "Pick at least one event to get tickets for."
	MsgRunTooMany     = "Muse can get tickets for up to 10 events at a time."
	MsgRunNotSold     = "Muse can't buy tickets for that one. Get them on the event's own site."
	MsgRunBooked      = "You already have tickets for that."
	MsgRunActive      = "Muse is already getting tickets for this plan."
	MsgRunInitialStep = "Waiting for Muse to open the ticket page"
)

// maxRunBudgetCents caps what one run may spend ($1,000).
const maxRunBudgetCents = 100_000

// maxRunItems caps the items in one run.
const maxRunItems = 10

// sellerHost is the host tickets for item are sold on when the agent can buy
// them there (MERCHANT_HOST), else "".
func (h *H) sellerHost(item *models.ItineraryItem) string {
	if item.Kind != models.ItemStop || item.TicketURL == nil {
		return ""
	}
	u, err := url.Parse(*item.TicketURL)
	if err != nil || !strings.EqualFold(u.Hostname(), h.d.Cfg.MerchantHost) {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// agentReady reports whether runs can be started here.
func (h *H) agentReady() bool { return h.d.Runner != nil && h.d.Cfg.AgentCheckoutReady() }

// Plan is GET /itineraries/{id}/checkout → CheckoutPlan: the paid stops the
// agent can buy for the viewer, a suggested budget and the card it would use.
func (h *H) Plan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	itin, err := h.d.Store.Itineraries().ForMember(ctx, mux.Vars(r)["id"], userID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	var ids []string
	for i := range itin.Items {
		if h.sellerHost(&itin.Items[i]) != "" {
			ids = append(ids, itin.Items[i].ID)
		}
	}
	tickets, err := h.d.Store.CheckoutReads().Tickets(ctx, userID, ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	plan := contract.CheckoutPlan{
		ItineraryID:        itin.ID,
		Available:          h.agentReady(),
		AgenticCheckout:    user.Prefs.InstantCheckout,
		Items:              []contract.CheckoutPlanItem{},
		DefaultBudgetCents: user.Prefs.InstantCheckoutLimitCents,
	}
	for i := range itin.Items {
		item := &itin.Items[i]
		host := h.sellerHost(item)
		if host == "" {
			continue
		}
		pi := contract.CheckoutPlanItem{
			ItemID: item.ID, Title: item.Title, Start: contract.NewTime(item.Start),
			Merchant: host, TicketURL: *item.TicketURL, PriceCents: item.PriceCents, Quantity: 1,
		}
		if t, ok := tickets[item.ID]; ok {
			pi.Booked, pi.Confirmation = true, t.Confirmation
		} else if item.PriceCents != nil {
			plan.EstimateCents += *item.PriceCents
		}
		plan.Items = append(plan.Items, pi)
	}
	plan.SuggestedBudgetCents = suggestedBudget(plan.EstimateCents, plan.DefaultBudgetCents)
	cards, err := h.d.Store.CheckoutReads().Cards(ctx, userID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if card := pickCard(cards, nil); card != nil {
		plan.PaymentMethodID, plan.CardBrand, plan.CardLast4 = &card.ID, &card.Brand, &card.Last4
	}
	if run, err := h.d.Store.CheckoutRuns().Active(ctx, userID, itin.ID); err == nil {
		plan.ActiveRunID = &run.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, plan)
}

// suggestedBudget covers the estimate plus 15% for fees, rounded up to whole
// dollars, and is never below the user's default budget.
func suggestedBudget(estimateCents, defaultCents int) int {
	withFees := (estimateCents*115/100 + 99) / 100 * 100
	return min(max(withFees, defaultCents), maxRunBudgetCents)
}

// CreateRun is POST /itineraries/{id}/checkout-runs (CreateCheckoutRun) →
// 201 CheckoutRun. This is the buyer's one approval: every listed item may
// be bought, and the purchases together never exceed budget_cents.
func (h *H) CreateRun(w http.ResponseWriter, r *http.Request) {
	var req contract.CreateCheckoutRun
	if !httpx.Decode(w, r, &req) {
		return
	}
	switch {
	case !h.agentReady():
		httpx.Error(w, http.StatusServiceUnavailable, MsgAgentOff)
		return
	case req.BudgetCents < 100 || req.BudgetCents > maxRunBudgetCents:
		httpx.Error(w, http.StatusBadRequest, MsgRunBudget)
		return
	case len(req.Items) == 0:
		httpx.Error(w, http.StatusBadRequest, MsgRunNoItems)
		return
	case len(req.Items) > maxRunItems:
		httpx.Error(w, http.StatusBadRequest, MsgRunTooMany)
		return
	}
	ctx := r.Context()
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	itin, err := h.d.Store.Itineraries().ForMember(ctx, mux.Vars(r)["id"], userID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	cards, err := h.d.Store.CheckoutReads().Cards(ctx, userID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	card := pickCard(cards, req.PaymentMethodID)
	if card == nil {
		httpx.Error(w, http.StatusBadRequest, MsgNoCard)
		return
	}
	ids := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		ids = append(ids, strings.TrimSpace(it.ItemID))
	}
	tickets, err := h.d.Store.CheckoutReads().Tickets(ctx, userID, ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	intents, status, msg := h.runIntents(userID, itin, ids, req.Items, tickets, card)
	if msg != "" {
		httpx.Error(w, status, msg)
		return
	}
	run := &models.CheckoutRun{
		UserID: userID, ItineraryID: itin.ID, State: models.RunRunning, BudgetCents: req.BudgetCents, Currency: "usd",
		PaymentMethodID: card.ID, CardBrand: card.Brand, CardLast4: card.Last4,
	}
	if err := h.d.Store.CheckoutRuns().Create(ctx, run, intents); err != nil {
		if errors.Is(err, store.ErrRunActive) {
			httpx.Error(w, http.StatusConflict, MsgRunActive)
			return
		}
		api.Fail(w, r, err)
		return
	}
	for _, intent := range intents {
		publish(h.d, intent)
	}
	realtime.CheckoutRun(h.d.Publish(), userID, run.ID, contract.CheckoutRunState(run.State), 0)
	h.d.Runner.Start(run.ID)
	httpx.JSON(w, http.StatusCreated, renderRun(run, intents))
}

// runIntents builds one intent per requested item, or the status and
// sentence that refuse the request.
func (h *H) runIntents(userID string, itin *models.Itinerary, ids []string, items []contract.CheckoutRunItem, tickets map[string]models.ItemTicket, card *models.PaymentMethod) ([]*models.CheckoutIntent, int, string) {
	seen := map[string]bool{}
	intents := make([]*models.CheckoutIntent, 0, len(items))
	for i, it := range items {
		id := ids[i]
		quantity := it.Quantity
		if quantity == 0 {
			quantity = 1
		}
		if quantity < 1 || quantity > maxQuantity {
			return nil, http.StatusBadRequest, MsgQuantity
		}
		item := findItem(itin, id)
		if item == nil || seen[id] {
			return nil, http.StatusBadRequest, MsgNoItem
		}
		seen[id] = true
		host := h.sellerHost(item)
		if host == "" {
			return nil, http.StatusBadRequest, MsgRunNotSold
		}
		if _, booked := tickets[id]; booked {
			return nil, http.StatusConflict, MsgRunBooked
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = defaultItemTitle
		}
		cardID := card.ID
		intent := &models.CheckoutIntent{
			ID: store.NewID(), UserID: userID, ItemID: item.ID, ItineraryID: itin.ID, ItemTitle: title,
			Quantity: quantity, PaymentMethodID: &cardID, CardBrand: card.Brand, CardLast4: card.Last4,
			State: models.CheckoutProcessing, Provider: models.ProviderAgentRun, Merchant: host, CheckoutURL: *item.TicketURL,
			Steps: []models.CheckoutStep{{Text: MsgRunInitialStep}},
		}
		if item.PriceCents != nil {
			subtotal := *item.PriceCents * quantity
			intent.SubtotalCents = &subtotal
		}
		intents = append(intents, intent)
	}
	return intents, 0, ""
}

func findItem(itin *models.Itinerary, id string) *models.ItineraryItem {
	for i := range itin.Items {
		if itin.Items[i].ID == id {
			return &itin.Items[i]
		}
	}
	return nil
}

// GetRun is GET /checkout/runs/{id} → CheckoutRun (the owner's only).
func (h *H) GetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	run, err := h.d.Store.CheckoutRuns().Get(ctx, api.UserID(r), mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	intents, err := h.d.Store.CheckoutIntents().ByRun(ctx, run)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, renderRun(run, intents))
}

// CancelRun is POST /checkout/runs/{id}/cancel → CheckoutRun: purchases not
// started yet are not made (one already paying still goes through). A
// finished run comes back as it is.
func (h *H) CancelRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runs := h.d.Store.CheckoutRuns()
	run, err := runs.Cancel(ctx, api.UserID(r), mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if run.State == models.RunCancelled {
		if err := h.d.Store.CheckoutIntents().CancelRun(ctx, run.ID); err != nil {
			api.Fail(w, r, err)
			return
		}
	}
	intents, err := h.d.Store.CheckoutIntents().ByRun(ctx, run)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	for _, intent := range intents {
		if intent.State == models.CheckoutCancelled {
			publish(h.d, intent)
		}
	}
	realtime.CheckoutRun(h.d.Publish(), run.UserID, run.ID, contract.CheckoutRunState(run.State), run.SpentCents)
	httpx.JSON(w, http.StatusOK, renderRun(run, intents))
}

// renderRun is the wire shape of a run and its intents.
func renderRun(run *models.CheckoutRun, intents []*models.CheckoutIntent) contract.CheckoutRun {
	out := contract.CheckoutRun{
		ID: run.ID, ItineraryID: run.ItineraryID, State: contract.CheckoutRunState(run.State),
		BudgetCents: run.BudgetCents, SpentCents: run.SpentCents, Currency: run.Currency,
		CardBrand: run.CardBrand, CardLast4: run.CardLast4, Intents: make([]contract.CheckoutIntent, 0, len(intents)),
		CreatedAt: contract.NewTime(run.CreatedAt),
	}
	if run.Agent != "" {
		out.Agent = &run.Agent
	}
	if run.Summary != "" {
		out.Summary = &run.Summary
	}
	if run.FinishedAt != nil {
		t := contract.NewTime(*run.FinishedAt)
		out.FinishedAt = &t
	}
	for _, intent := range intents {
		out.Intents = append(out.Intents, render(intent))
	}
	return out
}
