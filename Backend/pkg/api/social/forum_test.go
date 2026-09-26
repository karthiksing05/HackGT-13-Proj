package social_test

import (
	"Backend/pkg/api/social"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func freeNow(visibility contract.ForumPostVisibility, at contract.Coordinate, area string, radius *int) contract.NewForumPost {
	lat, lng := at.Lat, at.Lng
	return contract.NewForumPost{Type: contract.PostFreeNow, Visibility: visibility, Lat: &lat, Lng: &lng, AreaLabel: &area, RadiusMi: radius}
}

// feed is GET /forum/posts around a point with extra parameters.
func feed(t *testing.T, srv *testutil.Server, s *testutil.Session, at contract.Coordinate, params string) []contract.ForumPost {
	t.Helper()
	path := fmt.Sprintf("/forum/posts?lat=%v&lng=%v&area=Midtown&radius=2", at.Lat, at.Lng)
	if params != "" {
		path += "&" + params
	}
	var page contract.Page[contract.ForumPost]
	srv.Do(t, "GET", path, nil, s).Expect(t, http.StatusOK).JSON(t, &page)
	if page.NextCursor != nil {
		t.Fatalf("unexpected second page: %s", *page.NextCursor)
	}
	return page.Items
}

func ids(posts []contract.ForumPost) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.ID)
	}
	return out
}

func find(posts []contract.ForumPost, id string) *contract.ForumPost {
	for i := range posts {
		if posts[i].ID == id {
			return &posts[i]
		}
	}
	return nil
}

