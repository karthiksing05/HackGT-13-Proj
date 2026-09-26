// Package testutil gives handler and store tests a real MongoDB database per
// test, a full router behind httptest with a recording publisher and a
// frozen clock, and request helpers that always send X-Time-Zone.
package testutil

import (
	"Backend/pkg/store"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// DefaultMongoURI is used when MONGO_TEST_URI is unset (the sq-mongo container).
const DefaultMongoURI = "mongodb://127.0.0.1:27017"

var (
	clientOnce sync.Once
	client     *mongo.Client
	clientErr  error
)

// MongoURI is the test server address.
func MongoURI() string {
	if uri := os.Getenv("MONGO_TEST_URI"); uri != "" {
		return uri
	}
	return DefaultMongoURI
}

func connect() (*mongo.Client, error) {
	clientOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, err := mongo.Connect(options.Client().ApplyURI(MongoURI()).SetServerSelectionTimeout(2 * time.Second))
		if err != nil {
			clientErr = err
			return
		}
		if err := c.Ping(ctx, readpref.Primary()); err != nil {
			clientErr = err
			return
		}
		client = c
	})
	return client, clientErr
}

// DB returns a fresh database named sq_test_<pid>_<rand>, dropped when the
// test ends. Without a reachable server the test is skipped, unless CI=1
// makes that a failure.
func DB(t testing.TB) *mongo.Database {
	t.Helper()
	c, err := connect()
	if err != nil {
		if os.Getenv("CI") == "1" {
			t.Fatalf("MongoDB required in CI (MONGO_TEST_URI=%s): %v", MongoURI(), err)
		}
		t.Skipf("MongoDB not reachable at %s: %v", MongoURI(), err)
	}
	name := fmt.Sprintf("sq_test_%d_%06x", os.Getpid(), rand.IntN(1<<24))
	db := c.Database(name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Drop(ctx); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	return db
}

// Store returns a Store on a fresh test database with every index ensured.
func Store(t testing.TB, now func() time.Time) *store.Store {
	t.Helper()
	s := store.New(DB(t), now)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return s
}
