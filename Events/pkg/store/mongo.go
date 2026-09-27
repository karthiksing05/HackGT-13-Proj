package store

import (
	"context"
	"events/pkg/models"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	CollEvents   = "merchant_events"
	CollQuotes   = "merchant_quotes"
	CollOrders   = "merchant_orders"
	CollNonces   = "merchant_nonces"
	CollRejected = "merchant_rejected"
)

// MongoStore implements Store backed by MongoDB.
type MongoStore struct {
	db         *mongo.Database
	scenarioMu sync.RWMutex
	scenario   string
}

// NewMongoStore creates a new MongoStore.
func NewMongoStore(db *mongo.Database) *MongoStore {
	return &MongoStore{
		db:       db,
		scenario: models.ScenarioNormal,
	}
}

// EnsureIndexesAndSeed sets up indexes and seeds default events idempotently.
func (m *MongoStore) EnsureIndexesAndSeed(ctx context.Context) error {
	// 1. Events index on slug
	_, err := m.db.Collection(CollEvents).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "slug", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// 2. Quotes index on quoteId and TTL on expiresAt
	_, _ = m.db.Collection(CollQuotes).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "quoteId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})

	// 3. Orders index on orderId, ticketId, idempotencyKey
	_, _ = m.db.Collection(CollOrders).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "orderId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "ticket.ticketId", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys:    bson.D{{Key: "idempotencyKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
	})

	// 4. Nonces TTL
	_, _ = m.db.Collection(CollNonces).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "nonce", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})

	// Seed events idempotently
	for _, e := range DefaultDemoEvents {
		if models.IsReservedSlug(e.Slug) {
			continue
		}
		filter := bson.M{"slug": e.Slug}
		update := bson.M{
			"$setOnInsert": e,
		}
		_, _ = m.db.Collection(CollEvents).UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	}

	return nil
}

func (m *MongoStore) GetEvent(ctx context.Context, slug string) (*models.Event, error) {
	if models.IsReservedSlug(slug) {
		return nil, ErrNotFound
	}
	var e models.Event
	err := m.db.Collection(CollEvents).FindOne(ctx, bson.M{"slug": slug}).Decode(&e)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (m *MongoStore) ListEvents(ctx context.Context, category string, search string) ([]*models.Event, error) {
	filter := bson.M{}
	if category != "" && category != "all" {
		filter["category"] = category
	}
	if search != "" {
		filter["$or"] = []bson.M{
			{"title": bson.M{"$regex": search, "$options": "i"}},
			{"venue": bson.M{"$regex": search, "$options": "i"}},
			{"description": bson.M{"$regex": search, "$options": "i"}},
		}
	}
	cur, err := m.db.Collection(CollEvents).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var res []*models.Event
	for cur.Next(ctx) {
		var e models.Event
		if err := cur.Decode(&e); err == nil {
			res = append(res, &e)
		}
	}
	return res, nil
}

func (m *MongoStore) SaveQuote(ctx context.Context, quote *models.Quote) error {
	_, err := m.db.Collection(CollQuotes).InsertOne(ctx, quote)
	return err
}

func (m *MongoStore) GetQuote(ctx context.Context, quoteID string) (*models.Quote, error) {
	var q models.Quote
	err := m.db.Collection(CollQuotes).FindOne(ctx, bson.M{"quoteId": quoteID}).Decode(&q)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if time.Now().After(q.ExpiresAt) {
		return nil, ErrQuoteExpired
	}
	return &q, nil
}

func (m *MongoStore) SaveOrder(ctx context.Context, order *models.OrderConfirmation, idempotencyKey string) error {
	order.IdempotencyKey = idempotencyKey
	_, err := m.db.Collection(CollOrders).InsertOne(ctx, order)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateOrder
	}
	return err
}

