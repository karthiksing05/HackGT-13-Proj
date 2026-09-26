package main

import (
	"Backend/pkg/api"
	"Backend/pkg/api/checkout"
	"Backend/pkg/config"
	"Backend/pkg/datastore"
	"Backend/pkg/ml"
	plannerwire "Backend/pkg/planner/wire"
	"Backend/pkg/profiles"
	"Backend/pkg/realtime"
	"Backend/pkg/router"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const shutdownGrace = 10 * time.Second

// main: config → MustConnect → EnsureIndexes → hub → http.Server with the §8
// timeouts, then a graceful shutdown on SIGINT/SIGTERM.
func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "02 Jan 3:04:05 PM MST"})

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}
	ctx := context.Background()
	client := datastore.MustConnect(ctx, cfg)
	defer datastore.Disconnect(client)
	st := store.New(client.Database(cfg.MongoDB), time.Now)
	if err := st.EnsureIndexes(ctx); err != nil {
		log.Fatal().Err(err).Msg("ensure indexes")
	}
	log.Info().Str("db", cfg.MongoDB).Str("env", cfg.AppEnv).Msg("MongoDB connected, indexes ensured")

	deps := &api.Deps{
		Store: st,
		Cfg:   cfg,
		Now:   time.Now,
		ML:    ml.NewClient(cfg.MLServiceURL),
	}
	// Taste vectors (pkg/profiles) and the planner both run over the ML
	// client. A planner that fails to start leaves /plans/* answering 503
	// instead of taking the whole API down.
	deps.Profiles = profiles.New(st, deps.ML, time.Now)
	if p, err := plannerwire.Planner(ctx, st.DB(), deps.ML); err != nil {
		log.Error().Err(err).Msg("planner not started; /plans/* answer 503")
	} else {
		deps.Planner = p
	}
	hub := realtime.NewHub(deps.Auth().UserFromToken)
	deps.Hub = hub
	checkout.StartAgent(ctx, deps)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router.New(deps, hub),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errs := make(chan error, 1)
	go func() {
		log.Info().Str("addr", cfg.HTTPAddr).Str("public_base_url", cfg.PublicBaseURL).Msg("SideQuestz API listening")
		errs <- srv.ListenAndServe()
	}()

	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	select {
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("server exited")
		}
	case <-stop.Done():
		log.Info().Msg("shutting down")
	}
	shutdownCtx, done := context.WithTimeout(ctx, shutdownGrace)
	defer done()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("shutdown")
	}
	hub.Close()
}
