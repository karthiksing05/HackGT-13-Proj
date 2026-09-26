package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Why a join did not go through; both are ErrConflict for status mapping,
// but the join handler answers them as JoinResult statuses.
var (
	ErrPlanClosed = fmt.Errorf("plan locked or started: %w", ErrConflict)
	ErrPlanFull   = fmt.Errorf("plan full: %w", ErrConflict)
)

// Joins changes who is in a shared itinerary (the itineraries collection
// that the itineraries package owns, through the shared schema) and keeps
// the join_requests record of each join.
type Joins struct{ s *Store }

func (s *Store) Joins() Joins { return Joins{s} }

func (j Joins) plans() *mongo.Collection { return j.s.db.Collection(CollItineraries) }

func (j Joins) records() *mongo.Collection { return j.s.db.Collection(CollJoinRequests) }

// JoinClosed reports whether joining has stopped: the lock time or the
// start has been reached.
func JoinClosed(it *models.Itinerary, now time.Time) bool {
	if it.LockAt != nil && !it.LockAt.After(now) {
		return true
	}
	return !it.Start.After(now)
}

// JoinSpotsLeft is maxGroupSize minus the members (never below 0); nil when the
// plan has no size limit.
func JoinSpotsLeft(it *models.Itinerary) *int {
	if it.MaxGroupSize == nil {
		return nil
	}
	left := max(0, *it.MaxGroupSize-len(it.MemberIDs))
	return &left
}

// AddMember adds userID to an active shared itinerary, atomically refusing a
// locked, started or full one (ErrPlanClosed, ErrPlanFull). A user who is
// already a member gets the itinerary back unchanged (added false); a
// missing or unshared itinerary is ErrNotFound.
func (j Joins) AddMember(ctx context.Context, itineraryID, userID string, now time.Time) (it *models.Itinerary, added bool, err error) {
	filter := forumSharedFilter()
	filter["_id"] = itineraryID
	filter["memberIds"] = bson.M{"$ne": userID}
	filter["start"] = bson.M{"$gt": now}
	filter["$and"] = bson.A{
		bson.M{"$or": bson.A{bson.M{"lockAt": nil}, bson.M{"lockAt": bson.M{"$gt": now}}}},
		bson.M{"$or": bson.A{
			bson.M{"maxGroupSize": nil},
			bson.M{"$expr": bson.M{"$lt": bson.A{bson.M{"$size": "$memberIds"}, "$maxGroupSize"}}},
		}},
	}
	update := bson.M{"$addToSet": bson.M{"memberIds": userID}, "$set": bson.M{"updatedAt": j.s.Now()}}
	var doc models.Itinerary
	err = j.plans().FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if err == nil {
		return &doc, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}
	// Say why: gone, already in, locked or full.
	current, err := j.s.Forum().SharedPlan(ctx, itineraryID)
	if err != nil {
		return nil, false, err
	}
	switch {
	case current.IsMember(userID):
		return current, false, nil
	case JoinClosed(current, now):
		return nil, false, ErrPlanClosed
	}
	if left := JoinSpotsLeft(current); left != nil && *left == 0 {
		return nil, false, ErrPlanFull
	}
	return nil, false, ErrConflict
}

// SetThread records the group thread on the itinerary when it has none yet.
func (j Joins) SetThread(ctx context.Context, itineraryID, threadID string) error {
	_, err := j.plans().UpdateOne(ctx,
		bson.M{"_id": itineraryID, "$or": bson.A{bson.M{"threadId": nil}, bson.M{"threadId": ""}}},
		bson.M{"$set": bson.M{"threadId": threadID}})
	return err
}

// Record notes an accepted join (one record per post and user; a rejoin
// flips it back to accepted).
func (j Joins) Record(ctx context.Context, itineraryID, userID string) error {
	return j.upsertRecord(ctx, itineraryID, userID, models.JoinAccepted, true)
}

// Cancel marks the user's join record cancelled; without one it is a no-op.
func (j Joins) Cancel(ctx context.Context, itineraryID, userID string) error {
	return j.upsertRecord(ctx, itineraryID, userID, models.JoinCancelled, false)
}

func (j Joins) upsertRecord(ctx context.Context, itineraryID, userID, status string, upsert bool) error {
	now := j.s.Now()
	update := bson.M{
		"$set":         bson.M{"status": status, "itineraryId": itineraryID, "updatedAt": now},
		"$setOnInsert": bson.M{"_id": NewID(), "createdAt": now},
	}
	filter := bson.M{"postId": itineraryID, "userId": userID}
	opts := options.UpdateOne().SetUpsert(upsert)
	_, err := j.records().UpdateOne(ctx, filter, update, opts)
	if IsDuplicate(err) {
		// Lost an upsert race on postId_userId_unique: the record exists now.
		_, err = j.records().UpdateOne(ctx, filter, bson.M{"$set": update["$set"]})
	}
	return err
}

// RecordOf returns the user's join record for a post.
func (j Joins) RecordOf(ctx context.Context, postID, userID string) (*models.JoinRequest, error) {
	var rec models.JoinRequest
	if err := decodeOne(j.records().FindOne(ctx, bson.M{"postId": postID, "userId": userID}), &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}
