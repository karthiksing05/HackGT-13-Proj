package models

import "time"

// Scenarios supported for the demo booth.
const (
	ScenarioNormal    = "normal"
	ScenarioSoldOut   = "sold_out"
	ScenarioPriceBump = "price_bump"
	ScenarioSlow      = "slow"
	// ScenarioOvercharge makes the merchant try to charge 25% more than it
	// quoted. Stripe refuses: the charge is over the token's max_amount.
	ScenarioOvercharge = "overcharge"
)

// ValidScenario reports whether s is a scenario the booth can switch to.
func ValidScenario(s string) bool {
	switch s {
	case ScenarioNormal, ScenarioSoldOut, ScenarioPriceBump, ScenarioSlow, ScenarioOvercharge:
		return true
	}
	return false
}

// ScenarioState tracks the current active scenario.
type ScenarioState struct {
	Scenario string `json:"scenario"`
}

// RejectedRequest records a failed or unauthorized API request (logged, for debugging).
type RejectedRequest struct {
	ID         string    `json:"id" bson:"_id"`
	Method     string    `json:"method" bson:"method"`
	Path       string    `json:"path" bson:"path"`
	StatusCode int       `json:"status_code" bson:"statusCode"`
	Reason     string    `json:"reason" bson:"reason"`
	ClientIP   string    `json:"client_ip" bson:"clientIp"`
	Timestamp  time.Time `json:"timestamp" bson:"timestamp"`
}
