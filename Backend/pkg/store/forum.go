package store

import (
	"Backend/pkg/models"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// forumEarthRadiusMi converts a radius in miles to radians for $centerSphere.
const forumEarthRadiusMi = 3963.2

// One live free-now post per author: a double tap cannot leave two behind.
func init() {
	appIndexes = append(appIndexes, indexSpec{coll: CollForumPosts,
		keys: bson.D{{Key: "authorId", Value: 1}, {Key: "type", Value: 1}}, name: "authorId_type_free_unique", unique: true,
		partial: bson.D{{Key: "type", Value: models.PostFreeNow}}})
}

// Forum is the forum_posts collection, which stores only free-now posts,
// plus the reads that derive plan posts from itineraries (a plan post is an
// itinerary whose visibility is not just_me; post id = itinerary id).
type Forum struct{ s *Store }

func (s *Store) Forum() Forum { return Forum{s} }

func (f Forum) coll() *mongo.Collection { return f.s.db.Collection(CollForumPosts) }

func (f Forum) plans() *mongo.Collection { return f.s.db.Collection(CollItineraries) }

// ReplaceFreePost makes post the author's only free-now post, assigning its
// id, type, expiry (the TTL index removes it at Until) and createdAt.
func (f Forum) ReplaceFreePost(ctx context.Context, post *models.ForumPost) error {
	post.ID = NewID()
	post.Type = models.PostFreeNow
	post.ExpiresAt = post.Until
	post.CreatedAt = f.s.Now()
	// Each lost race means a concurrent post by the same author was stored
	// (and that request finished), so n simultaneous posts need n attempts.
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := f.coll().DeleteMany(ctx, bson.M{"authorId": post.AuthorID, "type": models.PostFreeNow}); err != nil {
			return err
		}
		_, err := f.coll().InsertOne(ctx, post)
		if err == nil {
			return nil
		}
		if !IsDuplicate(err) {
			return err
		}
	}
	return ErrConflict
}

