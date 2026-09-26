package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Devices is the devices collection: push tokens the app registers. A token
// identifies one app install, so it belongs to whichever account registered
// it last. Nothing sends push yet; the tokens are kept for when it does.
type Devices struct{ s *Store }

func (s *Store) Devices() Devices { return Devices{s} }

func (d Devices) coll() *mongo.Collection { return d.s.db.Collection(CollDevices) }

// Upsert registers token for userID; a token another account registered
// moves to this one.
func (d Devices) Upsert(ctx context.Context, userID, token, platform string) error {
	upsert := func() error {
		_, err := d.coll().UpdateOne(ctx, bson.M{"token": token},
			bson.M{
				"$set":         bson.M{"userId": userID, "platform": platform},
				"$setOnInsert": bson.M{"_id": NewID(), "createdAt": d.s.Now()},
			},
			options.UpdateOne().SetUpsert(true))
		return err
	}
	err := upsert()
	if IsDuplicate(err) {
		// Two first registrations of the same token raced; update the winner.
		// MongoDB 4.2+ retries this itself, so this only matters on older servers.
		err = upsert()
	}
	return err
}

// Delete unregisters one of the caller's tokens; an unknown token or one
// registered to another account is ErrNotFound.
func (d Devices) Delete(ctx context.Context, userID, token string) error {
	res, err := d.coll().DeleteOne(ctx, bson.M{"token": token, "userId": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ForUser lists a user's registered devices, oldest first.
func (d Devices) ForUser(ctx context.Context, userID string) ([]*models.Device, error) {
	cursor, err := d.coll().Find(ctx, bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(100))
	if err != nil {
		return nil, err
	}
	devices := []*models.Device{}
	if err := cursor.All(ctx, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}
