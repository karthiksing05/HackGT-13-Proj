//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"bytes"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// Fixed ids in the existing demo fixture database.
const (
	seedOpenPlan   = "seed-itin-open-plan"
	seedCrewPlan   = "seed-itin-market-crew"
	seedSoloPlan   = "seed-itin-solo-walk"
	seedCrewThread = "seed-thread-market-crew"
	seedFreePost   = "seed-post-marin-free"
	seedTheoAsk    = "seed-fr-theo-sandy"
	seedVisa       = "seed-pm-sandy-visa"
)

// demoPerson finds a seeded person by handle through the API (the seed may
// have adopted an existing account, so ids are not assumed).
func demoPerson(t testing.TB, s *Session, handle string) contract.UserSearchResult {
	t.Helper()
	for _, r := range get[[]contract.UserSearchResult](t, s, "/users/search?q="+url.QueryEscape("@"+handle)) {
		if r.Person.Username != nil && *r.Person.Username == handle {
			return r
		}
	}
	t.Fatalf("@%s not found", handle)
	return contract.UserSearchResult{}
}

// TestDemoProfile checks what the demo account is seeded with.
func TestDemoProfile(t *testing.T) {
	s := sandy(t)
	me := get[contract.User](t, s, "/me")
	if me.Name != "Sandy Byte" || me.Username == nil || *me.Username != "sandybyte" || me.AgeBracket != contract.AgeAdult ||
		me.School == nil || *me.School != "Saltlight Harbor College" || !me.SetupComplete || me.AvatarColor != contract.AvatarSage {
		t.Errorf("Sandy: %+v", me)
	}
	if me.City == nil || *me.City != "saltlight" {
		t.Errorf("Sandy's city %v, want saltlight", me.City)
	}
	if me.HomeBase == nil || me.HomeBase.Name != "Seaside Market Square" || me.HomeBase.Coordinate == nil ||
		fmt.Sprintf("%.3f,%.3f", me.HomeBase.Coordinate.Lat, me.HomeBase.Coordinate.Lng) != "31.368,-81.425" {
		t.Errorf("Sandy's home base: %+v", me.HomeBase)
	}

	prefs := get[contract.Preferences](t, s, "/me/preferences")
	if prefs.Ratings["outdoors"] != 5 || prefs.Ratings["long_walks"] != 5 || prefs.Ratings["big_crowds"] != 1 || len(prefs.Ratings) != 10 ||
		prefs.Company != contract.CompanySmallGroup || prefs.Spend != contract.SpendUnder15 || !prefs.PreferFree || prefs.InstantCheckout ||
		!strings.Contains(prefs.Answers["perfect_afternoon"], "long walk along the water") {
		t.Errorf("Sandy's preferences: %+v", prefs)
	}
	taste := get[contract.TasteProfile](t, s, "/me/taste-profile")
	values := map[string]float64{}
	for _, b := range taste.Bars {
		values[b.Label] = b.Value
	}
	if len(taste.Bars) != 5 || values["Outdoors"] <= values["Nightlife"] {
		t.Errorf("Sandy's taste: %+v", taste.Bars)
	}

	cards := get[[]contract.PaymentMethod](t, s, "/me/payment-methods")
	visa := slices.IndexFunc(cards, func(c contract.PaymentMethod) bool { return c.ID == seedVisa })
	if visa < 0 || cards[visa].Brand != "Visa" || cards[visa].Last4 != "4242" || !cards[visa].IsDefault {
		t.Errorf("Sandy's demo Visa: %+v", cards)
	}
	if fb := get[contract.FacebookConnection](t, s, "/integrations/facebook"); fb.Connected {
		t.Error("the demo account should not be connected to Facebook")
	}

	friends := get[[]contract.Friend](t, s, "/friends")
	if !slices.ContainsFunc(friends, func(f contract.Friend) bool { return f.Person.Name == "Marin Okafor" && f.StatusLine != "" }) {
		t.Errorf("Sandy's friends: %+v", friends)
	}
	requests := get[[]contract.FriendRequest](t, s, "/friends/requests")
	theo := slices.IndexFunc(requests, func(r contract.FriendRequest) bool { return r.ID == seedTheoAsk })
	if theo < 0 || requests[theo].Outgoing || requests[theo].Note != "Met at the market" || requests[theo].Person.Name != "Theo Park" {
		t.Errorf("Theo's pending request: %+v", requests)
	}
	marin := demoPerson(t, s, "marinokafor")
	theoRow := demoPerson(t, s, "theopark")
	if marin.Relation != contract.RelationFriend || theoRow.Relation != contract.RelationIncoming || theoRow.RequestID == nil || *theoRow.RequestID != seedTheoAsk {
		t.Errorf("relations: Marin %s, Theo %s %v", marin.Relation, theoRow.Relation, theoRow.RequestID)
	}
	checkPerson(t, "Marin", marin.Person)
}

