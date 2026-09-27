package main

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// snapshot is every document of the collections a reset reads or writes,
// as stored, by collection and id.
func snapshot(t *testing.T, st *store.Store) map[string]map[string][]byte {
	t.Helper()
	out := map[string]map[string][]byte{}
	for _, coll := range []string{store.CollUsers, store.CollItineraries, store.CollRatings, store.CollItemStates, store.CollJoinRequests,
		store.CollThreads, store.CollMessages, store.CollExpenses, store.CollPhotos, store.CollForumPosts, store.CollPlanTogether,
		store.CollCheckoutIntents, store.CollCheckoutRuns, store.CollCalendarEvents, store.CollFriendships, store.CollFriendRequests,
		store.CollPaymentMethods, collBackups} {
		cursor, err := st.Collection(coll).Find(context.Background(), bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		var raws []bson.Raw
		if err := cursor.All(context.Background(), &raws); err != nil {
			t.Fatal(err)
		}
		out[coll] = map[string][]byte{}
		for _, raw := range raws {
			out[coll][rawKey(raw.Lookup("_id"))] = raw
		}
	}
	return out
}

// diff lists how after differs from before, leaving out skip (ids).
func diff(before, after map[string]map[string][]byte, skip map[string]bool) []string {
	var out []string
	for coll, docs := range before {
		for id, raw := range docs {
			if skip[id] {
				continue
			}
			got, ok := after[coll][id]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s %s is gone", coll, id))
			case !bytes.Equal(got, raw):
				out = append(out, fmt.Sprintf("%s %s changed:\n  was %s\n  now %s", coll, id, bson.Raw(raw), bson.Raw(got)))
			}
		}
	}
	for coll, docs := range after {
		for id := range docs {
			if _, ok := before[coll][id]; !ok && !skip[id] {
				out = append(out, fmt.Sprintf("%s %s is new", coll, id))
			}
		}
	}
	slices.Sort(out)
	return out
}

// seededWorld is what production has after the seeds: the Atlanta
// showcase, and the three people's history and calendar.
func seededWorld(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	withShowcase(t, st, now)
	for _, mode := range []string{"history", "calendar"} {
		o := testOpts(t, now)
		sels, err := parseHistory(everyone)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "history" {
			o.History = sels
		} else {
			o.Calendar = sels
		}
		o.Apply = true
		seed(t, st, o)
	}
}

func resetOpts(t *testing.T, now time.Time, list string, apply bool) Options {
	t.Helper()
	o := testOpts(t, now)
	sels, err := parseHistory(list)
	if err != nil {
		t.Fatal(err)
	}
	o.Reset, o.Apply = sels, apply
	return o
}

