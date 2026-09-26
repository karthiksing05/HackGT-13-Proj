package profiles_test

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/profiles"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeML answers like the ML service, deterministically: profile vectors are unit vectors picked
// by the request's hash, activity vectors by the text's length, and the user-vector update does
// the real moving average (alpha 0.8).
type fakeML struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	hits   map[string]int
	bodies map[string][][]byte
	down   map[string]bool
	model  string
}

func newFakeML(t *testing.T) *fakeML {
	f := &fakeML{t: t, hits: map[string]int{}, bodies: map[string][][]byte{}, down: map[string]bool{}, model: ml.Model}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeML) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("reading the request: %v", err)
	}
	f.mu.Lock()
	f.hits[r.URL.Path]++
	f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], raw)
	down, model := f.down[r.URL.Path] || f.down["*"], f.model
	f.mu.Unlock()
	if down {
		reply(w, http.StatusServiceUnavailable, map[string]any{"detail": "Embedding provider unavailable."})
		return
	}
	switch r.URL.Path {
	case "/healthz":
		reply(w, http.StatusOK, map[string]any{
			"status": "ok", "uptime_seconds": 1.0, "jev": false, "profile_template_version": "profile-v1",
			"ranking":   map[string]any{"model_version": "classifier-v1", "embedding_dim": ml.Dim},
			"embedding": map[string]any{"model": model, "dim": ml.Dim, "mode": "local", "provider": "local", "providers": map[string]any{}},
		})
	case "/v1/user-profile":
		reply(w, http.StatusOK, profileAnswer(raw, model))
	case "/v1/embed":
		var req struct {
			Texts []string `json:"texts"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			f.t.Errorf("decoding /v1/embed: %v", err)
		}
		vectors := make([][]float64, len(req.Texts))
		for i, text := range req.Texts {
			vectors[i] = activityVector(text)
		}
		reply(w, http.StatusOK, map[string]any{"embeddings": vectors, "model": model, "dim": ml.Dim, "provider": "local", "cached": 0})
	case "/v1/compatibility/user-embedding/update":
		var req struct {
			Embedding      []float64 `json:"embedding"`
			Kind           string    `json:"kind"`
			EventEmbedding []float64 `json:"event_embedding"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			f.t.Errorf("decoding the update: %v", err)
		}
		reply(w, http.StatusOK, map[string]any{"embedding": movingAverage(req.Embedding, req.EventEmbedding), "kind": req.Kind})
	default:
		reply(w, http.StatusNotFound, map[string]any{"detail": "Not Found"})
	}
}

func reply(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		panic(err)
	}
}

func (f *fakeML) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeML) set(path string, down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down[path] = down
}

func (f *fakeML) setModel(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.model = model
}

func (f *fakeML) lastBody(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	bodies := f.bodies[path]
	if len(bodies) == 0 {
		f.t.Fatalf("no request to %s", path)
	}
	var out map[string]any
	if err := json.Unmarshal(bodies[len(bodies)-1], &out); err != nil {
		f.t.Fatalf("decoding the last %s request: %v", path, err)
	}
	return out
}

