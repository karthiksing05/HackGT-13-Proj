package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// realPerson is an account like the real ones: setup done, liked
// categories saved, taste vectors built, no ratings.
func realPerson(t *testing.T, st *store.Store, name, handle string, ratings map[string]int) *models.User {
	t.Helper()
	r := rand.New(rand.NewPCG(uint64(len(handle)), 7))
	vec := make([]float64, ml.Dim)
	for i := range vec {
		vec[i] = r.NormFloat64()
	}
	unit(vec)
	now := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Millisecond)
	u := &models.User{Email: handle + "@example.test", PasswordHash: "their-hash", Name: name, Username: handle,
		AvatarColor: "ink", Status: "open", SetupComplete: true, City: "atlanta",
		Prefs: models.UserPrefs{Ratings: ratings, Company: "small_group", Pace: "balanced", Spend: "under_15",
			Flexibility: "bit_over_ok", SplitStyle: "equally", PreferFree: true, Answers: map[string]string{"perfect_afternoon": "Anything outside"},
			InstantCheckoutLimitCents: 5000},
		Taste:             models.UserTaste{Tags: map[string]float64{}},
		PositiveText:      "Interests:\n- things they said",
		PositiveEmbedding: vec, NegativeEmbedding: make([]float64, ml.Dim), EmbeddingModel: ml.Model,
		ProfileTextHash: "profile-v1:theirs", ProfileUpdatedAt: &now}
	if err := st.Users().Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

// threePeople are the three accounts of the real run, shaped like theirs.
func threePeople(t *testing.T, st *store.Store) (karthik, bayan, jev *models.User) {
	karthik = realPerson(t, st, "Karthik Singaravadivelan", "lilk", map[string]int{"outdoors": 4, "food": 5, "live_music": 3})
	bayan = realPerson(t, st, "Bayan", "bayan_98d1", map[string]int{"nightlife": 4, "live_music": 5, "sports": 4})
	jev = realPerson(t, st, "Jev", "jev_d9ef", map[string]int{"museums": 4, "shopping": 2, "long_walks": 3})
	return
}

func historyOpts(t *testing.T, now time.Time, list string) Options {
	t.Helper()
	o := testOpts(t, now)
	sels, err := parseHistory(list)
	if err != nil {
		t.Fatal(err)
	}
	o.History = sels
	return o
}

func tagged(t *testing.T, st *store.Store, tag string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, coll := range []string{store.CollItineraries, store.CollRatings, collBackups, store.CollUsers} {
		n, err := st.Collection(coll).CountDocuments(context.Background(), bson.M{fieldSeed: tag})
		if err != nil {
			t.Fatal(err)
		}
		out[coll] = n
	}
	return out
}

// withShowcase writes the Atlanta showcase first, so past plans can take
// its people along.
func withShowcase(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	o := testOpts(t, now)
	o.Apply, o.Worlds = true, []string{worldAtlanta}
	seed(t, st, o)
}

const everyone = "@lilk,@bayan_98d1,@jev_d9ef"