// TestDemoPastEventsAndRating rates the unrated Heron Creek stop and checks
// Past, the unrated filter, paging and the insights.
func TestDemoPastEventsAndRating(t *testing.T) {
	s := sandy(t)
	page := get[contract.Page[contract.PastEvent]](t, s, "/me/past-events")
	find := func(events []contract.PastEvent, title string) *contract.PastEvent {
		for i := range events {
			if events[i].Title == title {
				return &events[i]
			}
		}
		return nil
	}
	heron := find(page.Items, "Heron Creek Greenway")
	market := find(page.Items, "Seaside Market Hall")
	coffee := find(page.Items, "Driftwood Coffee Roasters")
	if heron == nil || market == nil || coffee == nil {
		t.Fatalf("past events: %+v", page.Items)
	}
	if heron.Company != "solo" || heron.Kind != contract.KindSidequest {
		t.Errorf("Heron Creek: %+v", *heron)
	}
	if market.Company != "with 2 others" || market.Kind != contract.KindGroup || market.Rating == nil || market.Rating.Stars != 5 ||
		!slices.Contains(market.Rating.Tags, "Would go again") {
		t.Errorf("the rated market stop: %+v", *market)
	}
	if coffee.Rating == nil || coffee.Rating.Stars != 4 {
		t.Errorf("the rated coffee stop: %+v", *coffee)
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i].Date.After(page.Items[i-1].Date.Time) {
			t.Errorf("past events are not most recent first at %d", i)
		}
	}
	// Paging with limit=1 walks the same events.
	var walked []string
	path := "/me/past-events?limit=1"
	for n := 0; n < 20; n++ {
		p := get[contract.Page[contract.PastEvent]](t, s, path)
		if len(p.Items) > 1 {
			t.Fatalf("limit=1 returned %d", len(p.Items))
		}
		for _, e := range p.Items {
			walked = append(walked, e.ID)
		}
		if p.NextCursor == nil {
			break
		}
		path = "/me/past-events?limit=1&cursor=" + url.QueryEscape(*p.NextCursor)
	}
	if len(walked) != len(page.Items) {
		t.Errorf("paging walked %d events, the full page has %d", len(walked), len(page.Items))
	}
	fails(t, s, "GET", "/me/past-events?cursor=bm90LWEtY3Vyc29y", nil, http.StatusBadRequest)

	insightsBefore := get[contract.PastInsights](t, s, "/me/insights")
	if insightsBefore.Headline == "" || insightsBefore.BasedOn < 2 {
		t.Errorf("insights from two rated stops: %+v", insightsBefore)
	}
	tasteBefore := get[contract.TasteProfile](t, s, "/me/taste-profile")

	fails(t, s, "PUT", "/ratings/"+heron.ID, contract.Rating{Stars: 0, Tags: []string{}}, http.StatusBadRequest)
	fails(t, s, "PUT", "/ratings/no-such-item", contract.Rating{Stars: 4, Tags: []string{}}, http.StatusNotFound)
	note := "Herons at the second bridge."
	do(t, s, "PUT", "/ratings/"+heron.ID, contract.Rating{Stars: 5, Tags: []string{"Great views", " Great views ", "Would go again"}, Note: &note}).NoContent(t)

	after := get[contract.Page[contract.PastEvent]](t, s, "/me/past-events")
	rated := find(after.Items, "Heron Creek Greenway")
	if rated == nil || rated.Rating == nil || rated.Rating.Stars != 5 || !slices.Equal(rated.Rating.Tags, []string{"Great views", "Would go again"}) ||
		rated.Rating.Note == nil || *rated.Rating.Note != note {
		t.Errorf("Heron Creek after rating: %+v", rated)
	}
	unrated := get[contract.Page[contract.PastEvent]](t, s, "/me/past-events?unrated=true")
	if find(unrated.Items, "Heron Creek Greenway") != nil || find(unrated.Items, "Seaside Market Hall") != nil {
		t.Errorf("unrated=true still lists rated stops: %+v", unrated.Items)
	}
	if detail := get[contract.ItineraryItem](t, s, "/events/"+heron.ID); detail.Rating == nil || detail.Rating.Stars != 5 {
		t.Errorf("the Event sheet does not show the rating: %+v", detail.Rating)
	}
	insights := get[contract.PastInsights](t, s, "/me/insights")
	if wasUnrated := heron.Rating == nil; wasUnrated && insights.BasedOn != insightsBefore.BasedOn+1 {
		t.Errorf("insights based_on %d after a third 5-star rating (was %d)", insights.BasedOn, insightsBefore.BasedOn)
	}
	for _, h := range insights.Highlights {
		if h.ID == "" || h.Title == "" || h.Value == "" {
			t.Errorf("insight highlight %+v", h)
		}
	}
	tasteAfter := get[contract.TasteProfile](t, s, "/me/taste-profile")
	if tasteAfter.Bars[0].Label != "Outdoors" || tasteAfter.Bars[0].Value < tasteBefore.Bars[0].Value {
		t.Errorf("a 5-star greenway should not lower Outdoors: %v → %v", tasteBefore.Bars, tasteAfter.Bars)
	}
}

// TestDemoCalendar reads the seeded days and the range rules.
func TestDemoCalendar(t *testing.T) {
	s := sandy(t)
	c := config(t)
	past := get[contract.Page[contract.PastEvent]](t, s, "/me/past-events")
	var heron *contract.PastEvent
	for i := range past.Items {
		if past.Items[i].Title == "Heron Creek Greenway" {
			heron = &past.Items[i]
		}
	}
	if heron == nil {
		t.Fatal("no Heron Creek stop")
	}
	day := localDay(heron.Date.Time, c.Loc)
	from, to := day.AddDate(0, 0, -1), day.AddDate(0, 0, 2)
	days := get[[]contract.CalendarDay](t, s, "/calendar/days?from="+from.Format("2006-01-02")+"&to="+to.Format("2006-01-02"))
	if len(days) != 4 {
		t.Fatalf("4 days asked, %d returned", len(days))
	}
	for i, d := range days {
		want := from.AddDate(0, 0, i)
		if d.ID != want.Format("2006-01-02") || !d.Date.Equal(want) {
			t.Errorf("day %d is %s (%s), want %s", i, d.ID, d.Date, want.Format("2006-01-02"))
		}
	}
	found := false
	for _, item := range days[1].Items {
		if item.Title == "Heron Creek Greenway" {
			found = true
			if item.Kind != contract.KindSidequest || item.ItineraryID == nil || *item.ItineraryID != seedSoloPlan || !item.End.After(item.Start.Time) {
				t.Errorf("calendar block: %+v", item)
			}
		}
		if item.Kind == contract.KindTransit {
			t.Errorf("transit legs are not calendar blocks: %+v", item)
		}
	}
	if !found {
		t.Errorf("Heron Creek is not on %s: %+v", days[1].ID, days[1].Items)
	}
	fails(t, s, "GET", "/calendar/days?from=2026-13-40", nil, http.StatusBadRequest)
	fails(t, s, "GET", "/calendar/days?from=2026-09-01&to=2027-09-01", nil, http.StatusBadRequest)
	fails(t, s, "GET", "/calendar/days?from=2026-09-10&to=2026-09-01", nil, http.StatusBadRequest)
}

