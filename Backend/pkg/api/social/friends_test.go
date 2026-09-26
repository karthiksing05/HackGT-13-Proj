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
	"net/http"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func search(t *testing.T, srv *testutil.Server, s *testutil.Session, q string) []contract.UserSearchResult {
	t.Helper()
	var out []contract.UserSearchResult
	srv.Do(t, "GET", "/users/search?q="+q, nil, s).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func friendsOf(t *testing.T, srv *testutil.Server, s *testutil.Session) []contract.Friend {
	t.Helper()
	var out []contract.Friend
	srv.Do(t, "GET", "/friends", nil, s).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func requestsOf(t *testing.T, srv *testutil.Server, s *testutil.Session) []contract.FriendRequest {
	t.Helper()
	var out []contract.FriendRequest
	srv.Do(t, "GET", "/friends/requests", nil, s).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func TestFriendRequests(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Alma Asker")
	b := srv.Signup(t, "Bruno Bee")
	c := srv.Signup(t, "Cleo Sea")
	d := srv.Signup(t, "Dora Dee")

	if res := search(t, srv, a, "bruno"); len(res) != 1 || res[0].Person.ID != b.UserID || res[0].Relation != contract.RelationNone || res[0].RequestID != nil {
		t.Fatalf("search before: %+v", res)
	}
	if res := search(t, srv, a, "alma"); len(res) != 0 {
		t.Fatalf("search never finds yourself: %+v", res)
	}
	if res := search(t, srv, a, ""); res == nil || len(res) != 0 {
		t.Fatalf("empty search: %+v", res)
	}

	var sent contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: b.UserID}, a).Expect(t, http.StatusCreated).JSON(t, &sent)
	if !sent.Outgoing || sent.Person.ID != b.UserID || sent.Note != "Requested just now" {
		t.Fatalf("sent request: %+v", sent)
	}
	incoming := eventsFor[contract.FriendRequest](t, srv, b.UserID, realtime.EventFriendRequest)
	echo := eventsFor[contract.FriendRequest](t, srv, a.UserID, realtime.EventFriendRequest)
	if len(incoming) != 1 || incoming[0].Outgoing || incoming[0].Person.ID != a.UserID || incoming[0].Note != "Wants to be friends" ||
		len(echo) != 1 || !echo[0].Outgoing || echo[0].ID != sent.ID {
		t.Fatalf("friend.request: to B %+v, echo %+v", incoming, echo)
	}
	var again contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: b.UserID}, a).Expect(t, http.StatusOK).JSON(t, &again)
	if again.ID != sent.ID {
		t.Fatalf("sending twice returns the pending request: %+v", again)
	}
	if res := search(t, srv, a, "bruno"); res[0].Relation != contract.RelationOutgoing || res[0].RequestID == nil || *res[0].RequestID != sent.ID {
		t.Fatalf("A's view: %+v", res)
	}
	if res := search(t, srv, b, "alma"); res[0].Relation != contract.RelationIncoming || *res[0].RequestID != sent.ID {
		t.Fatalf("B's view: %+v", res)
	}
	if list := requestsOf(t, srv, a); len(list) != 1 || !list[0].Outgoing {
		t.Fatalf("A's requests: %+v", list)
	}
	if list := requestsOf(t, srv, b); len(list) != 1 || list[0].Outgoing || list[0].Person.ID != a.UserID {
		t.Fatalf("B's requests: %+v", list)
	}
	if res := srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: a.UserID}, a); res.Status != http.StatusBadRequest || res.Message() != social.MsgFriendSelf {
		t.Fatalf("self: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: bson.NewObjectID().Hex()}, a).Expect(t, http.StatusNotFound)

	// Only the recipient accepts; accepting makes friends and a DM, and both hear it.
	srv.Do(t, "POST", "/friends/requests/"+sent.ID+"/accept", nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "POST", "/friends/requests/"+sent.ID+"/accept", nil, c).Expect(t, http.StatusNotFound)
	srv.Events.Reset()
	srv.Do(t, "POST", "/friends/requests/"+sent.ID+"/accept", nil, b).Expect(t, http.StatusNoContent)
	if _, err := srv.Store.Friends().Get(context.Background(), a.UserID, b.UserID); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	dm, err := srv.Store.Threads().DM(context.Background(), a.UserID, b.UserID)
	if err != nil {
		t.Fatalf("DM after accepting: %v", err)
	}
	for _, pair := range [][2]*testutil.Session{{a, b}, {b, a}} {
		status := eventsFor[realtime.FriendStatusData](t, srv, pair[0].UserID, realtime.EventFriendStatus)
		threads := eventsFor[contract.ChatThread](t, srv, pair[0].UserID, realtime.EventThreadUpdated)
		if len(status) != 1 || status[0].UserID != pair[1].UserID || status[0].StatusLine != "Just added" ||
			len(threads) != 1 || threads[0].ID != dm.ID || threads[0].Subtitle != "Just added" {
			t.Fatalf("events for %s: %+v %+v", pair[0].User.Name, status, threads)
		}
	}
	if list := friendsOf(t, srv, a); len(list) != 1 || list[0].Person.ID != b.UserID || list[0].StatusLine != "Just added" || list[0].Activity != contract.ActivityNew {
		t.Fatalf("A's friends: %+v", list)
	}
	if len(requestsOf(t, srv, a)) != 0 || len(requestsOf(t, srv, b)) != 0 {
		t.Fatal("an accepted request leaves both lists")
	}
	srv.Do(t, "POST", "/friends/requests/"+sent.ID+"/accept", nil, b).Expect(t, http.StatusNoContent) // idempotent
	srv.Do(t, "POST", "/friends/requests/"+sent.ID+"/decline", nil, b).Expect(t, http.StatusNotFound)
	if res := srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: a.UserID}, b); res.Status != http.StatusConflict || res.Message() != social.MsgAlreadyFriend {
		t.Fatalf("already friends: %d %s", res.Status, res.Body)
	}
	if res := search(t, srv, a, "bruno"); res[0].Relation != contract.RelationFriend || res[0].RequestID != nil {
		t.Fatalf("friend relation: %+v", res)
	}

	// Decline, cancel (own outgoing only), and asking back accepts.
	var fromC, fromD, fromDToC contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: a.UserID}, c).Expect(t, http.StatusCreated).JSON(t, &fromC)
	srv.Do(t, "POST", "/friends/requests/"+fromC.ID+"/decline", nil, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: a.UserID}, d).Expect(t, http.StatusCreated).JSON(t, &fromD)
	srv.Do(t, "DELETE", "/friends/requests/"+fromD.ID, nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "DELETE", "/friends/requests/"+fromD.ID, nil, d).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", "/friends/requests/"+fromD.ID, nil, d).Expect(t, http.StatusNoContent)
	if len(requestsOf(t, srv, a)) != 0 {
		t.Fatal("declined and withdrawn requests are gone")
	}
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: c.UserID}, d).Expect(t, http.StatusCreated).JSON(t, &fromDToC)
	var back contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: d.UserID}, c).Expect(t, http.StatusOK).JSON(t, &back)
	if back.ID != fromDToC.ID || back.Note != "You're friends now" {
		t.Fatalf("asking back: %+v", back)
	}
	if _, err := srv.Store.Friends().Get(context.Background(), c.UserID, d.UserID); err != nil {
		t.Fatalf("asking back makes friends: %v", err)
	}

	// Removing keeps the DM (now without a status) and tells both sides.
	srv.Events.Reset()
	srv.Do(t, "DELETE", "/friends/"+b.UserID, nil, a).Expect(t, http.StatusNoContent)
	if len(friendsOf(t, srv, a)) != 0 || len(friendsOf(t, srv, b)) != 0 {
		t.Fatal("removed on both sides")
	}
	threads := eventsFor[contract.ChatThread](t, srv, b.UserID, realtime.EventThreadUpdated)
	if len(threads) != 1 || threads[0].ID != dm.ID || threads[0].Subtitle != "From the Forum" {
		t.Fatalf("the kept DM: %+v", threads)
	}
	srv.Do(t, "GET", "/threads/"+dm.ID, nil, a).Expect(t, http.StatusOK)
	srv.Do(t, "DELETE", "/friends/"+b.UserID, nil, a).Expect(t, http.StatusNoContent) // not friends any more: still fine
	srv.Do(t, "DELETE", "/friends/"+bson.NewObjectID().Hex(), nil, a).Expect(t, http.StatusNotFound)
	n, err := srv.Store.Collection(store.CollThreads).CountDocuments(context.Background(), bson.M{"dmKey": models.DMKey(a.UserID, c.UserID)})
	if err != nil || n != 0 {
		t.Fatalf("removing never creates a DM: %d %v", n, err)
	}
}