func TestCalendarDryRunApplyTwiceRemove(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogsWithin(t, st, 8)
	karthik, bayan, jev := threePeople(t, st)
	// One event of Karthik's own (a connected calendar) stays through everything.
	own := models.CalendarEvent{ID: "google-1", UserID: karthik.ID.Hex(), Title: "Dentist", Start: now, End: now.Add(time.Hour), Source: "google",
		CreatedAt: now, UpdatedAt: now}
	if _, err := st.Collection(store.CollCalendarEvents).InsertOne(ctx, own); err != nil {
		t.Fatal(err)
	}
	o := testOpts(t, now)
	sels, _ := parseHistory(everyone)
	o.Calendar = sels
	out := seed(t, st, o)
	if n, _ := st.Collection(store.CollCalendarEvents).CountDocuments(ctx, bson.M{}); n != 1 {
		t.Fatalf("the dry run wrote %d events", n-1)
	}
	for _, want := range []string{"@lilk", "Semester (outdoors story", "Semester (nightlife story", "Semester (arts story",
		"Tue Thu", "Mon Wed Fri", "once 2026-", "Dry run: nothing was written"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}

	o.Apply = true
	seed(t, st, o)
	classes := map[string]bool{}
	for _, sch := range schedules {
		for _, w := range sch.weekly {
			if w.class {
				classes[w.key] = true
			}
		}
	}
	for _, u := range []*models.User{karthik, bayan, jev} {
		var events []models.CalendarEvent
		if err := findAll(ctx, st, store.CollCalendarEvents, bson.M{fieldSeed: calendarTag, "userId": u.ID.Hex()}, bson.M{}, &events); err != nil {
			t.Fatal(err)
		}
		if len(events) < 150 {
			t.Errorf("@%s has %d events", u.Username, len(events))
		}
		series := map[string]int{}
		for _, e := range events {
			day := e.Start.In(nyTZ)
			if e.Source != "seed" || !e.End.After(e.Start) || day.Before(semesterStart) || day.After(semesterEnd.AddDate(0, 0, 1)) {
				t.Errorf("@%s %s: %+v", u.Username, e.ID, e)
			}
			if e.SeriesID == "" {
				continue
			}
			series[e.SeriesID]++
			if key := strings.TrimPrefix(e.SeriesID, calendarID(u.ID.Hex(), "")); classes[key] && noClasses[day.Format("2006-01-02")] {
				t.Errorf("@%s has class %q on a holiday (%s)", u.Username, e.Title, day.Format("Jan 2"))
			}
		}
		if len(series) < 7 {
			t.Errorf("@%s has %d recurring series", u.Username, len(series))
		}
	}
	// A rerun writes the very same documents.
	first := snapshot(t, st)
	o.Now = now.Add(time.Hour)
	seed(t, st, o)
	if d := diff(first, snapshot(t, st), nil); len(d) > 0 {
		t.Fatalf("the rerun changed:\n%s", strings.Join(d, "\n"))
	}
	events := len(first[store.CollCalendarEvents]) - 1
	// Remove: a dry run deletes nothing, then only the seed's events go.
	o.Remove, o.Apply = true, false
	seed(t, st, o)
	if n, _ := st.Collection(store.CollCalendarEvents).CountDocuments(ctx, bson.M{}); n != int64(events)+1 {
		t.Fatalf("a remove dry run deleted events")
	}
	o.Apply = true
	seed(t, st, o)
	if n, _ := st.Collection(store.CollCalendarEvents).CountDocuments(ctx, bson.M{}); n != 1 {
		t.Fatalf("%d events left, want Karthik's own one", n)
	}
	if n, _ := st.Collection(collBackups).CountDocuments(ctx, bson.M{fieldSeed: calendarTag}); n != 0 {
		t.Errorf("%d calendar marks left", n)
	}
}

