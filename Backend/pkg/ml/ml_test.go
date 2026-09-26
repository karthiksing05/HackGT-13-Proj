package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVecMarshalRoundsToFiveDecimals(t *testing.T) {
	got, err := json.Marshal(ml.Vec{0.123456789, -0.000001, 1, -0.5, 2.718281828, 12, -0.0})
	if err != nil {
		t.Fatal(err)
	}
	if want := `[0.12346,0.0,1.0,-0.5,2.71828,12.0,0.0]`; string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	var back []float64
	if err := json.Unmarshal(got, &back); err != nil || back[0] != 0.12346 {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

func TestVecNilAndOmitEmpty(t *testing.T) {
	got, err := json.Marshal(struct {
		A ml.Vec `json:"a"`
		B ml.Vec `json:"b,omitempty"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":null}` {
		t.Fatalf("got %s", got)
	}
}

func TestVecRejectsNonFiniteValues(t *testing.T) {
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := json.Marshal(ml.Vec{0.1, x}); err == nil {
			t.Errorf("%v: expected an error", x)
		}
	}
}

func TestUsableAndIsZero(t *testing.T) {
	cases := []struct {
		v      []float64
		usable bool
		zero   bool
	}{
		{basis(4, 1), true, false},
		{make([]float64, 4), false, true},
		{nil, false, true},
		{basis(3, 0), false, false},
		{[]float64{1, math.NaN(), 0, 0}, false, false},
	}
	for i, c := range cases {
		if got := ml.Usable(c.v, 4); got != c.usable {
			t.Errorf("case %d: Usable = %v", i, got)
		}
		if got := ml.IsZero(c.v); got != c.zero {
			t.Errorf("case %d: IsZero = %v", i, got)
		}
	}
}

func TestOptionsFromEnv(t *testing.T) {
	t.Setenv("ML_SERVICE_URL", "http://ml.internal:9000/")
	t.Setenv("ML_RERANK", "false")
	t.Setenv("ML_RERANK_TOP_K", "7")
	t.Setenv("ML_RANK_TIMEOUT_MS", "1500")
	t.Setenv("ML_RANK_RERANK_TIMEOUT_MS", "bogus")
	t.Setenv("ML_EMBED_TIMEOUT_MS", "250")
	t.Setenv("ML_SEARCH_TIMEOUT_MS", "-3")
	o := ml.OptionsFromEnv()
	if o.Rerank || o.RerankTopK != 7 || o.RankTimeout != 1500*time.Millisecond || o.EmbedTimeout != 250*time.Millisecond {
		t.Fatalf("parsed options: %+v", o)
	}
	if o.RerankTimeout != ml.DefaultRerankTimeout || o.SearchTimeout != ml.DefaultSearchTimeout {
		t.Fatalf("malformed values must keep the defaults: %+v", o)
	}
	if got := ml.NewClient("").BaseURL(); got != "http://ml.internal:9000" {
		t.Fatalf("BaseURL = %q", got)
	}
	if got := ml.NewClient(" http://explicit:2/ ").BaseURL(); got != "http://explicit:2" {
		t.Fatalf("an explicit URL must win: %q", got)
	}
}

func TestOptionsFromEnvDefaultsAndLegacyURL(t *testing.T) {
	t.Setenv("ML_SERVICE_URL", "")
	t.Setenv("ML_API_URL", "http://legacy:8001")
	t.Setenv("ML_RERANK", "")
	o := ml.OptionsFromEnv()
	if o.BaseURL != "http://legacy:8001" || !o.Rerank || o.RerankTopK != ml.DefaultRerankTopK || o.Dim != ml.Dim {
		t.Fatalf("options: %+v", o)
	}
	t.Setenv("ML_API_URL", "")
	if got := ml.NewClient("").BaseURL(); got != ml.DefaultBaseURL {
		t.Fatalf("BaseURL = %q", got)
	}
}

func TestServiceUnavailableIs503(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/embed", http.StatusServiceUnavailable, map[string]any{"detail": "Embedding provider unavailable."})
	_, err := f.client().Embed(context.Background(), []string{"x"}, ml.EmbedActivity)
	if !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	var status *ml.StatusError
	if !errors.As(err, &status) || status.Status != 503 || status.Detail != "Embedding provider unavailable." {
		t.Fatalf("status error: %#v", status)
	}
}

func TestValidationErrorIsAStatusError(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/embed", http.StatusUnprocessableEntity, map[string]any{"detail": []any{map[string]any{"loc": []any{"body", "texts"}, "msg": "too long"}}})
	_, err := f.client().Embed(context.Background(), []string{"x"}, ml.EmbedActivity)
	var status *ml.StatusError
	if !errors.As(err, &status) || status.Status != 422 || !strings.Contains(status.Detail, "too long") {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, ml.ErrUnavailable) {
		t.Fatal("a 422 is not an outage")
	}
}

func TestUnreachableServiceIsUnavailable(t *testing.T) {
	c := ml.NewClientWithOptions(ml.Options{BaseURL: "http://127.0.0.1:1"})
	if _, err := c.Embed(context.Background(), []string{"x"}, ml.EmbedActivity); !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func rankRequest() *ml.RankEventsRequest {
	return &ml.RankEventsRequest{
		User:   ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)},
		Events: []ml.EventInput{{ID: "a", Embedding: basis(ml.Dim, 1)}},
	}
}

var rankedA = map[string]any{"events": []any{map[string]any{"event_id": "a", "score": 0.7, "rerank_score": nil}}, "model_version": "classifier-v1", "reranked": false}

func TestDefaultDeadlineApplies(t *testing.T) {
	f := newFakeML(t)
	f.on("/v1/events/rank", slow(2*time.Second, rankedA))
	c := f.client(func(o *ml.Options) { o.RankTimeout = 50 * time.Millisecond })
	started := time.Now()
	_, err := c.RankEvents(context.Background(), rankRequest())
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the default deadline was not applied: %v", elapsed)
	}
}

func TestCallerDeadlineWins(t *testing.T) {
	f := newFakeML(t)
	f.on("/v1/events/rank", slow(150*time.Millisecond, rankedA))
	c := f.client(func(o *ml.Options) { o.RankTimeout = 50 * time.Millisecond })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.RankEvents(ctx, rankRequest())
	if err != nil || len(resp.Events) != 1 {
		t.Fatalf("a longer caller deadline must win: %v", err)
	}
}

func TestRerankGetsTheLongerDeadlineAndTopK(t *testing.T) {
	f := newFakeML(t)
	f.on("/v1/events/rank", slow(150*time.Millisecond, rankedA))
	c := f.client(func(o *ml.Options) {
		o.RankTimeout = 50 * time.Millisecond
		o.RerankTimeout = 5 * time.Second
	})
	req := rankRequest()
	req.Options.Rerank = true
	if _, err := c.RankEvents(context.Background(), req); err != nil {
		t.Fatalf("rerank call: %v", err)
	}
	options := f.body("/v1/events/rank", -1)["options"].(map[string]any)
	if options["rerank"] != true || options["rerank_top_k"] != float64(ml.DefaultRerankTopK) {
		t.Fatalf("options = %v", options)
	}
	req.Options.Rerank = false
	if _, err := c.RankEvents(context.Background(), req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("classifier-only call: err = %v, want the short deadline", err)
	}
}
