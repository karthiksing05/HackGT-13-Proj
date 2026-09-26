package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Package models holds Mongo document schemas only (no json tags). Handlers
// render them into pkg/contract per viewer.

// HomeBase is the user's default start place (users.homeBase; home_base on the wire).
type HomeBase struct {
	Name string  `bson:"name"`
	Lat  float64 `bson:"lat"`
	Lng  float64 `bson:"lng"`
}

// UserPrefs is the contract Preferences shape stored with snake_case keys
// (users.prefs). Company == "" means preferences were never saved.
type UserPrefs struct {
	Ratings                   map[string]int    `bson:"ratings"`
	Company                   string            `bson:"company"`
	Pace                      string            `bson:"pace"`
	Spend                     string            `bson:"spend"`
	Flexibility               string            `bson:"flexibility"`
	SplitStyle                string            `bson:"split_style"`
	PreferFree                bool              `bson:"prefer_free"`
	Answers                   map[string]string `bson:"answers"` // perfect_afternoon, never_do, plan_around
	InstantCheckout           bool              `bson:"instant_checkout"`
	InstantCheckoutLimitCents int               `bson:"instant_checkout_limit_cents"`
}

// IsSet reports whether preferences were ever saved.
func (p UserPrefs) IsSet() bool { return p.Company != "" }

// Flexible is the planner's "a bit over the budget is OK" flag.
func (p UserPrefs) Flexible() bool { return p.Flexibility == "bit_over_ok" }

// BudgetTier maps the spend tier to the Create-flow budget index (0 Free … 3 $$$).
func (p UserPrefs) BudgetTier() int {
	switch p.Spend {
	case "free_only":
		return 0
	case "15_to_40":
		return 2
	case "over_40":
		return 3
	}
	return 1
}

// UserTaste is learned taste: seeded from prefs, moved by ratings.
type UserTaste struct {
	Tags        map[string]float64 `bson:"tags"` // 0..1 per tag
	AvoidTags   []string           `bson:"avoidTags,omitempty"`
	RatingCount int                `bson:"ratingCount"`
}

// IntegrationState is one calendar connection (users.integrations.google / .outlook).
type IntegrationState struct {
	Connected bool       `bson:"connected"`
	At        *time.Time `bson:"at,omitempty"`
}

type UserIntegrations struct {
	Google  IntegrationState `bson:"google"`
	Outlook IntegrationState `bson:"outlook"`
}

// User is the users document (backend-contract §2.1). Ids elsewhere refer to
// it by ID.Hex(). Embedding fields keep the names the ML workstream writes.
type User struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	Email         string        `bson:"email"` // lowercase, unique
	PasswordHash  string        `bson:"passwordHash"`
	Name          string        `bson:"name"`
	NameLower     string        `bson:"nameLower"`
	Username      string        `bson:"username"` // no "@"
	UsernameLower string        `bson:"usernameLower"`
	PhotoID       *string       `bson:"photoId,omitempty"`
	AvatarColor   string        `bson:"avatarColor"` // ink | sage | clay | forest | sand
	Status        string        `bson:"status"`      // open | friends_only | busy
	BirthDate     *time.Time    `bson:"birthDate,omitempty"`
	School        *string       `bson:"school,omitempty"`
	SetupComplete bool          `bson:"setupComplete"`
	Catalog       string        `bson:"catalog"` // activities | demo_activities
	City          string        `bson:"city"`
	HomeBase      *HomeBase     `bson:"homeBase,omitempty"`

	Prefs        UserPrefs        `bson:"prefs"`
	Taste        UserTaste        `bson:"taste"`
	Integrations UserIntegrations `bson:"integrations"`

	PositiveText      string     `bson:"positiveText,omitempty"`
	NegativeText      string     `bson:"negativeText,omitempty"`
	Embedding         []float64  `bson:"embedding,omitempty"`
	PositiveEmbedding []float64  `bson:"positiveEmbedding,omitempty"`
	NegativeEmbedding []float64  `bson:"negativeEmbedding,omitempty"`
	EmbeddingModel    string     `bson:"embeddingModel,omitempty"`
	ProfileTextHash   string     `bson:"profileTextHash,omitempty"`
	ProfileUpdatedAt  *time.Time `bson:"profileUpdatedAt,omitempty"`
	FacebookInterests []string   `bson:"facebookInterests,omitempty"`

	LastLocation *GeoJSONPoint `bson:"lastLocation,omitempty"` // [lng, lat], saved while planning
	Roles        []string      `bson:"roles,omitempty"`        // bot | demo

	CreatedAt    time.Time `bson:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt"`
	LastActiveAt time.Time `bson:"lastActiveAt"`
}

// HasRole reports whether the user carries a role (bot, demo).
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}
