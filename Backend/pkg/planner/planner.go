package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/travel"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Deps are the planner's collaborators. Scorer and Search may be nil (the
// priors path); Travel defaults to the heuristic; Clock to the system
// clock; NewID to UUID v7.
type Deps struct {
	Source     CandidateSource
	Embeddings EmbeddingSource
	Lookup     ActivityLookup
	Scorer     Scorer
	Search     SearchVectorizer
	Pools      PoolStore
	Travel     travel.Provider
	Clock      Clock
	NewID      func() string
}

// Planner is the engine behind /plans/* and the save step.
type Planner struct {
	Cfg        Config
	Source     CandidateSource
	Embeddings EmbeddingSource
	Lookup     ActivityLookup
	Scorer     Scorer
	Search     SearchVectorizer
	Pools      PoolStore
	Travel     travel.Provider
	Clock      Clock
	NewID      func() string

	// Background is called with the Jev goroutine so tests can wait for
	// it; nil runs it detached.
	Background func(func())
}

// New wires a planner. Missing collaborators get honest defaults.
func New(cfg Config, d Deps) *Planner {
	p := &Planner{Cfg: cfg, Source: d.Source, Embeddings: d.Embeddings, Lookup: d.Lookup, Scorer: d.Scorer,
		Search: d.Search, Pools: d.Pools, Travel: d.Travel, Clock: d.Clock, NewID: d.NewID}
	if p.Travel == nil {
		p.Travel = travel.Heuristic{}
	}
	if p.Clock == nil {
		p.Clock = SystemClock{}
	}
	if p.NewID == nil {
		p.NewID = NewID
	}
	if p.Pools == nil {
		p.Pools = NewMemPoolStore(p.Clock)
	}
	if p.Cfg.Itinerary.Paces == nil {
		p.Cfg.Itinerary = itinerary.DefaultConfig()
	}
	p.Cfg.Itinerary.SeriesCap = p.Cfg.SeriesCap
	if p.Cfg.SearchTimeout <= 0 {
		p.Cfg.SearchTimeout = 5 * time.Second
	}
	return p
}

// NewID is a UUID v7 (time-ordered) string.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// ErrNoStart means the spec has no start point at all.
var ErrNoStart = errors.New("planner: spec has no start point")

func (p *Planner) newRun(user *UserContext, spec PlanSpec) *Run {
	now := p.Clock.Now()
	cfg := p.Cfg
	itCfg := cfg.Itinerary
	run := &Run{
		ID: p.NewID(), User: user, Spec: spec, Cfg: cfg, ItCfg: itCfg,
		Pool: NewPool(), Boosts: map[string]float64{}, Relaxed: []string{},
		Deadline: now.Add(cfg.SoftBudget), K: itCfg.K, MaxSlots: itCfg.MaxSlots, Mu: itCfg.Mu,
		startedAt: now,
	}
	run.Window = spec.Window()
	run.RadiusKm = radiusFor(&spec, cfg, spec.MaxLegKm)
	if spec.SnappedStart {
		run.Relaxed = append(run.Relaxed, "snapped_start")
	}
	facetNames := make([]string, 0, len(spec.Facets))
	for _, f := range spec.Facets {
		facetNames = append(facetNames, f.Name)
	}
	run.Log = PlanRun{
		ID: run.ID, UserID: user.ID, Catalog: spec.Catalog, City: spec.City,
		CreatedAt: now, ExpiresAt: now.Add(cfg.RunTTL), Request: string(spec.Raw),
		Spec: SpecLog{
			City: spec.City, From: spec.From, BackBy: spec.BackBy, LocalDate: spec.LocalDate,
			Start: PlaceAt(spec.StartName, *spec.Start), End: PlaceAt(spec.EndName, *spec.End),
			Mode: string(spec.Mode), DriveLabel: spec.DriveLabel, Range: spec.Range, MaxLegKm: spec.MaxLegKm,
			Budget: spec.Budget, Pace: spec.Pace, Who: spec.Who, OpenSeats: spec.OpenSeats,
			MoodText: spec.MoodText, QuickPicks: spec.QuickPicks, AgeBracket: spec.AgeBracket, Flexible: spec.Flexible,
		},
		TZ: spec.TZ.String(), SnappedStart: spec.SnappedStart,
		Filters: FilterLog{
			RadiusKm: run.RadiusKm, Window: TimeSlot{From: spec.From, To: spec.BackBy}, Budget: spec.Budget,
			AgeBracket: spec.AgeBracket, ExcludeCategories: spec.Hard.ExcludeCategories, ExcludeTags: spec.Hard.ExcludeTags,
			MoodHard: spec.Hard, Facets: facetNames,
		},
		Counts:    CountLog{Drops: map[string]int{}},
		Shortlist: []ShortlistEntry{},
		Rounds:    []RoundLog{},
		ML:        MLLog{Mode: "none"},
		Timings:   map[string]int64{},
	}
	return run
}

