// Package payments charges an order through Stripe test mode with a Shared
// Payment Token (SPT) the buyer's agent granted to this merchant. The token
// carries the agent's limits (max amount, currency, expiry); Stripe enforces
// them, so a charge above the limit fails at Stripe, not in our code.
package payments

import (
	"context"
	"errors"
	"fmt"
)

// Decline reasons (the decline_reason of a 402 from POST /api/orders).
const (
	ReasonOverLimit      = "over_limit"      // more than the token's max_amount
	ReasonExpired        = "expired"         // the token's expiry passed
	ReasonUsed           = "used"            // the token was spent or revoked
	ReasonUnknownToken   = "unknown_token"   // no such token for this merchant
	ReasonCardDeclined   = "card_declined"   // the card behind the token declined
	ReasonRequiresAction = "requires_action" // the buyer must authenticate (3-D Secure)
)

// ChargeRequest is one charge against a granted token.
type ChargeRequest struct {
	SPT            string
	AmountCents    int
	Currency       string
	IdempotencyKey string
	Description    string
	Metadata       map[string]string
}

// ChargeResult is what a successful charge reports: never the token itself.
type ChargeResult struct {
	PaymentIntentID string
	Brand           string
	Last4           string
	LimitCents      int // the token's max_amount
}

// DeclineError is a charge the payment layer refused.
type DeclineError struct {
	Reason  string // one of the Reason constants
	Code    string // the provider's own code, for logs
	Message string
}

func (e *DeclineError) Error() string {
	return fmt.Sprintf("payment declined: %s (%s: %s)", e.Reason, e.Code, e.Message)
}

// ErrNotConfigured is returned when no Stripe key is set.
var ErrNotConfigured = errors.New("payments: STRIPE_SECRET_KEY is not set")

// Charger charges orders.
type Charger interface {
	Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
}

// Unconfigured is the Charger when no Stripe key is set: every charge fails
// with ErrNotConfigured (the order answers 503, nothing is reserved for long).
type Unconfigured struct{}

// Charge implements Charger.
func (Unconfigured) Charge(context.Context, ChargeRequest) (*ChargeResult, error) {
	return nil, ErrNotConfigured
}
