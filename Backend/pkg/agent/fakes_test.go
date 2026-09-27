package agent_test

import (
	"Backend/pkg/agent"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/muse"
	"Backend/pkg/payments"
	"Backend/pkg/store"
	"Backend/pkg/tap"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const merchantHost = "events.sidequestz.tech"

// ---- a fake merchant ----------------------------------------------------------

// fakeMerchant is the Events contract in miniature: signed quotes and
// orders, the booth scenarios, and Stripe's token limits (a charge above the
// issued token's max_amount is declined).
type fakeMerchant struct {
	t        *testing.T
	srv      *httptest.Server
	issuer   *payments.Fake
	keys     *tap.InMemoryKeyDirectory
	mu       sync.Mutex
	prices   map[string]int // slug → unit cents
	left     map[string]int
	scenario string
	quotes   map[string]agent.Quote
	orders   map[string]agent.Confirmation // by Idempotency-Key
	bumped   map[string]bool
	nonces   map[string]bool
	n        int
	page     string // extra text on ticket pages
}

func newFakeMerchant(t *testing.T, issuer *payments.Fake) *fakeMerchant {
	keys, _, _ := tap.NewDemoKeyDirectory()
	m := &fakeMerchant{
		t: t, issuer: issuer, keys: keys, prices: map[string]int{}, left: map[string]int{},
		quotes: map[string]agent.Quote{}, orders: map[string]agent.Confirmation{}, bumped: map[string]bool{}, nonces: map[string]bool{},
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *fakeMerchant) add(slug string, unit, seats int) {
	m.prices[slug], m.left[slug] = unit, seats
}

func (m *fakeMerchant) CheckAndRecordNonce(nonce string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nonces[nonce] {
		return fmt.Errorf("replayed")
	}
	m.nonces[nonce] = true
	return nil
}

func (m *fakeMerchant) fail(w http.ResponseWriter, status int, code string, extra map[string]any) {
	body := map[string]any{"code": code, "message": code}
	for k, v := range extra {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (m *fakeMerchant) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/tickets") && r.Method == http.MethodGet:
		slug := strings.Split(strings.Trim(path, "/"), "/")[0]
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>%s tickets</title><script type="application/ld+json">{"@type":"Event","name":%q}</script>`+
			`<link rel="agent-checkout" href="/api/events/%s/offer"></head><body><h1>%s</h1><p>%s</p></body></html>`, slug, slug, slug, slug, m.page)
		return
	case strings.HasPrefix(path, "/api/events/") && strings.HasSuffix(path, "/offer"):
		if _, err := tap.Verify(r, m.keys, m, "agent-browser-auth", merchantHost, time.Now()); err != nil {
			m.fail(w, 401, "bad_signature", nil)
			return
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(path, "/api/events/"), "/offer")
		var qty int
		fmt.Sscan(r.URL.Query().Get("quantity"), &qty)
		m.mu.Lock()
		defer m.mu.Unlock()
		unit, ok := m.prices[slug]
		if !ok {
			m.fail(w, 404, "not_found", nil)
			return
		}
		if m.scenario == "sold_out" || m.left[slug] < qty {
			m.fail(w, 409, "sold_out", nil)
			return
		}
		m.n++
		q := agent.Quote{QuoteID: fmt.Sprintf("q_%d", m.n), Quantity: qty, Available: m.left[slug], Currency: "usd",
			UnitCents: unit, SubtotalCents: unit * qty, FeesCents: unit*qty*8/100 + 50*qty}
		q.TotalCents = q.SubtotalCents + q.FeesCents
		q.Event.Slug, q.Event.Title = slug, slug
		m.quotes[q.QuoteID] = q
		_ = json.NewEncoder(w).Encode(q)
		return
	case path == "/api/orders" && r.Method == http.MethodPost:
		m.order(w, r)
		return
	}
	m.fail(w, 404, "not_found", nil)
}

func (m *fakeMerchant) order(w http.ResponseWriter, r *http.Request) {
	if _, err := tap.Verify(r, m.keys, m, "agent-payer-auth", merchantHost, time.Now()); err != nil {
		m.fail(w, 401, "bad_signature", nil)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	var req agent.OrderRequest
	if key == "" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Payment.Scheme != "stripe_spt" || !strings.HasPrefix(req.Payment.Token, "spt_") {
		m.fail(w, 400, "bad_request", nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.orders[key]; ok {
		_ = json.NewEncoder(w).Encode(c)
		return
	}
	q, ok := m.quotes[req.QuoteID]
	if !ok {
		m.fail(w, 409, "quote_expired", nil)
		return
	}
	if req.Quantity != q.Quantity {
		m.fail(w, 409, "quantity_mismatch", nil)
		return
	}
	if m.scenario == "price_bump" && !m.bumped[q.Event.Slug] {
		m.bumped[q.Event.Slug] = true
		m.n++
		nq := q
		nq.QuoteID = fmt.Sprintf("q_%d", m.n)
		nq.TotalCents = q.TotalCents * 14 / 10
		nq.FeesCents = nq.TotalCents - nq.SubtotalCents
		m.quotes[nq.QuoteID] = nq
		m.fail(w, 409, "price_changed", map[string]any{"total_cents": nq.TotalCents, "quote_id": nq.QuoteID})
		return
	}
	if req.ExpectedTotalCents != q.TotalCents {
		m.fail(w, 409, "price_changed", map[string]any{"total_cents": q.TotalCents, "quote_id": q.QuoteID})
		return
	}
	amount := q.TotalCents
	if m.scenario == "overcharge" {
		amount = q.TotalCents * 125 / 100
	}
	// Stripe's side: the token's max_amount holds whatever the merchant asks.
	if limit, ok := m.tokenLimit(req.Payment.Token); !ok || amount > limit {
		m.fail(w, 402, "declined", map[string]any{"decline_reason": "over_limit"})
		return
	}
	m.left[q.Event.Slug] -= q.Quantity
	var c agent.Confirmation
	c.OrderID = fmt.Sprintf("SL-%05d", len(m.orders)+1)
	c.ConfirmationCode, c.Status, c.Quantity, c.Currency = c.OrderID, "confirmed", q.Quantity, "usd"
	c.SubtotalCents, c.FeesCents, c.TotalCents = q.SubtotalCents, amount-q.SubtotalCents, amount
	c.Ticket.TicketID, c.Ticket.Admit = "tkt-"+c.OrderID, q.Quantity
	c.Ticket.TicketURL = "https://" + merchantHost + "/t/tkt-" + c.OrderID
	m.orders[key] = c
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(c)
}

// tokenLimit is the max_amount the fake issuer gave a token.
func (m *fakeMerchant) tokenLimit(spt string) (int, bool) {
	for i, tok := range m.issuer.Tokens {
		if tok == spt {
			return m.issuer.Issued[i].MaxCents, true
		}
	}
	return 0, false
}

// ---- a scripted Muse ----------------------------------------------------------

// scriptedMuse plays Muse: each turn it sees the previous tool outputs and
// returns the calls next(outputs) decides.
type scriptedMuse struct {
	mu    sync.Mutex
	turns int
	next  func(turn int, outputs map[string]map[string]any) []muse.Item
	err   error
}

func (s *scriptedMuse) Create(_ context.Context, req muse.Request) (*muse.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	outputs := map[string]map[string]any{}
	for _, in := range req.Input {
		if out, ok := in.(muse.FunctionCallOutput); ok {
			var v map[string]any
			_ = json.Unmarshal([]byte(out.Output), &v)
			outputs[out.CallID] = v
		}
	}
	s.turns++
	return &muse.Response{ID: fmt.Sprintf("resp_%d", s.turns), Status: "completed", Output: s.next(s.turns, outputs)}, nil
}

func call(id, name string, args map[string]string) muse.Item {
	b, _ := json.Marshal(args)
	return muse.Item{Type: "function_call", Name: name, CallID: id, Arguments: string(b)}
}

// ---- the API around it -----------------------------------------------------

type harness struct {
	srv      *testutil.Server
	merchant *fakeMerchant
	issuer   *payments.Fake
	runner   *agent.Runner
	sess     *testutil.Session
	itin     *models.Itinerary
	items    []string // stop item ids, in order
}

// newHarness builds the API with a runner against the fake merchant, a
// user with a Visa 4242, and a plan with one stop per slug (prices in cents).
func newHarness(t *testing.T, model agent.Model, slugs map[string]int, order ...string) *harness {
	t.Helper()
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) {
		c.PaymentsMode, c.StripeSecretKey, c.StripeSellerProfile = "sandbox", "sk_test_agent", "profile_test_seller"
		c.MerchantHost, c.MerchantBaseURL = merchantHost, "https://"+merchantHost
	}))
	issuer := &payments.Fake{}
	fm := newFakeMerchant(t, issuer)
	_, priv := tap.DefaultKeyPair()
	runner, err := agent.New(context.Background(), srv.Deps, agent.Options{
		Issuer: issuer, Merchant: agent.NewMerchant(fm.srv.URL, merchantHost, priv, nil), Model: model,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Deps.Runner = runner
	h := &harness{srv: srv, merchant: fm, issuer: issuer, runner: runner}
	h.sess = srv.Signup(t, "Sandy Byte")
	card := &models.PaymentMethod{ID: store.NewID(), UserID: h.sess.UserID, Brand: "Visa", Last4: "4242", IsDefault: true, CreatedAt: time.Now().UTC()}
	if _, err := srv.Store.Collection(store.CollPaymentMethods).InsertOne(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	start := srv.Clock.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	h.itin = &models.Itinerary{
		ID: store.NewID(), HostID: h.sess.UserID, MemberIDs: []string{h.sess.UserID}, Title: "Evening out",
		DateKey: start.Format("2006-01-02"), TZ: testutil.TimeZone, Date: start.Truncate(24 * time.Hour), Start: start, BackBy: start.Add(6 * time.Hour),
		Visibility: models.VisibilityJustMe, Status: models.ItineraryActive, CreatedAt: start, UpdatedAt: start,
	}
	for i, slug := range order {
		unit := slugs[slug]
		fm.add(slug, unit, 60)
		ticket := "https://" + merchantHost + "/" + slug + "/tickets"
		id := store.NewID()
		h.items = append(h.items, id)
		h.itin.Items = append(h.itin.Items, models.ItineraryItem{
			ID: id, Kind: models.ItemStop, Title: slug, Start: start.Add(time.Duration(i) * time.Hour), End: start.Add(time.Duration(i)*time.Hour + 50*time.Minute),
			Bookable: true, PriceCents: &unit, TicketURL: &ticket,
		})
	}
	if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(context.Background(), h.itin); err != nil {
		t.Fatal(err)
	}
	return h
}

// start creates a run for every stop (quantity each) and waits for it.
func (h *harness) start(t *testing.T, budget int, quantity int) contract.CheckoutRun {
	t.Helper()
	req := contract.CreateCheckoutRun{BudgetCents: budget}
	for _, id := range h.items {
		req.Items = append(req.Items, contract.CheckoutRunItem{ItemID: id, Quantity: quantity})
	}
	var run contract.CheckoutRun
	h.srv.Do(t, "POST", "/itineraries/"+h.itin.ID+"/checkout-runs", req, h.sess).Expect(t, http.StatusCreated).JSON(t, &run)
	h.runner.Wait()
	var done contract.CheckoutRun
	h.srv.Do(t, "GET", "/checkout/runs/"+run.ID, nil, h.sess).Expect(t, http.StatusOK).JSON(t, &done)
	return done
}

func (h *harness) transcript(t *testing.T, runID string) string {
	t.Helper()
	run, err := h.srv.Store.CheckoutRuns().ByID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(run.Transcript)
	return string(b)
}
