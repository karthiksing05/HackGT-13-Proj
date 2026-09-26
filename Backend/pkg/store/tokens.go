package store

import (
	"Backend/pkg/models"
	"Backend/pkg/util"
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrTokenReused marks a revoked refresh token presented again; the whole
// family has been revoked. It is an ErrNotFound for status mapping.
var ErrTokenReused = fmt.Errorf("refresh token reused: %w", ErrNotFound)

const refreshTokenBytes = 32

// Tokens is the refresh_tokens collection: rotation with family reuse detection.
type Tokens struct{ s *Store }

func (s *Store) Tokens() Tokens { return Tokens{s} }

func (t Tokens) coll() *mongo.Collection { return t.s.db.Collection(CollRefreshTokens) }

// Issue creates a new session family and returns the raw token (only its
// sha256 is stored).
func (t Tokens) Issue(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	raw, err := t.insert(ctx, userID, NewID(), ttl)
	return raw, err
}

func (t Tokens) insert(ctx context.Context, userID, familyID string, ttl time.Duration) (string, error) {
	raw := util.RandomToken(refreshTokenBytes)
	now := t.s.Now()
	doc := models.RefreshToken{
		ID:        NewID(),
		TokenHash: util.SHA256Hex(raw),
		UserID:    userID,
		FamilyID:  familyID,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if _, err := t.coll().InsertOne(ctx, doc); err != nil {
		return "", err
	}
	return raw, nil
}

// Rotate exchanges a live token for a new one in the same family. An
// unknown or expired token is ErrNotFound; a revoked one revokes its whole
// family and is ErrTokenReused.
func (t Tokens) Rotate(ctx context.Context, raw string, ttl time.Duration) (newRaw, userID string, err error) {
	var current models.RefreshToken
	if err := decodeOne(t.coll().FindOne(ctx, bson.M{"tokenHash": util.SHA256Hex(raw)}), &current); err != nil {
		return "", "", err
	}
	now := t.s.Now()
	if current.RevokedAt != nil {
		if err := t.RevokeFamily(ctx, current.FamilyID); err != nil {
			return "", "", err
		}
		return "", "", ErrTokenReused
	}
	if !current.ExpiresAt.After(now) {
		return "", "", ErrNotFound
	}
	newRaw, err = t.insert(ctx, current.UserID, current.FamilyID, ttl)
	if err != nil {
		return "", "", err
	}
	// Claim the old token; losing the race to a concurrent rotation counts as reuse.
	replacement := util.SHA256Hex(newRaw)
	res, err := t.coll().UpdateOne(ctx,
		bson.M{"_id": current.ID, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": now, "replacedBy": replacement}})
	if err != nil {
		return "", "", err
	}
	if res.MatchedCount == 0 {
		if err := t.RevokeFamily(ctx, current.FamilyID); err != nil {
			return "", "", err
		}
		return "", "", ErrTokenReused
	}
	return newRaw, current.UserID, nil
}

// Revoke ends one session; an unknown token is not an error.
func (t Tokens) Revoke(ctx context.Context, raw string) error {
	_, err := t.coll().UpdateOne(ctx,
		bson.M{"tokenHash": util.SHA256Hex(raw), "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": t.s.Now()}})
	return err
}

// RevokeFamily ends every token descended from one issue.
func (t Tokens) RevokeFamily(ctx context.Context, familyID string) error {
	_, err := t.coll().UpdateMany(ctx,
		bson.M{"familyId": familyID, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": t.s.Now()}})
	return err
}

// RevokeAll signs a user out everywhere (password reset).
func (t Tokens) RevokeAll(ctx context.Context, userID string) error {
	_, err := t.coll().UpdateMany(ctx,
		bson.M{"userId": userID, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": t.s.Now()}})
	return err
}

// Resets is the reset_codes collection (one pending code per email).
type Resets struct{ s *Store }

func (s *Store) Resets() Resets { return Resets{s} }

func (r Resets) coll() *mongo.Collection { return r.s.db.Collection(CollResetCodes) }

// Create replaces any pending code for the email with a fresh one.
func (r Resets) Create(ctx context.Context, email, codeHash string, ttl time.Duration) error {
	now := r.s.Now()
	doc := models.ResetCode{Email: email, CodeHash: codeHash, ExpiresAt: now.Add(ttl), CreatedAt: now}
	_, err := r.coll().ReplaceOne(ctx, bson.M{"_id": email}, doc, options.Replace().SetUpsert(true))
	return err
}

func (r Resets) Get(ctx context.Context, email string) (*models.ResetCode, error) {
	var doc models.ResetCode
	if err := decodeOne(r.coll().FindOne(ctx, bson.M{"_id": email}), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// RecordAttempt counts a wrong code.
func (r Resets) RecordAttempt(ctx context.Context, email string) error {
	_, err := r.coll().UpdateOne(ctx, bson.M{"_id": email}, bson.M{"$inc": bson.M{"attempts": 1}})
	return err
}

// MarkVerified records the reset token (by jti) issued for a verified code.
func (r Resets) MarkVerified(ctx context.Context, email, jti string) error {
	res, err := r.coll().UpdateOne(ctx, bson.M{"_id": email},
		bson.M{"$set": bson.M{"verifiedAt": r.s.Now(), "resetJti": jti}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Consume deletes the verified code whose reset token is jti (single use);
// ErrNotFound when it was already used, never verified or replaced.
func (r Resets) Consume(ctx context.Context, email, jti string) error {
	res, err := r.coll().DeleteOne(ctx, bson.M{"_id": email, "resetJti": jti, "verifiedAt": bson.M{"$exists": true}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete drops a pending code (after a successful reset or on demand).
func (r Resets) Delete(ctx context.Context, email string) error {
	_, err := r.coll().DeleteOne(ctx, bson.M{"_id": email})
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	return nil
}
