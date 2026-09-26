package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CheckoutReads are what the checkout agent needs from collections other
// areas own: the buyer's saved cards (payment_methods), the item being
// bought (itineraries) and, once booked, the ticket on the buyer's state
// for that item (item_states). They use the shared pkg/models schemas only.
type CheckoutReads struct{ s *Store }

func (s *Store) CheckoutReads() CheckoutReads { return CheckoutReads{s} }

// Cards lists userID's saved cards, oldest first.
func (r CheckoutReads) Cards(ctx context.Context, userID string) ([]*models.PaymentMethod, error) {
	cursor, err := r.s.db.Collection(CollPaymentMethods).Find(ctx, bson.M{"userId": userID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	cards := []*models.PaymentMethod{}
	if err := cursor.All(ctx, &cards); err != nil {
		return nil, err
	}
	return cards, nil
}

// Card loads one of userID's cards; anyone else's is ErrNotFound.
func (r CheckoutReads) Card(ctx context.Context, userID, id string) (*models.PaymentMethod, error) {
	var card models.PaymentMethod
	if err := decodeOne(r.s.db.Collection(CollPaymentMethods).FindOne(ctx, bson.M{"_id": id, "userId": userID}), &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// MemberItem finds itemID in a live itinerary userID belongs to; an item of
// someone else's plan (or a deleted one) is ErrNotFound.
func (r CheckoutReads) MemberItem(ctx context.Context, userID, itemID string) (*models.Itinerary, *models.ItineraryItem, error) {
	return r.findItem(ctx, bson.M{
		"items.id":  itemID,
		"memberIds": userID,
		"status":    bson.M{"$ne": models.ItineraryDeleted},
	}, itemID)
}

// Item finds itemID in the given itinerary, whoever it belongs to (the
// public ticket page renders the booked item's time and place).
func (r CheckoutReads) Item(ctx context.Context, itineraryID, itemID string) (*models.Itinerary, *models.ItineraryItem, error) {
	return r.findItem(ctx, bson.M{"_id": itineraryID, "items.id": itemID}, itemID)
}

func (r CheckoutReads) findItem(ctx context.Context, filter bson.M, itemID string) (*models.Itinerary, *models.ItineraryItem, error) {
	var itin models.Itinerary
	if err := decodeOne(r.s.db.Collection(CollItineraries).FindOne(ctx, filter), &itin); err != nil {
		return nil, nil, err
	}
	for i := range itin.Items {
		if itin.Items[i].ID == itemID {
			return &itin, &itin.Items[i], nil
		}
	}
	return nil, nil, ErrNotFound
}

// SaveTicket records a booked ticket on userID's state for the item
// (item_states, _id = userId|itemId), creating that state when needed and
// leaving its notes and transit choice alone. Writing the same ticket twice
// is harmless, so the agent can retry a booking step.
func (r CheckoutReads) SaveTicket(ctx context.Context, userID, itineraryID, itemID string, ticket models.ItemTicket) error {
	_, err := r.s.db.Collection(CollItemStates).UpdateOne(ctx,
		bson.M{"userId": userID, "itemId": itemID},
		bson.M{
			"$set":         bson.M{"ticket": ticket, "updatedAt": r.s.Now()},
			"$setOnInsert": bson.M{"_id": models.PairID(userID, itemID), "itineraryId": itineraryID},
		},
		options.UpdateOne().SetUpsert(true))
	return err
}
