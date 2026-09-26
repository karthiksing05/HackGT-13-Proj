package datastore

import (
	"Backend/pkg/env"
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const ctxTimeout = 10 * time.Second

var client *mongo.Client

func Connect() error {
	var err error
	var clientOptions = options.Client().
		ApplyURI(env.GetMongoURI())

	client, err = mongo.Connect(clientOptions)
	if err != nil {
		return fmt.Errorf("unable to establish connection to MongoDB %v", err)
	}

	ctx, cancel := GetCtx()
	defer cancel()

	err = client.Ping(ctx, readpref.Primary())
	if err != nil {
		return fmt.Errorf("unable to establish connection to MongoDB: %v", err)
	}

	return nil
}

func GetCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), ctxTimeout)
}

func Disconnect() {
	conn := GetConn()
	if conn != nil {
		ctx, cancel := GetCtx()
		defer cancel()
		_ = conn.Disconnect(ctx)
	}
}

func GetCollection(collection string) *mongo.Collection {
	conn := GetConn()
	if conn == nil {
		return nil
	}
	return conn.Database(env.GetDB()).Collection(collection)
}

func GetConn() *mongo.Client {
	return client
}

func IsConnected() bool {
	return client != nil
}