func (m *MongoStore) GetOrder(ctx context.Context, orderID string) (*models.OrderConfirmation, error) {
	var o models.OrderConfirmation
	err := m.db.Collection(CollOrders).FindOne(ctx, bson.M{"orderId": orderID}).Decode(&o)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (m *MongoStore) GetOrderByTicketID(ctx context.Context, ticketID string) (*models.OrderConfirmation, error) {
	var o models.OrderConfirmation
	err := m.db.Collection(CollOrders).FindOne(ctx, bson.M{
		"$or": []bson.M{
			{"ticket.ticketId": ticketID},
			{"ticket.ticket_id": ticketID},
			{"ticket.ticketid": ticketID},
		},
	}).Decode(&o)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (m *MongoStore) GetOrderByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.OrderConfirmation, error) {
	var o models.OrderConfirmation
	err := m.db.Collection(CollOrders).FindOne(ctx, bson.M{"idempotencyKey": idempotencyKey}).Decode(&o)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (m *MongoStore) ReserveTickets(ctx context.Context, slug string, quantity int) error {
	// Atomic conditional decrement: remaining >= quantity
	filter := bson.M{
		"slug":      slug,
		"remaining": bson.M{"$gte": quantity},
	}
	update := bson.M{
		"$inc": bson.M{"remaining": -quantity},
	}
	res, err := m.db.Collection(CollEvents).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrSoldOut
	}
	return nil
}

func (m *MongoStore) ReleaseTickets(ctx context.Context, slug string, quantity int) error {
	filter := bson.M{"slug": slug}
	update := bson.M{
		"$inc": bson.M{"remaining": quantity},
	}
	_, err := m.db.Collection(CollEvents).UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoStore) CheckAndRecordNonce(nonce string, expiresAt time.Time) error {
	doc := bson.M{
		"nonce":     nonce,
		"expiresAt": expiresAt,
		"createdAt": time.Now(),
	}
	_, err := m.db.Collection(CollNonces).InsertOne(context.Background(), doc)
	if err != nil {
		return ErrReplayedNonce
	}
	return nil
}

func (m *MongoStore) RecordRejectedRequest(ctx context.Context, req *models.RejectedRequest) error {
	_, err := m.db.Collection(CollRejected).InsertOne(ctx, req)
	return err
}

func (m *MongoStore) ListRejectedRequests(ctx context.Context, limit int) ([]models.RejectedRequest, error) {
	opt := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(int64(limit))
	cur, err := m.db.Collection(CollRejected).Find(ctx, bson.M{}, opt)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var res []models.RejectedRequest
	for cur.Next(ctx) {
		var r models.RejectedRequest
		if err := cur.Decode(&r); err == nil {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *MongoStore) ListRecentOrders(ctx context.Context, limit int) ([]*models.OrderConfirmation, error) {
	opt := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit))
	cur, err := m.db.Collection(CollOrders).Find(ctx, bson.M{}, opt)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var res []*models.OrderConfirmation
	for cur.Next(ctx) {
		var o models.OrderConfirmation
		if err := cur.Decode(&o); err == nil {
			res = append(res, &o)
		}
	}
	return res, nil
}

func (m *MongoStore) GetScenario(ctx context.Context) string {
	m.scenarioMu.RLock()
	defer m.scenarioMu.RUnlock()
	return m.scenario
}

func (m *MongoStore) SetScenario(ctx context.Context, scenario string) {
	m.scenarioMu.Lock()
	defer m.scenarioMu.Unlock()
	m.scenario = scenario
}

func (m *MongoStore) TotalOrders(ctx context.Context) int {
	n, _ := m.db.Collection(CollOrders).CountDocuments(ctx, bson.M{})
	return int(n)
}

func (m *MongoStore) TotalGrossCents(ctx context.Context) int {
	orders, err := m.ListRecentOrders(ctx, 1000)
	if err != nil {
		return 0
	}
	total := 0
	for _, o := range orders {
		total += o.TotalCents
	}
	return total
}
