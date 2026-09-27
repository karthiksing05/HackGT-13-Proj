// Package payments is the checkout agent's side of Stripe test mode: it
// issues a Shared Payment Token (SPT) for one purchase at one merchant,
// limited to the quoted total and a short expiry, and revokes it when the
// purchase does not go through. The merchant charges the token; Stripe
// enforces its limits. Nothing here ever holds a card number.
//
// Simulated: saved cards are Stripe's test cards (TestPaymentMethod), so
// every token draws on a test PaymentMethod and no money moves.
package payments

import (
	"context"
	"strings"
	"time"
)

// IssueRequest is one token for one purchase.
type IssueRequest struct {
	PaymentMethod  string    // pm_… (TestPaymentMethod)
	SellerProfile  string    // the merchant's Stripe profile (profile_…)
	MaxCents       int       // the quoted total: the merchant can't charge more
	Currency       string    // "usd"
	ExpiresAt      time.Time // after this the token is dead
	IdempotencyKey string    // one per purchase attempt
}

// Issuer issues and revokes SPTs.
type Issuer interface {
	Issue(ctx context.Context, req IssueRequest) (string, error)
	Revoke(ctx context.Context, spt string) error
}

// testPaymentMethods are Stripe's test PaymentMethods by card number suffix;
// a card with another suffix falls back to its brand's.
var testPaymentMethods = map[string]string{
	"4242": "pm_card_visa",
	"4444": "pm_card_mastercard",
	"5556": "pm_card_visa_debit",
	"0002": "pm_card_chargeDeclined",
	"0005": "pm_card_amex",
	"1117": "pm_card_discover",
}

var brandPaymentMethods = map[string]string{
	"visa":       "pm_card_visa",
	"mastercard": "pm_card_mastercard",
	"amex":       "pm_card_amex",
	"discover":   "pm_card_discover",
}

// TestPaymentMethod is the Stripe test PaymentMethod a saved sandbox card
// pays with: by its last four digits, else by brand, else Visa.
func TestPaymentMethod(brand, last4 string) string {
	if pm, ok := testPaymentMethods[last4]; ok {
		return pm
	}
	if pm, ok := brandPaymentMethods[strings.ToLower(strings.ReplaceAll(brand, " ", ""))]; ok {
		return pm
	}
	return "pm_card_visa"
}
