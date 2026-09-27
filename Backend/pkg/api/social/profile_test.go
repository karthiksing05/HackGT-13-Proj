package social_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/testutil"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// profileOf is GET /users/{id}/profile as s.
func profileOf(t *testing.T, srv *testutil.Server, s *testutil.Session, id string) contract.PublicProfile {
	t.Helper()
	var out contract.PublicProfile
	srv.Do(t, "GET", "/users/"+id+"/profile", nil, s).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func planIDs(p contract.PublicProfile) []string { return ids(p.OpenPlans) }

func TestProfileRelations(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Vi Viewer")
	stranger := srv.Signup(t, "Stan Stranger")
	friend := srv.Signup(t, "Fran Friend")
	asked := srv.Signup(t, "Asa Asked")
	asker := srv.Signup(t, "Ash Asker")
	befriend(t, srv, viewer, friend)
	var sent, received contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: asked.UserID}, viewer).Expect(t, http.StatusCreated).JSON(t, &sent)
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: viewer.UserID}, asker).Expect(t, http.StatusCreated).JSON(t, &received)
	setUser(t, srv, viewer, bson.M{"school": "Georgia Tech", "status": string(contract.StatusFriendsOnly)})

	// Your own profile: "self", your own details, nothing about how you relate.
	me := profileOf(t, srv, viewer, viewer.UserID)
	if me.Relation != contract.ProfileSelf || me.RequestID != nil || me.Compatibility != nil || me.StatusLine != nil ||
		me.Person.ID != viewer.UserID || me.Person.Name != "Vi Viewer" || me.School == nil || *me.School != "Georgia Tech" ||
		me.City == nil || *me.City != "atlanta" || me.Status != contract.StatusFriendsOnly || me.MutualFriends.Count != 0 {
		t.Fatalf("own profile: %+v", me)
	}

	// Everyone else, with the pending request on either side; only a friend has a status line.
	cases := []struct {
		who      *testutil.Session
		relation contract.ProfileRelation
		request  string
	}{
		{stranger, contract.ProfileNone, ""},
		{friend, contract.ProfileFriend, ""},
		{asked, contract.ProfileOutgoing, sent.ID},
		{asker, contract.ProfileIncoming, received.ID},
	}
	for _, c := range cases {
		p := profileOf(t, srv, viewer, c.who.UserID)
		request := ""
		if p.RequestID != nil {
			request = *p.RequestID
		}
		if p.Person.ID != c.who.UserID || p.Relation != c.relation || request != c.request {
			t.Errorf("%s: relation %q, request %q", c.who.User.Name, p.Relation, request)
		}
		if (p.StatusLine != nil) != (c.relation == contract.ProfileFriend) {
			t.Errorf("%s: status line %v", c.who.User.Name, p.StatusLine)
		}
	}
	if p := profileOf(t, srv, friend, viewer.UserID); p.StatusLine == nil || *p.StatusLine != "Just added" {
		t.Errorf("a new friend's status line: %v", p.StatusLine)
	}
	if p := profileOf(t, srv, asked, viewer.UserID); p.Relation != contract.ProfileIncoming || p.RequestID == nil || *p.RequestID != sent.ID {
		t.Errorf("the request's other side: %+v", p)
	}

	// Empty lists are written as [] and optional fields without a value are left out.
	body := string(srv.Do(t, "GET", "/users/"+stranger.UserID+"/profile", nil, viewer).Expect(t, http.StatusOK).Body)
	for _, want := range []string{`"relation":"none"`, `"status":"open"`, `"match_reasons":[]`, `"likes":[]`, `"open_plans":[]`,
		`"mutual_friends":{"count":0,"people":[]}`, `"sidequests_done":0`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	for _, key := range []string{"compatibility", "status_line", "request_id", "school"} {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("%s written without a value: %s", key, body)
		}
	}

	// Unknown and malformed ids are 404; without a session, 401.
	srv.Do(t, "GET", "/users/"+bson.NewObjectID().Hex()+"/profile", nil, viewer).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/users/not-an-id/profile", nil, viewer).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/users/"+stranger.UserID+"/profile", nil, nil).Expect(t, http.StatusUnauthorized)
}

