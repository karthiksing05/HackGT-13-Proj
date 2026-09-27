// Package profiles is the real api.Profiles: it keeps users' taste vectors in step with what they
// tell the app. Refresh rebuilds a user's profile texts and vectors from their preferences,
// answers, Facebook interests and rated stops through the ML service; Rated folds one rating into
// the user's vectors with the rated activity's vector, embedding the activity first when it has a
// text but no vector yet. The ML service owns the text format and the encoder: nothing here builds
// an embedding text or invents a vector.
package profiles

import (
	"Backend/pkg/api"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// maxRatedStops is how many ratings (most recent first) a profile is built from.
	maxRatedStops = 20
	// healthTimeout bounds the /healthz lookup that decides whether a stored profile is current.
	healthTimeout = 2 * time.Second
	// inputHashVersion prefixes profileInputHash; bump it when what feeds the request changes.
	inputHashVersion = "in1:"
)

// Service implements api.Profiles over the ML client and the store.
type Service struct {
	st  *store.Store
	ml  *ml.Client
	now func() time.Time
}

var _ api.Profiles = (*Service)(nil)

// New wires the service; now stamps embeddingMeta.generatedAt (nil = time.Now).
func New(st *store.Store, client *ml.Client, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, ml: client, now: now}
}

var errNoClient = errors.New("profiles: no ML client configured")

// Refresh rebuilds the user's profile when it is out of date: never built or its last refresh
// failed, built from other inputs than the current ones, or by another model or template version
// than the ML service now uses. On success it stores the texts, both vectors (the zero vector for
// an empty profile), the model and the hashes; on failure it keeps the old vectors, clears the
// hashes so the next call retries, and returns the error.
func (s *Service) Refresh(ctx context.Context, userID string) error {
	if s.ml == nil {
		return errNoClient
	}
	user, err := s.st.Profiles().User(ctx, userID)
	if err != nil {
		return fmt.Errorf("profile refresh: loading the user: %w", err)
	}
	stops, err := s.st.Profiles().RatedStops(ctx, userID, store.CatalogFor(&user.User), maxRatedStops)
	if err != nil {
		return fmt.Errorf("profile refresh: loading ratings: %w", err)
	}
	req := ml.BuildUserProfileRequest(prefsOf(user.Prefs), user.FacebookInterests, ratedEvents(stops))
	inputHash, err := fingerprint(req)
	if err != nil {
		return fmt.Errorf("profile refresh: %w", err)
	}
	if !s.stale(ctx, user, inputHash) {
		return nil
	}

	profile, err := s.ml.UserProfile(ctx, req)
	if err != nil {
		if clearErr := s.st.Profiles().ClearProfileHash(ctx, userID); clearErr != nil {
			return errors.Join(fmt.Errorf("profile refresh: %w", err), clearErr)
		}
		return fmt.Errorf("profile refresh: %w", err)
	}
	return s.st.Profiles().SetProfile(ctx, userID, store.ProfileFields{
		PositiveText:      profile.PositiveText,
		NegativeText:      profile.NegativeText,
		PositiveEmbedding: profile.PositiveEmbedding,
		NegativeEmbedding: profile.NegativeEmbedding,
		EmbeddingModel:    profile.Model,
		ProfileTextHash:   profile.ProfileTextHash,
		ProfileInputHash:  inputHash,
	})
}

// stale combines ml.ProfileStale (no vector or hash, another model or template version) with the
// inputs check: ProfileStale alone cannot see that the preferences or ratings changed. When the
// service cannot be asked, only the stored fields decide.
func (s *Service) stale(ctx context.Context, user *store.ProfileUser, inputHash string) bool {
	if user.ProfileInputHash != inputHash {
		return true
	}
	hctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	health, err := s.ml.Health(hctx)
	if err != nil {
		health = nil
	}
	return ml.ProfileStale(ml.StoredProfile{
		PositiveEmbedding: user.PositiveEmbedding,
		EmbeddingModel:    user.EmbeddingModel,
		ProfileTextHash:   user.ProfileTextHash,
	}, health)
}

