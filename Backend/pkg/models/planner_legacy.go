package models

import "time"

// Legacy planner shapes, moved unchanged from models.go. pkg/itinerary still
// takes PlanGenerateRequest and PlanStop; the planner agent (pkg/planner)
// replaces these types and this file goes away with them. Nothing new should
// depend on them.

type Place struct {
	ID             string   `bson:"_id" json:"id"`
	Name           string   `bson:"name" json:"name"`
	Address        string   `bson:"address" json:"address"`
	Lat            float64  `bson:"lat" json:"lat"`
	Lng            float64  `bson:"lng" json:"lng"`
	Category       string   `bson:"category" json:"category"`
	SuggestionChip string   `bson:"suggestionChip,omitempty" json:"suggestion_chip,omitempty"`
	Tags           []string `bson:"tags" json:"tags"`
}

type PlanGenerateRequest struct {
	StartLocation string   `json:"start_location"`
	EndLocation   string   `json:"end_location"`
	Date          string   `json:"date"`
	StartTime     string   `json:"start_time"`
	BackByTime    string   `json:"back_by_time"`
	RangeKm       float64  `json:"range_km"`
	RideChoice    string   `json:"ride_choice"`
	OpenSeats     int      `json:"open_seats"`
	MoodText      string   `json:"mood_text"`
	Tags          []string `json:"tags"`
	BudgetCents   int64    `json:"budget_cents"`
	WhosComing    []string `json:"whos_coming"`
	Pace          string   `json:"pace"`
	TravelModes   []string `json:"travel_modes"`
}

type PlanStop struct {
	ID                 string  `json:"id"`
	PlaceID            string  `json:"place_id"`
	Name               string  `json:"name"`
	Address            string  `json:"address"`
	Lat                float64 `json:"lat"`
	Lng                float64 `json:"lng"`
	Order              int     `json:"order"`
	DurationMin        int     `json:"duration_min"`
	EstimatedCostCents int64   `json:"estimated_cost_cents"`
	Notes              string  `json:"notes,omitempty"`
	// Scheduled visit, set by the itinerary optimizer. Nil for legacy plans.
	ArriveTime *time.Time `json:"arrive_time,omitempty"`
	DepartTime *time.Time `json:"depart_time,omitempty"`
	Kind       string     `json:"kind,omitempty"`     // "event" | "place"
	Flexible   bool       `json:"flexible,omitempty"` // true when the visit time can move (drop-ins, places)
}

type PlanLeg struct {
	FromStopID  string  `json:"from_stop_id"`
	ToStopID    string  `json:"to_stop_id"`
	Mode        string  `json:"mode"`
	DurationMin int     `json:"duration_min"`
	DistanceKm  float64 `json:"distance_km"`
}

type PlanOption struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	Stops            []PlanStop `json:"stops"`
	Legs             []PlanLeg  `json:"legs"`
	RouteSummary     string     `json:"route_summary"`
	TotalCostCents   int64      `json:"total_cost_cents"`
	TotalDurationMin int        `json:"total_duration_min"`
	LateFlag         bool       `json:"late_flag"`
	ArrivalTime      time.Time  `json:"arrival_time"`
	Cursor           string     `json:"cursor,omitempty"`
}

type PlanRouteRequest struct {
	OptionID  string   `json:"option_id"`
	StopOrder []string `json:"stop_order"`
	Ride      string   `json:"ride,omitempty"`
	Modes     []string `json:"modes,omitempty"`
}

type TransitOption struct {
	Mode          string    `bson:"mode" json:"mode"` // walk, marta, rideshare
	DurationMin   int       `bson:"durationMin" json:"duration_min"`
	DistanceKm    float64   `bson:"distanceKm" json:"distance_km"`
	CostCents     int64     `bson:"costCents" json:"cost_cents"`
	Summary       string    `bson:"summary" json:"summary"`
	DepartureTime time.Time `bson:"departureTime" json:"departure_time"`
	ArrivalTime   time.Time `bson:"arrivalTime" json:"arrival_time"`
}
