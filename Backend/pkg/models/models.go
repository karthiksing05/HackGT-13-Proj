package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// GeoJSONPoint represents a GeoJSON 2D Point with [lng, lat]
type GeoJSONPoint struct {
	Type        string    `bson:"type" json:"type"`               // "Point"
	Coordinates []float64 `bson:"coordinates" json:"coordinates"` // [lng, lat] - lng FIRST
}

// UserAnswers represents the open-ended setup answers (typed or Wispr Flow voice)
type UserAnswers struct {
	PerfectAfternoon string `bson:"perfectAfternoon" json:"perfectAfternoon"`
	NeverWant        string `bson:"neverWant" json:"neverWant"`
	PlanAround       string `bson:"planAround" json:"planAround"`
}

// UserPrefs represents setup answers (steps 3–5)
type UserPrefs struct {
	Ratings    map[string]int `bson:"ratings" json:"ratings"`       // 1–5, missing = not rated
	Company    *string        `bson:"company" json:"company"`       // "solo" | "small_group" | "big_group" | null
	Pace       *string        `bson:"pace" json:"pace"`             // "chill" | "balanced" | "packed" | null
	SpendTier  *int           `bson:"spendTier" json:"spendTier"`   // 0 free, 1 <$15, 2 $15–40, 3 $40+
	Flexible   bool           `bson:"flexible" json:"flexible"`     // "a bit over is OK"
	SplitStyle string         `bson:"splitStyle" json:"splitStyle"` // "equal" | "own" | "turns"
	PreferFree bool           `bson:"preferFree" json:"preferFree"`
	Answers    UserAnswers    `bson:"answers" json:"answers"` // typed or voice
}

// UserTaste represents learned taste (seeded from prefs, updated by ratings)
type UserTaste struct {
	Tags        map[string]float64 `bson:"tags" json:"tags"`           // 0..1, same tag vocabulary as events
	AvoidTags   []string           `bson:"avoidTags" json:"avoidTags"` // e.g. "crowded"
	RatingCount int                `bson:"ratingCount" json:"ratingCount"`
}

// CalendarConn represents connected calendar (free/busy only)
type CalendarConn struct {
	Provider string    `bson:"provider" json:"provider"` // "google" | "outlook"
	TokenRef string    `bson:"tokenRef" json:"tokenRef"`
	SyncedAt time.Time `bson:"syncedAt" json:"syncedAt"`
}

// UserCard represents tokenized payment card
type UserCard struct {
	Brand string `bson:"brand" json:"brand"` // "visa"
	Last4 string `bson:"last4" json:"last4"` // "4242"
	Token string `bson:"token" json:"token"`
}

// User represents the full user document schema
type User struct {
	ID                bson.ObjectID   `bson:"_id,omitempty" json:"_id"`
	Email             string          `bson:"email" json:"email"`    // unique index, lowercase
	PasswordHash      string          `bson:"passwordHash" json:"-"` // never sent to clients
	Name              string          `bson:"name" json:"name"`
	Username          *string         `bson:"username" json:"username"` // unique, no "@"
	PhotoURL          *string         `bson:"photoUrl" json:"photoUrl"`
	AvatarColor       string          `bson:"avatarColor" json:"avatarColor"`             // "ink" | "sage" | "clay" | "forest" | "sand"
	BirthDate         *time.Time      `bson:"birthDate,omitempty" json:"-"`               // only used to set ageBracket, never shown
	AgeBracket        *string         `bson:"ageBracket" json:"ageBracket"`               // "13_17" | "18_20" | "21_plus" | null
	Status            string          `bson:"status" json:"status"`                       // "open" | "online" | "not_free"
	LastLocation      *GeoJSONPoint   `bson:"lastLocation,omitempty" json:"lastLocation"` // lng FIRST, only saved while planning
	Prefs             UserPrefs       `bson:"prefs" json:"prefs"`
	Taste             UserTaste       `bson:"taste" json:"taste"`
	Calendars         []CalendarConn  `bson:"calendars" json:"calendars"`
	Card              *UserCard       `bson:"card" json:"card"`
	APNsTokens        []string        `bson:"apnsTokens,omitempty" json:"apnsTokens,omitempty"`
	FriendIDs         []bson.ObjectID `bson:"friendIds" json:"friendIds"`
	Embedding         []float64       `bson:"embedding,omitempty" json:"embedding,omitempty"`
	PositiveEmbedding []float64       `bson:"positiveEmbedding,omitempty" json:"positive_embedding,omitempty"`
	NegativeEmbedding []float64       `bson:"negativeEmbedding,omitempty" json:"negative_embedding,omitempty"`
	PositiveText      string          `bson:"positiveText,omitempty" json:"positive_text,omitempty"`
	NegativeText      string          `bson:"negativeText,omitempty" json:"negative_text,omitempty"`
	EmbeddingModel    string          `bson:"embeddingModel,omitempty" json:"embeddingModel,omitempty"`
	EmbeddingTextHash string          `bson:"embeddingTextHash,omitempty" json:"embeddingTextHash,omitempty"`
	CreatedAt         time.Time       `bson:"createdAt" json:"createdAt"`
	UpdatedAt         time.Time       `bson:"updatedAt" json:"updatedAt"`
	LastActiveAt      time.Time       `bson:"lastActiveAt" json:"lastActiveAt"`
}

