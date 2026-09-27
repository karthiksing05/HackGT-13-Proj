package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"events/pkg/models"
	"events/pkg/tap"
	"events/pkg/ui"
	"events/pkg/visa"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// crockford alphabet excluding I, L, O, U
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomCrockford(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	out := make([]byte, length)
	for i, b := range bytes {
		out[i] = crockford[int(b)%len(crockford)]
	}
	return string(out)
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ------------------------------------------------------------- Web UI Handlers

// HandleHome renders the Ticketmaster-style discovery page at / and /events.
func (d *Deps) HandleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	category := r.URL.Query().Get("category")
	search := r.URL.Query().Get("q")

	events, err := d.Store.ListEvents(ctx, category, search)
	if err != nil {
		http.Error(w, "failed to list events", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ui.RenderHome(w, ui.HomeView{
		Events:   events,
		Category: category,
		Search:   search,
	})
}

// HandleEvent renders the event details page at /{slug}.
func (d *Deps) HandleEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := mux.Vars(r)["slug"]

	if models.IsReservedSlug(slug) {
		http.NotFound(w, r)
		return
	}

	event, err := d.Store.GetEvent(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	baseURL := d.Cfg.MerchantBaseURL
	jsonld := ui.BuildJSONLDEvent(event, baseURL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ui.RenderEvent(w, ui.EventView{
		Event:   event,
		JSONLD:  template.JS(jsonld),
		Host:    d.Cfg.MerchantHost,
		BaseURL: baseURL,
	})
}

// HandleTickets renders the ticket selection & checkout page at /{slug}/tickets.
func (d *Deps) HandleTickets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := mux.Vars(r)["slug"]

	if models.IsReservedSlug(slug) {
		http.NotFound(w, r)
		return
	}

	event, err := d.Store.GetEvent(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	baseURL := d.Cfg.MerchantBaseURL
	jsonld := ui.BuildJSONLDEvent(event, baseURL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ui.RenderTickets(w, ui.TicketsView{
		Event:   event,
		JSONLD:  template.JS(jsonld),
		Host:    d.Cfg.MerchantHost,
		BaseURL: baseURL,
	})
}

// HandleTicketPass renders or returns JSON for /t/{ticket_id}.
func (d *Deps) HandleTicketPass(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ticketID := mux.Vars(r)["ticket_id"]

	order, err := d.Store.GetOrderByTicketID(ctx, ticketID)
	if err != nil {
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			writeJSONError(w, http.StatusNotFound, "not_found", "")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = ui.RenderTicketPass(w, ui.TicketPassView{Found: false})
		return
	}

	// Content negotiation: Accept: application/json returns the ticket object
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(order.Ticket)
		return
	}

	// Hardened headers matching spec and checkout/ticket.go
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' data:;")

	startTime, _ := time.Parse(time.RFC3339, order.Event.StartsAt)
	jsonld := ui.BuildJSONLDReservation(order)

	_ = ui.RenderTicketPass(w, ui.TicketPassView{
		Found:         true,
		Order:         order,
		FormattedDate: startTime.Format("Mon, Jan 2, 2006"),
		FormattedTime: startTime.Format("3:04 PM MST"),
		JSONLD:        template.JS(jsonld),
	})
}

// HandleDashboard renders the live operator booth dashboard at /dashboard.
func (d *Deps) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.Header.Get("X-Demo-Key")
	}

	orders, _ := d.Store.ListRecentOrders(ctx, 20)
	rejected, _ := d.Store.ListRejectedRequests(ctx, 10)
	scenario := d.Store.GetScenario(ctx)
	totalOrders := d.Store.TotalOrders(ctx)
	totalGross := d.Store.TotalGrossCents(ctx)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = ui.RenderDashboard(w, ui.DashboardView{
		DemoKey:         d.Cfg.DemoKey,
		Scenario:        scenario,
		Orders:          orders,
		Rejected:        rejected,
		TotalOrders:     totalOrders,
		TotalGrossCents: totalGross,
		Host:            d.Cfg.MerchantHost,
	})
}

// ----------------------------------------------------------------- API Handlers

// HandleGetOffer returns a quote for GET /api/events/{slug}/offer?quantity=N.
// Route requires TAP signature tag: agent-browser-auth.
func (d *Deps) HandleGetOffer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := d.Now().UTC()

	// 1. Verify TAP signature with tag: agent-browser-auth
	_, err := tap.Verify(r, d.KeyDirectory, d.Store, "agent-browser-auth", d.Cfg.MerchantHost, now)
	if err != nil {
		d.recordRejection(ctx, r, http.StatusUnauthorized, "bad_signature")
		writeJSONError(w, http.StatusUnauthorized, "bad_signature", "")
		return
	}

	slug := mux.Vars(r)["slug"]
	event, err := d.Store.GetEvent(ctx, slug)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not_found", "")
		return
	}

	qty := 1
	if qStr := r.URL.Query().Get("quantity"); qStr != "" {
		if parsed, err := strconv.Atoi(qStr); err == nil && parsed > 0 && parsed <= 10 {
			qty = parsed
		}
	}

	// Check if active scenario is sold_out
	if d.Store.GetScenario(ctx) == models.ScenarioSoldOut || event.Remaining < qty {
		writeJSONError(w, http.StatusConflict, "sold_out", "")
		return
	}

	unitCents := event.UnitCents
	// If price_bump scenario active, bump unit price by 40%
	if d.Store.GetScenario(ctx) == models.ScenarioPriceBump {
		unitCents = (unitCents * 14) / 10
	}

	feesCents := models.CalculateFees(unitCents, qty)
	totalCents := (unitCents * qty) + feesCents

	quoteID := "q_" + randomHex(8)
	expiresAt := now.Add(10 * time.Minute)

	quote := &models.Quote{
		QuoteID:        quoteID,
		Event:          event.SummaryView(),
		QuoteExpiresAt: expiresAt.Format(time.RFC3339),
		Quantity:       qty,
		Available:      event.Remaining,
		Currency:       "usd",
		UnitCents:      unitCents,
		SubtotalCents:  unitCents * qty,
		FeesCents:      feesCents,
		TotalCents:     totalCents,
		MerchantID:     "sidequestz-events",
		Sandbox:        true,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}

	if err := d.Store.SaveQuote(ctx, quote); err != nil {
		http.Error(w, "failed to save quote", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(quote)
}

// HandlePostOrder places an order for POST /api/orders.
// Route requires TAP signature tag: agent-payer-auth.
func (d *Deps) HandlePostOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := d.Now().UTC()

	// 1. Verify TAP signature with tag: agent-payer-auth
	parsedSig, err := tap.Verify(r, d.KeyDirectory, d.Store, "agent-payer-auth", d.Cfg.MerchantHost, now)
	if err != nil {
		d.recordRejection(ctx, r, http.StatusUnauthorized, "bad_signature")
		writeJSONError(w, http.StatusUnauthorized, "bad_signature", "")
		return
	}

	// 2. Validate Idempotency-Key
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		http.Error(w, "missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Check if already ordered with this idempotency key
	if existing, err := d.Store.GetOrderByIdempotencyKey(ctx, idemKey); err == nil && existing != nil {
		// Idempotency replay: verify parameters match
		if existing.Quantity == req.Quantity && existing.TotalCents == req.ExpectedTotalCents {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(existing)
			return
		}
		// Conflict: body differs
		writeJSONError(w, http.StatusConflict, "idempotency_conflict", "")
		return
	}

	// Check scenario: slow
	if d.Store.GetScenario(ctx) == models.ScenarioSlow {
		time.Sleep(2500 * time.Millisecond)
	}

	// Check scenario: sold_out
	if d.Store.GetScenario(ctx) == models.ScenarioSoldOut {
		d.recordRejection(ctx, r, http.StatusConflict, "sold_out")
		writeJSONError(w, http.StatusConflict, "sold_out", "")
		return
	}

	// 3. Load quote
	quote, err := d.Store.GetQuote(ctx, req.QuoteID)
	if err != nil {
		d.recordRejection(ctx, r, http.StatusConflict, "quote_expired")
		writeJSONError(w, http.StatusConflict, "quote_expired", "")
		return
	}

	// 4. Re-pricing check (scenario: price_bump adds 40% at order time)
	if d.Store.GetScenario(ctx) == models.ScenarioPriceBump {
		bumpedUnit := (quote.UnitCents * 14) / 10
		bumpedFees := models.CalculateFees(bumpedUnit, req.Quantity)
		bumpedTotal := (bumpedUnit * req.Quantity) + bumpedFees
		newQID := "q_" + randomHex(8)
		newQuote := &models.Quote{
			QuoteID:        newQID,
			Event:          quote.Event,
			QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
			Quantity:       req.Quantity,
			Available:      quote.Available,
			Currency:       "usd",
			UnitCents:      bumpedUnit,
			SubtotalCents:  bumpedUnit * req.Quantity,
			FeesCents:      bumpedFees,
			TotalCents:     bumpedTotal,
			MerchantID:     "sidequestz-events",
			Sandbox:        true,
			CreatedAt:      now,
			ExpiresAt:      now.Add(10 * time.Minute),
		}
		_ = d.Store.SaveQuote(ctx, newQuote)
		d.recordRejection(ctx, r, http.StatusConflict, "price_changed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(models.APIError{
			Code:          http.StatusConflict,
			Message:       "price_changed",
			NewTotalCents: bumpedTotal,
			NewQuoteID:    newQID,
		})
		return
	}

	if req.ExpectedTotalCents != quote.TotalCents {
		d.recordRejection(ctx, r, http.StatusConflict, "price_changed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(models.APIError{
			Code:          http.StatusConflict,
			Message:       "price_changed",
			NewTotalCents: quote.TotalCents,
			NewQuoteID:    quote.QuoteID,
		})
		return
	}

	// 5. Reserve inventory atomically
	if err := d.Store.ReserveTickets(ctx, quote.Event.Slug, req.Quantity); err != nil {
		d.recordRejection(ctx, r, http.StatusConflict, "sold_out")
		writeJSONError(w, http.StatusConflict, "sold_out", "")
		return
	}

	// 6. Authorize payment with Visa sandbox
	authReq := visa.AuthorizeRequest{
		Token:         req.Payment.Token,
		Cryptogram:    req.Payment.Cryptogram,
		MerchantID:    "sidequestz-events",
		AmountCents:   quote.TotalCents,
		InstructionID: req.Payment.InstructionID,
	}

	authRes, err := d.Authorizer.Authorize(ctx, authReq)
	if err != nil || authRes.Result != "approved" {
		// Release reserved inventory on decline!
		_ = d.Store.ReleaseTickets(ctx, quote.Event.Slug, req.Quantity)
		reason := "unknown_error"
		if authRes != nil && authRes.Reason != "" {
			reason = authRes.Reason
		}
		d.recordRejection(ctx, r, http.StatusPaymentRequired, "declined: "+reason)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(models.APIError{
			Code:          http.StatusPaymentRequired,
			Message:       "payment declined",
			DeclineReason: reason,
		})
		return
	}

	// 7. Success! Issue ticket and record order
	orderID := "SL-" + randomCrockford(5)
	ticketID := randomHex(16) // 128 random bits
	barcode := "SLT-" + randomCrockford(4) + "-" + randomCrockford(4)
	last4 := extractLast4(req.Payment.Token)

	confirmation := &models.OrderConfirmation{
		OrderID:          orderID,
		ConfirmationCode: orderID,
		Status:           "confirmed",
		Sandbox:          true,
		Event:            quote.Event,
		Quantity:         req.Quantity,
		Currency:         "usd",
		SubtotalCents:    quote.SubtotalCents,
		FeesCents:        quote.FeesCents,
		TotalCents:       quote.TotalCents,
		Buyer:            req.Buyer,
		Ticket: models.TicketSummary{
			TicketID:  ticketID,
			TicketURL: fmt.Sprintf("%s/t/%s", d.Cfg.MerchantBaseURL, ticketID),
			Admit:     req.Quantity,
			Barcode:   barcode,
		},
		Payment: models.PaymentSummary{
			Scheme: "visa_agent_token",
			Last4:  last4,
			AuthID: authRes.AuthID,
		},
		IdempotencyKey: idemKey,
		InstructionID:  req.Payment.InstructionID,
		AgentKeyID:     parsedSig.KeyID,
		CreatedAt:      now,
	}

	if err := d.Store.SaveOrder(ctx, confirmation, idemKey); err != nil {
		log.Error().Err(err).Msg("failed to save order")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(confirmation)
}

