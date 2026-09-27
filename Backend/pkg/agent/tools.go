package agent

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/payments"
	"Backend/pkg/realtime"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// sptLifetime is how long an issued token can be charged.
const sptLifetime = 10 * time.Minute

// runState is one run in progress: its intents by item, the latest quote
// for each, and what the model said when it finished.
type runState struct {
	r        *Runner
	run      *models.CheckoutRun
	buyer    struct{ name, email string }
	items    []string // item ids in the run's order
	intents  map[string]*models.CheckoutIntent
	quotes   map[string]*Quote
	finished bool
	note     string
}

func (r *Runner) newRunState(ctx context.Context, run *models.CheckoutRun) (*runState, error) {
	intents, err := r.d.Store.CheckoutIntents().ByRun(ctx, run)
	if err != nil {
		return nil, err
	}
	st := &runState{r: r, run: run, intents: map[string]*models.CheckoutIntent{}, quotes: map[string]*Quote{}}
	for _, intent := range intents {
		st.items = append(st.items, intent.ItemID)
		st.intents[intent.ItemID] = intent
	}
	if user, err := r.d.Store.Users().ByID(ctx, run.UserID); err == nil {
		st.buyer.name, st.buyer.email = user.Name, user.Email
	}
	return st, nil
}

// pending is the item's intent when it still needs buying.
func (st *runState) pending(itemID string) (*models.CheckoutIntent, string) {
	intent, ok := st.intents[itemID]
	if !ok {
		return nil, "unknown_item"
	}
	if intent.State != models.CheckoutProcessing || intent.OrderRef != "" {
		return nil, "already_" + intent.State
	}
	return intent, ""
}

// remaining is the budget not yet spent or held.
func (st *runState) remaining(ctx context.Context) int {
	run, err := st.r.d.Store.CheckoutRuns().ByID(ctx, st.run.ID)
	if err != nil {
		return 0
	}
	return run.BudgetCents - run.SpentCents - run.ReservedCents
}

// ---- tools ------------------------------------------------------------------

