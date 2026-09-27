package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"events/pkg/api"
	"events/pkg/config"
	"events/pkg/models"
	"events/pkg/payments"
	"events/pkg/router"
	"events/pkg/store"
	"events/pkg/tap"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupTestServer(t *testing.T) (http.Handler, *api.Deps) {
	t.Helper()
	cfg := &config.Config{
		AppEnv:          "test",
		HTTPAddr:        ":8085",
		MerchantHost:    "events.sidequestz.tech",
		MerchantBaseURL: "http://localhost:8085",
		PaymentsMode:    "sandbox",
		DemoKey:         "test-demo-key",
	}
	memStore := store.NewMemoryStore()
	memStore.SeedDefaultEvents()

	deps, err := api.NewDeps(cfg, memStore)
	if err != nil {
		t.Fatal(err)
	}
	fake := payments.NewFake(nil)
	for _, spt := range []string{"spt_happy", "spt_idem", "spt_conc_1", "spt_conc_2", "spt_bump", "spt_over"} {
		fake.Grant(spt, payments.FakeToken{MaxCents: 100000})
	}
	fake.Grant("spt_declined", payments.FakeToken{MaxCents: 100000, Decline: true})
	deps.Charger = fake
	deps.Checkout = fake
	h := router.New(deps)
	return h, deps
}

// demoPriv is the demo agent key the test server trusts (APP_ENV=test).
func demoPriv() ed25519.PrivateKey {
	_, priv := tap.DefaultKeyPair()
	return priv
}

func TestSignatureRejection(t *testing.T) {
	h, _ := setupTestServer(t)

	// 1. Unsigned GET offer must return 401
	req1 := httptest.NewRequest(http.MethodGet, "/api/events/sunset-jazz-on-pier-nine/offer?quantity=2", nil)
	req1.Host = "events.sidequestz.tech"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unsigned offer, got %d", w1.Code)
	}

	// 2. Unsigned POST order must return 401
	req2 := httptest.NewRequest(http.MethodPost, "/api/orders", strings.NewReader(`{}`))
	req2.Host = "events.sidequestz.tech"
	req2.Header.Set("Idempotency-Key", "test-idem-1")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unsigned order, got %d", w2.Code)
	}
}