// HandleGetOrder retrieves an existing order for GET /api/orders/{order_id}.
func (d *Deps) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := d.Now().UTC()

	// Verify TAP signature with tag: agent-browser-auth
	_, err := tap.Verify(r, d.KeyDirectory, d.Store, "agent-browser-auth", d.Cfg.MerchantHost, now)
	if err != nil {
		d.recordRejection(ctx, r, http.StatusUnauthorized, "bad_signature")
		writeJSONError(w, http.StatusUnauthorized, "bad_signature", "")
		return
	}

	orderID := mux.Vars(r)["order_id"]
	order, err := d.Store.GetOrder(ctx, orderID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not_found", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

// HandleSetScenario toggles scenarios via POST /_demo/scenario.
func (d *Deps) HandleSetScenario(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Demo-Key")
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if key != d.Cfg.DemoKey {
		http.Error(w, "unauthorized demo key", http.StatusUnauthorized)
		return
	}

	var req models.ScenarioState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	valid := map[string]bool{
		models.ScenarioNormal:    true,
		models.ScenarioSoldOut:   true,
		models.ScenarioPriceBump: true,
		models.ScenarioSlow:      true,
	}
	if !valid[req.Scenario] {
		http.Error(w, "invalid scenario", http.StatusBadRequest)
		return
	}

	d.Store.SetScenario(r.Context(), req.Scenario)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"scenario": req.Scenario})
}

