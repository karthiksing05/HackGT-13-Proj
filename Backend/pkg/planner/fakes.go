package planner

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Fakes for every seam. They live in the package (not a _test file) so the
// Mongo implementation's tests and the service layer's tests can use them.

// FakeSource serves activities from memory with the same filter semantics
// as the Mongo source (MatchesQuery). Hidden activities only appear for
// expansion queries (IncludeCategories or AnyTags set), which lets a test
// prove a round-1 expansion found something round 0 could not.
type FakeSource struct {
	mu         sync.Mutex
	Activities []models.Activity
	Hidden     []models.Activity
	Embeddings map[string][]float64               // by hex id; nil entries mean "no vector"
	VectorFor  func(a *models.Activity) []float64 // used when Embeddings has no entry
	Err        error
	Calls      []CandidateQuery
	EmbedCalls [][]string
	Delay      time.Duration
}

func (f *FakeSource) FindCandidates(ctx context.Context, q CandidateQuery) ([]models.Activity, []models.Activity, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, q)
	f.mu.Unlock()
	if f.Err != nil {
		return nil, nil, f.Err
	}
	if f.Delay > 0 {
		select {
		case <-time.After(f.Delay):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	pool := f.Activities
	if len(q.IncludeCategories)+len(q.AnyTags) > 0 {
		pool = append(append([]models.Activity(nil), f.Activities...), f.Hidden...)
	}
	var events, places []models.Activity
	for i := range pool {
		a := pool[i]
		if !MatchesQuery(&a, &q) {
			continue
		}
		// Like the store's projection: vectors and texts come later, and
		// only for the survivors (FetchEmbeddings, FetchTexts).
		a.Embedding, a.EmbeddingText = nil, nil
		if a.Kind == "event" {
			events = append(events, a)
		} else {
			places = append(places, a)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].Start.Equal(*events[j].Start) {
			return events[i].Start.Before(*events[j].Start)
		}
		return events[i].ID.Hex() < events[j].ID.Hex()
	})
	sort.SliceStable(places, func(i, j int) bool {
		ri, rj := floatOrNeg(places[i].Rating), floatOrNeg(places[j].Rating)
		if ri != rj {
			return ri > rj
		}
		pi, pj := floatOrNeg(places[i].Popularity), floatOrNeg(places[j].Popularity)
		if pi != pj {
			return pi > pj
		}
		return places[i].ID.Hex() < places[j].ID.Hex()
	})
	if q.LimitEvents > 0 && len(events) > q.LimitEvents {
		events = events[:q.LimitEvents]
	}
	if q.LimitPlaces > 0 && len(places) > q.LimitPlaces {
		places = places[:q.LimitPlaces]
	}
	return events, places, nil
}

func floatOrNeg(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}

func (f *FakeSource) FetchEmbeddings(ctx context.Context, catalog string, ids []string) (map[string][]float64, error) {
	f.mu.Lock()
	f.EmbedCalls = append(f.EmbedCalls, append([]string(nil), ids...))
	f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	byID := map[string]*models.Activity{}
	for i := range f.Activities {
		byID[f.Activities[i].ID.Hex()] = &f.Activities[i]
	}
	for i := range f.Hidden {
		byID[f.Hidden[i].ID.Hex()] = &f.Hidden[i]
	}
	out := map[string][]float64{}
	for _, id := range ids {
		if v, ok := f.Embeddings[id]; ok {
			if v != nil {
				out[id] = v
			}
			continue
		}
		a := byID[id]
		if a == nil {
			continue
		}
		if len(a.Embedding) > 0 {
			out[id] = a.Embedding
		} else if f.VectorFor != nil {
			if v := f.VectorFor(a); v != nil {
				out[id] = v
			}
		}
	}
	return out, nil
}

// FetchTexts returns each activity's embedding text, else its name
// (synthetic activities carry no text).
func (f *FakeSource) FetchTexts(ctx context.Context, catalog string, ids []string) (map[string]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string]string{}
	for _, list := range [][]models.Activity{f.Activities, f.Hidden} {
		for _, a := range list {
			if !want[a.ID.Hex()] {
				continue
			}
			if a.EmbeddingText != nil && *a.EmbeddingText != "" {
				out[a.ID.Hex()] = *a.EmbeddingText
			} else {
				out[a.ID.Hex()] = a.Name
			}
		}
	}
	return out, nil
}

