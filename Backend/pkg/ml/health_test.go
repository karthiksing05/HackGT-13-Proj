package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// clock is a settable time source for the health cache.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newClock() *clock { return &clock{now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)} }

func TestHealthIsCachedForTheTTL(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(true))
	clk := newClock()
	c := f.client(func(o *ml.Options) { o.Now = clk.Now })

	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || !h.Jev || h.EmbeddingModel != ml.Model || h.EmbeddingDim != ml.Dim || h.TemplateVersion != "profile-v1" ||
		h.RankingModelVersion != "classifier-v1" || h.EmbeddingMode != "auto" || h.EmbeddingProvider != "local" || !h.CanEmbed() ||
		h.Providers["hf:deepinfra"].Status != "error" || h.Providers["local"].LatencyMS == nil || *h.Providers["local"].LatencyMS != 41.5 {
		t.Fatalf("health: %+v", h)
	}
	clk.Advance(59 * time.Second)
	if _, err := c.Health(context.Background()); err != nil || f.count("/healthz") != 1 {
		t.Fatalf("a report younger than the TTL is reused (%d calls, %v)", f.count("/healthz"), err)
	}
	clk.Advance(2 * time.Second)
	if _, err := c.Health(context.Background()); err != nil || f.count("/healthz") != 2 {
		t.Fatalf("a stale report is refetched (%d calls)", f.count("/healthz"))
	}
}

func TestHealthRemembersFailuresBriefly(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusInternalServerError, map[string]any{"detail": "boom"})
	clk := newClock()
	c := f.client(func(o *ml.Options) { o.Now = clk.Now })
	for i := 0; i < 3; i++ {
		if _, err := c.Health(context.Background()); err == nil {
			t.Fatal("expected an error")
		}
	}
	if f.count("/healthz") != 1 {
		t.Fatalf("a failure is remembered (%d calls)", f.count("/healthz"))
	}
	clk.Advance(11 * time.Second)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	if h, err := c.Health(context.Background()); err != nil || h.Jev || f.count("/healthz") != 2 {
		t.Fatalf("retried after 10 s: %v", err)
	}
}

func TestHealthIgnoresACallerThatGaveUp(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(true))
	c := f.client()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Health(ctx); err == nil {
		t.Fatal("a cancelled context fails the call")
	}
	if h, err := c.Health(context.Background()); err != nil || !h.Jev {
		t.Fatalf("the cancellation must not be remembered: %v", err)
	}
}

func TestCachedHealthNeverCallsTheService(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(true))
	c := f.client()
	if c.CachedHealth() != nil || f.count("/healthz") != 0 {
		t.Fatal("nothing cached yet, and no call")
	}
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h := c.CachedHealth(); h == nil || !h.Jev {
		t.Fatalf("cached: %+v", h)
	}
}

func TestJevAvailable(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(true))

	off := f.client(func(o *ml.Options) { o.Rerank = false })
	if off.JevAvailable() {
		t.Fatal("ML_RERANK=false disables Jev")
	}
	time.Sleep(20 * time.Millisecond)
	if f.count("/healthz") != 0 {
		t.Fatal("no health call when Jev is disabled")
	}

	on := f.client()
	if on.JevAvailable() {
		t.Fatal("nothing is known before the first report")
	}
	waitFor(t, "the background health refresh", func() bool { return on.CachedHealth() != nil })
	if !on.JevAvailable() {
		t.Fatal("jev: true after the refresh")
	}
}

func TestCanEmbed(t *testing.T) {
	if (*ml.Health)(nil).CanEmbed() || (&ml.Health{Status: "degraded", EmbeddingModel: ml.Model}).CanEmbed() || (&ml.Health{Status: "ok"}).CanEmbed() {
		t.Fatal("only an ok report with a model can embed")
	}
}
