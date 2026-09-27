package ml

import (
	"context"
	"net/http"
)

// Match is one taste match: Score is the service's raw score, Percent it on
// 0–100 for display.
type Match struct {
	ID      string  `json:"id"`
	Score   float64 `json:"score"`
	Percent int     `json:"percent"`
}

// UserCandidate is another user to match against.
type UserCandidate struct {
	ID       string
	Positive []float64
	Negative []float64 // nil or all zeros: no dislikes
}

// ItineraryStops is an itinerary's stop vectors keyed by activity id.
type ItineraryStops struct {
	ID    string
	Stops map[string][]float64
}

type matchUser struct {
	PositiveEmbedding Vec `json:"positive_embedding"`
	NegativeEmbedding Vec `json:"negative_embedding"`
}

type userCandidate struct {
	ID string `json:"id"`
	matchUser
}

type matchEvent struct {
	ID        string `json:"id"`
	Embedding Vec    `json:"embedding"`
}

type itineraryInput struct {
	ID     string       `json:"id"`
	Events []matchEvent `json:"events"`
}

type matchResponse struct {
	Results []Match `json:"results"`
}

// matchVectors is the wire form of a user's vectors: a missing or unusable
// negative vector goes as zeros (no dislikes). ok is false without a usable
// positive vector.
func (c *Client) matchVectors(positive, negative []float64) (matchUser, bool) {
	dim := c.opts.Dim
	if !Usable(positive, dim) {
		return matchUser{}, false
	}
	if !Usable(negative, dim) {
		negative = make([]float64, dim)
	}
	return matchUser{PositiveEmbedding: positive, NegativeEmbedding: negative}, true
}

// UserCompatibility is POST /v1/compatibility/users: how well each candidate's
// taste matches the viewer's, best first. Candidates without a usable positive
// vector are left out; a viewer without one is ErrNoUserEmbedding.
func (c *Client) UserCompatibility(ctx context.Context, viewer UserVectors, cands []UserCandidate) ([]Match, error) {
	user, ok := c.matchVectors(viewer.Positive, viewer.Negative)
	if !ok {
		return nil, ErrNoUserEmbedding
	}
	body := struct {
		User       matchUser       `json:"user"`
		Candidates []userCandidate `json:"candidates"`
	}{User: user, Candidates: []userCandidate{}}
	seen := map[string]bool{}
	for _, cand := range cands {
		v, ok := c.matchVectors(cand.Positive, cand.Negative)
		if !ok || cand.ID == "" || seen[cand.ID] {
			continue
		}
		seen[cand.ID] = true
		body.Candidates = append(body.Candidates, userCandidate{ID: cand.ID, matchUser: v})
	}
	if len(body.Candidates) == 0 {
		return []Match{}, nil
	}
	var resp matchResponse
	if err := c.do(ctx, http.MethodPost, "/v1/compatibility/users", c.opts.RankTimeout, body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// ItineraryCompatibility is POST /v1/compatibility/itineraries: the mean
// compatibility-model score of each itinerary's stops for the viewer, best
// first. Stops without a usable vector are left out, and itineraries left
// with none are absent from the answer.
func (c *Client) ItineraryCompatibility(ctx context.Context, viewer UserVectors, its []ItineraryStops) ([]Match, error) {
	user, ok := c.matchVectors(viewer.Positive, viewer.Negative)
	if !ok {
		return nil, ErrNoUserEmbedding
	}
	body := struct {
		User        matchUser        `json:"user"`
		Itineraries []itineraryInput `json:"itineraries"`
	}{User: user, Itineraries: []itineraryInput{}}
	seen := map[string]bool{}
	for _, it := range its {
		if it.ID == "" || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		in := itineraryInput{ID: it.ID, Events: []matchEvent{}}
		for id, v := range it.Stops {
			if id != "" && Usable(v, c.opts.Dim) {
				in.Events = append(in.Events, matchEvent{ID: id, Embedding: v})
			}
		}
		if len(in.Events) > 0 {
			body.Itineraries = append(body.Itineraries, in)
		}
	}
	if len(body.Itineraries) == 0 {
		return []Match{}, nil
	}
	var resp matchResponse
	if err := c.do(ctx, http.MethodPost, "/v1/compatibility/itineraries", c.opts.RankTimeout, body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
