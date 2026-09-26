package itineraries_test

import (
	"Backend/pkg/api"
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"Backend/pkg/travel"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const walkNote = "Route options below. Times update live if you run late."

func utc(hhmm string, day int) time.Time {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 9, day, t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// TestCreateFromContractExample saves docs/api/examples/CreateItineraryRequest.json
// and checks the items against the mock server's materialization rules.
func TestCreateFromContractExample(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Create")
	var it contract.Itinerary
	srv.Do(t, "POST", "/itineraries", json.RawMessage(readExample(t, "CreateItineraryRequest")), a).
		Expect(t, http.StatusCreated).JSON(t, &it)

	if it.ID == "" || it.Title != "Rooftop + murals" || !it.IsHost || it.GoingCount != 1 ||
		it.Visibility != contract.VisibilityFriends || it.MaxGroupSize == nil || *it.MaxGroupSize != 6 ||
		it.LockAt == nil || !it.LockAt.Equal(utc("17:30", 25)) {
		t.Fatalf("itinerary header wrong: %+v", it)
	}
	if !it.Date.Equal(time.Date(2026, 9, 25, 4, 0, 0, 0, time.UTC)) || !it.Start.Equal(utc("18:10", 25)) || !it.BackBy.Equal(utc("22:30", 25)) {
		t.Fatalf("date %s start %s back_by %s", it.Date, it.Start, it.BackBy)
	}
	if it.StartPlace.Name != "Tech Square (current location)" || it.StartPlace.Coordinate == nil || it.EndPlace.Name != "Home · North Ave Apts" {
		t.Fatalf("places: %+v %+v", it.StartPlace, it.EndPlace)
	}
	want := []struct {
		kind        contract.BlockKind
		title       string
		place       string
		coordinate  bool
		start, end  string
		description string
	}{
		{contract.KindTransit, "MARTA to Skyline Park rooftop", "Skyline Park rooftop", false, "18:10", "18:24", walkNote},
		{contract.KindSidequest, "Skyline Park rooftop", "Skyline Park rooftop", true, "18:24", "19:44", "Games + views · $"},
		{contract.KindTransit, "MARTA to Krog Street Tunnel murals", "Krog Street Tunnel murals", false, "19:44", "19:58", walkNote},
		{contract.KindSidequest, "Krog Street Tunnel murals", "Krog Street Tunnel murals", true, "19:58", "20:58", "Street art · Free"},
		{contract.KindTransit, "Walk to Krog Street Market", "Krog Street Market", false, "20:58", "21:01", walkNote},
		{contract.KindSidequest, "Krog Street Market", "Krog Street Market", true, "21:01", "21:36", "Food hall · $"},
		{contract.KindTransit, "MARTA to Home · North Ave Apts", "Home · North Ave Apts", false, "21:36", "21:57", walkNote},
	}
	if len(it.Items) != len(want) {
		t.Fatalf("%d items, want %d: %+v", len(it.Items), len(want), it.Items)
	}
	ids := map[string]bool{}
	for i, w := range want {
		got := it.Items[i]
		if got.Kind != w.kind || got.Title != w.title || got.Place == nil || got.Place.Name != w.place ||
			(got.Place.Coordinate != nil) != w.coordinate || !got.Start.Equal(utc(w.start, 25)) || !got.End.Equal(utc(w.end, 25)) ||
			got.Description == nil || *got.Description != w.description {
			t.Errorf("item %d = %+v (place %+v), want %+v", i, got, got.Place, w)
		}
		if len(got.People) != 0 || len(got.Interested) != 0 || got.ExtraGoing != 0 || got.Bookable || got.PriceCents != nil ||
			got.Notes != nil || got.Rating != nil || got.TransitMode != nil || got.Ticket != nil {
			t.Errorf("item %d carries extras it should not: %+v", i, got)
		}
		if got.ID == "" || ids[got.ID] {
			t.Errorf("item %d id %q empty or repeated", i, got.ID)
		}
		ids[got.ID] = true
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("a friends-visible plan broadcasts forum.update once, got %d", forumUpdates(srv))
	}

	doc, err := srv.Store.Itineraries().Get(context.Background(), it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.HostID != a.UserID || len(doc.MemberIDs) != 1 || doc.DateKey != "2026-09-25" || doc.TZ != testutil.TimeZone ||
		doc.OptionID != "opt-a" || doc.RouteMode != "marta" || doc.Status != models.ItineraryActive ||
		doc.Plan == nil || doc.Plan.MoodText == "" || doc.Plan.Budget != 1 || len(doc.Plan.Modes) != 2 {
		t.Fatalf("stored document: %+v", doc)
	}
	if doc.Items[0].Kind != models.ItemTransit || doc.Items[0].LegMode != "marta" || doc.Items[0].LegMinutes != 14 ||
		doc.Items[1].Kind != models.ItemStop || doc.Items[1].StopID != "opt-a-0" || doc.Items[1].DurationMin != 80 {
		t.Fatalf("stored items: %+v %+v", doc.Items[0], doc.Items[1])
	}

	// A private plan is not announced to the forum.
	srv.Events.Reset()
	req := exampleRequest(t)
	req.Visibility = contract.VisibilityJustMe
	create(t, srv, a, req)
	if forumUpdates(srv) != 0 {
		t.Error("a just_me plan must not broadcast forum.update")
	}
}

func TestCreateEnrichesStopsThroughThePlanner(t *testing.T) {
	price, site := 1800, "https://skyline.example"
	planner := fakePlanner{details: map[string]*api.StopDetail{
		"opt-a-0": {ActivityID: "64b000000000000000000001", PriceCents: &price, WebsiteURL: &site, Bookable: true, DurationMin: 999},
	}}
	srv := testutil.New(t, testutil.WithNow(exampleClock), testutil.WithPlanner(planner))
	a := srv.Signup(t, "Alice Planner")
	req := exampleRequest(t)
	own := "64b000000000000000000003"
	req.Option.Stops[2].ActivityID = &own
	it := create(t, srv, a, req)
	s := stops(it)
	if s[0].ActivityID == nil || *s[0].ActivityID != "64b000000000000000000001" || s[0].PriceCents == nil || *s[0].PriceCents != 1800 ||
		s[0].WebsiteURL == nil || *s[0].WebsiteURL != site || !s[0].Bookable {
		t.Fatalf("resolved stop not enriched: %+v", s[0])
	}
	if s[1].ActivityID != nil || s[1].PriceCents != nil || s[1].WebsiteURL != nil || s[1].Bookable {
		t.Fatalf("an unresolved stop keeps the option's data: %+v", s[1])
	}
	if s[2].ActivityID == nil || *s[2].ActivityID != own {
		t.Fatalf("the option's own activity_id is kept: %+v", s[2])
	}
	// Times and visit lengths come from the request, never from the planner.
	if !s[0].Start.Equal(utc("18:24", 25)) || !s[0].End.Equal(utc("19:44", 25)) {
		t.Fatalf("resolved stop time: %s–%s", s[0].Start, s[0].End)
	}
	doc, err := srv.Store.Itineraries().Get(context.Background(), it.ID)
	if err != nil || doc.Items[1].DurationMin != 80 || doc.Items[1].ActivityID != "64b000000000000000000001" {
		t.Fatalf("stored stop: %v %+v", err, doc.Items[1])
	}
}

func TestCreateValidation(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Invalid")
	cases := []struct {
		name string
		edit func(*contract.CreateItineraryRequest)
		msg  string
	}{
		{"unknown stop", func(r *contract.CreateItineraryRequest) { r.StopOrder = []string{"opt-a-0", "opt-z-9"} }, itineraries.MsgPlanChanged},
		{"repeated stop", func(r *contract.CreateItineraryRequest) { r.StopOrder = []string{"opt-a-0", "opt-a-0"} }, itineraries.MsgPlanChanged},
		{"no stops", func(r *contract.CreateItineraryRequest) { r.StopOrder = []string{} }, itineraries.MsgNoStops},
		{"route misses stops", func(r *contract.CreateItineraryRequest) { r.Route.StopTimes = r.Route.StopTimes[:1] }, itineraries.MsgPlanChanged},
		{"unknown leg mode", func(r *contract.CreateItineraryRequest) { r.Route.Legs[0].Mode = "teleport" }, itineraries.MsgPlanChanged},
		{"back by at the start", func(r *contract.CreateItineraryRequest) { r.Plan.BackBy = r.Plan.StartTime }, itineraries.MsgWindow},
		{"unknown visibility", func(r *contract.CreateItineraryRequest) { r.Visibility = "everyone" }, httpx.GenericBadRequest},
		{"group of one", func(r *contract.CreateItineraryRequest) { one := 1; r.MaxGroupSize = &one }, itineraries.MsgGroupSize},
		{"stop ends before it starts", func(r *contract.CreateItineraryRequest) {
			r.Route.StopTimes[1].End = contract.NewTime(r.Route.StopTimes[1].Start.Add(-time.Minute))
		}, itineraries.MsgPlanChanged},
		{"unknown pace", func(r *contract.CreateItineraryRequest) { r.Plan.Pace = "chill" }, httpx.GenericBadRequest},
		{"budget out of range", func(r *contract.CreateItineraryRequest) { r.Plan.Budget = 4 }, httpx.GenericBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := exampleRequest(t)
			c.edit(&req)
			res := srv.Do(t, "POST", "/itineraries", req, a).Expect(t, http.StatusBadRequest)
			if res.Message() != c.msg {
				t.Fatalf("message %q, want %q", res.Message(), c.msg)
			}
		})
	}
	srv.DoRaw(t, "POST", "/itineraries", nil, map[string]string{"Authorization": a.Bearer()}).Expect(t, http.StatusBadRequest)
	srv.Do(t, "POST", "/itineraries", exampleRequest(t), nil).Expect(t, http.StatusUnauthorized)
	var list []contract.Itinerary
	srv.Do(t, "GET", "/itineraries", nil, a).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 0 {
		t.Fatalf("rejected plans were stored: %d", len(list))
	}
}

func TestListGetAndScoping(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice List")
	b := srv.Signup(t, "Bob List")
	evening := create(t, srv, a, exampleRequest(t))
	morning := create(t, srv, a, plan("Morning walk", contract.VisibilityJustMe,
		stopSpec{id: "w0", title: "Piedmont Park loop", start: at(9, 25, 9, 30), minutes: 60}))

	var list []contract.Itinerary
	srv.Do(t, "GET", "/itineraries?status=active", nil, a).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 2 || list[0].ID != morning.ID || list[1].ID != evening.ID || !list[0].IsHost {
		t.Fatalf("active list (soonest first): %+v", list)
	}
	srv.Do(t, "GET", "/itineraries", nil, b).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 0 {
		t.Fatalf("B sees A's plans: %+v", list)
	}
	srv.Do(t, "GET", "/itineraries/"+evening.ID, nil, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/itineraries/nope", nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/itineraries?status=soon", nil, a).Expect(t, http.StatusBadRequest)

	// A member sees the plan as a guest: a group with both of them.
	addMember(t, srv, evening.ID, b)
	guest := getItinerary(t, srv, b, evening.ID)
	s := stops(guest)
	if guest.IsHost || guest.GoingCount != 2 || s[0].Kind != contract.KindGroup || len(s[0].People) != 2 ||
		s[0].People[0].ID != a.UserID || s[0].People[1].ID != b.UserID || s[0].People[0].Initials != "AL" ||
		guest.Items[0].Kind != contract.KindTransit || len(guest.Items[0].People) != 0 {
		t.Fatalf("guest view: %+v", guest)
	}

	// Once back_by passes the plan is past: off the active list, still readable.
	srv.Clock.Set(utc("22:31", 25))
	a = srv.Login(t, a.Email, a.Password) // the old access token expired with the clock move
	srv.Do(t, "GET", "/itineraries", nil, a).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 0 {
		t.Fatalf("ended plans are not active: %+v", list)
	}
	srv.Do(t, "GET", "/itineraries?status=past", nil, a).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 2 || list[0].ID != evening.ID || list[1].ID != morning.ID {
		t.Fatalf("past list (most recent first): %+v", list)
	}
	getItinerary(t, srv, a, evening.ID)
}

func TestPatchRetimesAndIsHostOnly(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Patch")
	b := srv.Signup(t, "Bob Patch")
	it := create(t, srv, a, exampleRequest(t))
	addMember(t, srv, it.ID, b)
	s := stops(it)
	rooftop, market := s[0], s[2]
	path := "/itineraries/" + it.ID

	if res := srv.Do(t, "PATCH", path, map[string]any{"title": "Mine"}, b).Expect(t, http.StatusForbidden); res.Message() != itineraries.MsgHostOnly {
		t.Fatalf("member edit: %s", res.Body)
	}
	for body, msg := range map[string]string{
		`{"title":"   "}`:                           itineraries.MsgNeedTitle,
		`{"stop_order":[]}`:                         itineraries.MsgNoStops,
		`{"stop_order":["nope"]}`:                   itineraries.MsgNoStops,
		`{"back_by":"2026-09-25T18:00:00Z"}`:        itineraries.MsgWindow,
		`{"visibility":"everyone"}`:                 httpx.GenericBadRequest,
		`{"stop_order":["` + it.Items[0].ID + `"]}`: itineraries.MsgNoStops, // a transit leg is not a stop
	} {
		if res := srv.Do(t, "PATCH", path, json.RawMessage(body), a).Expect(t, http.StatusBadRequest); res.Message() != msg {
			t.Errorf("%s → %q, want %q", body, res.Message(), msg)
		}
	}
	srv.Do(t, "PATCH", "/itineraries/nope", map[string]any{"title": "x"}, a).Expect(t, http.StatusNotFound)

	// Reorder and drop the murals: each kept stop follows a walk leg from the previous place.
	srv.Events.Reset()
	var got contract.Itinerary
	srv.Do(t, "PATCH", path, map[string]any{"title": "Rooftop evening", "stop_order": []string{market.ID, rooftop.ID}}, a).
		Expect(t, http.StatusOK).JSON(t, &got)
	walk := func(from, to *contract.Coordinate) time.Duration {
		return travel.Estimate(travel.Point{Lat: from.Lat, Lng: from.Lng}, travel.Point{Lat: to.Lat, Lng: to.Lng}, travel.Walk).Duration.Truncate(time.Minute)
	}
	leg1 := walk(it.StartPlace.Coordinate, market.Place.Coordinate)
	leg2 := walk(market.Place.Coordinate, rooftop.Place.Coordinate)
	if leg1 < 20*time.Minute || leg2 < 20*time.Minute {
		t.Fatalf("estimates look wrong: %s %s", leg1, leg2)
	}
	start := utc("18:10", 25)
	marketAt := start.Add(leg1)
	rooftopAt := marketAt.Add(35 * time.Minute).Add(leg2)
	wantItems := []struct {
		id, title, place string
		start, end       time.Time
	}{
		{"", "Walk to Krog Street Market", "Tech Square (current location) → Krog Street Market", start, marketAt},
		{market.ID, "Krog Street Market", "Krog Street Market", marketAt, marketAt.Add(35 * time.Minute)},
		{"", "Walk to Skyline Park rooftop", "Krog Street Market → Skyline Park rooftop", marketAt.Add(35 * time.Minute), rooftopAt},
		{rooftop.ID, "Skyline Park rooftop", "Skyline Park rooftop", rooftopAt, rooftopAt.Add(80 * time.Minute)},
	}
	if got.Title != "Rooftop evening" || len(got.Items) != len(wantItems) {
		t.Fatalf("patched: %+v", got)
	}
	for i, w := range wantItems {
		item := got.Items[i]
		if (w.id != "" && item.ID != w.id) || item.Title != w.title || item.Place == nil || item.Place.Name != w.place ||
			!item.Start.Equal(w.start) || !item.End.Equal(w.end) {
			t.Errorf("item %d = %s %q %v %s–%s, want %+v", i, item.ID, item.Title, item.Place, item.Start, item.End, w)
		}
	}
	if got.Items[0].Kind != contract.KindTransit || got.Items[1].Kind != contract.KindGroup || len(got.Items[1].People) != 2 {
		t.Errorf("kinds after retime: %+v", got.Items)
	}
	for _, id := range []string{a.UserID, b.UserID} {
		updates := itineraryEvents(t, srv, id, realtime.EventItineraryUpdated)
		if len(updates) != 1 || updates[0].Title != "Rooftop evening" || updates[0].IsHost != (id == a.UserID) {
			t.Errorf("itinerary.updated for %s: %+v", id, updates)
		}
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("a posted plan's edit refreshes the forum: %d", forumUpdates(srv))
	}

	// A later start moves everything and keeps the legs (and travel choices on them).
	var later contract.Itinerary
	srv.Do(t, "PATCH", path, map[string]any{"start": utc("18:40", 25)}, a).Expect(t, http.StatusOK).JSON(t, &later)
	for i := range got.Items {
		if later.Items[i].ID != got.Items[i].ID || !later.Items[i].Start.Equal(got.Items[i].Start.Add(30*time.Minute)) {
			t.Errorf("item %d after start change: %s %s (was %s %s)", i, later.Items[i].ID, later.Items[i].Start, got.Items[i].ID, got.Items[i].Start)
		}
	}

	// A new day moves the plan (lock time too) by whole days.
	var moved contract.Itinerary
	srv.Do(t, "PATCH", path, map[string]any{"date": time.Date(2026, 9, 26, 4, 0, 0, 0, time.UTC)}, a).
		Expect(t, http.StatusOK).JSON(t, &moved)
	if !moved.Date.Equal(time.Date(2026, 9, 26, 4, 0, 0, 0, time.UTC)) || !moved.Start.Equal(later.Start.Add(24*time.Hour)) ||
		!moved.BackBy.Equal(later.BackBy.Add(24*time.Hour)) || moved.LockAt == nil || !moved.LockAt.Equal(utc("17:30", 26)) {
		t.Fatalf("moved: date %s start %s back_by %s lock %v", moved.Date, moved.Start, moved.BackBy, moved.LockAt)
	}
	for i := range later.Items {
		if !moved.Items[i].Start.Equal(later.Items[i].Start.Add(24 * time.Hour)) {
			t.Errorf("item %d not moved a day: %s", i, moved.Items[i].Start)
		}
	}
	doc, err := srv.Store.Itineraries().Get(context.Background(), it.ID)
	if err != nil || doc.DateKey != "2026-09-26" {
		t.Fatalf("stored day: %v %+v", err, doc)
	}

	// Making it private refreshes the forum once more; editing a private plan does not.
	srv.Events.Reset()
	srv.Do(t, "PATCH", path, map[string]any{"visibility": "just_me"}, a).Expect(t, http.StatusOK)
	srv.Do(t, "PATCH", path, map[string]any{"title": "Quiet evening"}, a).Expect(t, http.StatusOK)
	if forumUpdates(srv) != 1 {
		t.Errorf("forum.update after going private: %d, want 1", forumUpdates(srv))
	}
}

// seedGroup gives an itinerary a group thread with messages, an expense, a
// photo and join records, plus an unrelated thread that must survive.
func seedGroup(t *testing.T, srv *testutil.Server, itineraryID string, members ...*testutil.Session) (threadID, otherThread string) {
	t.Helper()
	ctx := context.Background()
	now := srv.Clock.Now()
	var ids []string
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	threadID, otherThread = store.NewID(), store.NewID()
	insert := func(coll string, docs ...any) {
		if _, err := srv.Store.Collection(coll).InsertMany(ctx, docs); err != nil {
			t.Fatal(err)
		}
	}
	insert(store.CollThreads,
		models.Thread{ID: threadID, IsGroup: true, Title: "Crew", MemberIDs: ids, ItineraryID: itineraryID, CreatedBy: ids[0],
			LastMessageAt: now, Unread: map[string]int{}, ReadAt: map[string]time.Time{}, CreatedAt: now, UpdatedAt: now},
		models.Thread{ID: otherThread, IsGroup: false, MemberIDs: ids[:2], DMKey: models.DMKey(ids[0], ids[1]), CreatedBy: ids[0],
			LastMessageAt: now, Unread: map[string]int{}, ReadAt: map[string]time.Time{}, CreatedAt: now, UpdatedAt: now})
	insert(store.CollMessages,
		models.Message{ID: store.NewID(), ThreadID: threadID, SenderID: ids[0], Text: "grabbing a table", SentAt: now},
		models.Message{ID: store.NewID(), ThreadID: threadID, SenderID: ids[1], Text: "omw", SentAt: now},
		models.Message{ID: store.NewID(), ThreadID: otherThread, SenderID: ids[1], Text: "hey", SentAt: now})
	insert(store.CollExpenses, models.Expense{ID: store.NewID(), GroupID: threadID, What: "Dumplings", AmountCents: 4200, PayerID: ids[0],
		SplitAmong: ids, Shares: []int{2100, 2100}, CreatedBy: ids[0], CreatedAt: now})
	var joins []any
	for _, id := range ids[1:] {
		joins = append(joins, models.JoinRequest{ID: store.NewID(), PostID: itineraryID, ItineraryID: itineraryID, UserID: id,
			Status: models.JoinAccepted, CreatedAt: now, UpdatedAt: now})
	}
	insert(store.CollJoinRequests, joins...)
	insert(store.CollPlanTogether, models.PlanTogether{ID: models.PairID(itineraryID, ids[1]), PostID: itineraryID, UserID: ids[1],
		ThreadID: otherThread, CreatedAt: now})
	for _, group := range []string{threadID, otherThread} {
		if err := srv.Store.Photos().Put(ctx, &models.Photo{OwnerID: ids[0], Kind: models.PhotoKindGroup, GroupID: group,
			ContentType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff}}); err != nil {
			t.Fatal(err)
		}
	}
	return threadID, otherThread
}

func count(t *testing.T, srv *testutil.Server, coll string, filter bson.M) int64 {
	t.Helper()
	n, err := srv.Store.Collection(coll).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteRemovesThePlanAndItsGroup(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Delete")
	b := srv.Signup(t, "Bob Delete")
	req := exampleRequest(t)
	req.Visibility = contract.VisibilityOpen
	it := create(t, srv, a, req)
	addMember(t, srv, it.ID, b)
	thread, other := seedGroup(t, srv, it.ID, a, b)
	path := "/itineraries/" + it.ID

	if res := srv.Do(t, "DELETE", path, nil, b).Expect(t, http.StatusForbidden); res.Message() != itineraries.MsgMemberDelete {
		t.Fatalf("member delete: %s", res.Body)
	}
	srv.Events.Reset()
	srv.Do(t, "DELETE", path, nil, a).Expect(t, http.StatusNoContent)

	for _, id := range []string{a.UserID, b.UserID} {
		removed := 0
		for _, e := range srv.Events.Of(realtime.EventItineraryRemoved) {
			var data realtime.ItineraryRemovedData
			testutil.EventData(t, e, &data)
			if e.UserID == id && data.ItineraryID == it.ID {
				removed++
			}
		}
		if removed != 1 {
			t.Errorf("itinerary.removed for %s: %d", id, removed)
		}
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("forum.update after deleting an open plan: %d", forumUpdates(srv))
	}
	if count(t, srv, store.CollThreads, bson.M{"_id": thread}) != 0 || count(t, srv, store.CollMessages, bson.M{"threadId": thread}) != 0 ||
		count(t, srv, store.CollExpenses, bson.M{"groupId": thread}) != 0 || count(t, srv, store.CollPhotos, bson.M{"groupId": thread}) != 0 ||
		count(t, srv, store.CollJoinRequests, bson.M{"itineraryId": it.ID}) != 0 || count(t, srv, store.CollPlanTogether, bson.M{"postId": it.ID}) != 0 {
		t.Error("the group thread and its content must go with the plan")
	}
	if count(t, srv, store.CollThreads, bson.M{"_id": other}) != 1 || count(t, srv, store.CollMessages, bson.M{"threadId": other}) != 1 ||
		count(t, srv, store.CollPhotos, bson.M{"groupId": other}) != 1 {
		t.Error("an unrelated thread was touched")
	}
	srv.Do(t, "GET", path, nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", path, nil, b).Expect(t, http.StatusNotFound)
	srv.Do(t, "DELETE", path, nil, a).Expect(t, http.StatusNotFound)
	if n := count(t, srv, store.CollItineraries, bson.M{"_id": it.ID, "status": models.ItineraryDeleted}); n != 1 {
		t.Errorf("the itinerary is kept as deleted: %d", n)
	}
}

// socialRecorder is a fake social area: it records thread changes and
// answers searches with one post.
type socialRecorder struct {
	mu      sync.Mutex
	threads []string
}

func (s *socialRecorder) ThreadChanged(_ context.Context, _ *api.Deps, threadID string, _ *time.Location) {
	s.mu.Lock()
	s.threads = append(s.threads, threadID)
	s.mu.Unlock()
}

func (s *socialRecorder) SearchPosts(_ context.Context, _ *api.Deps, _ *models.User, q string, _ *time.Location) ([]contract.ForumPost, error) {
	title := "Found: " + q
	return []contract.ForumPost{{ID: "post-1", Type: contract.PostPlan, Title: &title, Tags: []string{}, Going: []contract.PersonRef{}, JoinStatus: contract.JoinNone}}, nil
}

func (s *socialRecorder) changed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.threads...)
}

func TestLeave(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	a := srv.Signup(t, "Alice Leave")
	b := srv.Signup(t, "Bob Leave")
	c := srv.Signup(t, "Cara Leave")
	d := srv.Signup(t, "Dan Leave")
	it := create(t, srv, a, exampleRequest(t))
	addMember(t, srv, it.ID, b)
	addMember(t, srv, it.ID, c)
	thread, _ := seedGroup(t, srv, it.ID, a, b, c)
	social := &socialRecorder{}
	itineraries.UseSocial(social)
	t.Cleanup(func() { itineraries.UseSocial(nil) })
	path := "/itineraries/" + it.ID + "/leave"

	if res := srv.Do(t, "POST", path, nil, a).Expect(t, http.StatusBadRequest); res.Message() != itineraries.MsgHostLeave {
		t.Fatalf("host leave: %s", res.Body)
	}
	srv.Do(t, "POST", path, nil, d).Expect(t, http.StatusNotFound)

	srv.Events.Reset()
	srv.Do(t, "POST", path, nil, b).Expect(t, http.StatusNoContent)
	srv.Do(t, "GET", "/itineraries/"+it.ID, nil, b).Expect(t, http.StatusNotFound)
	if left := getItinerary(t, srv, a, it.ID); left.GoingCount != 2 {
		t.Fatalf("going_count after a leave: %d", left.GoingCount)
	}
	var th models.Thread
	if err := srv.Store.Collection(store.CollThreads).FindOne(context.Background(), bson.M{"_id": thread}).Decode(&th); err != nil {
		t.Fatal(err)
	}
	if len(th.MemberIDs) != 2 || th.MemberIDs[0] != a.UserID || th.MemberIDs[1] != c.UserID {
		t.Fatalf("thread members after a leave: %v", th.MemberIDs)
	}
	if count(t, srv, store.CollJoinRequests, bson.M{"userId": b.UserID, "status": models.JoinCancelled}) != 1 ||
		count(t, srv, store.CollJoinRequests, bson.M{"userId": c.UserID, "status": models.JoinAccepted}) != 1 {
		t.Error("the leaver's join is cancelled, the others' kept")
	}
	if n := srv.EventCount(b.UserID, realtime.EventItineraryRemoved); n != 1 {
		t.Errorf("itinerary.removed to the leaver: %d", n)
	}
	for _, id := range []string{a.UserID, c.UserID} {
		updates := itineraryEvents(t, srv, id, realtime.EventItineraryUpdated)
		if len(updates) != 1 || updates[0].GoingCount != 2 || updates[0].IsHost != (id == a.UserID) {
			t.Errorf("itinerary.updated to %s: %+v", id, updates)
		}
	}
	if len(itineraryEvents(t, srv, b.UserID, realtime.EventItineraryUpdated)) != 0 {
		t.Error("the leaver gets removed, not updated")
	}
	if got := social.changed(); len(got) != 1 || got[0] != thread {
		t.Errorf("thread.updated goes through the social views: %v", got)
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("forum.update after leaving a posted plan: %d", forumUpdates(srv))
	}
	srv.Do(t, "POST", path, nil, b).Expect(t, http.StatusNotFound)
}
