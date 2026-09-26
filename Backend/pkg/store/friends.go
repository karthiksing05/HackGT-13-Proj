package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Friends is the friendships collection (_id = FriendshipID(a, b)) and the
// friend_requests collection (at most one pending request per direction).
type Friends struct{ s *Store }

func (s *Store) Friends() Friends { return Friends{s} }

func (f Friends) coll() *mongo.Collection { return f.s.db.Collection(CollFriendships) }

func (f Friends) requests() *mongo.Collection { return f.s.db.Collection(CollFriendRequests) }

// Of lists the user's friendships, newest first.
func (f Friends) Of(ctx context.Context, userID string) ([]*models.Friendship, error) {
	cursor, err := f.coll().Find(ctx, bson.M{"userIds": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	list := []*models.Friendship{}
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// IDs is the user's friends' ids.
func (f Friends) IDs(ctx context.Context, userID string) ([]string, error) {
	list, err := f.Of(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, fs := range list {
		ids = append(ids, FriendshipOther(fs, userID))
	}
	return ids, nil
}

// FriendshipOther is the other person of a friendship.
func FriendshipOther(fs *models.Friendship, userID string) string {
	for _, id := range fs.UserIDs {
		if id != userID {
			return id
		}
	}
	return ""
}

// Get loads the friendship of a and b; ErrNotFound when they are not friends.
func (f Friends) Get(ctx context.Context, a, b string) (*models.Friendship, error) {
	var fs models.Friendship
	if err := decodeOne(f.coll().FindOne(ctx, bson.M{"_id": models.FriendshipID(a, b)}), &fs); err != nil {
		return nil, err
	}
	return &fs, nil
}

// ByPairs loads the friendships among the given pair ids (FriendshipID),
// keyed by that id; pairs that are not friends are absent.
func (f Friends) ByPairs(ctx context.Context, pairIDs []string) (map[string]*models.Friendship, error) {
	out := make(map[string]*models.Friendship, len(pairIDs))
	if len(pairIDs) == 0 {
		return out, nil
	}
	cursor, err := f.coll().Find(ctx, bson.M{"_id": bson.M{"$in": pairIDs}})
	if err != nil {
		return nil, err
	}
	var list []*models.Friendship
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	for _, fs := range list {
		out[fs.ID] = fs
	}
	return out, nil
}

// Befriend makes a and b friends; created is false when they already were.
// Pending requests between them are marked accepted.
func (f Friends) Befriend(ctx context.Context, a, b string) (*models.Friendship, bool, error) {
	id := models.FriendshipID(a, b)
	ids := []string{a, b}
	sort.Strings(ids)
	now := f.s.Now()
	res, err := f.coll().UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$setOnInsert": bson.M{"userIds": ids, "createdAt": now}}, options.UpdateOne().SetUpsert(true))
	if IsDuplicate(err) {
		res, err = &mongo.UpdateResult{}, nil // a concurrent accept made it first
	}
	if err != nil {
		return nil, false, err
	}
	_, err = f.requests().UpdateMany(ctx,
		bson.M{"status": models.RequestPending, "$or": bson.A{bson.M{"fromId": a, "toId": b}, bson.M{"fromId": b, "toId": a}}},
		bson.M{"$set": bson.M{"status": models.RequestAccepted, "updatedAt": now}})
	if err != nil {
		return nil, false, err
	}
	fs, err := f.Get(ctx, a, b)
	if err != nil {
		return nil, false, err
	}
	return fs, res.UpsertedCount == 1, nil
}

// Unfriend ends a friendship; removed is false when there was none.
func (f Friends) Unfriend(ctx context.Context, a, b string) (bool, error) {
	res, err := f.coll().DeleteOne(ctx, bson.M{"_id": models.FriendshipID(a, b)})
	if err != nil {
		return false, err
	}
	return res.DeletedCount == 1, nil
}

// ---- requests -------------------------------------------------------------

// Request loads a friend request by id.
func (f Friends) Request(ctx context.Context, id string) (*models.FriendRequest, error) {
	var req models.FriendRequest
	if err := decodeOne(f.requests().FindOne(ctx, bson.M{"_id": id}), &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// Pending lists the user's pending requests: incoming (to them) and
// outgoing (from them), newest first.
func (f Friends) Pending(ctx context.Context, userID string) (incoming, outgoing []*models.FriendRequest, err error) {
	cursor, err := f.requests().Find(ctx,
		bson.M{"status": models.RequestPending, "$or": bson.A{bson.M{"toId": userID}, bson.M{"fromId": userID}}},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}))
	if err != nil {
		return nil, nil, err
	}
	var list []*models.FriendRequest
	if err := cursor.All(ctx, &list); err != nil {
		return nil, nil, err
	}
	incoming, outgoing = []*models.FriendRequest{}, []*models.FriendRequest{}
	for _, req := range list {
		if req.ToID == userID {
			incoming = append(incoming, req)
		} else {
			outgoing = append(outgoing, req)
		}
	}
	return incoming, outgoing, nil
}

// PendingFrom is the pending request from one user to another.
func (f Friends) PendingFrom(ctx context.Context, fromID, toID string) (*models.FriendRequest, error) {
	var req models.FriendRequest
	filter := bson.M{"fromId": fromID, "toId": toID, "status": models.RequestPending}
	if err := decodeOne(f.requests().FindOne(ctx, filter), &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// CreateRequest stores a pending request; when one from fromID to toID is
// already pending it is returned instead (created false).
func (f Friends) CreateRequest(ctx context.Context, fromID, toID, note string) (*models.FriendRequest, bool, error) {
	now := f.s.Now()
	req := &models.FriendRequest{ID: NewID(), FromID: fromID, ToID: toID, Note: note, Status: models.RequestPending,
		CreatedAt: now, UpdatedAt: now}
	_, err := f.requests().InsertOne(ctx, req)
	if err == nil {
		return req, true, nil
	}
	if !IsDuplicate(err) {
		return nil, false, err
	}
	existing, err := f.PendingFrom(ctx, fromID, toID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

// Answer moves a pending request addressed to toID to accepted or declined.
// Answering the same way twice is fine; anything else is ErrNotFound.
func (f Friends) Answer(ctx context.Context, id, toID, status string) (*models.FriendRequest, error) {
	return f.transition(ctx, bson.M{"_id": id, "toId": toID}, status)
}

// Cancel withdraws the sender's own pending request (idempotent).
func (f Friends) Cancel(ctx context.Context, id, fromID string) (*models.FriendRequest, error) {
	return f.transition(ctx, bson.M{"_id": id, "fromId": fromID}, models.RequestCancelled)
}

func (f Friends) transition(ctx context.Context, filter bson.M, status string) (*models.FriendRequest, error) {
	pending := bson.M{"status": models.RequestPending}
	for k, v := range filter {
		pending[k] = v
	}
	var req models.FriendRequest
	err := f.requests().FindOneAndUpdate(ctx, pending,
		bson.M{"$set": bson.M{"status": status, "updatedAt": f.s.Now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&req)
	if err == nil {
		return &req, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	if err := decodeOne(f.requests().FindOne(ctx, filter), &req); err != nil {
		return nil, err
	}
	if req.Status != status {
		return nil, ErrNotFound
	}
	return &req, nil
}

// SocialRelation is how another user relates to a viewer (search rows).
type SocialRelation struct {
	Kind      string // none | friend | outgoing | incoming
	RequestID string // the pending request, for outgoing and incoming
}

// Relations tells, for each of ids, whether the viewer is their friend or
// has a pending request with them.
func (f Friends) Relations(ctx context.Context, viewerID string, ids []string) (map[string]SocialRelation, error) {
	out := make(map[string]SocialRelation, len(ids))
	for _, id := range ids {
		out[id] = SocialRelation{Kind: "none"}
	}
	if len(ids) == 0 {
		return out, nil
	}
	pairs := make([]string, 0, len(ids))
	for _, id := range ids {
		pairs = append(pairs, models.FriendshipID(viewerID, id))
	}
	friends, err := f.ByPairs(ctx, pairs)
	if err != nil {
		return nil, err
	}
	cursor, err := f.requests().Find(ctx, bson.M{"status": models.RequestPending, "$or": bson.A{
		bson.M{"fromId": viewerID, "toId": bson.M{"$in": ids}},
		bson.M{"toId": viewerID, "fromId": bson.M{"$in": ids}},
	}})
	if err != nil {
		return nil, err
	}
	var pending []*models.FriendRequest
	if err := cursor.All(ctx, &pending); err != nil {
		return nil, err
	}
	for _, req := range pending {
		if req.FromID == viewerID {
			out[req.ToID] = SocialRelation{Kind: "outgoing", RequestID: req.ID}
		} else if out[req.FromID].Kind != "outgoing" {
			out[req.FromID] = SocialRelation{Kind: "incoming", RequestID: req.ID}
		}
	}
	for _, id := range ids {
		if friends[models.FriendshipID(viewerID, id)] != nil {
			out[id] = SocialRelation{Kind: "friend"}
		}
	}
	return out, nil
}
