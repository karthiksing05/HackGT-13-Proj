package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// StripeAPIVersion is the preview API version the SPT endpoints need.
const StripeAPIVersion = "2026-04-22.preview"

// DefaultStripeBase is Stripe's API host.
const DefaultStripeBase = "https://api.stripe.com"

// Stripe charges granted SPTs with a test-mode secret key. It is a small
// form-encoded client: the SPT endpoints are preview-only and not typed in
// stripe-go.
type Stripe struct {
	key  string
	base string
	http *http.Client
}

// NewStripe builds a charger for secretKey (sk_test_… / rk_test_…) against
// base (DefaultStripeBase outside tests).
func NewStripe(secretKey, base string) *Stripe {
	if base == "" {
		base = DefaultStripeBase
	}
	return &Stripe{key: secretKey, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 20 * time.Second}}
}

// stripeError is Stripe's error envelope.
type stripeError struct {
	Error struct {
		Type        string `json:"type"`
		Code        string `json:"code"`
		DeclineCode string `json:"decline_code"`
		Message     string `json:"message"`
	} `json:"error"`
}

type grantedToken struct {
	ID                string `json:"id"`
	DeactivatedAt     *int64 `json:"deactivated_at"`
	DeactivatedReason string `json:"deactivated_reason"`
	UsageLimits       struct {
		Currency  string `json:"currency"`
		ExpiresAt int64  `json:"expires_at"`
		MaxAmount int    `json:"max_amount"`
	} `json:"usage_limits"`
}

type paymentIntent struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	PaymentMethod *struct {
		Card *struct {
			Brand string `json:"brand"`
			Last4 string `json:"last4"`
		} `json:"card"`
	} `json:"payment_method"`
}

// Charge reads the token's limits, then creates and confirms a PaymentIntent
// with it. Stripe applies the token's limits to the charge.
func (s *Stripe) Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error) {
	var tok grantedToken
	if err := s.request(ctx, StripeAPIVersion, http.MethodGet, "/v1/shared_payment/granted_tokens/"+url.PathEscape(req.SPT), nil, "", &tok); err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("amount", strconv.Itoa(req.AmountCents))
	form.Set("currency", req.Currency)
	form.Set("confirm", "true")
	form.Set("payment_method_data[shared_payment_granted_token]", req.SPT)
	form.Add("expand[]", "payment_method")
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", v)
	}
	var pi paymentIntent
	if err := s.request(ctx, StripeAPIVersion, http.MethodPost, "/v1/payment_intents", form, req.IdempotencyKey, &pi); err != nil {
		return nil, err
	}
	switch pi.Status {
	case "succeeded", "processing":
	case "requires_action":
		return nil, &DeclineError{Reason: ReasonRequiresAction, Code: pi.Status, Message: "the buyer must authenticate the payment"}
	default:
		return nil, &DeclineError{Reason: ReasonCardDeclined, Code: pi.Status, Message: "the payment did not go through"}
	}
	res := &ChargeResult{PaymentIntentID: pi.ID, LimitCents: tok.UsageLimits.MaxAmount}
	if pi.PaymentMethod != nil && pi.PaymentMethod.Card != nil {
		res.Brand, res.Last4 = pi.PaymentMethod.Card.Brand, pi.PaymentMethod.Card.Last4
	}
	return res, nil
}

// request sends one request, pinned to version ("" = the account's default).
// A Stripe error becomes a *DeclineError when it is about the token or the
// card, and a plain error otherwise.
func (s *Stripe) request(ctx context.Context, version, method, path string, form url.Values, idemKey string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.key, "")
	if version != "" {
		req.Header.Set("Stripe-Version", version)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("stripe %s %s: read: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		var se stripeError
		_ = json.Unmarshal(raw, &se)
		if d := classify(resp.StatusCode, se); d != nil {
			return d
		}
		return fmt.Errorf("stripe %s %s: %d %s: %s", method, path, resp.StatusCode, se.Error.Code, se.Error.Message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("stripe %s %s: decode: %w", method, path, err)
	}
	return nil
}

// classify maps a Stripe error to a decline reason, or nil when the error is
// ours (auth, bad request shape) rather than the buyer's token or card.
// Stripe's test mode (API 2026-04-22.preview) answers these without a code,
// so the messages decide (seen with Backend/scripts/stripe-spt-smoke.sh):
//
//	over the limit: "The requested amount is greater than the remaining
//	                 amount capturable with this shared payment granted token."
//	spent/revoked:  "The shared payment granted token cannot be used because
//	                 it is already in a deactivated state."
func classify(status int, se stripeError) *DeclineError {
	e := se.Error
	d := &DeclineError{Code: firstNonEmpty(e.DeclineCode, e.Code, e.Type), Message: e.Message}
	msg := strings.ToLower(e.Message)
	code := strings.ToLower(e.Code + " " + e.DeclineCode)
	switch {
	case status == http.StatusNotFound || e.Code == "resource_missing":
		d.Reason = ReasonUnknownToken
	case strings.Contains(code, "expired") || strings.Contains(msg, "expired"):
		d.Reason = ReasonExpired
	case strings.Contains(msg, "greater than the remaining amount") || strings.Contains(msg, "remaining amount capturable") ||
		strings.Contains(msg, "max_amount") || strings.Contains(msg, "usage limit") ||
		strings.Contains(msg, "exceeds") || strings.Contains(code, "amount_too_large") || strings.Contains(code, "limit"):
		d.Reason = ReasonOverLimit
	case strings.Contains(msg, "already been used") || strings.Contains(msg, "deactivated") ||
		strings.Contains(msg, "revoked") || strings.Contains(msg, "consumed") || strings.Contains(code, "used"):
		d.Reason = ReasonUsed
	case e.Type == "card_error" || status == http.StatusPaymentRequired:
		d.Reason = ReasonCardDeclined
	default:
		return nil
	}
	return d
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
