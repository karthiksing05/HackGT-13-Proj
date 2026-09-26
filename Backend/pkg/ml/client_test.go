package ml_test

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRankEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events/rank" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req ml.RankEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(req.Events) != 2 {
			t.Errorf("expected 2 events, got %d", len(req.Events))
		}

		rerankScore := 3.8
		resp := ml.RankEventsResponse{
			Events: []ml.RankedEvent{
				{EventID: "evt_2", Score: 0.92, RerankScore: &rerankScore},
				{EventID: "evt_1", Score: 0.74, RerankScore: nil},
			},
			ModelVersion: "classifier-v1",
			Reranked:     true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := ml.NewClient(server.URL)
	req := ml.RankEventsRequest{
		User: ml.UserInput{
			PositiveEmbedding: make([]float64, 1024),
			NegativeEmbedding: make([]float64, 1024),
		},
		Events: []ml.EventInput{
			{ID: "evt_1", Embedding: make([]float64, 1024)},
			{ID: "evt_2", Embedding: make([]float64, 1024)},
		},
		Options: ml.RankingOptions{
			Rerank: true,
		},
	}
	req.User.PositiveEmbedding[0] = 1.0

	res, err := client.RankEvents(context.Background(), &req)
	if err != nil {
		t.Fatalf("RankEvents failed: %v", err)
	}

	if len(res.Events) != 2 {
		t.Fatalf("expected 2 events in response, got %d", len(res.Events))
	}
	if res.Events[0].EventID != "evt_2" || res.Events[0].Score != 0.92 {
		t.Errorf("unexpected top event: %+v", res.Events[0])
	}
	if !res.Reranked {
		t.Errorf("expected reranked=true")
	}
}

func TestUpdateUserEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compatibility/user-embedding/update" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req ml.UpdateUserEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.Kind != "positive" {
			t.Errorf("expected kind positive, got %s", req.Kind)
		}

		updatedVec := make([]float64, len(req.Embedding))
		for i := range updatedVec {
			updatedVec[i] = 0.5
		}

		resp := ml.UpdateUserEmbeddingResponse{
			Embedding: updatedVec,
			Kind:      req.Kind,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := ml.NewClient(server.URL)
	req := ml.UpdateUserEmbeddingRequest{
		Embedding:      make([]float64, 1024),
		Kind:           "positive",
		EventEmbedding: make([]float64, 1024),
	}

	res, err := client.UpdateUserEmbedding(context.Background(), &req)
	if err != nil {
		t.Fatalf("UpdateUserEmbedding failed: %v", err)
	}

	if res.Kind != "positive" {
		t.Errorf("expected kind positive, got %s", res.Kind)
	}
	if len(res.Embedding) != 1024 {
		t.Errorf("expected embedding length 1024, got %d", len(res.Embedding))
	}
}

func TestRankActivitiesFallback(t *testing.T) {
	// Client pointing to an unreachable port
	client := ml.NewClient("http://127.0.0.1:59999")

	id1 := bson.NewObjectID()
	id2 := bson.NewObjectID()
	activities := []models.Activity{
		{ID: id1, Name: "Activity 1", Category: "art"},
		{ID: id2, Name: "Activity 2", Category: "food"},
	}

	ranked := client.RankActivities(context.Background(), nil, activities, ml.RankingOptions{Rerank: true}, "weekend fun", nil)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 activities returned on fallback, got %d", len(ranked))
	}
	if ranked[0].ID != id1 || ranked[1].ID != id2 {
		t.Errorf("expected original ordering on fallback")
	}
}
