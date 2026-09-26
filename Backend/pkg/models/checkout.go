package models

import "time"

// Checkout states as stored (the contract CheckoutIntent.state values).
const (
	CheckoutPreparing        = "preparing"
	CheckoutAwaitingApproval = "awaiting_approval"
	CheckoutProcessing       = "processing"
	CheckoutBooked           = "booked"
	CheckoutCancelled        = "cancelled"
	CheckoutFailed           = "failed"
)

// CheckoutStep is one line of the agent's progress list.
type CheckoutStep struct {
	Text string `bson:"text"`
	Done bool   `bson:"done"`
}

// CheckoutIntent is one simulated ticket purchase (checkout_intents), driven
// by the checkout agent through preparing → awaiting_approval → processing →
// booked.
type CheckoutIntent struct {
	ID               string         `bson:"_id"` // uuid
	UserID           string         `bson:"userId"`
	ItemID           string         `bson:"itemId"`
	ItineraryID      string         `bson:"itineraryId"`
	ItemTitle        string         `bson:"itemTitle"`
	Quantity         int            `bson:"quantity"`
	PaymentMethodID  *string        `bson:"paymentMethodId,omitempty"`
	CardBrand        string         `bson:"cardBrand"`
	CardLast4        string         `bson:"cardLast4"`
	State            string         `bson:"state"`
	Steps            []CheckoutStep `bson:"steps"`
	SubtotalCents    *int           `bson:"subtotalCents,omitempty"`
	FeesCents        *int           `bson:"feesCents,omitempty"`
	TotalCents       *int           `bson:"totalCents,omitempty"`
	Instant          bool           `bson:"instant"`
	FailureReason    *string        `bson:"failureReason,omitempty"`
	NextTransitionAt *time.Time     `bson:"nextTransitionAt,omitempty"`
	TicketID         string         `bson:"ticketId,omitempty"`
	Confirmation     string         `bson:"confirmation,omitempty"`
	CreatedAt        time.Time      `bson:"createdAt"`
	UpdatedAt        time.Time      `bson:"updatedAt"`
}

// PaymentMethod is a simulated saved card (payment_methods). No card number
// is ever stored.
type PaymentMethod struct {
	ID        string    `bson:"_id"` // uuid
	UserID    string    `bson:"userId"`
	Brand     string    `bson:"brand"` // Visa | Mastercard | Amex …
	Last4     string    `bson:"last4"`
	IsDefault bool      `bson:"isDefault"`
	DemoToken string    `bson:"demoToken"`
	CreatedAt time.Time `bson:"createdAt"`
}

// Device is a registered push token (devices). Push itself is not sent.
type Device struct {
	ID        string    `bson:"_id"` // uuid
	UserID    string    `bson:"userId"`
	Token     string    `bson:"token"`
	Platform  string    `bson:"platform"`
	CreatedAt time.Time `bson:"createdAt"`
}
