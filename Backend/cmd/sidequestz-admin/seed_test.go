package main

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"Backend/pkg/travel"
	"Backend/pkg/util"
	"errors"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// demoCatalogFile is the Saltlight snapshot the VPS imports into demo_activities.
const demoCatalogFile = "../../../dataingestion/demo/saltlight_harbor.json"

const demoPassword = "tidepool-2026"

func readDemoCatalog(t *testing.T) []models.Activity {
	t.Helper()
	raw, err := os.ReadFile(demoCatalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var docs []models.Activity
	if err := bson.UnmarshalExtJSON(raw, false, &docs); err != nil {
		t.Fatal(err)
	}
	return docs
}

// seedServer is a test server whose database holds the demo catalog.
func seedServer(t *testing.T) (*testutil.Server, *config.Config) {
	t.Helper()
	srv := testutil.New(t)
	var docs []any
	for _, a := range readDemoCatalog(t) {
		docs = append(docs, a)
	}
	if _, err := srv.Store.Collection(store.CollDemoActivities).InsertMany(t.Context(), docs); err != nil {
		t.Fatal(err)
	}
	cfg := *srv.Cfg
	cfg.DemoPassword = demoPassword
	return srv, &cfg
}

var seededCollections = []string{
	store.CollUsers, store.CollItineraries, store.CollThreads, store.CollMessages, store.CollExpenses, store.CollRatings,
	store.CollFriendships, store.CollFriendRequests, store.CollForumPosts, store.CollPaymentMethods,
}

func countAll(t *testing.T, st *store.Store) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, coll := range seededCollections {
		n, err := st.Collection(coll).CountDocuments(t.Context(), bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		out[coll] = n
	}
	return out
}

func findDoc[T any](t *testing.T, st *store.Store, coll string, filter bson.M) T {
	t.Helper()
	var doc T
	if err := st.Collection(coll).FindOne(t.Context(), filter).Decode(&doc); err != nil {
		t.Fatalf("%s %v: %v", coll, filter, err)
	}
	return doc
}

func findDocs[T any](t *testing.T, st *store.Store, coll string, filter bson.M) []T {
	t.Helper()
	cursor, err := st.Collection(coll).Find(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	var docs []T
	if err := cursor.All(t.Context(), &docs); err != nil {
		t.Fatal(err)
	}
	return docs
}

// balances is MockLedger.balances: + they owe me, − I owe them.
func balances(expenses []models.Expense, me string, members []string) map[string]int {
	net := map[string]int{}
	for _, m := range members {
		if m != me {
			net[m] = 0
		}
	}
	for _, e := range expenses {
		for i, member := range e.SplitAmong {
			if member == e.PayerID {
				continue
			}
			if e.PayerID == me {
				net[member] += e.Shares[i]
			} else if member == me {
				net[e.PayerID] -= e.Shares[i]
			}
		}
	}
	return net
}

func TestSeedDemo(t *testing.T) {
	srv, cfg := seedServer(t)
	st := srv.Store
	ctx := t.Context()
	loc, _ := time.LoadLocation(cfg.DemoTZ)
	p := &testutil.ProfilesRecorder{}
	now := srv.Clock.Now()

	report, err := seedDemo(ctx, st, cfg, p, now)
	if err != nil {
		t.Fatal(err)
	}
	sandyID, marinID, theoID := sandy.id.Hex(), marin.id.Hex(), theo.id.Hex()
	if report.SandyID != sandyID || p.Refreshes(sandyID) != 1 || !strings.HasPrefix(report.Vectors, "refreshed") {
		t.Fatalf("report %+v, refreshes %d", report, p.Refreshes(sandyID))
	}
	want := map[string]int64{
		store.CollUsers: 3, store.CollItineraries: 3, store.CollThreads: 2, store.CollMessages: 5, store.CollExpenses: 2,
		store.CollRatings: 2, store.CollFriendships: 1, store.CollFriendRequests: 1, store.CollForumPosts: 1, store.CollPaymentMethods: 1,
	}
	first := countAll(t, st)
	for coll, n := range want {
		if first[coll] != n {
			t.Errorf("%s: %d documents, want %d", coll, first[coll], n)
		}
	}
	for _, c := range report.Counts {
		if int64(c.N) != want[c.Collection] {
			t.Errorf("report counts %s %d, want %d", c.Collection, c.N, want[c.Collection])
		}
	}

	// Sandy signs in with DEMO_PASSWORD and is set up in Saltlight.
	sess := srv.Login(t, sandy.email, demoPassword)
	var me contract.User
	srv.Do(t, "GET", "/me", nil, sess).Expect(t, http.StatusOK).JSON(t, &me)
	home := contract.Place{Name: "Seaside Market Square", Coordinate: &contract.Coordinate{Lat: 31.368, Lng: -81.425}}
	if me.ID != sandyID || me.Name != "Sandy Byte" || me.Username == nil || *me.Username != "sandybyte" || me.Email != sandy.email ||
		me.AvatarColor != contract.AvatarSage || me.Status != contract.StatusOpen || me.AgeBracket != contract.AgeAdult ||
		me.School == nil || *me.School != "Saltlight Harbor College" || !me.SetupComplete ||
		me.City == nil || *me.City != "saltlight" || me.HomeBase == nil || *me.HomeBase.Coordinate != *home.Coordinate || me.HomeBase.Name != home.Name {
		t.Fatalf("GET /me: %+v", me)
	}
	user := findDoc[models.User](t, st, store.CollUsers, bson.M{"_id": sandy.id})
	if user.Catalog != "demo_activities" || !user.HasRole("demo") || !reflect.DeepEqual(user.Prefs, sandyPrefs) || !user.Prefs.IsSet() {
		t.Fatalf("Sandy's document: %+v", user)
	}
	if user.Prefs.Answers["never_do"] != "Packed clubs, huge crowds, or anything that only gets going after midnight." || user.Prefs.Ratings["big_crowds"] != 1 {
		t.Fatalf("Sandy's answers/ratings: %+v", user.Prefs)
	}
	for _, bot := range []seedPerson{marin, theo} {
		doc := findDoc[models.User](t, st, store.CollUsers, bson.M{"_id": bot.id})
		if !doc.HasRole("bot") || doc.Catalog != "demo_activities" || doc.City != "saltlight" || doc.PasswordHash == "" {
			t.Fatalf("bot %s: %+v", bot.name, doc)
		}
		if ok, _ := util.VerifyPassword(demoPassword, doc.PasswordHash); ok {
			t.Fatalf("bot %s shares the demo password", bot.name)
		}
	}

	days := demoDays(now, loc)

	// Marin's open plan: tomorrow 17:30–20:00 over the three places nearest the market.
	open := findDoc[models.Itinerary](t, st, store.CollItineraries, bson.M{"_id": seedOpenPlanID})
	start, backBy, lock := at(days.tomorrow, 17, 30), at(days.tomorrow, 20, 0), at(days.tomorrow, 17, 0)
	if open.HostID != marinID || !slices.Equal(open.MemberIDs, []string{marinID, theoID}) || open.Visibility != models.VisibilityOpen ||
		open.MaxGroupSize == nil || *open.MaxGroupSize != 6 || open.LockAt == nil || !open.LockAt.Equal(lock) ||
		!open.Start.Equal(start) || !open.BackBy.Equal(backBy) || open.Status != models.ItineraryActive ||
		open.DateKey != days.tomorrow.Format("2006-01-02") || !open.Date.Equal(days.tomorrow) || open.TZ != cfg.DemoTZ {
		t.Fatalf("open plan: %+v", open)
	}
	var nearest []string
	{
		places := []models.Activity{}
		for _, a := range readDemoCatalog(t) {
			if a.Kind == "place" {
				places = append(places, a)
			}
		}
		sort.Slice(places, func(i, j int) bool {
			return travel.HaversineKm(homePoint, placePoint(&places[i])) < travel.HaversineKm(homePoint, placePoint(&places[j]))
		})
		for _, a := range places[:3] {
			nearest = append(nearest, a.ID.Hex())
		}
	}
	checkLayout(t, "open plan", open, start, backBy, 3)
	var got []string
	for _, stop := range stopsOf(open.Items) {
		got = append(got, stop.ActivityID)
	}
	if !sameSet(got, nearest) {
		t.Fatalf("open plan stops %v, want the nearest places %v", got, nearest)
	}

	// The market crew: last Saturday, rated stops, the group chat and splits.
	crew := findDoc[models.Itinerary](t, st, store.CollItineraries, bson.M{"_id": seedCrewPlanID})
	if crew.Status != models.ItineraryPast || crew.Title != "Saturday market crew" || len(crew.MemberIDs) != 3 || crew.ThreadID != seedCrewThreadID ||
		crew.DateKey != days.lastSaturday.Format("2006-01-02") || crew.Date.In(loc).Weekday() != time.Saturday || !crew.BackBy.Before(now) {
		t.Fatalf("crew plan: %+v", crew)
	}
	checkLayout(t, "crew plan", crew, crew.Start, crew.BackBy, 2)
	if n, _ := st.Collection(store.CollItineraries).CountDocuments(ctx, bson.M{"visibility": bson.M{"$ne": models.VisibilityJustMe}}); n != 1 {
		t.Fatalf("%d plans would be on the Forum; only Marin's open plan should", n)
	}
	for i, want := range []struct {
		stars int
		tags  []string
	}{{5, []string{"Great people", "Would go again"}}, {4, []string{"Good value"}}} {
		stop := stopsOf(crew.Items)[i]
		r := findDoc[models.Rating](t, st, store.CollRatings, bson.M{"_id": models.PairID(sandyID, stop.ID)})
		if r.Stars != want.stars || !slices.Equal(r.Tags, want.tags) || r.ItineraryID != seedCrewPlanID || r.ActivityID != stop.ActivityID || r.ActivityID == "" {
			t.Fatalf("rating %d: %+v", i, r)
		}
	}
	solo := findDoc[models.Itinerary](t, st, store.CollItineraries, bson.M{"_id": seedSoloPlanID})
	checkLayout(t, "solo walk", solo, solo.Start, solo.BackBy, 1)
	soloStop := stopsOf(solo.Items)[0]
	if !slices.Equal(solo.MemberIDs, []string{sandyID}) || solo.Status != models.ItineraryPast || !soloStop.End.Before(now) ||
		solo.DateKey != days.yesterday.Format("2006-01-02") {
		t.Fatalf("solo plan: %+v", solo)
	}
	if n, _ := st.Collection(store.CollRatings).CountDocuments(ctx, bson.M{"itemId": soloStop.ID}); n != 0 {
		t.Fatal("the solo stop must stay unrated")
	}

	group := findDoc[models.Thread](t, st, store.CollThreads, bson.M{"_id": seedCrewThreadID})
	crewMsgs := findDocs[models.Message](t, st, store.CollMessages, bson.M{"threadId": seedCrewThreadID})
	if !group.IsGroup || group.ItineraryID != seedCrewPlanID || group.Title != "Saturday market crew" || len(crewMsgs) != 3 || group.Unread[sandyID] != 0 {
		t.Fatalf("group thread %+v with %d messages", group, len(crewMsgs))
	}
	sort.Slice(crewMsgs, func(i, j int) bool { return crewMsgs[i].SentAt.Before(crewMsgs[j].SentAt) })
	if last := crewMsgs[2]; group.LastMessageText != last.Text || group.LastSenderID != last.SenderID || !group.LastMessageAt.Equal(last.SentAt) {
		t.Fatalf("group preview %+v vs last message %+v", group, last)
	}
	expenses := findDocs[models.Expense](t, st, store.CollExpenses, bson.M{"groupId": seedCrewThreadID})
	if len(expenses) != 2 {
		t.Fatalf("expenses: %+v", expenses)
	}
	for _, e := range expenses {
		sum := 0
		for _, s := range e.Shares {
			sum += s
		}
		if sum != e.AmountCents || len(e.Shares) != 3 || e.CreatedBy != e.PayerID {
			t.Fatalf("expense %+v", e)
		}
	}
	if got := balances(expenses, sandyID, group.MemberIDs); got[marinID] != -500 || got[theoID] != 300 {
		t.Fatalf("Sandy's balances %v: she owes Marin $5 and Theo owes her $3", got)
	}
	if got := balances(expenses, theoID, group.MemberIDs); got[marinID] != -800 || got[sandyID] != -300 {
		t.Fatalf("Theo's balances %v: he owes Marin $8 and Sandy $3", got)
	}

	dm := findDoc[models.Thread](t, st, store.CollThreads, bson.M{"dmKey": models.DMKey(sandyID, marinID)})
	dmMsgs := findDocs[models.Message](t, st, store.CollMessages, bson.M{"threadId": dm.ID})
	if dm.IsGroup || dm.Unread[sandyID] != 1 || len(dmMsgs) != 2 || dm.LastSenderID != marinID || !slices.Contains(dm.MemberIDs, sandyID) {
		t.Fatalf("DM %+v with %d messages", dm, len(dmMsgs))
	}

	post := findDoc[models.ForumPost](t, st, store.CollForumPosts, bson.M{"_id": seedFreePostID})
	if post.Type != models.PostFreeNow || post.AuthorID != marinID || !post.Until.After(now) || !post.ExpiresAt.Equal(post.Until) ||
		!strings.HasPrefix(post.Text, "Free until ") || post.Until.In(loc).Minute()%30 != 0 || len(post.Location.Coordinates) != 2 {
		t.Fatalf("free post: %+v", post)
	}
	if friends := findDoc[models.Friendship](t, st, store.CollFriendships, bson.M{"_id": models.FriendshipID(sandyID, marinID)}); len(friends.UserIDs) != 2 {
		t.Fatalf("friendship: %+v", friends)
	}
	request := findDoc[models.FriendRequest](t, st, store.CollFriendRequests, bson.M{"_id": seedRequestID})
	if request.FromID != theoID || request.ToID != sandyID || request.Status != models.RequestPending || request.Note != "Met at the market" {
		t.Fatalf("friend request: %+v", request)
	}
	card := findDoc[models.PaymentMethod](t, st, store.CollPaymentMethods, bson.M{"userId": sandyID})
	if card.Brand != "Visa" || card.Last4 != "4242" || !card.IsDefault {
		t.Fatalf("card: %+v", card)
	}

	// What a walkthrough changes, and a changed profile, go back to the seeded state.
	if _, err := st.Collection(store.CollRatings).InsertOne(ctx, models.Rating{ID: models.PairID(sandyID, soloStop.ID), UserID: sandyID,
		ItemID: soloStop.ID, ItineraryID: seedSoloPlanID, Stars: 3, Tags: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collection(store.CollFriendships).InsertOne(ctx, models.Friendship{ID: models.FriendshipID(sandyID, theoID),
		UserIDs: []string{sandyID, theoID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collection(store.CollFriendRequests).UpdateOne(ctx, bson.M{"_id": seedRequestID}, bson.M{"$set": bson.M{"status": models.RequestAccepted}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collection(store.CollPaymentMethods).InsertOne(ctx, models.PaymentMethod{ID: "pm-extra", UserID: sandyID, Brand: "Mastercard",
		Last4: "5454", IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Users().Update(ctx, sandyID, bson.M{"prefs.spend": "over_40", "profileTextHash": "profile-v1:abc", "positiveEmbedding": []float64{1, 0}}); err != nil {
		t.Fatal(err)
	}

	if _, err := seedDemo(ctx, st, cfg, p, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second := countAll(t, st)
	second[store.CollPaymentMethods]-- // the extra card stays; it is not the seed's
	for coll, n := range first {
		if second[coll] != n {
			t.Errorf("after a second run %s has %d documents, want %d", coll, second[coll], n)
		}
	}
	if p.Refreshes(sandyID) != 2 {
		t.Fatalf("refreshes after two runs: %d", p.Refreshes(sandyID))
	}
	if n, _ := st.Collection(store.CollRatings).CountDocuments(ctx, bson.M{"itemId": soloStop.ID}); n != 0 {
		t.Fatal("a second run must leave the solo stop unrated again")
	}
	if n, _ := st.Collection(store.CollFriendships).CountDocuments(ctx, bson.M{"_id": models.FriendshipID(sandyID, theoID)}); n != 0 {
		t.Fatal("a second run must undo the friendship with Theo")
	}
	if r := findDoc[models.FriendRequest](t, st, store.CollFriendRequests, bson.M{"_id": seedRequestID}); r.Status != models.RequestPending {
		t.Fatalf("Theo's request is %s after a second run", r.Status)
	}
	if extra := findDoc[models.PaymentMethod](t, st, store.CollPaymentMethods, bson.M{"_id": "pm-extra"}); extra.IsDefault {
		t.Fatal("the demo Visa must be the only default card")
	}
	user = findDoc[models.User](t, st, store.CollUsers, bson.M{"_id": sandy.id})
	if user.Prefs.Spend != "under_15" || user.ProfileTextHash != "" || len(user.PositiveEmbedding) != 2 {
		t.Fatalf("second run: prefs restored, hash cleared, vectors kept: %+v", user)
	}
	srv.Login(t, sandy.email, demoPassword)
}

// checkLayout asserts a walking plan: walk, stop, …, walk; back to back;
// inside [start, backBy]; unique ids.
func checkLayout(t *testing.T, name string, itin models.Itinerary, start, backBy time.Time, stops int) {
	t.Helper()
	if len(itin.Items) != 2*stops+1 {
		t.Fatalf("%s: %d items, want %d", name, len(itin.Items), 2*stops+1)
	}
	cursor := start
	seen := map[string]bool{}
	for i, item := range itin.Items {
		wantKind := models.ItemTransit
		if i%2 == 1 {
			wantKind = models.ItemStop
		}
		if item.Kind != wantKind || !item.Start.Equal(cursor) || !item.End.After(item.Start) || seen[item.ID] || !strings.HasPrefix(item.ID, "seed-") {
			t.Fatalf("%s item %d: %+v (cursor %s)", name, i, item, cursor)
		}
		if item.Kind == models.ItemStop && (item.Place == nil || !item.Place.HasCoordinate() || item.ActivityID == "" || item.Description == "") {
			t.Fatalf("%s stop %d lacks place/activity/description: %+v", name, i, item)
		}
		if item.Kind == models.ItemTransit && (item.LegMode != "walk" || item.LegMinutes < 1 || !strings.HasPrefix(item.Title, "Walk to ")) {
			t.Fatalf("%s leg %d: %+v", name, i, item)
		}
		seen[item.ID] = true
		cursor = item.End
	}
	if cursor.After(backBy) {
		t.Fatalf("%s ends %s, after back-by %s", name, cursor, backBy)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func TestSeedDemoRefusesWithoutPasswordOrCatalog(t *testing.T) {
	srv := testutil.New(t)
	cfg := *srv.Cfg
	cfg.DemoPassword = ""
	if _, err := seedDemo(t.Context(), srv.Store, &cfg, nil, srv.Clock.Now()); err == nil || !strings.Contains(err.Error(), "DEMO_PASSWORD") {
		t.Fatalf("without DEMO_PASSWORD: %v", err)
	}
	cfg.DemoPassword = demoPassword
	if _, err := seedDemo(t.Context(), srv.Store, &cfg, nil, srv.Clock.Now()); err == nil || !strings.Contains(err.Error(), "demo_activities") {
		t.Fatalf("without the catalog: %v", err)
	}
	for coll, n := range countAll(t, srv.Store) {
		if n != 0 {
			t.Errorf("a refused seed wrote %d %s", n, coll)
		}
	}
	cfg.DemoTZ = "Mars/Olympus_Mons"
	if _, err := seedDemo(t.Context(), srv.Store, &cfg, nil, srv.Clock.Now()); err == nil || !strings.Contains(err.Error(), "DEMO_TZ") {
		t.Fatalf("bad DEMO_TZ: %v", err)
	}
}

func TestSeedDemoVectors(t *testing.T) {
	srv, cfg := seedServer(t)
	report, err := seedDemo(t.Context(), srv.Store, cfg, nil, srv.Clock.Now())
	if err != nil || !strings.HasPrefix(report.Vectors, "pending") {
		t.Fatalf("no profiles: %v %+v", err, report)
	}
	p := &testutil.ProfilesRecorder{Err: errors.New("ml service down")}
	report, err = seedDemo(t.Context(), srv.Store, cfg, p, srv.Clock.Now())
	if err != nil || !strings.Contains(report.Vectors, "not refreshed") || p.Refreshes(sandy.id.Hex()) != 1 {
		t.Fatalf("a failing ML service must not fail the seed: %v %+v", err, report)
	}
	var out strings.Builder
	report.print(&out, "freetime")
	if !strings.Contains(out.String(), "Sandy Byte") || !strings.Contains(out.String(), "itineraries 3") || !strings.Contains(out.String(), "not refreshed") {
		t.Fatalf("report:\n%s", out.String())
	}
}

func TestSeedDemoAdoptsAccounts(t *testing.T) {
	srv, cfg := seedServer(t)
	req := testutil.SignupRequest("Sandy Byte")
	req.Email = sandy.email
	existing := srv.SignupWith(t, req)

	report, err := seedDemo(t.Context(), srv.Store, cfg, nil, srv.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.SandyID != existing.UserID || report.SandyID == sandy.id.Hex() {
		t.Fatalf("the account holding demo@ must be adopted: %s vs %s", report.SandyID, existing.UserID)
	}
	if n, _ := srv.Store.Collection(store.CollUsers).CountDocuments(t.Context(), bson.M{}); n != 3 {
		t.Fatalf("users = %d", n)
	}
	srv.Login(t, sandy.email, demoPassword)
	srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: sandy.email, Password: testutil.Password}, nil).Expect(t, http.StatusUnauthorized)
	if itin := findDoc[models.Itinerary](t, srv.Store, store.CollItineraries, bson.M{"_id": seedSoloPlanID}); itin.HostID != existing.UserID {
		t.Fatalf("seeded plans must belong to the adopted account: %s", itin.HostID)
	}

	// Someone else holding a bot's username stops the seed with a clear reason.
	other, cfg2 := seedServer(t)
	taken := testutil.SignupRequest("Not Marin")
	taken.Username = testutil.Ptr("marinokafor")
	other.SignupWith(t, taken)
	if _, err := seedDemo(t.Context(), other.Store, cfg2, nil, other.Clock.Now()); err == nil || !strings.Contains(err.Error(), "@marinokafor") {
		t.Fatalf("username taken: %v", err)
	}
}

func TestDemoDays(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	for _, tc := range []struct {
		now                                  time.Time
		today, tomorrow, yesterday, saturday string
	}{
		{time.Date(2026, 9, 26, 12, 0, 0, 0, ny), "2026-09-26", "2026-09-27", "2026-09-25", "2026-09-19"}, // a Saturday: a week back
		{time.Date(2026, 9, 27, 0, 30, 0, 0, ny), "2026-09-27", "2026-09-28", "2026-09-26", "2026-09-26"}, // Sunday
		{time.Date(2026, 10, 2, 23, 59, 0, 0, ny), "2026-10-02", "2026-10-03", "2026-10-01", "2026-09-26"},
		{time.Date(2026, 9, 27, 3, 30, 0, 0, time.UTC), "2026-09-26", "2026-09-27", "2026-09-25", "2026-09-19"}, // still Saturday in New York
		{time.Date(2026, 10, 31, 20, 0, 0, 0, ny), "2026-10-31", "2026-11-01", "2026-10-30", "2026-10-24"},      // DST ends overnight
	} {
		d := demoDays(tc.now, ny)
		got := []string{d.today.Format("2006-01-02"), d.tomorrow.Format("2006-01-02"), d.yesterday.Format("2006-01-02"), d.lastSaturday.Format("2006-01-02")}
		if !slices.Equal(got, []string{tc.today, tc.tomorrow, tc.yesterday, tc.saturday}) {
			t.Errorf("%s: %v", tc.now, got)
		}
		if d.tomorrow.Hour() != 0 || d.lastSaturday.Weekday() != time.Saturday {
			t.Errorf("%s: days must be local midnights: %v", tc.now, d)
		}
	}
	for _, tc := range []struct{ now, want time.Time }{
		{time.Date(2026, 9, 26, 10, 0, 0, 0, ny), time.Date(2026, 9, 26, 21, 0, 0, 0, ny)},
		{time.Date(2026, 9, 26, 19, 10, 0, 0, ny), time.Date(2026, 9, 26, 22, 30, 0, 0, ny)},
		{time.Date(2026, 9, 26, 23, 0, 0, 0, ny), time.Date(2026, 9, 27, 2, 0, 0, 0, ny)},
	} {
		if got := freeUntil(tc.now, ny); !got.Equal(tc.want) {
			t.Errorf("freeUntil(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
	for _, tc := range []struct {
		cents, n int
		want     []int
	}{{2400, 3, []int{800, 800, 800}}, {900, 3, []int{300, 300, 300}}, {4000, 3, []int{1334, 1333, 1333}}} {
		if got := equalShares(tc.cents, tc.n); !slices.Equal(got, tc.want) {
			t.Errorf("equalShares(%d, %d) = %v", tc.cents, tc.n, got)
		}
	}
}

// TestSeedWorldOpenPlan pins Marin's plan on the shipped Saltlight snapshot.
func TestSeedWorldOpenPlan(t *testing.T) {
	var places []*models.Activity
	for _, a := range readDemoCatalog(t) {
		if a.Kind == "place" {
			places = append(places, &a)
		}
	}
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, ny)
	w, err := buildSeedWorld(seedIDs{sandy: "s", marin: "m", theo: "t"}, places, now, ny)
	if err != nil {
		t.Fatal(err)
	}
	open := w.itineraries[0]
	var titles []string
	for _, stop := range stopsOf(open.Items) {
		titles = append(titles, stop.Title)
	}
	if !slices.Equal(titles, []string{"Seaside Market Hall", "Salvage and Sons Vintage", "Fish Box Karaoke"}) {
		t.Fatalf("open plan stops: %v", titles)
	}
	if !slices.Equal(open.Plan.Tags, []string{"Food", "Music"}) || open.Plan.Budget != 1 {
		t.Fatalf("plan snapshot: %+v", open.Plan)
	}
	if first := open.Items[0]; first.Title != "Walk to Seaside Market Hall" || first.Place.Name != "Seaside Market Square → Seaside Market Hall" {
		t.Fatalf("first leg: %+v", first)
	}
	if last := open.Items[len(open.Items)-1]; last.Title != "Walk to Seaside Market Square" || last.End.After(open.BackBy) {
		t.Fatalf("last leg: %+v", last)
	}
	if !strings.Contains(w.openPlanSummary, "Sun Sep 27 5:30–8 PM") {
		t.Fatalf("summary %q", w.openPlanSummary)
	}
	if post := w.posts[0]; post.Text != "Free until 9 PM near Seaside Market" {
		t.Fatalf("free post text %q", post.Text)
	}
}
