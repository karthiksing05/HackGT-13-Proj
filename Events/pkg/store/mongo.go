package store

import (
	"context"
	"crypto/rand"
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
	CollOrders   = "orders"
	CollNonces   = "merchant_nonces"
	CollRejected = "merchant_rejected"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomCrockford(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	out := make([]byte, length)
	for i, v := range b {
		out[i] = crockfordAlphabet[int(v)%len(crockfordAlphabet)]
	}
	return string(out)
}

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

	// 3. Orders index on orderId, ticketId, ticket.barcode, barcode, idempotencyKey
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
			Keys:    bson.D{{Key: "ticket.barcode", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys:    bson.D{{Key: "barcode", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys:    bson.D{{Key: "idempotencyKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
	})

	// Drop legacy merchant_orders collection after migrating any remaining docs
	if cur, err := m.db.Collection("merchant_orders").Find(ctx, bson.M{}); err == nil {
		for cur.Next(ctx) {
			var o models.OrderConfirmation
			if err := cur.Decode(&o); err == nil && o.OrderID != "" {
				m.fillBarcode(&o)
				filter := bson.M{"orderId": o.OrderID}
				update := bson.M{"$setOnInsert": o}
				_, _ = m.db.Collection(CollOrders).UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
			}
		}
		_ = cur.Close(ctx)
		_ = m.db.Collection("merchant_orders").Drop(ctx)
	}

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

func (m *MongoStore) fillBarcode(o *models.OrderConfirmation) {
	if o.Barcode == "" && o.Ticket.Barcode != "" {
		o.Barcode = o.Ticket.Barcode
	}
	if o.Ticket.Barcode == "" && o.Barcode != "" {
		o.Ticket.Barcode = o.Barcode
	}
}

func (m *MongoStore) SaveOrder(ctx context.Context, order *models.OrderConfirmation, idempotencyKey string) error {
	order.IdempotencyKey = idempotencyKey
	m.fillBarcode(order)

	_, err := m.db.Collection(CollOrders).InsertOne(ctx, order)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			if idempotencyKey != "" {
				if existing, gerr := m.GetOrderByIdempotencyKey(ctx, idempotencyKey); gerr == nil && existing != nil {
					return ErrDuplicateOrder
				}
			}
			// Accidental collision on orderId or barcode; retry up to 5 times with fresh codes
			for retry := 0; retry < 5; retry++ {
				order.OrderID = "SL-" + randomCrockford(6)
				order.ConfirmationCode = order.OrderID
				newBarcode := "SLT-" + randomCrockford(4) + "-" + randomCrockford(5)
				order.Barcode = newBarcode
				order.Ticket.Barcode = newBarcode
				_, retryErr := m.db.Collection(CollOrders).InsertOne(ctx, order)
				if retryErr == nil {
					return nil
				}
				if !mongo.IsDuplicateKeyError(retryErr) {
					return retryErr
				}
			}
			return ErrDuplicateOrder
		}
		return err
	}
	return nil
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
	m.fillBarcode(&o)
	return &o, nil
}

func (m *MongoStore) GetOrderByTicketID(ctx context.Context, ticketID string) (*models.OrderConfirmation, error) {
	var o models.OrderConfirmation
	filter := bson.M{
		"$or": []bson.M{
			{"ticket.ticketId": ticketID},
			{"ticket.ticket_id": ticketID},
			{"ticket.ticketid": ticketID},
			{"ticket.barcode": ticketID},
			{"barcode": ticketID},
		},
	}
	err := m.db.Collection(CollOrders).FindOne(ctx, filter).Decode(&o)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.fillBarcode(&o)
	return &o, nil
}

func (m *MongoStore) GetOrderByBarcode(ctx context.Context, barcode string) (*models.OrderConfirmation, error) {
	var o models.OrderConfirmation
	filter := bson.M{
		"$or": []bson.M{
			{"barcode": barcode},
			{"ticket.barcode": barcode},
		},
	}
	err := m.db.Collection(CollOrders).FindOne(ctx, filter).Decode(&o)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.fillBarcode(&o)
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
	m.fillBarcode(&o)
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