// HandleDashboardFeed supplies polling data for GET /api/dashboard/feed.
func (d *Deps) HandleDashboardFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orders, _ := d.Store.ListRecentOrders(ctx, 20)
	rejected, _ := d.Store.ListRejectedRequests(ctx, 10)
	scenario := d.Store.GetScenario(ctx)
	totalOrders := d.Store.TotalOrders(ctx)
	totalGross := d.Store.TotalGrossCents(ctx)

	feedOrders := make([]models.DashboardFeedItem, len(orders))
	for i, o := range orders {
		feedOrders[i] = models.DashboardFeedItem{
			OrderID:               o.OrderID,
			EventTitle:            o.Event.Title,
			EventSlug:             o.Event.Slug,
			Quantity:              o.Quantity,
			TotalCents:            o.TotalCents,
			AgentSigned:           true,
			AgentKeyID:            o.AgentKeyID,
			CardLast4:             o.Payment.Last4,
			InstructionID:         o.InstructionID,
			InstructionLimitCents: 6000, // default demo limit $60.00
			TicketURL:             o.Ticket.TicketURL,
			CreatedAt:             o.CreatedAt,
		}
	}

	feed := models.DashboardFeed{
		Scenario:        scenario,
		Orders:          feedOrders,
		Rejected:        rejected,
		TotalOrders:     totalOrders,
		TotalGrossCents: totalGross,
		ActiveEvents:    25,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(feed)
}

