package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Expenses is the expenses collection: a group's shared costs and the
// settle-up rows, oldest first.
type Expenses struct{ s *Store }

func (s *Store) Expenses() Expenses { return Expenses{s} }

func (e Expenses) coll() *mongo.Collection { return e.s.db.Collection(CollExpenses) }

var expenseOrder = bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}

// List is a group's expenses in the order they were added.
func (e Expenses) List(ctx context.Context, groupID string) ([]*models.Expense, error) {
	cursor, err := e.coll().Find(ctx, bson.M{"groupId": groupID}, options.Find().SetSort(expenseOrder))
	if err != nil {
		return nil, err
	}
	list := []*models.Expense{}
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ListForGroups loads the expenses of many groups (balance chips).
func (e Expenses) ListForGroups(ctx context.Context, groupIDs []string) (map[string][]*models.Expense, error) {
	out := make(map[string][]*models.Expense, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	cursor, err := e.coll().Find(ctx, bson.M{"groupId": bson.M{"$in": groupIDs}}, options.Find().SetSort(expenseOrder))
	if err != nil {
		return nil, err
	}
	var list []*models.Expense
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	for _, exp := range list {
		out[exp.GroupID] = append(out[exp.GroupID], exp)
	}
	return out, nil
}

// Insert stores expenses, assigning ids and createdAt (one timestamp for the
// batch; ids keep their order).
func (e Expenses) Insert(ctx context.Context, expenses ...*models.Expense) error {
	if len(expenses) == 0 {
		return nil
	}
	now := e.s.Now()
	docs := make([]any, 0, len(expenses))
	for _, exp := range expenses {
		exp.ID = NewID()
		exp.CreatedAt = now
		docs = append(docs, exp)
	}
	_, err := e.coll().InsertMany(ctx, docs)
	return err
}

// Get loads one expense of a group.
func (e Expenses) Get(ctx context.Context, groupID, id string) (*models.Expense, error) {
	var exp models.Expense
	if err := decodeOne(e.coll().FindOne(ctx, bson.M{"_id": id, "groupId": groupID}), &exp); err != nil {
		return nil, err
	}
	return &exp, nil
}

// Delete removes an expense the user added; ErrNotFound otherwise.
func (e Expenses) Delete(ctx context.Context, groupID, id, createdBy string) error {
	res, err := e.coll().DeleteOne(ctx, bson.M{"_id": id, "groupId": groupID, "createdBy": createdBy})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SettleCards reads the user's saved (simulated) cards from the shared
// payment_methods collection, oldest first, to pay a settle-up with.
func (e Expenses) SettleCards(ctx context.Context, userID string) ([]*models.PaymentMethod, error) {
	cursor, err := e.s.db.Collection(CollPaymentMethods).Find(ctx, bson.M{"userId": userID},
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
