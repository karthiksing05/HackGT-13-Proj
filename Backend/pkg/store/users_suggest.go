package store

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"Backend/pkg/models"
)

// Suggestable loads the people who may be suggested to someone in catalog:
// accounts on the same catalog (CatalogFor: the demo cast or everyone else)
// that have a taste vector, are not bots and are not in exclude. Each comes with its
// likes and dislikes vectors; limit caps the pool (default 300).
func (u Users) Suggestable(ctx context.Context, catalog string, exclude []string, limit int) ([]*models.User, error) {
	if limit <= 0 {
		limit = 300
	}
	filter := bson.M{
		"positiveEmbedding": bson.M{"$exists": true, "$ne": bson.A{}},
	}
	if catalog == CollDemoActivities {
		filter["roles"] = bson.M{"$eq": "demo", "$ne": "bot"}
	} else {
		filter["roles"] = bson.M{"$nin": bson.A{"demo", "bot"}}
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