type UserSummary struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Username    *string `json:"username"`
	PhotoURL    *string `json:"photoUrl"`
	Status      string  `json:"status"`
	AvatarColor string  `json:"avatarColor"`
}

// ActivityAddress represents the address component of an activity
type ActivityAddress struct {
	Formatted   *string `bson:"formatted" json:"formatted"`
	Street      *string `bson:"street" json:"street"`
	Locality    *string `bson:"locality" json:"locality"`
	Region      *string `bson:"region" json:"region"`
	PostalCode  *string `bson:"postalCode" json:"postalCode"`
	CountryCode *string `bson:"countryCode" json:"countryCode"`
}

// ActivityDuration represents duration metadata
type ActivityDuration struct {
	MedianMin float64 `bson:"medianMin" json:"medianMin"`
	Sigma     float64 `bson:"sigma" json:"sigma"`
	P75Min    float64 `bson:"p75Min" json:"p75Min"`
	Source    string  `bson:"source" json:"source"`
}

// ActivityPrice represents activity price and tier
type ActivityPrice struct {
	Tier     int      `bson:"tier" json:"tier"` // 0 free, 1 <$15, 2 $15–40, 3 $40+
	Cents    int64    `bson:"cents" json:"cents"`
	Min      *float64 `bson:"min,omitempty" json:"min,omitempty"` // whole currency units, as ingested
	Max      *float64 `bson:"max,omitempty" json:"max,omitempty"`
	Currency string   `bson:"currency" json:"currency"`
	IsFree   bool     `bson:"isFree" json:"isFree"`
}

// ActivitySource represents provenance of scraped or imported activity
type ActivitySource struct {
	Name      string    `bson:"name" json:"name"`
	ID        string    `bson:"id" json:"id"`
	URL       string    `bson:"url" json:"url"`
	FetchedAt time.Time `bson:"fetchedAt" json:"fetchedAt"`
}

// WeeklyHourRange represents open/close minutes of the week (e.g. 360 to 1080)
type WeeklyHourRange struct {
	Open  int `bson:"open" json:"open"`
	Close int `bson:"close" json:"close"`
}

// GeoJSONLineString represents a GeoJSON LineString geometry
type GeoJSONLineString struct {
	Type        string      `bson:"type" json:"type"`
	Coordinates [][]float64 `bson:"coordinates" json:"coordinates"`
}

// TrailInfo represents trail geometry and statistics
type TrailInfo struct {
	LengthKm float64           `bson:"lengthKm" json:"lengthKm"`
	AscentM  float64           `bson:"ascentM" json:"ascentM"`
	DescentM float64           `bson:"descentM" json:"descentM"`
	Loop     bool              `bson:"loop" json:"loop"`
	Geometry GeoJSONLineString `bson:"geometry" json:"geometry"`
}

