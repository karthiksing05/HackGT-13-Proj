package ml

import (
	"Backend/pkg/env"
	"Backend/pkg/models"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// UserInput models user constraints and preference vectors for POST /v1/events/rank
type UserInput struct {
	PositiveEmbedding  []float64 `json:"positive_embedding"`
	NegativeEmbedding  []float64 `json:"negative_embedding"`
	PositiveText       *string   `json:"positive_text,omitempty"`
	NegativeText       *string   `json:"negative_text,omitempty"`
	MaxPrice           *float64  `json:"max_price,omitempty"`
	Latitude           *float64  `json:"latitude,omitempty"`
	Longitude          *float64  `json:"longitude,omitempty"`
	MaxDistanceMiles   *float64  `json:"max_distance_miles,omitempty"`
	AvailableStart     *string   `json:"available_start,omitempty"` // RFC 3339 / ISO 8601
	AvailableEnd       *string   `json:"available_end,omitempty"`   // RFC 3339 / ISO 8601
	ExcludedCategories []string  `json:"excluded_categories"`
}

// EventInput models candidate events for POST /v1/events/rank
type EventInput struct {
	ID          string    `json:"id"`
	Embedding   []float64 `json:"embedding"`
	Description *string   `json:"description,omitempty"`
	Price       *float64  `json:"price,omitempty"`
	StartTime   *string   `json:"start_time,omitempty"`
	EndTime     *string   `json:"end_time,omitempty"`
	Latitude    *float64  `json:"latitude,omitempty"`
	Longitude   *float64  `json:"longitude,omitempty"`
	Category    *string   `json:"category,omitempty"`
}

// RankingOptions configures thresholding, limits and Jev reranking
type RankingOptions struct {
	MinScore   *float64 `json:"min_score,omitempty"`
	Limit      *int     `json:"limit,omitempty"`
	Rerank     bool     `json:"rerank"`
	RerankTopK *int     `json:"rerank_top_k,omitempty"`
}

// RankEventsRequest is the body for POST /v1/events/rank
type RankEventsRequest struct {
	User            UserInput      `json:"user"`
	Events          []EventInput   `json:"events"`
	SearchEmbedding []float64      `json:"search_embedding,omitempty"`
	SearchText      *string        `json:"search_text,omitempty"`
	Options         RankingOptions `json:"options"`
}

// RankedEvent represents a single scored event from the ML service
type RankedEvent struct {
	EventID     string   `json:"event_id"`
	Score       float64  `json:"score"`
	RerankScore *float64 `json:"rerank_score"`
}

// RankEventsResponse is the response from POST /v1/events/rank
type RankEventsResponse struct {
	Events       []RankedEvent `json:"events"`
	ModelVersion string        `json:"model_version"`
	Reranked     bool          `json:"reranked"`
}

// UpdateUserEmbeddingRequest is the body for POST /v1/compatibility/user-embedding/update
type UpdateUserEmbeddingRequest struct {
	Embedding      []float64 `json:"embedding"`
	Kind           string    `json:"kind"` // "positive" or "negative"
	EventEmbedding []float64 `json:"event_embedding"`
}

// UpdateUserEmbeddingResponse is the response from POST /v1/compatibility/user-embedding/update
type UpdateUserEmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
	Kind      string    `json:"kind"`
}

// Client interacts with the FastAPI inference and recommendation service
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Global default client
var (
	defaultClientLock sync.RWMutex
	defaultClient     *Client
)

// DefaultClient returns the ML client configured from environment
func DefaultClient() *Client {
	defaultClientLock.RLock()
	c := defaultClient
	defaultClientLock.RUnlock()
	if c != nil {
		return c
	}

	defaultClientLock.Lock()
	defer defaultClientLock.Unlock()
	if defaultClient == nil {
		defaultClient = NewClient(env.GetMLServiceURL())
	}
	return defaultClient
}

// SetDefaultClient allows overriding the default ML client (e.g. for testing)
func SetDefaultClient(c *Client) {
	defaultClientLock.Lock()
	defer defaultClientLock.Unlock()
	defaultClient = c
}

