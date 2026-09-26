package social_test

import (
	"Backend/pkg/api/social"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestJoinPlan(t *testing.T) {
	srv := testutil.New(t)
	host := srv.Signup(t, "Hana Host")
	member := srv.Signup(t, "Milo Member")
	joiner := srv.Signup(t, "Jo Joiner")
	stranger := srv.Signup(t, "Stan Stranger")
	now := srv.Clock.Now()
	it := insertPlan(t, srv, plan(host.UserID, now.Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.Title = "Rooftop + murals"
		it.MemberIDs = []string{host.UserID, member.UserID}
		it.MaxGroupSize = testutil.Ptr(6)
		it.LockAt = testutil.Ptr(now.Add(3 * time.Hour))
	}))
	// A private note and a rating of the joiner's are folded into their itinerary.updated.
	note := "bring cash"
	if _, err := srv.Store.Collection(store.CollItemStates).InsertOne(context.Background(), models.ItemState{
		ID: models.PairID(joiner.UserID, it.Items[1].ID), UserID: joiner.UserID, ItemID: it.Items[1].ID, ItineraryID: it.ID,
		Notes: &note, NotesScope: "private", TransitMode: "marta"}); err != nil {
		t.Fatal(err)
	}
	srv.Events.Reset()

	var res contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusOK).JSON(t, &res)
	if res.Status != contract.JoinJoined || res.ItineraryID == nil || *res.ItineraryID != it.ID || res.ThreadID == nil {
		t.Fatalf("join result: %+v", res)
	}
	threadID := *res.ThreadID
	stored := loadPlan(t, srv, it.ID)
	if !stored.IsMember(joiner.UserID) || len(stored.MemberIDs) != 3 || stored.ThreadID != threadID {
		t.Fatalf("itinerary after join: %+v", stored)
	}
	th, err := srv.Store.Threads().Get(context.Background(), threadID)
	if err != nil || !th.IsGroup || th.ItineraryID != it.ID || len(th.MemberIDs) != 3 || th.Title != "Rooftop + murals" {
		t.Fatalf("group thread: %v %+v", err, th)
	}
	rec, err := srv.Store.Joins().RecordOf(context.Background(), it.ID, joiner.UserID)
	if err != nil || rec.Status != models.JoinAccepted || rec.ItineraryID != it.ID {
		t.Fatalf("join record: %v %+v", err, rec)
	}

	// Events: the joiner, the host and every member.
	updates := eventsFor[realtime.JoinUpdateData](t, srv, joiner.UserID, realtime.EventJoinUpdate)
	if len(updates) != 1 || updates[0].PostID != it.ID || updates[0].Result.Status != contract.JoinJoined || *updates[0].Result.ThreadID != threadID {
		t.Fatalf("join.update: %+v", updates)
	}
	requests := eventsFor[realtime.JoinRequestData](t, srv, host.UserID, realtime.EventJoinRequest)
	if len(requests) != 1 || requests[0].From.ID != joiner.UserID || requests[0].From.Name != "Jo Joiner" || requests[0].PostID != it.ID {
		t.Fatalf("join.request: %+v", requests)
	}
	if direct(srv, member.UserID, realtime.EventJoinRequest) != 0 || direct(srv, joiner.UserID, realtime.EventJoinRequest) != 0 {
		t.Fatal("join.request goes to the host only")
	}
	for _, s := range []*testutil.Session{host, member, joiner} {
		its := eventsFor[contract.Itinerary](t, srv, s.UserID, realtime.EventItineraryUpdated)
		threads := eventsFor[contract.ChatThread](t, srv, s.UserID, realtime.EventThreadUpdated)
		if len(its) != 1 || its[0].ID != it.ID || its[0].GoingCount != 3 || its[0].IsHost != (s == host) {
			t.Fatalf("itinerary.updated for %s: %+v", s.User.Name, its)
		}
		if len(threads) != 1 || threads[0].ID != threadID || !threads[0].IsGroup || len(threads[0].Members) != 3 {
			t.Fatalf("thread.updated for %s: %+v", s.User.Name, threads)
		}
		stop := its[0].Items[1]
		if stop.Kind != contract.KindGroup || len(stop.People) != 3 || its[0].Items[0].Kind != contract.KindTransit {
			t.Fatalf("items for %s: %+v", s.User.Name, its[0].Items)
		}
		mine := s == joiner
		if (stop.Notes != nil) != mine || (stop.TransitMode != nil) != mine {
			t.Fatalf("per-viewer item state leaked or missing for %s: %+v", s.User.Name, stop)
		}
	}
	if broadcasts(srv, realtime.EventForumUpdate) != 1 || direct(srv, stranger.UserID, realtime.EventItineraryUpdated) != 0 {
		t.Fatal("forum.update broadcast / no itinerary for outsiders")
	}

	// The group thread works for the joiner right away.
	var thread contract.ChatThread
	srv.Do(t, "GET", "/threads/"+threadID, nil, joiner).Expect(t, http.StatusOK).JSON(t, &thread)
	if thread.Title != "Rooftop + murals" || len(thread.Faces) != 2 || thread.Chips[0] != "3 people" || thread.Chips[1] != "Settled up" || thread.Chips[2] != "0 photos" {
		t.Fatalf("group thread for the joiner: %+v", thread)
	}

	// Joining again is idempotent: same thread, no new events.
	srv.Events.Reset()
	var again contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusOK).JSON(t, &again)
	if again.Status != contract.JoinJoined || *again.ThreadID != threadID || len(srv.Events.Events()) != 0 {
		t.Fatalf("second join: %+v, events %d", again, len(srv.Events.Events()))
	}
	if res := srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, host); res.Status != http.StatusBadRequest || res.Message() != social.MsgJoinOwnPlan {
		t.Fatalf("host joining: %d %s", res.Status, res.Body)
	}

	// Closed (locked or started), full, friends-only, free posts, missing.
	closed := insertPlan(t, srv, plan(host.UserID, now.Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.LockAt = testutil.Ptr(now.Add(-time.Minute))
	}))
	started := insertPlan(t, srv, plan(host.UserID, now.Add(-time.Minute), techSquare))
	full := insertPlan(t, srv, plan(host.UserID, now.Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.MaxGroupSize = testutil.Ptr(2)
		it.MemberIDs = []string{host.UserID, member.UserID}
	}))
	for id, want := range map[string]contract.JoinStatus{closed.ID: contract.JoinClosed, started.ID: contract.JoinClosed, full.ID: contract.JoinFull} {
		var r contract.JoinResult
		srv.Do(t, "POST", "/forum/posts/"+id+"/join-requests", nil, stranger).Expect(t, http.StatusOK).JSON(t, &r)
		if r.Status != want || r.ThreadID != nil || r.ItineraryID != nil {
			t.Fatalf("join %s: %+v, want %s", id, r, want)
		}
	}
	friendsOnly := insertPlan(t, srv, plan(host.UserID, now.Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.Visibility = models.VisibilityFriends
	}))
	srv.Do(t, "POST", "/forum/posts/"+friendsOnly.ID+"/join-requests", nil, stranger).Expect(t, http.StatusNotFound)
	justMe := insertPlan(t, srv, plan(host.UserID, now.Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.Visibility = models.VisibilityJustMe
	}))
	srv.Do(t, "POST", "/forum/posts/"+justMe.ID+"/join-requests", nil, stranger).Expect(t, http.StatusNotFound)
	srv.Do(t, "POST", "/forum/posts/nope/join-requests", nil, stranger).Expect(t, http.StatusNotFound)
	var free contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostEveryone, techSquare, "Tech Square", nil), host).Expect(t, http.StatusCreated).JSON(t, &free)
	if res := srv.Do(t, "POST", "/forum/posts/"+free.ID+"/join-requests", nil, stranger); res.Status != http.StatusBadRequest || res.Message() != social.MsgJoinFreePost {
		t.Fatalf("joining a free post: %d %s", res.Status, res.Body)
	}
	befriend(t, srv, host, stranger)
	var friendJoin contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+friendsOnly.ID+"/join-requests", nil, stranger).Expect(t, http.StatusOK).JSON(t, &friendJoin)
	if friendJoin.Status != contract.JoinJoined {
		t.Fatalf("a friend joins a friends-only plan: %+v", friendJoin)
	}
}

