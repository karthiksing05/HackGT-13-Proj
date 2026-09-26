package ml

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// UserInput is the user side of POST /v1/events/rank. Prices are in dollars, the unit of every
// EventInput.Price. A missing optional field disables that hard filter on the service.
type UserInput struct {
	PositiveEmbedding  Vec        `json:"positive_embedding"`
	NegativeEmbedding  Vec        `json:"negative_embedding"` // empty: no dislikes (sent as zeros)
	PositiveText       string     `json:"positive_text,omitempty"`
	NegativeText       string     `json:"negative_text,omitempty"`
	MaxPrice           *float64   `json:"max_price,omitempty"`
	Latitude           *float64   `json:"latitude,omitempty"`
	Longitude          *float64   `json:"longitude,omitempty"`
	MaxDistanceMiles   *float64   `json:"max_distance_miles,omitempty"`
	AvailableStart     *time.Time `json:"available_start,omitempty"`
	AvailableEnd       *time.Time `json:"available_end,omitempty"`
	ExcludedCategories []string   `json:"excluded_categories,omitempty"`
}

// EventInput is one candidate for POST /v1/events/rank. The service's availability filter keeps
// an event only when StartTime >= AvailableStart and (EndTime, else StartTime) <= AvailableEnd,
// and always drops events that have ended: for a drop-in that is already running, send the
// visit's times clipped to the window (RankActivities does this for Candidate.DropIn).
type EventInput struct {
	ID          string     `json:"id"`
	Embedding   Vec        `json:"embedding"`
	Description string     `json:"description,omitempty"` // eight-section embeddingText, for Jev
	Price       *float64   `json:"price,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
	Latitude    *float64   `json:"latitude,omitempty"`
	Longitude   *float64   `json:"longitude,omitempty"`
	Category    string     `json:"category,omitempty"`
}

// RankingOptions: Limit 0 means no limit (applied after the rerank); Rerank false skips Jev.
type RankingOptions struct {
	MinScore   *float64 `json:"min_score,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Rerank     bool     `json:"rerank"`
	RerankTopK int      `json:"rerank_top_k,omitempty"`
}

// RankEventsRequest is the body of POST /v1/events/rank. SearchEmbedding and SearchText are this
// request's search (blended into the positive vector for this call only; read by Jev).
type RankEventsRequest struct {
	User            UserInput      `json:"user"`
	Events          []EventInput   `json:"events"`
	SearchEmbedding Vec            `json:"search_embedding,omitempty"`
	SearchText      string         `json:"search_text,omitempty"`
	Options         RankingOptions `json:"options"`
}

// RankedEvent is one scored event: Score is the classifier's (roughly 0–1), RerankScore Jev's
// 0–4 score when Jev judged it.
type RankedEvent struct {
	EventID     string   `json:"event_id"`
	Score       float64  `json:"score"`
	RerankScore *float64 `json:"rerank_score"`
}

// RankEventsResponse lists the surviving events best first. Events the service dropped (hard
// filters, min_score, limit) are absent.
type RankEventsResponse struct {
	Events       []RankedEvent `json:"events"`
	ModelVersion string        `json:"model_version"`
	Reranked     bool          `json:"reranked"`
}

