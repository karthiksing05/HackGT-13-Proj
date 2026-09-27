package api

import (
	"encoding/json"
	"errors"
	"events/pkg/models"
	"events/pkg/payments"
	"events/pkg/store"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// maxOrderBody caps the order request body.
const maxOrderBody = 16 << 10

// HandlePostOrder places an order for POST /api/orders.
// Route requires TAP signature tag: agent-payer-auth.
//
// Order: signature → Idempotency-Key → body → replay → scenarios → quote →
// re-price → reserve seats → charge the Stripe SPT → record. Seats go back
// when the charge fails. The charge carries an idempotency key derived from
// the order's, so a retried order never charges twice.
func (d *Deps) HandlePostOrder(w http.ResponseWriter, r *http.Request) {
	parsedSig, ok := d.verifyTAP(w, r, "agent-payer-auth")
	if !ok {
		return
	}

	ctx := r.Context()
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey == "" || len(idemKey) > 255 {
		writeJSONError(w, http.StatusBadRequest, "missing_idempotency", "")
		return
	}

	var req models.OrderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOrderBody)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "")
		return
	}
	if req.Payment.Scheme != models.PaymentSchemeStripeSPT || !strings.HasPrefix(req.Payment.Token, "spt_") {
		writeJSONError(w, http.StatusBadRequest, "unsupported_payment", "")
		return
	}

	// A retry of an order already placed returns that order.
	if existing, err := d.Store.GetOrderByIdempotencyKey(ctx, idemKey); err == nil && existing != nil {
		d.replay(w, existing, req)
		return
	}

	scenario := d.Store.GetScenario(ctx)
	if scenario == models.ScenarioSlow {
		select {
		case <-time.After(2500 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	if scenario == models.ScenarioSoldOut {
		d.reject(w, r, http.StatusConflict, "sold_out", "")
		return
	}

	quote, err := d.Store.GetQuote(ctx, req.QuoteID)
	if err != nil {
		d.reject(w, r, http.StatusConflict, "quote_expired", "")
		return
	}
	if req.Quantity != quote.Quantity {
		writeJSONError(w, http.StatusConflict, "quantity_mismatch", "")
		return
	}

	now := d.Now().UTC()
	// price_bump: the price rises 40% between the quote and the order, once.
	if scenario == models.ScenarioPriceBump && !quote.Repriced {
		bumped := d.repriceQuote(quote, now)
		if err := d.Store.SaveQuote(ctx, bumped); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal", "")
			return
		}
		d.recordRejection(ctx, r, http.StatusConflict, "price_changed")
		writeAPIError(w, http.StatusConflict, models.APIError{Code: "price_changed", TotalCents: bumped.TotalCents, QuoteID: bumped.QuoteID})
		return
	}
	if req.ExpectedTotalCents != quote.TotalCents {
		d.recordRejection(ctx, r, http.StatusConflict, "price_changed")
		writeAPIError(w, http.StatusConflict, models.APIError{Code: "price_changed", TotalCents: quote.TotalCents, QuoteID: quote.QuoteID})
		return
	}

	if err := d.Store.ReserveTickets(ctx, quote.Event.Slug, req.Quantity); err != nil {
		d.reject(w, r, http.StatusConflict, "sold_out", "")
		return
	}

	// overcharge: a misbehaving merchant asks for 25% more than it quoted.
	// Stripe holds the charge to the token's max_amount and refuses.
	amount := quote.TotalCents
	if scenario == models.ScenarioOvercharge {
		amount = quote.TotalCents * 125 / 100
	}
	charge, err := d.Charger.Charge(ctx, payments.ChargeRequest{
		SPT:            req.Payment.Token,
		AmountCents:    amount,
		Currency:       quote.Currency,
		IdempotencyKey: "order-" + idemKey,
		Description:    fmt.Sprintf("%d × %s (sandbox)", req.Quantity, quote.Event.Title),
		Metadata:       map[string]string{"quote_id": quote.QuoteID, "event": quote.Event.Slug, "agent_key": parsedSig.KeyID},
	})
	if err != nil {
		_ = d.Store.ReleaseTickets(ctx, quote.Event.Slug, req.Quantity)
		d.chargeFailed(w, r, err)
		return
	}

	confirmation := d.newConfirmation(r, quote.Event, req.Quantity, quote.SubtotalCents, amount, quote.Currency, req.Buyer, models.PaymentSummary{
		Scheme:          models.PaymentSchemeStripeSPT,
		Brand:           charge.Brand,
		Last4:           charge.Last4,
		PaymentIntentID: charge.PaymentIntentID,
		LimitCents:      charge.LimitCents,
	}, idemKey, now)
	confirmation.AgentKeyID = parsedSig.KeyID

	if err := d.Store.SaveOrder(ctx, confirmation, idemKey); err != nil {
		if errors.Is(err, store.ErrDuplicateOrder) {
			// A concurrent retry recorded it first; the charge was the same one.
			_ = d.Store.ReleaseTickets(ctx, quote.Event.Slug, req.Quantity)
			if existing, gerr := d.Store.GetOrderByIdempotencyKey(ctx, idemKey); gerr == nil {
				d.replay(w, existing, req)
				return
			}
		}
		log.Error().Err(err).Str("payment_intent", charge.PaymentIntentID).Msg("order charged but not saved")
		writeJSONError(w, http.StatusInternalServerError, "internal", "")
		return
	}

	writeJSON(w, http.StatusCreated, confirmation)
}

// newConfirmation is a confirmed order with a fresh ticket: the one shape
// for orders placed through the API and through the website.
func (d *Deps) newConfirmation(r *http.Request, event models.EventSummary, qty, subtotal, total int, currency string, buyer models.BuyerInfo,
	payment models.PaymentSummary, idemKey string, now time.Time) *models.OrderConfirmation {
	orderID := "SL-" + randomCrockford(5)
	ticketID := randomHex(16) // 128 random bits
	return &models.OrderConfirmation{
		OrderID:          orderID,
		ConfirmationCode: orderID,
		Status:           "confirmed",
		Sandbox:          true,
		Event:            event,
		Quantity:         qty,
		Currency:         currency,
		SubtotalCents:    subtotal,
		FeesCents:        total - subtotal,
		TotalCents:       total,
		Buyer:            buyer,
		Ticket: models.TicketSummary{
			TicketID:  ticketID,
			TicketURL: fmt.Sprintf("%s/t/%s", d.BaseURL(r), ticketID),
			Admit:     qty,
			Barcode:   "SLT-" + randomCrockford(4) + "-" + randomCrockford(4),
		},
		Payment:        payment,
		IdempotencyKey: idemKey,
		CreatedAt:      now,
	}
}

// replay answers a retried order: the stored confirmation when the retry is
// for the same order, 409 when the key was used for a different one.
func (d *Deps) replay(w http.ResponseWriter, existing *models.OrderConfirmation, req models.OrderRequest) {
	if existing.Quantity != req.Quantity {
		writeJSONError(w, http.StatusConflict, "idempotency_conflict", "")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// repriceQuote is quote with the price_bump surge applied, as a new quote.
func (d *Deps) repriceQuote(quote *models.Quote, now time.Time) *models.Quote {
	unit := quote.UnitCents * 14 / 10
	fees := models.CalculateFees(unit, quote.Quantity)
	expires := now.Add(10 * time.Minute)
	bumped := *quote
	bumped.QuoteID = "q_" + randomHex(8)
	bumped.UnitCents = unit
	bumped.SubtotalCents = unit * quote.Quantity
	bumped.FeesCents = fees
	bumped.TotalCents = unit*quote.Quantity + fees
	bumped.QuoteExpiresAt = expires.Format(time.RFC3339)
	bumped.CreatedAt = now
	bumped.ExpiresAt = expires
	bumped.Repriced = true
	return &bumped
}

// chargeFailed answers an order whose charge did not go through.
func (d *Deps) chargeFailed(w http.ResponseWriter, r *http.Request, err error) {
	var decline *payments.DeclineError
	switch {
	case errors.As(err, &decline):
		d.reject(w, r, http.StatusPaymentRequired, "declined", decline.Reason)
	case errors.Is(err, payments.ErrNotConfigured):
		writeJSONError(w, http.StatusServiceUnavailable, "payments_unavailable", "")
	default:
		log.Error().Err(err).Msg("charge failed")
		writeJSONError(w, http.StatusBadGateway, "payments_unavailable", "")
	}
}
