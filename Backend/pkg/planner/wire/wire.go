// Package wire builds the planner that main.go installs as api.Deps.Planner:
// the Mongo store for catalogs, pools and runs, the ML service for search
// vectors and scores, and the PLANNER_* knobs. It lives apart from
// pkg/planner because mongosource imports the planner.
package wire

import (
	"Backend/pkg/ml"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"context"
	"errors"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Planner is the api.Planner over db and the ML client (nil: priors only,
// which plan_runs records). The pool indexes are ensured here too; the
// store's EnsureIndexes creates the TTL ones under the same names, so a
// failure is logged, not fatal.
func Planner(ctx context.Context, db *mongo.Database, mlc *ml.Client) (*planner.Service, error) {
	if db == nil {
		return nil, errors.New("planner: no database")
	}
	cfg := planner.FromEnv()
	st := mongosource.New(db, nil)
	if err := st.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("planner: pool indexes")
	}
	deps := planner.Deps{Source: st, Embeddings: st, Lookup: st, Pools: st}
	if mlc != nil {
		scorer := planner.NewMLScorer(mlc)
		deps.Scorer, deps.Search = scorer, scorer
	}
	p, err := planner.New(cfg, deps)
	if err != nil {
		return nil, err
	}
	log.Info().Str("jev", cfg.Jev).Int("rounds", cfg.Rounds).Dur("soft_budget", cfg.SoftBudget).
		Bool("ml", mlc != nil).Msg("planner ready")
	return planner.NewService(p), nil
}
