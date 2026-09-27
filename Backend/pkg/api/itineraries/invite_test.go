package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// befriend makes each of others an accepted friend of sess.
func befriend(t *testing.T, srv *testutil.Server, sess *testutil.Session, others ...*testutil.Session) {
	t.Helper()
	for _, o := range others {
		if _, _, err := srv.Store.Friends().Befriend(context.Background(), sess.UserID, o.UserID); err != nil {
			t.Fatal(err)
		}
	}
}

// bringing is the example plan (friends-visible, max 6) with invite_user_ids.
func bringing(t *testing.T, ids ...string) contract.CreateItineraryRequest {
	t.Helper()
	req := exampleRequest(t)
	req.InviteUserIDs = ids
	return req
}

// activeIDs is GET /itineraries for sess, as ids.
func activeIDs(t *testing.T, srv *testutil.Server, sess *testutil.Session) []string {
	t.Helper()
	var list []contract.Itinerary
	srv.Do(t, "GET", "/itineraries", nil, sess).Expect(t, http.StatusOK).JSON(t, &list)
	ids := []string{}
	for _, it := range list {
		ids = append(ids, it.ID)
	}
	return ids
}

// pushed is the payloads of one event type sent to one user (not broadcasts).
func pushed[T any](t *testing.T, srv *testutil.Server, userID, typ string) []T {
	t.Helper()
	var out []T
	for _, e := range srv.Events.Of(typ) {
		if !e.Broadcast && e.UserID == userID {
			var v T
			testutil.EventData(t, e, &v)
			out = append(out, v)
		}
	}
	return out
}

