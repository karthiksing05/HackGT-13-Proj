package planner

import (
	"Backend/pkg/travel"
	"context"
	"time"
)

// SearchInput is what the ML service turns into the per-request search
// text and vector (POST /v1/search-profile).
type SearchInput struct {
	MoodText  string
	Tags      []string
	Who       string
	Pace      string
	Budget    int
	StartTime time.Time
	BackBy    time.Time
	Timezone  string
}

// SearchVector is the service's answer; Embedding is nil when the text was
// empty or the service was unavailable.
type SearchVector struct {
	Text      string
	Embedding []float64
}

// SearchVectorizer produces the search vector for a request.
type SearchVectorizer interface {
	SearchVector(ctx context.Context, in SearchInput) (SearchVector, error)
}

// ScoreRequest is one classifier (or reranker) call over candidates that
// already passed every hard filter. The constraints are repeated so the
// service can apply its own second check.
type ScoreRequest struct {
	User              *UserContext
	PositiveEmbedding []float64 // the user's vector, else the search vector
	NegativeEmbedding []float64
	Candidates        []*Candidate
	Query             QueryVector
	SearchText        string
	SearchEmbedding   []float64

	Rerank     bool
	RerankTopK int

	From, BackBy       time.Time
	Center             *travel.Point
	MaxDistanceKm      float64
	MaxPriceCents      *int64
	ExcludedCategories []string
}

// ScoreResult maps candidate ids to scores. Ids missing from Scores were
// dropped by the service and stay dropped.
type ScoreResult struct {
	Scores       map[string]float64 // classifier 0..1
	Rerank       map[string]float64 // reranker 0..4, when it ran
	ModelVersion string
	Reranked     bool
}

// Scorer is the ML classifier (± the Jev reranker).
type Scorer interface {
	Score(ctx context.Context, req ScoreRequest) (ScoreResult, error)
}

// JevCapable is implemented by scorers that can rerank; the planner only
// schedules Jev when it says so.
type JevCapable interface {
	JevAvailable() bool
}

// Clock is the planner's time source, fixed in tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time { return f() }