func TestHistoryDryRunWritesNothing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogsWithin(t, st, 8)
	withShowcase(t, st, now)
	threePeople(t, st)
	before := everything(t, st)
	out := seed(t, st, historyOpts(t, now, everyone))
	if after := everything(t, st); !equalCounts(before, after) {
		t.Fatalf("a history dry run wrote: before %v, after %v", before, after)
	}
	for _, want := range []string{"@lilk · Karthik Singaravadivelan", "Story: outdoors", "@bayan_98d1 · Bayan", "Story: nightlife",
		"@jev_d9ef · Jev", "Story: arts", "Past sidequests: 5 new", "left to rate", "Interests: outdoors 4 → 5", "museums 4 → 5",
		"Dry run: nothing was written"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

func TestHistoryApplyTwiceAndRemove(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogsWithin(t, st, 8)
	withShowcase(t, st, now)
	karthik, bayan, jev := threePeople(t, st)
	// Karthik's own data: a plan of his with a rated stop, and a friend.
	own := &models.Itinerary{HostID: karthik.ID.Hex(), Title: "Karthik's own plan", Visibility: models.VisibilityJustMe,
		Start: now.Add(-72 * time.Hour), BackBy: now.Add(-70 * time.Hour), Status: models.ItineraryPast,
		Items: []models.ItineraryItem{{ID: "own-stop", Kind: models.ItemStop, Title: "His stop", Start: now.Add(-72 * time.Hour), End: now.Add(-71 * time.Hour)}}}
	if err := st.Itineraries().Insert(ctx, own); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ratings().Upsert(ctx, models.Rating{UserID: karthik.ID.Hex(), ItemID: "own-stop", ItineraryID: own.ID, Stars: 5}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Friends().Befriend(ctx, karthik.ID.Hex(), bayan.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	accounts := map[string][]byte{}
	for _, u := range []*models.User{karthik, bayan, jev} {
		accounts[u.Username] = rawUser(t, st, u.ID)
	}
	showcase := tagged(t, st, seedTag)

	o := historyOpts(t, now, everyone)
	o.Apply = true
	seed(t, st, o)
	first := tagged(t, st, historyTag)
	if first[store.CollItineraries] != 15 || first[collBackups] != 3 || first[store.CollUsers] != 0 {
		t.Fatalf("first run wrote %v", first)
	}
	for _, u := range []*models.User{karthik, bayan, jev} {
		uid := u.ID.Hex()
		stops, err := st.Itineraries().PastStops(ctx, store.PastStopsQuery{UserID: uid, Now: now, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		ratings, err := st.Collection(store.CollRatings).CountDocuments(ctx, bson.M{fieldSeed: historyTag, "userId": uid})
		if err != nil {
			t.Fatal(err)
		}
		if ratings == 0 || int64(len(stops))-ratings < minUnrated {
			t.Errorf("@%s: %d past stops, %d rated", u.Username, len(stops), ratings)
		}
	}
	var plans []models.Itinerary
	if err := findAll(ctx, st, store.CollItineraries, bson.M{fieldSeed: historyTag}, bson.M{}, &plans); err != nil {
		t.Fatal(err)
	}
	for _, it := range plans {
		if it.Status != models.ItineraryPast || !it.BackBy.Before(now) || !it.IsMember(it.HostID) || len(it.Items) < 1 {
			t.Errorf("%s: status %s, back by %v, members %v", it.Title, it.Status, it.BackBy, it.MemberIDs)
		}
		if age := now.Sub(it.Start); age > 22*24*time.Hour {
			t.Errorf("%s is %v old", it.Title, age)
		}
	}
	firstPlans := readAll(t, st.Collection(store.CollItineraries), bson.M{fieldSeed: historyTag})
	var k models.User
	if err := st.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": karthik.ID}).Decode(&k); err != nil {
		t.Fatal(err)
	}
	if k.Prefs.Ratings["outdoors"] != 5 || k.Prefs.Ratings["food"] != 5 || k.Prefs.Ratings["live_music"] != 3 || k.Prefs.Ratings["long_walks"] != 4 {
		t.Errorf("Karthik's merged interests: %v (outdoors raised, food and live_music kept, long_walks filled)", k.Prefs.Ratings)
	}
	if k.Taste.RatingCount == 0 || len(k.Taste.Tags) == 0 {
		t.Errorf("the ratings did not move Karthik's taste tags: %+v", k.Taste)
	}

	// Again: the same documents, the ratings do not step the taste twice,
	// and the originals stay the first ones.
	o.Now = now.Add(time.Hour)
	seed(t, st, o)
	if again := tagged(t, st, historyTag); !equalCounts(again, first) {
		t.Fatalf("second run: %v, first %v", again, first)
	}
	againPlans := readAll(t, st.Collection(store.CollItineraries), bson.M{fieldSeed: historyTag})
	if len(againPlans) != len(firstPlans) {
		t.Fatalf("plans: %d then %d", len(firstPlans), len(againPlans))
	}
	var k2 models.User
	if err := st.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": karthik.ID}).Decode(&k2); err != nil {
		t.Fatal(err)
	}
	if k2.Taste.RatingCount != k.Taste.RatingCount || !mapsEqual(k2.Taste.Tags, k.Taste.Tags) {
		t.Errorf("the rerun moved the taste again: %+v, then %+v", k.Taste, k2.Taste)
	}
	// Another story over an existing history is refused.
	other := historyOpts(t, now.Add(time.Hour), "@lilk=arts")
	other.Apply = true
	if err := Run(ctx, st, other, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "already has the outdoors history") {
		t.Errorf("another story over a history: %v", err)
	}
	b, err := loadBackup(ctx, st, karthik.ID.Hex())
	if err != nil || b == nil {
		t.Fatalf("backup: %v", err)
	}
	if orig, _ := b.Original.LookupErr("prefs"); !bytes.Equal(orig.Value, bson.Raw(accounts["lilk"]).Lookup("prefs").Value) {
		t.Error("the rerun overwrote the saved originals")
	}

	// Remove: the history goes, the accounts are byte for byte what they were.
	o.Remove = true
	seed(t, st, o)
	if left := tagged(t, st, historyTag); left[store.CollItineraries]+left[store.CollRatings]+left[collBackups] != 0 {
		t.Fatalf("left after remove: %v", left)
	}
	for _, u := range []*models.User{karthik, bayan, jev} {
		if got := rawUser(t, st, u.ID); !bytes.Equal(got, accounts[u.Username]) {
			t.Errorf("@%s is not restored byte for byte", u.Username)
		}
	}
	if _, err := st.Itineraries().Get(ctx, own.ID); err != nil {
		t.Errorf("Karthik's own plan: %v", err)
	}
	if r, err := st.Ratings().ForUser(ctx, karthik.ID.Hex(), []string{"own-stop"}); err != nil || r["own-stop"] == nil {
		t.Errorf("Karthik's own rating: %v %v", r, err)
	}
	if _, err := st.Friends().Get(ctx, karthik.ID.Hex(), bayan.ID.Hex()); err != nil {
		t.Errorf("Karthik and Bayan's friendship: %v", err)
	}
	if got := tagged(t, st, seedTag); !equalCounts(got, showcase) {
		t.Errorf("the showcase changed: %v, was %v", got, showcase)
	}
}

func mapsEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || math.Abs(v-w) > 1e-12 {
			return false
		}
	}
	return true
}

func TestHistoryRemoveKeepsWhatTheyChangedSince(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogsWithin(t, st, 8)
	karthik, _, _ := threePeople(t, st)
	before := rawUser(t, st, karthik.ID)
	o := historyOpts(t, now, "@lilk")
	o.Apply = true
	seed(t, st, o)
	// After the seed Karthik lowers outdoors himself and rates something.
	if _, err := st.Users().Update(ctx, karthik.ID.Hex(), bson.M{"prefs.ratings.outdoors": 2}); err != nil {
		t.Fatal(err)
	}
	if err := st.Users().BumpTaste(ctx, karthik.ID.Hex(), map[string]float64{"food": 1}, true); err != nil {
		t.Fatal(err)
	}
	o.Remove = true
	out := seed(t, st, o)
	var k models.User
	if err := st.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": karthik.ID}).Decode(&k); err != nil {
		t.Fatal(err)
	}
	if k.Prefs.Ratings["outdoors"] != 2 {
		t.Errorf("his own change was undone: outdoors %d", k.Prefs.Ratings["outdoors"])
	}
	if _, filled := k.Prefs.Ratings["long_walks"]; filled || k.Prefs.Ratings["food"] != 5 {
		t.Errorf("the seed's keys should go back, his stay: %v", k.Prefs.Ratings)
	}
	if !strings.Contains(out, "changed by them, kept: outdoors") || !strings.Contains(out, "taste tags: changed since the seed") {
		t.Errorf("the removal should say what it kept:\n%s", out)
	}
	orig := bson.Raw(before)
	if !bytes.Equal(bson.Raw(rawUser(t, st, karthik.ID)).Lookup("positiveEmbedding").Value, orig.Lookup("positiveEmbedding").Value) {
		t.Error("the taste vectors were not restored")
	}
}