func TestFreePostLifecycle(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Ava Free")
	f := srv.Signup(t, "Finn Friend")
	befriend(t, srv, a, f)
	srv.Events.Reset()

	srv.Do(t, "GET", "/forum/posts/mine", nil, a).Expect(t, http.StatusNoContent)

	var mine contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostFriends, midtown, "Midtown Atlanta", testutil.Ptr(2)), a).
		Expect(t, http.StatusCreated).JSON(t, &mine)
	until := srv.Clock.Now().Add(3 * time.Hour).Truncate(time.Minute)
	wantText := "Free until " + httpx.Clock(until.In(ny)) + " near Midtown Atlanta"
	if mine.Text != wantText || mine.Visibility != contract.PostFriends || mine.Until == nil || !mine.Until.Equal(until) ||
		mine.AreaLabel == nil || *mine.AreaLabel != "Midtown Atlanta" || mine.RadiusMi == nil || *mine.RadiusMi != 2 {
		t.Fatalf("created post: %+v (want text %q)", mine, wantText)
	}
	var got contract.MyFreePost
	srv.Do(t, "GET", "/forum/posts/mine", nil, a).Expect(t, http.StatusOK).JSON(t, &got)
	if got.ID != mine.ID || got.Text != mine.Text {
		t.Fatalf("mine: %+v", got)
	}
	if broadcasts(srv, realtime.EventForumUpdate) != 1 {
		t.Fatal("posting must broadcast forum.update")
	}
	status := eventsFor[realtime.FriendStatusData](t, srv, f.UserID, realtime.EventFriendStatus)
	if len(status) != 1 || status[0].UserID != a.UserID || status[0].StatusLine != "Free until "+httpx.ClockShort(until.In(ny)) {
		t.Fatalf("friend.status to the friend: %+v", status)
	}

	// Posting again replaces the live post (one per author).
	var second contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostEveryone, techSquare, "Current location", nil), a).
		Expect(t, http.StatusCreated).JSON(t, &second)
	if second.ID == mine.ID || !strings.HasSuffix(second.Text, " near you") {
		t.Fatalf("replacement: %+v", second)
	}
	n, err := srv.Store.Collection(store.CollForumPosts).CountDocuments(context.Background(), bson.M{"authorId": a.UserID})
	if err != nil || n != 1 {
		t.Fatalf("one post per author, got %d (%v)", n, err)
	}

	// Only the author takes it down; a plan post is not deletable here.
	srv.Do(t, "DELETE", "/forum/posts/"+second.ID, nil, f).Expect(t, http.StatusNotFound)
	srv.Events.Reset()
	srv.Do(t, "DELETE", "/forum/posts/"+second.ID, nil, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "GET", "/forum/posts/mine", nil, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", "/forum/posts/"+second.ID, nil, a).Expect(t, http.StatusNotFound)
	if broadcasts(srv, realtime.EventForumUpdate) != 1 {
		t.Fatal("take-down must broadcast forum.update")
	}
	if status := eventsFor[realtime.FriendStatusData](t, srv, f.UserID, realtime.EventFriendStatus); len(status) != 1 || status[0].StatusLine != "Just added" {
		t.Fatalf("status after take-down: %+v", status)
	}
	it := insertPlan(t, srv, plan(a.UserID, srv.Clock.Now().Add(5*time.Hour), midtown))
	if res := srv.Do(t, "DELETE", "/forum/posts/"+it.ID, nil, a); res.Status != http.StatusBadRequest || res.Message() != social.MsgPlanPostOwn {
		t.Fatalf("plan post delete by host: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "DELETE", "/forum/posts/"+it.ID, nil, f).Expect(t, http.StatusNotFound)

	// Validation and defaults.
	for _, tc := range []struct {
		body contract.NewForumPost
		msg  string
	}{
		{contract.NewForumPost{Type: contract.PostPlan, Visibility: contract.PostEveryone}, social.MsgPostPlan},
		{contract.NewForumPost{Type: "nope", Visibility: contract.PostEveryone}, social.MsgPostType},
		{contract.NewForumPost{Type: contract.PostFreeNow, Visibility: "world"}, social.MsgPostAudience},
		{contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone, Until: contract.Ptr(srv.Clock.Now().Add(-time.Minute))}, social.MsgPostUntilPast},
		{contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone, Until: contract.Ptr(srv.Clock.Now().Add(25 * time.Hour))}, social.MsgPostUntilLong},
		{contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone, RadiusMi: testutil.Ptr(0)}, social.MsgPostRadius},
		{contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone}, social.MsgPostWhere}, // no lat/lng, no home base
	} {
		if res := srv.Do(t, "POST", "/forum/posts", tc.body, a); res.Status != http.StatusBadRequest || res.Message() != tc.msg {
			t.Fatalf("%+v: %d %s", tc.body, res.Status, res.Body)
		}
	}
	setUser(t, srv, a, home(krog))
	custom := srv.Clock.Now().Add(90 * time.Minute)
	var byHome contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone, Until: contract.Ptr(custom)}, a).
		Expect(t, http.StatusCreated).JSON(t, &byHome)
	stored, err := srv.Store.Forum().LiveFreePost(context.Background(), a.UserID, srv.Clock.Now())
	if err != nil || stored.Location.Coordinates[0] != krog.Lng || stored.Location.Coordinates[1] != krog.Lat ||
		!stored.Until.Equal(custom.Truncate(time.Millisecond)) || !strings.HasSuffix(byHome.Text, " near you") {
		t.Fatalf("home-base default: %v %+v %+v", err, stored, byHome)
	}
}