// Activity represents the full activity document schema from the freetime database
type Activity struct {
	ID                bson.ObjectID     `bson:"_id,omitempty" json:"_id"`
	Kind              string            `bson:"kind" json:"kind"` // "place" | "event"
	City              string            `bson:"city" json:"city"` // "atlanta"
	Name              string            `bson:"name" json:"name"`
	Summary           *string           `bson:"summary" json:"summary"`
	Description       *string           `bson:"description" json:"description"`
	Category          string            `bson:"category" json:"category"`
	SourceCategory    *string           `bson:"sourceCategory" json:"sourceCategory"`
	Tags              []string          `bson:"tags" json:"tags"`
	Location          GeoJSONPoint      `bson:"location" json:"location"` // coordinates: [lng, lat]
	Address           *ActivityAddress  `bson:"address" json:"address"`
	VenueName         *string           `bson:"venueName" json:"venueName"`
	Start             *time.Time        `bson:"start" json:"start"`
	End               *time.Time        `bson:"end" json:"end"`
	Attendance        *string           `bson:"attendance" json:"attendance"`
	Timezone          string            `bson:"timezone" json:"timezone"` // "America/New_York"
	WeeklyHours       []WeeklyHourRange `bson:"weeklyHours" json:"weeklyHours"`
	HoursSource       *string           `bson:"hoursSource" json:"hoursSource"`
	Recurrence        interface{}       `bson:"recurrence" json:"recurrence"`
	Duration          *ActivityDuration `bson:"duration" json:"duration"`
	Price             *ActivityPrice    `bson:"price" json:"price"`
	Rating            *float64          `bson:"rating" json:"rating"`
	RatingCount       *int              `bson:"ratingCount" json:"ratingCount"`
	Popularity        *float64          `bson:"popularity" json:"popularity"`
	Trail             *TrailInfo        `bson:"trail" json:"trail"`
	URL               *string           `bson:"url" json:"url"`
	TicketURL         *string           `bson:"ticketUrl" json:"ticketUrl"`
	ImageURL          *string           `bson:"imageUrl" json:"imageUrl"`
	SourceKeys        []string          `bson:"sourceKeys" json:"sourceKeys"`
	Sources           []ActivitySource  `bson:"sources" json:"sources"`
	GooglePlaceID     *string           `bson:"googlePlaceId" json:"googlePlaceId"`
	ExpiresAt         *time.Time        `bson:"expiresAt" json:"expiresAt"`
	CreatedAt         time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt         time.Time         `bson:"updatedAt" json:"updatedAt"`
	Embedding         []float64         `bson:"embedding,omitempty" json:"embedding,omitempty"`
	EmbeddingText     *string           `bson:"embeddingText,omitempty" json:"embeddingText,omitempty"`
	EmbeddingTextHash *string           `bson:"embeddingTextHash,omitempty" json:"embeddingTextHash,omitempty"`
	Score             *float64          `bson:"score,omitempty" json:"score,omitempty"`
	RerankScore       *float64          `bson:"rerankScore,omitempty" json:"rerank_score,omitempty"`
}

// Aliases for compatibility
type Event = Activity
type EventAddress = ActivityAddress
type EventDuration = ActivityDuration
type EventPrice = ActivityPrice
type EventSource = ActivitySource

type PasswordResetRecord struct {
	ID        string    `bson:"_id" json:"id"`
	Email     string    `bson:"email" json:"email"`
	Code      string    `bson:"code" json:"code"`
	Verified  bool      `bson:"verified" json:"verified"`
	ExpiresAt time.Time `bson:"expiresAt" json:"expires_at"`
}

type TimeBlock struct {
	StartTime time.Time `bson:"startTime" json:"start_time"`
	EndTime   time.Time `bson:"endTime" json:"end_time"`
	Title     string    `bson:"title" json:"title"`
}

