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

	// Agentic checkout runs only (omitted on simulated intents).
	RunID              *string `json:"run_id,omitempty"`
	Merchant           *string `json:"merchant,omitempty"`
	CheckoutURL        *string `json:"checkout_url,omitempty"`
	MaxAuthorizedCents *int    `json:"max_authorized_cents,omitempty"`
	FinalCents         *int    `json:"final_cents,omitempty"`
	FailureCode        *string `json:"failure_code,omitempty"`
	OrderRef           *string `json:"order_ref,omitempty"`
	Confirmation       *string `json:"confirmation,omitempty"`
	TicketURL          *string `json:"ticket_url,omitempty"`
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

// CheckoutPlanItem is one paid stop the agent can buy tickets for.
type CheckoutPlanItem struct {
	ItemID       string  `json:"item_id"`
	Title        string  `json:"title"`
	Start        Time    `json:"start"`
	Merchant     string  `json:"merchant"`
	TicketURL    string  `json:"ticket_url"`
	PriceCents   *int    `json:"price_cents,omitempty"` // per ticket, before fees; null = unknown
	Quantity     int     `json:"quantity"`              // suggested
	Booked       bool    `json:"booked"`                // the viewer already has a ticket
	IntentState  *string `json:"intent_state,omitempty"`
	Confirmation *string `json:"confirmation,omitempty"`
}

// CheckoutPlan is GET /itineraries/{id}/checkout: what agentic checkout would
// buy for the viewer, and with what.
type CheckoutPlan struct {
	ItineraryID          string             `json:"itinerary_id"`
	Available            bool               `json:"available"`        // the server can run agentic checkout
	AgenticCheckout      bool               `json:"agentic_checkout"` // the user switched it on
	Items                []CheckoutPlanItem `json:"items"`
	EstimateCents        int                `json:"estimate_cents"` // tickets before fees
	DefaultBudgetCents   int                `json:"default_budget_cents"`
	SuggestedBudgetCents int                `json:"suggested_budget_cents"`
	PaymentMethodID      *string            `json:"payment_method_id,omitempty"`
	CardBrand            *string            `json:"card_brand,omitempty"`
	CardLast4            *string            `json:"card_last4,omitempty"`
	ActiveRunID          *string            `json:"active_run_id,omitempty"`
}

// CheckoutRunItem is one item to buy and how many tickets.
type CheckoutRunItem struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

// CreateCheckoutRun is POST /itineraries/{id}/checkout-runs: the buyer's one
// approval (after Face ID in the app) covering every listed item.
type CreateCheckoutRun struct {
	BudgetCents     int               `json:"budget_cents"`
	Items           []CheckoutRunItem `json:"items"`
	PaymentMethodID *string           `json:"payment_method_id"`
}

// CheckoutRun is an agentic checkout: the approved budget, what went through,
// and every item's intent (with its confirmation and ticket once booked).
type CheckoutRun struct {
	ID          string           `json:"id"`
	ItineraryID string           `json:"itinerary_id"`
	State       CheckoutRunState `json:"state"`
	BudgetCents int              `json:"budget_cents"`
	SpentCents  int              `json:"spent_cents"`
	Currency    string           `json:"currency"`
	CardBrand   string           `json:"card_brand"`
	CardLast4   string           `json:"card_last4"`
	Agent       *string          `json:"agent,omitempty"` // muse | fallback
	Summary     *string          `json:"summary,omitempty"`
	Intents     []CheckoutIntent `json:"intents"`
	CreatedAt   Time             `json:"created_at"`
	FinishedAt  *Time            `json:"finished_at,omitempty"`
}
