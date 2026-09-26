package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// The public ticket page looks intents up by their ticket id.
func init() {
	appIndexes = append(appIndexes, indexSpec{coll: CollCheckoutIntents, keys: bson.D{{Key: "ticketId", Value: 1}}, name: "ticketId", sparse: true})
}

// CheckoutStateError reports an intent that exists but is in a state the
// operation does not accept (approve after booking, cancel while paying).
// It is an ErrConflict for status mapping; handlers read State to pick the
// sentence.
type CheckoutStateError struct{ State string }

func (e *CheckoutStateError) Error() string { return "checkout intent is " + e.State }

func (e *CheckoutStateError) Unwrap() error { return ErrConflict }

// CheckoutIntents is the checkout_intents collection: the simulated agent's
// persisted state machine (backend-contract §4 D). Handler-side calls are
// scoped to the owner, and every transition carries its state guard in the
// update filter, so an approve racing a cancel moves the intent once.
type CheckoutIntents struct{ s *Store }

func (s *Store) CheckoutIntents() CheckoutIntents { return CheckoutIntents{s} }

func (c CheckoutIntents) coll() *mongo.Collection { return c.s.db.Collection(CollCheckoutIntents) }

// Insert stores a new intent, stamping createdAt and updatedAt in the
// owner's business time; nextTransitionAt, which the agent compares with
// the real clock, stays real.
func (c CheckoutIntents) Insert(ctx context.Context, intent *models.CheckoutIntent) error {
	now := c.s.BusinessNow(ctx)
	if intent.ID == "" {
		intent.ID = NewID()
	}
	intent.CreatedAt, intent.UpdatedAt = now, now
	_, err := c.coll().InsertOne(ctx, intent)
	return err
}

// Get loads one of userID's intents; anyone else's (or an unknown id) is
// ErrNotFound.
func (c CheckoutIntents) Get(ctx context.Context, userID, id string) (*models.CheckoutIntent, error) {
	var intent models.CheckoutIntent
	if err := decodeOne(c.coll().FindOne(ctx, bson.M{"_id": id, "userId": userID}), &intent); err != nil {
		return nil, err
	}
	return &intent, nil
}

// ByTicket loads the booked intent a ticket belongs to (the public ticket
// page, where the unguessable ticket id is the only credential).
func (c CheckoutIntents) ByTicket(ctx context.Context, ticketID string) (*models.CheckoutIntent, error) {
	if ticketID == "" {
		return nil, ErrNotFound
	}
	var intent models.CheckoutIntent
	err := decodeOne(c.coll().FindOne(ctx, bson.M{"ticketId": ticketID, "state": models.CheckoutBooked}), &intent)
	if err != nil {
		return nil, err
	}
	return &intent, nil
}

// SetCard switches the card of an intent that is not paying yet (preparing
// or awaiting approval).
func (c CheckoutIntents) SetCard(ctx context.Context, userID, id string, card *models.PaymentMethod) (*models.CheckoutIntent, error) {
	return c.transition(ctx, userID, id, []string{models.CheckoutPreparing, models.CheckoutAwaitingApproval}, bson.M{
		"$set": bson.M{"paymentMethodId": card.ID, "cardBrand": card.Brand, "cardLast4": card.Last4, "updatedAt": c.s.BusinessNow(ctx)},
	})
}

// Approve moves an intent awaiting approval to processing, recording the
// ticket it will book and when the agent books it.
func (c CheckoutIntents) Approve(ctx context.Context, userID, id, ticketID, confirmation string, due time.Time) (*models.CheckoutIntent, error) {
	return c.transition(ctx, userID, id, []string{models.CheckoutAwaitingApproval}, bson.M{
		"$set": bson.M{
			"state":            models.CheckoutProcessing,
			"ticketId":         ticketID,
			"confirmation":     confirmation,
			"nextTransitionAt": due,
			"updatedAt":        c.s.BusinessNow(ctx),
		},
	})
}

// Cancel drops an intent that is not paying yet.
func (c CheckoutIntents) Cancel(ctx context.Context, userID, id string) (*models.CheckoutIntent, error) {
	return c.transition(ctx, userID, id, []string{models.CheckoutPreparing, models.CheckoutAwaitingApproval}, bson.M{
		"$set":   bson.M{"state": models.CheckoutCancelled, "updatedAt": c.s.BusinessNow(ctx)},
		"$unset": bson.M{"nextTransitionAt": ""},
	})
}

// transition applies update to userID's intent id if its state is one of
// from and returns the updated intent. When the guard does not match, an
// existing intent comes back with a *CheckoutStateError naming its state;
// a missing one is ErrNotFound.
func (c CheckoutIntents) transition(ctx context.Context, userID, id string, from []string, update bson.M) (*models.CheckoutIntent, error) {
	var intent models.CheckoutIntent
	err := c.coll().FindOneAndUpdate(ctx,
		bson.M{"_id": id, "userId": userID, "state": bson.M{"$in": from}}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&intent)
	if err == nil {
		return &intent, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	current, err := c.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return current, &CheckoutStateError{State: current.State}
}

// Due lists the intents whose timed step (preparing → awaiting approval,
// processing → booked) is due at now, the longest-waiting first.
func (c CheckoutIntents) Due(ctx context.Context, now time.Time, limit int) ([]*models.CheckoutIntent, error) {
	if limit <= 0 {
		limit = 100
	}
	cursor, err := c.coll().Find(ctx,
		bson.M{
			"state":            bson.M{"$in": []string{models.CheckoutPreparing, models.CheckoutProcessing}},
			"nextTransitionAt": bson.M{"$lte": now},
		},
		options.Find().SetSort(bson.D{{Key: "nextTransitionAt", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	intents := []*models.CheckoutIntent{}
	if err := cursor.All(ctx, &intents); err != nil {
		return nil, err
	}
	return intents, nil
}

// Advance performs one timed step atomically: set is applied (and
// nextTransitionAt cleared) only while the intent is still in state from and
// due at now. ok is false when a cancel or another worker got there first.
func (c CheckoutIntents) Advance(ctx context.Context, id, from string, now time.Time, set bson.M) (*models.CheckoutIntent, bool, error) {
	fields := bson.M{"updatedAt": c.s.BusinessNow(ctx)}
	for k, v := range set {
		fields[k] = v
	}
	var intent models.CheckoutIntent
	err := c.coll().FindOneAndUpdate(ctx,
		bson.M{"_id": id, "state": from, "nextTransitionAt": bson.M{"$lte": now}},
		bson.M{"$set": fields, "$unset": bson.M{"nextTransitionAt": ""}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&intent)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &intent, true, nil
}