// profileAnswer: an empty request is an empty profile (zero vectors); a request with any rating
// of 1–2 stars or a rated stop with 1–2 stars has dislikes.
func profileAnswer(raw []byte, model string) map[string]any {
	var req struct {
		Ratings     map[string]int   `json:"ratings"`
		Company     string           `json:"company"`
		Interests   []string         `json:"facebook_interests"`
		RatedEvents []map[string]any `json:"rated_events"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		panic(err)
	}
	sum := sha1.Sum(raw)
	hash := "profile-v1:" + hex.EncodeToString(sum[:])
	zeros := make([]float64, ml.Dim)
	if len(req.Ratings) == 0 && req.Company == "" && len(req.Interests) == 0 && len(req.RatedEvents) == 0 {
		return map[string]any{"positive_text": "", "negative_text": "", "positive_embedding": zeros, "negative_embedding": zeros,
			"profile_text_hash": hash, "template_version": "profile-v1", "model": model, "dim": ml.Dim, "provider": "none"}
	}
	dislikes := false
	for _, v := range req.Ratings {
		dislikes = dislikes || v <= 2
	}
	for _, e := range req.RatedEvents {
		dislikes = dislikes || e["stars"].(float64) <= 2
	}
	index := (int(sum[0])<<8 | int(sum[1])) % (ml.Dim - 1)
	negative, negativeText := zeros, ""
	if dislikes {
		negative, negativeText = basis(index+1), "Interests:\n- nightlife"
	}
	return map[string]any{"positive_text": "Interests:\n- outdoor recreation", "negative_text": negativeText,
		"positive_embedding": basis(index), "negative_embedding": negative,
		"profile_text_hash": hash, "template_version": "profile-v1", "model": model, "dim": ml.Dim, "provider": "local"}
}

func activityVector(text string) []float64 { return basis(100 + len(text)%500) }

func basis(i int) []float64 {
	v := make([]float64, ml.Dim)
	v[i] = 1
	return v
}

func movingAverage(current, event []float64) []float64 {
	out := make([]float64, len(current))
	norm := 0.0
	for i := range out {
		out[i] = 0.8*current[i] + 0.2*event[i]
		norm += out[i] * out[i]
	}
	if norm == 0 {
		return out
	}
	for i := range out {
		out[i] /= math.Sqrt(norm)
	}
	return out
}

// ---- fixture ------------------------------------------------------------------------------------

type fixture struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	ml  *fakeML
	svc *profiles.Service
}

var userSeq atomic.Int64

func newFixture(t *testing.T) *fixture {
	now := func() time.Time { return testutil.Fixed }
	st := testutil.Store(t, now)
	fake := newFakeML(t)
	opts := ml.DefaultOptions()
	opts.BaseURL = fake.srv.URL
	opts.HealthTTL = time.Nanosecond // every refresh sees the fake's current /healthz
	return &fixture{t: t, ctx: context.Background(), st: st, ml: fake, svc: profiles.New(st, ml.NewClientWithOptions(opts), now)}
}

func (fx *fixture) user(prefs models.UserPrefs, facebookInterests ...string) string {
	fx.t.Helper()
	n := userSeq.Add(1)
	u := &models.User{
		Email: testutil.UniqueEmail("pat"), Name: "Pat", Username: fmt.Sprintf("pat%d", n), Catalog: store.CollDemoActivities,
		Prefs: prefs, FacebookInterests: facebookInterests,
		Embedding: []float64{0.25, 0.75}, // a legacy fabricated vector: a refresh removes it
	}
	if err := fx.st.Users().Create(fx.ctx, u); err != nil {
		fx.t.Fatal(err)
	}
	return u.ID.Hex()
}

func (fx *fixture) activity(catalog string, doc bson.M) string {
	fx.t.Helper()
	id := bson.NewObjectID()
	doc["_id"] = id
	if _, err := fx.st.Collection(catalog).InsertOne(fx.ctx, doc); err != nil {
		fx.t.Fatal(err)
	}
	return id.Hex()
}

func (fx *fixture) rate(userID, activityID string, stars int, tags []string, at time.Time) {
	fx.t.Helper()
	itemID := store.NewID()
	r := models.Rating{ID: models.PairID(userID, itemID), UserID: userID, ItemID: itemID, ItineraryID: "itinerary-1",
		ActivityID: activityID, Stars: stars, Tags: tags, CreatedAt: at, UpdatedAt: at}
	if _, err := fx.st.Collection(store.CollRatings).InsertOne(fx.ctx, r); err != nil {
		fx.t.Fatal(err)
	}
}

func (fx *fixture) load(userID string) *store.ProfileUser {
	fx.t.Helper()
	u, err := fx.st.Profiles().User(fx.ctx, userID)
	if err != nil {
		fx.t.Fatal(err)
	}
	return u
}

func (fx *fixture) raw(catalog, id string) bson.M {
	fx.t.Helper()
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		fx.t.Fatal(err)
	}
	var doc bson.M
	if err := fx.st.Collection(catalog).FindOne(fx.ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		fx.t.Fatal(err)
	}
	return doc
}

func jordanPrefs() models.UserPrefs {
	return models.UserPrefs{
		Ratings: map[string]int{"outdoors": 5, "nightlife": 2, "sports": 0}, Company: "small_group", Pace: "balanced",
		Spend: "under_15", Flexibility: "bit_over_ok", PreferFree: true, Answers: map[string]string{"perfect_afternoon": "A long walk"},
	}
}

func assertUnit(t *testing.T, name string, v []float64) {
	t.Helper()
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	if len(v) != ml.Dim || math.Abs(math.Sqrt(norm)-1) > 1e-6 {
		t.Fatalf("%s: length %d, norm %.6f", name, len(v), math.Sqrt(norm))
	}
}

func assertClose(t *testing.T, name string, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length %d, want %d", name, len(got), len(want))
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-4 {
			t.Fatalf("%s[%d] = %.6f, want %.6f", name, i, got[i], want[i])
		}
	}
}

// ---- Refresh -------------------------------------------------------------------------------------

func TestRefreshStoresTheProfile(t *testing.T) {
	fx := newFixture(t)
	park := fx.activity(store.CollDemoActivities, bson.M{"name": "Anchor Park", "category": "park", "tags": bson.A{"outdoor"}, "embedding": basis(3)})
	user := fx.user(jordanPrefs(), "Hiking")
	fx.rate(user, park, 5, []string{"Great people"}, testutil.Fixed.Add(-time.Hour))
	fx.rate(user, "", 2, []string{"Too crowded"}, testutil.Fixed.Add(-2*time.Hour))

	if err := fx.svc.Refresh(fx.ctx, user); err != nil {
		t.Fatal(err)
	}
	u := fx.load(user)
	if u.PositiveText != "Interests:\n- outdoor recreation" || u.NegativeText != "Interests:\n- nightlife" || u.EmbeddingModel != ml.Model {
		t.Fatalf("texts/model: %q %q %q", u.PositiveText, u.NegativeText, u.EmbeddingModel)
	}
	assertUnit(t, "positiveEmbedding", u.PositiveEmbedding)
	assertUnit(t, "negativeEmbedding", u.NegativeEmbedding)
	if !strings.HasPrefix(u.ProfileTextHash, "profile-v1:") || !strings.HasPrefix(u.ProfileInputHash, "in1:") {
		t.Fatalf("hashes: %q %q", u.ProfileTextHash, u.ProfileInputHash)
	}
	if u.ProfileUpdatedAt == nil || !u.ProfileUpdatedAt.Equal(testutil.Fixed) {
		t.Fatalf("profileUpdatedAt = %v", u.ProfileUpdatedAt)
	}
	if u.Embedding != nil {
		t.Fatalf("the legacy embedding must be removed: %v", u.Embedding)
	}

	body := fx.ml.lastBody("/v1/user-profile")
	want := map[string]any{
		"ratings": map[string]any{"outdoors": 5.0, "nightlife": 2.0}, "company": "small_group", "pace": "balanced",
		"spend": "under_15", "flexibility": "bit_over_ok", "prefer_free": true,
		"answers":            map[string]any{"perfect_afternoon": "A long walk"},
		"facebook_interests": []any{"Hiking"},
		"rated_events": []any{
			map[string]any{"stars": 5.0, "tags": []any{"Great people"}, "category": "park", "activity_tags": []any{"outdoor"}},
			map[string]any{"stars": 2.0, "tags": []any{"Too crowded"}},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("request:\n got %v\nwant %v", body, want)
	}
}

func TestRefreshSkipsACurrentProfile(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(jordanPrefs())
	for i := 0; i < 2; i++ {
		if err := fx.svc.Refresh(fx.ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	if n := fx.ml.count("/v1/user-profile"); n != 1 {
		t.Fatalf("a current profile is not rebuilt: %d calls", n)
	}

	steps := []struct {
		name   string
		change func()
	}{
		{"preferences changed", func() {
			if _, err := fx.st.Users().Update(fx.ctx, user, bson.M{"prefs.ratings.food": 5}); err != nil {
				t.Fatal(err)
			}
		}},
		{"a new rating", func() { fx.rate(user, "", 4, []string{"Would go again"}, testutil.Fixed) }},
		{"Facebook interests imported", func() {
			if _, err := fx.st.Users().Update(fx.ctx, user, bson.M{"facebookInterests": []string{"Indie rock"}}); err != nil {
				t.Fatal(err)
			}
		}},
		{"the service now embeds with another model", func() { fx.ml.setModel("Qwen/Qwen3-Embedding-4B") }},
	}
	for i, step := range steps {
		step.change()
		if err := fx.svc.Refresh(fx.ctx, user); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if n := fx.ml.count("/v1/user-profile"); n != i+2 {
			t.Fatalf("%s: %d calls, want %d", step.name, n, i+2)
		}
	}
	if err := fx.svc.Refresh(fx.ctx, user); err != nil || fx.ml.count("/v1/user-profile") != len(steps)+1 {
		t.Fatalf("current again: %v, %d calls", err, fx.ml.count("/v1/user-profile"))
	}
}

func TestRefreshWhenTheServiceIsDown(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(jordanPrefs())
	if err := fx.svc.Refresh(fx.ctx, user); err != nil {
		t.Fatal(err)
	}
	before := fx.load(user)

	fx.ml.set("*", true)
	if err := fx.svc.Refresh(fx.ctx, user); err != nil {
		t.Fatalf("nothing changed, so a down service is not asked: %v", err)
	}

	if _, err := fx.st.Users().Update(fx.ctx, user, bson.M{"prefs.pace": "packed"}); err != nil {
		t.Fatal(err)
	}
	err := fx.svc.Refresh(fx.ctx, user)
	if !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	after := fx.load(user)
	if after.ProfileTextHash != "" || after.ProfileInputHash != "" {
		t.Fatalf("the hashes are cleared: %q %q", after.ProfileTextHash, after.ProfileInputHash)
	}
	if !reflect.DeepEqual(after.PositiveEmbedding, before.PositiveEmbedding) || !reflect.DeepEqual(after.NegativeEmbedding, before.NegativeEmbedding) ||
		after.PositiveText != before.PositiveText {
		t.Fatal("the old vectors and texts are kept")
	}
	if err := fx.svc.Refresh(fx.ctx, user); !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("a cleared profile is retried: %v", err)
	}

	fx.ml.set("*", false)
	if err := fx.svc.Refresh(fx.ctx, user); err != nil {
		t.Fatal(err)
	}
	if u := fx.load(user); !strings.HasPrefix(u.ProfileTextHash, "profile-v1:") || u.ProfileInputHash == "" {
		t.Fatalf("rebuilt once the service is back: %q", u.ProfileTextHash)
	}
}

func TestRefreshStoresTheZeroVectorForAnEmptyProfile(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{})
	for i := 0; i < 2; i++ {
		if err := fx.svc.Refresh(fx.ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	u := fx.load(user)
	if len(u.PositiveEmbedding) != ml.Dim || !ml.IsZero(u.PositiveEmbedding) || u.PositiveText != "" || u.ProfileTextHash == "" {
		t.Fatalf("empty profile: %d values, text %q, hash %q", len(u.PositiveEmbedding), u.PositiveText, u.ProfileTextHash)
	}
	if n := fx.ml.count("/v1/user-profile"); n != 1 {
		t.Fatalf("an empty profile is stored, not rebuilt on every call: %d calls", n)
	}
}

func TestRefreshUnknownUser(t *testing.T) {
	fx := newFixture(t)
	if err := fx.svc.Refresh(fx.ctx, bson.NewObjectID().Hex()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// ---- Rated ---------------------------------------------------------------------------------------

func TestRatedMovesTheVectors(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{Ratings: map[string]int{"outdoors": 5}, Company: "solo"})
	if err := fx.svc.Refresh(fx.ctx, user); err != nil {
		t.Fatal(err)
	}
	start := fx.load(user)
	if !ml.IsZero(start.NegativeEmbedding) {
		t.Fatal("no dislikes yet")
	}
	park := fx.activity(store.CollDemoActivities, bson.M{"category": "park", "embedding": basis(7), "embeddingText": "Interests:\n- parks"})

	if err := fx.svc.Rated(fx.ctx, user, park, 5); err != nil {
		t.Fatal(err)
	}
	liked := fx.load(user)
	assertClose(t, "positive after 5 stars", liked.PositiveEmbedding, movingAverage(start.PositiveEmbedding, basis(7)))
	assertClose(t, "negative after 5 stars", liked.NegativeEmbedding, start.NegativeEmbedding)
	if body := fx.ml.lastBody("/v1/compatibility/user-embedding/update"); body["kind"] != "positive" {
		t.Fatalf("kind = %v", body["kind"])
	}

	if err := fx.svc.Rated(fx.ctx, user, park, 1); err != nil {
		t.Fatal(err)
	}
	disliked := fx.load(user)
	assertClose(t, "first negative vector", disliked.NegativeEmbedding, basis(7))
	assertClose(t, "positive after 1 star", disliked.PositiveEmbedding, liked.PositiveEmbedding)

	calls := fx.ml.count("/v1/compatibility/user-embedding/update")
	for _, stars := range []int{3, 0, 6} {
		if err := fx.svc.Rated(fx.ctx, user, park, stars); err != nil {
			t.Fatalf("%d stars: %v", stars, err)
		}
	}
	if fx.ml.count("/v1/compatibility/user-embedding/update") != calls {
		t.Fatal("3 stars (and out-of-range stars) change nothing")
	}
	if u := fx.load(user); !reflect.DeepEqual(u.PositiveEmbedding, disliked.PositiveEmbedding) || !reflect.DeepEqual(u.NegativeEmbedding, disliked.NegativeEmbedding) {
		t.Fatal("vectors moved on a neutral rating")
	}
}

func TestRatedEmbedsAnActivityOnDemandOnce(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{Company: "solo"})
	text := "Interests:\n- live jazz\n\nCost:\n- free admission"
	market := fx.activity(store.CollDemoActivities, bson.M{"category": "live_music", "embeddingText": text, "embeddingTextHash": ml.TextHash(text)})

	if err := fx.svc.Rated(fx.ctx, user, market, 4); err != nil {
		t.Fatal(err)
	}
	if fx.ml.count("/v1/embed") != 1 {
		t.Fatalf("/v1/embed calls: %d", fx.ml.count("/v1/embed"))
	}
	oid, err := bson.ObjectIDFromHex(market)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fx.st.Collection(store.CollDemoActivities).FindOne(fx.ctx, bson.M{"_id": oid}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Embedding      []float64 `bson:"embedding"`
		EmbeddingModel string    `bson:"embeddingModel"`
		Meta           struct {
			Model        string    `bson:"model"`
			Dimension    int       `bson:"dimension"`
			Normalized   bool      `bson:"normalized"`
			MaxSeqLength int       `bson:"maxSeqLength"`
			GeneratedAt  time.Time `bson:"generatedAt"`
			TextHash     string    `bson:"textHash"`
			Source       string    `bson:"source"`
			Provider     string    `bson:"provider"`
		} `bson:"embeddingMeta"`
	}
	if err := bson.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Embedding) != ml.Dim || stored.EmbeddingModel != ml.Model {
		t.Fatalf("activity vector not stored: %d values, model %q", len(stored.Embedding), stored.EmbeddingModel)
	}
	meta := stored.Meta
	if meta.Source != ml.SourceOnDemand || meta.TextHash != ml.TextHash(text) || meta.Model != ml.Model || meta.Dimension != ml.Dim ||
		!meta.Normalized || meta.MaxSeqLength != 512 || meta.Provider != "local" || !meta.GeneratedAt.Equal(testutil.Fixed) {
		t.Fatalf("embeddingMeta = %+v", meta)
	}
	if prompt, err := raw.LookupErr("embeddingMeta", "prompt"); err != nil || prompt.Type != bson.TypeNull {
		t.Fatalf("embeddingMeta.prompt must be an explicit null (no instruction prefix): %v %v", prompt, err)
	}
	assertClose(t, "first positive vector", fx.load(user).PositiveEmbedding, activityVector(text))

	if err := fx.svc.Rated(fx.ctx, user, market, 5); err != nil {
		t.Fatal(err)
	}
	if fx.ml.count("/v1/embed") != 1 {
		t.Fatal("the stored vector is reused, not embedded again")
	}
}

func TestRatedDoesNotStoreAVectorForAChangedText(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{Company: "solo"})
	activity := fx.activity(store.CollDemoActivities, bson.M{"embeddingText": "Interests:\n- opera", "embeddingTextHash": "0123456789abcdef0123456789abcdef01234567"})
	if err := fx.svc.Rated(fx.ctx, user, activity, 5); err != nil {
		t.Fatal(err)
	}
	if _, ok := fx.raw(store.CollDemoActivities, activity)["embedding"]; ok {
		t.Fatal("a text whose hash no longer matches must not get a vector")
	}
	if u := fx.load(user); !ml.Usable(u.PositiveEmbedding, ml.Dim) {
		t.Fatal("the rating still counts")
	}
}

func TestRatedWithoutAUsableActivityIsANoOp(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{Company: "solo"})
	elsewhere := fx.activity(store.CollActivities, bson.M{"category": "park", "embedding": basis(9)}) // not in the user's catalog
	bare := fx.activity(store.CollDemoActivities, bson.M{"category": "park"})
	cases := map[string]string{
		"unknown id":          bson.NewObjectID().Hex(),
		"not an object id":    "stop_42",
		"other catalog":       elsewhere,
		"no vector, no text":  bare,
		"no activity at all:": "",
	}
	for name, id := range cases {
		if err := fx.svc.Rated(fx.ctx, user, id, 5); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, path := range []string{"/v1/embed", "/v1/compatibility/user-embedding/update"} {
		if n := fx.ml.count(path); n != 0 {
			t.Fatalf("%s called %d times", path, n)
		}
	}
	if u := fx.load(user); u.PositiveEmbedding != nil || u.NegativeEmbedding != nil {
		t.Fatal("no vector may appear")
	}
}

func TestRatedWhenTheServiceIsDown(t *testing.T) {
	fx := newFixture(t)
	user := fx.user(models.UserPrefs{Company: "solo"})
	park := fx.activity(store.CollDemoActivities, bson.M{"category": "park", "embedding": basis(7)})
	fx.ml.set("/v1/compatibility/user-embedding/update", true)
	if err := fx.svc.Rated(fx.ctx, user, park, 5); !errors.Is(err, ml.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if u := fx.load(user); u.PositiveEmbedding != nil {
		t.Fatal("nothing is written when the update fails")
	}
}

func TestWithoutAnMLClient(t *testing.T) {
	st := testutil.Store(t, nil)
	svc := profiles.New(st, nil, nil)
	if err := svc.Refresh(context.Background(), bson.NewObjectID().Hex()); err == nil {
		t.Fatal("a refresh without a client is an error the caller logs")
	}
	if err := svc.Rated(context.Background(), bson.NewObjectID().Hex(), "x", 3); err != nil {
		t.Fatalf("a neutral rating needs no client: %v", err)
	}
}
