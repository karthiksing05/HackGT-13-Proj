package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad expectation: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", raw, want)
	}
}

func TestBuildUserProfileRequestMapsPreferences(t *testing.T) {
	prefs := ml.Prefs{
		Ratings:     map[string]int{"food": 4, "museums": 3, "nightlife": 2, "outdoors": 5, "sports": 0, "shopping": 9},
		Company:     "small_group",
		Pace:        " Balanced ",
		Spend:       "under_15",
		Flexibility: "bit_over_ok",
		PreferFree:  true,
		Answers:     map[string]string{"perfectAfternoon": " A long walk ", "never_want": "Packed clubs", "plan_around": "", "bogus": "x"},
	}
	rated := []ml.RatedEvent{
		{Stars: 5, Tags: []string{"Great people"}, Category: "park", ActivityTags: []string{"outdoor"}},
		{Stars: 0},
		{Stars: 2, Category: "bar"},
	}
	req := ml.BuildUserProfileRequest(prefs, []string{"Hiking", " ", "Indie rock"}, rated)
	assertJSON(t, req, `{
		"ratings": {"food": 4, "museums": 3, "nightlife": 2, "outdoors": 5, "shopping": 5},
		"company": "small_group", "pace": "balanced", "spend": "under_15", "flexibility": "bit_over_ok",
		"prefer_free": true,
		"answers": {"perfect_afternoon": "A long walk", "never_do": "Packed clubs"},
		"facebook_interests": ["Hiking", "Indie rock"],
		"rated_events": [
			{"stars": 5, "tags": ["Great people"], "category": "park", "activity_tags": ["outdoor"]},
			{"stars": 2, "category": "bar"}
		]
	}`)
}

func TestBuildUserProfileRequestDropsUnknownValues(t *testing.T) {
	req := ml.BuildUserProfileRequest(ml.Prefs{Company: "duo", Pace: "frantic", Spend: "lots", Flexibility: "whatever"}, nil, nil)
	assertJSON(t, req, `{"prefer_free": false, "answers": {}}`)
	if chill := ml.BuildUserProfileRequest(ml.Prefs{Pace: "chill"}, nil, nil); chill.Pace != "chill" {
		t.Fatalf("chill is accepted: %q", chill.Pace)
	}
}

func TestBuildUserProfileRequestKeepsTheNewestTwentyRatings(t *testing.T) {
	rated := make([]ml.RatedEvent, 25)
	for i := range rated {
		rated[i] = ml.RatedEvent{Stars: 1 + i%5, Category: "park"}
	}
	req := ml.BuildUserProfileRequest(ml.Prefs{}, nil, rated)
	if len(req.RatedEvents) != 20 || req.RatedEvents[0].Stars != 1 || req.RatedEvents[19].Stars != 5 {
		t.Fatalf("rated events: %+v", req.RatedEvents)
	}
}

func userProfilePayload(positive, negative []float64, positiveText, negativeText string) map[string]any {
	return map[string]any{
		"positive_text": positiveText, "negative_text": negativeText,
		"positive_embedding": positive, "negative_embedding": negative,
		"profile_text_hash": "profile-v1:0123456789abcdef", "template_version": "profile-v1",
		"model": ml.Model, "dim": ml.Dim, "provider": "local",
	}
}

func TestUserProfile(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/user-profile", http.StatusOK, userProfilePayload(basis(ml.Dim, 3), basis(ml.Dim, 4), "Interests:\n- local food", "Interests:\n- nightlife"))
	req := ml.UserProfileRequest{Ratings: map[string]int{"food": 5, "nightlife": 1}, Company: "Solo", RatedEvents: []ml.RatedEvent{{Stars: 7}}}
	got, err := f.client().UserProfile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.PositiveEmbedding[3] != 1 || got.NegativeEmbedding[4] != 1 || got.ProfileTextHash != "profile-v1:0123456789abcdef" || got.TemplateVersion != "profile-v1" || got.Provider != "local" || got.Unranked() {
		t.Fatalf("profile: %+v", got)
	}
	body := f.body("/v1/user-profile", -1)
	if body["company"] != "solo" || body["rated_events"] != nil || body["answers"] == nil {
		t.Fatalf("the request must be sanitized: %v", body)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "embedding") {
		t.Fatalf("vectors must not serialize: %s", raw)
	}
}

