package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestProfilesRatedStopsJoinTheCatalogNewestFirst(t *testing.T) {
	ctx := context.Background()
	st := testutil.Store(t, func() time.Time { return testutil.Fixed })
	park := bson.NewObjectID()
	if _, err := st.Collection(store.CollDemoActivities).InsertMany(ctx, []any{
		bson.M{"_id": park, "category": "park", "tags": bson.A{"outdoor"}},
		bson.M{"_id": "string-id", "category": "bar", "tags": bson.A{"late_night"}},
	}); err != nil {
		t.Fatal(err)
	}
	rate := func(user, activity string, stars int, at time.Time) {
		item := store.NewID()
		r := models.Rating{ID: models.PairID(user, item), UserID: user, ItemID: item, ActivityID: activity, Stars: stars,
			Tags: []string{"t"}, CreatedAt: at.Add(-time.Hour), UpdatedAt: at}
		if _, err := st.Collection(store.CollRatings).InsertOne(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	rate("u1", park.Hex(), 5, testutil.Fixed.Add(-3*time.Hour))
	rate("u1", "string-id", 1, testutil.Fixed.Add(-1*time.Hour))
	rate("u1", "", 4, testutil.Fixed.Add(-2*time.Hour))
	rate("u1", bson.NewObjectID().Hex(), 2, testutil.Fixed.Add(-4*time.Hour)) // not in the catalog
	rate("u2", park.Hex(), 3, testutil.Fixed)

	stops, err := st.Profiles().RatedStops(ctx, "u1", store.CollDemoActivities, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := make([][3]any, len(stops))
	for i, s := range stops {
		got[i] = [3]any{s.Stars, s.Category, s.ActivityTags}
	}
	want := [][3]any{{1, "bar", []string{"late_night"}}, {4, "", []string(nil)}, {5, "park", []string{"outdoor"}}, {2, "", []string(nil)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stops:\n got %v\nwant %v", got, want)
	}
	if limited, err := st.Profiles().RatedStops(ctx, "u1", "", 2); err != nil || len(limited) != 2 || limited[0].Category != "" {
		t.Fatalf("limit and the default catalog: %v %+v", err, limited)
	}
	if _, err := st.Profiles().RatedStops(ctx, "u1", "bogus", 0); err == nil {
		t.Fatal("an unknown catalog is an error")
	}
}

func TestProfilesActivityEmbeddingGuard(t *testing.T) {
	ctx := context.Background()
	st := testutil.Store(t, nil)
	coll := st.Collection(store.CollActivities)
	ids := map[string]bson.ObjectID{}
	for name, doc := range map[string]bson.M{
		"missing": {"embeddingTextHash": "h1"},
		"stale":   {"embeddingTextHash": "h1", "embedding": bson.A{0.5}, "embeddingMeta": bson.M{"textHash": "h0"}},
		"current": {"embeddingTextHash": "h1", "embedding": bson.A{0.5}, "embeddingMeta": bson.M{"textHash": "h1"}},
		"foreign": {"embeddingTextHash": "h1", "embedding": bson.A{0.5}},
		"changed": {"embeddingTextHash": "h2"},
	} {
		ids[name] = bson.NewObjectID()
		doc["_id"] = ids[name]
		if _, err := coll.InsertOne(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]bool{"missing": true, "stale": true, "current": false, "foreign": false, "changed": false}
	for name, wrote := range want {
		got, err := st.Profiles().SetActivityEmbedding(ctx, "", ids[name].Hex(), "h1", []float64{1, 0}, "m", bson.M{"textHash": "h1", "source": "go_on_demand"})
		if err != nil || got != wrote {
			t.Errorf("%s: wrote=%v err=%v, want %v", name, got, err, wrote)
		}
	}
	activity, err := st.Profiles().Activity(ctx, store.CollActivities, ids["missing"].Hex())
	if err != nil || !reflect.DeepEqual(activity.Embedding, []float64{1, 0}) || activity.EmbeddingModel != "m" || activity.ID != ids["missing"].Hex() {
		t.Fatalf("activity after the write: %+v %v", activity, err)
	}
	if _, err := st.Profiles().Activity(ctx, store.CollActivities, bson.NewObjectID().Hex()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing activity: %v", err)
	}
}

func TestProfilesUserWrites(t *testing.T) {
	ctx := context.Background()
	st := testutil.Store(t, func() time.Time { return testutil.Fixed })
	user := &models.User{Email: testutil.UniqueEmail("ada"), Name: "Ada", Username: "ada_profiles", Embedding: []float64{0.3}}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	id := user.ID.Hex()
	fields := store.ProfileFields{PositiveText: "p", NegativeText: "", PositiveEmbedding: []float64{1, 0}, NegativeEmbedding: []float64{0, 0},
		EmbeddingModel: "m", ProfileTextHash: "profile-v1:x", ProfileInputHash: "in1:y"}
	if err := st.Profiles().SetProfile(ctx, id, fields); err != nil {
		t.Fatal(err)
	}
	u, err := st.Profiles().User(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.PositiveText != "p" || u.ProfileTextHash != "profile-v1:x" || u.ProfileInputHash != "in1:y" || u.EmbeddingModel != "m" ||
		!reflect.DeepEqual(u.NegativeEmbedding, []float64{0, 0}) || u.Embedding != nil || u.ProfileUpdatedAt == nil || u.Email != user.Email {
		t.Fatalf("user after SetProfile: %+v", u)
	}
	if err := st.Profiles().SetUserVector(ctx, id, true, []float64{0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.Profiles().ClearProfileHash(ctx, id); err != nil {
		t.Fatal(err)
	}
	if u, _ = st.Profiles().User(ctx, id); !reflect.DeepEqual(u.NegativeEmbedding, []float64{0, 1}) || u.ProfileTextHash != "" || u.ProfileInputHash != "" ||
		!reflect.DeepEqual(u.PositiveEmbedding, []float64{1, 0}) {
		t.Fatalf("after SetUserVector and ClearProfileHash: %+v", u)
	}
	missing := bson.NewObjectID().Hex()
	for name, err := range map[string]error{
		"User":             func() error { _, err := st.Profiles().User(ctx, missing); return err }(),
		"SetProfile":       st.Profiles().SetProfile(ctx, missing, fields),
		"SetUserVector":    st.Profiles().SetUserVector(ctx, "not-hex", false, []float64{1}),
		"ClearProfileHash": st.Profiles().ClearProfileHash(ctx, missing),
	} {
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s on a missing user: %v", name, err)
		}
	}
}