// Generate is §5.3: retrieval, the loop, rendering, persistence and the
// first page. A request that yields nothing returns an empty batch with a
// reason; only infrastructure failures are errors.
func (p *Planner) Generate(ctx context.Context, user *UserContext, spec PlanSpec) (Batch, error) {
	if user == nil {
		user = &UserContext{}
	}
	if spec.Start == nil || spec.End == nil || spec.TZ == nil {
		return Batch{}, ErrNoStart
	}
	ctx, cancel := context.WithTimeout(ctx, p.Cfg.HardTimeout)
	defer cancel()
	run := p.newRun(user, spec)

	if err := p.retrieve(ctx, run); err != nil {
		return Batch{}, fmt.Errorf("planner: retrieval: %w", err)
	}
	if run.Pool.Len() == 0 {
		return p.finishEmpty(ctx, run, "no_candidates_fit_window")
	}
	p.runLoop(ctx, run)
	if len(run.Best) == 0 {
		return p.finishEmpty(ctx, run, "no_feasible_itinerary")
	}

	options := p.render(run)
	if len(options) == 0 {
		return p.finishEmpty(ctx, run, "no_feasible_itinerary")
	}
	pool := p.buildPool(run, options)
	jevCands := p.prepareJev(run, options)
	run.Log.Final = FinalLog{OptionIDs: optionIDs(options), Relaxed: run.Relaxed, TotalMs: p.msSince(run.startedAt), Rejected: run.rejected}
	if err := p.Pools.SavePool(ctx, pool); err != nil {
		return Batch{}, fmt.Errorf("planner: save pool: %w", err)
	}
	if err := p.Pools.SaveRun(ctx, &run.Log); err != nil {
		log.Warn().Err(err).Str("run", run.ID).Msg("planner: save run")
	}
	p.logRun(run, len(options))
	p.startJev(run, jevCands)

	batch := pageBatch(pool, 0, p.Cfg.FirstPage)
	batch.Relaxed = run.Relaxed
	if p.Cfg.Debug {
		batch.Debug = &run.Log
	}
	return batch, nil
}

func (p *Planner) finishEmpty(ctx context.Context, run *Run, reason string) (Batch, error) {
	run.Log.Final = FinalLog{OptionIDs: []string{}, Relaxed: run.Relaxed, TotalMs: p.msSince(run.startedAt), Reason: reason, Rejected: run.rejected}
	if err := p.Pools.SaveRun(ctx, &run.Log); err != nil {
		log.Warn().Err(err).Str("run", run.ID).Msg("planner: save run")
	}
	p.logRun(run, 0)
	b := Batch{Options: []Option{}, Done: true, Planner: "dag", RunID: run.ID, Reason: reason, Relaxed: run.Relaxed}
	if p.Cfg.Debug {
		b.Debug = &run.Log
	}
	return b, nil
}

func (p *Planner) logRun(run *Run, options int) {
	log.Info().
		Str("run", run.ID).
		Str("catalog", run.Spec.Catalog).
		Str("city", run.Spec.City).
		Int("events_a", run.Log.Counts.EventsA).
		Int("places_a", run.Log.Counts.PlacesA).
		Int("feasible", run.Log.Counts.Feasible).
		Int("shortlist", run.Log.Counts.Shortlist).
		Int("pool", run.Pool.Len()).
		Int("rounds", len(run.Log.Rounds)).
		Int("options", options).
		Str("ml", run.Log.ML.Mode).
		Str("query_vector", run.QV.Source).
		Strs("relaxed", run.Relaxed).
		Interface("timings_ms", run.Log.Timings).
		Int64("total_ms", run.Log.Final.TotalMs).
		Msg("planner: generate")
}

func optionIDs(options []Option) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.ID
	}
	return out
}

func (p *Planner) buildPool(run *Run, options []Option) *PlanPool {
	spec := &run.Spec
	scores := map[string]PoolScore{}
	for _, c := range run.Pool.List() {
		if c.ML != nil || c.Jev != nil {
			scores[c.ID] = PoolScore{ML: c.ML, Jev: c.Jev}
		}
	}
	return &PlanPool{
		ID: run.ID, UserID: run.User.ID, Catalog: spec.Catalog,
		CreatedAt: run.startedAt, ExpiresAt: run.startedAt.Add(p.Cfg.PoolTTL),
		Window: PoolWindow{
			From: spec.From, BackBy: spec.BackBy, TZ: spec.TZ.String(),
			Start: PlaceAt(spec.StartName, *spec.Start), End: PlaceAt(spec.EndName, *spec.End),
			Mode: string(spec.Mode), DriveLabel: spec.DriveLabel, MaxLegKm: spec.MaxLegKm,
			BudgetCents: spec.Budget.TotalCents, Pace: spec.Pace,
		},
		Spec: PoolSpec{
			City: spec.City, Catalog: spec.Catalog, Range: spec.Range, RadiusKm: run.RadiusKm, Budget: spec.Budget,
			AgeBracket: spec.AgeBracket, Hard: spec.Hard, AvoidTags: spec.AvoidTags, Facets: spec.Facets,
			Who: spec.Who, MoodText: spec.MoodText, QuickPicks: spec.QuickPicks, Flexible: spec.Flexible,
		},
		QueryVector: run.QV.Q, NegVector: run.QV.Neg, HasNeg: run.QV.HasNeg, QuerySource: run.QV.Source,
		Options: options, Alternatives: map[string]Stop{}, Scores: scores,
	}
}