func TestUserProfileEmptyIsUnranked(t *testing.T) {
	f := newFakeML(t)
	zeros := make([]float64, ml.Dim)
	f.reply("/v1/user-profile", http.StatusOK, userProfilePayload(zeros, zeros, "", ""))
	got, err := f.client().UserProfile(context.Background(), ml.UserProfileRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unranked() || len(got.PositiveEmbedding) != ml.Dim {
		t.Fatalf("an empty profile is a zero vector, unranked: %+v", got.Unranked())
	}
}

func TestUserProfileRejectsMalformedAnswers(t *testing.T) {
	good := func() map[string]any {
		return userProfilePayload(basis(ml.Dim, 0), make([]float64, ml.Dim), "Interests:\n- x", "")
	}
	cases := map[string]func(map[string]any){
		"dim":               func(p map[string]any) { p["dim"] = 8 },
		"no negative":       func(p map[string]any) { p["negative_embedding"] = nil },
		"short positive":    func(p map[string]any) { p["positive_embedding"] = basis(8, 0) },
		"no hash":           func(p map[string]any) { p["profile_text_hash"] = "" },
		"no model reported": func(p map[string]any) { p["model"] = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeML(t)
			payload := good()
			mutate(payload)
			f.reply("/v1/user-profile", http.StatusOK, payload)
			if _, err := f.client().UserProfile(context.Background(), ml.UserProfileRequest{}); !errors.Is(err, ml.ErrBadResponse) {
				t.Fatalf("err = %v, want ErrBadResponse", err)
			}
		})
	}
}

func TestUserProfileUnavailable(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/user-profile", http.StatusServiceUnavailable, map[string]any{"detail": "Embedding provider unavailable."})
	if _, err := f.client().UserProfile(context.Background(), ml.UserProfileRequest{}); !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchProfile(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/search-profile", http.StatusOK, map[string]any{
		"search_text": "Interests:\n- outdoor recreation", "search_embedding": basis(ml.Dim, 7), "model": ml.Model, "dim": ml.Dim, "provider": "cache",
	})
	start := time.Date(2026, 9, 25, 18, 10, 0, 0, time.UTC)
	req := ml.SearchProfileRequest{
		MoodText: "Something chill and outside", Tags: []string{"Outdoors", " "}, Who: "friends", Pace: "balanced",
		Budget: ptr(0), StartTime: &start, BackBy: ptr(start.Add(4*time.Hour + 20*time.Minute)), Timezone: "America/New_York",
	}
	got, err := f.client().SearchProfile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchText != "Interests:\n- outdoor recreation" || got.SearchEmbedding[7] != 1 || got.Provider != "cache" {
		t.Fatalf("search profile: %+v", got)
	}
	assertJSON(t, f.body("/v1/search-profile", -1), `{
		"mood_text": "Something chill and outside", "tags": ["Outdoors"], "who": "friends", "pace": "balanced",
		"budget": 0, "start_time": "2026-09-25T18:10:00Z", "back_by": "2026-09-25T22:30:00Z", "timezone": "America/New_York"
	}`)
}

func TestSearchProfileWithoutTextHasNoVector(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/search-profile", http.StatusOK, map[string]any{"search_text": "", "search_embedding": nil, "model": ml.Model, "dim": ml.Dim, "provider": "none"})
	got, err := f.client().SearchProfile(context.Background(), ml.SearchProfileRequest{Who: "everyone", Pace: "frantic", Budget: ptr(7)})
	if err != nil || got.SearchEmbedding != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	assertJSON(t, f.body("/v1/search-profile", -1), `{}`)
}