func TestCancelJoinLeavesThePlan(t *testing.T) {
	srv := testutil.New(t)
	host := srv.Signup(t, "Hal Host")
	joiner := srv.Signup(t, "Jen Joiner")
	other := srv.Signup(t, "Otto Other")
	it := insertPlan(t, srv, plan(host.UserID, srv.Clock.Now().Add(4*time.Hour), techSquare))
	var res contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusOK).JSON(t, &res)
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, other).Expect(t, http.StatusOK)
	srv.Events.Reset()

	srv.Do(t, "DELETE", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusNoContent)
	stored := loadPlan(t, srv, it.ID)
	th, err := srv.Store.Threads().Get(context.Background(), *res.ThreadID)
	if err != nil || stored.IsMember(joiner.UserID) || slices.Contains(th.MemberIDs, joiner.UserID) || len(th.MemberIDs) != 2 {
		t.Fatalf("after leaving: %v %+v %+v", err, stored.MemberIDs, th)
	}
	if rec, err := srv.Store.Joins().RecordOf(context.Background(), it.ID, joiner.UserID); err != nil || rec.Status != models.JoinCancelled {
		t.Fatalf("record: %v %+v", err, rec)
	}
	removed := eventsFor[realtime.ItineraryRemovedData](t, srv, joiner.UserID, realtime.EventItineraryRemoved)
	if len(removed) != 1 || removed[0].ItineraryID != it.ID || direct(srv, joiner.UserID, realtime.EventThreadUpdated) != 0 {
		t.Fatalf("leaver events: %+v", removed)
	}
	for _, s := range []*testutil.Session{host, other} {
		its := eventsFor[contract.Itinerary](t, srv, s.UserID, realtime.EventItineraryUpdated)
		if len(its) != 1 || its[0].GoingCount != 2 || direct(srv, s.UserID, realtime.EventThreadUpdated) != 1 {
			t.Fatalf("remaining member %s: %+v", s.User.Name, its)
		}
	}
	if broadcasts(srv, realtime.EventForumUpdate) != 1 {
		t.Fatal("leaving must broadcast forum.update")
	}
	srv.Do(t, "GET", "/threads/"+*res.ThreadID, nil, joiner).Expect(t, http.StatusNotFound)

	// Nothing to cancel twice; the host deletes instead; unknown plans 404.
	srv.Do(t, "DELETE", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusNoContent)
	if res := srv.Do(t, "DELETE", "/forum/posts/"+it.ID+"/join-requests", nil, host); res.Status != http.StatusBadRequest || res.Message() != social.MsgLeaveOwnPlan {
		t.Fatalf("host cancel: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "DELETE", "/forum/posts/nope/join-requests", nil, joiner).Expect(t, http.StatusNotFound)

	// Rejoining flips the record back and reuses the group thread.
	var back contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, joiner).Expect(t, http.StatusOK).JSON(t, &back)
	if *back.ThreadID != *res.ThreadID {
		t.Fatalf("rejoin made a new thread: %s vs %s", *back.ThreadID, *res.ThreadID)
	}
	if rec, _ := srv.Store.Joins().RecordOf(context.Background(), it.ID, joiner.UserID); rec.Status != models.JoinAccepted {
		t.Fatalf("record after rejoin: %+v", rec)
	}
}