func TestForumVisibilityRadiusAndScope(t *testing.T) {
	srv := testutil.New(t)
	viewer := srv.Signup(t, "Vera Viewer")
	friend := srv.Signup(t, "Frank Friend")
	near := srv.Signup(t, "Nina Near")
	far := srv.Signup(t, "Farid Far")
	shy := srv.Signup(t, "Shay Shy")
	busy := srv.Signup(t, "Bo Busy")
	private := srv.Signup(t, "Pia Private")
	tight := srv.Signup(t, "Tia Tight")
	befriend(t, srv, viewer, friend)

	post := func(s *testutil.Session, v contract.ForumPostVisibility, at contract.Coordinate, radius *int) string {
		var mine contract.MyFreePost
		srv.Do(t, "POST", "/forum/posts", freeNow(v, at, "Somewhere", radius), s).Expect(t, http.StatusCreated).JSON(t, &mine)
		return mine.ID
	}
	friendPost := post(friend, contract.PostFriends, airport, nil) // friends see it at any distance
	nearPost := post(near, contract.PostEveryone, techSquare, nil)
	farPost := post(far, contract.PostEveryone, airport, nil)
	post(shy, contract.PostFriends, techSquare, nil)              // friends only, and not the viewer's friend
	post(busy, contract.PostEveryone, techSquare, nil)            // hidden while busy
	post(private, contract.PostEveryone, techSquare, nil)         // hidden from non-friends while friends_only
	post(tight, contract.PostEveryone, downtown, testutil.Ptr(1)) // 1.5 mi away but only visible within 1 mi
	post(viewer, contract.PostEveryone, techSquare, nil)          // your own post is never in your feed
	srv.Do(t, "PATCH", "/me", contract.UserPatch{Status: testutil.Ptr(contract.StatusBusy)}, busy).Expect(t, http.StatusOK)
	srv.Do(t, "PATCH", "/me", contract.UserPatch{Status: testutil.Ptr(contract.StatusFriendsOnly)}, private).Expect(t, http.StatusOK)

	posts := feed(t, srv, viewer, midtown, "")
	if got := ids(posts); len(got) != 2 || find(posts, friendPost) == nil || find(posts, nearPost) == nil {
		t.Fatalf("visible posts: %v", got)
	}
	fp, np := find(posts, friendPost), find(posts, nearPost)
	if !fp.IsFriend || !fp.FriendsOnly || fp.Meta != "Free now · 10.2 mi away" || fp.DistanceMi != 10.2 {
		t.Fatalf("friend's post: %+v", fp)
	}
	if np.IsFriend || np.FriendsOnly || np.Meta != "Free now · 0.6 mi away" || np.Type != contract.PostFreeNow ||
		np.Day != "today" || np.StartsInMinutes != 0 || np.PriceTier != 0 || len(np.Tags) != 0 || np.JoinStatus != contract.JoinNone ||
		np.Author.ID != near.UserID || np.Author.Name != "Nina Near" || np.Text == nil || np.Title != nil {
		t.Fatalf("near post: %+v", np)
	}

	// A wider radius reaches the far post; scope=friends keeps only friends'.
	var page contract.Page[contract.ForumPost]
	srv.Do(t, "GET", fmt.Sprintf("/forum/posts?lat=%v&lng=%v&radius=25", midtown.Lat, midtown.Lng), nil, viewer).Expect(t, http.StatusOK).JSON(t, &page)
	if find(page.Items, farPost) == nil || len(page.Items) != 3 {
		t.Fatalf("radius 25: %v", ids(page.Items))
	}
	if got := ids(feed(t, srv, viewer, midtown, "scope=friends")); len(got) != 1 || got[0] != friendPost {
		t.Fatalf("scope=friends: %v", got)
	}
	// A friend of the busy poster still does not see them as free.
	befriend(t, srv, busy, viewer)
	for _, p := range feed(t, srv, viewer, midtown, "") {
		if p.Author.ID == busy.UserID {
			t.Fatal("a busy author's free post must stay hidden")
		}
	}
	// Without lat/lng the home base is the center; without either only friends' posts show.
	srv.Do(t, "GET", "/forum/posts", nil, viewer).Expect(t, http.StatusOK).JSON(t, &page)
	if len(page.Items) != 1 || page.Items[0].ID != friendPost || page.Items[0].Meta != "Free now" {
		t.Fatalf("no center: %+v", page.Items)
	}
	setUser(t, srv, viewer, home(midtown))
	srv.Do(t, "GET", "/forum/posts", nil, viewer).Expect(t, http.StatusOK).JSON(t, &page)
	if len(page.Items) != 2 {
		t.Fatalf("home-base center: %v", ids(page.Items))
	}
	for _, bad := range []string{"type=nope", "scope=all", "when=later", "sort=best", "lat=91&lng=0", "lat=1", "radius=-1", "cost=5", "open_only=maybe", "max_dist=x"} {
		if res := srv.Do(t, "GET", "/forum/posts?"+bad, nil, viewer); res.Status != http.StatusBadRequest || res.Message() != social.MsgForumFilters {
			t.Fatalf("%s: %d %s", bad, res.Status, res.Body)
		}
	}
	srv.Do(t, "GET", "/forum/posts", nil, nil).Expect(t, http.StatusUnauthorized)
}

