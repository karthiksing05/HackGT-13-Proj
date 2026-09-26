package api

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// Profiles keeps a user's taste vectors in step with what they tell us:
// preferences and answers, Facebook interests and ratings. pkg/profiles
// implements it over the ML service and main wires it; handlers go through
// the nil-safe helpers below and never call the ML client themselves.
// A slow or failing ML service never fails the request that triggered it.
type Profiles interface {
	// Refresh rebuilds the user's profile texts and vectors from their
	// current preferences, answers, Facebook interests and rated stops.
	Refresh(ctx context.Context, userID string) error
	// Rated folds one rating into the user's vectors using the activity's
	// stored vector. An activity without a vector is a no-op.
	Rated(ctx context.Context, userID, activityID string, stars int) error
}

// Profile work runs detached from the request with a bounded number in
// flight; beyond that it is skipped (the next refresh catches up).
var profileSlots = make(chan struct{}, 8)

// RefreshProfile runs Profiles.Refresh within timeout and returns its error
// for the caller to log. Nil Profiles is a no-op.
func (d *Deps) RefreshProfile(ctx context.Context, userID string, timeout time.Duration) error {
	if d.Profiles == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return d.Profiles.Refresh(ctx, userID)
}

// RefreshProfileAsync runs Profiles.Refresh in the background.
func (d *Deps) RefreshProfileAsync(userID string, timeout time.Duration) {
	if d.Profiles == nil {
		return
	}
	d.goProfile("profile refresh", userID, timeout, func(ctx context.Context) error {
		return d.Profiles.Refresh(ctx, userID)
	})
}

// RatedAsync folds a rating into the user's vectors in the background.
func (d *Deps) RatedAsync(userID, activityID string, stars int, timeout time.Duration) {
	if d.Profiles == nil || activityID == "" {
		return
	}
	d.goProfile("rating feedback", userID, timeout, func(ctx context.Context) error {
		return d.Profiles.Rated(ctx, userID, activityID, stars)
	})
}

func (d *Deps) goProfile(what, userID string, timeout time.Duration, fn func(context.Context) error) {
	select {
	case profileSlots <- struct{}{}:
	default:
		log.Warn().Str("user", userID).Msg(what + " skipped: too many in flight")
		return
	}
	go func() {
		defer func() { <-profileSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Warn().Err(err).Str("user", userID).Msg(what + " failed")
		}
	}()
}
