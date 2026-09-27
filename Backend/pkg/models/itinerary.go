package models

import "time"

// Itinerary statuses, item kinds and visibilities as stored.
const (
	ItineraryActive  = "active"
	ItineraryPast    = "past"
	ItineraryDeleted = "deleted"

	ItemStop    = "stop"
	ItemTransit = "transit"

	VisibilityJustMe  = "just_me"
	VisibilityFriends = "friends"
	VisibilityOpen    = "open"
)

// PlaceDoc is a named place with an optional coordinate (the contract Place).
type PlaceDoc struct {
	Name string   `bson:"name"`
	Lat  *float64 `bson:"lat,omitempty"`
	Lng  *float64 `bson:"lng,omitempty"`
}

// HasCoordinate reports whether both lat and lng are set.
func (p PlaceDoc) HasCoordinate() bool { return p.Lat != nil && p.Lng != nil }

// PlanSnapshot is the part of the Create request an itinerary keeps: enough
// to explain it later and to re-plan from it.
type PlanSnapshot struct {
	Range     string   `bson:"range"`
	Ride      string   `bson:"ride"`
	OpenSeats *int     `bson:"openSeats,omitempty"`
	MoodText  string   `bson:"moodText,omitempty"`
	Tags      []string `bson:"tags,omitempty"`
	Budget    int      `bson:"budget"`
	Who       string   `bson:"who"`
	Pace      string   `bson:"pace"`
	Modes     []string `bson:"modes,omitempty"`
}

// ItineraryItem is one timed block of an itinerary: a stop, or the transit
// leg that leads to the next stop. Per-viewer state (private notes, chosen
// transit, tickets) lives in item_states; ratings in ratings.
type ItineraryItem struct {
	ID            string    `bson:"id"`   // uuid, unique across itineraries (index items.id)
	Kind          string    `bson:"kind"` // stop | transit
	Title         string    `bson:"title"`
	Place         *PlaceDoc `bson:"place,omitempty"`
	Start         time.Time `bson:"start"`
	End           time.Time `bson:"end"`
	Description   string    `bson:"description,omitempty"`
	WebsiteURL    *string   `bson:"websiteUrl,omitempty"`
	TicketURL     *string   `bson:"ticketUrl,omitempty"` // the catalog's ticket page (agentic checkout)
	Bookable      bool      `bson:"bookable"`
	PriceCents    *int      `bson:"priceCents,omitempty"`
	ActivityID    string    `bson:"activityId,omitempty"` // catalog _id hex of the stop
	StopID        string    `bson:"stopId,omitempty"`     // the plan option's stop id it came from
	DurationMin   int       `bson:"durationMin,omitempty"`
	SharedNotes   *string   `bson:"sharedNotes,omitempty"`
	SharedNotesBy string    `bson:"sharedNotesBy,omitempty"`
	LegMode       string    `bson:"legMode,omitempty"` // transit items: walk | marta | drive | rideshare
	LegMinutes    int       `bson:"legMinutes,omitempty"`
}

// Itinerary is the itineraries document: a saved sidequest shared by its
// members. Forum plan posts are derived from itineraries whose visibility
// is not just_me (post id = itinerary id).
type Itinerary struct {
	ID           string          `bson:"_id"` // uuid
	HostID       string          `bson:"hostId"`
	MemberIDs    []string        `bson:"memberIds"` // includes the host
	Title        string          `bson:"title"`
	DateKey      string          `bson:"dateKey"` // YYYY-MM-DD in TZ
	TZ           string          `bson:"tz"`      // IANA name from X-Time-Zone at creation
	Date         time.Time       `bson:"date"`    // local midnight of DateKey
	Start        time.Time       `bson:"start"`
	BackBy       time.Time       `bson:"backBy"`
	StartPlace   PlaceDoc        `bson:"startPlace"`
	EndPlace     PlaceDoc        `bson:"endPlace"`
	Visibility   string          `bson:"visibility"` // just_me | friends | open
	LockAt       *time.Time      `bson:"lockAt,omitempty"`
	MaxGroupSize *int            `bson:"maxGroupSize,omitempty"`
	Items        []ItineraryItem `bson:"items"`
	Plan         *PlanSnapshot   `bson:"plan,omitempty"`
	OptionID     string          `bson:"optionId,omitempty"`
	RunID        string          `bson:"runId,omitempty"`
	RouteMode    string          `bson:"routeMode,omitempty"`
	Status       string          `bson:"status"` // active | past | deleted
	ThreadID     string          `bson:"threadId,omitempty"`
	PostID       string          `bson:"postId,omitempty"`
	CreatedAt    time.Time       `bson:"createdAt"`
	UpdatedAt    time.Time       `bson:"updatedAt"`
}

// IsMember reports whether userID belongs to the itinerary.
func (it *Itinerary) IsMember(userID string) bool {
	for _, id := range it.MemberIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// ItemTicket is a booked ticket for one item (written by the checkout agent).
type ItemTicket struct {
	ID           string  `bson:"id"`
	Quantity     int     `bson:"quantity"`
	TotalCents   *int    `bson:"totalCents,omitempty"`
	Confirmation *string `bson:"confirmation,omitempty"`
	URL          *string `bson:"url,omitempty"`
}

// ItemState is one viewer's state on one item (item_states, _id = userId|itemId).
type ItemState struct {
	ID          string      `bson:"_id"`
	UserID      string      `bson:"userId"`
	ItemID      string      `bson:"itemId"`
	ItineraryID string      `bson:"itineraryId"`
	Notes       *string     `bson:"notes,omitempty"`
	NotesScope  string      `bson:"notesScope,omitempty"` // private | shared
	TransitMode string      `bson:"transitMode,omitempty"`
	Ticket      *ItemTicket `bson:"ticket,omitempty"`
	UpdatedAt   time.Time   `bson:"updatedAt"`
}

// Rating is one user's rating of one stop (ratings, _id = userId|itemId).
type Rating struct {
	ID          string    `bson:"_id"`
	UserID      string    `bson:"userId"`
	ItemID      string    `bson:"itemId"`
	ItineraryID string    `bson:"itineraryId"`
	ActivityID  string    `bson:"activityId,omitempty"`
	Stars       int       `bson:"stars"`
	Tags        []string  `bson:"tags"`
	Note        *string   `bson:"note,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

// PairID is the "<a>|<b>" id used by item_states, ratings and plan_together.
func PairID(a, b string) string { return a + "|" + b }