// TestDemoSearchAndPlaces covers Home's search bar and the place pickers.
func TestDemoSearchAndPlaces(t *testing.T) {
	s := sandy(t)
	empty := get[contract.SearchResults](t, s, "/search?q=")
	if len(empty.Sidequests)+len(empty.People)+len(empty.Places)+len(empty.Posts) != 0 {
		t.Errorf("an empty query found something: %+v", empty)
	}
	marin := get[contract.SearchResults](t, s, "/search?q=marin")
	if !slices.ContainsFunc(marin.People, func(r contract.UserSearchResult) bool {
		return r.Person.Name == "Marin Okafor" && r.Relation == contract.RelationFriend
	}) {
		t.Errorf("search people for marin: %+v", marin.People)
	}
	if !slices.ContainsFunc(marin.Posts, func(p contract.ForumPost) bool { return p.ID == seedOpenPlan }) {
		t.Errorf("search posts for marin should find his plan: %+v", marin.Posts)
	}
	market := get[contract.SearchResults](t, s, "/search?q=market")
	if !slices.ContainsFunc(market.Places, func(p contract.Place) bool { return p.Name == "Seaside Market Hall" && p.Coordinate != nil }) {
		t.Errorf("search places for market: %+v", market.Places)
	}
	if !slices.ContainsFunc(market.Posts, func(p contract.ForumPost) bool { return p.Title != nil && *p.Title == "Golden hour by the market" }) {
		t.Errorf("search posts for market: %+v", market.Posts)
	}
	for _, it := range market.Sidequests {
		if it.ID == seedCrewPlan {
			t.Error("a finished plan is not an active sidequest")
		}
	}
	theo := get[contract.SearchResults](t, s, "/search?q=theo")
	if !slices.ContainsFunc(theo.People, func(r contract.UserSearchResult) bool {
		return r.Person.Name == "Theo Park" && r.Relation == contract.RelationIncoming && r.RequestID != nil
	}) {
		t.Errorf("search people for theo: %+v", theo.People)
	}

	nearby := get[[]contract.Place](t, s, "/places/search")
	if len(nearby) != 5 || nearby[0].Name != "Seaside Market Square" {
		t.Errorf("places without a query: the home base and 4 nearest, got %+v", nearby)
	}
	for _, p := range nearby[1:] {
		if p.Name == "Seaside Market Square" || p.Coordinate == nil {
			t.Errorf("nearby place %+v", p)
		}
	}
	coffee := get[[]contract.Place](t, s, "/places/search?q=coffee")
	hit := slices.IndexFunc(coffee, func(p contract.Place) bool { return p.Name == "Driftwood Coffee Roasters" })
	if hit < 0 {
		t.Fatalf("places for coffee: %+v", coffee)
	}
	for _, p := range coffee {
		if !strings.Contains(strings.ToLower(p.Name), "coffee") {
			t.Errorf("%q does not match coffee", p.Name)
		}
	}
	pt := coffee[hit].Coordinate
	rev := get[contract.Place](t, s, fmt.Sprintf("/places/reverse?lat=%f&lng=%f", pt.Lat, pt.Lng))
	if rev.Name != "Driftwood Coffee Roasters" {
		t.Errorf("reverse of the café's own point: %+v", rev)
	}
	pin := get[contract.Place](t, s, "/places/reverse?lat=0.5&lng=0.5")
	if pin.Name != "Dropped pin" || pin.Coordinate == nil || pin.Coordinate.Lat != 0.5 {
		t.Errorf("reverse in the ocean: %+v", pin)
	}
	fails(t, s, "GET", "/places/reverse?lat=abc&lng=1", nil, http.StatusBadRequest)
	fails(t, s, "GET", "/places/search?near=nowhere", nil, http.StatusBadRequest)
}

// forum is GET /forum/posts with the given query.
func forum(t testing.TB, s *Session, query string) []contract.ForumPost {
	t.Helper()
	page := get[contract.Page[contract.ForumPost]](t, s, "/forum/posts"+query)
	if page.NextCursor != nil {
		t.Errorf("forum %s: unexpected second page", query)
	}
	return page.Items
}

func postIDs(posts []contract.ForumPost) []string {
	out := make([]string, len(posts))
	for i, p := range posts {
		out[i] = p.ID
	}
	return out
}