func TestHistoryOverHTTP(t *testing.T) {
	srv := testutil.New(t)
	ctx := context.Background()
	catalogsWithin(t, srv.Store, 8)
	srv.Deps.ML = fakeML(t)
	withShowcase(t, srv.Store, srv.Clock.Now())
	jevSession := srv.SignupWith(t, contract.SignupRequest{Name: "Jev", Email: "jev@example.test", Password: testutil.Password,
		Username: testutil.Ptr("jev_d9ef")})
	srv.Do(t, "PUT", "/me/preferences", contract.Preferences{Ratings: contract.Ratings{"museums": 4, "shopping": 2, "long_walks": 3}},
		jevSession).Expect(t, http.StatusOK)
	jevID, _ := bson.ObjectIDFromHex(jevSession.UserID)
	accountBefore := rawUser(t, srv.Store, jevID)
	tasteBefore := decode[contract.TasteProfile](t, srv.Do(t, "GET", "/me/taste-profile", nil, jevSession).Expect(t, http.StatusOK))

	o := historyOpts(t, srv.Clock.Now(), "@jev_d9ef")
	o.Apply, o.ML, o.MLURL = true, fakeML(t), "the fake ML service"
	seed(t, srv.Store, o)

	past := decode[contract.Page[contract.PastEvent]](t, srv.Do(t, "GET", "/me/past-events?unrated=false", nil, jevSession).Expect(t, http.StatusOK))
	toRate := decode[contract.Page[contract.PastEvent]](t, srv.Do(t, "GET", "/me/past-events?unrated=true", nil, jevSession).Expect(t, http.StatusOK))
	rated, company := 0, 0
	for _, e := range past.Items {
		if e.Rating != nil {
			rated++
			if e.Rating.Stars < 3 || e.Rating.Stars > 5 || len(e.Rating.Tags) > 12 {
				t.Errorf("rating of %s: %+v", e.Title, e.Rating)
			}
		}
		if e.Company != "solo" {
			company++
		}
	}
	if len(toRate.Items) < minUnrated || rated == 0 || len(past.Items) != rated+len(toRate.Items) || company == 0 {
		t.Fatalf("Past: %d stops, %d rated, %d to rate, %d with company", len(past.Items), rated, len(toRate.Items), company)
	}
	history := decode[[]contract.Itinerary](t, srv.Do(t, "GET", "/itineraries?status=past", nil, jevSession).Expect(t, http.StatusOK))
	if len(history) < 4 || len(history) > 6 {
		t.Errorf("%d past sidequests", len(history))
	}
	insights := decode[contract.PastInsights](t, srv.Do(t, "GET", "/me/insights", nil, jevSession).Expect(t, http.StatusOK))
	if insights.BasedOn != rated || insights.Headline == "" || strings.HasPrefix(insights.Headline, "Rate a few") || len(insights.Highlights) == 0 {
		t.Errorf("insights: %+v (rated %d)", insights, rated)
	}
	tasteAfter := decode[contract.TasteProfile](t, srv.Do(t, "GET", "/me/taste-profile", nil, jevSession).Expect(t, http.StatusOK))
	if bar(tasteAfter, "Art") <= bar(tasteBefore, "Art") {
		t.Errorf("the Art bar did not rise: %v → %v", tasteBefore, tasteAfter)
	}
	prefs := decode[contract.Preferences](t, srv.Do(t, "GET", "/me/preferences", nil, jevSession).Expect(t, http.StatusOK))
	if prefs.Ratings["museums"] != 5 || prefs.Ratings["shopping"] != 3 || prefs.Ratings["long_walks"] != 4 {
		t.Errorf("merged interests: %v", prefs.Ratings)
	}
	var stored storedProfile
	if err := srv.Store.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": jevID}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.ProfileTextHash != "profile-v1:fake" || len(stored.PositiveEmbedding) != ml.Dim {
		t.Errorf("the taste vectors were not rebuilt through the ML service: %q", stored.ProfileTextHash)
	}
	// Someone else sees the past on Jev's profile.
	other := srv.Signup(t, "Other Person")
	profile := decode[contract.PublicProfile](t, srv.Do(t, "GET", "/users/"+jevSession.UserID+"/profile", nil, other).Expect(t, http.StatusOK))
	if profile.SidequestsDone != len(history) {
		t.Errorf("sidequests done %d, want %d", profile.SidequestsDone, len(history))
	}

	o.Remove = true
	seed(t, srv.Store, o)
	if got := rawUser(t, srv.Store, jevID); !bytes.Equal(got, accountBefore) {
		t.Error("Jev's account is not restored byte for byte")
	}
	past = decode[contract.Page[contract.PastEvent]](t, srv.Do(t, "GET", "/me/past-events", nil, jevSession).Expect(t, http.StatusOK))
	if len(past.Items) != 0 {
		t.Errorf("Past after remove: %+v", past.Items)
	}
}

