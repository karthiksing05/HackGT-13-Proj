package store

import (
	"Backend/pkg/models"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// PresenceFacts is what a friend's status line is made of besides their
// account status: a live free-now post and a sidequest running right now.
type PresenceFacts struct {
	FreeUntil *time.Time
	Sidequest *models.Itinerary
}

// Presence gathers PresenceFacts for many users at now.
func (u Users) Presence(ctx context.Context, userIDs []string, now time.Time) (map[string]PresenceFacts, error) {
	out := make(map[string]PresenceFacts, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	posts, err := u.s.Forum().LiveFreePostsBy(ctx, userIDs, now)
	if err != nil {
		return nil, err
	}
	for _, post := range posts {
		facts := out[post.AuthorID]
		if facts.FreeUntil == nil || post.Until.After(*facts.FreeUntil) {
			until := post.Until
			facts.FreeUntil = &until
		}
		out[post.AuthorID] = facts
	}
	cursor, err := u.s.db.Collection(CollItineraries).Find(ctx, bson.M{
		"status":    models.ItineraryActive,
		"memberIds": bson.M{"$in": userIDs},
		"start":     bson.M{"$lte": now},
		"backBy":    bson.M{"$gt": now},
	}, options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}).
		SetProjection(bson.M{"items": 0, "plan": 0}))
	if err != nil {
		return nil, err
	}
	var running []*models.Itinerary
	if err := cursor.All(ctx, &running); err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		wanted[id] = true
	}
	for _, it := range running {
		for _, member := range it.MemberIDs {
			if !wanted[member] {
				continue
			}
			facts := out[member]
			if facts.Sidequest == nil {
				facts.Sidequest = it
				out[member] = facts
			}
		}
	}
	return out, nil
}
