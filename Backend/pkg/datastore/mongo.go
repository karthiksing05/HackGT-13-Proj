// Package datastore opens the MongoDB connection. There is no in-memory
// fallback: an unreachable database is fatal at startup and a 503 on /healthz.
package datastore

import (
	"Backend/pkg/config"
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	connectTimeout    = 10 * time.Second
	pingTimeout       = 2 * time.Second
	disconnectTimeout = 5 * time.Second
)

// Connect opens a client and pings the primary (10 s). The caller owns the client.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(connectTimeout))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		Disconnect(client)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, nil
}

// MustConnect is Connect or a fatal log: the server never runs without its database.
func MustConnect(ctx context.Context, cfg *config.Config) *mongo.Client {
	client, err := Connect(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatal().Err(err).Msg("MongoDB unreachable; refusing to start")
	}
	return client
}

// Ping is the /healthz probe (2 s).
func Ping(ctx context.Context, client *mongo.Client) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return client.Ping(ctx, readpref.Primary())
}

// Disconnect closes the client, logging rather than returning a failure.
func Disconnect(client *mongo.Client) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		log.Warn().Err(err).Msg("mongo disconnect")
	}
}
