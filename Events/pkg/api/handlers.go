package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"events/pkg/models"
	"events/pkg/tap"
	"events/pkg/ui"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
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

// HandleHome renders the discovery page at / and /events.
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
	if !d.demoKeyOK(key) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Open the dashboard with ?key=<DEMO_KEY>.\n"))
		return
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

	// The quote is the list price; price_bump raises it at order time.
	unitCents := event.UnitCents
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
		MerchantID:     MerchantID,
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
	if !d.demoKeyOK(key) {
		writeJSONError(w, http.StatusUnauthorized, "bad_demo_key", "")
		return
	}

	var req models.ScenarioState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !models.ValidScenario(req.Scenario) {
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
	key := r.Header.Get("X-Demo-Key")
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if !d.demoKeyOK(key) {
		writeJSONError(w, http.StatusUnauthorized, "bad_demo_key", "")
		return
	}
	orders, _ := d.Store.ListRecentOrders(ctx, 20)
	rejected, _ := d.Store.ListRejectedRequests(ctx, 10)
	scenario := d.Store.GetScenario(ctx)
	totalOrders := d.Store.TotalOrders(ctx)
	totalGross := d.Store.TotalGrossCents(ctx)

	feedOrders := make([]models.DashboardFeedItem, len(orders))
	for i, o := range orders {
		feedOrders[i] = models.DashboardFeedItem{
			OrderID:         o.OrderID,
			EventTitle:      o.Event.Title,
			EventSlug:       o.Event.Slug,
			Quantity:        o.Quantity,
			TotalCents:      o.TotalCents,
			AgentSigned:     true,
			AgentKeyID:      o.AgentKeyID,
			CardBrand:       o.Payment.Brand,
			CardLast4:       o.Payment.Last4,
			LimitCents:      o.Payment.LimitCents,
			PaymentIntentID: o.Payment.PaymentIntentID,
			TicketURL:       o.Ticket.TicketURL,
			CreatedAt:       o.CreatedAt,
		}
	}

	feed := models.DashboardFeed{
		Scenario:        scenario,
		Orders:          feedOrders,
		Rejected:        rejected,
		TotalOrders:     totalOrders,
		TotalGrossCents: totalGross,
		ActiveEvents:    d.activeEvents(ctx),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(feed)
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

func writeJSONError(w http.ResponseWriter, status int, code, declineReason string) {
	writeAPIError(w, status, models.APIError{Code: code, Message: errorMessages[code], DeclineReason: declineReason})
}

func writeAPIError(w http.ResponseWriter, status int, e models.APIError) {
	if e.Message == "" {
		e.Message = errorMessages[e.Code]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// errorMessages are the sentences that go with each error code.
var errorMessages = map[string]string{
	"bad_signature":        "This request isn't signed by a trusted agent.",
	"bad_request":          "The request body isn't a valid order.",
	"missing_idempotency":  "Send an Idempotency-Key header with every order.",
	"idempotency_conflict": "This Idempotency-Key was already used for a different order.",
	"not_found":            "There's nothing here.",
	"sold_out":             "Those tickets are sold out.",
	"quote_expired":        "That quote expired. Ask for a new offer.",
	"price_changed":        "The price changed. Review the new total.",
	"quantity_mismatch":    "The quantity doesn't match the quote.",
	"unsupported_payment":  "Pay with a Stripe shared payment token (scheme stripe_spt).",
	"declined":             "The payment was declined.",
	"payments_unavailable": "Payments aren't set up on this merchant yet.",
	"bad_demo_key":         "That demo key isn't right.",
	"internal":             "Something went wrong on our side.",
}

// activeEvents counts the catalog for the dashboard.
func (d *Deps) activeEvents(ctx context.Context) int {
	events, err := d.Store.ListEvents(ctx, "", "")
	if err != nil {
		return 0
	}
	return len(events)
}