// TestDemoForumFeed checks the seeded feed and every filter and sort.
func TestDemoForumFeed(t *testing.T) {
	s := sandy(t)
	posts := forum(t, s, "")
	free := slices.IndexFunc(posts, func(p contract.ForumPost) bool { return p.ID == seedFreePost })
	plan := slices.IndexFunc(posts, func(p contract.ForumPost) bool { return p.ID == seedOpenPlan })
	if free < 0 || plan < 0 {
		t.Fatalf("the seeded posts are not in Sandy's feed: %v", postIDs(posts))
	}
	fp, pp := posts[free], posts[plan]
	if fp.Type != contract.PostFreeNow || !fp.FriendsOnly || !fp.IsFriend || fp.Text == nil || !strings.HasPrefix(*fp.Text, "Free until ") ||
		!strings.HasPrefix(fp.Meta, "Free now") || fp.Day != "today" || fp.JoinStatus != contract.JoinNone || fp.Author.Name != "Marin Okafor" {
		t.Errorf("Marin's free-now post: %+v", fp)
	}
	if pp.Type != contract.PostPlan || pp.Title == nil || *pp.Title != "Golden hour by the market" || pp.Capacity == nil || *pp.Capacity != 6 ||
		pp.LockLabel == nil || !strings.HasPrefix(*pp.LockLabel, "Locks ") || pp.When == nil || !strings.Contains(*pp.When, "5:30–8 PM") ||
		pp.Route == nil || !strings.Contains(*pp.Route, "stops") || !strings.HasPrefix(pp.Meta, "Hosting") {
		t.Errorf("Marin's open plan: %+v", pp)
	}
	// going lists the members other than the host (who is the author).
	if pp.JoinStatus == contract.JoinNone && (pp.SpotsLeft == nil || *pp.SpotsLeft != 4 || pp.GoingCount != 2 || len(pp.Going) != 1 || pp.Going[0].Name != "Theo Park") {
		t.Errorf("Marin's plan before Sandy joins: spots %v, going %d %v", pp.SpotsLeft, pp.GoingCount, pp.Going)
	}
	for i, p := range posts {
		checkPerson(t, fmt.Sprintf("post %d author", i), p.Author)
	}

	only := func(query string, want ...string) {
		t.Helper()
		got := postIDs(forum(t, s, query))
		for _, id := range []string{seedFreePost, seedOpenPlan} {
			if slices.Contains(want, id) != slices.Contains(got, id) {
				t.Errorf("forum %s: got %v, want %v among the seeded posts", query, got, want)
				return
			}
		}
	}
	only("?type=plans", seedOpenPlan)
	only("?type=free_now", seedFreePost)
	only("?type=all&scope=friends", seedFreePost, seedOpenPlan)
	only("?when=now", seedFreePost)
	only("?when=today", seedFreePost)
	only("?open_only=true", seedOpenPlan)
	if len(pp.Tags) > 0 {
		only("?tags="+url.QueryEscape(strings.ToLower(pp.Tags[0])), seedOpenPlan)
	}
	if pp.PriceTier == 0 {
		only("?cost=0", seedFreePost, seedOpenPlan)
	} else {
		only(fmt.Sprintf("?cost=%d", pp.PriceTier), seedOpenPlan)
	}
	only("?max_dist=0.05")
	home := get[contract.User](t, s, "/me").HomeBase.Coordinate
	only(fmt.Sprintf("?lat=%f&lng=%f&area=Seaside&radius=5", home.Lat, home.Lng), seedFreePost, seedOpenPlan)

	order := func(query string) []string {
		var ids []string
		for _, id := range postIDs(forum(t, s, query)) {
			if id == seedFreePost || id == seedOpenPlan {
				ids = append(ids, id)
			}
		}
		return ids
	}
	if got := order("?sort=soonest"); !slices.Equal(got, []string{seedFreePost, seedOpenPlan}) {
		t.Errorf("soonest: %v", got)
	}
	if got := order("?sort=spots"); !slices.Equal(got, []string{seedOpenPlan, seedFreePost}) {
		t.Errorf("spots: %v", got)
	}
	if got := order("?sort=newest"); !slices.Equal(got, []string{seedFreePost, seedOpenPlan}) {
		t.Errorf("newest: %v", got)
	}
	closest := forum(t, s, "?sort=closest")
	for i := 1; i < len(closest); i++ {
		if closest[i].DistanceMi < closest[i-1].DistanceMi {
			t.Errorf("closest is not ascending: %v", postIDs(closest))
		}
	}
	for _, bad := range []string{"?type=parties", "?sort=loudest", "?lat=31", "?radius=-2", "?cost=7", "?open_only=maybe"} {
		if msg := fails(t, s, "GET", "/forum/posts"+bad, nil, http.StatusBadRequest); msg != "Check the filters and try again." {
			t.Errorf("forum %s: %q", bad, msg)
		}
	}
}

// TestDemoFreeNowPost posts, replaces and deletes Sandy's "I'm free" post.
func TestDemoFreeNowPost(t *testing.T) {
	s := sandy(t)
	if res := do(t, s, "GET", "/forum/posts/mine", nil); res.Status == http.StatusOK {
		var old contract.MyFreePost
		res.JSON(t, &old)
		do(t, s, "DELETE", "/forum/posts/"+old.ID, nil).NoContent(t)
	}
	do(t, s, "GET", "/forum/posts/mine", nil).NoContent(t)

	home := get[contract.User](t, s, "/me").HomeBase
	until := contract.NewTime(businessNow(t, s).Add(2 * time.Hour).Truncate(time.Minute))
	area, radius := "Seaside Market", 3
	body := contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostFriends, Until: &until,
		Lat: &home.Coordinate.Lat, Lng: &home.Coordinate.Lng, AreaLabel: &area, RadiusMi: &radius}
	mine := send[contract.MyFreePost](t, s, "POST", "/forum/posts", body, http.StatusCreated)
	if mine.ID == "" || mine.Visibility != contract.PostFriends || mine.Until == nil || !mine.Until.Equal(until.Time) ||
		mine.AreaLabel == nil || *mine.AreaLabel != area || mine.RadiusMi == nil || *mine.RadiusMi != 3 ||
		!strings.HasPrefix(mine.Text, "Free until ") || !strings.HasSuffix(mine.Text, "near Seaside Market") {
		t.Errorf("new free post: %+v", mine)
	}
	if got := get[contract.MyFreePost](t, s, "/forum/posts/mine"); got.ID != mine.ID {
		t.Errorf("mine is %s, want %s", got.ID, mine.ID)
	}
	for _, p := range forum(t, s, "") {
		if p.ID == mine.ID {
			t.Error("your own post is not in your feed")
		}
	}
	fails(t, s, "POST", "/forum/posts/"+mine.ID+"/plan-together", nil, http.StatusBadRequest)
	fails(t, s, "POST", "/forum/posts/"+mine.ID+"/join-requests", nil, http.StatusBadRequest)

	// A second post replaces the first.
	everyone := contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone}
	second := send[contract.MyFreePost](t, s, "POST", "/forum/posts", everyone, http.StatusCreated)
	if got := get[contract.MyFreePost](t, s, "/forum/posts/mine"); got.ID != second.ID || got.Visibility != contract.PostEveryone || got.Until == nil {
		t.Errorf("after a second post, mine is %+v", got)
	}
	fails(t, s, "DELETE", "/forum/posts/"+mine.ID, nil, http.StatusNotFound)

	past := contract.NewTime(businessNow(t, s).Add(-time.Minute))
	tooLong := contract.NewTime(businessNow(t, s).Add(25 * time.Hour))
	zero := 0
	for name, bad := range map[string]contract.NewForumPost{
		"plan type":  {Type: contract.PostPlan, Visibility: contract.PostEveryone},
		"audience":   {Type: contract.PostFreeNow, Visibility: "strangers"},
		"past until": {Type: contract.PostFreeNow, Visibility: contract.PostFriends, Until: &past},
		"24h+ until": {Type: contract.PostFreeNow, Visibility: contract.PostFriends, Until: &tooLong},
		"radius 0":   {Type: contract.PostFreeNow, Visibility: contract.PostFriends, RadiusMi: &zero},
	} {
		if msg := fails(t, s, "POST", "/forum/posts", bad, http.StatusBadRequest); msg == "" {
			t.Errorf("%s: no sentence", name)
		}
	}
	fails(t, s, "DELETE", "/forum/posts/"+seedFreePost, nil, http.StatusNotFound)
	do(t, s, "DELETE", "/forum/posts/"+second.ID, nil).NoContent(t)
	do(t, s, "GET", "/forum/posts/mine", nil).NoContent(t)
}

