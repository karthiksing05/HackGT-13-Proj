package store

import (
	"Backend/pkg/democlock"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// BusinessNow is the store clock as the request's account lives it: Now,
// or for a demo account (DEMO_DATE set, see pkg/democlock) the demo date at
// the real time of day. It stamps what people see and what orders their
// plans, chats and posts (sentAt, the createdAt of posts, plans, expenses,
// ratings, friendships, …). Tokens, sessions, codes, account metadata and
// every TTL expiresAt keep Now: MongoDB expires documents by the wall clock.
func (s *Store) BusinessNow(ctx context.Context) time.Time { return democlock.Now(ctx, s.Now()) }

// RealAt is the wall-clock instant of the business instant t for the
// request's account (t itself for everyone but demo accounts): the TTL
// expiry of a business deadline, so the document goes when the deadline
// passes for its owner.
func (s *Store) RealAt(ctx context.Context, t time.Time) time.Time {
	return democlock.RealAt(ctx, t, s.Now())
}

// Roles is a user's roles (demo, bot), read without the rest of the
// document; the demo clock asks it for signed-in requests it has not cached.
func (u Users) Roles(ctx context.Context, id string) ([]string, error) {
	oid, ok := objectID(id)
	if !ok {
		return nil, ErrNotFound
	}
	var doc struct {
		Roles []string `bson:"roles"`
	}
	if err := decodeOne(u.coll().FindOne(ctx, bson.M{"_id": oid},
		options.FindOne().SetProjection(bson.M{"roles": 1})), &doc); err != nil {
		return nil, err
	}
	return doc.Roles, nil
}
