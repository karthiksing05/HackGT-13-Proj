package payments

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SessionRequest is a hosted checkout for tickets bought in a browser: the
// buyer pays on Stripe's page, then comes back to SuccessURL.
type SessionRequest struct {
	Title       string // line item name ("Sunset Jazz on Pier Nine")
	UnitCents   int
	Quantity    int
	FeesCents   int
	Currency    string
	Email       string
	SuccessURL  string // must contain {CHECKOUT_SESSION_ID}
	CancelURL   string
	Metadata    map[string]string
	Description string
}

// Session is a hosted checkout as Stripe reports it.
type Session struct {
	ID              string
	URL             string // where to send the buyer
	Paid            bool
	AmountTotal     int
	Email           string
	PaymentIntentID string
	Brand           string
	Last4           string
	Metadata        map[string]string
}

// Checkout runs hosted checkouts for browser purchases.
type Checkout interface {
	CreateSession(ctx context.Context, req SessionRequest) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
}

// CreateSession implements Checkout.
func (Unconfigured) CreateSession(context.Context, SessionRequest) (*Session, error) {
	return nil, ErrNotConfigured
}

// GetSession implements Checkout.
func (Unconfigured) GetSession(context.Context, string) (*Session, error) {
	return nil, ErrNotConfigured
}

type checkoutSession struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"`
	PaymentStatus string            `json:"payment_status"`
	AmountTotal   int               `json:"amount_total"`
	Metadata      map[string]string `json:"metadata"`
	Customer      *struct {
		Email string `json:"email"`
	} `json:"customer_details"`
	PaymentIntent *struct {
		ID            string `json:"id"`
		PaymentMethod *struct {
			Card *struct {
				Brand string `json:"brand"`
				Last4 string `json:"last4"`
			} `json:"card"`
		} `json:"payment_method"`
	} `json:"payment_intent"`
}

func (c *checkoutSession) session() *Session {
	s := &Session{ID: c.ID, URL: c.URL, Paid: c.PaymentStatus == "paid", AmountTotal: c.AmountTotal, Metadata: c.Metadata}
	if c.Customer != nil {
		s.Email = c.Customer.Email
	}
	if pi := c.PaymentIntent; pi != nil {
		s.PaymentIntentID = pi.ID
		if pi.PaymentMethod != nil && pi.PaymentMethod.Card != nil {
			s.Brand, s.Last4 = pi.PaymentMethod.Card.Brand, pi.PaymentMethod.Card.Last4
		}
	}
	return s
}

// CreateSession creates a Stripe Checkout Session (mode payment): the tickets
// and the service fee as two line items.
func (s *Stripe) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) {
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("managed_payments[enabled]", "false")
	form.Set("success_url", req.SuccessURL)
	form.Set("cancel_url", req.CancelURL)
	if req.Email != "" {
		form.Set("customer_email", req.Email)
	}
	line := func(i int, name string, cents, qty int) {
		p := "line_items[" + strconv.Itoa(i) + "]"
		form.Set(p+"[price_data][currency]", req.Currency)
		form.Set(p+"[price_data][product_data][name]", name)
		form.Set(p+"[price_data][unit_amount]", strconv.Itoa(cents))
		form.Set(p+"[quantity]", strconv.Itoa(qty))
	}
	line(0, req.Title, req.UnitCents, req.Quantity)
	if req.FeesCents > 0 {
		line(1, "Service fee", req.FeesCents, 1)
	}
	if req.Description != "" {
		form.Set("payment_intent_data[description]", req.Description)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", v)
	}
	var cs checkoutSession
	if err := s.request(ctx, "", http.MethodPost, "/v1/checkout/sessions", form, "", &cs); err != nil {
		return nil, err
	}
	return cs.session(), nil
}

// GetSession reads a Checkout Session with its payment's card.
func (s *Stripe) GetSession(ctx context.Context, id string) (*Session, error) {
	var cs checkoutSession
	path := "/v1/checkout/sessions/" + url.PathEscape(id) + "?expand[]=payment_intent.payment_method"
	if err := s.request(ctx, "", http.MethodGet, path, nil, "", &cs); err != nil {
		return nil, err
	}
	return cs.session(), nil
}