func TestJoinNeverOverfillsAPlan(t *testing.T) {
	srv := testutil.New(t)
	host := srv.Signup(t, "Cap Host")
	it := insertPlan(t, srv, plan(host.UserID, srv.Clock.Now().Add(4*time.Hour), techSquare, func(it *models.Itinerary) {
		it.MaxGroupSize = testutil.Ptr(3)
	}))
	joiners := make([]*testutil.Session, 6)
	for i := range joiners {
		joiners[i] = srv.Signup(t, "Racer "+string(rune('A'+i)))
	}
	var wg sync.WaitGroup
	responses := make([]*testutil.Response, len(joiners))
	for i, s := range joiners {
		wg.Add(1)
		go func(i int, s *testutil.Session) {
			defer wg.Done()
			responses[i] = srv.Do(t, "POST", "/forum/posts/"+it.ID+"/join-requests", nil, s)
		}(i, s)
	}
	wg.Wait()
	results := make([]contract.JoinStatus, len(joiners))
	for i, res := range responses {
		var r contract.JoinResult
		res.Expect(t, http.StatusOK).JSON(t, &r)
		results[i] = r.Status
	}
	joined, full := 0, 0
	for _, st := range results {
		switch st {
		case contract.JoinJoined:
			joined++
		case contract.JoinFull:
			full++
		}
	}
	stored := loadPlan(t, srv, it.ID)
	if joined != 2 || full != 4 || len(stored.MemberIDs) != 3 {
		t.Fatalf("joined %d, full %d, members %v", joined, full, stored.MemberIDs)
	}
	groups, err := srv.Store.Threads().ByItinerary(context.Background(), []string{it.ID})
	if err != nil || len(groups[it.ID].MemberIDs) != 3 {
		t.Fatalf("one group thread with the three members: %v %+v", err, groups)
	}
}

