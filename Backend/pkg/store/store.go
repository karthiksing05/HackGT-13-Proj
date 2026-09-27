// Package store is the Mongo persistence layer. Every query takes the
// caller's user id where ownership matters and reports ErrNotFound /
// ErrForbidden / ErrConflict; handlers map those to 404 / 403 / 409.
//
// Layout: one namespace per collection (Users, Tokens, Resets, WebSessions,
// Photos here; Itineraries, Forum, Threads, … in the area agents' files),
// reached through accessor methods on *Store. Area agents add their own
// files (itineraries.go, users_prefs.go, …) with new namespaces or new
// methods on existing ones and never edit store.go, users.go, tokens.go,
// websessions.go or photos.go.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Sentinel errors; wrap them (fmt.Errorf("…: %w", ErrConflict)) to keep the
// status mapping while adding detail.
var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("conflict")

	ErrEmailTaken    = fmt.Errorf("email taken: %w", ErrConflict)
	ErrUsernameTaken = fmt.Errorf("username taken: %w", ErrConflict)
)

// Collection names (backend-contract §2.1).
const (
	CollUsers            = "users"
	CollRefreshTokens    = "refresh_tokens"
	CollResetCodes       = "reset_codes"
	CollWebSessions      = "web_sessions"
	CollDevices          = "devices"
	CollPaymentMethods   = "payment_methods"
	CollPhotos           = "photos"
	CollItineraries      = "itineraries"
	CollItemStates       = "item_states"
	CollRatings          = "ratings"
	CollCheckoutIntents  = "checkout_intents"
	CollCheckoutRuns     = "checkout_runs"
	CollForumPosts       = "forum_posts"
	CollJoinRequests     = "join_requests"
	CollThreads          = "threads"
	CollMessages         = "messages"
	CollExpenses         = "expenses"
	CollFriendships      = "friendships"
	CollFriendRequests   = "friend_requests"
	CollInvites          = "invites"
	CollFacebookAccounts = "facebook_accounts"
	CollFacebookImports  = "facebook_imports"
	CollPlanPools        = "plan_pools"
	CollPlanRuns         = "plan_runs"
	CollPlanTogether     = "plan_together"
	CollActivities       = "activities"
	CollDemoActivities   = "demo_activities"
)

// CatalogCollections are read-only here and never reset or TTL-touched.
var CatalogCollections = []string{CollActivities, CollDemoActivities}

// Store wraps the database and the clock used for createdAt/updatedAt.
type Store struct {
	db  *mongo.Database
	now func() time.Time
}

// New builds a Store; now is the testable clock (nil = time.Now).
func New(db *mongo.Database, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// DB is the underlying database.
func (s *Store) DB() *mongo.Database { return s.db }

// Collection returns a collection by name.
func (s *Store) Collection(name string) *mongo.Collection { return s.db.Collection(name) }

// Now is the store's clock, truncated to milliseconds (BSON datetime precision).
func (s *Store) Now() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// Ping is the /healthz probe.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Client().Ping(ctx, readpref.Primary())
}

// NewID is the _id for every non-user document: a UUIDv7 string.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// IsDuplicate reports a unique-index violation.
func IsDuplicate(err error) bool { return mongo.IsDuplicateKeyError(err) }

// duplicateOn reports whether a duplicate-key error names the given index.
func duplicateOn(err error, indexName string) bool {
	return IsDuplicate(err) && strings.Contains(err.Error(), "index: "+indexName)
}

// ---- indexes -------------------------------------------------------------

type indexSpec struct {
	coll    string
	keys    bson.D
	name    string
	unique  bool
	sparse  bool
	ttl     bool // expireAfterSeconds: 0
	partial bson.D
}