func TestProfileFriendsInCommonAndStatusLine(t *testing.T) {
	srv := testutil.New(t) // free-now posts expire by the wall clock: keep the real time
	viewer := srv.Signup(t, "Vi Viewer")
	person := srv.Signup(t, "Pat Person")
	stranger := srv.Signup(t, "Stan Stranger")
	for _, name := range []string{"eve", "Cal", "ben", "Ana"} {
		common := srv.Signup(t, name)
		befriend(t, srv, viewer, common)
		befriend(t, srv, person, common)
	}
	befriend(t, srv, person, srv.Signup(t, "Dee Only Theirs"))
	befriend(t, srv, viewer, srv.Signup(t, "Mo Only Mine"))

	// Four in common: the count, and the first three by name.
	p := profileOf(t, srv, viewer, person.UserID)
	var names []string
	for _, ref := range p.MutualFriends.People {
		names = append(names, ref.Name)
	}
	if p.MutualFriends.Count != 4 || !reflect.DeepEqual(names, []string{"Ana", "ben", "Cal"}) {
		t.Fatalf("friends in common: %d %v", p.MutualFriends.Count, names)
	}
	if p.Relation != contract.ProfileNone || p.StatusLine != nil {
		t.Fatalf("not friends: %+v", p)
	}
	if p := profileOf(t, srv, stranger, person.UserID); p.MutualFriends.Count != 0 || len(p.MutualFriends.People) != 0 {
		t.Fatalf("no friends in common: %+v", p.MutualFriends)
	}

	// A friend who is free says so to their friends, not to anyone else.
	befriend(t, srv, viewer, person)
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostEveryone, midtown, "Midtown", nil), person).Expect(t, http.StatusCreated)
	if p := profileOf(t, srv, viewer, person.UserID); p.Relation != contract.ProfileFriend || p.StatusLine == nil ||
		!strings.HasPrefix(*p.StatusLine, "Free until ") {
		t.Fatalf("a free friend: %+v", p)
	}
	if p := profileOf(t, srv, stranger, person.UserID); p.StatusLine != nil {
		t.Fatalf("a stranger sees the status line %q", *p.StatusLine)
	}
}

func TestProfilePlansFollowForumVisibility(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	host := srv.Signup(t, "Hana Host")
	friend := srv.Signup(t, "Fran Friend")
	stranger := srv.Signup(t, "Stan Stranger")
	befriend(t, srv, host, friend)
	setUser(t, srv, stranger, home(midtown))
	now := srv.Clock.Now()
	visibility := func(v string) func(*models.Itinerary) {
		return func(it *models.Itinerary) { it.Visibility = v }
	}
	open := insertPlan(t, srv, plan(host.UserID, now.Add(2*time.Hour), techSquare))
	friendsOnly := insertPlan(t, srv, plan(host.UserID, now.Add(5*time.Hour), techSquare, visibility(models.VisibilityFriends)))
	insertPlan(t, srv, plan(host.UserID, now.Add(3*time.Hour), techSquare, visibility(models.VisibilityJustMe)))
	insertPlan(t, srv, plan(friend.UserID, now.Add(time.Hour), techSquare)) // someone else's
	// Over: never listed, but done (the one still marked active is flipped to past).
	insertPlan(t, srv, plan(host.UserID, now.Add(-48*time.Hour), techSquare))
	insertPlan(t, srv, plan(host.UserID, now.Add(-72*time.Hour), techSquare, func(it *models.Itinerary) {
		it.MemberIDs = []string{host.UserID, friend.UserID}
		it.Status = models.ItineraryPast
	}))

	// A stranger sees the open plan only, as the Forum shows it (distance from their home base).
	p := profileOf(t, srv, stranger, host.UserID)
	if got := planIDs(p); !reflect.DeepEqual(got, []string{open.ID}) {
		t.Fatalf("a stranger's view: %v", got)
	}
	post := p.OpenPlans[0]
	if post.Type != contract.PostPlan || post.Author.ID != host.UserID || post.IsFriend || post.JoinStatus != contract.JoinNone ||
		post.Title == nil || *post.Title != "Sunset walk" || post.When == nil || post.Meta != "Hosting · 0.6 mi away" || post.GoingCount != 1 {
		t.Fatalf("plan post: %+v", post)
	}
	if p.SidequestsDone != 2 {
		t.Fatalf("host's sidequests done: %d", p.SidequestsDone)
	}

	// A friend sees the friends-only one too, soonest first; so does the host on their own profile.
	p = profileOf(t, srv, friend, host.UserID)
	if got := planIDs(p); !reflect.DeepEqual(got, []string{open.ID, friendsOnly.ID}) {
		t.Fatalf("a friend's view: %v", got)
	}
	if !p.OpenPlans[0].IsFriend || !p.OpenPlans[1].FriendsOnly || p.OpenPlans[0].FriendsOnly {
		t.Fatalf("friend flags: %+v", p.OpenPlans)
	}
	own := profileOf(t, srv, host, host.UserID)
	if got := planIDs(own); !reflect.DeepEqual(got, []string{open.ID, friendsOnly.ID}) || own.OpenPlans[0].JoinStatus != contract.JoinJoined {
		t.Fatalf("own plans: %v %+v", got, own.OpenPlans)
	}
	if p := profileOf(t, srv, host, friend.UserID); p.SidequestsDone != 1 || len(p.OpenPlans) != 1 {
		t.Fatalf("friend's profile: done %d, plans %v", p.SidequestsDone, planIDs(p))
	}

	// Once in, the card says so and leads to the plan's group chat.
	var joined contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+open.ID+"/join-requests", nil, friend).Expect(t, http.StatusOK).JSON(t, &joined)
	p = profileOf(t, srv, friend, host.UserID)
	if post := p.OpenPlans[0]; post.JoinStatus != contract.JoinJoined || post.ThreadID == nil || joined.ThreadID == nil ||
		*post.ThreadID != *joined.ThreadID || post.GoingCount != 2 {
		t.Fatalf("joined plan: %+v", post)
	}
}