func (f *FakeSource) GetActivities(ctx context.Context, catalog string, ids []string) ([]models.Activity, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []models.Activity
	for _, list := range [][]models.Activity{f.Activities, f.Hidden} {
		for _, a := range list {
			if want[a.ID.Hex()] {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

// FakeScorer scores with a function and can drop ids, fail, or pretend to
// rerank. It counts calls so tests can assert "one classifier call".
type FakeScorer struct {
	mu       sync.Mutex
	Fn       func(c *Candidate) float64 // nil: 0.5 for everyone
	RerankFn func(c *Candidate) float64 // nil: Fn·4
	Drop     map[string]bool
	Err      error
	Jev      bool
	Calls    []ScoreRequest
}

func (f *FakeScorer) Score(ctx context.Context, req ScoreRequest) (ScoreResult, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, req)
	f.mu.Unlock()
	if f.Err != nil {
		return ScoreResult{}, f.Err
	}
	if err := ctx.Err(); err != nil {
		return ScoreResult{}, err
	}
	res := ScoreResult{Scores: map[string]float64{}, ModelVersion: "fake"}
	if req.Rerank {
		res.Rerank = map[string]float64{}
		res.Reranked = true
	}
	for i, c := range req.Candidates {
		if f.Drop[c.ID] {
			continue
		}
		s := 0.5
		if f.Fn != nil {
			s = f.Fn(c)
		}
		res.Scores[c.ID] = s
		if req.Rerank && (req.RerankTopK <= 0 || i < req.RerankTopK) {
			if f.RerankFn != nil {
				res.Rerank[c.ID] = f.RerankFn(c)
			} else {
				res.Rerank[c.ID] = s * 4
			}
		}
	}
	return res, nil
}

func (f *FakeScorer) JevAvailable() bool { return f.Jev }

// CallCount is how many Score calls were made.
func (f *FakeScorer) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// FakeVectorizer returns a vector computed from the input, or none.
type FakeVectorizer struct {
	Fn    func(in SearchInput) []float64
	Err   error
	Calls []SearchInput
}

func (f *FakeVectorizer) SearchVector(ctx context.Context, in SearchInput) (SearchVector, error) {
	f.Calls = append(f.Calls, in)
	if f.Err != nil {
		return SearchVector{}, f.Err
	}
	var emb []float64
	if f.Fn != nil {
		emb = f.Fn(in)
	}
	return SearchVector{Text: in.MoodText, Embedding: emb}, nil
}

// FakeClock is a settable clock.
type FakeClock struct {
	mu sync.Mutex
	T  time.Time
}

func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{T: t} }

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.T
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.T = c.T.Add(d)
}

// MemPoolStore keeps pools and runs in memory with the same TTL behaviour
// as Mongo (documents past ExpiresAt are gone).
type MemPoolStore struct {
	mu    sync.Mutex
	Clock Clock
	pools map[string]*PlanPool
	runs  map[string]*PlanRun
	Err   error
}

func NewMemPoolStore(clock Clock) *MemPoolStore {
	if clock == nil {
		clock = SystemClock{}
	}
	return &MemPoolStore{Clock: clock, pools: map[string]*PlanPool{}, runs: map[string]*PlanRun{}}
}

func (m *MemPoolStore) SaveRun(ctx context.Context, run *PlanRun) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *run
	m.runs[run.ID] = &cp
	return nil
}

func (m *MemPoolStore) PatchRun(ctx context.Context, id string, patch bson.M) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return ErrPoolNotFound
	}
	// The patches the planner writes are known: outcome, ml.jev and timings.
	for k, v := range patch {
		switch k {
		case "outcome":
			if o, ok := v.(OutcomeLog); ok {
				run.Outcome = &o
			} else if o, ok := v.(*OutcomeLog); ok {
				run.Outcome = o
			}
		case "ml.jev":
			if j, ok := v.(JevLog); ok {
				run.ML.Jev = &j
			} else if j, ok := v.(*JevLog); ok {
				run.ML.Jev = j
			}
		}
	}
	return nil
}

func (m *MemPoolStore) SavePool(ctx context.Context, pool *PlanPool) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *pool
	m.pools[pool.ID] = &cp
	return nil
}

func (m *MemPoolStore) GetPool(ctx context.Context, runID string) (*PlanPool, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[runID]
	if !ok || !p.ExpiresAt.After(m.Clock.Now()) {
		return nil, ErrPoolNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemPoolStore) AddAlternatives(ctx context.Context, runID string, alts []Stop) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[runID]
	if !ok {
		return ErrPoolNotFound
	}
	if p.Alternatives == nil {
		p.Alternatives = map[string]Stop{}
	}
	for _, s := range alts {
		p.Alternatives[s.ID] = s
	}
	return nil
}

func (m *MemPoolStore) SetScores(ctx context.Context, runID string, scores map[string]PoolScore) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[runID]
	if !ok {
		return ErrPoolNotFound
	}
	if p.Scores == nil {
		p.Scores = map[string]PoolScore{}
	}
	for id, s := range scores {
		cur := p.Scores[id]
		if s.ML != nil {
			cur.ML = s.ML
		}
		if s.Jev != nil {
			cur.Jev = s.Jev
		}
		p.Scores[id] = cur
	}
	return nil
}

// GetRun returns a saved run (tests only; the Mongo store has no reader).
func (m *MemPoolStore) GetRun(id string) (*PlanRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// ErrPoolNotFound means the run's pool expired or never existed.
var ErrPoolNotFound = errors.New("plan pool not found")