// appIndexes are backend-contract §2.2 for the app collections, with fixed
// names. "Exists with different options" is a startup failure.
var appIndexes = []indexSpec{
	{coll: CollUsers, keys: bson.D{{Key: "email", Value: 1}}, name: "email_unique", unique: true},
	{coll: CollUsers, keys: bson.D{{Key: "usernameLower", Value: 1}}, name: "usernameLower_unique", unique: true},
	{coll: CollUsers, keys: bson.D{{Key: "nameLower", Value: 1}}, name: "nameLower"},
	{coll: CollUsers, keys: bson.D{{Key: "roles", Value: 1}}, name: "roles"},

	{coll: CollRefreshTokens, keys: bson.D{{Key: "tokenHash", Value: 1}}, name: "tokenHash_unique", unique: true},
	{coll: CollRefreshTokens, keys: bson.D{{Key: "userId", Value: 1}}, name: "userId"},
	{coll: CollRefreshTokens, keys: bson.D{{Key: "familyId", Value: 1}}, name: "familyId"},
	{coll: CollRefreshTokens, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},

	{coll: CollResetCodes, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},
	{coll: CollWebSessions, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},

	{coll: CollDevices, keys: bson.D{{Key: "token", Value: 1}}, name: "token_unique", unique: true},
	{coll: CollDevices, keys: bson.D{{Key: "userId", Value: 1}}, name: "userId"},

	{coll: CollPaymentMethods, keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: 1}}, name: "userId_createdAt"},

	{coll: CollPhotos, keys: bson.D{{Key: "ownerId", Value: 1}}, name: "ownerId"},
	{coll: CollPhotos, keys: bson.D{{Key: "groupId", Value: 1}, {Key: "createdAt", Value: -1}}, name: "groupId_createdAt", sparse: true},

	{coll: CollItineraries, keys: bson.D{{Key: "memberIds", Value: 1}, {Key: "start", Value: -1}}, name: "memberIds_start"},
	{coll: CollItineraries, keys: bson.D{{Key: "hostId", Value: 1}, {Key: "start", Value: -1}}, name: "hostId_start"},
	{coll: CollItineraries, keys: bson.D{{Key: "items.id", Value: 1}}, name: "items_id"},
	{coll: CollItineraries, keys: bson.D{{Key: "status", Value: 1}, {Key: "backBy", Value: 1}}, name: "status_backBy"},
	{coll: CollItineraries, keys: bson.D{{Key: "postId", Value: 1}}, name: "postId", sparse: true},
	{coll: CollItineraries, keys: bson.D{{Key: "threadId", Value: 1}}, name: "threadId", sparse: true},

	{coll: CollItemStates, keys: bson.D{{Key: "userId", Value: 1}, {Key: "itemId", Value: 1}}, name: "userId_itemId_unique", unique: true},
	{coll: CollItemStates, keys: bson.D{{Key: "itineraryId", Value: 1}}, name: "itineraryId"},

	{coll: CollRatings, keys: bson.D{{Key: "userId", Value: 1}, {Key: "itemId", Value: 1}}, name: "userId_itemId_unique", unique: true},
	{coll: CollRatings, keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}, name: "userId_createdAt"},

	{coll: CollCheckoutIntents, keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}, name: "userId_createdAt"},
	{coll: CollCheckoutIntents, keys: bson.D{{Key: "state", Value: 1}, {Key: "nextTransitionAt", Value: 1}}, name: "state_nextTransitionAt"},

	{coll: CollForumPosts, keys: bson.D{{Key: "location", Value: "2dsphere"}}, name: "location_2dsphere"},
	{coll: CollForumPosts, keys: bson.D{{Key: "authorId", Value: 1}}, name: "authorId"},
	{coll: CollForumPosts, keys: bson.D{{Key: "type", Value: 1}, {Key: "startsAt", Value: 1}}, name: "type_startsAt"},
	{coll: CollForumPosts, keys: bson.D{{Key: "itineraryId", Value: 1}}, name: "itineraryId_unique", unique: true, sparse: true},
	{coll: CollForumPosts, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},

	{coll: CollJoinRequests, keys: bson.D{{Key: "postId", Value: 1}, {Key: "userId", Value: 1}}, name: "postId_userId_unique", unique: true},
	{coll: CollJoinRequests, keys: bson.D{{Key: "userId", Value: 1}}, name: "userId"},

	{coll: CollThreads, keys: bson.D{{Key: "memberIds", Value: 1}, {Key: "lastMessageAt", Value: -1}}, name: "memberIds_lastMessageAt"},
	{coll: CollThreads, keys: bson.D{{Key: "itineraryId", Value: 1}}, name: "itineraryId_unique", unique: true, sparse: true},
	{coll: CollThreads, keys: bson.D{{Key: "dmKey", Value: 1}}, name: "dmKey_unique", unique: true, sparse: true},

	{coll: CollMessages, keys: bson.D{{Key: "threadId", Value: 1}, {Key: "sentAt", Value: -1}}, name: "threadId_sentAt"},
	{coll: CollMessages, keys: bson.D{{Key: "threadId", Value: 1}, {Key: "senderId", Value: 1}, {Key: "clientId", Value: 1}}, name: "threadId_senderId_clientId_unique", unique: true,
		partial: bson.D{{Key: "clientId", Value: bson.D{{Key: "$exists", Value: true}}}}},

	{coll: CollExpenses, keys: bson.D{{Key: "groupId", Value: 1}, {Key: "createdAt", Value: 1}}, name: "groupId_createdAt"},

	{coll: CollFriendships, keys: bson.D{{Key: "userIds", Value: 1}}, name: "userIds"},

	{coll: CollFriendRequests, keys: bson.D{{Key: "toId", Value: 1}, {Key: "status", Value: 1}}, name: "toId_status"},
	{coll: CollFriendRequests, keys: bson.D{{Key: "fromId", Value: 1}, {Key: "status", Value: 1}}, name: "fromId_status"},
	{coll: CollFriendRequests, keys: bson.D{{Key: "fromId", Value: 1}, {Key: "toId", Value: 1}}, name: "fromId_toId_pending_unique", unique: true,
		partial: bson.D{{Key: "status", Value: "pending"}}},

	{coll: CollInvites, keys: bson.D{{Key: "userId", Value: 1}}, name: "userId"},
	{coll: CollInvites, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},

	{coll: CollFacebookAccounts, keys: bson.D{{Key: "fbUserId", Value: 1}}, name: "fbUserId_unique", unique: true, sparse: true},

	{coll: CollPlanPools, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},
	{coll: CollPlanRuns, keys: bson.D{{Key: "expiresAt", Value: 1}}, name: "expiresAt_ttl", ttl: true},
	{coll: CollPlanRuns, keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}, name: "userId_createdAt"},
}

