package ml

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

// maxRatedEvents is how many ratings (newest first) a profile is built from.
const maxRatedEvents = 20

var (
	companies     = []string{"solo", "small_group", "big_group"}
	paces         = []string{"relaxed", "chill", "balanced", "packed"}
	spends        = []string{"free_only", "under_15", "15_to_40", "over_40"}
	flexibilities = []string{"stick_to_budget", "bit_over_ok"}
	whos          = []string{"just_me", "friends", "open"}
)

// Prefs is the part of the app's saved `Preferences` a profile is built from, in the contract's
// shape (snake_case enum values).
type Prefs struct {
	Ratings     map[string]int    // outdoors, food, museums, live_music, …: 1–5
	Company     string            // solo | small_group | big_group
	Pace        string            // relaxed | balanced | packed ("chill" accepted)
	Spend       string            // free_only | under_15 | 15_to_40 | over_40
	Flexibility string            // stick_to_budget | bit_over_ok
	PreferFree  bool              //
	Answers     map[string]string // perfect_afternoon, never_do, plan_around (camelCase keys accepted)
}

// ProfileAnswers are Setup's open answers: the first and last are likes, never_do dislikes.
type ProfileAnswers struct {
	PerfectAfternoon string `json:"perfect_afternoon,omitempty"`
	NeverDo          string `json:"never_do,omitempty"`
	PlanAround       string `json:"plan_around,omitempty"`
}

// RatedEvent is one of the user's ratings with what is known about the rated activity.
type RatedEvent struct {
	Stars        int      `json:"stars"`                   // 1–5
	Tags         []string `json:"tags,omitempty"`          // the rating's tags ("Great people", "Too crowded", …)
	Category     string   `json:"category,omitempty"`      // the activity's category (park, bar, museum, …)
	ActivityTags []string `json:"activity_tags,omitempty"` // the activity's own tags (outdoor, late_night, …)
}

// UserProfileRequest is the body of POST /v1/user-profile. Build it with BuildUserProfileRequest.
type UserProfileRequest struct {
	Ratings           map[string]int `json:"ratings,omitempty"`
	Company           string         `json:"company,omitempty"`
	Pace              string         `json:"pace,omitempty"`
	Spend             string         `json:"spend,omitempty"`
	Flexibility       string         `json:"flexibility,omitempty"`
	PreferFree        bool           `json:"prefer_free"`
	Answers           ProfileAnswers `json:"answers"`
	FacebookInterests []string       `json:"facebook_interests,omitempty"`
	RatedEvents       []RatedEvent   `json:"rated_events,omitempty"` // newest first
}

// BuildUserProfileRequest maps saved preferences, imported Facebook interests and the user's
// ratings (newest first; the first 20 count) to a /v1/user-profile request. Values the service
// would reject are dropped here instead of failing the whole request: unknown enum values,
// ratings outside 1–5 (0 means "not rated", not "hate it") and stars outside 1–5.
func BuildUserProfileRequest(p Prefs, facebookInterests []string, rated []RatedEvent) UserProfileRequest {
	return UserProfileRequest{
		Ratings:           p.Ratings,
		Company:           p.Company,
		Pace:              p.Pace,
		Spend:             p.Spend,
		Flexibility:       p.Flexibility,
		PreferFree:        p.PreferFree,
		Answers:           answersFrom(p.Answers),
		FacebookInterests: facebookInterests,
		RatedEvents:       rated,
	}.sanitized()
}

func (r UserProfileRequest) sanitized() UserProfileRequest {
	out := r
	out.Company = oneOf(r.Company, companies)
	out.Pace = oneOf(r.Pace, paces)
	out.Spend = oneOf(r.Spend, spends)
	out.Flexibility = oneOf(r.Flexibility, flexibilities)
	out.Ratings = nil
	for key, value := range r.Ratings {
		if key = strings.TrimSpace(key); key == "" || value < 1 {
			continue
		}
		if out.Ratings == nil {
			out.Ratings = make(map[string]int, len(r.Ratings))
		}
		out.Ratings[key] = min(value, 5)
	}
	out.FacebookInterests = nil
	for _, interest := range r.FacebookInterests {
		if s := strings.TrimSpace(interest); s != "" {
			out.FacebookInterests = append(out.FacebookInterests, s)
		}
	}
	out.RatedEvents = nil
	for _, e := range r.RatedEvents {
		if e.Stars < 1 || e.Stars > 5 {
			continue
		}
		out.RatedEvents = append(out.RatedEvents, e)
		if len(out.RatedEvents) == maxRatedEvents {
			break
		}
	}
	return out
}

