package main

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/democlock"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const demoDate = "2026-09-27" // production's DEMO_DATE, a Sunday

// catalogs copies the local freetime catalogs (read only) into st: every
// demo_activities document (Saltlight, the same 100 as production) and, as
// pitch_activities, the Atlanta sample's documents within four miles of
// Midtown. The sample's places are trails that close at 6 PM, so their
// copies get the evening hours the pitch catalog's parks have (6 AM to
// 11 PM); evening plans need something open.
func catalogs(t *testing.T, st *store.Store) { catalogsWithin(t, st, 4) }

// catalogsWithin is catalogs with the Atlanta sample taken within miles of Midtown.
func catalogsWithin(t *testing.T, st *store.Store, miles float64) {
	t.Helper()
	src := st.DB().Client().Database("freetime")
	demo := readAll(t, src.Collection(store.CollDemoActivities), bson.M{})
	near := bson.M{"location": bson.M{"$geoWithin": bson.M{"$centerSphere": bson.A{bson.A{midtown.Lng, midtown.Lat}, miles / 3963.2}}}}
	atl := readAll(t, src.Collection(store.CollActivities), near)
	if len(demo) == 0 || len(atl) == 0 {
		if os.Getenv("CI") == "1" {
			t.Fatal("freetime.demo_activities or freetime.activities is empty; load the local seed")
		}
		t.Skip("freetime.demo_activities or freetime.activities is empty")
	}
	evening := bson.A{}
	for day := range 7 {
		evening = append(evening, bson.M{"open": day*1440 + 6*60, "close": day*1440 + 23*60})
	}
	for _, d := range atl {
		if d["kind"] == "place" {
			d["weeklyHours"] = evening
		}
	}
	insertAll(t, st.Collection(store.CollDemoActivities), demo)
	insertAll(t, st.Collection(store.CollPitchActivities), atl)
}