// TestDemoPlanTogether answers Marin's free-now post with the DM.
func TestDemoPlanTogether(t *testing.T) {
	s := sandy(t)
	marin := demoPerson(t, s, "marinokafor")
	th := send[contract.ChatThread](t, s, "POST", "/forum/posts/"+seedFreePost+"/plan-together", nil, http.StatusOK)
	if th.IsGroup || len(th.Members) != 2 || th.Title != "Marin Okafor" {
		t.Errorf("plan together thread: %+v", th)
	}
	if !slices.ContainsFunc(th.Members, func(p contract.PersonRef) bool { return p.ID == marin.Person.ID }) {
		t.Errorf("the DM is not with Marin: %+v", th.Members)
	}
	again := send[contract.ChatThread](t, s, "POST", "/forum/posts/"+seedFreePost+"/plan-together", nil, http.StatusOK)
	if again.ID != th.ID {
		t.Errorf("plan together twice made two threads: %s, %s", th.ID, again.ID)
	}
	dm := send[contract.ChatThread](t, s, "POST", "/threads/dm", contract.StartDMRequest{UserID: marin.Person.ID}, http.StatusOK)
	if dm.ID != th.ID {
		t.Errorf("POST /threads/dm with Marin is %s, plan together used %s", dm.ID, th.ID)
	}
	msgs := get[[]contract.Message](t, s, "/threads/"+th.ID+"/messages")
	n := 0
	for _, m := range msgs {
		if m.Text == "Saw your post. Want to plan something together?" {
			n++
			if m.SenderName != "You" || m.SenderID != s.UserID || m.ClientID != nil {
				t.Errorf("plan together message: %+v", m)
			}
		}
	}
	if n != 1 {
		t.Errorf("plan together message %d times, want once", n)
	}
	for _, p := range forum(t, s, "") {
		if p.ID == seedFreePost && !p.PlanTogetherSent {
			t.Error("the post does not show plan_together_sent")
		}
	}
	fails(t, s, "POST", "/forum/posts/no-such-post/plan-together", nil, http.StatusNotFound)
}