func TestPlanPostsDerivedFromItineraries(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 2026-09-26 12:00 New York
	viewer := srv.Signup(t, "Vic Viewer")
	host := srv.Signup(t, "Maya Host")
	member := srv.Signup(t, "Dev Member")
	stranger := srv.Signup(t, "Sam Stranger")
	befriend(t, srv, viewer, host)
	now := srv.Clock.Now()
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 9, 26+day, hour, minute, 0, 0, ny) }

	today := insertPlan(t, srv, plan(host.UserID, at(0, 17, 30), techSquare, func(it *models.Itinerary) {
		it.Title = "Sunset + tacos"
		it.BackBy = at(0, 20, 0)
		it.LockAt = testutil.Ptr(at(0, 17, 0))
		it.MaxGroupSize = testutil.Ptr(6)
		it.MemberIDs = []string{host.UserID, member.UserID}
		it.CreatedAt = now.Add(-25 * time.Minute)
	}))
	created := func(ago time.Duration) func(*models.Itinerary) {
		return func(it *models.Itinerary) { it.CreatedAt = now.Add(-ago) }
	}
	monday := insertPlan(t, srv, plan(host.UserID, at(2, 6, 0), krog, func(it *models.Itinerary) {
		it.Visibility = models.VisibilityFriends
		it.BackBy = at(2, 10, 0)
		it.LockAt = testutil.Ptr(at(1, 17, 0))
		it.Plan.Budget = 2
		it.Plan.Tags = []string{"Active"}
		it.Items[0].LegMode, it.Items[2].LegMode = "marta", "marta"
	}, created(4*time.Hour)))
	full := insertPlan(t, srv, plan(host.UserID, at(0, 19, 0), techSquare, func(it *models.Itinerary) {
		it.MaxGroupSize = testutil.Ptr(2)
		it.MemberIDs = []string{host.UserID, member.UserID}
	}, created(time.Hour)))
	locked := insertPlan(t, srv, plan(host.UserID, at(0, 15, 0), techSquare, func(it *models.Itinerary) {
		it.LockAt = testutil.Ptr(at(0, 11, 0))
	}, created(3*time.Hour)))
	insertPlan(t, srv, plan(host.UserID, at(0, 18, 0), techSquare, func(it *models.Itinerary) { it.Visibility = models.VisibilityJustMe }))
	insertPlan(t, srv, plan(host.UserID, at(-1, 9, 0), techSquare))                                                                     // over yesterday
	insertPlan(t, srv, plan(host.UserID, at(0, 18, 0), techSquare, func(it *models.Itinerary) { it.Status = models.ItineraryDeleted })) // deleted
	strangerOpen := insertPlan(t, srv, plan(stranger.UserID, at(1, 10, 0), techSquare, created(2*time.Hour)))
	insertPlan(t, srv, plan(stranger.UserID, at(1, 10, 0), techSquare, func(it *models.Itinerary) { it.Visibility = models.VisibilityFriends }))
	insertPlan(t, srv, plan(stranger.UserID, at(1, 10, 0), airport))  // open but 10 mi away
	insertPlan(t, srv, plan(viewer.UserID, at(0, 18, 0), techSquare)) // the viewer's own

	posts := feed(t, srv, viewer, midtown, "type=plans")
	want := []string{locked.ID, today.ID, full.ID, strangerOpen.ID, monday.ID} // soonest first
	if got := ids(posts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("plan posts %v, want %v", got, want)
	}
	p := find(posts, today.ID)
	if p.Type != contract.PostPlan || p.Title == nil || *p.Title != "Sunset + tacos" || p.Text != nil ||
		p.Meta != "Hosting · 0.6 mi away" || p.When == nil || *p.When != "Today · 5:30–8 PM" ||
		p.Route == nil || *p.Route != "2 stops · walking" || p.StartsInMinutes != 330 || p.Day != "today" ||
		p.PriceTier != 1 || strings.Join(p.Tags, ",") != "Outdoors,Food" || p.SpotsLeft == nil || *p.SpotsLeft != 4 ||
		p.Capacity == nil || *p.Capacity != 6 || p.LockLabel == nil || *p.LockLabel != "Locks 5:00 PM" ||
		len(p.Going) != 1 || p.Going[0].ID != member.UserID || p.GoingCount != 2 || p.InterestedCount != 0 ||
		p.PostedMinutesAgo != 25 || p.JoinStatus != contract.JoinNone || p.PlanTogetherSent || p.ThreadID != nil ||
		!p.IsFriend || p.FriendsOnly || p.Author.ID != host.UserID {
		t.Fatalf("today's plan: %+v", p)
	}
	m := find(posts, monday.ID)
	if *m.When != "Mon · 6–10 AM" || m.Day != "mon" || *m.LockLabel != "Locks Sun 5 PM" || *m.Route != "2 stops · MARTA" ||
		m.PriceTier != 2 || !m.FriendsOnly || m.SpotsLeft != nil || m.Capacity != nil || m.Meta != "Hosting · 2.2 mi away" {
		t.Fatalf("monday plan: %+v", m)
	}
	if f := find(posts, full.ID); f.JoinStatus != contract.JoinFull || *f.SpotsLeft != 0 {
		t.Fatalf("full plan: %+v", f)
	}
	if l := find(posts, locked.ID); l.JoinStatus != contract.JoinClosed || *l.LockLabel != "Locked 11:00 AM" {
		t.Fatalf("locked plan: %+v", l)
	}
	if s := find(posts, strangerOpen.ID); s.IsFriend || *s.When != "Tomorrow · 10 AM–12:30 PM" || s.Day != "sun" {
		t.Fatalf("stranger's open plan: %+v", s)
	}

	// Joining shows joined with the group thread.
	var joined contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+today.ID+"/join-requests", nil, viewer).Expect(t, http.StatusOK).JSON(t, &joined)
	p = find(feed(t, srv, viewer, midtown, "type=plans"), today.ID)
	if p.JoinStatus != contract.JoinJoined || p.ThreadID == nil || *p.ThreadID != *joined.ThreadID || *p.SpotsLeft != 3 || p.GoingCount != 3 {
		t.Fatalf("after joining: %+v", p)
	}

	// Filters and sorts.
	check := func(params string, want ...string) {
		t.Helper()
		if got := ids(feed(t, srv, viewer, midtown, params)); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: got %v, want %v", params, got, want)
		}
	}
	check("type=plans&open_only=true", today.ID, strangerOpen.ID, monday.ID)
	check("type=plans&when=today", locked.ID, today.ID, full.ID)
	check("type=plans&when=now")                                                    // nothing starts within the hour
	check("type=plans&when=weekend", locked.ID, today.ID, full.ID, strangerOpen.ID) // Saturday and Sunday
	check("type=plans&cost=2", monday.ID)
	check("type=plans&tags=active,Music", monday.ID)
	check("type=plans&max_dist=1", locked.ID, today.ID, full.ID, strangerOpen.ID)
	check("type=plans&sort=closest&max_dist=1", today.ID, full.ID, strangerOpen.ID, locked.ID) // same place: newest first
	check("type=plans&sort=spots", strangerOpen.ID, locked.ID, monday.ID, today.ID, full.ID)   // unlimited first
	check("type=free_now")
	check("type=plans&scope=friends", locked.ID, today.ID, full.ID, monday.ID)
	check("type=plans&sort=newest", today.ID, full.ID, strangerOpen.ID, locked.ID, monday.ID)

	// A stranger does not see the friends-only plan.
	if find(feed(t, srv, stranger, midtown, ""), monday.ID) != nil {
		t.Fatal("friends-only plan leaked to a stranger")
	}
}

