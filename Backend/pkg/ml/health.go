package ml

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Health is the service's /healthz report, reduced to what the backend decides with.
type Health struct {
	Status              string // "ok", or "degraded" when no embedding provider can serve
	Jev                 bool   // the Jev rerank is configured on the service
	RankingModelVersion string
	EmbeddingModel      string // "" when the service runs without an embedder
	EmbeddingDim        int
	EmbeddingMode       string // EMBED_PROVIDER: auto | vertex | hf | local
	EmbeddingProvider   string // the provider that served last ("none" before the first call)
	Providers           map[string]ProviderStatus
	TemplateVersion     string // profile-text template: the prefix of every profile_text_hash
	UptimeSeconds       float64
	FetchedAt           time.Time
}

// ProviderStatus is one embedding provider (or HF route) as /healthz reports it.
type ProviderStatus struct {
	Status    string   `json:"status"` // ok | error | unknown | not_loaded | loading | loaded | disabled
	Detail    string   `json:"detail"`
	LatencyMS *float64 `json:"latency_ms"`
}

// CanEmbed reports whether some embedding provider can serve.
func (h *Health) CanEmbed() bool {
	return h != nil && h.Status == "ok" && h.EmbeddingModel != ""
}

type healthResponse struct {
	Status        string  `json:"status"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	Ranking       struct {
		ModelVersion string `json:"model_version"`
	} `json:"ranking"`
	Embedding struct {
		Model     string                    `json:"model"`
		Dim       int                       `json:"dim"`
		Mode      string                    `json:"mode"`
		Provider  string                    `json:"provider"`
		Providers map[string]ProviderStatus `json:"providers"`
	} `json:"embedding"`
	TemplateVersion string `json:"profile_template_version"`
	Jev             bool   `json:"jev"`
}

func (r healthResponse) health(at time.Time) Health {
	return Health{
		Status:              r.Status,
		Jev:                 r.Jev,
		RankingModelVersion: r.Ranking.ModelVersion,
		EmbeddingModel:      r.Embedding.Model,
		EmbeddingDim:        r.Embedding.Dim,
		EmbeddingMode:       r.Embedding.Mode,
		EmbeddingProvider:   r.Embedding.Provider,
		Providers:           r.Embedding.Providers,
		TemplateVersion:     r.TemplateVersion,
		UptimeSeconds:       r.UptimeSeconds,
		FetchedAt:           at,
	}
}

// Health returns the service's report, reusing one younger than HealthTTL (60 s). A failed fetch
// is remembered for 10 s so a down service is not asked on every request. Deadline: HealthTimeout
// unless ctx has one.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	now := c.opts.Now()
	c.healthMu.Lock()
	if c.health != nil && now.Sub(c.healthAt) < c.opts.HealthTTL {
		h := *c.health
		c.healthMu.Unlock()
		return &h, nil
	}
	if c.healthErr != nil && now.Sub(c.healthErrAt) < healthErrorTTL {
		err := c.healthErr
		c.healthMu.Unlock()
		return nil, err
	}
	c.healthMu.Unlock()
	return c.fetchHealth(ctx)
}

func (c *Client) fetchHealth(ctx context.Context) (*Health, error) {
	var resp healthResponse
	err := c.do(ctx, http.MethodGet, "/healthz", c.opts.HealthTimeout, nil, &resp)
	now := c.opts.Now()
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if err != nil {
		if ctx.Err() == nil { // a caller giving up says nothing about the service
			c.healthErr, c.healthErrAt = err, now
		}
		return nil, err
	}
	h := resp.health(now)
	c.health, c.healthAt, c.healthErr = &h, now, nil
	out := h
	return &out, nil
}

// CachedHealth returns the last report without any network call (nil before the first fetch).
func (c *Client) CachedHealth() *Health {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if c.health == nil {
		return nil
	}
	h := *c.health
	return &h
}

// JevAvailable reports, without blocking, whether a rerank can run: ML_RERANK allows it and the
// last /healthz said jev: true. A missing or stale report is refreshed in the background, so the
// first calls after startup answer false.
func (c *Client) JevAvailable() bool {
	if !c.opts.Rerank {
		return false
	}
	c.healthMu.Lock()
	h, at := c.health, c.healthAt
	c.healthMu.Unlock()
	if h == nil || c.opts.Now().Sub(at) >= c.opts.HealthTTL {
		c.refreshHealthInBackground()
	}
	return h != nil && h.Jev
}

func (c *Client) refreshHealthInBackground() {
	if !c.refreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.refreshing.Store(false)
		if _, err := c.Health(context.Background()); err != nil {
			log.Debug().Err(err).Msg("ml health refresh failed")
		}
	}()
}
