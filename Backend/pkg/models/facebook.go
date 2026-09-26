package models

import "time"

// FacebookAccount is a user's Facebook connection (facebook_accounts,
// _id = user id). The long-lived token is stored encrypted only. FBUserID
// has a sparse unique index, so it is omitted until known.
type FacebookAccount struct {
	UserID         string    `bson:"_id"`
	FBUserID       string    `bson:"fbUserId,omitempty"`
	AccessTokenEnc string    `bson:"accessTokenEnc"`
	TokenExpiresAt time.Time `bson:"tokenExpiresAt"`
	GrantedScopes  []string  `bson:"grantedScopes"`
	DeclinedScopes []string  `bson:"declinedScopes"`
	NeedsReconnect bool      `bson:"needsReconnect"`
	Name           string    `bson:"name"`
	ConnectedAt    time.Time `bson:"connectedAt"`
	UpdatedAt      time.Time `bson:"updatedAt"`
}

// FacebookPage is one liked Page as read from the Graph API.
type FacebookPage struct {
	ID           string     `bson:"id"`
	Name         string     `bson:"name"`
	Category     string     `bson:"category"`
	CategoryList []string   `bson:"categoryList,omitempty"`
	LikedAt      *time.Time `bson:"likedAt,omitempty"`
}

// FacebookImport is the latest import for a user (facebook_imports,
// _id = user id), replaced on each import.
type FacebookImport struct {
	UserID           string         `bson:"_id"`
	ImportedAt       time.Time      `bson:"importedAt"`
	LikedPages       int            `bson:"likedPages"`
	Pages            []FacebookPage `bson:"pages"`
	City             *string        `bson:"city,omitempty"`
	FriendFBIDs      []string       `bson:"friendFbIds"`
	SuggestedRatings map[string]int `bson:"suggestedRatings"`
	Interests        []string       `bson:"interests"`
}