// Rated folds a rating into the user's vectors: 4–5 stars move the positive vector towards the
// activity, 1–2 the negative one, 3 changes nothing. An activity missing from the user's catalog,
// or without a vector or a text to embed, is a no-op.
func (s *Service) Rated(ctx context.Context, userID, activityID string, stars int) error {
	var kind ml.VectorKind
	switch {
	case stars >= 4 && stars <= 5:
		kind = ml.VectorPositive
	case stars >= 1 && stars <= 2:
		kind = ml.VectorNegative
	default:
		return nil
	}
	if activityID == "" {
		return nil
	}
	if s.ml == nil {
		return errNoClient
	}
	user, err := s.st.Profiles().User(ctx, userID)
	if err != nil {
		return fmt.Errorf("rating feedback: loading the user: %w", err)
	}
	activity, err := s.st.Profiles().Activity(ctx, store.CatalogFor(&user.User), activityID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rating feedback: loading the activity: %w", err)
	}
	vector := activity.Embedding
	if !ml.Usable(vector, ml.Dim) {
		if strings.TrimSpace(activity.EmbeddingText) == "" {
			return nil
		}
		if vector, err = s.embedOnDemand(ctx, store.CatalogFor(&user.User), activity); err != nil {
			return fmt.Errorf("rating feedback: %w", err)
		}
	}

	current := user.PositiveEmbedding
	if kind == ml.VectorNegative {
		current = user.NegativeEmbedding
	}
	if len(current) != ml.Dim {
		current = nil // no vector yet: the activity's direction becomes the first one
	}
	updated, err := s.ml.UpdateUserEmbedding(ctx, current, kind, vector)
	if err != nil {
		return fmt.Errorf("rating feedback: %w", err)
	}
	return s.st.Profiles().SetUserVector(ctx, userID, kind == ml.VectorNegative, updated)
}

// embedOnDemand embeds an activity's text and stores the vector with the same embeddingMeta as the
// Python writers (guarded by the text hash, so a text that changed meanwhile keeps its state). The
// vector is used for this rating either way.
func (s *Service) embedOnDemand(ctx context.Context, catalog string, activity *store.CatalogActivity) ([]float64, error) {
	emb, err := s.ml.Embed(ctx, []string{activity.EmbeddingText}, ml.EmbedActivity)
	if err != nil {
		return nil, fmt.Errorf("embedding activity %s: %w", activity.ID, err)
	}
	if emb.Model != ml.Model {
		return nil, fmt.Errorf("embedding activity %s: the service embeds with %q, the catalog with %q", activity.ID, emb.Model, ml.Model)
	}
	vector := emb.Vectors[0]
	if !ml.Usable(vector, ml.Dim) {
		return nil, fmt.Errorf("embedding activity %s: no usable vector", activity.ID)
	}
	textHash := ml.TextHash(activity.EmbeddingText)
	meta := ml.NewActivityEmbeddingMeta(emb, textHash, ml.SourceOnDemand, s.now())
	if _, err := s.st.Profiles().SetActivityEmbedding(ctx, catalog, activity.ID, textHash, vector, emb.Model, meta); err != nil {
		return nil, fmt.Errorf("storing the vector of activity %s: %w", activity.ID, err)
	}
	return vector, nil
}

func prefsOf(p models.UserPrefs) ml.Prefs {
	return ml.Prefs{
		Ratings:     p.Ratings,
		Company:     p.Company,
		Pace:        p.Pace,
		Spend:       p.Spend,
		Flexibility: p.Flexibility,
		PreferFree:  p.PreferFree,
		Answers:     p.Answers,
	}
}

func ratedEvents(stops []store.RatedStop) []ml.RatedEvent {
	out := make([]ml.RatedEvent, 0, len(stops))
	for _, stop := range stops {
		out = append(out, ml.RatedEvent{Stars: stop.Stars, Tags: stop.Tags, Category: stop.Category, ActivityTags: stop.ActivityTags})
	}
	return out
}

// fingerprint identifies the request a profile is built from (map keys are sorted by encoding/json,
// the rated stops come in a fixed order).
func fingerprint(req ml.UserProfileRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("fingerprinting the profile request: %w", err)
	}
	sum := sha1.Sum(payload)
	return inputHashVersion + hex.EncodeToString(sum[:]), nil
}
