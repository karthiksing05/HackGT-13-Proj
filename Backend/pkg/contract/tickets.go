package contract

// MyTicket is one row of GET /me/tickets: a ticket the viewer booked (a
// single checkout or a Muse run) and the stop it admits them to, as that
// stop is now. The ticket is always theirs (mine: true). ItineraryTitle is
// left out once the viewer can no longer see the plan (they left it, or the
// host deleted it); the ticket still lists.
type MyTicket struct {
	Ticket         Ticket    `json:"ticket"`
	ItemID         string    `json:"item_id"`
	ItineraryID    string    `json:"itinerary_id"`
	ItineraryTitle *string   `json:"itinerary_title,omitempty"`
	Title          string    `json:"title"`
	Start          Time      `json:"start"`
	End            Time      `json:"end"`
	Place          Place     `json:"place"`
	Kind           BlockKind `json:"kind"`
}