// dispatch runs one tool call and returns the JSON result for the model.
func (st *runState) dispatch(ctx context.Context, name, args string) string {
	var in struct {
		URL     string `json:"url"`
		ItemID  string `json:"item_id"`
		QuoteID string `json:"quote_id"`
		Reason  string `json:"reason"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return result(map[string]any{"error": "bad_arguments"})
	}
	st.log(ctx, "tool_call", name, args)
	var out map[string]any
	switch name {
	case "open_page":
		out = st.openPage(ctx, in.URL)
	case "get_offer":
		out = st.getOffer(ctx, in.ItemID)
	case "buy_tickets":
		out = st.buyTickets(ctx, in.ItemID, in.QuoteID)
	case "skip_item":
		out = st.skipItem(ctx, in.ItemID, in.Reason)
	case "finish":
		st.finished, st.note = true, clip(in.Summary, 500)
		out = map[string]any{"ok": true}
	default:
		out = map[string]any{"error": "unknown_tool"}
	}
	res := result(out)
	st.log(ctx, "tool_result", name, res)
	return res
}

func result(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// openPage reads a merchant page. Only MERCHANT_HOST URLs open.
func (st *runState) openPage(ctx context.Context, rawURL string) map[string]any {
	page, err := st.r.merchant.Page(ctx, rawURL)
	if errors.Is(err, ErrNotMerchant) {
		return map[string]any{"error": "not_allowed", "hint": "Only the items' ticket pages on the ticket merchant can be opened."}
	}
	if err != nil {
		return map[string]any{"error": "page_unavailable"}
	}
	for _, id := range st.items {
		if intent := st.intents[id]; Slug(intent.CheckoutURL) == Slug(rawURL) {
			st.step(ctx, intent, "Opened the ticket page", true)
		}
	}
	var out map[string]any
	b, _ := json.Marshal(page)
	_ = json.Unmarshal(b, &out)
	return out
}

// getOffer asks the merchant for the item's price at the approved quantity.
func (st *runState) getOffer(ctx context.Context, itemID string) map[string]any {
	intent, why := st.pending(itemID)
	if intent == nil {
		return map[string]any{"error": why}
	}
	q, err := st.r.merchant.Offer(ctx, Slug(intent.CheckoutURL), intent.Quantity)
	var merr *MerchantError
	switch {
	case errors.As(err, &merr) && merr.Code == "sold_out":
		st.fail(ctx, intent, models.FailureSoldOut, "Sold out on "+intent.Merchant+".")
		return map[string]any{"error": "sold_out"}
	case err != nil:
		return map[string]any{"error": "merchant_unavailable"}
	}
	st.quotes[itemID] = q
	st.setQuote(ctx, intent, q, fmt.Sprintf("Found %d %s · %s with fees", q.Quantity, plural(q.Quantity, "ticket"), money(q.TotalCents)))
	return map[string]any{
		"quote_id": q.QuoteID, "quantity": q.Quantity, "currency": q.Currency, "unit_cents": q.UnitCents,
		"fees_cents": q.FeesCents, "total_cents": q.TotalCents, "available": q.Available,
		"remaining_budget_cents": st.remaining(ctx),
	}
}

// buyTickets pays for the item's latest quote: reserve the budget, issue a
// token limited to the quote, place the signed order, record the ticket.
func (st *runState) buyTickets(ctx context.Context, itemID, quoteID string) map[string]any {
	intent, why := st.pending(itemID)
	if intent == nil {
		return map[string]any{"error": why}
	}
	q := st.quotes[itemID]
	if q == nil || q.QuoteID != quoteID {
		return map[string]any{"error": "unknown_quote", "hint": "Call get_offer for this item and use its quote_id."}
	}
	runs := st.r.d.Store.CheckoutRuns()
	if running, _ := runs.Running(ctx, st.run.ID); !running {
		return map[string]any{"error": "cancelled"}
	}
	reserved, err := runs.Reserve(ctx, st.run.ID, q.TotalCents)
	if err != nil {
		return map[string]any{"error": "try_again"}
	}
	if !reserved {
		left := st.remaining(ctx)
		st.fail(ctx, intent, models.FailureOverBudget, fmt.Sprintf("%s is more than the %s left in your budget.", money(q.TotalCents), money(max(left, 0))))
		return map[string]any{"error": "over_budget", "remaining_budget_cents": left}
	}
	release := func() { _ = runs.Release(context.WithoutCancel(ctx), st.run.ID, q.TotalCents) }

	spt, err := st.r.issuer.Issue(ctx, payments.IssueRequest{
		PaymentMethod: payments.TestPaymentMethod(st.run.CardBrand, st.run.CardLast4),
		SellerProfile: st.r.seller, MaxCents: q.TotalCents, Currency: currencyOf(q),
		ExpiresAt: st.r.d.Clock().Add(sptLifetime), IdempotencyKey: "spt-" + intent.ID + "-" + q.QuoteID,
	})
	if err != nil {
		release()
		if errors.Is(err, payments.ErrDeclined) {
			st.fail(ctx, intent, models.FailureCardDeclined, "Your "+st.run.CardBrand+" •••• "+st.run.CardLast4+" was declined.")
			return map[string]any{"error": "card_declined"}
		}
		return map[string]any{"error": "payment_unavailable"}
	}
	limit := q.TotalCents
	st.update(ctx, intent, bson.M{"maxAuthorizedCents": limit}, fmt.Sprintf("Paying with a Stripe token limited to %s", money(limit)), true)

	var order OrderRequest
	order.QuoteID, order.Quantity, order.ExpectedTotalCents = q.QuoteID, intent.Quantity, q.TotalCents
	order.Payment.Scheme, order.Payment.Token = "stripe_spt", spt
	order.Buyer.Name, order.Buyer.Email = st.buyer.name, st.buyer.email
	conf, err := st.r.merchant.Order(ctx, intent.ID+":"+q.QuoteID, order)
	if err != nil {
		release()
		_ = st.r.issuer.Revoke(context.WithoutCancel(ctx), spt)
		return st.orderFailed(ctx, intent, err)
	}
	if err := runs.Commit(ctx, st.run.ID, q.TotalCents, conf.TotalCents); err != nil {
		return map[string]any{"error": "try_again"}
	}
	st.booked(ctx, intent, conf)
	return map[string]any{
		"status": "booked", "confirmation": conf.ConfirmationCode, "total_cents": conf.TotalCents,
		"admit": conf.Ticket.Admit, "ticket_url": conf.Ticket.TicketURL, "remaining_budget_cents": st.remaining(ctx),
	}
}

// orderFailed turns a merchant refusal into the model's answer (and the
// intent's failure when it can't be retried).
func (st *runState) orderFailed(ctx context.Context, intent *models.CheckoutIntent, err error) map[string]any {
	var merr *MerchantError
	if !errors.As(err, &merr) {
		return map[string]any{"error": "merchant_unavailable"}
	}
	switch merr.Code {
	case "price_changed":
		q := *st.quotes[intent.ItemID]
		q.QuoteID, q.TotalCents = merr.QuoteID, merr.TotalCents
		q.FeesCents = q.TotalCents - q.SubtotalCents
		st.quotes[intent.ItemID] = &q
		st.setQuote(ctx, intent, &q, "The price changed to "+money(q.TotalCents))
		return map[string]any{"error": "price_changed", "quote_id": q.QuoteID, "total_cents": q.TotalCents, "remaining_budget_cents": st.remaining(ctx)}
	case "sold_out":
		st.fail(ctx, intent, models.FailureSoldOut, "Sold out on "+intent.Merchant+".")
		return map[string]any{"error": "sold_out"}
	case "quote_expired":
		delete(st.quotes, intent.ItemID)
		return map[string]any{"error": "quote_expired", "hint": "Call get_offer again."}
	case "declined":
		sentence := "The payment was declined."
		if merr.DeclineReason == "over_limit" {
			sentence = "The merchant asked for more than you approved, so Stripe declined the charge."
		}
		st.fail(ctx, intent, models.FailureDeclined, sentence)
		return map[string]any{"error": "declined", "reason": merr.DeclineReason}
	}
	st.fail(ctx, intent, models.FailureMerchantError, "The ticket site didn't take the order.")
	return map[string]any{"error": merr.Code}
}

// skipItem gives an item up (the model's decision, e.g. nothing affordable).
func (st *runState) skipItem(ctx context.Context, itemID, reason string) map[string]any {
	intent, why := st.pending(itemID)
	if intent == nil {
		return map[string]any{"error": why}
	}
	sentence := "Muse skipped this one."
	if r := clip(reason, 160); r != "" {
		sentence = "Muse skipped this one: " + r
	}
	st.fail(ctx, intent, models.FailureSkipped, sentence)
	return map[string]any{"ok": true}
}

// ---- intents ------------------------------------------------------------------

func (st *runState) step(ctx context.Context, intent *models.CheckoutIntent, text string, done bool) {
	st.update(ctx, intent, bson.M{}, text, done)
}

// update sets fields on a processing intent, appends a step and tells the app.
func (st *runState) update(ctx context.Context, intent *models.CheckoutIntent, set bson.M, text string, done bool) {
	var steps []models.CheckoutStep
	if text != "" {
		steps = append(steps, models.CheckoutStep{Text: text, Done: done})
	}
	st.apply(ctx, intent, []string{models.CheckoutProcessing}, set, steps...)
}

func (st *runState) setQuote(ctx context.Context, intent *models.CheckoutIntent, q *Quote, text string) {
	st.update(ctx, intent, bson.M{"subtotalCents": q.SubtotalCents, "feesCents": q.FeesCents, "totalCents": q.TotalCents}, text, true)
}

func (st *runState) fail(ctx context.Context, intent *models.CheckoutIntent, code, sentence string) {
	st.apply(ctx, intent, []string{models.CheckoutProcessing},
		bson.M{"state": models.CheckoutFailed, "failureCode": code, "failureReason": sentence},
		models.CheckoutStep{Text: sentence, Done: true})
}

// booked records the purchase: the ticket on the buyer's item first (so a
// crash between the writes is repaired by a retry), then the intent. A run
// cancelled while this purchase was paying still books it: it was paid.
func (st *runState) booked(ctx context.Context, intent *models.CheckoutIntent, conf *Confirmation) {
	ctx = context.WithoutCancel(ctx)
	total, code, url := conf.TotalCents, conf.ConfirmationCode, conf.Ticket.TicketURL
	admit := conf.Ticket.Admit
	if admit == 0 {
		admit = intent.Quantity
	}
	_ = st.r.d.Store.CheckoutReads().SaveTicket(ctx, intent.UserID, intent.ItineraryID, intent.ItemID, models.ItemTicket{
		ID: conf.Ticket.TicketID, Quantity: admit, TotalCents: &total, Confirmation: &code, URL: &url,
	})
	st.apply(ctx, intent, []string{models.CheckoutProcessing, models.CheckoutCancelled}, bson.M{
		"state": models.CheckoutBooked, "failureCode": "", "orderRef": conf.OrderID, "confirmation": code,
		"ticketUrl": url, "finalCents": total, "totalCents": total, "subtotalCents": conf.SubtotalCents, "feesCents": conf.FeesCents,
	}, models.CheckoutStep{Text: "Booked · " + code, Done: true})
}

// apply writes to the intent and publishes it; the local copy follows.
func (st *runState) apply(ctx context.Context, intent *models.CheckoutIntent, from []string, set bson.M, steps ...models.CheckoutStep) {
	updated, ok, err := st.r.d.Store.CheckoutIntents().RunUpdate(ctx, intent.ID, from, set, steps...)
	if err != nil || !ok {
		if fresh, gerr := st.r.d.Store.CheckoutIntents().Get(ctx, intent.UserID, intent.ID); gerr == nil {
			*intent = *fresh
		}
		return
	}
	*intent = *updated
	realtime.CheckoutStatus(st.r.d.Publish(), intent.UserID, intent.ID, contract.CheckoutState(intent.State))
}

// log appends a transcript line (tool arguments and results never hold a
// payment token: tokens stay inside buyTickets).
func (st *runState) log(ctx context.Context, kind, name, text string) {
	_ = st.r.d.Store.CheckoutRuns().Log(context.WithoutCancel(ctx), st.run.ID,
		models.RunEvent{At: st.r.d.Clock().UTC(), Kind: kind, Name: name, Text: clip(text, 600)})
}

// ---- endings ------------------------------------------------------------------

// fallback buys every item still pending in a fixed order: the offer, then
// the purchase, once more if the price changed (the budget still applies).
func (st *runState) fallback(ctx context.Context) {
	for _, id := range st.items {
		if running, _ := st.r.d.Store.CheckoutRuns().Running(ctx, st.run.ID); !running || ctx.Err() != nil {
			return
		}
		if intent, _ := st.pending(id); intent == nil {
			continue
		}
		offer := st.dispatch(ctx, "get_offer", itemArgs(id, ""))
		var o struct {
			QuoteID string `json:"quote_id"`
		}
		if json.Unmarshal([]byte(offer), &o) != nil || o.QuoteID == "" {
			continue
		}
		res := st.dispatch(ctx, "buy_tickets", itemArgs(id, o.QuoteID))
		var b struct {
			Error   string `json:"error"`
			QuoteID string `json:"quote_id"`
		}
		if json.Unmarshal([]byte(res), &b) == nil && b.Error == "price_changed" && b.QuoteID != "" {
			st.dispatch(ctx, "buy_tickets", itemArgs(id, b.QuoteID))
		}
	}
}

func itemArgs(itemID, quoteID string) string {
	b, _ := json.Marshal(map[string]string{"item_id": itemID, "quote_id": quoteID})
	return string(b)
}

// failLeftovers fails what neither Muse nor the fallback could finish (a run
// cancelled meanwhile has cancelled them already).
func (st *runState) failLeftovers(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	for _, id := range st.items {
		if intent, _ := st.pending(id); intent != nil {
			st.fail(ctx, intent, models.FailureAgentError, "Muse couldn't finish this one. Try again, or buy it on the event's page.")
		}
	}
}

// finish writes the summary, ends the run and tells the app.
func (st *runState) finish(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	intents, err := st.r.d.Store.CheckoutIntents().ByRun(ctx, st.run)
	if err != nil {
		return err
	}
	run, err := st.r.d.Store.CheckoutRuns().Finish(ctx, st.run.ID, Summary(st.run.BudgetCents, intents), st.note)
	if err != nil {
		return err
	}
	realtime.CheckoutRun(st.r.d.Publish(), run.UserID, run.ID, contract.CheckoutRunState(run.State), run.SpentCents)
	return nil
}

// Summary is the run's result in plain words, from what actually happened.
func Summary(budgetCents int, intents []*models.CheckoutIntent) string {
	var booked, missed []string
	spent := 0
	for _, in := range intents {
		switch in.State {
		case models.CheckoutBooked:
			total := 0
			if in.FinalCents != nil {
				total = *in.FinalCents
			}
			spent += total
			booked = append(booked, fmt.Sprintf("%s (%d %s, %s, %s)", in.ItemTitle, in.Quantity, plural(in.Quantity, "ticket"), money(total), in.Confirmation))
		default:
			why := "not bought"
			switch in.FailureCode {
			case models.FailureSoldOut:
				why = "sold out"
			case models.FailureOverBudget:
				why = "over budget"
			case models.FailureDeclined, models.FailureCardDeclined:
				why = "payment declined"
			case models.FailureCancelled:
				why = "cancelled"
			}
			missed = append(missed, in.ItemTitle+": "+why)
		}
	}
	var parts []string
	switch {
	case len(booked) == 0:
		parts = append(parts, "Muse couldn't get any tickets.")
	case len(missed) == 0:
		parts = append(parts, "Muse got all your tickets: "+strings.Join(booked, "; ")+".")
	default:
		parts = append(parts, fmt.Sprintf("Muse got %d of %d: %s.", len(booked), len(intents), strings.Join(booked, "; ")))
	}
	if len(missed) > 0 {
		parts = append(parts, strings.Join(missed, "; ")+".")
	}
	parts = append(parts, fmt.Sprintf("Spent %s of your %s budget (sandbox: nothing was charged).", money(spent), money(budgetCents)))
	return strings.Join(parts, " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func currencyOf(q *Quote) string {
	if q.Currency == "" {
		return "usd"
	}
	return strings.ToLower(q.Currency)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
