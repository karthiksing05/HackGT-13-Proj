package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ItemStates is item_states: one viewer's own state on one item (private
// notes and their scope, the "Getting there" choice, a booked ticket),
// _id = PairID(userId, itemId). Rendering folds it into ItineraryItem.
type ItemStates struct{ s *Store }

func (s *Store) ItemStates() ItemStates { return ItemStates{s} }

func (st ItemStates) coll() *mongo.Collection { return st.s.db.Collection(CollItemStates) }

// ForUser loads userID's states for the given items, keyed by item id.
func (st ItemStates) ForUser(ctx context.Context, userID string, itemIDs []string) (map[string]*models.ItemState, error) {
	out := make(map[string]*models.ItemState, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		ids = append(ids, models.PairID(userID, itemID))
	}
	cursor, err := st.coll().Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	var docs []*models.ItemState
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for _, doc := range docs {
		out[doc.ItemID] = doc
	}
	return out, nil
}

// Get loads one state; ErrNotFound when the user never touched the item.
func (st ItemStates) Get(ctx context.Context, userID, itemID string) (*models.ItemState, error) {
	var doc models.ItemState
	if err := decodeOne(st.coll().FindOne(ctx, bson.M{"_id": models.PairID(userID, itemID)}), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// SetNotes saves the user's own note on an item (nil clears it) and, when
// scope is not nil, who sees it (private | shared).
func (st ItemStates) SetNotes(ctx context.Context, userID, itemID, itineraryID string, notes, scope *string) error {
	set, unset := bson.M{}, bson.M{}
	if notes == nil {
		unset["notes"] = ""
	} else {
		set["notes"] = *notes
	}
	if scope != nil {
		set["notesScope"] = *scope
	}
	return st.upsert(ctx, userID, itemID, itineraryID, set, unset)
}

// SetTransitMode saves the user's "Getting there" choice for an item.
func (st ItemStates) SetTransitMode(ctx context.Context, userID, itemID, itineraryID, mode string) error {
	return st.upsert(ctx, userID, itemID, itineraryID, bson.M{"transitMode": mode}, nil)
}

func (st ItemStates) upsert(ctx context.Context, userID, itemID, itineraryID string, set, unset bson.M) error {
	fields := bson.M{"itineraryId": itineraryID, "updatedAt": st.s.Now()}
	for k, v := range set {
		fields[k] = v
	}
	update := bson.M{
		"$set":         fields,
		"$setOnInsert": bson.M{"userId": userID, "itemId": itemID},
	}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	_, err := st.coll().UpdateOne(ctx, bson.M{"_id": models.PairID(userID, itemID)}, update, options.UpdateOne().SetUpsert(true))
	return err
}