// UserProfile is the service's profile for one user: the eight-section texts, their vectors (Dim
// values each; NegativeEmbedding is all zeros when NegativeText is empty) and the hash naming the
// text version. Store PositiveEmbedding even when it is all zeros (an empty profile) so
// ProfileStale does not rebuild it on every call.
type UserProfile struct {
	PositiveText      string    `json:"positive_text"`
	NegativeText      string    `json:"negative_text"`
	PositiveEmbedding []float64 `json:"-"`
	NegativeEmbedding []float64 `json:"-"`
	ProfileTextHash   string    `json:"profile_text_hash"` // "profile-v1:<sha1>"
	TemplateVersion   string    `json:"template_version"`
	Model             string    `json:"model"`
	Dim               int       `json:"dim"`
	Provider          string    `json:"provider"`
}

// Unranked reports whether the profile carries no signal yet (no ratings, answers or interests):
// its positive vector is all zeros, so rank by the search vector or a prior instead.
func (p *UserProfile) Unranked() bool {
	return p == nil || p.PositiveText == "" || IsZero(p.PositiveEmbedding)
}

type userProfileResponse struct {
	PositiveText      string    `json:"positive_text"`
	NegativeText      string    `json:"negative_text"`
	PositiveEmbedding []float64 `json:"positive_embedding"`
	NegativeEmbedding []float64 `json:"negative_embedding"`
	ProfileTextHash   string    `json:"profile_text_hash"`
	TemplateVersion   string    `json:"template_version"`
	Model             string    `json:"model"`
	Dim               int       `json:"dim"`
	Provider          string    `json:"provider"`
}

// UserProfile builds the user's texts and embeds them (POST /v1/user-profile). The request is
// sanitized as in BuildUserProfileRequest. Deadline: EmbedTimeout unless ctx has one. On error
// keep the stored vectors and clear the stored hash, so the next rank call retries.
func (c *Client) UserProfile(ctx context.Context, req UserProfileRequest) (*UserProfile, error) {
	var resp userProfileResponse
	if err := c.do(ctx, http.MethodPost, "/v1/user-profile", c.opts.EmbedTimeout, req.sanitized(), &resp); err != nil {
		return nil, err
	}
	if resp.Dim != c.opts.Dim {
		return nil, fmt.Errorf("%w: /v1/user-profile reports dim %d, want %d", ErrBadResponse, resp.Dim, c.opts.Dim)
	}
	if err := checkVector(resp.PositiveEmbedding, c.opts.Dim, "positive_embedding"); err != nil {
		return nil, err
	}
	if err := checkVector(resp.NegativeEmbedding, c.opts.Dim, "negative_embedding"); err != nil {
		return nil, err
	}
	if resp.ProfileTextHash == "" || resp.Model == "" {
		return nil, fmt.Errorf("%w: /v1/user-profile has no profile_text_hash or model", ErrBadResponse)
	}
	return &UserProfile{
		PositiveText:      resp.PositiveText,
		NegativeText:      resp.NegativeText,
		PositiveEmbedding: resp.PositiveEmbedding,
		NegativeEmbedding: resp.NegativeEmbedding,
		ProfileTextHash:   resp.ProfileTextHash,
		TemplateVersion:   resp.TemplateVersion,
		Model:             resp.Model,
		Dim:               resp.Dim,
		Provider:          resp.Provider,
	}, nil
}

// StoredProfile is what the backend keeps on the user document (`positiveEmbedding`,
// `embeddingModel`, `profileTextHash`).
type StoredProfile struct {
	PositiveEmbedding []float64
	EmbeddingModel    string
	ProfileTextHash   string
}

// ProfileStale reports whether a stored profile must be rebuilt before ranking: it has no
// positive vector or no hash (never built, or the last refresh failed and cleared it), or the
// service now embeds with another model or renders another template version. h may be nil when
// the service is unreachable; only the stored fields are checked then.
func ProfileStale(p StoredProfile, h *Health) bool {
	if len(p.PositiveEmbedding) == 0 || p.ProfileTextHash == "" {
		return true
	}
	if h == nil {
		return false
	}
	if h.EmbeddingModel != "" && p.EmbeddingModel != h.EmbeddingModel {
		return true
	}
	return h.TemplateVersion != "" && !strings.HasPrefix(p.ProfileTextHash, h.TemplateVersion+":")
}

// VectorKind names which of a user's vectors an update folds an activity into.
type VectorKind string

const (
	VectorPositive VectorKind = "positive" // liked or attended (stars >= 3)
	VectorNegative VectorKind = "negative" // disliked
)

type updateRequest struct {
	Embedding      Vec        `json:"embedding"`
	Kind           VectorKind `json:"kind"`
	EventEmbedding Vec        `json:"event_embedding"`
}

type updateResponse struct {
	Embedding []float64 `json:"embedding"`
	Kind      string    `json:"kind"`
}