type CalendarEventItem struct {
	ID        string    `bson:"_id" json:"id"`
	Title     string    `bson:"title" json:"title"`
	StartTime time.Time `bson:"startTime" json:"start_time"`
	EndTime   time.Time `bson:"endTime" json:"end_time"`
	Location  string    `bson:"location" json:"location"`
}

type CalendarDay struct {
	Date        string              `json:"date"` // YYYY-MM-DD
	BusyBlocks  []TimeBlock         `json:"busy_blocks"`
	Sidequests  []ItinerarySummary  `json:"sidequests"`
	GroupEvents []CalendarEventItem `json:"group_events"`
}

type CalendarLinkResponse struct {
	Token             string            `json:"token"`
	URL               string            `json:"url"`
	WebcalURL         string            `json:"webcal_url"`
	GoogleCalendarURL string            `json:"google_calendar_url"`
	Instructions      map[string]string `json:"instructions"`
}

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
	Kind       string     `json:"kind,omitempty"` // "event" | "place"
	Flexible   bool       `json:"flexible"`       // true when the visit time can move (drop-ins, places)
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

type ItineraryItem struct {
	ID             string          `bson:"id" json:"id"`
	Title          string          `bson:"title" json:"title"`
	Type           string          `bson:"type" json:"type"`
	LocationName   string          `bson:"locationName" json:"location_name"`
	Address        string          `bson:"address" json:"address"`
	Lat            float64         `bson:"lat" json:"lat"`
	Lng            float64         `bson:"lng" json:"lng"`
	ArriveTime     time.Time       `bson:"arriveTime" json:"arrive_time"`
	DepartTime     time.Time       `bson:"departTime" json:"depart_time"`
	PriceCents     int64           `bson:"priceCents" json:"price_cents"`
	PrivateNotes   string          `bson:"privateNotes,omitempty" json:"private_notes,omitempty"`
	SharedNotes    string          `bson:"sharedNotes,omitempty" json:"shared_notes,omitempty"`
	TransitOptions []TransitOption `bson:"transitOptions,omitempty" json:"transit_options,omitempty"`
}

type ItineraryMember struct {
	UserID      string  `bson:"userId" json:"user_id"`
	Name        string  `bson:"name" json:"name"`
	Username    *string `bson:"username" json:"username"`
	PhotoURL    *string `bson:"photoUrl,omitempty" json:"photo_url,omitempty"`
	AvatarColor string  `bson:"avatarColor" json:"avatar_color"`
	Role        string  `bson:"role" json:"role"` // host, member
}

type Itinerary struct {
	ID           string            `bson:"_id" json:"id"`
	HostUserID   string            `bson:"hostUserId" json:"host_user_id"`
	HostName     string            `bson:"hostName" json:"host_name"`
	Title        string            `bson:"title" json:"title"`
	Date         string            `bson:"date" json:"date"`
	StartTime    string            `bson:"startTime" json:"start_time"`
	BackByTime   string            `bson:"backByTime" json:"back_by_time"`
	Visibility   string            `bson:"visibility" json:"visibility"` // just_me, friends, open
	LockTime     *time.Time        `bson:"lockTime,omitempty" json:"lock_time,omitempty"`
	MaxGroupSize int               `bson:"maxGroupSize" json:"max_group_size"`
	Members      []ItineraryMember `bson:"members" json:"members"`
	Items        []ItineraryItem   `bson:"items" json:"items"`
	Status       string            `bson:"status" json:"status"` // active, completed, cancelled
	CreatedAt    time.Time         `bson:"createdAt" json:"created_at"`
	UpdatedAt    time.Time         `bson:"updatedAt" json:"updated_at"`
}

type ItinerarySummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Date         string    `json:"date"`
	StartTime    string    `json:"start_time"`
	BackByTime   string    `json:"back_by_time"`
	Status       string    `json:"status"`
	MaxGroupSize int       `json:"max_group_size"`
	MemberCount  int       `json:"member_count"`
	StopCount    int       `json:"stop_count"`
	FirstStop    string    `json:"first_stop,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Rating struct {
	ItemID    string    `bson:"_id" json:"item_id"`
	UserID    string    `bson:"userId" json:"user_id"`
	Stars     int       `bson:"stars" json:"stars"` // 1-5
	Tags      []string  `bson:"tags" json:"tags"`
	Note      string    `bson:"note,omitempty" json:"note,omitempty"`
	CreatedAt time.Time `bson:"createdAt" json:"created_at"`
}

type PastEvent struct {
	ItemID       string    `bson:"_id" json:"item_id"`
	EventID      string    `bson:"eventId" json:"event_id"`
	UserID       string    `bson:"userId" json:"user_id"`
	Title        string    `bson:"title" json:"title"`
	Date         time.Time `bson:"date" json:"date"`
	PhotoURL     *string   `bson:"photoUrl,omitempty" json:"photo_url,omitempty"`
	LocationName string    `bson:"locationName" json:"location_name"`
	Rated        bool      `bson:"rated" json:"rated"`
	Rating       *Rating   `bson:"rating,omitempty" json:"rating,omitempty"`
}

type CheckoutIntent struct {
	ID              string    `bson:"_id" json:"id"`
	UserID          string    `bson:"userId" json:"user_id"`
	ItemID          string    `bson:"itemId" json:"item_id"`
	ItemTitle       string    `bson:"itemTitle" json:"item_title"`
	Quantity        int       `bson:"quantity" json:"quantity"`
	UnitPriceCents  int64     `bson:"unitPriceCents" json:"unit_price_cents"`
	FeesCents       int64     `bson:"feesCents" json:"fees_cents"`
	TotalCents      int64     `bson:"totalCents" json:"total_cents"`
	Steps           []string  `bson:"steps" json:"steps"`
	CurrentStep     int       `bson:"currentStep" json:"current_step"`
	Status          string    `bson:"status" json:"status"` // awaiting_approval, completed, cancelled
	PaymentMethodID string    `bson:"paymentMethodId,omitempty" json:"payment_method_id,omitempty"`
	CreatedAt       time.Time `bson:"createdAt" json:"created_at"`
	UpdatedAt       time.Time `bson:"updatedAt" json:"updated_at"`
}

type ForumPost struct {
	ID                string     `bson:"_id" json:"id"`
	UserID            string     `bson:"userId" json:"user_id"`
	AuthorName        string     `bson:"authorName" json:"author_name"`
	AuthorUsername    *string    `bson:"authorUsername" json:"author_username"`
	AuthorPhoto       *string    `bson:"authorPhoto,omitempty" json:"author_photo,omitempty"`
	Type              string     `bson:"type" json:"type"` // im_free, itinerary
	Title             string     `bson:"title" json:"title"`
	Content           string     `bson:"content" json:"content"`
	Lat               float64    `bson:"lat" json:"lat"`
	Lng               float64    `bson:"lng" json:"lng"`
	Visibility        string     `bson:"visibility" json:"visibility"` // friends, everyone
	UntilTime         *time.Time `bson:"untilTime,omitempty" json:"until_time,omitempty"`
	ItineraryID       string     `bson:"itineraryId,omitempty" json:"itinerary_id,omitempty"`
	Tags              []string   `bson:"tags" json:"tags"`
	CostCents         int64      `bson:"costCents" json:"cost_cents"`
	OpenOnly          bool       `bson:"openOnly" json:"open_only"`
	JoinRequestsCount int        `bson:"joinRequestsCount" json:"join_requests_count"`
	CreatedAt         time.Time  `bson:"createdAt" json:"created_at"`
}

type JoinRequest struct {
	ID          string    `bson:"_id" json:"id"`
	PostID      string    `bson:"postId,omitempty" json:"post_id,omitempty"`
	ItineraryID string    `bson:"itineraryId,omitempty" json:"itinerary_id,omitempty"`
	UserID      string    `bson:"userId" json:"user_id"`
	UserName    string    `bson:"userName" json:"user_name"`
	UserPhoto   *string   `bson:"userPhoto,omitempty" json:"user_photo,omitempty"`
	Status      string    `bson:"status" json:"status"` // pending, approved, declined
	CreatedAt   time.Time `bson:"createdAt" json:"created_at"`
}

type Message struct {
	ID          string    `bson:"_id" json:"id"`
	ThreadID    string    `bson:"threadId" json:"thread_id"`
	SenderID    string    `bson:"senderId" json:"sender_id"`
	SenderName  string    `bson:"senderName" json:"sender_name"`
	SenderPhoto *string   `bson:"senderPhoto,omitempty" json:"sender_photo,omitempty"`
	Content     string    `bson:"content" json:"content"`
	Attachments []string  `bson:"attachments,omitempty" json:"attachments,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"created_at"`
}

