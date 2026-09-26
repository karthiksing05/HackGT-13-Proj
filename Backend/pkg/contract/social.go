package contract

type ForumPost struct {
	ID               string        `json:"id"`
	Type             ForumPostType `json:"type"`
	Author           PersonRef     `json:"author"`
	IsFriend         bool          `json:"is_friend"`
	FriendsOnly      bool          `json:"friends_only"`
	Title            *string       `json:"title,omitempty"`
	Text             *string       `json:"text,omitempty"`
	Meta             string        `json:"meta"`
	When             *string       `json:"when,omitempty"`
	Route            *string       `json:"route,omitempty"`
	StartsInMinutes  int           `json:"starts_in_minutes"`
	Day              string        `json:"day"`
	DistanceMi       float64       `json:"distance_mi"`
	PriceTier        int           `json:"price_tier"`
	Tags             []string      `json:"tags"`
	SpotsLeft        *int          `json:"spots_left,omitempty"`
	Capacity         *int          `json:"capacity,omitempty"`
	LockLabel        *string       `json:"lock_label,omitempty"`
	Going            []PersonRef   `json:"going"`
	GoingCount       int           `json:"going_count"`
	InterestedCount  int           `json:"interested_count"`
	PostedMinutesAgo int           `json:"posted_minutes_ago"`
	JoinStatus       JoinStatus    `json:"join_status"`
	PlanTogetherSent bool          `json:"plan_together_sent"`
	ThreadID         *string       `json:"thread_id,omitempty"`
}

type JoinResult struct {
	Status      JoinStatus `json:"status"`
	ItineraryID *string    `json:"itinerary_id,omitempty"`
	ThreadID    *string    `json:"thread_id,omitempty"`
}

// MyFreePost is the viewer's live "I'm free" post.
type MyFreePost struct {
	ID         string              `json:"id"`
	Visibility ForumPostVisibility `json:"visibility"`
	Text       string              `json:"text"`
	Until      *Time               `json:"until,omitempty"`
	AreaLabel  *string             `json:"area_label,omitempty"`
	RadiusMi   *int                `json:"radius_mi,omitempty"`
}

// NewForumPost is the body of POST /forum/posts as LiveAPIClient sends it
// (the app's NewFreePost flattened: type, visibility, until, lat, lng,
// area_label, radius_mi).
type NewForumPost struct {
	Type       ForumPostType       `json:"type"`
	Visibility ForumPostVisibility `json:"visibility"`
	Until      *Time               `json:"until,omitempty"`
	Lat        *float64            `json:"lat,omitempty"`
	Lng        *float64            `json:"lng,omitempty"`
	AreaLabel  *string             `json:"area_label,omitempty"`
	RadiusMi   *int                `json:"radius_mi,omitempty"`
}

type ChatThread struct {
	ID            string      `json:"id"`
	IsGroup       bool        `json:"is_group"`
	Title         string      `json:"title"`
	Subtitle      string      `json:"subtitle"`
	Members       []PersonRef `json:"members"`
	Faces         []PersonRef `json:"faces"`
	LastMessage   string      `json:"last_message"`
	LastTime      string      `json:"last_time"`
	Chips         []string    `json:"chips"`
	Unread        int         `json:"unread"`
	AlbumTitle    *string     `json:"album_title,omitempty"`
	AlbumSubtitle *string     `json:"album_subtitle,omitempty"`
}

type Message struct {
	ID         string  `json:"id"`
	SenderID   string  `json:"sender_id"`
	SenderName string  `json:"sender_name"`
	Text       string  `json:"text"`
	SentAt     Time    `json:"sent_at"`
	ClientID   *string `json:"client_id,omitempty"`
}

// NewMessage is POST /threads/{id}/messages.
type NewMessage struct {
	Text     string  `json:"text"`
	ClientID *string `json:"client_id,omitempty"`
}

// StartDMRequest is POST /threads/dm and POST /friends/requests ({user_id}).
type StartDMRequest struct {
	UserID string `json:"user_id"`
}

type GroupPhoto struct {
	ID             string  `json:"id"`
	ByName         string  `json:"by_name"`
	UploaderID     *string `json:"uploader_id,omitempty"`
	CreatedAt      *Time   `json:"created_at,omitempty"`
	URL            *string `json:"url,omitempty"`
	PlaceholderHex *string `json:"placeholder_hex,omitempty"`
}

type Expense struct {
	ID          string   `json:"id"`
	What        string   `json:"what"`
	AmountCents int      `json:"amount_cents"`
	PayerID     string   `json:"payer_id"`
	SplitAmong  []string `json:"split_among"`
	Shares      []int    `json:"shares"`
	CreatedBy   *string  `json:"created_by,omitempty"`
}

type NewExpense struct {
	What        string   `json:"what"`
	AmountCents int      `json:"amount_cents"`
	PayerID     string   `json:"payer_id"`
	SplitAmong  []string `json:"split_among"`
}

// Balance is per other member: + they owe the viewer, − the viewer owes them.
type Balance struct {
	UserID   string `json:"user_id"`
	NetCents int    `json:"net_cents"`
}

type SettleRequest struct {
	AmountCents     int     `json:"amount_cents"`
	PaymentMethodID *string `json:"payment_method_id"`
}

// GroupLedger is the app's bundle of thread members, expenses and balances
// (three calls); it is a contract type so the example round-trips.
type GroupLedger struct {
	Members  []PersonRef `json:"members"`
	Expenses []Expense   `json:"expenses"`
	Balances []Balance   `json:"balances"`
}
