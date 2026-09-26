package models

import (
	"sort"
	"time"
)

// Post types, join and friend-request statuses as stored.
const (
	PostPlan    = "plan"
	PostFreeNow = "free_now"

	PostVisibilityFriends  = "friends"
	PostVisibilityEveryone = "everyone"

	JoinAccepted  = "accepted"
	JoinCancelled = "cancelled"

	RequestPending   = "pending"
	RequestAccepted  = "accepted"
	RequestDeclined  = "declined"
	RequestCancelled = "cancelled"
)

// ForumPost is the forum_posts document. Only free_now posts are stored:
// plan posts are derived from itineraries (post id = itinerary id). The TTL
// index on expiresAt removes a free post when its "free until" passes.
type ForumPost struct {
	ID         string       `bson:"_id"`  // uuid
	Type       string       `bson:"type"` // free_now
	AuthorID   string       `bson:"authorId"`
	Visibility string       `bson:"visibility"` // friends | everyone
	Location   GeoJSONPoint `bson:"location"`   // [lng, lat]
	AreaLabel  *string      `bson:"areaLabel,omitempty"`
	RadiusMi   *int         `bson:"radiusMi,omitempty"`
	Text       string       `bson:"text"`
	Until      time.Time    `bson:"until"`
	ExpiresAt  time.Time    `bson:"expiresAt"`
	CreatedAt  time.Time    `bson:"createdAt"`
}

// JoinRequest records a join (auto-accepted) or its cancellation.
type JoinRequest struct {
	ID          string    `bson:"_id"` // uuid
	PostID      string    `bson:"postId"`
	ItineraryID string    `bson:"itineraryId"`
	UserID      string    `bson:"userId"`
	Status      string    `bson:"status"` // accepted | cancelled
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

// PlanTogether marks that userID already sent "Plan together" for a post
// (plan_together, _id = postId|userId).
type PlanTogether struct {
	ID        string    `bson:"_id"`
	PostID    string    `bson:"postId"`
	UserID    string    `bson:"userId"`
	ThreadID  string    `bson:"threadId"`
	CreatedAt time.Time `bson:"createdAt"`
}

// Thread is a DM or a group chat (threads). A group thread belongs to one
// itinerary and its id is the group id used by /groups/{id}/…. Sparse unique
// indexes on itineraryId and dmKey require these to be omitted when empty.
type Thread struct {
	ID              string               `bson:"_id"` // uuid
	IsGroup         bool                 `bson:"isGroup"`
	Title           string               `bson:"title,omitempty"`
	MemberIDs       []string             `bson:"memberIds"`
	ItineraryID     string               `bson:"itineraryId,omitempty"`
	DMKey           string               `bson:"dmKey,omitempty"` // DMKey(a, b)
	CreatedBy       string               `bson:"createdBy"`
	LastMessageText string               `bson:"lastMessageText,omitempty"`
	LastSenderID    string               `bson:"lastSenderId,omitempty"`
	LastMessageAt   time.Time            `bson:"lastMessageAt"`
	Unread          map[string]int       `bson:"unread"` // userId → count
	ReadAt          map[string]time.Time `bson:"readAt"` // userId → last read
	Archived        bool                 `bson:"archived,omitempty"`
	CreatedAt       time.Time            `bson:"createdAt"`
	UpdatedAt       time.Time            `bson:"updatedAt"`
}

// DMKey is the order-independent key of a two-person thread.
func DMKey(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	return ids[0] + "|" + ids[1]
}

// Message is one chat message (messages). ClientID makes sends idempotent
// (unique partial index on threadId+senderId+clientId), so omit it when empty.
type Message struct {
	ID       string    `bson:"_id"` // uuid
	ThreadID string    `bson:"threadId"`
	SenderID string    `bson:"senderId"`
	Text     string    `bson:"text"`
	ClientID string    `bson:"clientId,omitempty"`
	SentAt   time.Time `bson:"sentAt"`
}

// Expense is one shared cost in a group (expenses). Settlement rows record
// settle-up payments.
type Expense struct {
	ID          string    `bson:"_id"` // uuid
	GroupID     string    `bson:"groupId"`
	What        string    `bson:"what"`
	AmountCents int       `bson:"amountCents"`
	PayerID     string    `bson:"payerId"`
	SplitAmong  []string  `bson:"splitAmong"`
	Shares      []int     `bson:"shares"` // cents per SplitAmong entry
	CreatedBy   string    `bson:"createdBy"`
	Settlement  bool      `bson:"settlement"`
	CreatedAt   time.Time `bson:"createdAt"`
}

// Friendship links two users (friendships, _id = FriendshipID(a, b)).
type Friendship struct {
	ID        string    `bson:"_id"`
	UserIDs   []string  `bson:"userIds"` // sorted pair
	CreatedAt time.Time `bson:"createdAt"`
}

// FriendshipID is the sorted "<a>|<b>" id of a friendship.
func FriendshipID(a, b string) string { return DMKey(a, b) }

// FriendRequest is a pending or answered request (friend_requests).
type FriendRequest struct {
	ID        string    `bson:"_id"` // uuid
	FromID    string    `bson:"fromId"`
	ToID      string    `bson:"toId"`
	Note      string    `bson:"note"`
	Status    string    `bson:"status"` // pending | accepted | declined | cancelled
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// Invite is a shareable friend link (invites, _id = code).
type Invite struct {
	Code      string    `bson:"_id"` // 8 chars, Crockford base32
	UserID    string    `bson:"userId"`
	Uses      int       `bson:"uses"`
	ExpiresAt time.Time `bson:"expiresAt"`
	CreatedAt time.Time `bson:"createdAt"`
}