func TestPlanTogether(t *testing.T) {
	srv := testutil.New(t)
	poster := srv.Signup(t, "Priya Poster")
	asker := srv.Signup(t, "Ari Asker")
	friendOnlyViewer := srv.Signup(t, "Fay Outsider")
	var post contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostEveryone, techSquare, "Tech Square", nil), poster).Expect(t, http.StatusCreated).JSON(t, &post)
	srv.Events.Reset()

	var dm contract.ChatThread
	srv.Do(t, "POST", "/forum/posts/"+post.ID+"/plan-together", nil, asker).Expect(t, http.StatusOK).JSON(t, &dm)
	if dm.IsGroup || dm.Title != "Priya Poster" || dm.LastMessage != "You: "+social.MsgPlanTogether || dm.Unread != 0 ||
		len(dm.Faces) != 1 || dm.Faces[0].ID != poster.UserID || dm.Subtitle != "From the Forum" {
		t.Fatalf("plan-together thread: %+v", dm)
	}
	msgs := eventsFor[realtime.MessageNewData](t, srv, poster.UserID, realtime.EventMessageNew)
	if len(msgs) != 1 || msgs[0].ThreadID != dm.ID || msgs[0].Message.SenderName != "Ari" || msgs[0].Message.Text != social.MsgPlanTogether {
		t.Fatalf("message.new to the author: %+v", msgs)
	}
	threads := eventsFor[contract.ChatThread](t, srv, poster.UserID, realtime.EventThreadUpdated)
	if len(threads) != 1 || threads[0].Unread != 1 || threads[0].Title != "Ari Asker" || threads[0].LastMessage != "Ari: "+social.MsgPlanTogether {
		t.Fatalf("thread.updated to the author: %+v", threads)
	}
	if own := eventsFor[realtime.MessageNewData](t, srv, asker.UserID, realtime.EventMessageNew); len(own) != 1 || own[0].Message.SenderName != "You" {
		t.Fatalf("the sender's other devices: %+v", own)
	}

	// Once per post: the same DM comes back without a second message.
	var again contract.ChatThread
	srv.Do(t, "POST", "/forum/posts/"+post.ID+"/plan-together", nil, asker).Expect(t, http.StatusOK).JSON(t, &again)
	var messages []contract.Message
	srv.Do(t, "GET", "/threads/"+dm.ID+"/messages", nil, asker).Expect(t, http.StatusOK).JSON(t, &messages)
	if again.ID != dm.ID || len(messages) != 1 || messages[0].ClientID != nil {
		t.Fatalf("second plan-together: %s, %+v", again.ID, messages)
	}
	posts := feed(t, srv, asker, midtown, "type=free_now")
	if p := find(posts, post.ID); p == nil || !p.PlanTogetherSent {
		t.Fatalf("plan_together_sent: %+v", posts)
	}
	if p := find(feed(t, srv, friendOnlyViewer, midtown, "type=free_now"), post.ID); p == nil || p.PlanTogetherSent {
		t.Fatalf("plan_together_sent is per viewer: %+v", p)
	}

	// Own post, missing post, friends-only post of a stranger; plans work too.
	if res := srv.Do(t, "POST", "/forum/posts/"+post.ID+"/plan-together", nil, poster); res.Status != http.StatusBadRequest || res.Message() != social.MsgOwnPost {
		t.Fatalf("own post: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "POST", "/forum/posts/nope/plan-together", nil, asker).Expect(t, http.StatusNotFound)
	var hidden contract.MyFreePost
	srv.Do(t, "POST", "/forum/posts", freeNow(contract.PostFriends, techSquare, "Tech Square", nil), friendOnlyViewer).Expect(t, http.StatusCreated).JSON(t, &hidden)
	srv.Do(t, "POST", "/forum/posts/"+hidden.ID+"/plan-together", nil, asker).Expect(t, http.StatusNotFound)
	it := insertPlan(t, srv, plan(poster.UserID, srv.Clock.Now().Add(5*time.Hour), techSquare))
	var viaPlan contract.ChatThread
	srv.Do(t, "POST", "/forum/posts/"+it.ID+"/plan-together", nil, asker).Expect(t, http.StatusOK).JSON(t, &viaPlan)
	srv.Do(t, "GET", "/threads/"+dm.ID+"/messages", nil, asker).Expect(t, http.StatusOK).JSON(t, &messages)
	if viaPlan.ID != dm.ID || len(messages) != 2 {
		t.Fatalf("plan-together on a plan: same DM, a message for that post: %s, %d", viaPlan.ID, len(messages))
	}
	n, err := srv.Store.Collection(store.CollPlanTogether).CountDocuments(context.Background(), bson.M{"userId": asker.UserID})
	if err != nil || n != 2 {
		t.Fatalf("plan_together marks: %d %v", n, err)
	}
}
