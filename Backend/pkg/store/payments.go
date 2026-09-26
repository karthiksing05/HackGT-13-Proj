package store

import (
	"Backend/pkg/models"
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Payments is the payment_methods collection: simulated saved cards, brand
// and last four digits only (no card number is ever stored). The first card
// a user adds is the default; deleting the default promotes the oldest card
// left. A partial unique index keeps at most one default per user, so other
// areas can read {userId, isDefault: true} directly.
type Payments struct{ s *Store }

const paymentDefaultIndex = "userId_default_unique"

func init() {
	appIndexes = append(appIndexes, indexSpec{coll: CollPaymentMethods, keys: bson.D{{Key: "userId", Value: 1}},
		name: paymentDefaultIndex, unique: true, partial: bson.D{{Key: "isDefault", Value: true}}})
}

func (s *Store) Payments() Payments { return Payments{s} }

func (p Payments) coll() *mongo.Collection { return p.s.db.Collection(CollPaymentMethods) }

// cardOrder is the list order: oldest first, which puts the default on top.
var cardOrder = bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}

// List returns the user's cards, oldest first.
func (p Payments) List(ctx context.Context, userID string) ([]*models.PaymentMethod, error) {
	cursor, err := p.coll().Find(ctx, bson.M{"userId": userID}, options.Find().SetSort(cardOrder).SetLimit(100))
	if err != nil {
		return nil, err
	}
	cards := []*models.PaymentMethod{}
	if err := cursor.All(ctx, &cards); err != nil {
		return nil, err
	}
	return cards, nil
}

// Count is how many cards the user has saved.
func (p Payments) Count(ctx context.Context, userID string) (int, error) {
	n, err := p.coll().CountDocuments(ctx, bson.M{"userId": userID})
	return int(n), err
}

// Add saves a card for userID; it becomes the default when the user has none.
func (p Payments) Add(ctx context.Context, userID, brand, last4, demoToken string) (*models.PaymentMethod, error) {
	card := &models.PaymentMethod{
		ID:        NewID(),
		UserID:    userID,
		Brand:     brand,
		Last4:     last4,
		DemoToken: demoToken,
		CreatedAt: p.s.Now(),
	}
	defaults, err := p.coll().CountDocuments(ctx, bson.M{"userId": userID, "isDefault": true})
	if err != nil {
		return nil, err
	}
	card.IsDefault = defaults == 0
	_, err = p.coll().InsertOne(ctx, card)
	if card.IsDefault && duplicateOn(err, paymentDefaultIndex) {
		// Another card became the default meanwhile; this one is a spare.
		card.IsDefault = false
		_, err = p.coll().InsertOne(ctx, card)
	}
	if err != nil {
		return nil, err
	}
	return card, nil
}

// Delete removes one of the user's cards (another account's card or an
// unknown id is ErrNotFound) and, when it was the default, promotes the
// oldest card left.
func (p Payments) Delete(ctx context.Context, userID, id string) error {
	var removed models.PaymentMethod
	err := p.coll().FindOneAndDelete(ctx, bson.M{"_id": id, "userId": userID}).Decode(&removed)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !removed.IsDefault {
		return nil
	}
	err = p.coll().FindOneAndUpdate(ctx, bson.M{"userId": userID}, bson.M{"$set": bson.M{"isDefault": true}},
		options.FindOneAndUpdate().SetSort(cardOrder)).Err()
	switch {
	case err == nil, errors.Is(err, mongo.ErrNoDocuments):
		return nil // promoted, or that was the last card
	case duplicateOn(err, paymentDefaultIndex):
		return nil // a card added meanwhile already became the default
	}
	return err
}
