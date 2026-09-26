package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveService runs the client against a real ML service (skipped unless ML_LIVE_URL is set):
//
//	ML_LIVE_URL=http://127.0.0.1:8000 go test ./pkg/ml/ -run Live -v
func TestLiveService(t *testing.T) {
	url := os.Getenv("ML_LIVE_URL")
	if url == "" {
		t.Skip("set ML_LIVE_URL to run against a real ML service")
	}
	o := ml.DefaultOptions()
	o.BaseURL = url
	o.Rerank = false                   // Jev is a paid call; the rerank path is covered by the fakes
	o.EmbedTimeout = 120 * time.Second // the first call may load the local model
	o.SearchTimeout = 120 * time.Second
	c := ml.NewClientWithOptions(o)
	ctx := context.Background()

	h, err := c.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.EmbeddingModel != ml.Model || h.EmbeddingDim != ml.Dim || h.TemplateVersion != "profile-v1" {
		t.Fatalf("health: %+v", h)
	}
	t.Logf("health: status=%s mode=%s ranking=%s providers=%v", h.Status, h.EmbeddingMode, h.RankingModelVersion, h.Providers)

	prefs := ml.Prefs{
		Ratings: map[string]int{"outdoors": 5, "food": 4, "nightlife": 1, "big_crowds": 1},
		Company: "small_group", Pace: "balanced", Spend: "under_15", Flexibility: "bit_over_ok", PreferFree: true,
	}
	profile, err := c.UserProfile(ctx, ml.BuildUserProfileRequest(prefs, []string{"Hiking", "Street food"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile.PositiveText, "- outdoor recreation") || !strings.HasPrefix(profile.ProfileTextHash, "profile-v1:") ||
		profile.Unranked() || math.Abs(norm(profile.PositiveEmbedding)-1) > 1e-6 || math.Abs(norm(profile.NegativeEmbedding)-1) > 1e-6 {
		t.Fatalf("profile: %+v", profile)
	}
	t.Logf("profile via %s:\n%s\n--- dislikes ---\n%s", profile.Provider, profile.PositiveText, profile.NegativeText)

	start := time.Date(2026, 9, 25, 18, 10, 0, 0, time.UTC)
	back := start.Add(4*time.Hour + 20*time.Minute)
	search, err := c.SearchProfile(ctx, ml.SearchProfileRequest{
		MoodText: "Something chill and outside, then cheap food after.", Tags: []string{"Outdoors", "Food"},
		Who: "friends", Pace: "balanced", Budget: ptr(1), StartTime: &start, BackBy: &back, Timezone: "America/New_York",
	})
	if err != nil || len(search.SearchEmbedding) != ml.Dim || !strings.Contains(search.SearchText, "- friday afternoon") {
		t.Fatalf("search: %+v %v", search, err)
	}

	texts := []string{
		"Interests:\n- outdoor recreation\n- nature\n\nActivities:\n- park visit\n- walking trails\n\nEnvironment:\n- outdoor setting\n\nCost:\n- free admission",
		"Interests:\n- nightlife\n- electronic music\n\nActivities:\n- dancing\n\nSocial:\n- large crowds\n\nEnvironment:\n- loud nightclub setting\n\nTiming:\n- late night",
		"Interests:\n- local food\n- street food\n\nActivities:\n- food tasting\n- market browsing\n\nCost:\n- under $15 admission",
	}
	emb, err := c.Embed(ctx, texts, ml.EmbedActivity)
	if err != nil || len(emb.Vectors) != 3 {
		t.Fatalf("embed: %v", err)
	}
	candidates := []ml.Candidate{
		{ID: "park", Embedding: emb.Vectors[0], Description: texts[0]},
		{ID: "club", Embedding: emb.Vectors[1], Description: texts[1]},
		{ID: "market", Embedding: emb.Vectors[2], Description: texts[2]},
		{ID: "no-vector"},
	}
	user := ml.UserVectors{Positive: profile.PositiveEmbedding, Negative: profile.NegativeEmbedding, PositiveText: profile.PositiveText, NegativeText: profile.NegativeText}
	res := c.RankActivities(ctx, user, candidates, ml.Search{Embedding: search.SearchEmbedding, Text: search.SearchText}, ml.Constraints{}, 0)
	if res.Reason != "" || res.Sent != 3 || res.Unembedded != 1 || res.Items[len(res.Items)-1].ID != "no-vector" {
		t.Fatalf("rank: %+v", res)
	}
	for _, item := range res.Items {
		if item.Score != nil {
			t.Logf("rank: %-9s %.3f", item.ID, *item.Score)
		}
	}
	if position(res.IDs(), "club") < position(res.IDs(), "park") {
		t.Fatalf("a nightclub outranks a park for a nightlife-averse outdoors lover: %v", res.IDs())
	}

	updated, err := c.UpdateUserEmbedding(ctx, profile.NegativeEmbedding, ml.VectorNegative, emb.Vectors[1])
	if err != nil || math.Abs(norm(updated)-1) > 1e-6 {
		t.Fatalf("update: %v", err)
	}
}

func norm(v []float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x * x
	}
	return math.Sqrt(sum)
}

func position(ids []string, id string) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return -1
}
