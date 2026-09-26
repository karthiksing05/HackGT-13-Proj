package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func init() {
	appIndexes = append(appIndexes, indexSpec{coll: CollPlanTogether,
		keys: bson.D{{Key: "postId", Value: 1}, {Key: "userId", Value: 1}}, name: "postId_userId_unique", unique: true})
}

// PlanTogetherMarks is the plan_together collection: which posts a user has
// already sent "Plan together" for (_id = PairID(postId, userId)).
type PlanTogetherMarks struct{ s *Store }

func (s *Store) PlanTogether() PlanTogetherMarks { return PlanTogetherMarks{s} }

func (p PlanTogetherMarks) coll() *mongo.Collection { return p.s.db.Collection(CollPlanTogether) }

// Mark records that userID sent "Plan together" for postID through threadID;
// created is false when it was already recorded.
func (p PlanTogetherMarks) Mark(ctx context.Context, postID, userID, threadID string) (bool, error) {
	id := models.PairID(postID, userID)
	res, err := p.coll().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$setOnInsert": bson.M{
		"postId": postID, "userId": userID, "threadId": threadID, "createdAt": p.s.BusinessNow(ctx),
	}}, options.UpdateOne().SetUpsert(true))
	if IsDuplicate(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return res.UpsertedCount == 1, nil
}

// Sent reports which of postIDs the user has sent "Plan together" for.
func (p PlanTogetherMarks) Sent(ctx context.Context, userID string, postIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(postIDs))
	for _, postID := range postIDs {
		ids = append(ids, models.PairID(postID, userID))
	}
	cursor, err := p.coll().Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, options.Find().SetProjection(bson.M{"postId": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		PostID string `bson:"postId"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PostID] = true
	}
	return out, nil
}
