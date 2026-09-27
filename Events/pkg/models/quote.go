package models

import "time"

// Quote represents a price quote for a specific event and quantity.
type Quote struct {
	QuoteID        string       `json:"quote_id" bson:"quoteId"`
	Event          EventSummary `json:"event" bson:"event"`
	QuoteExpiresAt string       `json:"quote_expires_at" bson:"quoteExpiresAt"` // RFC 3339 UTC
	Quantity       int          `json:"quantity" bson:"quantity"`
	Available      int          `json:"available" bson:"available"`
	Currency       string       `json:"currency" bson:"currency"`
	UnitCents      int          `json:"unit_cents" bson:"unitCents"`
	SubtotalCents  int          `json:"subtotal_cents" bson:"subtotalCents"`
	FeesCents      int          `json:"fees_cents" bson:"feesCents"`
	TotalCents     int          `json:"total_cents" bson:"totalCents"`
	MerchantID     string       `json:"merchant_id" bson:"merchantId"`
	Sandbox        bool         `json:"sandbox" bson:"sandbox"`
	CreatedAt      time.Time    `json:"created_at" bson:"createdAt"`
	ExpiresAt      time.Time    `json:"-" bson:"expiresAt"`
	// Repriced marks a quote issued by a price_changed answer: the price_bump
	// scenario bumps a price once, so an order against this quote goes through.
	Repriced bool `json:"-" bson:"repriced"`
}

// CalculateFees is the service fee: 8% of the subtotal plus $0.50 per ticket.
func CalculateFees(unitCents, quantity int) int {
	return unitCents*quantity*8/100 + 50*quantity
}
