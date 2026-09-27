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

	// Agentic checkout (a CheckoutRun): who sells it, what the buyer let the
	// agent spend on it and what came back. Simulated intents leave these empty.
	RunID              string `bson:"runId,omitempty"`
	Provider           string `bson:"provider,omitempty"`    // simulated | agent_run
	Merchant           string `bson:"merchant,omitempty"`    // the ticket URL's host
	CheckoutURL        string `bson:"checkoutUrl,omitempty"` // the ticket page Muse starts from
	MaxAuthorizedCents *int   `bson:"maxAuthorizedCents,omitempty"`
	FinalCents         *int   `bson:"finalCents,omitempty"`
	FailureCode        string `bson:"failureCode,omitempty"`
	OrderRef           string `bson:"orderRef,omitempty"`
	TicketURL          string `bson:"ticketUrl,omitempty"` // the merchant's ticket page
}

// Checkout providers: the simulated timer agent, or a run bought by the
// checkout agent from a real (sandbox) merchant.
const (
	ProviderSimulated = "simulated"
	ProviderAgentRun  = "agent_run"
)

// Failure codes of a checkout intent (failed + failureCode).
const (
	FailureSoldOut       = "sold_out"
	FailurePriceChanged  = "price_changed"
	FailureOverBudget    = "over_budget"
	FailureDeclined      = "declined"
	FailureCardDeclined  = "card_declined"
	FailureMerchantError = "merchant_error"
	FailureAgentError    = "agent_error"
	FailureSkipped       = "skipped"
	FailureCancelled     = "cancelled"
)

// Checkout run states.
const (
	RunRunning   = "running"
	RunDone      = "done"
	RunCancelled = "cancelled"
)

// CheckoutRun is one agentic checkout (checkout_runs): the buyer approved
// budgetCents once, and the agent buys each listed item from its merchant,
// never spending more in total. reservedCents is what purchases in flight
// hold; spentCents what went through.
type CheckoutRun struct {
	ID              string     `bson:"_id"` // uuid
	UserID          string     `bson:"userId"`
	ItineraryID     string     `bson:"itineraryId"`
	State           string     `bson:"state"`
	BudgetCents     int        `bson:"budgetCents"`
	ReservedCents   int        `bson:"reservedCents"`
	SpentCents      int        `bson:"spentCents"`
	Currency        string     `bson:"currency"`
	IntentIDs       []string   `bson:"intentIds"`
	PaymentMethodID string     `bson:"paymentMethodId"`
	CardBrand       string     `bson:"cardBrand"`
	CardLast4       string     `bson:"cardLast4"`
	Agent           string     `bson:"agent,omitempty"` // muse | fallback
	Summary         string     `bson:"summary,omitempty"`
	AgentNote       string     `bson:"agentNote,omitempty"` // Muse's own closing words
	Transcript      []RunEvent `bson:"transcript,omitempty"`
	LeaseUntil      *time.Time `bson:"leaseUntil,omitempty"`
	CreatedAt       time.Time  `bson:"createdAt"`
	UpdatedAt       time.Time  `bson:"updatedAt"`
	FinishedAt      *time.Time `bson:"finishedAt,omitempty"`
}

// RunEvent is one line of a run's transcript: a model turn, a tool call or
// its result. It never holds payment credentials.
type RunEvent struct {
	At   time.Time `bson:"at"`
	Kind string    `bson:"kind"` // model | tool_call | tool_result | note
	Name string    `bson:"name,omitempty"`
	Text string    `bson:"text"`
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
