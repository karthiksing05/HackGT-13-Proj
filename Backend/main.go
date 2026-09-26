package main

import (
	"Backend/pkg/datastore"
	"Backend/pkg/env"
	"Backend/pkg/realtime"
	"Backend/pkg/router"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "02 Jan 3:04:05 PM MST",
	})

	log.Info().Msg("Initializing SideQuestz Backend")

	// Attempt connection to MongoDB
	err := datastore.Connect()
	if err != nil {
		log.Warn().Err(err).Msg("MongoDB connection not established; operating with in-memory store fallback")
	} else {
		log.Info().Str("uri", env.GetMongoURI()).Msg("Connected to MongoDB successfully")
		defer datastore.Disconnect()
	}

	// Start WebSocket realtime event hub
	go realtime.GlobalHub.Run()

	// Initialize HTTP router with full endpoint specifications
	r := mux.NewRouter().StrictSlash(true)
	router.SetupRoutes(r)

	appAddr := env.GetHTTPAddr()
	log.Info().Str("addr", appAddr).Msg("SideQuestz API server starting")

	if err := http.ListenAndServe(appAddr, r); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Server exited with error")
	}
}
