package contract

type CheckoutStep struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type CheckoutIntent struct {
	ID              string         `json:"id"`
	ItemID          string         `json:"item_id"`
	ItemTitle       string         `json:"item_title"`
	Steps           []CheckoutStep `json:"steps"`
	SubtotalCents   *int           `json:"subtotal_cents,omitempty"`
	FeesCents       *int           `json:"fees_cents,omitempty"`
	TotalCents      *int           `json:"total_cents,omitempty"`
	CardBrand       string         `json:"card_brand"`
	CardLast4       string         `json:"card_last4"`
	State           CheckoutState  `json:"state"`
	Quantity        int            `json:"quantity"`
	PaymentMethodID *string        `json:"payment_method_id,omitempty"`
	FailureReason   *string        `json:"failure_reason,omitempty"`
	Instant         bool           `json:"instant"`
}

type CreateCheckoutIntent struct {
	ItemID          string  `json:"item_id"`
	Quantity        int     `json:"quantity"`
	PaymentMethodID *string `json:"payment_method_id"`
	Instant         bool    `json:"instant"`
}

// CheckoutPatch is PATCH /checkout/intents/{id} (Checkout › "Change").
type CheckoutPatch struct {
	PaymentMethodID *string `json:"payment_method_id"`
}
