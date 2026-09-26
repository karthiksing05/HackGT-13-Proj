package store

import (
	"Backend/pkg/models"
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// threadPathNotViable is Mongo's code for a $set/$inc through a null field
// (a thread written with unread: null).
const threadPathNotViable = 28

// Threads is the threads collection: DMs (dmKey) and group chats (one per
// itinerary; the thread id is the group id of /groups/{id}/…). Archived
// threads read as missing.
type Threads struct{ s *Store }

func (s *Store) Threads() Threads { return Threads{s} }

func (t Threads) coll() *mongo.Collection { return t.s.db.Collection(CollThreads) }

// threadLive excludes archived threads.
func threadLive(filter bson.M) bson.M {
	filter["archived"] = bson.M{"$ne": true}
	return filter
}

// Get loads a thread by id.
func (t Threads) Get(ctx context.Context, id string) (*models.Thread, error) {
	var th models.Thread
	if err := decodeOne(t.coll().FindOne(ctx, threadLive(bson.M{"_id": id})), &th); err != nil {
		return nil, err
	}
	return &th, nil
}

// ForMember loads a thread the user belongs to; anyone else gets ErrNotFound.
func (t Threads) ForMember(ctx context.Context, id, userID string) (*models.Thread, error) {
	var th models.Thread
	if err := decodeOne(t.coll().FindOne(ctx, threadLive(bson.M{"_id": id, "memberIds": userID})), &th); err != nil {
		return nil, err
	}
	return &th, nil
}

// GroupForMember is ForMember for group threads only (/groups/{id}/…).
func (t Threads) GroupForMember(ctx context.Context, id, userID string) (*models.Thread, error) {
	var th models.Thread
	err := decodeOne(t.coll().FindOne(ctx, threadLive(bson.M{"_id": id, "memberIds": userID, "isGroup": true})), &th)
	if err != nil {
		return nil, err
	}
	return &th, nil
}

// DM loads the two-person thread of a and b without creating it.
func (t Threads) DM(ctx context.Context, a, b string) (*models.Thread, error) {
	var th models.Thread
	if err := decodeOne(t.coll().FindOne(ctx, threadLive(bson.M{"dmKey": models.DMKey(a, b)})), &th); err != nil {
		return nil, err
	}
	return &th, nil
}

// ListForUser lists the user's threads, most recent message first.
func (t Threads) ListForUser(ctx context.Context, userID string, limit int) ([]*models.Thread, error) {
	if limit <= 0 {
		limit = 200
	}
	cursor, err := t.coll().Find(ctx, threadLive(bson.M{"memberIds": userID}),
		options.Find().SetSort(bson.D{{Key: "lastMessageAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	list := []*models.Thread{}
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ByItinerary maps itinerary ids to their group threads.
func (t Threads) ByItinerary(ctx context.Context, itineraryIDs []string) (map[string]*models.Thread, error) {
	out := make(map[string]*models.Thread, len(itineraryIDs))
	if len(itineraryIDs) == 0 {
		return out, nil
	}
	cursor, err := t.coll().Find(ctx, threadLive(bson.M{"itineraryId": bson.M{"$in": itineraryIDs}}))
	if err != nil {
		return nil, err
	}
	var list []*models.Thread
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	for _, th := range list {
		out[th.ItineraryID] = th
	}
	return out, nil
}

// EnsureDM returns the two-person thread of a and b, creating it (created
// true) when they have none.
func (t Threads) EnsureDM(ctx context.Context, a, b string) (*models.Thread, bool, error) {
	key := models.DMKey(a, b)
	now := t.s.Now()
	update := bson.M{"$setOnInsert": bson.M{
		"_id": NewID(), "isGroup": false, "memberIds": []string{a, b}, "createdBy": a,
		"lastMessageAt": now, "unread": bson.M{}, "readAt": bson.M{}, "createdAt": now, "updatedAt": now,
	}}
	return t.upsert(ctx, bson.M{"dmKey": key}, update)
}

// EnsureGroup returns the group thread of an itinerary, creating it (titled
// after the itinerary) when missing, and adds every itinerary member to it.
func (t Threads) EnsureGroup(ctx context.Context, it *models.Itinerary) (*models.Thread, bool, error) {
	now := t.s.Now()
	update := bson.M{
		"$setOnInsert": bson.M{
			"_id": NewID(), "isGroup": true, "title": it.Title, "createdBy": it.HostID,
			"lastMessageAt": now, "unread": bson.M{}, "readAt": bson.M{}, "createdAt": now,
		},
		"$addToSet": bson.M{"memberIds": bson.M{"$each": forumIDs(it.MemberIDs)}},
		"$set":      bson.M{"updatedAt": now},
	}
	return t.upsert(ctx, bson.M{"itineraryId": it.ID}, update)
}

// upsert applies an upsert on a unique key (dmKey, itineraryId), retrying
// once when a concurrent upsert created the document first.
func (t Threads) upsert(ctx context.Context, filter, update bson.M) (*models.Thread, bool, error) {
	res, err := t.coll().UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if IsDuplicate(err) {
		res, err = t.coll().UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	}
	if err != nil {
		return nil, false, err
	}
	var th models.Thread
	if err := decodeOne(t.coll().FindOne(ctx, filter), &th); err != nil {
		return nil, false, err
	}
	return &th, res.UpsertedCount == 1, nil
}

// AddMembers puts users into a thread.
func (t Threads) AddMembers(ctx context.Context, threadID string, userIDs ...string) (*models.Thread, error) {
	return t.update(ctx, threadID, bson.M{
		"$addToSet": bson.M{"memberIds": bson.M{"$each": forumIDs(userIDs)}},
		"$set":      bson.M{"updatedAt": t.s.Now()},
	})
}

// RemoveMember takes a user out of a thread with their unread count.
func (t Threads) RemoveMember(ctx context.Context, threadID, userID string) (*models.Thread, error) {
	return t.update(ctx, threadID, bson.M{
		"$pull":  bson.M{"memberIds": userID},
		"$unset": bson.M{"unread." + userID: "", "readAt." + userID: ""},
		"$set":   bson.M{"updatedAt": t.s.Now()},
	})
}

// Touch records a new message: last message fields, +1 unread for every
// other member, and the sender's own thread read up to it.
func (t Threads) Touch(ctx context.Context, th *models.Thread, msg *models.Message) (*models.Thread, error) {
	set := bson.M{
		"lastMessageText": msg.Text, "lastSenderId": msg.SenderID, "lastMessageAt": msg.SentAt,
		"updatedAt": t.s.Now(), "unread." + msg.SenderID: 0, "readAt." + msg.SenderID: msg.SentAt,
	}
	update := bson.M{"$set": set}
	inc := bson.M{}
	for _, id := range th.MemberIDs {
		if id != msg.SenderID {
			inc["unread."+id] = 1
		}
	}
	if len(inc) > 0 {
		update["$inc"] = inc
	}
	return t.update(ctx, th.ID, update)
}

// MarkRead clears the user's unread count.
func (t Threads) MarkRead(ctx context.Context, threadID, userID string) (*models.Thread, error) {
	now := t.s.Now()
	return t.update(ctx, threadID, bson.M{"$set": bson.M{"unread." + userID: 0, "readAt." + userID: now}})
}

// update applies an update to a live thread and returns it. A thread whose
// unread/readAt maps were stored as null is repaired once and retried.
func (t Threads) update(ctx context.Context, threadID string, update bson.M) (*models.Thread, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	filter := threadLive(bson.M{"_id": threadID})
	var th models.Thread
	err := t.coll().FindOneAndUpdate(ctx, filter, update, opts).Decode(&th)
	var se mongo.ServerError
	if errors.As(err, &se) && se.HasErrorCode(threadPathNotViable) {
		if err := t.repairMaps(ctx, threadID); err != nil {
			return nil, err
		}
		err = t.coll().FindOneAndUpdate(ctx, filter, update, opts).Decode(&th)
	}
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &th, nil
}

// repairMaps turns null unread/readAt maps into empty documents.
func (t Threads) repairMaps(ctx context.Context, threadID string) error {
	for _, field := range []string{"unread", "readAt"} {
		_, err := t.coll().UpdateOne(ctx,
			bson.M{"_id": threadID, field: bson.M{"$type": "null"}},
			bson.M{"$set": bson.M{field: bson.M{}}})
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- group photos (read side of the shared photos collection) ------------

// GroupPhotoCounts is the number of photos of each group (album chips).
func (p Photos) GroupPhotoCounts(ctx context.Context, groupIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	cursor, err := p.coll().Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"groupId": bson.M{"$in": groupIDs}}}},
		{{Key: "$group", Value: bson.M{"_id": "$groupId", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `bson:"_id"`
		N  int    `bson:"n"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.N
	}
	return out, nil
}

// GroupPhotoMeta loads one photo of a group without its bytes.
func (p Photos) GroupPhotoMeta(ctx context.Context, groupID, photoID string) (*models.Photo, error) {
	var photo models.Photo
	err := decodeOne(p.coll().FindOne(ctx, bson.M{"_id": photoID, "groupId": groupID},
		options.FindOne().SetProjection(bson.M{"bytes": 0})), &photo)
	if err != nil {
		return nil, err
	}
	return &photo, nil
}