func TestResetReturnsToTheBaseline(t *testing.T) {
	srv := testutil.New(t)
	ctx := context.Background()
	st := srv.Store
	catalogsWithin(t, st, 8)
	// Karthik signs up in the app; Bayan and Jev are there already.
	ks := srv.SignupWith(t, contract.SignupRequest{Name: "Karthik Singaravadivelan", Email: "karthik@example.test",
		Password: testutil.Password, Username: testutil.Ptr("lilk")})
	srv.Do(t, "PUT", "/me/preferences", contract.Preferences{Ratings: contract.Ratings{"outdoors": 4, "food": 5, "live_music": 3}}, ks).
		Expect(t, http.StatusOK)
	bayan := realPerson(t, st, "Bayan", "bayan_98d1", map[string]int{"nightlife": 4, "live_music": 5, "sports": 4})
	realPerson(t, st, "Jev", "jev_d9ef", map[string]int{"museums": 4, "shopping": 2, "long_walks": 3})
	kid := ks.UserID
	// A sidequest Karthik made before the seeds: not part of the baseline either.
	before := &models.Itinerary{ID: "karthik-before", HostID: kid, Title: "Late night ramen run", Visibility: models.VisibilityFriends,
		Start: srv.Clock.Now().Add(-9 * 24 * time.Hour), BackBy: srv.Clock.Now().Add(-9*24*time.Hour + 3*time.Hour), Status: models.ItineraryPast}
	if err := st.Itineraries().Insert(ctx, before); err != nil {
		t.Fatal(err)
	}
	seededWorld(t, st, srv.Clock.Now())
	// The baseline is the seeds' world without that old plan.
	if _, err := st.Collection(store.CollItineraries).DeleteOne(ctx, bson.M{"_id": before.ID}); err != nil {
		t.Fatal(err)
	}
	baseline := snapshot(t, st)
	if _, err := st.Collection(store.CollItineraries).InsertOne(ctx, before); err != nil {
		t.Fatal(err)
	}

	// Practice. A new plan of his, with a chat, a ticket and a checkout; Bayan joins it.
	practice := &models.Itinerary{ID: "practice-plan", HostID: kid, Title: "Practice picnic", Visibility: models.VisibilityOpen,
		Start: srv.Clock.Now().Add(26 * time.Hour), BackBy: srv.Clock.Now().Add(29 * time.Hour),
		Items: []models.ItineraryItem{{ID: "practice-stop", Kind: models.ItemStop, Title: "Picnic spot",
			Start: srv.Clock.Now().Add(26 * time.Hour), End: srv.Clock.Now().Add(27 * time.Hour)}}}
	if err := st.Itineraries().Insert(ctx, practice); err != nil {
		t.Fatal(err)
	}
	joined, err := st.Itineraries().AddMember(ctx, practice.ID, bayan.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	chat, _, err := st.Threads().EnsureGroup(ctx, joined)
	if err != nil {
		t.Fatal(err)
	}
	srv.Do(t, "POST", "/threads/"+chat.ID+"/messages", contract.NewMessage{Text: "Bring a blanket"}, ks).Expect(t, http.StatusCreated)
	for _, doc := range []any{
		models.ItemState{ID: models.PairID(kid, "practice-stop"), UserID: kid, ItemID: "practice-stop", ItineraryID: practice.ID,
			Ticket: &models.ItemTicket{ID: "tkt-1", Quantity: 1}, UpdatedAt: srv.Clock.Now()},
		models.ItemState{ID: models.PairID(bayan.ID.Hex(), "practice-stop"), UserID: bayan.ID.Hex(), ItemID: "practice-stop",
			ItineraryID: practice.ID, UpdatedAt: srv.Clock.Now()},
	} {
		if _, err := st.Collection(store.CollItemStates).InsertOne(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	for coll, doc := range map[string]bson.M{
		store.CollCheckoutIntents: {"_id": "practice-intent", "userId": kid, "itemId": "practice-stop", "itineraryId": practice.ID},
		store.CollCheckoutRuns:    {"_id": "practice-run", "userId": kid, "itineraryId": practice.ID},
		store.CollCalendarEvents:  {"_id": "practice-event", "userId": kid, "title": "Made in the app", "source": "app"},
	} {
		if _, err := st.Collection(coll).InsertOne(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	// He joins a showcase plan and says hi in its chat.
	join := decode[contract.JoinResult](t, srv.Do(t, "POST", "/forum/posts/showcase-atl-plan-crawl/join-requests", nil, ks).Expect(t, http.StatusOK))
	if join.Status != contract.JoinJoined || join.ThreadID == nil {
		t.Fatalf("join: %+v", join)
	}
	srv.Do(t, "POST", "/threads/"+*join.ThreadID+"/messages", contract.NewMessage{Text: "Saving a seat for me too?"}, ks).
		Expect(t, http.StatusCreated)
	// He rates stops the baseline leaves unrated, and says he is free now.
	toRate := decode[contract.Page[contract.PastEvent]](t, srv.Do(t, "GET", "/me/past-events?unrated=true", nil, ks).Expect(t, http.StatusOK))
	if len(toRate.Items) < 2 {
		t.Fatalf("%d stops to rate", len(toRate.Items))
	}
	for _, e := range toRate.Items[:2] {
		srv.Do(t, "PUT", "/ratings/"+e.ID, contract.Rating{Stars: 2, Tags: []string{"Too crowded"}}, ks).Expect(t, http.StatusNoContent)
	}
	lat, lng := 33.7767, -84.3895
	srv.Do(t, "POST", "/forum/posts", contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone, Lat: &lat, Lng: &lng}, ks).
		Expect(t, http.StatusCreated)
	// Never touched: a friend request, a direct message, a card.
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: bayan.ID.Hex()}, ks).Expect(t, http.StatusCreated)
	dm := decode[contract.ChatThread](t, srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: bayan.ID.Hex()}, ks).Expect(t, http.StatusOK))
	sent := decode[contract.Message](t, srv.Do(t, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: "Hey"}, ks).Expect(t, http.StatusCreated))
	srv.Do(t, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: "tok_visa_4242"}, ks).Expect(t, http.StatusCreated)
	var keep []string
	for _, coll := range []string{store.CollFriendRequests, store.CollPaymentMethods} {
		var ids []string
		if err := st.Collection(coll).Distinct(ctx, "_id", bson.M{}).Decode(&ids); err != nil {
			t.Fatal(err)
		}
		keep = append(keep, ids...)
	}
	keep = append(keep, dm.ID, sent.ID)

	// The dry run lists it all and changes nothing.
	practiced := snapshot(t, st)
	out := seed(t, st, resetOpts(t, srv.Clock.Now(), "@lilk", false))
	if d := diff(practiced, snapshot(t, st), nil); len(d) > 0 {
		t.Fatalf("the reset dry run changed:\n%s", strings.Join(d, "\n"))
	}
	var crawl models.Itinerary
	if err := st.Collection(store.CollItineraries).FindOne(ctx, bson.M{"_id": "showcase-atl-plan-crawl"}).Decode(&crawl); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"“Late night ramen run”", "“Practice picnic”", "Bayan loses it", "“" + crawl.Title + "”",
		"2 messages", "2 ratings", "1 “free now” posts", "1 calendar events not from the seed", "Back to the baseline: 5 history plans",
		"Default upcoming sidequests (made again with fresh dates on every --apply and --reset): 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run lacks %q:\n%s", want, out)
		}
	}

	seed(t, st, resetOpts(t, srv.Clock.Now(), "@lilk", true))
	skip := map[string]bool{}
	for _, id := range keep {
		skip[id] = true
	}
	if d := diff(baseline, snapshot(t, st), skip); len(d) > 0 {
		t.Fatalf("after the reset, not the baseline:\n%s", strings.Join(d, "\n"))
	}
	// Home is not empty: the default upcoming sidequests, one theirs, one a showcase person's.
	home := decode[[]contract.Itinerary](t, srv.Do(t, "GET", "/itineraries", nil, ks).Expect(t, http.StatusOK))
	hosting, joinedOne := 0, 0
	for _, it := range home {
		if !strings.HasPrefix(it.ID, baselineTag+"-"+kid+"-") || !it.Start.After(srv.Clock.Now()) || it.Start.After(srv.Clock.Now().Add(5*24*time.Hour)) {
			t.Errorf("Home has %s %q at %v", it.ID, it.Title, it.Start.Time)
		}
		if it.IsHost {
			hosting++
		} else {
			joinedOne++
		}
	}
	if hosting != 1 || joinedOne != 1 {
		t.Errorf("Home after the reset: %d hosted, %d joined: %+v", hosting, joinedOne, home)
	}
	// What was never to be touched is there, as it was.
	after := snapshot(t, st)
	for _, id := range keep {
		found := false
		for _, docs := range after {
			if raw, ok := docs[id]; ok {
				found = true
				for _, prev := range practiced {
					if old, ok := prev[id]; ok && !bytes.Equal(old, raw) {
						t.Errorf("%s changed", id)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s is gone", id)
		}
	}
	// A second reset has nothing left to do.
	out = seed(t, st, resetOpts(t, srv.Clock.Now(), "@lilk", false))
	if !strings.Contains(out, "deleted with everything on them: 0") || !strings.Contains(out, "Also deleted: nothing") {
		t.Errorf("a second reset still finds work:\n%s", out)
	}
}

func TestResetLeavesOtherPeoplesPlans(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogsWithin(t, st, 8)
	karthik, bayan, _ := threePeople(t, st)
	seededWorld(t, st, now)
	kid, bid := karthik.ID.Hex(), bayan.ID.Hex()
	theirs := &models.Itinerary{ID: "bayan-plan", HostID: bid, Title: "Bayan's own plan", Visibility: models.VisibilityFriends,
		Start: now.Add(24 * time.Hour), BackBy: now.Add(27 * time.Hour)}
	if err := st.Itineraries().Insert(ctx, theirs); err != nil {
		t.Fatal(err)
	}
	chat, _, err := st.Threads().EnsureGroup(ctx, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ratings().Upsert(ctx, models.Rating{UserID: bid, ItemID: "their-item", ItineraryID: theirs.ID, Stars: 5}); err != nil {
		t.Fatal(err)
	}
	baseline := snapshot(t, st)
	// Karthik joins Bayan's plan (and its chat), and leaves a note there.
	if _, err := st.Collection(store.CollItineraries).UpdateOne(ctx, bson.M{"_id": theirs.ID}, bson.M{"$push": bson.M{"memberIds": kid}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collection(store.CollThreads).UpdateOne(ctx, bson.M{"_id": chat.ID},
		bson.M{"$push": bson.M{"memberIds": kid}, "$set": bson.M{"unread." + kid: 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collection(store.CollItemStates).InsertOne(ctx, models.ItemState{ID: models.PairID(kid, "their-item"), UserID: kid,
		ItemID: "their-item", ItineraryID: theirs.ID}); err != nil {
		t.Fatal(err)
	}
	out := seed(t, st, resetOpts(t, now, "@lilk", true))
	if !strings.Contains(out, "Other people's plans they are in, left: 1") {
		t.Errorf("the reset should leave Bayan's plan:\n%s", out)
	}
	if d := diff(baseline, snapshot(t, st), nil); len(d) > 0 {
		t.Fatalf("Bayan's plan or anyone else's data changed:\n%s", strings.Join(d, "\n"))
	}
}

func TestResetRefusals(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	st := testutil.Store(t, func() time.Time { return now })
	ctx := context.Background()
	catalogsWithin(t, st, 8)
	withShowcase(t, st, now)
	threePeople(t, st)
	sandy := makeSandy(t, st)
	bot := &models.User{Email: "bot@example.test", Name: "Some Bot", Username: "somebot", Roles: []string{"bot"}}
	if err := st.Users().Create(ctx, bot); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		list      string
		allowDemo bool
		want      string
	}{
		{"@nobody", false, "@nobody matches 0 accounts"},
		{"@sandybyte", false, "@sandybyte is in the demo cast"},
		{"@somebot", true, "@somebot is in the demo cast"},
		{"@hana.kim", true, "@hana.kim is a seeded showcase account"},
		{"@sandybyte", true, ""},
	} {
		o := resetOpts(t, now, c.list, false)
		o.AllowDemo = c.allowDemo
		err := Run(ctx, st, o, &bytes.Buffer{})
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s with --allow-demo: %v", c.list, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v, want %q", c.list, err, c.want)
		}
	}
	for _, args := range [][]string{
		{"--reset", "@lilk=arts"}, {"--reset", "@lilk", "--remove"}, {"--allow-demo"}, {"--reset", "@lilk", "--history", "@lilk"},
		{"--calendar", "@lilk", "--world", "atlanta"},
	} {
		if code := cli(args, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
			t.Errorf("%v exits %d, want 2", args, code)
		}
	}
	_ = sandy
}

func TestResetTheDemoAccount(t *testing.T) {
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) { c.DemoDate = demoDate }))
	ctx := context.Background()
	st := srv.Store
	catalogs(t, st)
	sandy := srv.SignupWith(t, contract.SignupRequest{Name: "Sandy Byte", Email: defaultDemoEmail, Password: testutil.Password,
		Username: testutil.Ptr("sandybyte")})
	if _, err := st.Users().Update(ctx, sandy.UserID, bson.M{"roles": []string{"demo"}, "city": "saltlight",
		"homeBase": models.HomeBase{Name: "Seaside Market Square", Lat: seasideSquare.Lat, Lng: seasideSquare.Lng}}); err != nil {
		t.Fatal(err)
	}
	o := testOpts(t, srv.Clock.Now())
	o.Apply = true
	seed(t, st, o)
	baseline := snapshot(t, st)
	// The walkthrough: she reads her Bowling night chat and joins the ferry.
	srv.Do(t, "POST", "/threads/showcase-slt-chat-bowling/read", nil, sandy).Expect(t, http.StatusNoContent)
	srv.Do(t, "POST", "/forum/posts/showcase-slt-plan-ferry/join-requests", nil, sandy).Expect(t, http.StatusOK)
	reset := resetOpts(t, srv.Clock.Now(), "@sandybyte", true)
	if err := Run(ctx, st, reset, &bytes.Buffer{}); err == nil {
		t.Fatal("the demo account was reset without --allow-demo")
	}
	reset.AllowDemo = true
	seed(t, st, reset)
	if d := diff(baseline, snapshot(t, st), nil); len(d) > 0 {
		t.Fatalf("the demo account is not back to the showcase as seeded:\n%s", strings.Join(d, "\n"))
	}
}
