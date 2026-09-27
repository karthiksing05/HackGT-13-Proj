package contract

type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Place is a named location; coordinate is nil for label-only places.
type Place struct {
	Name       string      `json:"name"`
	Coordinate *Coordinate `json:"coordinate,omitempty"`
}

// User is the signed-in person (GET /me, AuthResponse.user).
type User struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Username      *string        `json:"username,omitempty"`
	Email         string         `json:"email"`
	PhotoURL      *string        `json:"photo_url,omitempty"`
	AvatarColor   AvatarColor    `json:"avatar_color"`
	Status        PresenceStatus `json:"status"`
	AgeBracket    AgeBracket     `json:"age_bracket"`
	School        *string        `json:"school,omitempty"`
	SetupComplete bool           `json:"setup_complete"`
	HomeBase      *Place         `json:"home_base,omitempty"`
	City          *string        `json:"city,omitempty"`
	// DemoDate is "2026-09-24" for a demo account while DEMO_DATE is set:
	// the server's "today" for that account (pkg/democlock).
	DemoDate *string `json:"demo_date,omitempty"`
}

// PersonRef is how everyone else appears (avatars, senders, members).
type PersonRef struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Initials string  `json:"initials"`
	ColorHex string  `json:"color_hex"`
	PhotoURL *string `json:"photo_url,omitempty"`
	Username *string `json:"username,omitempty"`
}

type Friend struct {
	Person     PersonRef      `json:"person"`
	StatusLine string         `json:"status_line"`
	Activity   FriendActivity `json:"activity"`
}

type FriendRequest struct {
	ID       string    `json:"id"`
	Person   PersonRef `json:"person"`
	Note     string    `json:"note"`
	Outgoing bool      `json:"outgoing"`
}

type UserSearchResult struct {
	Person    PersonRef      `json:"person"`
	Relation  FriendRelation `json:"relation"`
	RequestID *string        `json:"request_id,omitempty"`
}

// PersonSuggestion is one row of GET /people/suggested: someone whose taste
// matches the viewer's, with the match as a whole-number percent (0–100),
// or (no compatibility) someone recently active, when taste matches run out.
type PersonSuggestion struct {
	Person        PersonRef      `json:"person"`
	Relation      FriendRelation `json:"relation"`
	RequestID     *string        `json:"request_id,omitempty"`
	Compatibility *int           `json:"compatibility,omitempty"`
}
