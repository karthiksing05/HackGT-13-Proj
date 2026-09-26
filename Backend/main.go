package main

import (
	"Backend/pkg/config"
	"Backend/pkg/datastore"
	"context"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Temporary entry point while the foundation lands: the HTTP server, router
// and graceful shutdown arrive with pkg/router in a later commit.
func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "02 Jan 3:04:05 PM MST"})

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}
	client := datastore.MustConnect(context.Background(), cfg)
	defer datastore.Disconnect(client)
	log.Info().Str("db", cfg.MongoDB).Msg("MongoDB connected")
}