func bar(p contract.TasteProfile, label string) float64 {
	for _, b := range p.Bars {
		if b.Label == label {
			return b.Value
		}
	}
	return -1
}

func TestHistoryRefusals(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogsWithin(t, st, 8)
	withShowcase(t, st, now)
	threePeople(t, st)
	makeSandy(t, st)
	bot := &models.User{Email: "bot@example.test", Name: "Some Bot", Username: "somebot", Roles: []string{"bot"}}
	if err := st.Users().Create(context.Background(), bot); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ list, want string }{
		{"@lilk,@nobody", "@nobody matches 0 accounts"},
		{"@sandybyte", "@sandybyte is in the demo cast"},
		{"@somebot", "@somebot is in the demo cast"},
		{"@hana.kim", "@hana.kim is a seeded showcase account"},
	} {
		o := historyOpts(t, now, c.list)
		o.Apply = true
		var buf bytes.Buffer
		if err := Run(context.Background(), st, o, &buf); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.list, err, c.want)
		}
	}
	if n := tagged(t, st, historyTag); n[store.CollItineraries]+n[collBackups]+n[store.CollRatings] != 0 {
		t.Fatalf("a refused run wrote %v", n)
	}
	for _, bad := range []string{"@lilk,@lilk", "@lilk=beach", ",", "@lilk=arts,@jev_d9ef=arts,@x=" + "nope"} {
		if _, err := parseHistory(bad); err == nil {
			t.Errorf("parseHistory(%q) accepted", bad)
		}
	}
	if sels, err := parseHistory("@LilK=arts, bayan_98d1"); err != nil || len(sels) != 2 || sels[0] != (historySel{"lilk", "arts"}) ||
		sels[1] != (historySel{"bayan_98d1", ""}) {
		t.Errorf("parseHistory: %+v %v", sels, err)
	}
	if code := cli([]string{"--history", "@lilk", "--world", "atlanta"}, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Errorf("--history with --world exits %d", code)
	}
	if !slices.Contains(historyFields, "prefs") || !slices.Contains(historyFields, "taste") {
		t.Error("the backup must cover preferences and taste")
	}
}