// storedPlan is the itinerary document.
func storedPlan(t *testing.T, srv *testutil.Server, id string) *models.Itinerary {
	t.Helper()
	doc, err := srv.Store.Itineraries().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestCreateBringsFriendsAlong saves a plan with invite_user_ids: the friends
// are members from the start, as after an accepted join (Home, join records,
// the group thread), the thread opens with the host's line, and each friend
// hears what a join sends.
func TestCreateBringsFriendsAlong(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	host := srv.Signup(t, "Jordan Lee")
	maya := srv.Signup(t, "Maya Rivera")
	dev := srv.Signup(t, "Dev Patel")
	stranger := srv.Signup(t, "Stan Stranger")
	befriend(t, srv, host, maya, dev)
	ctx := context.Background()
	srv.Events.Reset()

	// Duplicates and the host are ignored; the friends keep the order asked.
	it := create(t, srv, host, bringing(t, maya.UserID, host.UserID, dev.UserID, maya.UserID))
	if !it.IsHost || it.GoingCount != 3 || it.Visibility != contract.VisibilityFriends || it.MaxGroupSize == nil || *it.MaxGroupSize != 6 {
		t.Fatalf("the host's plan: %+v", it)
	}
	for _, s := range stops(it) {
		if s.Kind != contract.KindGroup || len(s.People) != 3 || s.People[0].ID != host.UserID ||
			s.People[1].ID != maya.UserID || s.People[2].ID != dev.UserID || s.People[1].Name != "Maya Rivera" {
			t.Fatalf("a stop shows who's going: %+v", s)
		}
	}
	doc := storedPlan(t, srv, it.ID)
	if !slices.Equal(doc.MemberIDs, []string{host.UserID, maya.UserID, dev.UserID}) || doc.HostID != host.UserID || doc.ThreadID == "" {
		t.Fatalf("stored plan: members %v host %s thread %q", doc.MemberIDs, doc.HostID, doc.ThreadID)
	}

	// Each friend has it on Home, as a member who joined.
	for _, s := range []*testutil.Session{maya, dev} {
		if ids := activeIDs(t, srv, s); !slices.Equal(ids, []string{it.ID}) {
			t.Fatalf("GET /itineraries as %s: %v", s.User.Name, ids)
		}
		if theirs := getItinerary(t, srv, s, it.ID); theirs.IsHost || theirs.GoingCount != 3 {
			t.Fatalf("%s's plan: is_host %v, going_count %d", s.User.Name, theirs.IsHost, theirs.GoingCount)
		}
		rec, err := srv.Store.Joins().RecordOf(ctx, it.ID, s.UserID)
		if err != nil || rec.Status != models.JoinAccepted || rec.ItineraryID != it.ID {
			t.Fatalf("join record of %s: %v %+v", s.User.Name, err, rec)
		}
	}
	if ids := activeIDs(t, srv, stranger); len(ids) != 0 {
		t.Fatalf("a stranger has %v", ids)
	}
	srv.Do(t, "GET", "/itineraries/"+it.ID, nil, stranger).Expect(t, http.StatusNotFound)

	// The group thread: everyone in it, opened by the host's line.
	th, err := srv.Store.Threads().Get(ctx, doc.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if !th.IsGroup || th.ItineraryID != it.ID || th.Title != "Rooftop + murals" || len(th.MemberIDs) != 3 ||
		th.Unread[maya.UserID] != 1 || th.Unread[dev.UserID] != 1 || th.Unread[host.UserID] != 0 {
		t.Fatalf("group thread: %+v", th)
	}
	for _, s := range []*testutil.Session{host, maya} {
		var msgs []contract.Message
		srv.Do(t, "GET", "/threads/"+th.ID+"/messages", nil, s).Expect(t, http.StatusOK).JSON(t, &msgs)
		name := "Jordan"
		if s == host {
			name = "You"
		}
		if len(msgs) != 1 || msgs[0].SenderID != host.UserID || msgs[0].SenderName != name || msgs[0].Text != "Jordan added Maya and Dev" {
			t.Fatalf("the thread as %s: %+v", s.User.Name, msgs)
		}
	}
	srv.Do(t, "GET", "/threads/"+th.ID, nil, stranger).Expect(t, http.StatusNotFound)

	// Events: what an accepted join sends each friend, and the thread to everyone in it.
	for _, s := range []*testutil.Session{maya, dev} {
		joins := pushed[realtime.JoinUpdateData](t, srv, s.UserID, realtime.EventJoinUpdate)
		if len(joins) != 1 || joins[0].PostID != it.ID || joins[0].Result.Status != contract.JoinJoined ||
			joins[0].Result.ItineraryID == nil || *joins[0].Result.ItineraryID != it.ID ||
			joins[0].Result.ThreadID == nil || *joins[0].Result.ThreadID != th.ID {
			t.Fatalf("join.update to %s: %+v", s.User.Name, joins)
		}
		// itinerary.updated, rendered for them exactly as GET /itineraries/{id} is.
		var sent []any
		for _, e := range srv.Events.Of(realtime.EventItineraryUpdated) {
			if e.UserID == s.UserID {
				sent = append(sent, e.Data)
			}
		}
		if len(sent) != 1 {
			t.Fatalf("itinerary.updated to %s: %d", s.User.Name, len(sent))
		}
		raw, err := json.Marshal(sent[0])
		if err != nil {
			t.Fatal(err)
		}
		got := srv.Do(t, "GET", "/itineraries/"+it.ID, nil, s).Expect(t, http.StatusOK)
		if canonicalJSON(t, raw) != canonicalJSON(t, got.Body) {
			t.Fatalf("itinerary.updated to %s differs from GET:\n%s\n%s", s.User.Name, raw, got.Body)
		}
	}
	for _, s := range []*testutil.Session{host, maya, dev} {
		threads := pushed[contract.ChatThread](t, srv, s.UserID, realtime.EventThreadUpdated)
		if len(threads) != 1 || threads[0].ID != th.ID || !threads[0].IsGroup || len(threads[0].Members) != 3 || threads[0].Title != "Rooftop + murals" {
			t.Fatalf("thread.updated to %s: %+v", s.User.Name, threads)
		}
	}
	if n := len(pushed[contract.Itinerary](t, srv, host.UserID, realtime.EventItineraryUpdated)); n != 0 {
		t.Errorf("the host has the response, not itinerary.updated (%d)", n)
	}
	if n := len(srv.Events.Of(realtime.EventJoinRequest)); n != 0 {
		t.Errorf("nobody asked to join, yet %d join.request", n)
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("forum.update: %d", forumUpdates(srv))
	}
	for _, e := range srv.Events.For(stranger.UserID) {
		if !e.Broadcast {
			t.Errorf("a stranger heard %s", e.Type)
		}
	}
}

// TestCreateRefusesInvitesThatDontFit: strangers, pending requests, unknown
// ids, more than 12 people and a group too small for everyone are 400s, and
// nothing is saved; the host alone in invite_user_ids is no invite at all.
func TestCreateRefusesInvitesThatDontFit(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	host := srv.Signup(t, "Jordan Refused")
	maya := srv.Signup(t, "Maya Friend")
	dev := srv.Signup(t, "Dev Friend")
	asked := srv.Signup(t, "Ada Asked")
	stranger := srv.Signup(t, "Stan Stranger")
	befriend(t, srv, host, maya, dev)
	if _, _, err := srv.Store.Friends().CreateRequest(context.Background(), host.UserID, asked.UserID, ""); err != nil {
		t.Fatal(err)
	}
	many := make([]string, 13)
	for i := range many {
		many[i] = fmt.Sprintf("64b0000000000000000000%02x", i)
	}
	small := bringing(t, maya.UserID, dev.UserID)
	small.MaxGroupSize = testutil.Ptr(2)
	cases := []struct {
		name string
		req  contract.CreateItineraryRequest
		want string
	}{
		{"a stranger", bringing(t, maya.UserID, stranger.UserID), itineraries.MsgInviteFriendsOnly},
		{"a pending friend request", bringing(t, asked.UserID), itineraries.MsgInviteFriendsOnly},
		{"an unknown user", bringing(t, "64b00000000000000000ffff"), itineraries.MsgInviteFriendsOnly},
		{"not a user id", bringing(t, "u-mr"), itineraries.MsgInviteFriendsOnly},
		{"13 people", bringing(t, many...), itineraries.MsgInviteTooMany},
		{"a group of 2 for 3", small, itineraries.MsgInviteNoRoom},
	}
	for _, c := range cases {
		if res := srv.Do(t, "POST", "/itineraries", c.req, host).Expect(t, http.StatusBadRequest); res.Message() != c.want {
			t.Errorf("%s: %q, want %q", c.name, res.Message(), c.want)
		}
	}
	for _, coll := range []string{store.CollItineraries, store.CollThreads, store.CollMessages, store.CollJoinRequests} {
		if n := count(t, srv, coll, bson.M{}); n != 0 {
			t.Fatalf("a refused plan left %d documents in %s", n, coll)
		}
	}

	// The host alone is no invite: a plain plan, no thread.
	alone := create(t, srv, host, bringing(t, host.UserID, host.UserID))
	if alone.GoingCount != 1 || storedPlan(t, srv, alone.ID).ThreadID != "" {
		t.Fatalf("the host inviting themselves: %+v", alone)
	}
	// Room for exactly the host and both friends.
	fits := bringing(t, maya.UserID, dev.UserID)
	fits.MaxGroupSize = testutil.Ptr(3)
	if it := create(t, srv, host, fits); it.GoingCount != 3 {
		t.Fatalf("a group of 3 for 3: going_count %d", it.GoingCount)
	}
}

// TestCreateJustMeWithFriends: a just_me plan that brings friends is saved
// friends-visible (and announced to the forum like one).
func TestCreateJustMeWithFriends(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	host := srv.Signup(t, "Jordan Solo")
	maya := srv.Signup(t, "Maya Along")
	befriend(t, srv, host, maya)
	srv.Events.Reset()

	req := bringing(t, maya.UserID)
	req.Visibility, req.LockAt, req.MaxGroupSize = contract.VisibilityJustMe, nil, nil
	it := create(t, srv, host, req)
	if it.Visibility != contract.VisibilityFriends || it.GoingCount != 2 || it.MaxGroupSize != nil {
		t.Fatalf("just me + a friend: %+v", it)
	}
	if doc := storedPlan(t, srv, it.ID); doc.Visibility != models.VisibilityFriends {
		t.Fatalf("stored visibility %q", doc.Visibility)
	}
	if forumUpdates(srv) != 1 {
		t.Errorf("forum.update: %d", forumUpdates(srv))
	}
	if theirs := getItinerary(t, srv, maya, it.ID); theirs.GoingCount != 2 || theirs.IsHost {
		t.Fatalf("Maya's view: %+v", theirs)
	}
}

// TestInvitedFriendsCanLeave: a friend who was brought along leaves like
// anyone who joined, with POST /itineraries/{id}/leave or from the forum post.
func TestInvitedFriendsCanLeave(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	host := srv.Signup(t, "Jordan Stays")
	maya := srv.Signup(t, "Maya Leaves")
	dev := srv.Signup(t, "Dev Leaves")
	befriend(t, srv, host, maya, dev)
	it := create(t, srv, host, bringing(t, maya.UserID, dev.UserID))
	threadID := storedPlan(t, srv, it.ID).ThreadID
	ctx := context.Background()

	srv.Events.Reset()
	srv.Do(t, "POST", "/itineraries/"+it.ID+"/leave", nil, maya).Expect(t, http.StatusNoContent)
	if ids := activeIDs(t, srv, maya); len(ids) != 0 {
		t.Fatalf("Maya still has %v", ids)
	}
	srv.Do(t, "GET", "/threads/"+threadID, nil, maya).Expect(t, http.StatusNotFound)
	if left := getItinerary(t, srv, host, it.ID); left.GoingCount != 2 {
		t.Fatalf("going_count after Maya left: %d", left.GoingCount)
	}
	th, err := srv.Store.Threads().Get(ctx, threadID)
	if err != nil || !slices.Equal(th.MemberIDs, []string{host.UserID, dev.UserID}) {
		t.Fatalf("thread after Maya left: %v %+v", err, th)
	}
	if rec, err := srv.Store.Joins().RecordOf(ctx, it.ID, maya.UserID); err != nil || rec.Status != models.JoinCancelled {
		t.Fatalf("Maya's join record: %v %+v", err, rec)
	}
	if n := srv.EventCount(maya.UserID, realtime.EventItineraryRemoved); n != 1 {
		t.Errorf("itinerary.removed to Maya: %d", n)
	}
	for _, s := range []*testutil.Session{host, dev} {
		updates := pushed[contract.Itinerary](t, srv, s.UserID, realtime.EventItineraryUpdated)
		threads := pushed[contract.ChatThread](t, srv, s.UserID, realtime.EventThreadUpdated)
		if len(updates) != 1 || updates[0].GoingCount != 2 || len(threads) != 1 || len(threads[0].Members) != 2 {
			t.Errorf("%s after Maya left: %+v %+v", s.User.Name, updates, threads)
		}
	}

	// Dev leaves from the forum post instead.
	srv.Do(t, "DELETE", "/forum/posts/"+it.ID+"/join-requests", nil, dev).Expect(t, http.StatusNoContent)
	if ids := activeIDs(t, srv, dev); len(ids) != 0 {
		t.Fatalf("Dev still has %v", ids)
	}
	if left := getItinerary(t, srv, host, it.ID); left.GoingCount != 1 {
		t.Fatalf("going_count after both left: %d", left.GoingCount)
	}
}

func TestAddedLine(t *testing.T) {
	for _, c := range []struct {
		friends []string
		want    string
	}{
		{[]string{"Maya"}, "Jordan added Maya"},
		{[]string{"Maya", "Dev"}, "Jordan added Maya and Dev"},
		{[]string{"Maya", "Dev", "Sam"}, "Jordan added Maya, Dev and Sam"},
	} {
		if got := itineraries.AddedLine("Jordan", c.friends); got != c.want {
			t.Errorf("AddedLine(%v) = %q, want %q", c.friends, got, c.want)
		}
	}
}
