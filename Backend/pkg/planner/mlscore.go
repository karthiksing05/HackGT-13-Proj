package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/ml"
	"context"
	"time"
)

// MLScorer is the planner's Scorer, SearchVectorizer and JevCapable over
// the ML service. The service wants dollars and miles; the planner keeps
// cents and kilometres, so the conversion happens here and nowhere else.
type MLScorer struct {
	Client *ml.Client
}

var (
	_ Scorer           = MLScorer{}
	_ SearchVectorizer = MLScorer{}
	_ JevCapable       = MLScorer{}
)

// NewMLScorer wraps an ML client.
func NewMLScorer(c *ml.Client) MLScorer { return MLScorer{Client: c} }

// JevAvailable reports, without blocking, whether the service can rerank.
func (m MLScorer) JevAvailable() bool { return m.Client != nil && m.Client.JevAvailable() }

// SearchVector asks /v1/search-profile for this request's text and vector.
func (m MLScorer) SearchVector(ctx context.Context, in SearchInput) (SearchVector, error) {
	budget := in.Budget
	req := ml.SearchProfileRequest{
		MoodText: in.MoodText, Tags: in.Tags, Who: in.Who, Pace: appPace(in.Pace), Budget: &budget,
		Timezone: in.Timezone,
	}
	if !in.StartTime.IsZero() {
		start := in.StartTime.UTC()
		req.StartTime = &start
	}
	if !in.BackBy.IsZero() {
		backBy := in.BackBy.UTC()
		req.BackBy = &backBy
	}
	sp, err := m.Client.SearchProfile(ctx, req)
	if err != nil {
		return SearchVector{}, err
	}
	return SearchVector{Text: sp.SearchText, Embedding: sp.SearchEmbedding}, nil
}

// Score ranks the candidates with /v1/events/rank, repeating the planner's
// hard filters as the service's second check. Ids missing from the answer
// were dropped by the service.
func (m MLScorer) Score(ctx context.Context, req ScoreRequest) (ScoreResult, error) {
	user := ml.UserInput{
		PositiveEmbedding:  req.PositiveEmbedding,
		ExcludedCategories: req.ExcludedCategories,
	}
	if len(req.NegativeEmbedding) == len(req.PositiveEmbedding) {
		user.NegativeEmbedding = req.NegativeEmbedding
	}
	if req.User != nil {
		user.PositiveText, user.NegativeText = req.User.PositiveText, req.User.NegativeText
	}
	if req.MaxPriceCents != nil {
		user.MaxPrice = dollars(*req.MaxPriceCents)
	}
	if req.Center != nil && req.MaxDistanceKm > 0 {
		lat, lng, miles := req.Center.Lat, req.Center.Lng, req.MaxDistanceKm/kmPerMile
		user.Latitude, user.Longitude, user.MaxDistanceMiles = &lat, &lng, &miles
	}
	if !req.From.IsZero() && !req.BackBy.IsZero() {
		from, backBy := req.From.UTC(), req.BackBy.UTC()
		user.AvailableStart, user.AvailableEnd = &from, &backBy
	}
	events := make([]ml.EventInput, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		events = append(events, eventInput(c, req.From, req.BackBy))
	}
	in := ml.RankInput{
		User: user, Events: events, SearchText: req.SearchText,
		Opts: ml.RankingOptions{Rerank: req.Rerank},
	}
	if len(req.SearchEmbedding) == len(req.PositiveEmbedding) {
		in.SearchEmbedding = req.SearchEmbedding
	}
	if req.Rerank {
		in.Opts.RerankTopK = req.RerankTopK
	}
	out, err := m.Client.Rank(ctx, in)
	if err != nil {
		return ScoreResult{}, err
	}
	return ScoreResult{Scores: out.Scores, Rerank: out.Rerank, ModelVersion: out.ModelVersion, Reranked: out.Reranked}, nil
}

// eventInput is one candidate on the wire. Places carry no times (the
// service's time filters keep them); events attended whole their own; a
// drop-in, a multi-day span or an event that can be joined late or left
// early is sent as the part of it inside the window, so the service's
// "inside the window" and "not over yet" checks keep it.
func eventInput(c *Candidate, from, backBy time.Time) ml.EventInput {
	a := &c.Act
	lat, lng := c.Point.Lat, c.Point.Lng
	e := ml.EventInput{
		ID: c.ID, Embedding: a.Embedding, Description: c.Text,
		Latitude: &lat, Longitude: &lng, Category: a.Category,
	}
	if c.PriceKnown {
		e.Price = dollars(c.CostCents)
	}
	if a.Kind != "event" || a.Start == nil {
		return e
	}
	start := a.Start.UTC()
	var end *time.Time
	if a.End != nil && a.End.After(start) {
		v := a.End.UTC()
		end = &v
	}
	dropIn := (a.Attendance != nil && *a.Attendance == "drop_in") || (end != nil && end.Sub(start) > 6*time.Hour) ||
		itinerary.Clippable(a)
	if dropIn && !from.IsZero() {
		if start.Before(from) {
			start = from
		}
		if end == nil || end.After(backBy) {
			v := backBy
			end = &v
		}
	}
	e.StartTime, e.EndTime = &start, end
	return e
}

func dollars(cents int64) *float64 {
	v := float64(cents) / 100
	return &v
}

// appPace is the app's pace word ("chill" is the planner's name for relaxed).
func appPace(p string) string {
	if p == "chill" {
		return "relaxed"
	}
	return p
}