// TestDemoThreads covers the Groups list, messages, paging, sending with
// client ids, reading, DMs and the realtime events for all of it.
func TestDemoThreads(t *testing.T) {
	s := sandy(t)
	marin := demoPerson(t, s, "marinokafor")
	threads := get[[]contract.ChatThread](t, s, "/threads")
	crewAt := slices.IndexFunc(threads, func(th contract.ChatThread) bool { return th.ID == seedCrewThread })
	if crewAt < 0 {
		t.Fatalf("no crew thread in %+v", threads)
	}
	crew := threads[crewAt]
	if !crew.IsGroup || crew.Title != "Saturday market crew" || len(crew.Members) != 3 || len(crew.Faces) != 2 ||
		!slices.Contains(crew.Chips, "3 people") || crew.AlbumTitle == nil || !strings.HasPrefix(*crew.AlbumTitle, "Saturday market crew · ") {
		t.Errorf("crew thread: %+v", crew)
	}
	dm := send[contract.ChatThread](t, s, "POST", "/threads/dm", contract.StartDMRequest{UserID: marin.Person.ID}, http.StatusOK)
	if !slices.ContainsFunc(threads, func(th contract.ChatThread) bool { return th.ID == dm.ID }) {
		t.Errorf("the DM with Marin is not in the list")
	}
	if got := get[contract.ChatThread](t, s, "/threads/"+seedCrewThread); got.ID != seedCrewThread || got.Title != crew.Title {
		t.Errorf("GET thread: %+v", got)
	}

	msgs := get[[]contract.Message](t, s, "/threads/"+seedCrewThread+"/messages")
	idx := func(ms []contract.Message, id string) int {
		return slices.IndexFunc(ms, func(m contract.Message) bool { return m.ID == id })
	}
	c1, c2, c3 := idx(msgs, "seed-msg-crew-1"), idx(msgs, "seed-msg-crew-2"), idx(msgs, "seed-msg-crew-3")
	if c1 < 0 || c2 < 0 || c3 < 0 || !(c1 < c2 && c2 < c3) {
		t.Fatalf("seeded crew messages, oldest first: %d %d %d", c1, c2, c3)
	}
	if msgs[c1].SenderName != "Marin" || msgs[c2].SenderName != "You" || msgs[c2].SenderID != s.UserID || msgs[c3].SenderName != "Theo" {
		t.Errorf("sender names: %q %q %q", msgs[c1].SenderName, msgs[c2].SenderName, msgs[c3].SenderName)
	}
	before := get[[]contract.Message](t, s, "/threads/"+seedCrewThread+"/messages?before=seed-msg-crew-3")
	if len(before) != 2 || before[0].ID != "seed-msg-crew-1" || before[1].ID != "seed-msg-crew-2" {
		t.Errorf("page before crew-3: %v", before)
	}
	if first := get[[]contract.Message](t, s, "/threads/"+seedCrewThread+"/messages?before=seed-msg-crew-1"); len(first) != 0 {
		t.Errorf("page before the first message: %v", first)
	}

	// Two devices: both hear the message; reading on one tells the other.
	phone, tablet := dial(t, s), dial(t, s)
	clientID := "e2e-" + randomHex(6)
	text := "E2E check-in " + randomHex(3)
	sent := send[contract.Message](t, s, "POST", "/threads/"+seedCrewThread+"/messages", contract.NewMessage{Text: "  " + text + " ", ClientID: &clientID}, http.StatusCreated)
	if sent.Text != text || sent.SenderName != "You" || sent.ClientID == nil || *sent.ClientID != clientID || time.Since(sent.SentAt.Time) > time.Minute {
		t.Errorf("sent message: %+v", sent)
	}
	replay := send[contract.Message](t, s, "POST", "/threads/"+seedCrewThread+"/messages", contract.NewMessage{Text: text, ClientID: &clientID}, http.StatusOK)
	if replay.ID != sent.ID {
		t.Errorf("the same client_id posted twice: %s, %s", sent.ID, replay.ID)
	}
	for _, w := range []*WS{phone, tablet} {
		var ev struct {
			ThreadID string           `json:"thread_id"`
			Message  contract.Message `json:"message"`
		}
		expectEvent(t, w, "message.new", 10*time.Second, &ev, func(v struct {
			ThreadID string           `json:"thread_id"`
			Message  contract.Message `json:"message"`
		}) bool {
			return v.Message.ID == sent.ID
		})
		if ev.ThreadID != seedCrewThread || ev.Message.SenderName != "You" || ev.Message.ClientID == nil || *ev.Message.ClientID != clientID {
			t.Errorf("message.new: %+v", ev)
		}
		var th contract.ChatThread
		expectEvent(t, w, "thread.updated", 10*time.Second, &th, func(v contract.ChatThread) bool { return v.ID == seedCrewThread })
		if th.LastMessage != "You: "+text {
			t.Errorf("thread.updated last message %q", th.LastMessage)
		}
	}
	expectNoEvent(t, phone, "message.new", 1500*time.Millisecond, has(sent.ID))
	after := get[[]contract.Message](t, s, "/threads/"+seedCrewThread+"/messages")
	count := 0
	for _, m := range after {
		if m.ID == sent.ID {
			count++
		}
	}
	if count != 1 || after[len(after)-1].ID != sent.ID {
		t.Errorf("the new message is listed %d times and last=%v", count, after[len(after)-1].ID == sent.ID)
	}
	if msg := fails(t, s, "POST", "/threads/"+seedCrewThread+"/messages", contract.NewMessage{Text: "   "}, http.StatusBadRequest); msg != "Type a message first." {
		t.Errorf("empty message: %q", msg)
	}

	do(t, s, "POST", "/threads/"+dm.ID+"/read", nil).NoContent(t)
	var read struct {
		ThreadID string `json:"thread_id"`
	}
	expectEvent(t, tablet, "thread.read", 10*time.Second, &read, func(v struct {
		ThreadID string `json:"thread_id"`
	}) bool {
		return v.ThreadID == dm.ID
	})
	if got := get[contract.ChatThread](t, s, "/threads/"+dm.ID); got.Unread != 0 {
		t.Errorf("unread %d after reading", got.Unread)
	}
	fails(t, s, "POST", "/threads/dm", contract.StartDMRequest{UserID: s.UserID}, http.StatusBadRequest)
	fails(t, s, "POST", "/threads/dm", contract.StartDMRequest{UserID: "5eed0000000000000000ffff"}, http.StatusNotFound)
	fails(t, s, "GET", "/threads/no-such-thread", nil, http.StatusNotFound)

	// Someone outside the crew sees none of it.
	alice, _ := people(t)
	fails(t, alice, "GET", "/threads/"+seedCrewThread, nil, http.StatusNotFound)
	fails(t, alice, "GET", "/threads/"+seedCrewThread+"/messages", nil, http.StatusNotFound)
	fails(t, alice, "POST", "/threads/"+seedCrewThread+"/messages", contract.NewMessage{Text: "hi"}, http.StatusNotFound)
	fails(t, alice, "GET", "/groups/"+seedCrewThread+"/expenses", nil, http.StatusNotFound)
	fails(t, alice, "GET", "/groups/"+seedCrewThread+"/photos", nil, http.StatusNotFound)
}

func balanceOf(bs []contract.Balance, id string) (int, bool) {
	for _, b := range bs {
		if b.UserID == id {
			return b.NetCents, true
		}
	}
	return 0, false
}