func TestSearchProfileRejectsAShortVector(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/search-profile", http.StatusOK, map[string]any{"search_text": "Pace:\n- chill", "search_embedding": basis(8, 0), "model": ml.Model, "dim": ml.Dim})
	if _, err := f.client().SearchProfile(context.Background(), ml.SearchProfileRequest{Tags: []string{"Chill"}}); !errors.Is(err, ml.ErrBadResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestProfileStale(t *testing.T) {
	health := &ml.Health{EmbeddingModel: ml.Model, TemplateVersion: "profile-v1"}
	fresh := ml.StoredProfile{PositiveEmbedding: basis(ml.Dim, 0), EmbeddingModel: ml.Model, ProfileTextHash: "profile-v1:abc"}
	cases := []struct {
		name   string
		stored ml.StoredProfile
		health *ml.Health
		stale  bool
	}{
		{"fresh", fresh, health, false},
		{"fresh, service unknown", fresh, nil, false},
		{"empty profile stored as zeros", ml.StoredProfile{PositiveEmbedding: make([]float64, ml.Dim), EmbeddingModel: ml.Model, ProfileTextHash: "profile-v1:e"}, health, false},
		{"no vector", ml.StoredProfile{EmbeddingModel: ml.Model, ProfileTextHash: "profile-v1:abc"}, health, true},
		{"hash cleared after a failed refresh", ml.StoredProfile{PositiveEmbedding: basis(ml.Dim, 0), EmbeddingModel: ml.Model}, nil, true},
		{"other model", ml.StoredProfile{PositiveEmbedding: basis(ml.Dim, 0), EmbeddingModel: "other", ProfileTextHash: "profile-v1:abc"}, health, true},
		{"old template", ml.StoredProfile{PositiveEmbedding: basis(ml.Dim, 0), EmbeddingModel: ml.Model, ProfileTextHash: "profile-v0:abc"}, health, true},
		{"service reports no model or template", fresh, &ml.Health{}, false},
	}
	for _, c := range cases {
		if got := ml.ProfileStale(c.stored, c.health); got != c.stale {
			t.Errorf("%s: stale = %v", c.name, got)
		}
	}
}

func TestUpdateUserEmbedding(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/compatibility/user-embedding/update", http.StatusOK, map[string]any{"embedding": basis(ml.Dim, 9), "kind": "negative"})
	c := f.client()
	activity := filled(ml.Dim, 0.123456789)
	got, err := c.UpdateUserEmbedding(context.Background(), nil, ml.VectorNegative, activity)
	if err != nil || got[9] != 1 {
		t.Fatalf("got %v, %v", got != nil, err)
	}
	body := f.body("/v1/compatibility/user-embedding/update", -1)
	current, event := body["embedding"].([]any), body["event_embedding"].([]any)
	if body["kind"] != "negative" || len(current) != ml.Dim || current[0] != 0.0 || event[0] != 0.12346 {
		t.Fatalf("request: kind %v, current[0] %v, event[0] %v", body["kind"], current[0], event[0])
	}

	for name, call := range map[string]func() ([]float64, error){
		"kind": func() ([]float64, error) {
			return c.UpdateUserEmbedding(context.Background(), nil, "neutral", activity)
		},
		"zero activity": func() ([]float64, error) {
			return c.UpdateUserEmbedding(context.Background(), nil, ml.VectorPositive, make([]float64, ml.Dim))
		},
		"user dim": func() ([]float64, error) {
			return c.UpdateUserEmbedding(context.Background(), basis(8, 0), ml.VectorPositive, activity)
		},
	} {
		if _, err := call(); !errors.Is(err, ml.ErrInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := f.count("/v1/compatibility/user-embedding/update"); n != 1 {
		t.Fatalf("invalid updates must not be sent (%d requests)", n)
	}

	f.reply("/v1/compatibility/user-embedding/update", http.StatusOK, map[string]any{"embedding": basis(8, 0), "kind": "positive"})
	if _, err := c.UpdateUserEmbedding(context.Background(), basis(ml.Dim, 1), ml.VectorPositive, activity); !errors.Is(err, ml.ErrBadResponse) {
		t.Fatalf("short answer: err = %v", err)
	}
}
