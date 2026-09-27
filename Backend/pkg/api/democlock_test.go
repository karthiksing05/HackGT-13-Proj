package api_test

import (
	"Backend/pkg/api"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The demo clock end to end: DEMO_DATE=2026-09-24 while the real time is
// pinned to Sunday Sep 27, noon in New York. Demo accounts (catalog
// demo_activities) live on Thursday Sep 24 at the real time of day;
// everyone else, and everything security- or expiry-related, stays real.

var (
	ny, _     = time.LoadLocation("America/New_York")
	demoReal  = time.Date(2026, 9, 27, 12, 0, 0, 0, ny)
	demoToday = time.Date(2026, 9, 24, 0, 0, 0, 0, ny)
	demoDate  = "2026-09-24"
)

func demoServer(t *testing.T) *testutil.Server {
	t.Helper()
	if store.ActivityCollection != store.CollDemoActivities {
		t.Skip("demo clock requires ActivityCollection = CollDemoActivities")
	}
	return testutil.New(t, testutil.WithNow(demoReal.UTC()), testutil.WithConfig(func(c *config.Config) { c.DemoDate = demoDate }))
}

// demoUser signs up an account in the demo city.
func demoUser(t *testing.T, srv *testutil.Server, name string) *testutil.Session {
	t.Helper()
	req := testutil.SignupRequest(name)
	req.Email = testutil.UniqueEmail(name)
	sess := srv.SignupWith(t, req)
	if srv.Cfg.DemoDate != "" && (sess.User.DemoDate == nil || *sess.User.DemoDate != demoDate) {
		t.Fatalf("sign-up has no demo date: %+v", sess.User)
	}
	if _, err := srv.Store.Users().Update(t.Context(), sess.UserID, bson.M{"city": "saltlight"}); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestClockSelectionIgnoresEmail(t *testing.T) {
	d := &api.Deps{
		Cfg: &config.Config{DemoDate: demoDate, DemoTZ: "America/New_York"},
		Now: func() time.Time { return demoReal },
	}
	wantDemo := store.ActivityCollection == store.CollDemoActivities
	for _, email := range []string{"student@gatech.edu", "student@example.com"} {
		user := &models.User{Email: email}
		if (d.ClockFor(user) != nil) != wantDemo {
			t.Fatalf("clock for %s differs from the selected collection", email)
		}
		if (d.UserView(user).DemoDate != nil) != wantDemo {
			t.Fatalf("demo date for %s differs from the selected collection", email)
		}
	}
	// No Store is needed: the clock no longer queries the user's email.
	ctx := d.ForUser(context.Background(), "user-id")
	wantDay := dayOf(demoReal)
	if wantDemo {
		wantDay = demoDate
	}
	if got := dayOf(d.BusinessNow(ctx)); got != wantDay {
		t.Fatalf("business date = %s, want %s", got, wantDay)
	}
}

// dayPlan is a one-stop plan on a local day: leave at hour, the stop from
// hour:10 to hour+1:10, back by hour+3.
func dayPlan(day time.Time, hour int, title string) contract.CreateItineraryRequest {
	start := day.Add(time.Duration(hour) * time.Hour)
	arrive, leave := start.Add(10*time.Minute), start.Add(70*time.Minute)
	return contract.CreateItineraryRequest{
		Plan: contract.PlanRequest{
			Start: contract.Place{Name: "Home"}, End: contract.Place{Name: "Home"},
			Date: contract.NewTime(day), StartTime: contract.NewTime(start), BackBy: contract.NewTime(start.Add(3 * time.Hour)),
			Range: contract.RangeWalkable, Ride: contract.RideNone, Budget: 1, Who: contract.VisibilityFriends, Pace: contract.PaceBalanced,
		},
		Option: contract.PlanOption{ID: "opt-" + title, Name: title, Tag: "Best match", Stops: []contract.PlanStop{
			{ID: "s0", Title: title + " stop", Subtitle: "Park · Free", Place: contract.Place{Name: "Harbor Park"}, DurationMinutes: 60},
		}},
		StopOrder: []string{"s0"},
		Route: contract.RouteResult{
			Legs:      []contract.Leg{{Mode: contract.ModeWalk, Minutes: 10}, {Mode: contract.ModeWalk, Minutes: 10}},
			StopTimes: []contract.StopWindow{{Start: contract.NewTime(arrive), End: contract.NewTime(leave)}},
			Arrival:   contract.NewTime(leave.Add(10 * time.Minute)), BrokenAt: -1,
		},
		Visibility: contract.VisibilityJustMe,
	}
}

func createPlan(t *testing.T, srv *testutil.Server, sess *testutil.Session, req contract.CreateItineraryRequest) contract.Itinerary {
	t.Helper()
	var it contract.Itinerary
	srv.Do(t, "POST", "/itineraries", req, sess).Expect(t, http.StatusCreated).JSON(t, &it)
	return it
}

func listIDs(t *testing.T, srv *testutil.Server, sess *testutil.Session, path string) []string {
	t.Helper()
	var its []contract.Itinerary
	srv.Do(t, "GET", path, nil, sess).Expect(t, http.StatusOK).JSON(t, &its)
	ids := []string{}
	for _, it := range its {
		ids = append(ids, it.ID)
	}
	return ids
}

func dayOf(t time.Time) string { return t.In(ny).Format("2006-01-02") }

func TestDemoDateOnTheOwnUser(t *testing.T) {
	srv := demoServer(t)
	normal := srv.Signup(t, "Real Person")
	if normal.User.DemoDate == nil || *normal.User.DemoDate != demoDate {
		t.Fatalf("all accounts should use the selected demo clock: %+v", normal.User)
	}

	sandy := srv.Login(t, demoUser(t, srv, "Sandy Byte").Email, testutil.Password)
	if sandy.User.DemoDate == nil || *sandy.User.DemoDate != demoDate {
		t.Fatalf("log-in AuthResponse: %+v", sandy.User)
	}
	var me contract.User
	srv.Do(t, "GET", "/me", nil, sandy).Expect(t, http.StatusOK).JSON(t, &me)
	if me.DemoDate == nil || *me.DemoDate != demoDate || me.AgeBracket != contract.AgeAdult {
		t.Fatalf("GET /me: %+v", me)
	}
	busy := contract.StatusBusy
	srv.Do(t, "PATCH", "/me", contract.UserPatch{Status: &busy}, sandy).Expect(t, http.StatusOK).JSON(t, &me)
	if me.DemoDate == nil || *me.DemoDate != demoDate || me.Status != contract.StatusBusy {
		t.Fatalf("PATCH /me: %+v", me)
	}

	// Calendar days start on each account's own today.
	for _, tc := range []struct {
		sess  *testutil.Session
		first string
	}{{sandy, demoDate}, {normal, demoDate}} {
		var days []contract.CalendarDay
		srv.Do(t, "GET", "/calendar/days", nil, tc.sess).Expect(t, http.StatusOK).JSON(t, &days)
		if len(days) != 14 || days[0].ID != tc.first {
			t.Fatalf("calendar for %s starts %s, want %s", tc.sess.User.Name, days[0].ID, tc.first)
		}
	}

	// Without DEMO_DATE the same account is on real time and nothing says otherwise.
	plain := testutil.New(t, testutil.WithNow(demoReal.UTC()))
	sess := demoUser(t, plain, "Plain Sandy")
	me = contract.User{}
	plain.Do(t, "GET", "/me", nil, sess).Expect(t, http.StatusOK).JSON(t, &me)
	if me.DemoDate != nil || me.City == nil || *me.City != "saltlight" {
		t.Fatalf("demo_date without DEMO_DATE: %v", *me.DemoDate)
	}
	var days []contract.CalendarDay
	plain.Do(t, "GET", "/calendar/days", nil, sess).Expect(t, http.StatusOK).JSON(t, &days)
	if days[0].ID != "2026-09-27" {
		t.Fatalf("calendar without DEMO_DATE starts %s", days[0].ID)
	}
}

func TestDemoDatePlansPastAndInsights(t *testing.T) {
	srv := demoServer(t)
	ctx := t.Context()
	sandy := demoUser(t, srv, "Sandy Byte")
	normal := srv.Signup(t, "Real Person")
	thursday, friday, wednesday := demoToday, demoToday.AddDate(0, 0, 1), demoToday.AddDate(0, 0, -1)

	// Friday Sep 25 is tomorrow for Sandy (and long past in real time).
	upcoming := createPlan(t, srv, sandy, dayPlan(friday, 17, "Friday"))
	done := createPlan(t, srv, sandy, dayPlan(wednesday, 16, "Wednesday"))
	if got := listIDs(t, srv, sandy, "/itineraries"); !slices.Equal(got, []string{upcoming.ID}) {
		t.Fatalf("Sandy's active plans %v, want only Friday's %s", got, upcoming.ID)
	}
	if got := listIDs(t, srv, sandy, "/itineraries?status=past"); !slices.Equal(got, []string{done.ID}) {
		t.Fatalf("Sandy's past plans %v, want only Wednesday's %s", got, done.ID)
	}
	doc, err := srv.Store.Itineraries().Get(ctx, upcoming.ID)
	if err != nil || dayOf(doc.CreatedAt) != demoDate || doc.Status != models.ItineraryActive {
		t.Fatalf("stored plan: %v %+v", err, doc)
	}
	// Every account uses the selected collection's business time.
	theirs := createPlan(t, srv, normal, dayPlan(friday, 17, "Friday"))
	if got := listIDs(t, srv, normal, "/itineraries"); !slices.Equal(got, []string{theirs.ID}) {
		t.Fatalf("another account's active plans: %v", got)
	}

	// Moving Wednesday's plan to Saturday makes it upcoming again for Sandy.
	saturday := contract.NewTime(demoToday.AddDate(0, 0, 2))
	var moved contract.Itinerary
	srv.Do(t, "PATCH", "/itineraries/"+done.ID, contract.ItineraryUpdate{Date: &saturday}, sandy).Expect(t, http.StatusOK).JSON(t, &moved)
	if got := listIDs(t, srv, sandy, "/itineraries"); !slices.Equal(got, []string{upcoming.ID, done.ID}) {
		t.Fatalf("after moving Wednesday to Saturday: %v", got)
	}
	back := contract.NewTime(wednesday)
	srv.Do(t, "PATCH", "/itineraries/"+done.ID, contract.ItineraryUpdate{Date: &back}, sandy).Expect(t, http.StatusOK)

	// Past events are the stops that ended before Sandy's now; the calendar
	// shows the Friday stop on her tomorrow.
	stopOf := func(it contract.Itinerary) string {
		for _, item := range it.Items {
			if item.Kind != contract.KindTransit {
				return item.ID
			}
		}
		t.Fatalf("no stop in %s", it.ID)
		return ""
	}
	var past contract.Page[contract.PastEvent]
	srv.Do(t, "GET", "/me/past-events", nil, sandy).Expect(t, http.StatusOK).JSON(t, &past)
	if len(past.Items) != 1 || past.Items[0].ID != stopOf(done) || dayOf(past.Items[0].Date.Time) != "2026-09-23" {
		t.Fatalf("Sandy's past events: %+v", past.Items)
	}
	srv.Do(t, "GET", "/me/past-events", nil, normal).Expect(t, http.StatusOK).JSON(t, &past)
	if len(past.Items) != 0 {
		t.Fatalf("a normal account's past events: %+v", past.Items)
	}
	var days []contract.CalendarDay
	srv.Do(t, "GET", "/calendar/days", nil, sandy).Expect(t, http.StatusOK).JSON(t, &days)
	if days[0].ID != dayOf(thursday) || days[1].ID != dayOf(friday) || len(days[1].Items) != 1 || days[1].Items[0].ID != stopOf(upcoming) {
		t.Fatalf("Sandy's calendar: %+v", days[:2])
	}

	// Insights read only what has happened for her: one of two rated stops.
	for _, it := range []contract.Itinerary{upcoming, done} {
		srv.Do(t, "PUT", "/ratings/"+stopOf(it), contract.Rating{Stars: 5, Tags: []string{"Great people"}}, sandy).Expect(t, http.StatusNoContent)
	}
	var insights contract.PastInsights
	srv.Do(t, "GET", "/me/insights", nil, sandy).Expect(t, http.StatusOK).JSON(t, &insights)
	if insights.BasedOn != 1 {
		t.Fatalf("Sandy's insights are based on %d rated stops, want 1: %+v", insights.BasedOn, insights)
	}
	rating := findRating(t, srv, sandy.UserID, stopOf(done))
	if dayOf(rating.CreatedAt) != demoDate {
		t.Fatalf("a rating is stamped %s, want the demo date", rating.CreatedAt)
	}
}

func findRating(t *testing.T, srv *testutil.Server, userID, itemID string) models.Rating {
	t.Helper()
	var r models.Rating
	if err := srv.Store.Collection(store.CollRatings).FindOne(t.Context(), bson.M{"userId": userID, "itemId": itemID}).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDemoDateFreeNowAndChats(t *testing.T) {
	srv := demoServer(t)
	ctx := t.Context()
	ada, bo := demoUser(t, srv, "Ada Demo"), demoUser(t, srv, "Bo Demo")
	if _, _, err := srv.Store.Friends().Befriend(ctx, ada.UserID, bo.UserID); err != nil {
		t.Fatal(err)
	}
	lat, lng := 31.368, -81.425

	// "Until" is validated in Ada's time: 1 PM real Sunday is days away for her.
	realSoon := contract.NewTime(demoReal.Add(time.Hour))
	res := srv.Do(t, "POST", "/forum/posts", contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostFriends,
		Until: &realSoon, Lat: &lat, Lng: &lng}, ada).Expect(t, http.StatusBadRequest)
	if res.Message() != "A free-now post can last up to 24 hours." {
		t.Fatalf("until in real time: %q", res.Message())
	}
	// The default: three hours from her now, Thursday 3 PM.
	var mine contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostFriends,
		Lat: &lat, Lng: &lng}, ada).Expect(t, http.StatusCreated).JSON(t, &mine)
	thursday3PM := demoToday.Add(15 * time.Hour)
	if mine.Until == nil || !mine.Until.Equal(thursday3PM) || mine.Text != "Free until 3:00 PM near you" {
		t.Fatalf("Ada's post: %+v", mine)
	}
	var post models.ForumPost
	if err := srv.Store.Collection(store.CollForumPosts).FindOne(ctx, bson.M{"_id": mine.ID}).Decode(&post); err != nil {
		t.Fatal(err)
	}
	// The TTL monitor compares expiresAt with the wall clock: it is three
	// real hours away, not days in the past.
	if !post.ExpiresAt.Equal(demoReal.Add(3*time.Hour)) || !post.ExpiresAt.After(srv.Clock.Now()) || dayOf(post.CreatedAt) != demoDate {
		t.Fatalf("stored post: until %s, expiresAt %s, createdAt %s", post.Until, post.ExpiresAt, post.CreatedAt)
	}
	srv.Do(t, "GET", "/forum/posts/mine", nil, ada).Expect(t, http.StatusOK)

	// Bo lives on the same day: the post is live and Ada reads as free.
	var feed contract.Page[contract.ForumPost]
	srv.Do(t, "GET", "/forum/posts?type=free_now&scope=friends", nil, bo).Expect(t, http.StatusOK).JSON(t, &feed)
	if len(feed.Items) != 1 || feed.Items[0].ID != mine.ID || !feed.Items[0].IsFriend {
		t.Fatalf("Bo's free-now feed: %+v", feed.Items)
	}
	var friends []contract.Friend
	srv.Do(t, "GET", "/friends", nil, bo).Expect(t, http.StatusOK).JSON(t, &friends)
	if len(friends) != 1 || friends[0].StatusLine != "Free until 3 PM" {
		t.Fatalf("Bo's friends: %+v", friends)
	}

	// A chat: sent_at and the thread's time label are Thursday's.
	var th contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: bo.UserID}, ada).Expect(t, http.StatusOK).JSON(t, &th)
	var msg contract.Message
	srv.Do(t, "POST", "/threads/"+th.ID+"/messages", contract.NewMessage{Text: "Coffee?"}, ada).Expect(t, http.StatusCreated).JSON(t, &msg)
	if !msg.SentAt.Equal(demoToday.Add(12 * time.Hour)) {
		t.Fatalf("sent_at %s, want Thursday noon", msg.SentAt)
	}
	var threads []contract.ChatThread
	srv.Do(t, "GET", "/threads", nil, bo).Expect(t, http.StatusOK).JSON(t, &threads)
	if len(threads) != 1 || threads[0].LastTime != "12:00 PM" || threads[0].Unread != 1 {
		t.Fatalf("Bo's threads: %+v", threads)
	}
}