func readAll(t *testing.T, coll *mongo.Collection, filter bson.M) []bson.M {
	t.Helper()
	cursor, err := coll.Find(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	var docs []bson.M
	if err := cursor.All(context.Background(), &docs); err != nil {
		t.Fatal(err)
	}
	return docs
}

func insertAll(t *testing.T, coll *mongo.Collection, docs []bson.M) {
	t.Helper()
	rows := make([]any, len(docs))
	for i, d := range docs {
		rows[i] = d
	}
	if _, err := coll.InsertMany(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
}

// makeSandy is the demo account as production has it (role demo, Saltlight).
func makeSandy(t *testing.T, st *store.Store) *models.User {
	t.Helper()
	u := &models.User{Email: defaultDemoEmail, PasswordHash: "sandy-hash", Name: "Sandy Byte", Username: "sandybyte",
		AvatarColor: "sage", Status: "open", SetupComplete: true, City: "saltlight",
		HomeBase: &models.HomeBase{Name: "Seaside Market Square", Lat: seasideSquare.Lat, Lng: seasideSquare.Lng},
		Prefs:    models.UserPrefs{Ratings: map[string]int{"outdoors": 5, "live_music": 4}, Company: "small_group", Answers: map[string]string{}},
		Taste:    models.UserTaste{Tags: map[string]float64{}}, Roles: []string{"demo"}}
	if err := st.Users().Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func testOpts(t *testing.T, now time.Time) Options {
	t.Helper()
	demo, err := democlock.New(demoDate, democlock.DefaultTZ)
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation(atlantaTZ)
	if err != nil {
		t.Fatal(err)
	}
	return Options{Worlds: []string{worldAtlanta, worldSaltlight}, DemoEmail: defaultDemoEmail, Demo: demo, Atlanta: ny,
		Now: now, Target: "the test database"}
}

func seed(t *testing.T, st *store.Store, o Options) string {
	t.Helper()
	var out bytes.Buffer
	if err := Run(context.Background(), st, o, &out); err != nil {
		t.Fatalf("seed: %v\n%s", err, out.String())
	}
	return out.String()
}

// seeded counts the seed's documents per collection.
func seeded(t *testing.T, st *store.Store) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, coll := range writeOrder {
		n, err := st.Collection(coll).CountDocuments(context.Background(), bson.M{fieldSeed: seedTag})
		if err != nil {
			t.Fatal(err)
		}
		out[coll] = n
	}
	return out
}

// everything counts every document of every collection.
func everything(t *testing.T, st *store.Store) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	names, err := st.DB().ListCollectionNames(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, name := range names {
		if out[name], err = st.Collection(name).CountDocuments(ctx, bson.M{}); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// wantSeeded is what a run with Sandy and no earlier bots writes: 8 + 5
// people, 12 + 9 friendships, Rosa's request, 5 + 4 plans with a chat each,
// their 18 + 9 messages and 3 + 2 free-now posts.
var wantSeeded = map[string]int64{store.CollUsers: 13, store.CollFriendships: 21, store.CollFriendRequests: 1,
	store.CollItineraries: 9, store.CollThreads: 9, store.CollMessages: 27, store.CollForumPosts: 5}

func equalCounts(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func rawUser(t *testing.T, st *store.Store, id bson.ObjectID) bson.Raw {
	t.Helper()
	raw, err := st.Collection(store.CollUsers).FindOne(context.Background(), bson.M{"_id": id}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDryRunWritesNothing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogs(t, st)
	makeSandy(t, st)
	before := everything(t, st)
	out := seed(t, st, testOpts(t, now))
	if after := everything(t, st); !equalCounts(before, after) {
		t.Fatalf("a dry run wrote: before %v, after %v", before, after)
	}
	for _, want := range []string{"Mode: dry run", "People: 8 new", "Plans: 5 new; with 5 group chats, 18 messages",
		"Free-now posts: 3 new", "People: 5 new", "Friend requests: 1 new", "Plans: 4 new; with 4 group chats, 9 messages",
		"Tacos, then sea shanties", "Bowling night", "Dry run: nothing was written"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

func TestApplyTwiceKeepsTheSameDocuments(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogs(t, st)
	makeSandy(t, st)
	o := testOpts(t, now)
	o.Apply = true
	seed(t, st, o)
	if got := seeded(t, st); !equalCounts(got, wantSeeded) {
		t.Fatalf("first run wrote %v, want %v", got, wantSeeded)
	}
	ctx := context.Background()
	var ferry models.Itinerary
	if err := st.Collection(store.CollItineraries).FindOne(ctx, bson.M{"_id": "showcase-slt-plan-ferry"}).Decode(&ferry); err != nil {
		t.Fatal(err)
	}

	o.Now = now.Add(90 * time.Minute)
	out := seed(t, st, o)
	if got := seeded(t, st); !equalCounts(got, wantSeeded) {
		t.Fatalf("second run left %v, want %v", got, wantSeeded)
	}
	if !strings.Contains(out, "Total: 85 rewritten") {
		t.Errorf("the rerun should rewrite every document:\n%s", out)
	}
	var again models.Itinerary
	if err := st.Collection(store.CollItineraries).FindOne(ctx, bson.M{"_id": "showcase-slt-plan-ferry"}).Decode(&again); err != nil {
		t.Fatal(err)
	}
	if got := again.CreatedAt.Sub(ferry.CreatedAt); got != 90*time.Minute {
		t.Errorf("the rerun moved the plan's times by %v, want 1h30m", got)
	}

	// Every seeded person with likes in the catalog has unit taste vectors
	// in the catalog's embedding space, like a real account.
	var users []models.User
	if err := findAll(ctx, st, store.CollUsers, bson.M{fieldSeed: seedTag}, bson.M{}, &users); err != nil {
		t.Fatal(err)
	}
	withTaste := 0
	for _, u := range users {
		if len(u.PositiveEmbedding) == 0 {
			continue
		}
		withTaste++
		norm := 0.0
		for _, x := range u.PositiveEmbedding {
			norm += x * x
		}
		if math.Abs(norm-1) > 1e-9 || u.EmbeddingModel != ml.Model || len(u.PositiveEmbedding) != ml.Dim {
			t.Errorf("%s: |v|² %.6f, model %q, %d values", u.Name, norm, u.EmbeddingModel, len(u.PositiveEmbedding))
		}
		if slices.Contains(u.Roles, "demo") || (!slices.Contains(u.Roles, "showcase") && !slices.Contains(u.Roles, "bot")) {
			t.Errorf("%s has roles %v", u.Name, u.Roles)
		}
	}
	if withTaste < 10 {
		t.Errorf("only %d of %d seeded people have taste vectors", withTaste, len(users))
	}
}

func TestRemoveTakesOnlyWhatItWrote(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogs(t, st)
	sandy := makeSandy(t, st)
	alice := &models.User{Email: "alice@example.test", Name: "Alice Real", Username: "alicereal"}
	bob := &models.User{Email: "bob@example.test", Name: "Bob Real", Username: "bobreal"}
	for _, u := range []*models.User{alice, bob} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	aliceID := alice.ID.Hex()
	if _, _, err := st.Friends().Befriend(ctx, aliceID, bob.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	own := &models.Itinerary{HostID: aliceID, Title: "Alice's own plan", Visibility: models.VisibilityOpen,
		Start: now.Add(time.Hour), BackBy: now.Add(3 * time.Hour)}
	if err := st.Itineraries().Insert(ctx, own); err != nil {
		t.Fatal(err)
	}
	o := testOpts(t, now)
	o.Apply = true
	seed(t, st, o)
	sandyRaw := rawUser(t, st, sandy.ID)

	// Alice joins a seeded plan and writes in its chat, as the app does, and
	// asks Hana to be friends.
	planID := "showcase-atl-plan-crawl"
	it, err := st.Itineraries().AddMember(ctx, planID, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	th, _, err := st.Threads().EnsureGroup(ctx, it)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Messages().Insert(ctx, &models.Message{ThreadID: th.ID, SenderID: aliceID, Text: "Joining you two!"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Joins().Record(ctx, planID, aliceID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Friends().CreateRequest(ctx, aliceID, "5eed5c00000000000000a003", "Hi Hana"); err != nil {
		t.Fatal(err)
	}

	// A rerun keeps Alice in the plan and its chat.
	seed(t, st, o)
	var rerun models.Itinerary
	if err := st.Collection(store.CollItineraries).FindOne(ctx, bson.M{"_id": planID}).Decode(&rerun); err != nil {
		t.Fatal(err)
	}
	var chat models.Thread
	if err := st.Collection(store.CollThreads).FindOne(ctx, bson.M{"_id": th.ID}).Decode(&chat); err != nil {
		t.Fatal(err)
	}
	if !rerun.IsMember(aliceID) || !slices.Contains(chat.MemberIDs, aliceID) || chat.LastMessageText != "Joining you two!" {
		t.Fatalf("the rerun dropped Alice: plan %v, chat %v, last %q", rerun.MemberIDs, chat.MemberIDs, chat.LastMessageText)
	}

	before := everything(t, st)
	o.Remove, o.Apply = true, false
	out := seed(t, st, o)
	if after := everything(t, st); !equalCounts(before, after) {
		t.Fatalf("a remove dry run deleted: before %v, after %v", before, after)
	}
	if !strings.Contains(out, "messages: 1 more (written by others in the seeded chats)") ||
		!strings.Contains(out, "join_requests: 1 more") {
		t.Errorf("the removal report misses what lives inside the seeded plans:\n%s", out)
	}

	o.Apply = true
	seed(t, st, o)
	for coll, n := range seeded(t, st) {
		if n != 0 {
			t.Errorf("%d seeded %s left", n, coll)
		}
	}
	after := everything(t, st)
	for coll, want := range map[string]int64{store.CollUsers: 3, store.CollFriendships: 1, store.CollFriendRequests: 1,
		store.CollItineraries: 1, store.CollMessages: 0, store.CollJoinRequests: 0, store.CollThreads: 0, store.CollForumPosts: 0} {
		if after[coll] != want {
			t.Errorf("%s: %d left, want %d", coll, after[coll], want)
		}
	}
	if got := rawUser(t, st, sandy.ID); !bytes.Equal(got, sandyRaw) {
		t.Error("the seed changed Sandy's account")
	}
	if _, err := st.Itineraries().Get(ctx, own.ID); err != nil {
		t.Errorf("Alice's own plan: %v", err)
	}
}

func TestShowcaseOverHTTP(t *testing.T) {
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) { c.DemoDate = demoDate }))
	ctx := context.Background()
	catalogs(t, srv.Store)
	srv.Deps.ML = fakeML(t)

	// Sandy signs in like the app; production made her a demo account with a
	// taste profile.
	sandy := srv.Signup(t, "Sandy Byte")
	demoVectors, err := loadVectors(ctx, srv.Store, store.CollDemoActivities)
	if err != nil {
		t.Fatal(err)
	}
	sandyTaste, _ := blend(map[string]int{"outdoors": 5, "long_walks": 5, "live_music": 4, "food": 4}, demoVectors, true)
	if _, err := srv.Store.Users().Update(ctx, sandy.UserID, bson.M{"email": defaultDemoEmail, "roles": []string{"demo"},
		"city": "saltlight", "homeBase": models.HomeBase{Name: "Seaside Market Square", Lat: seasideSquare.Lat, Lng: seasideSquare.Lng},
		"positiveEmbedding": sandyTaste}); err != nil {
		t.Fatal(err)
	}
	sandyID, _ := bson.ObjectIDFromHex(sandy.UserID)
	sandyBefore := rawUser(t, srv.Store, sandyID)
	// Marin Okafor predates the seed: she is reused, never changed.
	marin := &models.User{Email: "marin@bots.sidequestz.tech", Name: "Marin Okafor", Username: "marinokafor", Roles: []string{"bot"},
		PasswordHash: "unknown", City: "saltlight", Status: "open"}
	if err := srv.Store.Users().Create(ctx, marin); err != nil {
		t.Fatal(err)
	}
	marinBefore := rawUser(t, srv.Store, marin.ID)

	o := testOpts(t, srv.Clock.Now())
	o.Apply = true
	out := seed(t, srv.Store, o)
	if !strings.Contains(out, "= Marin Okafor") {
		t.Errorf("Marin should be kept as she is:\n%s", out)
	}

	atlantaIDs, saltlightIDs := map[string]string{}, map[string]string{}
	for _, p := range atlanta.cast {
		atlantaIDs[p.id.Hex()] = p.name
	}
	for _, p := range saltlight.cast {
		saltlightIDs[p.id.Hex()] = p.name
	}
	saltlightIDs[marin.ID.Hex()] = marin.Name

	t.Run("regular account", func(t *testing.T) {
		fresh := srv.Signup(t, "Fresh Face")
		// No taste profile yet: showcase people fill People for you.
		people := decode[[]contract.PersonSuggestion](t, srv.Do(t, "GET", "/people/suggested", nil, fresh).Expect(t, http.StatusOK))
		showcase := 0
		for _, p := range people {
			if _, ok := atlantaIDs[p.Person.ID]; ok {
				showcase++
			}
			if _, ok := saltlightIDs[p.Person.ID]; ok || p.Person.ID == sandy.UserID {
				t.Errorf("the demo cast is suggested: %s", p.Person.Name)
			}
		}
		if showcase != len(atlanta.cast) {
			t.Errorf("%d showcase people suggested, want %d", showcase, len(atlanta.cast))
		}

		// With one, the showcase people come with a match percent.
		pitchVectors, err := loadVectors(ctx, srv.Store, store.CollPitchActivities)
		if err != nil {
			t.Fatal(err)
		}
		taste, _ := blend(map[string]int{"outdoors": 5, "live_music": 4}, pitchVectors, true)
		if _, err := srv.Store.Users().Update(ctx, fresh.UserID, bson.M{"positiveEmbedding": taste}); err != nil {
			t.Fatal(err)
		}
		people = decode[[]contract.PersonSuggestion](t, srv.Do(t, "GET", "/people/suggested", nil, fresh).Expect(t, http.StatusOK))
		matched := 0
		for _, p := range people {
			if _, ok := atlantaIDs[p.Person.ID]; ok && p.Compatibility != nil {
				matched++
			}
		}
		if matched < 6 {
			t.Errorf("%d showcase people come with a match percent, want most of %d: %+v", matched, len(atlanta.cast), people)
		}

		// The Forum as the app opens it: Midtown, 2 miles, For you.
		feed := decode[contract.Page[contract.ForumPost]](t, srv.Do(t, "GET",
			"/forum/posts?lat=33.7838&lng=-84.3833&radius=2&scope=everyone&type=all&when=any&open_only=false&sort=for_you", nil, fresh).
			Expect(t, http.StatusOK))
		plans, free := 0, 0
		var joinable *contract.ForumPost
		for i, p := range feed.Items {
			if _, ok := saltlightIDs[p.Author.ID]; ok {
				t.Errorf("a Saltlight post in Atlanta: %+v", p)
			}
			if _, ok := atlantaIDs[p.Author.ID]; !ok {
				continue
			}
			switch p.Type {
			case contract.PostPlan:
				plans++
				if p.Compatibility == nil {
					t.Errorf("plan %q has no match percent", *p.Title)
				}
				if joinable == nil && p.JoinStatus == contract.JoinNone {
					joinable = &feed.Items[i]
				}
			case contract.PostFreeNow:
				free++
			}
		}
		// Four open plans (the picnic is for friends) and three people free now.
		if plans != 4 || free != 3 {
			t.Fatalf("the feed shows %d showcase plans and %d free-now posts, want 4 and 3: %+v", plans, free, feed.Items)
		}
		if joinable == nil {
			t.Fatal("no open plan to join")
		}

		// Joining one puts it on Home, with its group chat.
		join := decode[contract.JoinResult](t, srv.Do(t, "POST", "/forum/posts/"+joinable.ID+"/join-requests", nil, fresh).Expect(t, http.StatusOK))
		if join.Status != contract.JoinJoined || join.ThreadID == nil {
			t.Fatalf("join: %+v", join)
		}
		home := decode[[]contract.Itinerary](t, srv.Do(t, "GET", "/itineraries", nil, fresh).Expect(t, http.StatusOK))
		if len(home) != 1 || home[0].ID != joinable.ID || len(home[0].Items) == 0 || home[0].IsHost {
			t.Fatalf("Home after joining: %+v", home)
		}
		msgs := decode[[]contract.Message](t, srv.Do(t, "GET", "/threads/"+*join.ThreadID+"/messages", nil, fresh).Expect(t, http.StatusOK))
		if len(msgs) < 3 {
			t.Errorf("the plan's chat has %d messages", len(msgs))
		}
	})

	t.Run("demo account", func(t *testing.T) {
		feed := decode[contract.Page[contract.ForumPost]](t, srv.Do(t, "GET",
			"/forum/posts?lat=31.368&lng=-81.425&radius=2&scope=everyone&type=all&when=any&open_only=false&sort=for_you", nil, sandy).
			Expect(t, http.StatusOK))
		plans, free, joined := 0, 0, 0
		for _, p := range feed.Items {
			if _, ok := atlantaIDs[p.Author.ID]; ok {
				t.Errorf("an Atlanta post in Saltlight: %+v", p)
			}
			if _, ok := saltlightIDs[p.Author.ID]; !ok {
				continue
			}
			switch p.Type {
			case contract.PostPlan:
				plans++
				if p.JoinStatus == contract.JoinJoined {
					joined++
				}
				if p.Compatibility == nil {
					t.Errorf("plan %q has no match percent", *p.Title)
				}
			case contract.PostFreeNow:
				free++
			}
		}
		if plans != 4 || joined != 1 || free != 2 {
			t.Fatalf("Sandy's feed: %d plans (%d joined), %d free-now posts; want 4 (1), 2: %+v", plans, joined, free, feed.Items)
		}

		friends := decode[[]contract.Friend](t, srv.Do(t, "GET", "/friends", nil, sandy).Expect(t, http.StatusOK))
		var names []string
		for _, f := range friends {
			names = append(names, f.Person.Name)
		}
		slices.Sort(names)
		if want := []string{"Juno Reyes", "Kai Nakamura", "Marin Okafor", "Theo Park"}; !slices.Equal(names, want) {
			t.Errorf("Sandy's friends %v, want %v", names, want)
		}
		requests := decode[[]contract.FriendRequest](t, srv.Do(t, "GET", "/friends/requests", nil, sandy).Expect(t, http.StatusOK))
		if len(requests) != 1 || requests[0].Person.Name != "Rosa Delgado" || requests[0].Outgoing {
			t.Errorf("Sandy's requests: %+v", requests)
		}
		home := decode[[]contract.Itinerary](t, srv.Do(t, "GET", "/itineraries", nil, sandy).Expect(t, http.StatusOK))
		if len(home) != 1 || home[0].Title != "Bowling night" || home[0].GoingCount != 3 {
			t.Errorf("Sandy's Home: %+v", home)
		}
		threads := decode[[]contract.ChatThread](t, srv.Do(t, "GET", "/threads", nil, sandy).Expect(t, http.StatusOK))
		if len(threads) != 1 || threads[0].Title != "Bowling night" || threads[0].Unread != 2 {
			t.Errorf("Sandy's chats: %+v", threads)
		}
		if people := decode[[]contract.PersonSuggestion](t, srv.Do(t, "GET", "/people/suggested", nil, sandy).Expect(t, http.StatusOK)); len(people) != 0 {
			t.Errorf("the demo account gets suggestions: %+v", people)
		}
		if got := rawUser(t, srv.Store, sandyID); !bytes.Equal(got, sandyBefore) {
			t.Error("the seed changed Sandy's account")
		}
		if got := rawUser(t, srv.Store, marin.ID); !bytes.Equal(got, marinBefore) {
			t.Error("the seed changed Marin's account")
		}
	})
}

func TestConflictsStopTheWrite(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogs(t, st)
	makeSandy(t, st)
	taken := &models.User{Email: "someone@example.test", Name: "Someone Else", Username: "hana.kim"}
	if err := st.Users().Create(context.Background(), taken); err != nil {
		t.Fatal(err)
	}
	o := testOpts(t, now)
	out := seed(t, st, o)
	if !strings.Contains(out, "! Hana Kim · @hana.kim · SCAD Atlanta (@hana.kim belongs to another account, "+taken.ID.Hex()+")") {
		t.Errorf("the dry run should show the conflict:\n%s", out)
	}
	o.Apply = true
	var buf bytes.Buffer
	err := Run(context.Background(), st, o, &buf)
	if err == nil || !strings.Contains(err.Error(), "refusing to write") {
		t.Fatalf("apply with a conflict: %v", err)
	}
	for coll, n := range seeded(t, st) {
		if n != 0 {
			t.Errorf("%d %s written despite the conflict", n, coll)
		}
	}
}

func TestTasteFromTheMLService(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogs(t, st)
	makeSandy(t, st)
	o := testOpts(t, now)
	o.Apply, o.ML, o.MLURL = true, fakeML(t), "the fake ML service"
	out := seed(t, st, o)
	if !strings.Contains(out, "taste vectors: 8 built by the ML service") || !strings.Contains(out, "taste vectors: 5 built by the ML service") {
		t.Errorf("the ML service should build every profile:\n%s", out)
	}
	var diego storedProfile
	if err := st.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": oid("5eed5c00000000000000a002")}).Decode(&diego); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(diego.ProfileTextHash, "profile-v1:") || diego.EmbeddingModel != ml.Model || diego.PositiveText == "" ||
		len(diego.PositiveEmbedding) != ml.Dim || diego.ProfileInputHash == "" {
		t.Fatalf("Diego's profile: hash %q, model %q, text %q, %d values", diego.ProfileTextHash, diego.EmbeddingModel,
			diego.PositiveText, len(diego.PositiveEmbedding))
	}

	// Without the service a rerun keeps what it built instead of blending.
	down := ml.DefaultOptions()
	down.BaseURL = "http://127.0.0.1:1"
	o.ML, o.MLURL = ml.NewClientWithOptions(down), "http://127.0.0.1:1"
	out = seed(t, st, o)
	if !strings.Contains(out, "taste vectors: 8 kept from an earlier ML build") {
		t.Errorf("a rerun without the service should keep the vectors:\n%s", out)
	}
	var again storedProfile
	if err := st.Collection(store.CollUsers).FindOne(ctx, bson.M{"_id": oid("5eed5c00000000000000a002")}).Decode(&again); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(again.PositiveEmbedding, diego.PositiveEmbedding) || again.ProfileTextHash != diego.ProfileTextHash {
		t.Error("the rerun replaced the ML-built vectors")
	}
}

// storedProfile is a user with the profile bookkeeping pkg/profiles keeps.
type storedProfile struct {
	models.User      `bson:",inline"`
	ProfileInputHash string `bson:"profileInputHash"`
}

func TestSaltlightNeedsTheDemoAccount(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	catalogs(t, st)
	o := testOpts(t, now)
	out := seed(t, st, o)
	if !strings.Contains(out, "skipped, no account demo@sidequestz.tech") || !strings.Contains(out, "People: 8 new") {
		t.Errorf("without Sandy only Atlanta is planned:\n%s", out)
	}
	o.Worlds, o.Named = []string{worldSaltlight}, true
	var buf bytes.Buffer
	if err := Run(context.Background(), st, o, &buf); err == nil || !strings.Contains(err.Error(), "saltlight: no account") {
		t.Fatalf("--world saltlight without Sandy: %v", err)
	}
}

func TestRefusesWithoutTheAppCollections(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := store.New(testutil.DB(t), func() time.Time { return now }) // no indexes: no app collections
	o := testOpts(t, now)
	o.Apply = true
	var buf bytes.Buffer
	if err := Run(context.Background(), st, o, &buf); err == nil || !strings.Contains(err.Error(), "refusing to write: the target has no users") {
		t.Fatalf("apply on an empty database: %v", err)
	}
}

func TestReadOptions(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	if _, _, _, err := readOptions(env(nil), true, false, "all", defaultDemoEmail); err == nil || !strings.Contains(err.Error(), "MONGO_DB") {
		t.Errorf("--apply without MONGO_DB: %v", err)
	}
	if code := cli([]string{"--apply"}, env(nil), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Errorf("cli --apply without MONGO_DB exits %d, want 2", code)
	}
	o, uri, db, err := readOptions(env(nil), false, false, "all", defaultDemoEmail)
	if err != nil || db != defaultMongoDB || uri != defaultMongoURI || o.Demo.Date() != demoDate || len(o.Worlds) != 2 || o.Named {
		t.Errorf("dry-run defaults: %+v %q %q %v", o, uri, db, err)
	}
	secret := "mongodb://admin:s3cret-pass@db.example.test:27017,db2.example.test:27018/?authSource=admin"
	o, _, db, err = readOptions(env(map[string]string{"MONGO_URI": secret, "MONGO_DB": "freetime", "DEMO_DATE": "2026-10-01"}),
		true, false, "saltlight", "demo@example.test")
	if err != nil || db != "freetime" || !o.Named || o.Worlds[0] != worldSaltlight || o.Demo.Date() != "2026-10-01" {
		t.Fatalf("explicit options: %+v %v", o, err)
	}
	if strings.Contains(o.Target, "s3cret") || strings.Contains(o.Target, "admin") || !strings.Contains(o.Target, "db.example.test:27017") {
		t.Errorf("the printed target leaks credentials or loses the host: %q", o.Target)
	}
	if got := redact("connect "+secret+": auth failed for s3cret-pass", secret); strings.Contains(got, "s3cret") {
		t.Errorf("redact: %q", got)
	}
	if _, _, _, err := readOptions(env(nil), false, false, "mars", defaultDemoEmail); err == nil {
		t.Error("an unknown world is accepted")
	}
	if _, _, _, err := readOptions(env(map[string]string{"DEMO_DATE": "Sunday"}), false, false, "all", defaultDemoEmail); err == nil {
		t.Error("a malformed DEMO_DATE is accepted")
	}
}

func TestPlanTimes(t *testing.T) {
	ny, _ := time.LoadLocation(atlantaTZ)
	spec := &planSpec{day: 0, start: [2]int{18, 30}}
	morning := time.Date(2026, 9, 27, 9, 0, 0, 0, ny)
	if got := planStart(spec, morning, ny); !got.Equal(time.Date(2026, 9, 27, 18, 30, 0, 0, ny)) {
		t.Errorf("a morning run: %v", got)
	}
	late := time.Date(2026, 9, 27, 18, 0, 0, 0, ny) // less than leadTime before the start
	if got := planStart(spec, late, ny); !got.Equal(time.Date(2026, 9, 28, 18, 30, 0, 0, ny)) {
		t.Errorf("a late run moves the plan a day: %v", got)
	}
	if got := freeUntil(21, late, ny); !got.Equal(time.Date(2026, 9, 27, 21, 0, 0, 0, ny)) {
		t.Errorf("free until tonight: %v", got)
	}
	if got := freeUntil(21, time.Date(2026, 9, 27, 20, 20, 0, 0, ny), ny); !got.Equal(time.Date(2026, 9, 27, 22, 30, 0, 0, ny)) {
		t.Errorf("free until two hours from a late run, on the half hour: %v", got)
	}
	sent := time.Date(2026, 9, 26, 12, 0, 0, 0, ny)
	for start, want := range map[time.Time]string{
		time.Date(2026, 9, 26, 19, 0, 0, 0, ny): "tonight", time.Date(2026, 9, 26, 15, 0, 0, 0, ny): "today",
		time.Date(2026, 9, 27, 19, 0, 0, 0, ny): "tomorrow", time.Date(2026, 9, 28, 19, 0, 0, 0, ny): "on Monday",
	} {
		if got := whenWord(start, sent); got != want {
			t.Errorf("whenWord(%v) = %q, want %q", start, got, want)
		}
	}
}

// decode reads a response strictly into the contract type: a field the
// type does not know fails the test.
func decode[T any](t *testing.T, res *testutil.Response) T {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(res.Body))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %T: %v\n%s", v, err, res.Body)
	}
	return v
}

// fakeML is the ML service's health, profile and match routes over plain
// vector math: a profile is a unit vector made from the ratings, a match
// is the cosine of two taste vectors (a plan's, the mean over its stops).
func fakeML(t *testing.T) *ml.Client {
	t.Helper()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	cos := func(a, b []float64) float64 {
		var dot, na, nb float64
		for i := range a {
			dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
		}
		if na == 0 || nb == 0 {
			return 0
		}
		return dot / math.Sqrt(na*nb)
	}
	type match struct {
		ID      string  `json:"id"`
		Score   float64 `json:"score"`
		Percent int     `json:"percent"`
	}
	percent := func(c float64) int { return int(math.Round(100 * max(0, c))) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"status": "ok", "profile_template_version": "profile-v1",
			"embedding": map[string]any{"model": ml.Model, "dim": ml.Dim, "mode": "local", "provider": "local"}})
	})
	mux.HandleFunc("POST /v1/user-profile", func(w http.ResponseWriter, r *http.Request) {
		var req ml.UserProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("fake ML: %v", err)
		}
		v := make([]float64, ml.Dim)
		for i, trip := range contract.TripTypes {
			v[i] = float64(req.Ratings[trip])
		}
		v[ml.Dim-1] = 1
		unit(v)
		write(w, map[string]any{"positive_text": "Interests:\n- " + req.Answers.PerfectAfternoon, "negative_text": "",
			"positive_embedding": v, "negative_embedding": make([]float64, ml.Dim), "profile_text_hash": "profile-v1:fake",
			"template_version": "profile-v1", "model": ml.Model, "dim": ml.Dim, "provider": "fake"})
	})
	mux.HandleFunc("POST /v1/compatibility/users", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User struct {
				Positive []float64 `json:"positive_embedding"`
			} `json:"user"`
			Candidates []struct {
				ID       string    `json:"id"`
				Positive []float64 `json:"positive_embedding"`
			} `json:"candidates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("fake ML: %v", err)
		}
		out := []match{}
		for _, c := range body.Candidates {
			s := cos(body.User.Positive, c.Positive)
			out = append(out, match{c.ID, s, percent(s)})
		}
		slices.SortFunc(out, func(a, b match) int { return int(math.Copysign(1, b.Score-a.Score)) })
		write(w, map[string]any{"results": out})
	})
	mux.HandleFunc("POST /v1/compatibility/itineraries", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User struct {
				Positive []float64 `json:"positive_embedding"`
			} `json:"user"`
			Itineraries []struct {
				ID     string `json:"id"`
				Events []struct {
					Embedding []float64 `json:"embedding"`
				} `json:"events"`
			} `json:"itineraries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("fake ML: %v", err)
		}
		out := []match{}
		for _, it := range body.Itineraries {
			sum := 0.0
			for _, e := range it.Events {
				sum += cos(body.User.Positive, e.Embedding)
			}
			s := sum / float64(len(it.Events))
			out = append(out, match{it.ID, s, percent(s)})
		}
		write(w, map[string]any{"results": out})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	o := ml.DefaultOptions()
	o.BaseURL = srv.URL
	return ml.NewClientWithOptions(o)
}
