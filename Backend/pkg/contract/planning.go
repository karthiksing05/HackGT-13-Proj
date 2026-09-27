package contract

// Wire shapes of the Create flow. Required keys mirror Planning.swift; the
// fields marked additive are optional extras the planner may fill
// (docs/design/planner.md §6.2) and the app decodes leniently.

type PlanRequest struct {
	Start     Place       `json:"start"`
	End       Place       `json:"end"`
	Date      Time        `json:"date"`
	StartTime Time        `json:"start_time"`
	BackBy    Time        `json:"back_by"`
	Range     TravelRange `json:"range"`
	Ride      RideChoice  `json:"ride"`
	OpenSeats *int        `json:"open_seats,omitempty"`
	MoodText  string      `json:"mood_text"`
	Tags      []string    `json:"tags"`
	Budget    int         `json:"budget"` // 0 Free, 1 $, 2 $$, 3 $$$
	Who       Visibility  `json:"who"`
	Pace      Pace        `json:"pace"`
	Modes     TravelModes `json:"modes"`
	// MustInclude (additive) are the must-see picks: catalog activity ids
	// (ActivityHit.id) every option contains; at most 10, left out when
	// empty.
	MustInclude []string `json:"must_include,omitempty"`
}

// ActivityHit is one result of GET /activities/search: an event or place
// of the user's catalog that Create's must-see search offers as a pick.
type ActivityHit struct {
	ID       string       `json:"id"` // the catalog _id, the planner's activity_id
	Title    string       `json:"title"`
	Kind     PlanStopKind `json:"kind"`
	Category string       `json:"category"`
	// Subtitle is one display line: "Live music · 6:30 PM · 0.7 mi".
	Subtitle string `json:"subtitle"`
	Place    Place  `json:"place"`
	Start    *Time  `json:"start,omitempty"` // events only
	End      *Time  `json:"end,omitempty"`   // events only, when known
	// PriceCents is left out when the price is unknown.
	PriceCents *int `json:"price_cents,omitempty"`
	// DistanceMi is from the request's near point, left out without one.
	DistanceMi *float64 `json:"distance_mi,omitempty"`
}

type PlanStop struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle"`
	Place           Place  `json:"place"`
	DurationMinutes int    `json:"duration_minutes"`
	// Additive planner extras.
	ArriveTime *Time        `json:"arrive_time,omitempty"`
	DepartTime *Time        `json:"depart_time,omitempty"`
	Kind       PlanStopKind `json:"kind,omitempty"`
	Flexible   *bool        `json:"flexible,omitempty"`
	ActivityID *string      `json:"activity_id,omitempty"`
}

type PlanOption struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Tag   string     `json:"tag"`
	Meta  string     `json:"meta"`
	Stops []PlanStop `json:"stops"`
	// LateFlag marks an option that gets back after back_by; always present.
	LateFlag       bool `json:"late_flag"`
	TotalCostCents *int `json:"total_cost_cents,omitempty"`
}

type PlanBatch struct {
	Options []PlanOption `json:"options"`
	Cursor  *string      `json:"cursor,omitempty"`
	Done    bool         `json:"done"`
	// Reason is set only with empty options: no_candidates_fit_window,
	// no_feasible_itinerary, "invalid_request: …", and for must-see picks
	// "must_include_unavailable: <title>" or must_include_no_fit.
	Reason *string `json:"reason,omitempty"`
}

// MoreRequest is POST /plans/generate/more.
type MoreRequest struct {
	Cursor string `json:"cursor"`
}

type PlanAlternative struct {
	Stop   PlanStop `json:"stop"`
	Reason string   `json:"reason"`
}

type Leg struct {
	Mode    TravelMode `json:"mode"`
	Minutes int        `json:"minutes"`
}

type StopWindow struct {
	Start Time `json:"start"`
	End   Time `json:"end"`
}

type RouteRequest struct {
	OptionID  string      `json:"option_id"`
	StopOrder []string    `json:"stop_order"`
	Start     Place       `json:"start"`
	End       Place       `json:"end"`
	StartTime Time        `json:"start_time"`
	BackBy    Time        `json:"back_by"`
	Ride      RideChoice  `json:"ride"`
	Modes     TravelModes `json:"modes"`
}

type RouteResult struct {
	Legs        []Leg        `json:"legs"`
	StopTimes   []StopWindow `json:"stop_times"`
	Arrival     Time         `json:"arrival"`
	MinutesLate int          `json:"minutes_late"`
	// BrokenAt is the index of the first stop reached late, -1 for none
	// (always present; builders set -1 explicitly).
	BrokenAt int `json:"broken_at"`
}

type AlternativesRequest struct {
	OptionID  string   `json:"option_id"`
	StopID    string   `json:"stop_id"`
	StopOrder []string `json:"stop_order"`
}

type CreateItineraryRequest struct {
	Plan         PlanRequest `json:"plan"`
	Option       PlanOption  `json:"option"`
	StopOrder    []string    `json:"stop_order"`
	Route        RouteResult `json:"route"`
	Visibility   Visibility  `json:"visibility"`
	LockAt       *Time       `json:"lock_at,omitempty"`
	MaxGroupSize *int        `json:"max_group_size,omitempty"`
}
