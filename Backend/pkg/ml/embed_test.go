package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// embedHandler answers /v1/embed with basis vectors, one per text, and the given provider.
func embedHandler(t *testing.T, provider string, cached int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
			Kind  string   `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding /v1/embed request: %v", err)
		}
		vectors := make([][]float64, len(req.Texts))
		for i := range vectors {
			vectors[i] = basis(ml.Dim, i)
		}
		writeJSON(w, http.StatusOK, map[string]any{"embeddings": vectors, "model": ml.Model, "dim": ml.Dim, "provider": provider, "cached": cached})
	}
}

func TestEmbed(t *testing.T) {
	f := newFakeML(t)
	f.on("/v1/embed", embedHandler(t, "hf:deepinfra", 1))
	got, err := f.client().Embed(context.Background(), []string{"Interests:\n- jazz", "Cost:\n- free"}, ml.EmbedUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Vectors) != 2 || got.Vectors[1][1] != 1 || got.Model != ml.Model || got.Dim != ml.Dim || got.Provider != "hf:deepinfra" || got.Cached != 1 {
		t.Fatalf("embeddings: %+v", got)
	}
	body := f.body("/v1/embed", -1)
	if body["kind"] != "user" || len(body["texts"].([]any)) != 2 {
		t.Fatalf("request: %v", body)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "vectors") || len(raw) > 200 {
		t.Fatalf("vectors must not serialize: %s", raw)
	}
}

func TestEmbedSplitsBatchesOf64(t *testing.T) {
	f := newFakeML(t)
	calls := 0
	f.on("/v1/embed", func(w http.ResponseWriter, r *http.Request) {
		calls++
		provider := "local"
		if calls > 1 {
			provider = "cache"
		}
		embedHandler(t, provider, 2)(w, r)
	})
	texts := make([]string, 70)
	for i := range texts {
		texts[i] = "text"
	}
	got, err := f.client().Embed(context.Background(), texts, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.count("/v1/embed") != 2 || len(f.body("/v1/embed", 0)["texts"].([]any)) != 64 || len(f.body("/v1/embed", 1)["texts"].([]any)) != 6 {
		t.Fatalf("batches: %d requests", f.count("/v1/embed"))
	}
	if f.body("/v1/embed", 0)["kind"] != "activity" {
		t.Fatal("the kind defaults to activity")
	}
	if len(got.Vectors) != 70 || got.Cached != 4 || got.Provider != "local+cache" {
		t.Fatalf("merged: %d vectors, cached %d, provider %q", len(got.Vectors), got.Cached, got.Provider)
	}
}

func TestEmbedRejectsMalformedAnswers(t *testing.T) {
	cases := map[string]map[string]any{
		"count":  {"embeddings": [][]float64{basis(ml.Dim, 0)}, "model": ml.Model, "dim": ml.Dim},
		"dim":    {"embeddings": [][]float64{basis(8, 0), basis(8, 1)}, "model": ml.Model, "dim": 8},
		"length": {"embeddings": [][]float64{basis(ml.Dim, 0), basis(8, 1)}, "model": ml.Model, "dim": ml.Dim},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeML(t)
			f.reply("/v1/embed", http.StatusOK, payload)
			if _, err := f.client().Embed(context.Background(), []string{"a", "b"}, ml.EmbedActivity); !errors.Is(err, ml.ErrBadResponse) {
				t.Fatalf("err = %v, want ErrBadResponse", err)
			}
		})
	}
}

func TestEmbedNeedsTexts(t *testing.T) {
	f := newFakeML(t)
	if _, err := f.client().Embed(context.Background(), nil, ml.EmbedActivity); !errors.Is(err, ml.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
	if f.count("/v1/embed") != 0 {
		t.Fatal("no request expected")
	}
}

func TestEmbedClipsLongTexts(t *testing.T) {
	f := newFakeML(t)
	f.on("/v1/embed", embedHandler(t, "local", 0))
	if _, err := f.client().Embed(context.Background(), []string{strings.Repeat("é", 9000)}, ml.EmbedActivity); err != nil {
		t.Fatal(err)
	}
	sent := f.body("/v1/embed", -1)["texts"].([]any)[0].(string)
	if n := utf8.RuneCountInString(sent); n != 8000 {
		t.Fatalf("sent %d characters", n)
	}
}

func TestTextHashMatchesTheStoredActivityHash(t *testing.T) {
	// An activity from freetime.activities: its embeddingText and embeddingTextHash as stored.
	text := "Interests:\n- disney\n- theater\n- ice skating\n\nActivities:\n- live performance\n- spectator sports\n\n" +
		"Social:\n- family friendly\n- large group\n\nEnvironment:\n- indoor\n- arena\n- loud atmosphere\n- high energy\n\n" +
		"Pace:\n- high energy\n\nTiming:\n- morning\n\nExperience:\n- ice show\n- character performance"
	if got := ml.TextHash(text); got != "b0060f0fe111bfe2ed5e03b2bccf3091fe48381f" {
		t.Fatalf("TextHash = %s", got)
	}
}

func TestNewActivityEmbeddingMeta(t *testing.T) {
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	meta := ml.NewActivityEmbeddingMeta(&ml.Embeddings{Model: ml.Model, Dim: ml.Dim, Provider: "local"}, "abc123", ml.SourceOnDemand, now)
	want := ml.ActivityEmbeddingMeta{
		Model: ml.Model, Dimension: ml.Dim, Normalized: true, Prompt: nil, MaxSeqLength: 512,
		GeneratedAt: now.UTC(), TextHash: "abc123", Source: "go_on_demand", Provider: "local",
	}
	if meta != want {
		t.Fatalf("meta = %+v", meta)
	}
}
