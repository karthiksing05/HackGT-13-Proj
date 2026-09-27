package models

import "time"

// PaymentCredential contains the Visa agent token and cryptogram.
type PaymentCredential struct {
	Scheme        string `json:"scheme"`
	InstructionID string `json:"instruction_id"`
	Token         string `json:"token"`
	Cryptogram    string `json:"cryptogram"`
}

// BuyerInfo identifies the attendee.
type BuyerInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
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

// PaymentSummary contains payment details returned in order confirmation without sensitive tokens.
type PaymentSummary struct {
	Scheme string `json:"scheme" bson:"scheme"`
	Last4  string `json:"last4" bson:"last4"`
	AuthID string `json:"auth_id" bson:"authId"`
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
	InstructionID  string    `json:"-" bson:"instructionId"`
	AgentKeyID     string    `json:"-" bson:"agentKeyId"`
	CreatedAt      time.Time `json:"-" bson:"createdAt"`
}

// APIError represents a structured error returned by the merchant API.
type APIError struct {
	Code          int    `json:"code"`
	Message       string `json:"message"`
	DeclineReason string `json:"decline_reason,omitempty"`
	NewTotalCents int    `json:"new_total_cents,omitempty"`
	NewQuoteID    string `json:"new_quote_id,omitempty"`
}
