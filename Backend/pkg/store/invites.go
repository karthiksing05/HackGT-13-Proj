package store

import (
	"Backend/pkg/models"
	"context"
	"crypto/rand"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// inviteAlphabet is Crockford's base32 (no I, L, O, U).
const inviteAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Invites is the invites collection: shareable friend links keyed by an
// 8-character code; the TTL index removes them at expiresAt.
type Invites struct{ s *Store }

func (s *Store) Invites() Invites { return Invites{s} }

func (i Invites) coll() *mongo.Collection { return i.s.db.Collection(CollInvites) }

// Live is the user's newest invite that has not expired.
func (i Invites) Live(ctx context.Context, userID string, now time.Time) (*models.Invite, error) {
	var inv models.Invite
	err := decodeOne(i.coll().FindOne(ctx, bson.M{"userId": userID, "expiresAt": bson.M{"$gt": now}},
		options.FindOne().SetSort(bson.D{{Key: "expiresAt", Value: -1}})), &inv)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// Create issues a new code for the user valid for ttl.
func (i Invites) Create(ctx context.Context, userID string, ttl time.Duration) (*models.Invite, error) {
	now := i.s.Now()
	for range 5 {
		inv := &models.Invite{Code: NewInviteCode(), UserID: userID, ExpiresAt: now.Add(ttl), CreatedAt: now}
		_, err := i.coll().InsertOne(ctx, inv)
		if err == nil {
			return inv, nil
		}
		if !IsDuplicate(err) {
			return nil, err
		}
	}
	return nil, ErrConflict
}

// Get loads an invite by code (normalized: case, hyphens and Crockford's
// look-alikes are forgiven). Expired invites are returned; callers check.
func (i Invites) Get(ctx context.Context, code string) (*models.Invite, error) {
	norm := NormalizeInviteCode(code)
	if len(norm) != 8 {
		return nil, ErrNotFound
	}
	var inv models.Invite
	if err := decodeOne(i.coll().FindOne(ctx, bson.M{"_id": norm}), &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// Use counts one accepted invite.
func (i Invites) Use(ctx context.Context, code string) error {
	_, err := i.coll().UpdateOne(ctx, bson.M{"_id": code}, bson.M{"$inc": bson.M{"uses": 1}})
	return err
}

// NewInviteCode is 8 random Crockford base32 characters (40 bits).
func NewInviteCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("store: crypto/rand failed: " + err.Error())
	}
	for k := range b {
		b[k] = inviteAlphabet[b[k]&31]
	}
	return string(b)
}

// NormalizeInviteCode uppercases a code, drops hyphens and spaces and maps
// Crockford's look-alikes (O → 0, I and L → 1).
func NormalizeInviteCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	return b.String()
}
