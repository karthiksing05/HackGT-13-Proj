package planner

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Rendered shapes. The JSON tags are the app's wire names (the required
// keys of the contract plus the additive fields of §6.2); the bson tags are
// how the pool persists them. The service layer copies these into the
// contract structs field by field.

// Option is one itinerary as the app shows it.
type Option struct {
	ID    string `bson:"id" json:"id"`
	Name  string `bson:"name" json:"name"`
	Tag   string `bson:"tag" json:"tag"`
	Meta  string `bson:"meta" json:"meta"`
	Stops []Stop `bson:"stops" json:"stops"`

	Summary          string      `bson:"summary" json:"summary,omitempty"`
	RouteSummary     string      `bson:"routeSummary" json:"route_summary,omitempty"`
	Legs             []Leg       `bson:"legs" json:"legs,omitempty"`
	TotalCostCents   int64       `bson:"totalCostCents" json:"total_cost_cents"`
	CostKnown        bool        `bson:"costKnown" json:"cost_known"`
	TotalDurationMin int         `bson:"totalDurationMin" json:"total_duration_min"`
	Depart           time.Time   `bson:"depart" json:"depart_time"`
	Arrival          time.Time   `bson:"arrival" json:"arrival_time"`
	LateFlag         bool        `bson:"lateFlag" json:"late_flag"`
	Score            float64     `bson:"score" json:"score"`
	Metrics          PlanMetrics `bson:"metrics" json:"metrics"`
}

// Stop is one visit. Its id is "stop_<activityId>_<i>" for an option's own
// stops and "alt_<activityId>_<slot>" for alternatives.
type Stop struct {
	ID              string `bson:"id" json:"id"`
	Title           string `bson:"title" json:"title"`
	Subtitle        string `bson:"subtitle" json:"subtitle"`
	Place           Place  `bson:"place" json:"place"`
	DurationMinutes int    `bson:"durationMin" json:"duration_minutes"`

	ActivityID string    `bson:"activityId" json:"activity_id,omitempty"`
	Kind       string    `bson:"kind" json:"kind,omitempty"`
	Category   string    `bson:"category" json:"category,omitempty"`
	Tags       []string  `bson:"tags" json:"tags,omitempty"`
	Arrive     time.Time `bson:"arrive" json:"arrive_time"`
	Depart     time.Time `bson:"depart" json:"depart_time"`
	Flexible   bool      `bson:"flexible" json:"flexible"`
	PriceCents *int64    `bson:"priceCents" json:"price_cents"` // null when unknown
	PriceKnown bool      `bson:"priceKnown" json:"price_known"`
	Address    string    `bson:"address" json:"address,omitempty"`
	WebsiteURL string    `bson:"url" json:"website_url,omitempty"`
	TicketURL  string    `bson:"ticketUrl" json:"ticket_url,omitempty"`
	ImageURL   string    `bson:"imageUrl" json:"image_url,omitempty"`
	Summary    string    `bson:"summary" json:"summary,omitempty"`
	Utility    float64   `bson:"utility" json:"utility"`
	Order      int       `bson:"order" json:"order"`
	SeriesKey  string    `bson:"seriesKey" json:"-"`
	Tier       int       `bson:"tier" json:"-"`
	TierKnown  bool      `bson:"tierKnown" json:"-"`
	// OpenSlots is when a flexible stop can be visited (a place's opening
	// hours, a drop-in's span) around the plan's window, so a reorder that
	// moves the visit outside them is caught. Empty for fixed events.
	OpenSlots []TimeSlot `bson:"openSlots,omitempty" json:"-"`
}

// Leg is one hop. Mode is always one of the app's values: walk, marta,
// drive, rideshare.
type Leg struct {
	FromStopID string  `bson:"from" json:"from_stop_id"`
	ToStopID   string  `bson:"to" json:"to_stop_id"`
	Mode       string  `bson:"mode" json:"mode"`
	Minutes    int     `bson:"minutes" json:"minutes"`
	DistanceKm float64 `bson:"distanceKm" json:"distance_km"`
}

