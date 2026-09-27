package store

import (
	"Backend/pkg/models"
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Profiles is what pkg/profiles reads and writes to keep users' taste vectors current: the user
// with the profile bookkeeping, their rated stops joined with their catalog, one catalog activity,
// and the vector writes. A catalog is written only through SetActivityEmbedding, guarded by the
// text hash.
type Profiles struct{ s *Store }

func (s *Store) Profiles() Profiles { return Profiles{s} }

func (p Profiles) users() *mongo.Collection { return p.s.db.Collection(CollUsers) }

// catalog resolves a user's catalog name ("" is the default catalog).
func (p Profiles) catalog(name string) (*mongo.Collection, error) {
	switch name {
	case "":
		return p.s.db.Collection(ActivityCollection), nil
	case CollActivities, CollDemoActivities:
		return p.s.db.Collection(name), nil
	}
	return nil, fmt.Errorf("unknown catalog %q", name)
}

// catalogID is an activity id as stored: an ObjectID for a hex id, else the string itself.
func catalogID(id string) any {
	if oid, ok := objectID(id); ok {
		return oid
	}
	return id
}

func idString(v any) string {
	switch id := v.(type) {
	case bson.ObjectID:
		return id.Hex()
	case string:
		return id
	}
	return fmt.Sprint(v)
}

// ProfileUser is a user plus profileInputHash: the fingerprint of the inputs the stored profile was
// built from (preferences, Facebook interests, rated stops). Only pkg/profiles reads it, so it is
// not part of models.User.
type ProfileUser struct {
	models.User      `bson:",inline"`
	ProfileInputHash string `bson:"profileInputHash,omitempty"`
}

// User loads a user with the profile bookkeeping; an unknown id is ErrNotFound.
func (p Profiles) User(ctx context.Context, id string) (*ProfileUser, error) {
	oid, ok := objectID(id)
	if !ok {
		return nil, ErrNotFound
	}
	var user ProfileUser
	if err := decodeOne(p.users().FindOne(ctx, bson.M{"_id": oid}), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// RatedStop is one rating with the rated activity's category and tags, which stay empty when the
// stop has no activity or the activity is not in the catalog.
type RatedStop struct {
	ActivityID   string
	Stars        int
	Tags         []string // the rating's own tags ("Great people", "Too crowded", …)
	Category     string
	ActivityTags []string
	RatedAt      time.Time
}

// RatedStops are the user's ratings, most recently changed first (at most limit; 0 = all), joined
// with the activities of their catalog.
func (p Profiles) RatedStops(ctx context.Context, userID, catalog string, limit int) ([]RatedStop, error) {
	coll, err := p.catalog(catalog)
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := p.s.db.Collection(CollRatings).Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	var ratings []models.Rating
	if err := cursor.All(ctx, &ratings); err != nil {
		return nil, err
	}
	ids := make([]any, 0, len(ratings))
	for _, r := range ratings {
		if r.ActivityID != "" {
			ids = append(ids, catalogID(r.ActivityID))
		}
	}
	activities := map[string]catalogDoc{}
	if len(ids) > 0 {
		cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"category": 1, "tags": 1}))
		if err != nil {
			return nil, err
		}
		var docs []catalogDoc
		if err := cursor.All(ctx, &docs); err != nil {
			return nil, err
		}
		for _, d := range docs {
			activities[idString(d.ID)] = d
		}
	}
	out := make([]RatedStop, 0, len(ratings))
	for _, r := range ratings {
		stop := RatedStop{ActivityID: r.ActivityID, Stars: r.Stars, Tags: r.Tags, RatedAt: r.UpdatedAt}
		if a, ok := activities[r.ActivityID]; ok {
			stop.Category, stop.ActivityTags = a.Category, a.Tags
		}
		out = append(out, stop)
	}
	return out, nil
}

// CatalogActivity is the part of a catalog document the profile code reads.
type CatalogActivity struct {
	ID                string
	Category          string
	Tags              []string
	Embedding         []float64
	EmbeddingModel    string
	EmbeddingText     string
	EmbeddingTextHash string
}

type catalogDoc struct {
	ID                any       `bson:"_id"`
	Category          string    `bson:"category"`
	Tags              []string  `bson:"tags"`
	Embedding         []float64 `bson:"embedding"`
	EmbeddingModel    string    `bson:"embeddingModel"`
	EmbeddingText     string    `bson:"embeddingText"`
	EmbeddingTextHash string    `bson:"embeddingTextHash"`
}

