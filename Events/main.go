package main

import (
	"context"
	"errors"
	"events/pkg/api"
	"events/pkg/config"
	"events/pkg/router"
	"events/pkg/store"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const shutdownGrace = 10 * time.Second

func main() {
	// Console logger matching Backend
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "02 Jan 3:04:05 PM MST"})

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	ctx := context.Background()

	// Initialize Store (MongoDB with automatic fallback to high-fidelity In-Memory store)
	st, err := store.NewStore(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize event merchant store")
	}

	deps := api.NewDeps(cfg, st)
	handler := router.New(deps)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info().
			Str("addr", cfg.HTTPAddr).
			Str("merchant_host", cfg.MerchantHost).
			Str("payments_mode", cfg.PaymentsMode).
			Msg("SideQuestz Events Merchant (Ticketmaster Demo) listening")

		log.Info().Msgf("🎟  Event Discovery:  http://localhost%s/", cfg.HTTPAddr)
		log.Info().Msgf("📊  Booth Dashboard:  http://localhost%s/dashboard", cfg.HTTPAddr)

		errs <- srv.ListenAndServe()
	}()

	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	select {
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("events server exited")
		}
	case <-stop.Done():
		log.Info().Msg("shutting down events server")
	}

	shutdownCtx, done := context.WithTimeout(context.Background(), shutdownGrace)
	defer done()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("shutdown error")
	}
	log.Info().Msg("events server exited cleanly")
}