// NewClient creates a new ML API client with the given base URL
func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RankEvents calls POST /v1/events/rank on the FastAPI service
func (c *Client) RankEvents(ctx context.Context, req *RankEventsRequest) (*RankEventsResponse, error) {
	url := fmt.Sprintf("%s/v1/events/rank", c.baseURL)

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rank request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ml rank request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ml response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ml rank returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res RankEventsResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ml rank response: %w", err)
	}

	return &res, nil
}

// UpdateUserEmbedding calls POST /v1/compatibility/user-embedding/update
func (c *Client) UpdateUserEmbedding(ctx context.Context, req *UpdateUserEmbeddingRequest) (*UpdateUserEmbeddingResponse, error) {
	url := fmt.Sprintf("%s/v1/compatibility/user-embedding/update", c.baseURL)

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update user embedding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ml update user embedding request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ml response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ml update user embedding returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res UpdateUserEmbeddingResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ml update response: %w", err)
	}

	return &res, nil
}

// GenerateDeterministicEmbedding produces a normalized 1024-d unit vector from a text seed
// Used when real embeddings have not yet been populated for an activity or user.
func GenerateDeterministicEmbedding(seed string, dim int) []float64 {
	if dim <= 0 {
		dim = 1024
	}
	vec := make([]float64, dim)
	h := sha256.Sum256([]byte(seed))
	seedInt := int64(binary.BigEndian.Uint64(h[:8]))
	rnd := rand.New(rand.NewSource(seedInt))

	var sumSq float64
	for i := 0; i < dim; i++ {
		v := rnd.NormFloat64()
		vec[i] = v
		sumSq += v * v
	}
	norm := math.Sqrt(sumSq)
	if norm > 0 {
		for i := 0; i < dim; i++ {
			vec[i] /= norm
		}
	}
	return vec
}

