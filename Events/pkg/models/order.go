package models

import "time"

// PaymentSchemeStripeSPT is the only payment scheme the merchant accepts: a
// Stripe Shared Payment Token granted to this merchant for one purchase.
const PaymentSchemeStripeSPT = "stripe_spt"

// PaymentCredential is the payment part of an order: the scheme and the
// Stripe Shared Payment Token (spt_…). The merchant never sees card data.
type PaymentCredential struct {
	Scheme string `json:"scheme"`
	Token  string `json:"token"`
}

// BuyerInfo identifies the attendee.
type BuyerInfo struct {
	Name  string `json:"name" bson:"name"`
	Email string `json:"email" bson:"email"`
}

// OrderRequest is the payload sent to POST /api/orders.
type OrderRequest struct {
	QuoteID            string            `json:"quote_id"`
	Quantity           int               `json:"quantity"`
	ExpectedTotalCents int               `json:"expected_total_cents"`
	Payment            PaymentCredential `json:"payment"`
	Buyer              BuyerInfo         `json:"buyer"`
}

// TicketSummary contains ticket information embedded in order confirmations.
type TicketSummary struct {
	TicketID  string `json:"ticket_id" bson:"ticketId"`
	TicketURL string `json:"ticket_url" bson:"ticketUrl"`
	Admit     int    `json:"admit" bson:"admit"`
	Barcode   string `json:"barcode" bson:"barcode"`
}

// PaymentSummary is what the order records about the payment: never the
// token itself. LimitCents is the token's max_amount, the most the buyer's
// agent let this merchant charge.
type PaymentSummary struct {
	Scheme          string `json:"scheme" bson:"scheme"`
	Brand           string `json:"brand" bson:"brand"`
	Last4           string `json:"last4" bson:"last4"`
	PaymentIntentID string `json:"payment_intent_id" bson:"paymentIntentId"`
	LimitCents      int    `json:"limit_cents" bson:"limitCents"`
}

// OrderConfirmation is returned upon successful order placement (201 Created).
type OrderConfirmation struct {
	OrderID          string         `json:"order_id" bson:"orderId"`
	ConfirmationCode string         `json:"confirmation_code" bson:"confirmationCode"`
	Status           string         `json:"status" bson:"status"`
	Sandbox          bool           `json:"sandbox" bson:"sandbox"`
	Event            EventSummary   `json:"event" bson:"event"`
	Quantity         int            `json:"quantity" bson:"quantity"`
	Currency         string         `json:"currency" bson:"currency"`
	SubtotalCents    int            `json:"subtotal_cents" bson:"subtotalCents"`
	FeesCents        int            `json:"fees_cents" bson:"feesCents"`
	TotalCents       int            `json:"total_cents" bson:"totalCents"`
	Buyer            BuyerInfo      `json:"buyer" bson:"buyer"`
	Ticket           TicketSummary  `json:"ticket" bson:"ticket"`
	Payment          PaymentSummary `json:"payment" bson:"payment"`

	// Internal metadata
	IdempotencyKey string    `json:"-" bson:"idempotencyKey"`
	AgentKeyID     string    `json:"-" bson:"agentKeyId"`
	CreatedAt      time.Time `json:"-" bson:"createdAt"`
}

// APIError is the merchant API's error body: a machine code from the shared
// contract (sold_out, price_changed, quote_expired, declined, bad_signature,
// not_found, …) and a sentence for people.
type APIError struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	DeclineReason string `json:"decline_reason,omitempty"`
	TotalCents    int    `json:"total_cents,omitempty"` // price_changed: the new total
	QuoteID       string `json:"quote_id,omitempty"`    // price_changed: the new quote
}
