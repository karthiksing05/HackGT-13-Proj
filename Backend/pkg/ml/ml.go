// Package ml is the backend's client for the ML service (`ml/` in this repo, FastAPI on
// 127.0.0.1:8000): embeddings, user-profile and search-profile texts, event ranking and
// user-vector updates.
//
// The service owns the text format and the encoder, so Go never builds an embedding text and
// never fabricates a vector: every vector here came from the service or from Mongo, and a failure
// is an error, never a stand-in. Every call carries a deadline: the caller's context deadline when
// it has one, otherwise the per-call default from Options. Result types tag vectors `json:"-"` so
// they cannot leak into app responses; the request types sent to the service keep their wire
// names.
//
// The package imports no other Backend package: callers adapt their models to the small structs
// defined here.
package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Model is the encoder every stored vector comes from; Dim is its dimension.
	Model = "Qwen/Qwen3-Embedding-0.6B"
	Dim   = 1024

	DefaultBaseURL       = "http://127.0.0.1:8000"
	DefaultRerankTopK    = 12
	DefaultRankTimeout   = 2 * time.Second
	DefaultRerankTimeout = 20 * time.Second
	DefaultEmbedTimeout  = 15 * time.Second
	DefaultSearchTimeout = 5 * time.Second
	DefaultUpdateTimeout = 5 * time.Second
	DefaultHealthTimeout = 2 * time.Second
	DefaultHealthTTL     = 60 * time.Second

	httpBackstop     = 90 * time.Second // http.Client timeout; per-call deadlines are always shorter
	healthErrorTTL   = 10 * time.Second // how long a failed /healthz is remembered
	maxResponseBytes = 64 << 20
	maxEmbedBatch    = 64   // the service's limit per /v1/embed request
	maxTextChars     = 8000 // the service's limit per text
)

var (
	// ErrUnavailable: the service could not be reached, timed out, or answered 503 (no embedding
	// provider works).
	ErrUnavailable = errors.New("ml: service unavailable")
	// ErrBadResponse: a 2xx answer that cannot be used (wrong dimension or count, bad JSON).
	ErrBadResponse = errors.New("ml: malformed response")
	// ErrNoUserEmbedding: a rank request without a usable positive vector (the service rejects
	// all-zero ones); rank by another signal instead.
	ErrNoUserEmbedding = errors.New("ml: no usable user embedding")
	// ErrInvalidRequest: the request cannot be sent as is.
	ErrInvalidRequest = errors.New("ml: invalid request")
)

// StatusError is a non-2xx answer from the service. A 503 unwraps to ErrUnavailable.
type StatusError struct {
	Method, Path string
	Status       int
	Detail       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ml: %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Detail)
}

func (e *StatusError) Unwrap() error {
	if e.Status == http.StatusServiceUnavailable {
		return ErrUnavailable
	}
	return nil
}

// Options configure a Client. Start from DefaultOptions or OptionsFromEnv and override fields;
// NewClientWithOptions fills zero durations and counts with the defaults.
type Options struct {
	BaseURL string
	// Rerank allows the Jev rerank (ML_RERANK, default true). It runs only when /healthz also
	// reports jev: true.
	Rerank     bool
	RerankTopK int // ML_RERANK_TOP_K (12)

	// Default deadlines, used when the caller's context has none; a caller's deadline always wins.
	RankTimeout   time.Duration // ML_RANK_TIMEOUT_MS (2000): classifier-only ranking
	RerankTimeout time.Duration // ML_RANK_RERANK_TIMEOUT_MS (20000): ranking with the Jev rerank
	EmbedTimeout  time.Duration // ML_EMBED_TIMEOUT_MS (15000): /v1/embed and /v1/user-profile
	SearchTimeout time.Duration // ML_SEARCH_TIMEOUT_MS (5000): /v1/search-profile
	UpdateTimeout time.Duration // user-vector updates (5 s)
	HealthTimeout time.Duration // /healthz (2 s)
	HealthTTL     time.Duration // how long Health reuses a report (60 s)

	Dim        int          // expected vector dimension (1024)
	HTTPClient *http.Client // default: a client with a 90 s backstop timeout
	Now        func() time.Time
}

// DefaultOptions are the documented defaults.
func DefaultOptions() Options {
	return Options{
		BaseURL:       DefaultBaseURL,
		Rerank:        true,
		RerankTopK:    DefaultRerankTopK,
		RankTimeout:   DefaultRankTimeout,
		RerankTimeout: DefaultRerankTimeout,
		EmbedTimeout:  DefaultEmbedTimeout,
		SearchTimeout: DefaultSearchTimeout,
		UpdateTimeout: DefaultUpdateTimeout,
		HealthTimeout: DefaultHealthTimeout,
		HealthTTL:     DefaultHealthTTL,
		Dim:           Dim,
	}
}

// OptionsFromEnv is DefaultOptions overridden by ML_SERVICE_URL (or the older ML_API_URL),
// ML_RERANK, ML_RERANK_TOP_K, ML_RANK_TIMEOUT_MS, ML_RANK_RERANK_TIMEOUT_MS, ML_EMBED_TIMEOUT_MS
// and ML_SEARCH_TIMEOUT_MS. Malformed values keep the default.
func OptionsFromEnv() Options {
	o := DefaultOptions()
	if v := firstEnv("ML_SERVICE_URL", "ML_API_URL"); v != "" {
		o.BaseURL = v
	}
	o.Rerank = envBool("ML_RERANK", o.Rerank)
	o.RerankTopK = envInt("ML_RERANK_TOP_K", o.RerankTopK)
	o.RankTimeout = envMillis("ML_RANK_TIMEOUT_MS", o.RankTimeout)
	o.RerankTimeout = envMillis("ML_RANK_RERANK_TIMEOUT_MS", o.RerankTimeout)
	o.EmbedTimeout = envMillis("ML_EMBED_TIMEOUT_MS", o.EmbedTimeout)
	o.SearchTimeout = envMillis("ML_SEARCH_TIMEOUT_MS", o.SearchTimeout)
	return o
}

