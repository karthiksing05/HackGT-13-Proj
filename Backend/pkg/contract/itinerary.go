package contract

type Rating struct {
	Stars int      `json:"stars"`
	Tags  []string `json:"tags"`
	Note  *string  `json:"note,omitempty"`
}

type Ticket struct {
	ID           string  `json:"id"`
	Quantity     int     `json:"quantity"`
	TotalCents   *int    `json:"total_cents,omitempty"`
	Confirmation *string `json:"confirmation,omitempty"`
	URL          *string `json:"url,omitempty"`
	// Mine is false for a ticket another member of the plan booked (shown
	// so the group sees it); the viewer can still get their own.
	Mine bool `json:"mine"`
}

// ItineraryItem is one timeline block, rendered per viewer (notes, rating,
// transit choice and ticket are the viewer's own).
type ItineraryItem struct {
	ID          string      `json:"id"`
	Kind        BlockKind   `json:"kind"`
	Title       string      `json:"title"`
	Place       *Place      `json:"place,omitempty"`
	Start       Time        `json:"start"`
	End         Time        `json:"end"`
	Description *string     `json:"description,omitempty"`
	WebsiteURL  *string     `json:"website_url,omitempty"`
	TicketURL   *string     `json:"ticket_url,omitempty"`
	Bookable    bool        `json:"bookable"`
	PriceCents  *int        `json:"price_cents,omitempty"`
	People      []PersonRef `json:"people"`
	Interested  []PersonRef `json:"interested"`
	ExtraGoing  int         `json:"extra_going"`
	Notes       *string     `json:"notes,omitempty"`
	NotesScope  *NotesScope `json:"notes_scope,omitempty"`
	Rating      *Rating     `json:"rating,omitempty"`
	TransitMode *TravelMode `json:"transit_mode,omitempty"`
	Ticket      *Ticket     `json:"ticket,omitempty"`
	ActivityID  *string     `json:"activity_id,omitempty"`
}

type Itinerary struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Date         Time            `json:"date"`
	Start        Time            `json:"start"`
	BackBy       Time            `json:"back_by"`
	StartPlace   Place           `json:"start_place"`
	EndPlace     Place           `json:"end_place"`
	Visibility   Visibility      `json:"visibility"`
	LockAt       *Time           `json:"lock_at,omitempty"`
	MaxGroupSize *int            `json:"max_group_size,omitempty"`
	Items        []ItineraryItem `json:"items"`
	GoingCount   int             `json:"going_count"`
	IsHost       bool            `json:"is_host"`
}

// ItineraryUpdate is PATCH /itineraries/{id}: only present fields change; a
// present but empty stop_order is a 400.
type ItineraryUpdate struct {
	Title      *string     `json:"title,omitempty"`
	Date       *Time       `json:"date,omitempty"`
	Start      *Time       `json:"start,omitempty"`
	BackBy     *Time       `json:"back_by,omitempty"`
	Visibility *Visibility `json:"visibility,omitempty"`
	StopOrder  []string    `json:"stop_order,omitempty"`
}

// ItemNotesPatch is PATCH /itineraries/{id}/items/{itemId} and PATCH /events/{id}.
type ItemNotesPatch struct {
	Notes      string      `json:"notes"`
	NotesScope *NotesScope `json:"notes_scope"`
}

type TransitOption struct {
	Mode      TravelMode `json:"mode"`
	Minutes   int        `json:"minutes"`
	CostCents *int       `json:"cost_cents,omitempty"`
}

// TransitSelection is PUT …/transit ({mode}).
type TransitSelection struct {
	Mode TravelMode `json:"mode"`
}

type CalendarItem struct {
	ID          string      `json:"id"`
	Kind        BlockKind   `json:"kind"`
	Title       string      `json:"title"`
	Start       Time        `json:"start"`
	End         Time        `json:"end"`
	People      []PersonRef `json:"people"`
	Interested  []PersonRef `json:"interested"`
	ItineraryID *string     `json:"itinerary_id,omitempty"`
}

type CalendarDay struct {
	ID    string         `json:"id"` // "2026-09-25" in the request time zone
	Date  Time           `json:"date"`
	Items []CalendarItem `json:"items"`
}

type PastEvent struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Place   string    `json:"place"`
	Company string    `json:"company"`
	Date    Time      `json:"date"`
	Kind    BlockKind `json:"kind"`
	Rating  *Rating   `json:"rating,omitempty"`
}

// Page is the cursor-paginated list shape ({items, next_cursor}); next_cursor
// is null on the last page.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// NewPage builds a Page, never with a nil items slice.
func NewPage[T any](items []T, next *string) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, NextCursor: next}
}
