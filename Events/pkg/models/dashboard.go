package models

import "time"

// Scenarios supported for the demo booth.
const (
	ScenarioNormal    = "normal"
	ScenarioSoldOut   = "sold_out"
	ScenarioPriceBump = "price_bump"
	ScenarioSlow      = "slow"
)

// ScenarioState tracks the current active scenario.
type ScenarioState struct {
	Scenario string `json:"scenario"`
}

// RejectedRequest records a failed or unauthorized attempt for the booth dashboard.
type RejectedRequest struct {
	ID         string    `json:"id" bson:"_id"`
	Method     string    `json:"method" bson:"method"`
	Path       string    `json:"path" bson:"path"`
	StatusCode int       `json:"status_code" bson:"statusCode"`
	Reason     string    `json:"reason" bson:"reason"`
	ClientIP   string    `json:"client_ip" bson:"clientIp"`
	Timestamp  time.Time `json:"timestamp" bson:"timestamp"`
}

// DashboardFeedItem is an item in the live order stream.
type DashboardFeedItem struct {
	OrderID               string    `json:"order_id"`
	EventTitle            string    `json:"event_title"`
	EventSlug             string    `json:"event_slug"`
	Quantity              int       `json:"quantity"`
	TotalCents            int       `json:"total_cents"`
	AgentSigned           bool      `json:"agent_signed"`
	AgentKeyID            string    `json:"agent_key_id"`
	CardLast4             string    `json:"card_last4"`
	InstructionID         string    `json:"instruction_id"`
	InstructionLimitCents int       `json:"instruction_limit_cents"`
	TicketURL             string    `json:"ticket_url"`
	CreatedAt             time.Time `json:"created_at"`
}

// DashboardFeed represents the payload for live feed polling.
type DashboardFeed struct {
	Scenario        string              `json:"scenario"`
	Orders          []DashboardFeedItem `json:"orders"`
	Rejected        []RejectedRequest   `json:"rejected"`
	TotalOrders     int                 `json:"total_orders"`
	TotalGrossCents int                 `json:"total_gross_cents"`
	ActiveEvents    int                 `json:"active_events"`
}
