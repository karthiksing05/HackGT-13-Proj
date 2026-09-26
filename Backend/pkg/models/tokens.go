package models

import "time"

// RefreshToken is one session (refresh_tokens). Only the sha256 of the token
// is stored; rotation links the family so a replayed token revokes all of it.
type RefreshToken struct {
	ID         string     `bson:"_id"` // uuid
	TokenHash  string     `bson:"tokenHash"`
	UserID     string     `bson:"userId"`
	FamilyID   string     `bson:"familyId"`
	ExpiresAt  time.Time  `bson:"expiresAt"`
	RevokedAt  *time.Time `bson:"revokedAt,omitempty"`
	ReplacedBy *string    `bson:"replacedBy,omitempty"`
	CreatedAt  time.Time  `bson:"createdAt"`
}

// ResetCode is the pending password reset for an email (reset_codes, _id = email).
type ResetCode struct {
	Email      string     `bson:"_id"`
	CodeHash   string     `bson:"codeHash"`
	Attempts   int        `bson:"attempts"`
	ExpiresAt  time.Time  `bson:"expiresAt"`
	VerifiedAt *time.Time `bson:"verifiedAt,omitempty"`
	ResetJTI   string     `bson:"resetJti,omitempty"` // jti of the reset token issued by verify
	CreatedAt  time.Time  `bson:"createdAt"`
}

// WebSession is a one-time token for hosted pages and OAuth state
// (web_sessions, _id = token). Purposes: payment_setup, calendar_connect,
// facebook_state, fb_deletion.
type WebSession struct {
	Token     string     `bson:"_id"`
	UserID    string     `bson:"userId"`
	Purpose   string     `bson:"purpose"`
	Provider  string     `bson:"provider,omitempty"`
	Rerequest bool       `bson:"rerequest,omitempty"`
	ExpiresAt time.Time  `bson:"expiresAt"`
	UsedAt    *time.Time `bson:"usedAt,omitempty"`
	CreatedAt time.Time  `bson:"createdAt"`
}