// catalogIndexes are created only when no index with the same keys exists,
// under Mongo's default names; the seed already carries most of them and TTL
// indexes on the catalogs are never touched here.
var catalogIndexes = []bson.D{
	{{Key: "location", Value: "2dsphere"}},
	{{Key: "city", Value: 1}, {Key: "kind", Value: 1}, {Key: "start", Value: 1}},
	{{Key: "name", Value: 1}},
}

func (spec indexSpec) model() mongo.IndexModel {
	opts := options.Index().SetName(spec.name)
	if spec.unique {
		opts.SetUnique(true)
	}
	if spec.sparse {
		opts.SetSparse(true)
	}
	if spec.ttl {
		opts.SetExpireAfterSeconds(0)
	}
	if spec.partial != nil {
		opts.SetPartialFilterExpression(spec.partial)
	}
	return mongo.IndexModel{Keys: spec.keys, Options: opts}
}

// EnsureIndexes creates every §2.2 index. It is idempotent; an index that
// exists with different options is an error (fatal at startup).
func (s *Store) EnsureIndexes(ctx context.Context) error {
	byColl := map[string][]mongo.IndexModel{}
	var order []string
	for _, spec := range appIndexes {
		if _, ok := byColl[spec.coll]; !ok {
			order = append(order, spec.coll)
		}
		byColl[spec.coll] = append(byColl[spec.coll], spec.model())
	}
	for _, coll := range order {
		if _, err := s.db.Collection(coll).Indexes().CreateMany(ctx, byColl[coll]); err != nil {
			return fmt.Errorf("ensure indexes on %s: %w", coll, err)
		}
	}
	for _, coll := range CatalogCollections {
		if err := s.ensureCatalogIndexes(ctx, coll); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureCatalogIndexes(ctx context.Context, coll string) error {
	existing, err := s.existingKeyPatterns(ctx, coll)
	if err != nil {
		return fmt.Errorf("list indexes on %s: %w", coll, err)
	}
	var missing []mongo.IndexModel
	for _, keys := range catalogIndexes {
		if !existing[keyPattern(keys)] {
			missing = append(missing, mongo.IndexModel{Keys: keys})
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if _, err := s.db.Collection(coll).Indexes().CreateMany(ctx, missing); err != nil {
		return fmt.Errorf("ensure catalog indexes on %s: %w", coll, err)
	}
	return nil
}

// existingKeyPatterns lists a collection's indexes by their key pattern.
func (s *Store) existingKeyPatterns(ctx context.Context, coll string) (map[string]bool, error) {
	cursor, err := s.db.Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	var docs []struct {
		Key bson.D `bson:"key"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		out[keyPattern(d.Key)] = true
	}
	return out, nil
}

// keyPattern renders a key document as "field:value,field:value" (order kept).
func keyPattern(keys bson.D) string {
	parts := make([]string, 0, len(keys))
	for _, e := range keys {
		parts = append(parts, fmt.Sprintf("%s:%v", e.Key, e.Value))
	}
	return strings.Join(parts, ",")
}

// decodeOne maps a missing document to ErrNotFound.
func decodeOne(res *mongo.SingleResult, v any) error {
	if err := res.Decode(v); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