// TestDemoSplits checks the crew's ledger, adding and deleting an expense,
// the split math, and settling up (then undoes what it added).
func TestDemoSplits(t *testing.T) {
	s := sandy(t)
	marin := demoPerson(t, s, "marinokafor").Person.ID
	theo := demoPerson(t, s, "theopark").Person.ID
	group := "/groups/" + seedCrewThread
	expenses := get[[]contract.Expense](t, s, group+"/expenses")
	coffee := slices.IndexFunc(expenses, func(e contract.Expense) bool { return e.ID == "seed-exp-coffee" })
	fares := slices.IndexFunc(expenses, func(e contract.Expense) bool { return e.ID == "seed-exp-bus-fares" })
	if coffee < 0 || fares < 0 {
		t.Fatalf("seeded expenses: %+v", expenses)
	}
	if e := expenses[coffee]; e.What != "Coffee" || e.AmountCents != 2400 || e.PayerID != marin || !slices.Equal(e.Shares, []int{800, 800, 800}) ||
		e.CreatedBy == nil || *e.CreatedBy != marin {
		t.Errorf("coffee: %+v", e)
	}
	if e := expenses[fares]; e.What != "Bus fares" || e.AmountCents != 900 || e.PayerID != s.UserID || !slices.Equal(e.Shares, []int{300, 300, 300}) {
		t.Errorf("bus fares: %+v", e)
	}
	base := get[[]contract.Balance](t, s, group+"/balances")
	if len(base) != 2 {
		t.Fatalf("balances: %+v", base)
	}
	marinNet, _ := balanceOf(base, marin)
	theoNet, _ := balanceOf(base, theo)
	if len(expenses) == 2 && (marinNet != -500 || theoNet != 300) {
		t.Errorf("seeded balances: Marin %d (want -500), Theo %d (want 300)", marinNet, theoNet)
	}

	for name, bad := range map[string]contract.NewExpense{
		"no what":        {What: " ", AmountCents: 100, PayerID: s.UserID, SplitAmong: []string{s.UserID}},
		"zero":           {What: "Air", AmountCents: 0, PayerID: s.UserID, SplitAmong: []string{s.UserID}},
		"nobody":         {What: "Air", AmountCents: 100, PayerID: s.UserID, SplitAmong: []string{}},
		"outside payer":  {What: "Air", AmountCents: 100, PayerID: "5eed0000000000000000ffff", SplitAmong: []string{s.UserID}},
		"outside member": {What: "Air", AmountCents: 100, PayerID: s.UserID, SplitAmong: []string{s.UserID, "5eed0000000000000000ffff"}},
	} {
		if msg := fails(t, s, "POST", group+"/expenses", bad, http.StatusBadRequest); msg == "" {
			t.Errorf("%s: no sentence", name)
		}
	}

	ws := dial(t, s)
	pizza := send[contract.Expense](t, s, "POST", group+"/expenses",
		contract.NewExpense{What: " Pizza ", AmountCents: 4000, PayerID: s.UserID, SplitAmong: []string{s.UserID, marin, theo, marin}}, http.StatusCreated)
	if pizza.What != "Pizza" || !slices.Equal(pizza.SplitAmong, []string{s.UserID, marin, theo}) || !slices.Equal(pizza.Shares, []int{1334, 1333, 1333}) ||
		pizza.CreatedBy == nil || *pizza.CreatedBy != s.UserID {
		t.Errorf("pizza split: %+v", pizza)
	}
	var added struct {
		GroupID string           `json:"group_id"`
		Expense contract.Expense `json:"expense"`
	}
	expectEvent(t, ws, "expense.added", 10*time.Second, &added, func(v struct {
		GroupID string           `json:"group_id"`
		Expense contract.Expense `json:"expense"`
	}) bool {
		return v.Expense.ID == pizza.ID
	})
	if added.GroupID != seedCrewThread {
		t.Errorf("expense.added group %s", added.GroupID)
	}
	withPizza := get[[]contract.Balance](t, s, group+"/balances")
	if m, _ := balanceOf(withPizza, marin); m != marinNet+1333 {
		t.Errorf("Marin after pizza: %d, want %d", m, marinNet+1333)
	}
	if th, _ := balanceOf(withPizza, theo); th != theoNet+1333 {
		t.Errorf("Theo after pizza: %d, want %d", th, theoNet+1333)
	}
	fails(t, s, "DELETE", group+"/expenses/seed-exp-coffee", nil, http.StatusForbidden)
	do(t, s, "DELETE", group+"/expenses/"+pizza.ID, nil).NoContent(t)
	fails(t, s, "DELETE", group+"/expenses/"+pizza.ID, nil, http.StatusNotFound)

	// Settling: the amount must be what Sandy owes right now, the sum of her
	// debts ($5 to Marin), not the net across the group (−$2, Theo owes her
	// $3). The net is refused as stale.
	owed := 0
	for _, b := range get[[]contract.Balance](t, s, group+"/balances") {
		if b.NetCents < 0 {
			owed -= b.NetCents
		}
	}
	if owed == 0 {
		t.Skip("Sandy owes nothing in the crew (already settled); reseed to test settling")
	}
	net := marinNet + theoNet
	if net < 0 && -net != owed {
		if msg := fails(t, s, "POST", group+"/settle", contract.SettleRequest{AmountCents: -net}, http.StatusConflict); !strings.Contains(msg, "balance changed") {
			t.Errorf("settling the net: %q", msg)
		}
	}
	fails(t, s, "POST", group+"/settle", contract.SettleRequest{AmountCents: owed + 1}, http.StatusConflict)
	card := seedVisa
	do(t, s, "POST", group+"/settle", contract.SettleRequest{AmountCents: owed, PaymentMethodID: &card}).NoContent(t)
	settled := get[[]contract.Balance](t, s, group+"/balances")
	for _, b := range settled {
		if b.NetCents < 0 {
			t.Errorf("still owes %s %d after settling", b.UserID, b.NetCents)
		}
	}
	var rows []contract.Expense
	for _, e := range get[[]contract.Expense](t, s, group+"/expenses") {
		if strings.HasPrefix(e.What, "Settled up with Visa •••• 4242") {
			rows = append(rows, e)
			if e.PayerID != s.UserID || len(e.SplitAmong) != 1 {
				t.Errorf("settlement row: %+v", e)
			}
		}
	}
	if len(rows) == 0 {
		t.Fatal("no settlement rows")
	}
	do(t, s, "POST", group+"/settle", contract.SettleRequest{AmountCents: 0}).NoContent(t)
	for _, e := range rows {
		do(t, s, "DELETE", group+"/expenses/"+e.ID, nil).NoContent(t)
	}
	restored := get[[]contract.Balance](t, s, group+"/balances")
	if m, _ := balanceOf(restored, marin); m != marinNet {
		t.Errorf("after undoing the settlement Marin is %d, want %d", m, marinNet)
	}
}

