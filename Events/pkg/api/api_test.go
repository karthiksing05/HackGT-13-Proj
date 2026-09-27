package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"events/pkg/api"
	"events/pkg/config"
	"events/pkg/models"
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
		AppEnv:            "test",
		HTTPAddr:          ":8081",
		MerchantHost:      "events.sidequestz.tech",
		MerchantBaseURL:   "http://localhost:8081",
		PaymentsMode:      "sandbox",
		DemoKey:           "test-demo-key",
		SandboxNetworkKey: "test-network-key",
	}
	memStore := store.NewMemoryStore()
	memStore.SeedDefaultEvents()

	deps := api.NewDeps(cfg, memStore)
	h := router.New(deps)
	return h, deps
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
	h, deps := setupTestServer(t)
	now := time.Now().UTC()

	// 1. Get Offer (signed with agent-browser-auth)
	offerReq := httptest.NewRequest(http.MethodGet, "/api/events/sunset-jazz-on-pier-nine/offer?quantity=2", nil)
	offerReq.Host = "events.sidequestz.tech"
	if err := tap.Sign(offerReq, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-browser-auth", "events.sidequestz.tech", now); err != nil {
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
	if quote.QuoteID == "" || quote.Quantity != 2 || quote.TotalCents != 2710 {
		t.Fatalf("unexpected quote values: %+v", quote)
	}

	// 2. Place Order (signed with agent-payer-auth)
	orderPayload := models.OrderRequest{
		QuoteID:            quote.QuoteID,
		Quantity:           2,
		ExpectedTotalCents: quote.TotalCents,
		Payment: models.PaymentCredential{
			Scheme:        "visa_agent_token",
			InstructionID: "sbx_ins_demo123",
			Token:         "sbx_vtok_test_1881",
			Cryptogram:    "sbx_cgm_test_4567",
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
	if err := tap.Sign(orderReq, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now); err != nil {
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
	if conf.Status != "confirmed" || !conf.Sandbox || conf.TotalCents != 2710 {
		t.Fatalf("unexpected order confirmation: %+v", conf)
	}
	if conf.Ticket.TicketID == "" || conf.Payment.Last4 != "1881" {
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
	if !strings.Contains(bodyStr, conf.ConfirmationCode) || !strings.Contains(bodyStr, "Paid with Visa agent token") {
		t.Fatalf("ticket HTML missing confirmation code or payment note")
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
			Scheme:     "visa_agent_token",
			Token:      "sbx_vtok_idem_1",
			Cryptogram: "sbx_cgm_idem_1",
		},
		Buyer: models.BuyerInfo{Name: "Sandy", Email: "demo@sidequestz.tech"},
	}
	b, _ := json.Marshal(payload)

	// First attempt
	req1 := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	req1.Host = "events.sidequestz.tech"
	req1.Header.Set("Idempotency-Key", "idem-key-100")
	req1.Header.Set("Content-Type", "application/json")
	_ = tap.Sign(req1, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)
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
	_ = tap.Sign(req2, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now.Add(time.Second))
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
				Scheme:     "visa_agent_token",
				Token:      token,
				Cryptogram: "sbx_cgm_test",
			},
			Buyer: models.BuyerInfo{Name: "Buyer", Email: "b@example.com"},
		}
		b, _ := json.Marshal(p)
		r := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
		r.Host = "events.sidequestz.tech"
		r.Header.Set("Idempotency-Key", idem)
		r.Header.Set("Content-Type", "application/json")
		_ = tap.Sign(r, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		codes[idx] = w.Code
	}

	wg.Add(2)
	go makeOrder(0, "q_last_1", "sbx_vtok_conc_1", "idem-conc-1")
	go makeOrder(1, "q_last_2", "sbx_vtok_conc_2", "idem-conc-2")
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

	// Send an invalid token that the Visa network will decline
	payload := models.OrderRequest{
		QuoteID:            "q_decline_test",
		Quantity:           2,
		ExpectedTotalCents: 1200,
		Payment: models.PaymentCredential{
			Scheme:     "visa_agent_token",
			Token:      "invalid_prefix_token", // Will decline
			Cryptogram: "sbx_cgm_test",
		},
		Buyer: models.BuyerInfo{Name: "Buyer", Email: "test@example.com"},
	}
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(b))
	r.Host = "events.sidequestz.tech"
	r.Header.Set("Idempotency-Key", "idem-decline-1")
	r.Header.Set("Content-Type", "application/json")
	_ = tap.Sign(r, deps.DemoPrivKey, tap.DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now)

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
	if !strings.Contains(w2.Body.String(), `rel="agent-checkout"`) {
		t.Fatalf("tickets page missing rel=agent-checkout link")
	}

	// 3. Ticket pass page /t/{ticket_id}
	order := &models.OrderConfirmation{
		OrderID:          "SL-TEST1",
		ConfirmationCode: "SL-TEST1",
		Status:           "confirmed",
		Quantity:         2,
		TotalCents:       2710,
		Event: models.EventSummary{
			Title:    "Sunset Jazz",
			Venue:    "Pier Nine",
			StartsAt: time.Now().Format(time.RFC3339),
		},
		Buyer: models.BuyerInfo{Name: "Sandy Byte"},
		Ticket: models.TicketSummary{
			TicketID:  "tkt_valid_123",
			TicketURL: "http://localhost:8081/t/tkt_valid_123",
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