// LiveFreePost is the author's free-now post that has not ended yet.
func (f Forum) LiveFreePost(ctx context.Context, authorID string, now time.Time) (*models.ForumPost, error) {
	var post models.ForumPost
	err := decodeOne(f.coll().FindOne(ctx, bson.M{"authorId": authorID, "type": models.PostFreeNow, "until": bson.M{"$gt": now}}), &post)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

// FreePost is a live free-now post by id (anyone's; callers check visibility).
func (f Forum) FreePost(ctx context.Context, id string, now time.Time) (*models.ForumPost, error) {
	var post models.ForumPost
	err := decodeOne(f.coll().FindOne(ctx, bson.M{"_id": id, "type": models.PostFreeNow, "until": bson.M{"$gt": now}}), &post)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

// DeleteFreePost removes the author's free-now post; ErrNotFound when the
// post does not exist or belongs to someone else.
func (f Forum) DeleteFreePost(ctx context.Context, authorID, postID string) error {
	res, err := f.coll().DeleteOne(ctx, bson.M{"_id": postID, "authorId": authorID, "type": models.PostFreeNow})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// FreePostQuery narrows the live free-now posts a viewer may see.
type FreePostQuery struct {
	ViewerID    string
	FriendIDs   []string
	FriendsOnly bool     // scope=friends: only friends' posts
	Lng, Lat    *float64 // center for everyone-visible posts of non-friends
	RadiusMi    float64
}

// VisibleFreePosts lists live free-now posts other than the viewer's own:
// every friend's post, plus (scope everyone, with a center) the
// everyone-visible posts inside the radius. Handlers apply the finer rules
// (author status, the post's own radius).
func (f Forum) VisibleFreePosts(ctx context.Context, q FreePostQuery, now time.Time, limit int) ([]*models.ForumPost, error) {
	or := []bson.M{{"authorId": bson.M{"$in": forumIDs(q.FriendIDs)}}}
	if !q.FriendsOnly && q.Lat != nil && q.Lng != nil && q.RadiusMi > 0 {
		or = append(or, bson.M{
			"visibility": models.PostVisibilityEveryone,
			"location": bson.M{"$geoWithin": bson.M{
				"$centerSphere": bson.A{bson.A{*q.Lng, *q.Lat}, q.RadiusMi / forumEarthRadiusMi},
			}},
		})
	}
	filter := bson.M{
		"type":     models.PostFreeNow,
		"until":    bson.M{"$gt": now},
		"authorId": bson.M{"$ne": q.ViewerID},
		"$or":      or,
	}
	return f.findPosts(ctx, filter, limit)
}

// LiveFreePostsBy lists the live free-now posts of the given authors (presence).
func (f Forum) LiveFreePostsBy(ctx context.Context, authorIDs []string, now time.Time) ([]*models.ForumPost, error) {
	if len(authorIDs) == 0 {
		return []*models.ForumPost{}, nil
	}
	filter := bson.M{"type": models.PostFreeNow, "authorId": bson.M{"$in": authorIDs}, "until": bson.M{"$gt": now}}
	return f.findPosts(ctx, filter, len(authorIDs)*2)
}

func (f Forum) findPosts(ctx context.Context, filter bson.M, limit int) ([]*models.ForumPost, error) {
	if limit <= 0 {
		limit = 500
	}
	cursor, err := f.coll().Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	posts := []*models.ForumPost{}
	if err := cursor.All(ctx, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

// forumSharedFilter is an active itinerary shown on the forum.
func forumSharedFilter() bson.M {
	return bson.M{
		"status":     models.ItineraryActive,
		"visibility": bson.M{"$in": bson.A{models.VisibilityFriends, models.VisibilityOpen}},
	}
}

// SharedPlan is an active itinerary shared to the forum (a plan post), by id.
func (f Forum) SharedPlan(ctx context.Context, id string) (*models.Itinerary, error) {
	filter := forumSharedFilter()
	filter["_id"] = id
	var it models.Itinerary
	if err := decodeOne(f.plans().FindOne(ctx, filter), &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// ActivePlan is an active itinerary by id whatever its visibility (leaving
// a plan that stopped being shared).
func (f Forum) ActivePlan(ctx context.Context, id string) (*models.Itinerary, error) {
	var it models.Itinerary
	if err := decodeOne(f.plans().FindOne(ctx, bson.M{"_id": id, "status": models.ItineraryActive}), &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// SharedPlans lists the plan posts a viewer may see before the distance
// check: shared, active, not over yet, not hosted by the viewer, and hosted
// by a friend or open to everyone (friendsOnly: friends' plans only).
func (f Forum) SharedPlans(ctx context.Context, viewerID string, friendIDs []string, friendsOnly bool, now time.Time, limit int) ([]*models.Itinerary, error) {
	filter := forumSharedFilter()
	filter["backBy"] = bson.M{"$gt": now}
	filter["hostId"] = bson.M{"$ne": viewerID}
	or := []bson.M{{"hostId": bson.M{"$in": forumIDs(friendIDs)}}}
	if !friendsOnly {
		or = append(or, bson.M{"visibility": models.VisibilityOpen})
	}
	filter["$or"] = or
	if limit <= 0 {
		limit = 500
	}
	cursor, err := f.plans().Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	plans := []*models.Itinerary{}
	if err := cursor.All(ctx, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

// Plans loads itineraries by id in any status (group threads, events);
// unknown ids are absent.
func (f Forum) Plans(ctx context.Context, ids []string) (map[string]*models.Itinerary, error) {
	out := make(map[string]*models.Itinerary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cursor, err := f.plans().Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	var list []*models.Itinerary
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	for _, it := range list {
		out[it.ID] = it
	}
	return out, nil
}

// forumIDs keeps $in arguments arrays: a nil slice would encode as null.
func forumIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
