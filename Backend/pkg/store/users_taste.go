package store

import (
	"context"
	"fmt"
	"regexp"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TasteStep is how far one rating moves a taste tag toward its target.
const TasteStep = 0.3

// tasteKeyPattern keeps field names safe: tags are canonical trip-type keys.
var tasteKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)

// BumpTaste moves the user's taste tags (users.taste.tags, canonical
// trip-type keys, 0..1) toward the targets of one rating: each tag steps
// TasteStep of the way from its value to its target. A tag seen for the
// first time starts from the saved preference for that key, (rating−1)/4,
// or 0.5 without one. counted adds one to taste.ratingCount (a first
// rating of an item). The update is one atomic pipeline, so concurrent
// ratings never lose each other's steps.
func (u Users) BumpTaste(ctx context.Context, id string, targets map[string]float64, counted bool) error {
	oid, ok := objectID(id)
	if !ok {
		return ErrNotFound
	}
	set := bson.M{"updatedAt": u.s.Now()}
	for key, target := range targets {
		if !tasteKeyPattern.MatchString(key) {
			return fmt.Errorf("taste key %q is not a trip type", key)
		}
		current := bson.M{"$ifNull": bson.A{"$taste.tags." + key, bson.M{"$divide": bson.A{
			bson.M{"$subtract": bson.A{bson.M{"$ifNull": bson.A{"$prefs.ratings." + key, 3}}, 1}}, 4,
		}}}}
		set["taste.tags."+key] = bson.M{"$round": bson.A{
			bson.M{"$add": bson.A{current, bson.M{"$multiply": bson.A{TasteStep, bson.M{"$subtract": bson.A{target, current}}}}}}, 4,
		}}
	}
	if counted {
		set["taste.ratingCount"] = bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$taste.ratingCount", 0}}, 1}}
	}
	res, err := u.coll().UpdateOne(ctx, bson.M{"_id": oid}, mongo.Pipeline{{{Key: "$set", Value: set}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}
