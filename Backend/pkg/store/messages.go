package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Messages is the messages collection. Pages are ordered by (sentAt, _id);
// ids are UUIDv7, so they also rise with time.
type Messages struct{ s *Store }

func (s *Store) Messages() Messages { return Messages{s} }

func (m Messages) coll() *mongo.Collection { return m.s.db.Collection(CollMessages) }

// Insert stores a message, assigning its id and sentAt. A message with a
// ClientID the sender already used in the thread is not stored again: msg
// becomes the earlier one and created is false.
func (m Messages) Insert(ctx context.Context, msg *models.Message) (created bool, err error) {
	msg.ID = NewID()
	msg.SentAt = m.s.Now()
	_, err = m.coll().InsertOne(ctx, msg)
	if err == nil {
		return true, nil
	}
	if !IsDuplicate(err) || msg.ClientID == "" {
		return false, err
	}
	var existing models.Message
	filter := bson.M{"threadId": msg.ThreadID, "senderId": msg.SenderID, "clientId": msg.ClientID}
	if err := decodeOne(m.coll().FindOne(ctx, filter), &existing); err != nil {
		return false, err
	}
	*msg = existing
	return false, nil
}

// PageBefore returns up to limit messages of a thread, oldest first, that
// come before the message beforeID (the newest page when beforeID is "").
// An unknown beforeID yields an empty page.
func (m Messages) PageBefore(ctx context.Context, threadID, beforeID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 30
	}
	filter := bson.M{"threadId": threadID}
	if beforeID != "" {
		var anchor models.Message
		err := decodeOne(m.coll().FindOne(ctx, bson.M{"_id": beforeID, "threadId": threadID}), &anchor)
		if errors.Is(err, ErrNotFound) {
			return []*models.Message{}, nil
		}
		if err != nil {
			return nil, err
		}
		filter["$or"] = bson.A{
			bson.M{"sentAt": bson.M{"$lt": anchor.SentAt}},
			bson.M{"sentAt": anchor.SentAt, "_id": bson.M{"$lt": anchor.ID}},
		}
	}
	cursor, err := m.coll().Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "sentAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	page := []*models.Message{}
	if err := cursor.All(ctx, &page); err != nil {
		return nil, err
	}
	slices.Reverse(page)
	return page, nil
}
