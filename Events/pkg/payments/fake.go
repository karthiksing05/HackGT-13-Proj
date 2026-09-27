package payments

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FakeToken is a granted token the Fake knows about.
type FakeToken struct {
	MaxCents  int
	ExpiresAt time.Time
	Brand     string
	Last4     string
	Decline   bool // the card behind it declines
}

// Fake is an in-memory Charger for tests. It applies the same limits Stripe
// applies to an SPT: max amount, expiry and single use.
type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	tokens   map[string]*FakeToken
	used     map[string]string // spt → payment intent id
	byKey    map[string]*ChargeResult
	sessions map[string]*fakeSession
	n        int
}

// NewFake builds an empty Fake on the given clock (time.Now when nil).
func NewFake(now func() time.Time) *Fake {
	if now == nil {
		now = time.Now
	}
	return &Fake{now: now, tokens: map[string]*FakeToken{}, used: map[string]string{}, byKey: map[string]*ChargeResult{}}
}

// Grant registers a token.
func (f *Fake) Grant(spt string, t FakeToken) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.Brand == "" {
		t.Brand = "visa"
	}
	if t.Last4 == "" {
		t.Last4 = "4242"
	}
	if t.ExpiresAt.IsZero() {
		t.ExpiresAt = f.now().Add(10 * time.Minute)
	}
	f.tokens[spt] = &t
}

// Charge implements Charger.
func (f *Fake) Charge(_ context.Context, req ChargeRequest) (*ChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.IdempotencyKey != "" {
		if res, ok := f.byKey[req.IdempotencyKey]; ok {
			return res, nil
		}
	}
	t, ok := f.tokens[req.SPT]
	switch {
	case !ok:
		return nil, &DeclineError{Reason: ReasonUnknownToken, Code: "resource_missing"}
	case f.used[req.SPT] != "":
		return nil, &DeclineError{Reason: ReasonUsed, Code: "token_used"}
	case !f.now().Before(t.ExpiresAt):
		return nil, &DeclineError{Reason: ReasonExpired, Code: "token_expired"}
	case req.AmountCents > t.MaxCents:
		return nil, &DeclineError{Reason: ReasonOverLimit, Code: "amount_exceeds_max_amount"}
	case t.Decline:
		return nil, &DeclineError{Reason: ReasonCardDeclined, Code: "card_declined"}
	}
	f.n++
	res := &ChargeResult{PaymentIntentID: fmt.Sprintf("pi_fake_%d", f.n), Brand: t.Brand, Last4: t.Last4, LimitCents: t.MaxCents}
	f.used[req.SPT] = res.PaymentIntentID
	if req.IdempotencyKey != "" {
		f.byKey[req.IdempotencyKey] = res
	}
	return res, nil
}

// fakeSession is a hosted checkout the Fake created.
type fakeSession struct {
	req  SessionRequest
	paid bool
}

// CreateSession implements Checkout: the session's URL is a stand-in for
// Stripe's page; call Pay to play the buyer finishing it.
func (f *Fake) CreateSession(_ context.Context, req SessionRequest) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]*fakeSession{}
	}
	f.n++
	id := fmt.Sprintf("cs_test_fake_%d", f.n)
	f.sessions[id] = &fakeSession{req: req}
	return &Session{ID: id, URL: "https://checkout.stripe.test/c/pay/" + id, Metadata: req.Metadata}, nil
}

// Pay marks a session paid, as if the buyer completed Stripe's page.
func (f *Fake) Pay(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.sessions[id]; s != nil {
		s.paid = true
	}
}

// GetSession implements Checkout.
func (f *Fake) GetSession(_ context.Context, id string) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	if s == nil {
		return nil, &DeclineError{Reason: ReasonUnknownToken, Code: "resource_missing"}
	}
	out := &Session{ID: id, Paid: s.paid, AmountTotal: s.req.UnitCents*s.req.Quantity + s.req.FeesCents,
		Email: s.req.Email, Metadata: s.req.Metadata}
	if s.paid {
		out.PaymentIntentID, out.Brand, out.Last4 = "pi_fake_"+id, "visa", "4242"
	}
	return out, nil
}