// browserGet opens a hosted page without a bearer token or redirects.
func browserGet(t *testing.T, srv *testutil.Server, method, rawURL string, form url.Values) *testutil.Response {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, srv.URL(u.RequestURI()), body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *srv.HTTP.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return &testutil.Response{Status: res.StatusCode, Header: res.Header, Body: raw}
}

func TestDemoDateKeepsRealTimeForLinksAndTokens(t *testing.T) {
	srv := demoServer(t)
	sandy := demoUser(t, srv, "Sandy Byte")

	// The calendar connect page.
	var link contract.URLResponse
	srv.Do(t, "POST", "/integrations/google/connect", nil, sandy).Expect(t, http.StatusOK).JSON(t, &link)
	if res := browserGet(t, srv, "GET", link.URL, nil); res.Status != http.StatusFound {
		t.Fatalf("calendar page: %d %s", res.Status, res.Body)
	}
	var integrations []contract.Integration
	srv.Do(t, "GET", "/integrations", nil, sandy).Expect(t, http.StatusOK).JSON(t, &integrations)
	if len(integrations) == 0 || !integrations[0].Connected {
		t.Fatalf("integrations: %+v", integrations)
	}

	// The card page: open it, save the demo card.
	srv.Do(t, "POST", "/me/payment-methods/setup", nil, sandy).Expect(t, http.StatusOK).JSON(t, &link)
	if res := browserGet(t, srv, "GET", link.URL, nil); res.Status != http.StatusOK {
		t.Fatalf("card page: %d", res.Status)
	}
	token := func() string {
		u, _ := url.Parse(link.URL)
		return u.Query().Get("t")
	}()
	if res := browserGet(t, srv, "POST", link.URL, url.Values{"t": {token}, "card": {"demo"}}); res.Status != http.StatusFound {
		t.Fatalf("card page submit: %d %s", res.Status, res.Body)
	}
	var cards []contract.PaymentMethod
	srv.Do(t, "GET", "/me/payment-methods", nil, sandy).Expect(t, http.StatusOK).JSON(t, &cards)
	if len(cards) != 1 {
		t.Fatalf("cards: %+v", cards)
	}

	// Tokens: the access token expires an hour after the real now and the
	// refresh token rotates.
	var fresh contract.RefreshResponse
	srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: sandy.Refresh}, nil).Expect(t, http.StatusOK).JSON(t, &fresh)
	if !fresh.ExpiresAt.Equal(demoReal.Add(time.Hour)) {
		t.Fatalf("access token expires %s", fresh.ExpiresAt)
	}
	sandy.Access = fresh.AccessToken
	srv.Do(t, "GET", "/me", nil, sandy).Expect(t, http.StatusOK)
}
