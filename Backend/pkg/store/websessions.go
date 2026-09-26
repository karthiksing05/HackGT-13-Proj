package store

import (
	"Backend/pkg/models"
	"Backend/pkg/util"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Web-session purposes shared by the hosted pages and OAuth flows.
const (
	PurposePaymentSetup    = "payment_setup"
	PurposeCalendarConnect = "calendar_connect"
	PurposeFacebookState   = "facebook_state"
	PurposeFacebookDelete  = "fb_deletion"
)

const webSessionBytes = 32

// WebSessions is the web_sessions collection: one-time tokens for hosted
// pages (?t=) and OAuth state, consumed exactly once.
type WebSessions struct{ s *Store }

func (s *Store) WebSessions() WebSessions { return WebSessions{s} }

func (w WebSessions) coll() *mongo.Collection { return w.s.db.Collection(CollWebSessions) }

// Create issues a token for userID and purpose (provider optional) valid for ttl.
func (w WebSessions) Create(ctx context.Context, userID, purpose, provider string, ttl time.Duration) (string, error) {
	return w.Insert(ctx, models.WebSession{UserID: userID, Purpose: purpose, Provider: provider}, ttl)
}

// Insert stores a prepared session (extra fields such as Rerequest set by
// the caller), filling token and timestamps; it returns the token.
func (w WebSessions) Insert(ctx context.Context, sess models.WebSession, ttl time.Duration) (string, error) {
	now := w.s.Now()
	sess.Token = util.RandomToken(webSessionBytes)
	sess.CreatedAt = now
	sess.ExpiresAt = now.Add(ttl)
	sess.UsedAt = nil
	if _, err := w.coll().InsertOne(ctx, sess); err != nil {
		return "", err
	}
	return sess.Token, nil
}

// Peek reads a live (unused, unexpired) session without consuming it, for
// pages that render a form before the POST consumes the token.
func (w WebSessions) Peek(ctx context.Context, token, purpose string) (*models.WebSession, error) {
	var sess models.WebSession
	err := decodeOne(w.coll().FindOne(ctx, w.liveFilter(token, purpose)), &sess)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// Consume marks a live session used and returns it; a used, expired, unknown
// or wrong-purpose token is ErrNotFound.
func (w WebSessions) Consume(ctx context.Context, token, purpose string) (*models.WebSession, error) {
	var sess models.WebSession
	err := w.coll().FindOneAndUpdate(ctx, w.liveFilter(token, purpose),
		bson.M{"$set": bson.M{"usedAt": w.s.Now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&sess)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sess, nil
}

func (w WebSessions) liveFilter(token, purpose string) bson.M {
	return bson.M{
		"_id":       token,
		"purpose":   purpose,
		"usedAt":    bson.M{"$exists": false},
		"expiresAt": bson.M{"$gt": w.s.Now()},
	}
}
