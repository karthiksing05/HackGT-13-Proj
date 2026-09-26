package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Photos is the photos collection (avatar and group photos as bytes).
type Photos struct{ s *Store }

func (s *Store) Photos() Photos { return Photos{s} }

func (p Photos) coll() *mongo.Collection { return p.s.db.Collection(CollPhotos) }

// Put stores a photo, assigning its id and createdAt (business time: an
// album shows it) and recording the size.
func (p Photos) Put(ctx context.Context, photo *models.Photo) error {
	if photo.ID == "" {
		photo.ID = NewID()
	}
	photo.Size = len(photo.Bytes)
	photo.CreatedAt = p.s.BusinessNow(ctx)
	_, err := p.coll().InsertOne(ctx, photo)
	return err
}

// Get loads a photo with its bytes.
func (p Photos) Get(ctx context.Context, id string) (*models.Photo, error) {
	var photo models.Photo
	if err := decodeOne(p.coll().FindOne(ctx, bson.M{"_id": id}), &photo); err != nil {
		return nil, err
	}
	return &photo, nil
}

// Delete removes a photo; ErrNotFound when it does not exist.
func (p Photos) Delete(ctx context.Context, id string) error {
	res, err := p.coll().DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListForGroup lists a group's photos newest first without their bytes.
func (p Photos) ListForGroup(ctx context.Context, groupID string, limit int) ([]*models.Photo, error) {
	if limit <= 0 {
		limit = 200
	}
	cursor, err := p.coll().Find(ctx, bson.M{"groupId": groupID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)).
			SetProjection(bson.M{"bytes": 0}))
	if err != nil {
		return nil, err
	}
	photos := []*models.Photo{}
	if err := cursor.All(ctx, &photos); err != nil {
		return nil, err
	}
	return photos, nil
}

// CountForGroup is the "N photos" chip.
func (p Photos) CountForGroup(ctx context.Context, groupID string) (int, error) {
	n, err := p.coll().CountDocuments(ctx, bson.M{"groupId": groupID})
	return int(n), err
}
