package store

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"Backend/pkg/models"
)

// Suggestable loads the people who may be suggested outside the demo:
// accounts that have a taste vector, are not the demo cast (demo account,
// bots) and are not in exclude. Each comes with its
// likes and dislikes vectors; limit caps the pool (default 300).
func (u Users) Suggestable(ctx context.Context, exclude []string, limit int) ([]*models.User, error) {
	if limit <= 0 {
		limit = 300
	}
	filter := bson.M{
		"positiveEmbedding": bson.M{"$exists": true, "$ne": bson.A{}},
		"roles":             bson.M{"$nin": bson.A{"bot", "demo"}},
	}
	if oids := objectIDs(exclude); len(oids) > 0 {
		filter["_id"] = bson.M{"$nin": oids}
	}
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "lastActiveAt", Value: -1}})
	cursor, err := u.coll().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	users := []*models.User{}
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// objectIDs parses hex ids, skipping malformed ones.
func objectIDs(ids []string) []bson.ObjectID {
	out := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, ok := objectID(id); ok {
			out = append(out, oid)
		}
	}
	return out
}