func TestForumPagination(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Page Viewer")
	host := srv.Signup(t, "Page Host")
	for i := 0; i < 53; i++ {
		insertPlan(t, srv, plan(host.UserID, testutil.Fixed.Add(time.Duration(i+1)*time.Hour), techSquare))
	}
	var first, second contract.Page[contract.ForumPost]
	path := fmt.Sprintf("/forum/posts?lat=%v&lng=%v", midtown.Lat, midtown.Lng)
	srv.Do(t, "GET", path, nil, viewer).Expect(t, http.StatusOK).JSON(t, &first)
	if len(first.Items) != 50 || first.NextCursor == nil {
		t.Fatalf("first page: %d %v", len(first.Items), first.NextCursor)
	}
	srv.Do(t, "GET", path+"&cursor="+url.QueryEscape(*first.NextCursor), nil, viewer).Expect(t, http.StatusOK).JSON(t, &second)
	if len(second.Items) != 3 || second.NextCursor != nil || second.Items[0].StartsInMinutes <= first.Items[49].StartsInMinutes {
		t.Fatalf("second page: %d %v", len(second.Items), second.NextCursor)
	}
	res := srv.Do(t, "GET", path, nil, viewer)
	if !strings.Contains(string(res.Body), `"next_cursor":"`) {
		t.Fatalf("page shape: %s", res.Body[:80])
	}
}