var catalogActivityProjection = bson.M{
	"category": 1, "tags": 1, "embedding": 1, "embeddingModel": 1, "embeddingText": 1, "embeddingTextHash": 1,
}

// Activity loads one activity from a catalog; a missing one is ErrNotFound.
func (p Profiles) Activity(ctx context.Context, catalog, id string) (*CatalogActivity, error) {
	coll, err := p.catalog(catalog)
	if err != nil {
		return nil, err
	}
	var d catalogDoc
	res := coll.FindOne(ctx, bson.M{"_id": catalogID(id)}, options.FindOne().SetProjection(catalogActivityProjection))
	if err := decodeOne(res, &d); err != nil {
		return nil, err
	}
	return &CatalogActivity{
		ID: idString(d.ID), Category: d.Category, Tags: d.Tags, Embedding: d.Embedding,
		EmbeddingModel: d.EmbeddingModel, EmbeddingText: d.EmbeddingText, EmbeddingTextHash: d.EmbeddingTextHash,
	}, nil
}

// ProfileFields are what a profile refresh stores on the user.
type ProfileFields struct {
	PositiveText      string
	NegativeText      string
	PositiveEmbedding []float64
	NegativeEmbedding []float64
	EmbeddingModel    string
	ProfileTextHash   string
	ProfileInputHash  string
}

// SetProfile stores a rebuilt profile (profileUpdatedAt = now). It also drops the legacy
// `embedding` field, which only ever held a fabricated vector.
func (p Profiles) SetProfile(ctx context.Context, userID string, f ProfileFields) error {
	return p.updateUser(ctx, userID, bson.M{
		"$set": bson.M{
			"positiveText":      f.PositiveText,
			"negativeText":      f.NegativeText,
			"positiveEmbedding": f.PositiveEmbedding,
			"negativeEmbedding": f.NegativeEmbedding,
			"embeddingModel":    f.EmbeddingModel,
			"profileTextHash":   f.ProfileTextHash,
			"profileInputHash":  f.ProfileInputHash,
			"profileUpdatedAt":  p.s.Now(),
		},
		"$unset": bson.M{"embedding": ""},
	})
}

// ClearProfileHash marks the stored profile as out of date (the next refresh rebuilds it) and
// keeps its vectors.
func (p Profiles) ClearProfileHash(ctx context.Context, userID string) error {
	return p.updateUser(ctx, userID, bson.M{"$unset": bson.M{"profileTextHash": "", "profileInputHash": ""}})
}

// SetUserVector stores one of the user's vectors after a rating moved it.
func (p Profiles) SetUserVector(ctx context.Context, userID string, negative bool, vector []float64) error {
	field := "positiveEmbedding"
	if negative {
		field = "negativeEmbedding"
	}
	return p.updateUser(ctx, userID, bson.M{"$set": bson.M{field: vector, "profileUpdatedAt": p.s.Now()}})
}

func (p Profiles) updateUser(ctx context.Context, userID string, update bson.M) error {
	oid, ok := objectID(userID)
	if !ok {
		return ErrNotFound
	}
	res, err := p.users().UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SetActivityEmbedding stores an activity's vector computed for the text whose hash is textHash,
// with its embeddingModel and embeddingMeta sub-document (meta: the shape every writer uses, see
// ml.ActivityEmbeddingMeta). The write is guarded as the Python backfill's: only while
// embeddingTextHash still names that text and the activity has no vector or one computed for
// another text. It reports whether it wrote.
func (p Profiles) SetActivityEmbedding(ctx context.Context, catalog, activityID, textHash string, vector []float64, model string, meta any) (bool, error) {
	coll, err := p.catalog(catalog)
	if err != nil {
		return false, err
	}
	filter := bson.M{
		"_id":               catalogID(activityID),
		"embeddingTextHash": textHash,
		"$or": bson.A{
			bson.M{"embedding": nil}, // missing or null
			bson.M{"embeddingMeta.textHash": bson.M{"$exists": true, "$ne": textHash}},
		},
	}
	res, err := coll.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"embedding": vector, "embeddingModel": model, "embeddingMeta": meta}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}