func TestGetOfferAndOrderHappyPath(t *testing.T) {
	h, _ := setupTestServer(t)
	now := time.Now().UTC()

	// 1. Get Offer (signed with agent-browser-auth)
	offerReq := httptest.NewRequest(http.MethodGet, "/api/events/sunset-jazz-on-pier-nine/offer?quantity=2", nil)
	offerReq.Host = "events.sidequestz.tech"
	if err := tap.Sign(offerReq, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-browser-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatal(err)
	}
	wOffer := httptest.NewRecorder()
	h.ServeHTTP(wOffer, offerReq)

	if wOffer.Code != http.StatusOK {
		t.Fatalf("expected 200 on offer, got %d: %s", wOffer.Code, wOffer.Body.String())
	}

	var quote models.Quote
	if err := json.Unmarshal(wOffer.Body.Bytes(), &quote); err != nil {
		t.Fatal(err)
	}
	if quote.QuoteID == "" || quote.Quantity != 2 || quote.TotalCents != 2692 {
		t.Fatalf("unexpected quote values: %+v", quote)
	}

	// 2. Place Order (signed with agent-payer-auth)
	orderPayload := models.OrderRequest{
		QuoteID:            quote.QuoteID,
		Quantity:           2,
		ExpectedTotalCents: quote.TotalCents,
		Payment: models.PaymentCredential{
			Scheme: "stripe_spt",
			Token:  "spt_happy",
		},
		Buyer: models.BuyerInfo{
			Name:  "Sandy Byte",
			Email: "demo@sidequestz.tech",
		},
	}
	bodyBytes, _ := json.Marshal(orderPayload)

	orderReq := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(bodyBytes))
	orderReq.Host = "events.sidequestz.tech"
	orderReq.Header.Set("Idempotency-Key", "intent-999")
	orderReq.Header.Set("Content-Type", "application/json")
	if err := tap.Sign(orderReq, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatal(err)
	}

	wOrder := httptest.NewRecorder()
	h.ServeHTTP(wOrder, orderReq)

	if wOrder.Code != http.StatusCreated {
		t.Fatalf("expected 201 on order, got %d: %s", wOrder.Code, wOrder.Body.String())
	}

	var conf models.OrderConfirmation
	if err := json.Unmarshal(wOrder.Body.Bytes(), &conf); err != nil {
		t.Fatal(err)
	}
	if conf.Payment.Scheme != "stripe_spt" || conf.Payment.PaymentIntentID == "" || conf.Payment.Last4 != "4242" || conf.Payment.LimitCents != 100000 {
		t.Fatalf("payment summary = %+v", conf.Payment)
	}
	if strings.Contains(wOrder.Body.String(), "spt_happy") {
		t.Fatal("the confirmation echoes the payment token")
	}
	if conf.Status != "confirmed" || !conf.Sandbox || conf.TotalCents != 2692 {
		t.Fatalf("unexpected order confirmation: %+v", conf)
	}
	if conf.Ticket.TicketID == "" || conf.Payment.Last4 != "4242" {
		t.Fatalf("missing ticket ID or last4: %+v", conf)
	}

	// 3. GET /t/{ticket_id} with Accept: application/json
	tktReq := httptest.NewRequest(http.MethodGet, "/t/"+conf.Ticket.TicketID, nil)
	tktReq.Header.Set("Accept", "application/json")
	wTkt := httptest.NewRecorder()
	h.ServeHTTP(wTkt, tktReq)

	if wTkt.Code != http.StatusOK {
		t.Fatalf("expected 200 on ticket JSON, got %d", wTkt.Code)
	}
	var tkt models.TicketSummary
	if err := json.Unmarshal(wTkt.Body.Bytes(), &tkt); err != nil || tkt.TicketID != conf.Ticket.TicketID {
		t.Fatalf("invalid ticket JSON: %v", err)
	}

	// 4. GET /t/{ticket_id} as HTML
	tktHTMLReq := httptest.NewRequest(http.MethodGet, "/t/"+conf.Ticket.TicketID, nil)
	wTktHTML := httptest.NewRecorder()
	h.ServeHTTP(wTktHTML, tktHTMLReq)
	if wTktHTML.Code != http.StatusOK {
		t.Fatalf("expected 200 on ticket HTML, got %d", wTktHTML.Code)
	}
	bodyStr := wTktHTML.Body.String()
	if !strings.Contains(bodyStr, conf.ConfirmationCode) || !strings.Contains(bodyStr, "Visa •••• 4242") {
		t.Fatalf("ticket HTML missing confirmation code or card")
	}
}