// pageBatch is the stateless cursor page: options[offset:offset+n].
func pageBatch(pool *PlanPool, offset, n int) Batch {
	b := Batch{Options: []Option{}, Planner: "dag", RunID: pool.ID}
	if offset < 0 || offset >= len(pool.Options) || n <= 0 {
		b.Done = true
		return b
	}
	end := offset + n
	if end > len(pool.Options) {
		end = len(pool.Options)
	}
	b.Options = append(b.Options, pool.Options[offset:end]...)
	if end < len(pool.Options) {
		b.Cursor = EncodeCursor(pool.ID, end)
	} else {
		b.Done = true
	}
	return b
}

// More serves the next page for a cursor. Unknown or expired cursors give
// an empty last page rather than an error.
func (p *Planner) More(ctx context.Context, user *UserContext, cursor string) (Batch, error) {
	runID, offset, ok := DecodeCursor(cursor)
	if !ok {
		return Batch{Options: []Option{}, Done: true, Planner: "dag"}, nil
	}
	pool, err := p.Pools.GetPool(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrPoolNotFound) {
			return Batch{Options: []Option{}, Done: true, Planner: "dag"}, nil
		}
		return Batch{}, err
	}
	if user != nil && user.ID != "" && pool.UserID != "" && pool.UserID != user.ID {
		return Batch{Options: []Option{}, Done: true, Planner: "dag"}, nil
	}
	return pageBatch(pool, offset, p.Cfg.MorePage), nil
}

// prepareJev picks the candidates the async reranker will see and marks
// the run as having requested it (before the run is saved). With
// PLANNER_JEV=sync the reranker already ran during retrieval.
func (p *Planner) prepareJev(run *Run, options []Option) []*Candidate {
	if !p.jevEnabled() || p.Cfg.Jev != "async" || run.Log.ML.Mode != "classifier" {
		return nil
	}
	cands := p.jevCandidates(run, options)
	if len(cands) > 0 {
		run.Log.ML.Jev = &JevLog{Requested: true, Scores: map[string]float64{}}
	}
	return cands
}

// startJev fires one background rerank over the final shortlist after the
// response is built; its scores land in plan_runs.ml.jev and in the pool's
// score cache, which /plans/alternatives reads.
func (p *Planner) startJev(run *Run, cands []*Candidate) {
	if len(cands) == 0 {
		return
	}
	job := func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.Cfg.JevTimeout)
		defer cancel()
		res, err := p.Scorer.Score(ctx, p.scoreRequest(run, cands, true))
		now := p.Clock.Now()
		jl := JevLog{Requested: true, CompletedAt: &now, Scores: map[string]float64{}}
		scores := map[string]PoolScore{}
		if err != nil {
			jl.Err = err.Error()
		} else {
			for _, c := range cands {
				if s, ok := res.Rerank[c.ID]; ok {
					v := math.Max(0, math.Min(4, s))
					jl.Scores[c.ID] = v
					scores[c.ID] = PoolScore{Jev: &v}
				}
			}
		}
		if err := p.Pools.PatchRun(ctx, run.ID, map[string]any{"ml.jev": jl}); err != nil {
			log.Warn().Err(err).Str("run", run.ID).Msg("planner: jev patch run")
		}
		if len(scores) > 0 {
			if err := p.Pools.SetScores(ctx, run.ID, scores); err != nil {
				log.Warn().Err(err).Str("run", run.ID).Msg("planner: jev pool scores")
			}
		}
	}
	if p.Background != nil {
		p.Background(job)
		return
	}
	go job()
}

// jevCandidates are the stops of the rendered options plus the best of the
// pool, up to JevTopK, in a fixed order.
func (p *Planner) jevCandidates(run *Run, options []Option) []*Candidate {
	seen := map[string]bool{}
	var out []*Candidate
	add := func(id string) {
		if seen[id] || len(out) >= p.Cfg.JevTopK {
			return
		}
		if c := run.Pool.Get(id); c != nil && len(c.Act.Embedding) > 0 {
			seen[id] = true
			out = append(out, c)
		}
	}
	for _, o := range options {
		for _, s := range o.Stops {
			add(s.ActivityID)
		}
	}
	for _, id := range run.Pool.IDs() {
		add(id)
	}
	return out
}
