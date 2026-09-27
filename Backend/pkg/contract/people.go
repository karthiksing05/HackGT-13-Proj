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
// matches the viewer's, with the match as a whole-number percent (0–100).
type PersonSuggestion struct {
	Person        PersonRef      `json:"person"`
	Relation      FriendRelation `json:"relation"`
	RequestID     *string        `json:"request_id,omitempty"`
	Compatibility int            `json:"compatibility"`
}

// ProfileRelation is how the person on a profile relates to the viewer:
// the FriendRelation words, or "self" on the viewer's own profile.
type ProfileRelation string

const (
	ProfileSelf     ProfileRelation = "self"
	ProfileNone     ProfileRelation = "none"
	ProfileFriend   ProfileRelation = "friend"
	ProfileOutgoing ProfileRelation = "outgoing"
	ProfileIncoming ProfileRelation = "incoming"
)

// MutualFriends are the friends two people have in common: how many, and
// up to three of them.
type MutualFriends struct {
	Count  int         `json:"count"`
	People []PersonRef `json:"people"`
}

// PublicProfile is GET /users/{id}/profile: someone as the viewer sees
// them. StatusLine is the line their friends see ("Free until 6:30 PM"),
// for friends only. Compatibility is the taste match as a whole-number
// percent (0–100), absent on the viewer's own profile and when it can't be
// scored. MatchReasons (up to 3) come from what both like, Likes (up to 5)
// are what they like most, as labels. OpenPlans are their upcoming plans
// the viewer may see, rendered as the Forum renders plan posts.
type PublicProfile struct {
	Person         PersonRef       `json:"person"`
	School         *string         `json:"school,omitempty"`
	City           *string         `json:"city,omitempty"`
	Status         PresenceStatus  `json:"status"`
	StatusLine     *string         `json:"status_line,omitempty"`
	Relation       ProfileRelation `json:"relation"`
	RequestID      *string         `json:"request_id,omitempty"`
	Compatibility  *int            `json:"compatibility,omitempty"`
	MatchReasons   []string        `json:"match_reasons"`
	Likes          []string        `json:"likes"`
	MutualFriends  MutualFriends   `json:"mutual_friends"`
	OpenPlans      []ForumPost     `json:"open_plans"`
	SidequestsDone int             `json:"sidequests_done"`
}