// TestDemoGroupPhotos adds a photo to the crew album and removes it.
func TestDemoGroupPhotos(t *testing.T) {
	s := sandy(t)
	group := "/groups/" + seedCrewThread
	before := get[[]contract.GroupPhoto](t, s, group+"/photos")
	ws := dial(t, s)
	data := jpegBytes(t, color.RGBA{R: 30, G: 160, B: 150, A: 255})
	photo := send[contract.GroupPhoto](t, s, "POST", group+"/photos", photoForm(t, "photo", data), http.StatusCreated)
	if photo.ByName != "You" || photo.UploaderID == nil || *photo.UploaderID != s.UserID || photo.URL == nil || photo.CreatedAt == nil {
		t.Errorf("uploaded photo: %+v", photo)
	}
	var ev struct {
		GroupID string              `json:"group_id"`
		Photo   contract.GroupPhoto `json:"photo"`
	}
	expectEvent(t, ws, "photo.added", 10*time.Second, &ev, func(v struct {
		GroupID string              `json:"group_id"`
		Photo   contract.GroupPhoto `json:"photo"`
	}) bool {
		return v.Photo.ID == photo.ID
	})
	served := do(t, nil, "GET", *photo.URL, nil).Expect(t, http.StatusOK)
	if !bytes.Equal(served.Body, data) {
		t.Error("the album photo's bytes differ")
	}
	list := get[[]contract.GroupPhoto](t, s, group+"/photos")
	if len(list) != len(before)+1 || list[0].ID != photo.ID {
		t.Errorf("album after upload: %d photos, first %v", len(list), list[0].ID)
	}
	th := get[contract.ChatThread](t, s, "/threads/"+seedCrewThread)
	want := fmt.Sprintf("%d photo", len(list))
	if !slices.ContainsFunc(th.Chips, func(c string) bool { return strings.HasPrefix(c, want) }) {
		t.Errorf("thread chips %v do not count %d photos", th.Chips, len(list))
	}
	fails(t, s, "POST", group+"/photos", photoForm(t, "photo", []byte("GIF89a not really")), http.StatusBadRequest)
	do(t, s, "DELETE", group+"/photos/"+photo.ID, nil).NoContent(t)
	do(t, nil, "GET", *photo.URL, nil).Expect(t, http.StatusNotFound)
	if after := get[[]contract.GroupPhoto](t, s, group+"/photos"); len(after) != len(before) {
		t.Errorf("album after delete: %d, want %d", len(after), len(before))
	}
	fails(t, s, "DELETE", group+"/photos/"+photo.ID, nil, http.StatusNotFound)
}

// TestDemoDate checks the per-account demo clock when the server runs the
// demo on a fixed date (DEMO_DATE); set E2E_DEMO_DATE to the same date.
func TestDemoDate(t *testing.T) {
	c := config(t)
	want := envOr("E2E_DEMO_DATE", "")
	if want == "" {
		t.Skip("set E2E_DEMO_DATE to the server's DEMO_DATE to check the demo clock")
	}
	s := sandy(t)
	me := get[contract.User](t, s, "/me")
	if me.DemoDate == nil || *me.DemoDate != want {
		t.Fatalf("Sandy's demo_date %v, want %s", me.DemoDate, want)
	}
	day, err := time.ParseInLocation("2006-01-02", want, c.Loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := demoToday(t, s); !got.Equal(day) {
		t.Errorf("demoToday %s", got)
	}
	// Her calendar starts on the demo date; a real account's on today.
	days := get[[]contract.CalendarDay](t, s, "/calendar/days")
	if len(days) == 0 || days[0].ID != want {
		t.Errorf("Sandy's calendar starts on %v, want %s", days, want)
	}
	alice, _ := people(t)
	if a := get[contract.User](t, alice, "/me"); a.DemoDate != nil {
		t.Errorf("a real account has demo_date %s", *a.DemoDate)
	}
	real := localDay(time.Now(), c.Loc).Format("2006-01-02")
	if ad := get[[]contract.CalendarDay](t, alice, "/calendar/days"); len(ad) == 0 || ad[0].ID != real {
		t.Errorf("Alice's calendar starts on %v, want %s", ad[0].ID, real)
	}
	// Messages she sends are stamped on the demo date, at the real time of day.
	msg := send[contract.Message](t, s, "POST", "/threads/"+seedCrewThread+"/messages", contract.NewMessage{Text: "Demo clock check"}, http.StatusCreated)
	if got := msg.SentAt.In(c.Loc).Format("2006-01-02"); got != want {
		t.Errorf("a message sent now is dated %s, want the demo date %s", got, want)
	}
	now := time.Now().In(c.Loc)
	bizNow := time.Date(day.Year(), day.Month(), day.Day(), now.Hour(), now.Minute(), now.Second(), 0, c.Loc)
	if d := msg.SentAt.Sub(bizNow); d < -2*time.Minute || d > 2*time.Minute {
		t.Errorf("the message should keep the real time of day: sent %s, now %s", msg.SentAt.In(c.Loc).Format("15:04"), now.Format("15:04"))
	}
	// Past events and the Forum read her date: yesterday's walk is in the past,
	// Marin's plan is still ahead.
	if posts := forum(t, s, "?type=plans"); !slices.ContainsFunc(posts, func(p contract.ForumPost) bool { return p.ID == seedOpenPlan && p.StartsInMinutes > 0 }) {
		t.Errorf("Marin's plan is not upcoming on the demo date: %v", postIDs(posts))
	}
}