func TestFriendsPresence(t *testing.T) {
	srv := testutil.New(t)
	me := srv.Signup(t, "Mia Me")
	free := srv.Signup(t, "Fred Free")
	onQuest := srv.Signup(t, "Quinn Quest")
	secret := srv.Signup(t, "Sid Secret")
	busy := srv.Signup(t, "Bea Busy")
	fresh := srv.Signup(t, "Nia New")
	idle := srv.Signup(t, "Ian Idle")
	for _, s := range []*testutil.Session{free, onQuest, secret, busy, fresh, idle} {
		befriend(t, srv, me, s)
	}
	// Everyone but Nia has been a friend for days.
	_, err := srv.Store.Collection(store.CollFriendships).UpdateMany(context.Background(),
		bson.M{"_id": bson.M{"$ne": models.FriendshipID(me.UserID, fresh.UserID)}},
		bson.M{"$set": bson.M{"createdAt": srv.Clock.Now().Add(-72 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	var post contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostFriends, techSquare, "Tech Square", nil), free).Expect(t, http.StatusCreated).JSON(t, &post)
	now := srv.Clock.Now()
	insertPlan(t, srv, plan(onQuest.UserID, now.Add(-time.Hour), techSquare, func(it *models.Itinerary) { it.Title = "Thrift crawl" }))
	insertPlan(t, srv, plan(secret.UserID, now.Add(-time.Hour), techSquare, func(it *models.Itinerary) { it.Visibility = models.VisibilityJustMe }))
	srv.Events.Reset()
	srv.Do(t, "PATCH", "/me", contract.UserPatch{Status: testutil.Ptr(contract.StatusBusy)}, busy).Expect(t, http.StatusOK)
	if got := eventsFor[realtime.FriendStatusData](t, srv, me.UserID, realtime.EventFriendStatus); len(got) != 1 || got[0].UserID != busy.UserID || got[0].StatusLine != "Busy" {
		t.Fatalf("PATCH /me status tells friends: %+v", got)
	}
	srv.Do(t, "PATCH", "/me", contract.UserPatch{Name: testutil.Ptr("Bea B.")}, busy).Expect(t, http.StatusOK)
	if n := direct(srv, me.UserID, realtime.EventFriendStatus); n != 1 {
		t.Fatalf("no friend.status without a status change: %d", n)
	}

	list := friendsOf(t, srv, me)
	lines := make([]string, 0, len(list))
	for _, f := range list {
		lines = append(lines, f.Person.Name+": "+f.StatusLine+" ("+string(f.Activity)+")")
	}
	until := post.Until.In(ny)
	want := []string{
		"Nia New: Just added (new)",
		"Fred Free: Free until " + httpx.ClockShort(until) + " (free)",
		"Ian Idle: Open to plans (free)",
		"Quinn Quest: On a sidequest · Thrift crawl (on_sidequest)",
		"Sid Secret: On a sidequest (on_sidequest)",
		"Bea B.: Busy (busy)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("friends:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// The DM subtitle is the same status line.
	var dm contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: onQuest.UserID}, me).Expect(t, http.StatusOK).JSON(t, &dm)
	if dm.Subtitle != "On a sidequest · Thrift crawl" {
		t.Fatalf("DM subtitle: %q", dm.Subtitle)
	}
}

func TestInvites(t *testing.T) {
	srv := testutil.New(t)
	inviter := srv.Signup(t, "Ines Inviter")
	guest := srv.Signup(t, "Gil Guest")
	late := srv.Signup(t, "Lou Late")

	var link contract.URLResponse
	srv.Do(t, "POST", "/invites", nil, inviter).Expect(t, http.StatusCreated).JSON(t, &link)
	code := strings.TrimPrefix(link.URL, social.InviteBaseURL)
	if !strings.HasPrefix(link.URL, "https://sidequests.app/invite/") || len(code) != 8 || strings.ToUpper(code) != code {
		t.Fatalf("invite link: %s", link.URL)
	}
	var same contract.URLResponse
	srv.Do(t, "POST", "/invites", nil, inviter).Expect(t, http.StatusOK).JSON(t, &same)
	if same.URL != link.URL {
		t.Fatalf("one live link per user: %s vs %s", same.URL, link.URL)
	}

	srv.Events.Reset()
	var friend contract.Friend
	srv.Do(t, "POST", "/invites/"+strings.ToLower(code)+"/accept", nil, guest).Expect(t, http.StatusOK).JSON(t, &friend)
	if friend.Person.ID != inviter.UserID || friend.StatusLine != "Just added" || friend.Activity != contract.ActivityNew {
		t.Fatalf("accepted invite: %+v", friend)
	}
	if _, err := srv.Store.Friends().Get(context.Background(), inviter.UserID, guest.UserID); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	status := eventsFor[realtime.FriendStatusData](t, srv, inviter.UserID, realtime.EventFriendStatus)
	threads := eventsFor[contract.ChatThread](t, srv, inviter.UserID, realtime.EventThreadUpdated)
	if len(status) != 1 || status[0].UserID != guest.UserID || len(threads) != 1 || threads[0].Title != "Gil Guest" {
		t.Fatalf("the inviter hears about it: %+v %+v", status, threads)
	}
	inv, err := srv.Store.Invites().Get(context.Background(), code)
	if err != nil || inv.Uses != 1 {
		t.Fatalf("uses: %v %+v", err, inv)
	}
	srv.Do(t, "POST", "/invites/"+code+"/accept", nil, guest).Expect(t, http.StatusOK).JSON(t, &friend) // already friends
	if inv, _ := srv.Store.Invites().Get(context.Background(), code); inv.Uses != 1 {
		t.Fatalf("a repeat accept is not a new use: %+v", inv)
	}

	for _, tc := range []struct {
		code string
		who  *testutil.Session
		msg  string
	}{
		{code, inviter, social.MsgInviteOwn},
		{"ZZZZZZZZ", late, social.MsgInviteBroken},
		{"short", late, social.MsgInviteBroken},
	} {
		if res := srv.Do(t, "POST", "/invites/"+tc.code+"/accept", nil, tc.who); res.Status != http.StatusBadRequest || res.Message() != tc.msg {
			t.Fatalf("%s: %d %s", tc.code, res.Status, res.Body)
		}
	}
	if _, err := srv.Store.Collection(store.CollInvites).UpdateOne(context.Background(), bson.M{"_id": code},
		bson.M{"$set": bson.M{"expiresAt": srv.Clock.Now().Add(-time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	if res := srv.Do(t, "POST", "/invites/"+code+"/accept", nil, late); res.Status != http.StatusBadRequest || res.Message() != social.MsgInviteBroken {
		t.Fatalf("expired: %d %s", res.Status, res.Body)
	}
	var fresh contract.URLResponse
	srv.Do(t, "POST", "/invites", nil, inviter).Expect(t, http.StatusCreated).JSON(t, &fresh)
	if fresh.URL == link.URL {
		t.Fatal("an expired link is replaced")
	}
}