func TestProfileTasteMatchLikesAndReasons(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Vi Viewer")
	person := srv.Signup(t, "Pat Person")
	noTaste := srv.Signup(t, "Nat NoTaste")
	bot := srv.Signup(t, "Bo Bot")
	setTaste(t, srv, viewer, 0.5, nil)
	setTaste(t, srv, person, 0.8, nil)
	setTaste(t, srv, bot, 0.9, bson.M{"roles": []string{"bot"}})

	// Without the ML service the profile still answers, without a match.
	if p := profileOf(t, srv, viewer, person.UserID); p.Compatibility != nil {
		t.Fatalf("match without ML: %d", *p.Compatibility)
	}
	calls := fakeMatchML(t, srv)
	if p := profileOf(t, srv, viewer, person.UserID); p.Compatibility == nil || *p.Compatibility != 80 {
		t.Fatalf("match: %v", p.Compatibility)
	}
	// No match without both taste vectors, across catalogs (a bot plans in Saltlight) or for yourself.
	before := *calls
	for _, s := range []*testutil.Session{noTaste, bot, viewer} {
		if p := profileOf(t, srv, viewer, s.UserID); p.Compatibility != nil {
			t.Errorf("%s: match %d", s.User.Name, *p.Compatibility)
		}
	}
	if p := profileOf(t, srv, noTaste, person.UserID); p.Compatibility != nil {
		t.Errorf("viewer without taste: match %d", *p.Compatibility)
	}
	if *calls != before {
		t.Errorf("%d ML calls for profiles that can't be matched", *calls-before)
	}

	// Likes: what they said in Setup, blended with what their ratings taught us.
	setUser(t, srv, viewer, bson.M{"prefs.ratings": bson.M{"live_music": 5, "food": 4, "outdoors": 2, "nightlife": 3}})
	setUser(t, srv, person, bson.M{"prefs.ratings": bson.M{"live_music": 5, "nightlife": 4, "food": 3, "museums": 1},
		"taste.tags": bson.M{"food": 0.9, "nightlife": 0.5}})
	p := profileOf(t, srv, viewer, person.UserID)
	if want := []string{"Live music", "Food & drinks", "Nightlife"}; !reflect.DeepEqual(p.Likes, want) {
		t.Errorf("likes: %v, want %v", p.Likes, want)
	}
	if want := []string{"You both love live music", "You're both foodies"}; !reflect.DeepEqual(p.MatchReasons, want) {
		t.Errorf("reasons: %v, want %v", p.MatchReasons, want)
	}
	if own := profileOf(t, srv, viewer, viewer.UserID); !reflect.DeepEqual(own.Likes, []string{"Live music", "Food & drinks"}) ||
		len(own.MatchReasons) != 0 {
		t.Errorf("own likes %v, reasons %v", own.Likes, own.MatchReasons)
	}

	// At most five likes and three reasons, the strongest first.
	all := bson.M{}
	for _, key := range contract.TripTypes {
		all[key] = 5
	}
	setUser(t, srv, viewer, bson.M{"prefs.ratings": all})
	setUser(t, srv, person, bson.M{"prefs.ratings": all, "taste.tags": bson.M{"food": 0.9, "outdoors": 0.1}})
	p = profileOf(t, srv, viewer, person.UserID)
	if want := []string{"Food & drinks", "Museums & art", "Live music", "Nightlife", "Sports & games"}; !reflect.DeepEqual(p.Likes, want) {
		t.Errorf("five likes: %v, want %v", p.Likes, want)
	}
	if want := []string{"You're both foodies", "You both love museums and art", "You both love live music"}; !reflect.DeepEqual(p.MatchReasons, want) {
		t.Errorf("three reasons: %v, want %v", p.MatchReasons, want)
	}
}

func TestProfileKeepsAMinorsSchoolForFriends(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Vi Viewer")
	teen := srv.Signup(t, "Tee Teen")
	adult := srv.Signup(t, "Al Adult")
	setUser(t, srv, teen, bson.M{"school": "Midtown High", "birthDate": testutil.Fixed.AddDate(-15, 0, 0)})
	setUser(t, srv, adult, bson.M{"school": "Georgia Tech"})

	if p := profileOf(t, srv, viewer, teen.UserID); p.School != nil {
		t.Fatalf("a stranger sees a minor's school: %s", *p.School)
	}
	if p := profileOf(t, srv, viewer, adult.UserID); p.School == nil || *p.School != "Georgia Tech" {
		t.Fatalf("adult's school: %v", p.School)
	}
	if p := profileOf(t, srv, teen, teen.UserID); p.School == nil {
		t.Fatal("own school hidden")
	}
	befriend(t, srv, viewer, teen)
	if p := profileOf(t, srv, viewer, teen.UserID); p.School == nil || *p.School != "Midtown High" {
		t.Fatalf("a friend's school: %v", p.School)
	}
}