// MarshalJSON writes the app's Place shape: {name, coordinate?{lat,lng}}.
func (p Place) MarshalJSON() ([]byte, error) {
	type coord struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	out := struct {
		Name       string `json:"name"`
		Coordinate *coord `json:"coordinate,omitempty"`
	}{Name: p.Name}
	if p.HasCoord {
		out.Coordinate = &coord{Lat: p.Lat, Lng: p.Lng}
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads the app's Place shape.
func (p *Place) UnmarshalJSON(b []byte) error {
	var in wirePlace
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	*p = in.place()
	return nil
}

// Batch is a page of options (§6.2 PlanBatch plus the additive fields).
type Batch struct {
	Options []Option `json:"options"`
	Cursor  string   `json:"cursor,omitempty"`
	Done    bool     `json:"done"`
	Planner string   `json:"planner,omitempty"`
	RunID   string   `json:"run_id,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Relaxed []string `json:"relaxed,omitempty"`
	Debug   *PlanRun `json:"debug,omitempty"`
}

// --- persisted documents ---------------------------------------------------

// PoolWindow is the plan window, enough to re-time a reorder later.
type PoolWindow struct {
	From        time.Time `bson:"from"`
	BackBy      time.Time `bson:"backBy"`
	TZ          string    `bson:"tz"`
	Start       Place     `bson:"start"`
	End         Place     `bson:"end"`
	Mode        string    `bson:"mode"`
	DriveLabel  string    `bson:"driveLabel"`
	MaxLegKm    float64   `bson:"maxLegKm"`
	BudgetCents int64     `bson:"budgetCents"`
	Pace        string    `bson:"pace"`
}

// PoolSpec is the part of the spec alternatives need.
type PoolSpec struct {
	City       string          `bson:"city"`
	Catalog    string          `bson:"catalog"`
	Range      string          `bson:"range"`
	RadiusKm   float64         `bson:"radiusKm"`
	Budget     Budget          `bson:"budget"`
	AgeBracket string          `bson:"ageBracket"`
	Hard       HardConstraints `bson:"hard"`
	AvoidTags  []string        `bson:"avoidTags"`
	Facets     []Facet         `bson:"facets"`
	Who        string          `bson:"who"`
	MoodText   string          `bson:"moodText"`
	QuickPicks []string        `bson:"quickPicks"`
	Flexible   bool            `bson:"flexible"`
}

// PoolScore is a cached ML score for one activity.
type PoolScore struct {
	ML  *float64 `bson:"ml" json:"ml"`
	Jev *float64 `bson:"jev" json:"jev"`
}

// PlanPool is the plan_pools document (TTL PoolTTL): everything paging,
// re-routing, alternatives and saving need after the response went out.
type PlanPool struct {
	ID           string               `bson:"_id"`
	UserID       string               `bson:"userId"`
	Catalog      string               `bson:"catalog"`
	CreatedAt    time.Time            `bson:"createdAt"`
	ExpiresAt    time.Time            `bson:"expiresAt"`
	Window       PoolWindow           `bson:"window"`
	Spec         PoolSpec             `bson:"spec"`
	QueryVector  []float64            `bson:"queryVector"`
	NegVector    []float64            `bson:"negVector,omitempty"`
	HasNeg       bool                 `bson:"hasNeg"`
	QuerySource  string               `bson:"querySource"`
	Options      []Option             `bson:"options"`
	Alternatives map[string]Stop      `bson:"alternatives"`
	Scores       map[string]PoolScore `bson:"scores"`
}

// PlanRun is the plan_runs document (TTL RunTTL): the audit trail of one
// Generate call.
type PlanRun struct {
	ID           string           `bson:"_id" json:"id"`
	UserID       string           `bson:"userId" json:"user_id"`
	Catalog      string           `bson:"catalog" json:"catalog"`
	City         string           `bson:"city" json:"city"`
	CreatedAt    time.Time        `bson:"createdAt" json:"created_at"`
	ExpiresAt    time.Time        `bson:"expiresAt" json:"expires_at"`
	Request      string           `bson:"request" json:"request"` // raw body
	Spec         SpecLog          `bson:"spec" json:"spec"`
	TZ           string           `bson:"tz" json:"tz"`
	SnappedStart bool             `bson:"snappedStart" json:"snapped_start"`
	Filters      FilterLog        `bson:"filters" json:"filters"`
	Counts       CountLog         `bson:"counts" json:"counts"`
	Shortlist    []ShortlistEntry `bson:"shortlist" json:"shortlist"`
	Rounds       []RoundLog       `bson:"rounds" json:"rounds"`
	ML           MLLog            `bson:"ml" json:"ml"`
	Final        FinalLog         `bson:"final" json:"final"`
	Outcome      *OutcomeLog      `bson:"outcome,omitempty" json:"outcome,omitempty"`
	Timings      map[string]int64 `bson:"timings" json:"timings"` // stage → ms
}

// SpecLog is the spec without the raw body.
type SpecLog struct {
	City       string    `bson:"city" json:"city"`
	From       time.Time `bson:"from" json:"from"`
	BackBy     time.Time `bson:"backBy" json:"back_by"`
	LocalDate  string    `bson:"localDate" json:"local_date"`
	Start      Place     `bson:"start" json:"start"`
	End        Place     `bson:"end" json:"end"`
	Mode       string    `bson:"mode" json:"mode"`
	DriveLabel string    `bson:"driveLabel" json:"drive_label"`
	Range      string    `bson:"range" json:"range"`
	MaxLegKm   float64   `bson:"maxLegKm" json:"max_leg_km"`
	Budget     Budget    `bson:"budget" json:"budget"`
	Pace       string    `bson:"pace" json:"pace"`
	Who        string    `bson:"who" json:"who"`
	OpenSeats  int       `bson:"openSeats" json:"open_seats"`
	MoodText   string    `bson:"moodText" json:"mood_text"`
	QuickPicks []string  `bson:"quickPicks" json:"quick_picks"`
	AgeBracket string    `bson:"ageBracket" json:"age_bracket"`
	Flexible   bool      `bson:"flexible" json:"flexible"`
}

type FilterLog struct {
	RadiusKm          float64         `bson:"radiusKm" json:"radius_km"`
	Window            TimeSlot        `bson:"window" json:"window"`
	Budget            Budget          `bson:"budget" json:"budget"`
	AgeBracket        string          `bson:"ageBracket" json:"age_bracket"`
	ExcludeCategories []string        `bson:"excludeCategories" json:"exclude_categories"`
	ExcludeTags       []string        `bson:"excludeTags" json:"exclude_tags"`
	MoodHard          HardConstraints `bson:"moodHard" json:"mood_hard"`
	Facets            []string        `bson:"facets" json:"facets"`
}

type CountLog struct {
	EventsA   int            `bson:"eventsA" json:"events_a"`
	PlacesA   int            `bson:"placesA" json:"places_a"`
	Feasible  int            `bson:"feasible" json:"feasible"`
	Drops     map[string]int `bson:"drops" json:"drops"`
	Shortlist int            `bson:"shortlist" json:"shortlist"`
	Ranked    int            `bson:"ranked" json:"ranked"`
	MLDropped int            `bson:"mlDropped" json:"ml_dropped"`
	Embedded  int            `bson:"embedded" json:"embedded"`
	// SeriesSiblings: feasible candidates that share a series with an
	// earlier one and took its scores instead of their own ranker slot.
	SeriesSiblings int `bson:"seriesSiblings" json:"series_siblings"`
}

type ShortlistEntry struct {
	ID       string   `bson:"id" json:"id"`
	Kind     string   `bson:"kind" json:"kind"`
	Category string   `bson:"category" json:"category"`
	Cos      float64  `bson:"cos" json:"cos"`
	Dislike  float64  `bson:"dislike" json:"dislike"`
	Prior    float64  `bson:"prior" json:"prior"`
	ML       *float64 `bson:"ml" json:"ml"`
	Source   string   `bson:"source" json:"source"`
	Round    int      `bson:"round" json:"round"`
}

type RoundLog struct {
	Round       int                `bson:"round" json:"round"`
	K           int                `bson:"k" json:"k"`
	Mu          float64            `bson:"mu" json:"mu"`
	ScoreMu     float64            `bson:"scoreMu" json:"score_mu"`
	StopBonus   float64            `bson:"stopBonus" json:"stop_bonus"` // the under-pace bonus in force
	Boosts      map[string]float64 `bson:"boosts" json:"boosts"`
	PoolSize    int                `bson:"poolSize" json:"pool_size"`
	Nodes       int                `bson:"nodes" json:"nodes"`
	Edges       int                `bson:"edges" json:"edges"`
	Itineraries int                `bson:"itineraries" json:"itineraries"`
	WithWeak    bool               `bson:"withWeak" json:"with_weak"` // weak stops were allowed (too few plans without)
	Drops       map[string]int     `bson:"drops" json:"drops"`
	SolveMs     int64              `bson:"solveMs" json:"solve_ms"`
	MLMs        int64              `bson:"mlMs" json:"ml_ms"`
	MongoMs     int64              `bson:"mongoMs" json:"mongo_ms"`
	Top3        []TopLog           `bson:"top3" json:"top3"`
	Top3Sum     float64            `bson:"top3Sum" json:"top3_sum"` // the three best scores, the convergence measure
	Issues      []IssueLog         `bson:"issues" json:"issues"`
	Expansions  []ExpansionLog     `bson:"expansions" json:"expansions"`
	Adapted     []string           `bson:"adapted" json:"adapted"`
	Stop        string             `bson:"stop,omitempty" json:"stop,omitempty"` // why the loop ended here
}

type TopLog struct {
	Signature string      `bson:"signature" json:"signature"`
	Score     float64     `bson:"score" json:"score"`
	Metrics   PlanMetrics `bson:"metrics" json:"metrics"`
}

type IssueLog struct {
	Kind     string    `bson:"kind" json:"kind"`
	Facet    string    `bson:"facet,omitempty" json:"facet,omitempty"`
	StopRef  string    `bson:"stopRef,omitempty" json:"stop_ref,omitempty"`
	Category string    `bson:"category,omitempty" json:"category,omitempty"`
	Slot     *TimeSlot `bson:"slot,omitempty" json:"slot,omitempty"`
}

type ExpansionLog struct {
	Kind     string `bson:"kind" json:"kind"`
	Facet    string `bson:"facet,omitempty" json:"facet,omitempty"`
	Fetched  int    `bson:"fetched" json:"fetched"`
	Feasible int    `bson:"feasible" json:"feasible"`
	Added    int    `bson:"added" json:"added"`
	Err      string `bson:"err,omitempty" json:"err,omitempty"`
}

type MLLog struct {
	Model     string  `bson:"model" json:"model"`
	Mode      string  `bson:"mode" json:"mode"` // classifier | fallback:<err> | skipped:<why>
	SearchErr string  `bson:"searchErr,omitempty" json:"search_err,omitempty"`
	Jev       *JevLog `bson:"jev,omitempty" json:"jev,omitempty"`
}

type JevLog struct {
	Requested   bool               `bson:"requested" json:"requested"`
	CompletedAt *time.Time         `bson:"completedAt,omitempty" json:"completed_at,omitempty"`
	Scores      map[string]float64 `bson:"scores" json:"scores"`
	Err         string             `bson:"err,omitempty" json:"err,omitempty"`
}

type FinalLog struct {
	OptionIDs []string `bson:"optionIds" json:"option_ids"`
	Relaxed   []string `bson:"relaxed" json:"relaxed"`
	TotalMs   int64    `bson:"totalMs" json:"total_ms"`
	Reason    string   `bson:"reason,omitempty" json:"reason,omitempty"`
	Rejected  int      `bson:"rejected" json:"rejected"` // options left out by CheckOption (0 unless there is a bug)
	// TravelHeavy: plans left out for travelling more than MaxTravelShare
	// of their time while another plan did not.
	TravelHeavy int `bson:"travelHeavy" json:"travel_heavy"`
	// WeakDropped: plans left out for a stop at or below the bar while at
	// least a page of plans had none.
	WeakDropped int `bson:"weakDropped" json:"weak_dropped"`
}

// OutcomeLog is patched in by POST /itineraries.
type OutcomeLog struct {
	SavedOptionID    string    `bson:"savedOptionId" json:"saved_option_id"`
	SavedAt          time.Time `bson:"savedAt" json:"saved_at"`
	StopOrder        []string  `bson:"stopOrder" json:"stop_order"`
	AlternativesUsed []string  `bson:"alternativesUsed" json:"alternatives_used"`
	ItineraryID      string    `bson:"itineraryId,omitempty" json:"itinerary_id,omitempty"`
}

// TimeSlot is a UTC interval.
type TimeSlot struct {
	From time.Time `bson:"from" json:"from"`
	To   time.Time `bson:"to" json:"to"`
}

// PoolStore persists pools and runs. Implementations: mongosource.Store
// and MemPoolStore.
type PoolStore interface {
	SaveRun(ctx context.Context, run *PlanRun) error
	PatchRun(ctx context.Context, id string, patch bson.M) error
	SavePool(ctx context.Context, pool *PlanPool) error
	GetPool(ctx context.Context, runID string) (*PlanPool, error) // ErrPoolNotFound when expired
	AddAlternatives(ctx context.Context, runID string, alts []Stop) error
	SetScores(ctx context.Context, runID string, scores map[string]PoolScore) error
}

// --- cursors and ids ------------------------------------------------------------

const cursorPrefix = "dag_"

// EncodeCursor is "dag_<runId>_<offset>": stateless, so any worker can
// serve the next page from the pool.
func EncodeCursor(runID string, offset int) string {
	return cursorPrefix + runID + "_" + strconv.Itoa(offset)
}

// DecodeCursor reverses EncodeCursor; ok is false for anything else.
func DecodeCursor(cursor string) (runID string, offset int, ok bool) {
	if !strings.HasPrefix(cursor, cursorPrefix) {
		return "", 0, false
	}
	rest := cursor[len(cursorPrefix):]
	i := strings.LastIndex(rest, "_")
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return rest[:i], n, true
}

// OptionID is "<runId>-<n>".
func OptionID(runID string, n int) string {
	return runID + "-" + strconv.Itoa(n)
}

// RunIDFromOption reverses OptionID.
func RunIDFromOption(optionID string) (string, bool) {
	i := strings.LastIndex(optionID, "-")
	if i <= 0 {
		return "", false
	}
	if _, err := strconv.Atoi(optionID[i+1:]); err != nil {
		return "", false
	}
	return optionID[:i], true
}

// StopID is "stop_<activityId>_<i>"; AltStopID is "alt_<activityId>_<slot>".
func StopID(activityID string, i int) string    { return "stop_" + activityID + "_" + strconv.Itoa(i) }
func AltStopID(activityID string, i int) string { return "alt_" + activityID + "_" + strconv.Itoa(i) }

// ActivityIDFromStop reads the activity id out of a stop or alternative id.
func ActivityIDFromStop(stopID string) (string, bool) {
	rest, ok := strings.CutPrefix(stopID, "stop_")
	if !ok {
		rest, ok = strings.CutPrefix(stopID, "alt_")
	}
	if !ok {
		return "", false
	}
	i := strings.LastIndex(rest, "_")
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}
