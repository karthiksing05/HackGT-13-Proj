package profiles_test

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/profiles"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TestLiveProfiles runs Refresh and Rated against a real ML service (skipped unless ML_LIVE_URL is
// set). The demo seed user's texts must match the ML golden byte for byte, and a real demo
// activity (copied from the read-only seed catalog without its vector) is embedded on demand to
// the stored production vector:
//
//	ML_LIVE_URL=http://127.0.0.1:8000 go test ./pkg/profiles/ -run Live -v
func TestLiveProfiles(t *testing.T) {
	url := os.Getenv("ML_LIVE_URL")
	if url == "" {
		t.Skip("set ML_LIVE_URL to run against a real ML service")
	}
	ctx := context.Background()
	st := testutil.Store(t, time.Now)
	opts := ml.DefaultOptions()
	opts.BaseURL = url
	opts.EmbedTimeout = 120 * time.Second // the first call may load the local model
	svc := profiles.New(st, ml.NewClientWithOptions(opts), time.Now)

	var golden struct {
		Request struct {
			Ratings     map[string]int    `json:"ratings"`
			Company     string            `json:"company"`
			Pace        string            `json:"pace"`
			Spend       string            `json:"spend"`
			Flexibility string            `json:"flexibility"`
			PreferFree  bool              `json:"prefer_free"`
			Answers     map[string]string `json:"answers"`
		} `json:"request"`
		PositiveText    string `json:"positive_text"`
		NegativeText    string `json:"negative_text"`
		ProfileTextHash string `json:"profile_text_hash"`
	}
	raw, err := os.ReadFile("../../../ml/tests/fixtures/profile_seed_user.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	r := golden.Request
	user := &models.User{
		Email: testutil.UniqueEmail("sandy"), Name: "Sandy Byte", Username: "sandy_live",
		Prefs: models.UserPrefs{Ratings: r.Ratings, Company: r.Company, Pace: r.Pace, Spend: r.Spend, Flexibility: r.Flexibility,
			PreferFree: r.PreferFree, Answers: r.Answers},
	}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	id := user.ID.Hex()
	if err := svc.Refresh(ctx, id); err != nil {
		t.Fatal(err)
	}
	u, err := st.Profiles().User(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.PositiveText != golden.PositiveText || u.NegativeText != golden.NegativeText || u.ProfileTextHash != golden.ProfileTextHash {
		t.Fatalf("texts differ from the ML golden:\n%s\n--- dislikes ---\n%s\n%s", u.PositiveText, u.NegativeText, u.ProfileTextHash)
	}
	t.Logf("profile %s via %s, %d-d vectors", u.ProfileTextHash, u.EmbeddingModel, len(u.PositiveEmbedding))

	var seed bson.M
	err = st.DB().Client().Database("freetime").Collection(store.ActivityCollection).
		FindOne(ctx, bson.M{"embedding": bson.M{"$exists": true}, "embeddingText": bson.M{"$exists": true}, "category": "park"}).Decode(&seed)
	if errors.Is(err, mongo.ErrNoDocuments) {
		t.Skip("no seeded demo_activities with a vector in the freetime database")
	}
	if err != nil {
		t.Fatal(err)
	}
	stored := toFloats(t, seed["embedding"])
	delete(seed, "embedding")
	delete(seed, "embeddingMeta")
	delete(seed, "embeddingModel")
	if _, err := st.Collection(store.ActivityCollection).InsertOne(ctx, seed); err != nil {
		t.Fatal(err)
	}
	activityID := seed["_id"].(bson.ObjectID).Hex()
	if err := svc.Rated(ctx, id, activityID, 5); err != nil {
		t.Fatal(err)
	}
	activity, err := st.Profiles().Activity(ctx, store.ActivityCollection, activityID)
	if err != nil {
		t.Fatal(err)
	}
	cosine := dot(activity.Embedding, stored)
	t.Logf("%v (%s): on-demand vector vs the stored production vector: cosine %.6f", seed["name"], activity.Category, cosine)
	if cosine < 0.995 {
		t.Fatalf("on-demand vector drifted from the catalog's: cosine %.6f", cosine)
	}
	after, err := st.Profiles().User(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if moved := dot(after.PositiveEmbedding, u.PositiveEmbedding); moved >= 0.999999 || !ml.Usable(after.PositiveEmbedding, ml.Dim) {
		t.Fatalf("a 5-star rating moves the positive vector (cosine to before %.6f)", moved)
	}
}

func toFloats(t *testing.T, v any) []float64 {
	t.Helper()
	arr, ok := v.(bson.A)
	if !ok {
		t.Fatalf("embedding is %T", v)
	}
	out := make([]float64, len(arr))
	for i, x := range arr {
		out[i] = x.(float64)
	}
	return out
}

func dot(a, b []float64) float64 {
	if len(a) != len(b) {
		return math.NaN()
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