type Thread struct {
	ID             string         `bson:"_id" json:"id"`
	Type           string         `bson:"type" json:"type"` // group, dm
	Title          string         `bson:"title" json:"title"`
	ParticipantIDs []string       `bson:"participantIds" json:"participant_ids"`
	Participants   []UserSummary  `bson:"participants" json:"participants"`
	LastMessage    *Message       `bson:"lastMessage,omitempty" json:"last_message,omitempty"`
	UnreadCounts   map[string]int `bson:"unreadCounts" json:"unread_counts"`
	CreatedAt      time.Time      `bson:"createdAt" json:"created_at"`
	UpdatedAt      time.Time      `bson:"updatedAt" json:"updated_at"`
}

type GroupPhoto struct {
	ID           string    `bson:"_id" json:"id"`
	GroupID      string    `bson:"groupId" json:"group_id"`
	UploaderID   string    `bson:"uploaderId" json:"uploader_id"`
	UploaderName string    `bson:"uploaderName" json:"uploader_name"`
	PhotoURL     string    `bson:"photoUrl" json:"photo_url"`
	Caption      string    `bson:"caption,omitempty" json:"caption,omitempty"`
	CreatedAt    time.Time `bson:"createdAt" json:"created_at"`
}

type Expense struct {
	ID                  string           `bson:"_id" json:"id"`
	GroupID             string           `bson:"groupId" json:"group_id"`
	PayerID             string           `bson:"payerId" json:"payer_id"`
	PayerName           string           `bson:"payerName" json:"payer_name"`
	What                string           `bson:"what" json:"what"`
	AmountCents         int64            `bson:"amountCents" json:"amount_cents"`
	SplitBetweenUserIDs []string         `bson:"splitBetweenUserIds" json:"split_between_user_ids"`
	Shares              map[string]int64 `bson:"shares" json:"shares"`
	CreatedAt           time.Time        `bson:"createdAt" json:"created_at"`
}

type NetBalance struct {
	FromUserID   string `json:"from_user_id"`
	FromUserName string `json:"from_user_name"`
	ToUserID     string `json:"to_user_id"`
	ToUserName   string `json:"to_user_name"`
	AmountCents  int64  `json:"amount_cents"`
	Text         string `json:"text"`
}

type FriendRequest struct {
	ID         string       `bson:"_id" json:"id"`
	FromUserID string       `bson:"fromUserId" json:"from_user_id"`
	FromUser   *UserSummary `bson:"fromUser,omitempty" json:"from_user,omitempty"`
	ToUserID   string       `bson:"toUserId" json:"to_user_id"`
	ToUser     *UserSummary `bson:"toUser,omitempty" json:"to_user,omitempty"`
	Status     string       `bson:"status" json:"status"` // pending, accepted, declined
	CreatedAt  time.Time    `bson:"createdAt" json:"created_at"`
}

type InviteLink struct {
	ID            string    `bson:"_id" json:"id"`
	Code          string    `bson:"code" json:"code"`
	CreatorUserID string    `bson:"creatorUserId" json:"creator_user_id"`
	InviteURL     string    `bson:"inviteUrl" json:"invite_url"`
	CreatedAt     time.Time `bson:"createdAt" json:"created_at"`
	ExpiresAt     time.Time `bson:"expiresAt" json:"expires_at"`
}