// UpdateUserEmbedding folds an activity's vector into one of the user's vectors
// (POST /v1/compatibility/user-embedding/update: normalize(α·current + (1−α)·activity)) and
// returns the new vector to store. A missing or all-zero current vector becomes the activity's
// direction, which is how a first negative vector is built. Deadline: UpdateTimeout unless ctx
// has one.
func (c *Client) UpdateUserEmbedding(ctx context.Context, current []float64, kind VectorKind, activity []float64) ([]float64, error) {
	if kind != VectorPositive && kind != VectorNegative {
		return nil, fmt.Errorf("%w: vector kind %q", ErrInvalidRequest, kind)
	}
	if !Usable(activity, c.opts.Dim) {
		return nil, fmt.Errorf("%w: the activity has no usable %d-d vector", ErrInvalidRequest, c.opts.Dim)
	}
	if len(current) == 0 {
		current = make([]float64, c.opts.Dim) // no signal yet
	}
	if len(current) != c.opts.Dim {
		return nil, fmt.Errorf("%w: the user vector has %d values, want %d", ErrInvalidRequest, len(current), c.opts.Dim)
	}
	var resp updateResponse
	body := updateRequest{Embedding: current, Kind: kind, EventEmbedding: activity}
	if err := c.do(ctx, http.MethodPost, "/v1/compatibility/user-embedding/update", c.opts.UpdateTimeout, body, &resp); err != nil {
		return nil, err
	}
	if err := checkVector(resp.Embedding, c.opts.Dim, "embedding"); err != nil {
		return nil, err
	}
	return resp.Embedding, nil
}

// SearchProfileRequest is the preference part of a PlanRequest (POST /v1/search-profile).
type SearchProfileRequest struct {
	MoodText  string     `json:"mood_text,omitempty"`
	Tags      []string   `json:"tags,omitempty"` // the quick picks as the app sends them ("Outdoors", "Meet people")
	Who       string     `json:"who,omitempty"`  // just_me | friends | open
	Pace      string     `json:"pace,omitempty"` // relaxed | balanced | packed
	Budget    *int       `json:"budget,omitempty"`
	StartTime *time.Time `json:"start_time,omitempty"`
	BackBy    *time.Time `json:"back_by,omitempty"`
	Timezone  string     `json:"timezone,omitempty"` // IANA zone of the viewer, for "friday afternoon"
}

func (r SearchProfileRequest) sanitized() SearchProfileRequest {
	out := r
	out.Who = oneOf(r.Who, whos)
	out.Pace = oneOf(r.Pace, paces)
	if r.Budget != nil && (*r.Budget < 0 || *r.Budget > 3) {
		out.Budget = nil
	}
	out.Tags = nil
	for _, tag := range r.Tags {
		if s := strings.TrimSpace(tag); s != "" {
			out.Tags = append(out.Tags, s)
		}
	}
	return out
}

// SearchProfile is the per-request search text and vector; SearchEmbedding is nil when the
// request says nothing (SearchText == "").
type SearchProfile struct {
	SearchText      string    `json:"search_text"`
	SearchEmbedding []float64 `json:"-"`
	Model           string    `json:"model"`
	Dim             int       `json:"dim"`
	Provider        string    `json:"provider"`
}

type searchProfileResponse struct {
	SearchText      string    `json:"search_text"`
	SearchEmbedding []float64 `json:"search_embedding"`
	Model           string    `json:"model"`
	Dim             int       `json:"dim"`
	Provider        string    `json:"provider"`
}

// SearchProfile builds and embeds the search text for one plan request. It is never stored.
// Deadline: SearchTimeout unless ctx has one.
func (c *Client) SearchProfile(ctx context.Context, req SearchProfileRequest) (*SearchProfile, error) {
	var resp searchProfileResponse
	if err := c.do(ctx, http.MethodPost, "/v1/search-profile", c.opts.SearchTimeout, req.sanitized(), &resp); err != nil {
		return nil, err
	}
	out := &SearchProfile{SearchText: resp.SearchText, Model: resp.Model, Dim: resp.Dim, Provider: resp.Provider}
	if resp.SearchText == "" {
		return out, nil
	}
	if err := checkVector(resp.SearchEmbedding, c.opts.Dim, "search_embedding"); err != nil {
		return nil, err
	}
	out.SearchEmbedding = resp.SearchEmbedding
	return out, nil
}

// answersFrom maps the stored answers to the request, accepting camelCase keys and the old
// "never_want" name.
func answersFrom(answers map[string]string) ProfileAnswers {
	var out ProfileAnswers
	for key, value := range answers {
		value = strings.TrimSpace(value)
		switch snakeCase(key) {
		case "perfect_afternoon":
			out.PerfectAfternoon = value
		case "never_do", "never_want":
			out.NeverDo = value
		case "plan_around":
			out.PlanAround = value
		}
	}
	return out
}

// snakeCase turns "perfectAfternoon", "Perfect Afternoon" or "perfect-afternoon" into
// "perfect_afternoon".
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r == '-' || r == ' ':
			b.WriteRune('_')
		case unicode.IsUpper(r):
			if i > 0 {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(b.String(), "__", "_")
}

// oneOf returns the normalized value when it is allowed, else "" (omitted on the wire).
func oneOf(value string, allowed []string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(allowed, v) {
		return v
	}
	return ""
}