func TestIdempotentReplay(t *testing.T) {
	h, deps := setupTestServer(t)
	now := time.Now().UTC()

	// Create quote first
	q := &models.Quote{
		QuoteID:        "q_idem_test",
		Event:          models.EventSummary{Slug: "sunset-jazz-on-pier-nine", Title: "Sunset Jazz"},
		QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       1,
		TotalCents:     1350,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
	_ = deps.Store.SaveQuote(context.Background(), q)

	payload := models.OrderRequest{
		QuoteID:            "q_idem_test",
		Quantity:           1,
		ExpectedTotalCents: 1350,
		Payment: models.PaymentCredential{
			Scheme: "stripe_spt",
			Token:  "spt_idem",
		},
		Buyer: models.BuyerInfo{Name: "Sandy", Email: "demo@sidequestz.tech"},
	}
	b, _ := json.Marshal(payload)

	// First attempt
	req1 := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	req1.Host = "events.sidequestz.tech"
	req1.Header.Set("Idempotency-Key", "idem-key-100")
	req1.Header.Set("Content-Type", "application/json")
	_ = tap.Sign(req1, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first order failed: %d: %s", w1.Code, w1.Body.String())
	}
	var firstConf models.OrderConfirmation
	_ = json.Unmarshal(w1.Body.Bytes(), &firstConf)

	// Replay attempt with same idempotency key
	req2 := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	req2.Host = "events.sidequestz.tech"
	req2.Header.Set("Idempotency-Key", "idem-key-100")
	req2.Header.Set("Content-Type", "application/json")
	_ = tap.Sign(req2, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now.Add(time.Second))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK && w2.Code != http.StatusCreated {
		t.Fatalf("replay failed: %d", w2.Code)
	}
	var replayConf models.OrderConfirmation
	_ = json.Unmarshal(w2.Body.Bytes(), &replayConf)
	if replayConf.OrderID != firstConf.OrderID {
		t.Fatalf("replay returned different order ID: %s vs %s", replayConf.OrderID, firstConf.OrderID)
	}
}

func TestConcurrentOrdersOnLastSeat(t *testing.T) {
	h, deps := setupTestServer(t)
	now := time.Now().UTC()

	slug := "barnacle-bash-silent-disco"
	// Set remaining to exactly 1 ticket
	_ = deps.Store.ReserveTickets(context.Background(), slug, 59) // 60 - 59 = 1 remaining

	// Prepare two quotes for 1 ticket
	q1 := &models.Quote{
		QuoteID:        "q_last_1",
		Event:          models.EventSummary{Slug: slug, Title: "Barnacle Bash"},
		QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       1,
		TotalCents:     1670,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
	q2 := &models.Quote{
		QuoteID:        "q_last_2",
		Event:          models.EventSummary{Slug: slug, Title: "Barnacle Bash"},
		QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       1,
		TotalCents:     1670,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
	_ = deps.Store.SaveQuote(context.Background(), q1)
	_ = deps.Store.SaveQuote(context.Background(), q2)

	var wg sync.WaitGroup
	codes := make([]int, 2)

	makeOrder := func(idx int, quoteID, token, idem string) {
		defer wg.Done()
		p := models.OrderRequest{
			QuoteID:            quoteID,
			Quantity:           1,
			ExpectedTotalCents: 1670,
			Payment: models.PaymentCredential{
				Scheme: "stripe_spt",
				Token:  token,
			},
			Buyer: models.BuyerInfo{Name: "Buyer", Email: "b@example.com"},
		}
		b, _ := json.Marshal(p)
		r := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
		r.Host = "events.sidequestz.tech"
		r.Header.Set("Idempotency-Key", idem)
		r.Header.Set("Content-Type", "application/json")
		_ = tap.Sign(r, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		codes[idx] = w.Code
	}

	wg.Add(2)
	go makeOrder(0, "q_last_1", "spt_conc_1", "idem-conc-1")
	go makeOrder(1, "q_last_2", "spt_conc_2", "idem-conc-2")
	wg.Wait()

	has201 := (codes[0] == http.StatusCreated || codes[1] == http.StatusCreated)
	has409 := (codes[0] == http.StatusConflict || codes[1] == http.StatusConflict)

	if !has201 || !has409 {
		t.Fatalf("expected exactly one 201 Created and one 409 Conflict, got codes: %v", codes)
	}
}

func TestInventoryReleasedOnDecline(t *testing.T) {
	h, deps := setupTestServer(t)
	now := time.Now().UTC()

	slug := "shipyard-robot-regatta"
	event, _ := deps.Store.GetEvent(context.Background(), slug)
	initialRemaining := event.Remaining

	q := &models.Quote{
		QuoteID:        "q_decline_test",
		Event:          event.SummaryView(),
		QuoteExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
		Quantity:       2,
		TotalCents:     1200,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
	_ = deps.Store.SaveQuote(context.Background(), q)

	// A token whose card declines
	payload := models.OrderRequest{
		QuoteID:            "q_decline_test",
		Quantity:           2,
		ExpectedTotalCents: 1200,
		Payment: models.PaymentCredential{
			Scheme: "stripe_spt",
			Token:  "spt_declined", // the card behind it declines
		},
		Buyer: models.BuyerInfo{Name: "Buyer", Email: "test@example.com"},
	}
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	r.Host = "events.sidequestz.tech"
	r.Header.Set("Idempotency-Key", "idem-decline-1")
	r.Header.Set("Content-Type", "application/json")
	_ = tap.Sign(r, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 declined, got %d: %s", w.Code, w.Body.String())
	}

	// Inventory must be fully released back
	afterEvent, _ := deps.Store.GetEvent(context.Background(), slug)
	if afterEvent.Remaining != initialRemaining {
		t.Fatalf("expected remaining tickets %d, got %d", initialRemaining, afterEvent.Remaining)
	}
}

func TestJSONLDValidity(t *testing.T) {
	h, deps := setupTestServer(t)

	// 1. Event page /{slug}
	r1 := httptest.NewRequest(http.MethodGet, "/sunset-jazz-on-pier-nine", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 on event page, got %d", w1.Code)
	}
	extractAndValidateJSONLD(t, w1.Body.String(), "Event")

	// 2. Tickets page /{slug}/tickets
	r2 := httptest.NewRequest(http.MethodGet, "/sunset-jazz-on-pier-nine/tickets", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on tickets page, got %d", w2.Code)
	}
	extractAndValidateJSONLD(t, w2.Body.String(), "Event")

	// 3. Ticket pass page /t/{ticket_id}
	order := &models.OrderConfirmation{
		OrderID:          "SL-TEST1",
		ConfirmationCode: "SL-TEST1",
		Status:           "confirmed",
		Quantity:         2,
		TotalCents:       2692,
		Event: models.EventSummary{
			Title:    "Sunset Jazz",
			Venue:    "Pier Nine",
			StartsAt: time.Now().Format(time.RFC3339),
		},
		Buyer: models.BuyerInfo{Name: "Sandy Byte"},
		Ticket: models.TicketSummary{
			TicketID:  "tkt_valid_123",
			TicketURL: "http://localhost:8085/t/tkt_valid_123",
			Barcode:   "SLT-1234-5678",
		},
		Payment: models.PaymentSummary{Last4: "1881"},
	}
	_ = deps.Store.SaveOrder(context.Background(), order, "idem_jsonld")

	r3 := httptest.NewRequest(http.MethodGet, "/t/tkt_valid_123", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 on ticket pass page, got %d", w3.Code)
	}
	extractAndValidateJSONLD(t, w3.Body.String(), "EventReservation")
}

func extractAndValidateJSONLD(t *testing.T, html string, expectedType string) {
	t.Helper()
	startTag := `<script type="application/ld+json">`
	endTag := `</script>`

	startIdx := strings.Index(html, startTag)
	if startIdx == -1 {
		t.Fatalf("missing JSON-LD script tag in HTML")
	}
	startIdx += len(startTag)
	endIdx := strings.Index(html[startIdx:], endTag)
	if endIdx == -1 {
		t.Fatalf("missing JSON-LD closing tag in HTML")
	}
	jsonStr := strings.TrimSpace(html[startIdx : startIdx+endIdx])

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("invalid JSON-LD syntax: %v\nJSON:\n%s", err, jsonStr)
	}
	if parsed["@type"] != expectedType {
		t.Fatalf("expected JSON-LD @type %q, got %q", expectedType, parsed["@type"])
	}
}

func TestReservedSlugChecks(t *testing.T) {
	h, _ := setupTestServer(t)

	reserved := []string{"api", "t", "dashboard", "_demo", "healthz"}
	for _, slug := range reserved {
		// Event detail on reserved slug must not return event 200
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%s/tickets", slug), nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == http.StatusOK {
			t.Errorf("reserved slug %q unexpectedly returned 200 for tickets page", slug)
		}
	}
}

// signedOrder posts body to /api/orders signed as the payer agent.
func signedOrder(t *testing.T, h http.Handler, idem string, body models.OrderRequest) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	r.Host = "events.sidequestz.tech"
	r.Header.Set("Idempotency-Key", idem)
	r.Header.Set("Content-Type", "application/json")
	if err := tap.Sign(r, demoPriv(), tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// saveQuote stores a live quote for 2 tickets to slug at total.
func saveQuote(t *testing.T, deps *api.Deps, id, slug string, unit int) *models.Quote {
	t.Helper()
	ev, err := deps.Store.GetEvent(context.Background(), slug)
	if err != nil {
		t.Fatal(err)
	}
	fees := models.CalculateFees(unit, 2)
	q := &models.Quote{
		QuoteID: id, Event: ev.SummaryView(), Quantity: 2, Currency: "usd",
		UnitCents: unit, SubtotalCents: unit * 2, FeesCents: fees, TotalCents: unit*2 + fees,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if err := deps.Store.SaveQuote(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestOverchargeIsDeclinedByThePaymentLayer(t *testing.T) {
	h, deps := setupTestServer(t)
	fake := deps.Charger.(*payments.Fake)
	slug := "sunset-jazz-on-pier-nine"
	q := saveQuote(t, deps, "q_over", slug, 1200)
	// The agent's token allows exactly the quoted total.
	fake.Grant("spt_exact", payments.FakeToken{MaxCents: q.TotalCents})
	deps.Store.SetScenario(context.Background(), models.ScenarioOvercharge)
	before, _ := deps.Store.GetEvent(context.Background(), slug)

	w := signedOrder(t, h, "idem-over", models.OrderRequest{
		QuoteID: q.QuoteID, Quantity: 2, ExpectedTotalCents: q.TotalCents,
		Payment: models.PaymentCredential{Scheme: "stripe_spt", Token: "spt_exact"},
		Buyer:   models.BuyerInfo{Name: "Sandy Byte", Email: "demo@sidequestz.tech"},
	})
	var e models.APIError
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if w.Code != http.StatusPaymentRequired || e.Code != "declined" || e.DeclineReason != payments.ReasonOverLimit {
		t.Fatalf("got %d %+v, want 402 declined/over_limit", w.Code, e)
	}
	after, _ := deps.Store.GetEvent(context.Background(), slug)
	if after.Remaining != before.Remaining {
		t.Fatalf("seats not released: %d → %d", before.Remaining, after.Remaining)
	}
}

func TestPriceBumpRepricesOnce(t *testing.T) {
	h, deps := setupTestServer(t)
	q := saveQuote(t, deps, "q_bump", "sunset-jazz-on-pier-nine", 1200)
	deps.Store.SetScenario(context.Background(), models.ScenarioPriceBump)
	order := models.OrderRequest{
		QuoteID: q.QuoteID, Quantity: 2, ExpectedTotalCents: q.TotalCents,
		Payment: models.PaymentCredential{Scheme: "stripe_spt", Token: "spt_bump"},
		Buyer:   models.BuyerInfo{Name: "Sandy Byte", Email: "demo@sidequestz.tech"},
	}
	w := signedOrder(t, h, "idem-bump-1", order)
	var e models.APIError
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if w.Code != http.StatusConflict || e.Code != "price_changed" || e.QuoteID == "" || e.TotalCents <= q.TotalCents {
		t.Fatalf("got %d %+v, want 409 price_changed with a higher total", w.Code, e)
	}
	// Ordering against the new quote goes through: the surge applies once.
	order.QuoteID, order.ExpectedTotalCents = e.QuoteID, e.TotalCents
	w = signedOrder(t, h, "idem-bump-2", order)
	if w.Code != http.StatusCreated {
		t.Fatalf("order on the repriced quote: %d %s", w.Code, w.Body.String())
	}
}

func TestOrderRejectsBadPaymentAndQuantity(t *testing.T) {
	h, deps := setupTestServer(t)
	q := saveQuote(t, deps, "q_bad", "sunset-jazz-on-pier-nine", 1200)
	base := models.OrderRequest{
		QuoteID: q.QuoteID, Quantity: 2, ExpectedTotalCents: q.TotalCents,
		Buyer: models.BuyerInfo{Name: "Sandy Byte", Email: "demo@sidequestz.tech"},
	}
	visa := base
	visa.Payment = models.PaymentCredential{Scheme: "visa_agent_token", Token: "sbx_vtok_1"}
	if w := signedOrder(t, h, "idem-bad-1", visa); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported_payment") {
		t.Fatalf("non-SPT payment: %d %s", w.Code, w.Body.String())
	}
	more := base
	more.Quantity = 5
	more.Payment = models.PaymentCredential{Scheme: "stripe_spt", Token: "spt_happy"}
	if w := signedOrder(t, h, "idem-bad-2", more); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "quantity_mismatch") {
		t.Fatalf("quantity above the quote: %d %s", w.Code, w.Body.String())
	}
}