// RankActivities ranks candidate activities for a user using the FastAPI inference service.
// If the ML service is unavailable, it gracefully returns the original activities without failing.
func (c *Client) RankActivities(
	ctx context.Context,
	user *models.User,
	activities []models.Activity,
	opts RankingOptions,
	searchText string,
	searchEmbedding []float64,
) []models.Activity {
	if len(activities) == 0 {
		return activities
	}

	dim := 1024
	if user != nil {
		if len(user.PositiveEmbedding) > 0 {
			dim = len(user.PositiveEmbedding)
		} else if len(user.Embedding) > 0 {
			dim = len(user.Embedding)
		}
	}

	// Prepare user positive and negative embeddings
	var posEmb, negEmb []float64
	var posText, negText *string

	if user != nil {
		if len(user.PositiveEmbedding) == dim {
			posEmb = user.PositiveEmbedding
		} else if len(user.Embedding) == dim {
			posEmb = user.Embedding
		}

		if len(user.NegativeEmbedding) == dim {
			negEmb = user.NegativeEmbedding
		}

		if user.PositiveText != "" {
			posText = &user.PositiveText
		} else if user.Prefs.Answers["perfect_afternoon"] != "" {
			t := user.Prefs.Answers["perfect_afternoon"]
			posText = &t
		}

		if user.NegativeText != "" {
			negText = &user.NegativeText
		} else if user.Prefs.Answers["never_do"] != "" {
			t := user.Prefs.Answers["never_do"]
			negText = &t
		}
	}

	// Fallback positive embedding if not set: generate from user taste/prefs
	if len(posEmb) != dim {
		seed := "user_preferences_default"
		if user != nil {
			var tagKeys []string
			if user.Taste.Tags != nil {
				for k, v := range user.Taste.Tags {
					tagKeys = append(tagKeys, fmt.Sprintf("%s:%.2f", k, v))
				}
			}
			seed = fmt.Sprintf("user:%s:%s:%s", user.ID.Hex(), user.Name, strings.Join(tagKeys, ","))
		}
		posEmb = GenerateDeterministicEmbedding(seed, dim)
	}

	// Negative embedding: all zeros if no dislikes signal
	if len(negEmb) != dim {
		negEmb = make([]float64, dim)
	}

	userInput := UserInput{
		PositiveEmbedding:  posEmb,
		NegativeEmbedding:  negEmb,
		PositiveText:       posText,
		NegativeText:       negText,
		ExcludedCategories: []string{},
	}

	if user != nil && user.Taste.AvoidTags != nil {
		userInput.ExcludedCategories = user.Taste.AvoidTags
	}

	// Map activities to EventInputs
	seenIDs := make(map[string]bool)
	var eventInputs []EventInput
	actMap := make(map[string]models.Activity)

	for _, act := range activities {
		idStr := act.ID.Hex()
		if idStr == "" || seenIDs[idStr] {
			continue
		}
		seenIDs[idStr] = true
		actMap[idStr] = act

		emb := act.Embedding
		if len(emb) != dim {
			embSeed := fmt.Sprintf("event:%s:%s:%s", act.Category, act.Name, strings.Join(act.Tags, ","))
			emb = GenerateDeterministicEmbedding(embSeed, dim)
		}

		var desc *string
		if act.Description != nil && *act.Description != "" {
			desc = act.Description
		} else if act.EmbeddingText != nil && *act.EmbeddingText != "" {
			desc = act.EmbeddingText
		} else if act.Summary != nil && *act.Summary != "" {
			desc = act.Summary
		} else {
			d := fmt.Sprintf("%s in %s. Tags: %s", act.Name, act.City, strings.Join(act.Tags, ", "))
			desc = &d
		}

		var priceFloat *float64
		if act.Price != nil && act.Price.Cents > 0 {
			p := float64(act.Price.Cents) / 100.0
			priceFloat = &p
		}

		var startStr, endStr *string
		if act.Start != nil {
			s := act.Start.UTC().Format(time.RFC3339)
			startStr = &s
		}
		if act.End != nil {
			e := act.End.UTC().Format(time.RFC3339)
			endStr = &e
		}

		var latPtr, lngPtr *float64
		if len(act.Location.Coordinates) >= 2 {
			lng := act.Location.Coordinates[0]
			lat := act.Location.Coordinates[1]
			lngPtr = &lng
			latPtr = &lat
		}

		var catPtr *string
		if act.Category != "" {
			catPtr = &act.Category
		}

		eventInputs = append(eventInputs, EventInput{
			ID:          idStr,
			Embedding:   emb,
			Description: desc,
			Price:       priceFloat,
			StartTime:   startStr,
			EndTime:     endStr,
			Latitude:    latPtr,
			Longitude:   lngPtr,
			Category:    catPtr,
		})
	}

	if len(eventInputs) == 0 {
		return activities
	}

	req := &RankEventsRequest{
		User:            userInput,
		Events:          eventInputs,
		SearchEmbedding: searchEmbedding,
		Options:         opts,
	}
	if searchText != "" {
		req.SearchText = &searchText
	}

	resp, err := c.RankEvents(ctx, req)
	if err != nil {
		log.Warn().Err(err).Msg("ML FastAPI ranking failed, falling back to database order")
		return activities
	}

	var rankedActivities []models.Activity
	scoredIDs := make(map[string]bool)

	for _, re := range resp.Events {
		if act, ok := actMap[re.EventID]; ok {
			scoreVal := re.Score
			act.Score = &scoreVal
			act.RerankScore = re.RerankScore
			rankedActivities = append(rankedActivities, act)
			scoredIDs[re.EventID] = true
		}
	}

	// Append any candidates not explicitly dropped by filters
	for _, act := range activities {
		if !scoredIDs[act.ID.Hex()] {
			rankedActivities = append(rankedActivities, act)
		}
	}

	log.Debug().
		Int("input_count", len(activities)).
		Int("ranked_count", len(rankedActivities)).
		Str("model", resp.ModelVersion).
		Bool("reranked", resp.Reranked).
		Msg("Successfully ranked activities with ML service")

	return rankedActivities
}