// Client talks to the ML service. It is safe for concurrent use.
type Client struct {
	baseURL string
	opts    Options
	http    *http.Client

	healthMu    sync.Mutex
	health      *Health
	healthAt    time.Time
	healthErr   error
	healthErrAt time.Time
	refreshing  atomic.Bool
}

// NewClient is a client for baseURL (ML_SERVICE_URL, else the default, when empty) with the other
// options from the environment.
func NewClient(baseURL string) *Client {
	o := OptionsFromEnv()
	if strings.TrimSpace(baseURL) != "" {
		o.BaseURL = baseURL
	}
	return NewClientWithOptions(o)
}

// NewClientWithOptions is a client with explicit options; zero fields take their defaults.
func NewClientWithOptions(o Options) *Client {
	d := DefaultOptions()
	if strings.TrimSpace(o.BaseURL) == "" {
		o.BaseURL = d.BaseURL
	}
	if o.RerankTopK <= 0 {
		o.RerankTopK = d.RerankTopK
	}
	for _, f := range []struct {
		value *time.Duration
		def   time.Duration
	}{
		{&o.RankTimeout, d.RankTimeout},
		{&o.RerankTimeout, d.RerankTimeout},
		{&o.EmbedTimeout, d.EmbedTimeout},
		{&o.SearchTimeout, d.SearchTimeout},
		{&o.UpdateTimeout, d.UpdateTimeout},
		{&o.HealthTimeout, d.HealthTimeout},
		{&o.HealthTTL, d.HealthTTL},
	} {
		if *f.value <= 0 {
			*f.value = f.def
		}
	}
	if o.Dim <= 0 {
		o.Dim = d.Dim
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: httpBackstop}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Client{baseURL: strings.TrimRight(strings.TrimSpace(o.BaseURL), "/"), opts: o, http: o.HTTPClient}
}

// BaseURL is the service address this client calls.
func (c *Client) BaseURL() string { return c.baseURL }

// withDeadline applies the default deadline d unless the caller's context already has one.
func withDeadline(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// do sends one JSON request and decodes a 2xx answer into out.
func (c *Client) do(ctx context.Context, method, path string, timeout time.Duration, in, out any) error {
	ctx, cancel := withDeadline(ctx, timeout)
	defer cancel()

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%w: encoding %s: %v", ErrInvalidRequest, path, err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %v", ErrInvalidRequest, method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ml: %s %s: %w: %w", method, path, ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("ml: %s %s: reading the response: %w: %w", method, path, ErrUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Method: method, Path: path, Status: resp.StatusCode, Detail: errorDetail(data)}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%w: %s %s: %v", ErrBadResponse, method, path, err)
		}
	}
	return nil
}

// errorDetail is the service's `detail` (a sentence, or the validation errors), else the body.
func errorDetail(body []byte) string {
	var payload struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &payload) == nil && len(payload.Detail) > 0 {
		var sentence string
		if json.Unmarshal(payload.Detail, &sentence) == nil {
			return sentence
		}
		return truncate(string(payload.Detail), 300)
	}
	return truncate(strings.TrimSpace(string(body)), 300)
}

// Vec is a vector on the wire to the service. MarshalJSON writes every value rounded to 5
// decimals, always with a decimal point: rank requests shrink about 2.5x and cosines move by less
// than 1e-5. Non-finite values are an error, as they are for encoding/json.
type Vec []float64

// MarshalJSON implements json.Marshaler.
func (v Vec) MarshalJSON() ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	buf := make([]byte, 0, 2+len(v)*9)
	buf = append(buf, '[')
	for i, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("ml: vector value %d is not finite", i)
		}
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendRounded(buf, x)
	}
	return append(buf, ']'), nil
}

func appendRounded(buf []byte, x float64) []byte {
	if math.Abs(x) < 1e9 {
		x = math.Round(x*1e5) / 1e5
	}
	if x == 0 { // also drops the sign of -0
		return append(buf, "0.0"...)
	}
	start := len(buf)
	buf = strconv.AppendFloat(buf, x, 'f', -1, 64)
	if !bytes.ContainsRune(buf[start:], '.') {
		buf = append(buf, ".0"...)
	}
	return buf
}

// Usable reports whether v can be ranked with: exactly dim finite values, not all zero.
func Usable(v []float64, dim int) bool {
	if len(v) != dim {
		return false
	}
	nonZero := false
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
		if x != 0 {
			nonZero = true
		}
	}
	return nonZero
}

// IsZero reports whether v carries no signal: empty, or every value zero.
func IsZero(v []float64) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// checkVector validates a vector the service returned.
func checkVector(v []float64, dim int, what string) error {
	if len(v) != dim {
		return fmt.Errorf("%w: %s has %d values, want %d", ErrBadResponse, what, len(v), dim)
	}
	for i, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("%w: %s value %d is not finite", ErrBadResponse, what, i)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func envBool(name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(name string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n > 0 {
		return n
	}
	return def
}

func envMillis(name string, def time.Duration) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n > 0 {
		return time.Duration(n) * time.Millisecond
	}
	return def
}
