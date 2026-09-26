package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Read-only lookups GET /search needs from collections the social area
// owns (friendships, friend_requests; schemas in pkg/models). Nothing here
// writes them.

// PeopleRelation is how one person relates to a viewer: none, friend,
// outgoing (the viewer asked them) or incoming (they asked the viewer),
// with the pending request's id for the last two.
type PeopleRelation struct {
	Kind      string
	RequestID string
}

// PeopleRelations reports, for each of otherIDs, how that person relates to
// viewerID; people with no relation map to Kind "none".
func (s *Store) PeopleRelations(ctx context.Context, viewerID string, otherIDs []string) (map[string]PeopleRelation, error) {
	out := make(map[string]PeopleRelation, len(otherIDs))
	if len(otherIDs) == 0 {
		return out, nil
	}
	for _, id := range otherIDs {
		out[id] = PeopleRelation{Kind: "none"}
	}
	pairs := make([]string, 0, len(otherIDs))
	for _, id := range otherIDs {
		pairs = append(pairs, models.FriendshipID(viewerID, id))
	}
	cursor, err := s.db.Collection(CollFriendships).Find(ctx, bson.M{"_id": bson.M{"$in": pairs}})
	if err != nil {
		return nil, err
	}
	var friendships []models.Friendship
	if err := cursor.All(ctx, &friendships); err != nil {
		return nil, err
	}
	for _, f := range friendships {
		for _, id := range f.UserIDs {
			if id != viewerID {
				out[id] = PeopleRelation{Kind: "friend"}
			}
		}
	}
	cursor, err = s.db.Collection(CollFriendRequests).Find(ctx, bson.M{
		"status": models.RequestPending,
		"$or": []bson.M{
			{"fromId": viewerID, "toId": bson.M{"$in": otherIDs}},
			{"toId": viewerID, "fromId": bson.M{"$in": otherIDs}},
		},
	})
	if err != nil {
		return nil, err
	}
	var requests []models.FriendRequest
	if err := cursor.All(ctx, &requests); err != nil {
		return nil, err
	}
	for _, req := range requests {
		other, kind := req.ToID, "outgoing"
		if req.ToID == viewerID {
			other, kind = req.FromID, "incoming"
		}
		if out[other].Kind == "friend" {
			continue
		}
		out[other] = PeopleRelation{Kind: kind, RequestID: req.ID}
	}
	return out, nil
}
