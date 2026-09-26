package store

import (
	"Backend/pkg/models"
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Ratings is the ratings collection: one user's stars, tags and note for
// one stop, unique on {userId, itemId} (_id = PairID(userId, itemId) when
// created here; reads and writes match on the pair, like item_states).
// activityId is copied from the item so taste-vector refreshes can find
// the catalog activity.
type Ratings struct{ s *Store }

func (s *Store) Ratings() Ratings { return Ratings{s} }

func (r Ratings) coll() *mongo.Collection { return r.s.db.Collection(CollRatings) }

// Upsert saves userID's rating of an item and returns the rating it
// replaced (nil for a first rating).
func (r Ratings) Upsert(ctx context.Context, rating models.Rating) (*models.Rating, error) {
	now := r.s.Now()
	if rating.Tags == nil {
		rating.Tags = []string{}
	}
	set := bson.M{"itineraryId": rating.ItineraryID, "stars": rating.Stars, "tags": rating.Tags, "updatedAt": now}
	unset := bson.M{}
	if rating.Note != nil {
		set["note"] = *rating.Note
	} else {
		unset["note"] = ""
	}
	if rating.ActivityID != "" {
		set["activityId"] = rating.ActivityID
	} else {
		unset["activityId"] = ""
	}
	update := bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"_id": models.PairID(rating.UserID, rating.ItemID), "createdAt": now},
	}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	var prev models.Rating
	err := r.coll().FindOneAndUpdate(ctx, bson.M{"userId": rating.UserID, "itemId": rating.ItemID}, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before)).Decode(&prev)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &prev, nil
}

// ForUser loads userID's ratings of the given items, keyed by item id.
func (r Ratings) ForUser(ctx context.Context, userID string, itemIDs []string) (map[string]*models.Rating, error) {
	out := make(map[string]*models.Rating, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	docs, err := r.find(ctx, bson.M{"userId": userID, "itemId": bson.M{"$in": itemIDs}})
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		out[doc.ItemID] = doc
	}
	return out, nil
}

// RatedItemIDs is every item userID has rated.
func (r Ratings) RatedItemIDs(ctx context.Context, userID string) ([]string, error) {
	cursor, err := r.coll().Find(ctx, bson.M{"userId": userID}, options.Find().SetProjection(bson.M{"itemId": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ItemID string `bson:"itemId"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ItemID)
	}
	return ids, nil
}

// ListByUser is userID's ratings, newest first (at most limit), for
// taste-vector refreshes.
func (r Ratings) ListByUser(ctx context.Context, userID string, limit int) ([]*models.Rating, error) {
	if limit <= 0 {
		limit = 20
	}
	return r.find(ctx, bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)))
}

func (r Ratings) find(ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]*models.Rating, error) {
	cursor, err := r.coll().Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	docs := []*models.Rating{}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}