// HandleSimulatePurchase allows browser UI users to trigger a full agentic purchase.
func (d *Deps) HandleSimulatePurchase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Slug     string `json:"slug"`
		Quantity int    `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 2
	}

	event, err := d.Store.GetEvent(ctx, req.Slug)
	if err != nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	fees := models.CalculateFees(event.UnitCents, req.Quantity)
	total := (event.UnitCents * req.Quantity) + fees
	now := d.Now().UTC()

	quoteID := "q_" + randomHex(8)
	quote := &models.Quote{
		QuoteID:        quoteID,
		Event:          event.SummaryView(),
		QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       req.Quantity,
		Available:      event.Remaining,
		Currency:       "usd",
		UnitCents:      event.UnitCents,
		SubtotalCents:  event.UnitCents * req.Quantity,
		FeesCents:      fees,
		TotalCents:     total,
		MerchantID:     "sidequestz-events",
		Sandbox:        true,
		CreatedAt:      now,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
	_ = d.Store.SaveQuote(ctx, quote)

	// Reserve inventory
	if err := d.Store.ReserveTickets(ctx, event.Slug, req.Quantity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "sold_out"})
		return
	}

	// Issue order
	orderID := "SL-" + randomCrockford(5)
	ticketID := randomHex(16)
	barcode := "SLT-" + randomCrockford(4) + "-" + randomCrockford(4)

	conf := &models.OrderConfirmation{
		OrderID:          orderID,
		ConfirmationCode: orderID,
		Status:           "confirmed",
		Sandbox:          true,
		Event:            quote.Event,
		Quantity:         req.Quantity,
		Currency:         "usd",
		SubtotalCents:    quote.SubtotalCents,
		FeesCents:        quote.FeesCents,
		TotalCents:       quote.TotalCents,
		Buyer: models.BuyerInfo{
			Name:  "Sandy Byte",
			Email: "demo@sidequestz.tech",
		},
		Ticket: models.TicketSummary{
			TicketID:  ticketID,
			TicketURL: fmt.Sprintf("%s/t/%s", d.Cfg.MerchantBaseURL, ticketID),
			Admit:     req.Quantity,
			Barcode:   barcode,
		},
		Payment: models.PaymentSummary{
			Scheme: "visa_agent_token",
			Last4:  "1881",
			AuthID: "sbx_auth_" + randomHex(6),
		},
		InstructionID: "sbx_ins_demo",
		AgentKeyID:    tap.DefaultDemoAgentKeyID,
		CreatedAt:     now,
	}

	if err := d.Store.SaveOrder(ctx, conf, "sim_"+uuid.NewString()); err != nil {
		log.Error().Err(err).Msg("failed to save simulated order")
		http.Error(w, "failed to save order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(conf)
}

// HandleGetTapKey serves public key directory at GET /sandbox/tap/keys/{keyid}.
func (d *Deps) HandleGetTapKey(w http.ResponseWriter, r *http.Request) {
	keyID := mux.Vars(r)["keyid"]
	pubKey, err := d.KeyDirectory.GetPublicKey(keyID)
	if err != nil {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keyid":      keyID,
		"alg":        "ed25519",
		"public_key": hex.EncodeToString(pubKey),
	})
}

// HandleHealthz returns service health.
func (d *Deps) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"service": "sidequestz-events",
		"sandbox": true,
	})
}

func (d *Deps) recordRejection(ctx context.Context, r *http.Request, statusCode int, reason string) {
	clientIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		clientIP = fwd
	}
	req := &models.RejectedRequest{
		ID:         uuid.NewString(),
		Method:     r.Method,
		Path:       r.URL.Path,
		StatusCode: statusCode,
		Reason:     reason,
		ClientIP:   clientIP,
		Timestamp:  d.Now().UTC(),
	}
	_ = d.Store.RecordRejectedRequest(ctx, req)
}

func writeJSONError(w http.ResponseWriter, status int, msg, declineReason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(models.APIError{
		Code:          status,
		Message:       msg,
		DeclineReason: declineReason,
	})
}

func extractLast4(token string) string {
	digits := ""
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] >= '0' && token[i] <= '9' {
			digits = string(token[i]) + digits
			if len(digits) == 4 {
				return digits
			}
		}
	}
	if digits != "" {
		return digits
	}
	return "1881"
}
