package ml

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// EmbedKind says what a text is, for the service's logs and metrics. It never changes the text:
// no instruction prefix is added for any kind.
type EmbedKind string

const (
	EmbedUser     EmbedKind = "user"
	EmbedSearch   EmbedKind = "search"
	EmbedActivity EmbedKind = "activity"
)

// Embeddings are the vectors for Embed's texts, in order: Dim values each, unit norm, all zeros
// for a blank text (never store those as an activity's vector).
type Embeddings struct {
	Vectors  [][]float64 `json:"-"`
	Model    string      `json:"model"`
	Dim      int         `json:"dim"`
	Provider string      `json:"provider"` // who embedded the uncached texts; "cache" when none needed it
	Cached   int         `json:"cached"`
}

type embedRequest struct {
	Texts []string  `json:"texts"`
	Kind  EmbedKind `json:"kind"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Model      string      `json:"model"`
	Dim        int         `json:"dim"`
	Provider   string      `json:"provider"`
	Cached     int         `json:"cached"`
}

// Embed embeds raw texts with POST /v1/embed, 64 per request. Use it for activity texts (e.g. a
// rated activity that has no vector yet); user and search texts come from UserProfile and
// SearchProfile, which also build the text. Texts over 8000 characters are cut, as the service
// accepts no more. Deadline: EmbedTimeout per request unless ctx has one.
func (c *Client) Embed(ctx context.Context, texts []string, kind EmbedKind) (*Embeddings, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: no texts to embed", ErrInvalidRequest)
	}
	if kind == "" {
		kind = EmbedActivity
	}
	out := &Embeddings{Vectors: make([][]float64, 0, len(texts))}
	var providers []string
	for start := 0; start < len(texts); start += maxEmbedBatch {
		batch := texts[start:min(start+maxEmbedBatch, len(texts))]
		clipped := make([]string, len(batch))
		for i, text := range batch {
			clipped[i] = clipText(text, maxTextChars)
		}
		var resp embedResponse
		if err := c.do(ctx, http.MethodPost, "/v1/embed", c.opts.EmbedTimeout, embedRequest{Texts: clipped, Kind: kind}, &resp); err != nil {
			return nil, err
		}
		if len(resp.Embeddings) != len(batch) {
			return nil, fmt.Errorf("%w: /v1/embed returned %d vectors for %d texts", ErrBadResponse, len(resp.Embeddings), len(batch))
		}
		if resp.Dim != c.opts.Dim {
			return nil, fmt.Errorf("%w: /v1/embed reports dim %d, want %d", ErrBadResponse, resp.Dim, c.opts.Dim)
		}
		for i, v := range resp.Embeddings {
			if err := checkVector(v, c.opts.Dim, fmt.Sprintf("embedding %d", start+i)); err != nil {
				return nil, err
			}
		}
		out.Vectors = append(out.Vectors, resp.Embeddings...)
		out.Model, out.Dim = resp.Model, resp.Dim
		out.Cached += resp.Cached
		if !containsString(providers, resp.Provider) {
			providers = append(providers, resp.Provider)
		}
	}
	out.Provider = strings.Join(providers, "+")
	return out, nil
}

// TextHash is the hex sha1 of a text: an activity's `embeddingTextHash`, and the `textHash` that
// records which text a stored vector was computed from.
func TextHash(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// SourceOnDemand marks activity vectors the backend embedded itself (e.g. a rated activity that
// had none); tools/embed_missing.py writes "embed_missing" and the Raven backfill "backfill".
const SourceOnDemand = "go_on_demand"

// ActivityEmbeddingMeta is the `embeddingMeta` sub-document stored next to an activity's
// `embedding` and `embeddingModel`, in the shape every writer uses (datagen/mongo_backfill.py,
// tools/embed_missing.py and this package), so staleness checks work the same for all of them.
// Write it only with a filter on `embeddingTextHash` equal to TextHash, so a text that changed
// meanwhile never gets the old text's vector.
type ActivityEmbeddingMeta struct {
	Model        string    `bson:"model"`
	Dimension    int       `bson:"dimension"`
	Normalized   bool      `bson:"normalized"`
	Prompt       *string   `bson:"prompt"` // always null: no instruction prefix
	MaxSeqLength int       `bson:"maxSeqLength"`
	GeneratedAt  time.Time `bson:"generatedAt"`
	TextHash     string    `bson:"textHash"`
	Source       string    `bson:"source"`
	Provider     string    `bson:"provider,omitempty"`
}

// NewActivityEmbeddingMeta describes a vector Embed returned for the text whose hash is textHash.
func NewActivityEmbeddingMeta(e *Embeddings, textHash, source string, now time.Time) ActivityEmbeddingMeta {
	return ActivityEmbeddingMeta{
		Model:        e.Model,
		Dimension:    e.Dim,
		Normalized:   true,
		MaxSeqLength: 512,
		GeneratedAt:  now.UTC(),
		TextHash:     textHash,
		Source:       source,
		Provider:     e.Provider,
	}
}

func clipText(s string, maxChars int) string {
	if len(s) <= maxChars { // bytes >= characters, so this is the common fast path
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars])
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