// RankEvents is POST /v1/events/rank. It checks what the service would reject with a 400 (an
// unusable positive vector, event vectors of the wrong size or all zeros, repeated ids) and sends
// zeros for a missing negative vector. Deadline: RerankTimeout when Options.Rerank is set (with
// RerankTopK defaulting to the client's), else RankTimeout, unless ctx has one.
func (c *Client) RankEvents(ctx context.Context, req *RankEventsRequest) (*RankEventsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil rank request", ErrInvalidRequest)
	}
	dim := c.opts.Dim
	if !Usable(req.User.PositiveEmbedding, dim) {
		return nil, ErrNoUserEmbedding
	}
	body := *req
	switch n := len(body.User.NegativeEmbedding); {
	case n == 0:
		body.User.NegativeEmbedding = make(Vec, dim) // no dislikes: the classifier's zero vector
	case n != dim:
		return nil, fmt.Errorf("%w: negative_embedding has %d values, want %d", ErrInvalidRequest, n, dim)
	}
	if n := len(body.SearchEmbedding); n != 0 && n != dim {
		return nil, fmt.Errorf("%w: search_embedding has %d values, want %d", ErrInvalidRequest, n, dim)
	}
	if body.Events == nil {
		body.Events = []EventInput{}
	}
	seen := make(map[string]bool, len(body.Events))
	for _, e := range body.Events {
		if e.ID == "" || seen[e.ID] {
			return nil, fmt.Errorf("%w: event id %q is empty or repeated", ErrInvalidRequest, e.ID)
		}
		seen[e.ID] = true
		if !Usable(e.Embedding, dim) {
			return nil, fmt.Errorf("%w: event %q has no usable %d-d embedding", ErrInvalidRequest, e.ID, dim)
		}
	}
	timeout := c.opts.RankTimeout
	if body.Options.Rerank {
		timeout = c.opts.RerankTimeout
		if body.Options.RerankTopK <= 0 {
			body.Options.RerankTopK = c.opts.RerankTopK
		}
	}
	var resp RankEventsResponse
	if err := c.do(ctx, http.MethodPost, "/v1/events/rank", timeout, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RankInput is one scoring call in the planner's shape: every event must carry a usable vector
// and pass the caller's own hard filters already.
type RankInput struct {
	User            UserInput
	Events          []EventInput
	SearchEmbedding []float64
	SearchText      string
	Opts            RankingOptions
}

// RankOutput maps event ids to the classifier's score and, when it ran, Jev's. Ids missing from
// Scores were dropped by the service and stay dropped.
type RankOutput struct {
	Scores       map[string]float64
	Rerank       map[string]float64
	Order        []string // the service's order: Jev-judged first by Jev score, then by score
	ModelVersion string
	Reranked     bool
	Sent         int
	Latency      time.Duration
}

// Rank scores events with RankEvents and returns the scores by id.
func (c *Client) Rank(ctx context.Context, in RankInput) (RankOutput, error) {
	started := time.Now()
	resp, err := c.RankEvents(ctx, &RankEventsRequest{
		User:            in.User,
		Events:          in.Events,
		SearchEmbedding: in.SearchEmbedding,
		SearchText:      in.SearchText,
		Options:         in.Opts,
	})
	out := RankOutput{Sent: len(in.Events), Latency: time.Since(started)}
	if err != nil {
		return out, err
	}
	out.Scores = make(map[string]float64, len(resp.Events))
	out.Rerank = make(map[string]float64)
	out.Order = make([]string, 0, len(resp.Events))
	for _, e := range resp.Events {
		out.Scores[e.EventID] = e.Score
		if e.RerankScore != nil {
			out.Rerank[e.EventID] = *e.RerankScore
		}
		out.Order = append(out.Order, e.EventID)
	}
	out.ModelVersion, out.Reranked = resp.ModelVersion, resp.Reranked
	log.Debug().Str("model_version", out.ModelVersion).Int("sent", out.Sent).Int("returned", len(out.Order)).
		Bool("reranked", out.Reranked).Dur("latency", out.Latency).Msg("ml rank")
	return out, nil
}

// Reasons RankActivities did not use the service's order.
const (
	ReasonNoUserEmbedding      = "no_user_embedding"      // the user has no usable positive vector
	ReasonNoEmbeddedCandidates = "no_embedded_candidates" // no candidate has a usable vector
	ReasonMLUnavailable        = "ml_unavailable"         // the rank call failed or timed out
)

// Candidate is an activity to rank, with what the service needs to score and filter it.
type Candidate struct {
	ID          string
	Embedding   []float64 `json:"-"` // the stored activity vector; anything but a usable Dim vector leaves it unembedded
	Description string    // the eight-section embeddingText, read by the Jev rerank
	Price       *float64  // dollars (the unit of Constraints.MaxPrice); nil = unknown, never filtered
	Start, End  *time.Time
	DropIn      bool // can be joined any time while it runs: its times are clipped to the availability window
	Lat, Lng    *float64
	Category    string
}

// UserVectors are the user's stored profile vectors and texts.
type UserVectors struct {
	Positive     []float64 `json:"-"`
	Negative     []float64 `json:"-"` // nil or all zeros: no dislikes
	PositiveText string
	NegativeText string
}

// Search is the per-request search vector and text from SearchProfile; both are optional.
type Search struct {
	Embedding []float64 `json:"-"`
	Text      string
}

// Constraints repeat the caller's own hard filters so the service applies them as a second check.
type Constraints struct {
	MaxPrice           *float64 // dollars
	Lat, Lng           *float64
	MaxDistanceMiles   *float64
	AvailableStart     *time.Time
	AvailableEnd       *time.Time
	ExcludedCategories []string
}

// RankedItem is one candidate in the final order; Score is nil when the service did not score it.
type RankedItem struct {
	ID          string
	Score       *float64
	RerankScore *float64
}

// RankResult is RankActivities' answer: the service's order, then the unembedded candidates
// unscored in input order. Candidates the service dropped are not in Items.
type RankResult struct {
	Items        []RankedItem
	ModelVersion string
	Reranked     bool
	Reason       string // "" when the service's order was used, else a Reason* constant
	Sent         int    // candidates sent to the service
	Returned     int    // candidates it scored
	Dropped      int    // Sent − Returned: removed by its filters, min score or limit
	Unembedded   int    // candidates without a usable vector, appended unscored
	Latency      time.Duration
}

// IDs are the candidate ids in final order.
func (r RankResult) IDs() []string {
	ids := make([]string, len(r.Items))
	for i, item := range r.Items {
		ids[i] = item.ID
	}
	return ids
}

// RankActivities ranks candidates for a user (limit 0 = all).
//
//   - No usable user vector: input order, Reason no_user_embedding, no call.
//   - Only candidates with a usable Dim vector are sent; the others are appended unscored.
//   - Candidates the service drops (constraints, limit) are not re-appended.
//   - Jev reranks only when the user has a positive text, ML_RERANK allows it and /healthz says
//     jev: true; the call then gets the rerank deadline and top-k.
//   - The service failing or timing out: input order, Reason ml_unavailable.
func (c *Client) RankActivities(ctx context.Context, user UserVectors, activities []Candidate, search Search, cons Constraints, limit int) RankResult {
	dim := c.opts.Dim
	unique := dedupe(activities)
	if !Usable(user.Positive, dim) {
		log.Debug().Int("candidates", len(unique)).Msg("ml rank skipped: no user embedding")
		return RankResult{Items: unscored(unique, limit), Reason: ReasonNoUserEmbedding}
	}
	var events []EventInput
	var unembedded []Candidate
	for _, a := range unique {
		if Usable(a.Embedding, dim) {
			events = append(events, eventInput(a, cons))
		} else {
			unembedded = append(unembedded, a)
		}
	}
	res := RankResult{Unembedded: len(unembedded)}
	if len(events) == 0 {
		res.Items, res.Reason = unscored(unique, limit), ReasonNoEmbeddedCandidates
		return res
	}
	negative := user.Negative
	if len(negative) != dim {
		negative = nil // RankEvents sends zeros: no dislikes
	}
	req := &RankEventsRequest{
		User: UserInput{
			PositiveEmbedding:  user.Positive,
			NegativeEmbedding:  negative,
			PositiveText:       user.PositiveText,
			NegativeText:       user.NegativeText,
			MaxPrice:           cons.MaxPrice,
			Latitude:           cons.Lat,
			Longitude:          cons.Lng,
			MaxDistanceMiles:   cons.MaxDistanceMiles,
			AvailableStart:     cons.AvailableStart,
			AvailableEnd:       cons.AvailableEnd,
			ExcludedCategories: cons.ExcludedCategories,
		},
		Events:     events,
		SearchText: search.Text,
		Options:    RankingOptions{Limit: max(limit, 0)},
	}
	if Usable(search.Embedding, dim) {
		req.SearchEmbedding = search.Embedding
	}
	if user.PositiveText != "" && c.opts.Rerank {
		if h, err := c.Health(ctx); err == nil && h.Jev {
			req.Options.Rerank, req.Options.RerankTopK = true, c.opts.RerankTopK
		}
	}

	started := time.Now()
	resp, err := c.RankEvents(ctx, req)
	res.Sent, res.Latency = len(events), time.Since(started)
	if err != nil {
		log.Warn().Err(err).Int("sent", res.Sent).Dur("latency", res.Latency).Msg("ml rank failed; keeping input order")
		res.Items, res.Reason = unscored(unique, limit), ReasonMLUnavailable
		return res
	}
	pending := make(map[string]bool, len(events))
	for _, e := range events {
		pending[e.ID] = true
	}
	for _, e := range resp.Events {
		if !pending[e.EventID] { // not one of ours, or repeated
			continue
		}
		delete(pending, e.EventID)
		score := e.Score
		res.Items = append(res.Items, RankedItem{ID: e.EventID, Score: &score, RerankScore: e.RerankScore})
	}
	res.Returned = len(res.Items)
	res.Dropped = res.Sent - res.Returned
	for _, a := range unembedded {
		res.Items = append(res.Items, RankedItem{ID: a.ID})
	}
	if limit > 0 && len(res.Items) > limit {
		res.Items = res.Items[:limit]
	}
	res.ModelVersion, res.Reranked = resp.ModelVersion, resp.Reranked
	log.Info().Str("model_version", res.ModelVersion).Int("sent", res.Sent).Int("returned", res.Returned).
		Int("dropped", res.Dropped).Int("unembedded", res.Unembedded).Bool("reranked", res.Reranked).
		Dur("latency", res.Latency).Msg("ml rank")
	return res
}

func eventInput(a Candidate, cons Constraints) EventInput {
	e := EventInput{
		ID:          a.ID,
		Embedding:   a.Embedding,
		Description: a.Description,
		Price:       a.Price,
		StartTime:   a.Start,
		EndTime:     a.End,
		Latitude:    a.Lat,
		Longitude:   a.Lng,
		Category:    a.Category,
	}
	if a.DropIn {
		e.StartTime, e.EndTime = clipToWindow(a.Start, a.End, cons.AvailableStart, cons.AvailableEnd)
	}
	return e
}

// clipToWindow narrows a drop-in's [start, end] to the availability window, so the service's
// "inside the window" filter keeps a drop-in that overlaps it. Open-ended listings and intervals
// that miss the window are returned unchanged.
func clipToWindow(start, end, from, to *time.Time) (*time.Time, *time.Time) {
	if start == nil || end == nil {
		return start, end
	}
	s, e := *start, *end
	if (from != nil && e.Before(*from)) || (to != nil && s.After(*to)) {
		return start, end
	}
	if from != nil && s.Before(*from) {
		s = *from
	}
	if to != nil && e.After(*to) {
		e = *to
	}
	return &s, &e
}

// dedupe keeps the first candidate per id and drops candidates without one.
func dedupe(activities []Candidate) []Candidate {
	seen := make(map[string]bool, len(activities))
	out := make([]Candidate, 0, len(activities))
	for _, a := range activities {
		if a.ID == "" || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out
}

func unscored(activities []Candidate, limit int) []RankedItem {
	n := len(activities)
	if limit > 0 && limit < n {
		n = limit
	}
	items := make([]RankedItem, n)
	for i := range items {
		items[i] = RankedItem{ID: activities[i].ID}
	}
	return items
}