func TestSearchPosts(t *testing.T) {
	srv := testutil.New(t)
	viewer := srv.Signup(t, "Sid Searcher")
	host := srv.Signup(t, "Tara Tacos")
	setUser(t, srv, viewer, home(midtown))
	it := insertPlan(t, srv, plan(host.UserID, srv.Clock.Now().Add(3*time.Hour), techSquare, func(it *models.Itinerary) { it.Title = "Taco crawl" }))
	insertPlan(t, srv, plan(host.UserID, srv.Clock.Now().Add(3*time.Hour), airport, func(it *models.Itinerary) { it.Title = "Airport tacos" }))
	user, err := srv.Store.Users().ByID(context.Background(), viewer.UserID)
	if err != nil {
		t.Fatal(err)
	}
	found, err := social.SearchPosts(context.Background(), srv.Deps, user, "TACO", ny)
	if err != nil || len(found) != 1 || found[0].ID != it.ID {
		t.Fatalf("search: %v %+v", err, found)
	}
	if found, _ := social.SearchPosts(context.Background(), srv.Deps, user, "tara", ny); len(found) != 1 {
		t.Fatalf("by author name: %+v", found)
	}
	if found, _ := social.SearchPosts(context.Background(), srv.Deps, user, " ", ny); found == nil || len(found) != 0 {
		t.Fatalf("empty query: %+v", found)
	}
}

// TestSearchIncludesForumPosts goes through GET /search (the itineraries
// area), which gets its posts from this area through itineraries.UseSocial.
func TestSearchIncludesForumPosts(t *testing.T) {
	srv := testutil.New(t)
	sandy := srv.Signup(t, "Sandy Byte")
	marin := srv.Signup(t, "Marin Okafor")
	stranger := srv.Signup(t, "Sol Stranger")
	befriend(t, srv, sandy, marin)
	setUser(t, srv, sandy, home(midtown))
	setUser(t, srv, stranger, home(midtown))
	var free contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostFriends, techSquare, "Seaside Market", nil), marin).Expect(t, http.StatusCreated).JSON(t, &free)
	walk := insertPlan(t, srv, plan(marin.UserID, srv.Clock.Now().Add(3*time.Hour), techSquare, func(it *models.Itinerary) { it.Title = "Golden hour walk" }))
	search := func(s *testutil.Session, q string) []string {
		t.Helper()
		var res contract.SearchResults
		srv.Do(t, "GET", "/search?q="+q, nil, s).Expect(t, http.StatusOK).JSON(t, &res)
		return ids(res.Posts)
	}
	for _, tc := range []struct {
		who  *testutil.Session
		q    string
		want []string
	}{
		{sandy, "market", []string{free.ID}}, // "Free until … near Seaside Market"
		{sandy, "GOLDEN", []string{walk.ID}},
		{sandy, "marin", []string{free.ID, walk.ID}}, // the author's name, soonest first
		{stranger, "market", nil},                    // friends-only: not for a stranger
		{stranger, "golden", []string{walk.ID}},      // open, 0.6 mi from their home
	} {
		if got := search(tc.who, tc.q); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s searching %q: posts %v, want %v", tc.who.User.Name, tc.q, got, tc.want)
		}
	}
}
