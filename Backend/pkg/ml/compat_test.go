package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestUserCompatibility(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/compatibility/users", http.StatusOK, map[string]any{"results": []map[string]any{
		{"id": "b", "score": 0.8, "percent": 90}, {"id": "a", "score": 0.4, "percent": 10},
	}})
	c := f.client()
	viewer := ml.UserVectors{Positive: basis(ml.Dim, 0)}
	got, err := c.UserCompatibility(context.Background(), viewer, []ml.UserCandidate{
		{ID: "a", Positive: basis(ml.Dim, 1), Negative: basis(ml.Dim, 2)},
		{ID: "b", Positive: basis(ml.Dim, 3)},
		{ID: "no-vector"},
		{ID: "a", Positive: basis(ml.Dim, 4)},
	})
	if err != nil || len(got) != 2 || got[0].ID != "b" || got[0].Percent != 90 {
		t.Fatalf("got %+v, %v", got, err)
	}
	body := f.body("/v1/compatibility/users", -1)
	user := body["user"].(map[string]any)
	if neg := user["negative_embedding"].([]any); len(neg) != ml.Dim || neg[0] != 0.0 {
		t.Fatalf("missing dislikes should go as zeros, got %d values", len(neg))
	}
	cands := body["candidates"].([]any)
	if len(cands) != 2 {
		t.Fatalf("sent %d candidates, want 2 (no vector and repeated id dropped)", len(cands))
	}

	if _, err := c.UserCompatibility(context.Background(), ml.UserVectors{}, nil); !errors.Is(err, ml.ErrNoUserEmbedding) {
		t.Fatalf("no viewer vector: err = %v", err)
	}
	if got, err := c.UserCompatibility(context.Background(), viewer, nil); err != nil || len(got) != 0 {
		t.Fatalf("no candidates: %v, %v", got, err)
	}
	if n := f.count("/v1/compatibility/users"); n != 1 {
		t.Fatalf("service called %d times, want 1", n)
	}
}

func TestItineraryCompatibility(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/compatibility/itineraries", http.StatusOK, map[string]any{
		"results":       []map[string]any{{"id": "p1", "score": 0.7, "percent": 70, "scored_events": 2}},
		"model_version": "classifier-v1",
	})
	c := f.client()
	viewer := ml.UserVectors{Positive: basis(ml.Dim, 0)}
	got, err := c.ItineraryCompatibility(context.Background(), viewer, []ml.ItineraryStops{
		{ID: "p1", Stops: map[string][]float64{"a": basis(ml.Dim, 1), "b": basis(ml.Dim, 2), "bad": make([]float64, ml.Dim)}},
		{ID: "empty", Stops: map[string][]float64{"x": nil}},
	})
	if err != nil || len(got) != 1 || got[0].Percent != 70 {
		t.Fatalf("got %+v, %v", got, err)
	}
	its := f.body("/v1/compatibility/itineraries", -1)["itineraries"].([]any)
	if len(its) != 1 || len(its[0].(map[string]any)["events"].([]any)) != 2 {
		t.Fatalf("sent %v", its)
	}

	f.reply("/v1/compatibility/itineraries", http.StatusServiceUnavailable, map[string]any{"detail": "down"})
	if _, err := c.ItineraryCompatibility(context.Background(), viewer, []ml.ItineraryStops{{ID: "p1", Stops: map[string][]float64{"a": basis(ml.Dim, 1)}}}); !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("503: err = %v", err)
	}
}
