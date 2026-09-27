//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// techSquare is where the Atlanta accounts plan from (they have no home base).
var techSquare = contract.Place{Name: "Tech Square", Coordinate: &contract.Coordinate{Lat: 33.7766, Lng: -84.389}}

func firstName(s *Session) string { return strings.Fields(s.Name)[0] }

// unfriend makes sure a and b are not friends and have nothing pending.
func unfriend(t testing.TB, a, b *Session) {
	t.Helper()
	do(t, a, "DELETE", "/friends/"+b.UserID, nil).NoContent(t)
	for _, s := range []*Session{a, b} {
		for _, r := range get[[]contract.FriendRequest](t, s, "/friends/requests") {
			other := a
			if s == a {
				other = b
			}
			if r.Person.ID != other.UserID {
				continue
			}
			if r.Outgoing {
				do(t, s, "DELETE", "/friends/requests/"+r.ID, nil).NoContent(t)
			} else {
				do(t, s, "POST", "/friends/requests/"+r.ID+"/decline", nil).NoContent(t)
			}
		}
	}
}

// relation is how b appears to a in people search.
func relation(t testing.TB, a, b *Session) contract.UserSearchResult {
	t.Helper()
	handle := *b.User.Username
	for _, r := range get[[]contract.UserSearchResult](t, a, "/users/search?q=%40"+handle) {
		if r.Person.ID == b.UserID {
			return r
		}
	}
	t.Fatalf("%s cannot find @%s", a.Label, handle)
	return contract.UserSearchResult{}
}

func isFriend(t testing.TB, a, b *Session) bool {
	t.Helper()
	return slices.ContainsFunc(get[[]contract.Friend](t, a, "/friends"), func(f contract.Friend) bool { return f.Person.ID == b.UserID })
}

// TestFriendsAndInvites walks requests (send, replay, decline, cancel,
// cross-accept, accept), the realtime friend.request, removing, and invites.
func TestFriendsAndInvites(t *testing.T) {
	alice, bob := people(t)
	unfriend(t, alice, bob)
	aws, bws := dial(t, alice), dial(t, bob)

	if r := relation(t, alice, bob); r.Relation != contract.RelationNone || r.RequestID != nil {
		t.Errorf("strangers: %+v", r)
	}
	byName := get[[]contract.UserSearchResult](t, alice, "/users/search?q="+strings.ToLower(firstName(bob)))
	if !slices.ContainsFunc(byName, func(r contract.UserSearchResult) bool { return r.Person.ID == bob.UserID }) {
		t.Errorf("searching %q does not find Bob", firstName(bob))
	}
	if self := get[[]contract.UserSearchResult](t, alice, "/users/search?q=%40"+*alice.User.Username); len(self) != 0 {
		t.Errorf("people search lists yourself: %+v", self)
	}
	if none := get[[]contract.UserSearchResult](t, alice, "/users/search?q="); len(none) != 0 {
		t.Errorf("an empty search found %d people", len(none))
	}

	sent := send[contract.FriendRequest](t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusCreated)
	if !sent.Outgoing || sent.Person.ID != bob.UserID || !strings.HasPrefix(sent.Note, "Requested ") {
		t.Errorf("sent request: %+v", sent)
	}
	var incoming contract.FriendRequest
	expectEvent(t, bws, "friend.request", 10*time.Second, &incoming, func(v contract.FriendRequest) bool { return v.ID == sent.ID })
	if incoming.Outgoing || incoming.Person.ID != alice.UserID || incoming.Note != "Wants to be friends" {
		t.Errorf("Bob's friend.request: %+v", incoming)
	}
	var echo contract.FriendRequest
	expectEvent(t, aws, "friend.request", 10*time.Second, &echo, func(v contract.FriendRequest) bool { return v.ID == sent.ID })
	if !echo.Outgoing {
		t.Errorf("the sender's own devices get the outgoing request: %+v", echo)
	}
	replay := send[contract.FriendRequest](t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusOK)
	if replay.ID != sent.ID {
		t.Errorf("a second request made a new one: %s", replay.ID)
	}
	if r := relation(t, alice, bob); r.Relation != contract.RelationOutgoing || r.RequestID == nil || *r.RequestID != sent.ID {
		t.Errorf("Alice sees Bob as %+v", r)
	}
	if r := relation(t, bob, alice); r.Relation != contract.RelationIncoming || r.RequestID == nil || *r.RequestID != sent.ID {
		t.Errorf("Bob sees Alice as %+v", r)
	}
	if reqs := get[[]contract.FriendRequest](t, bob, "/friends/requests"); !slices.ContainsFunc(reqs, func(r contract.FriendRequest) bool { return r.ID == sent.ID && !r.Outgoing }) {
		t.Errorf("Bob's requests: %+v", reqs)
	}
	fails(t, alice, "POST", "/friends/requests/"+sent.ID+"/accept", nil, http.StatusNotFound) // only the recipient answers
	fails(t, bob, "DELETE", "/friends/requests/"+sent.ID, nil, http.StatusNotFound)           // only the sender withdraws

	// Declined, then sent again and withdrawn.
	do(t, bob, "POST", "/friends/requests/"+sent.ID+"/decline", nil).NoContent(t)
	if r := relation(t, alice, bob); r.Relation != contract.RelationNone {
		t.Errorf("after declining: %s", r.Relation)
	}
	again := send[contract.FriendRequest](t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusCreated)
	do(t, alice, "DELETE", "/friends/requests/"+again.ID, nil).NoContent(t)
	if reqs := get[[]contract.FriendRequest](t, bob, "/friends/requests"); slices.ContainsFunc(reqs, func(r contract.FriendRequest) bool { return r.ID == again.ID }) {
		t.Error("a withdrawn request is still listed")
	}

	// Bob asks, Alice asks back: that is a yes.
	bobs := send[contract.FriendRequest](t, bob, "POST", "/friends/requests", contract.StartDMRequest{UserID: alice.UserID}, http.StatusCreated)
	yes := send[contract.FriendRequest](t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusOK)
	if yes.ID != bobs.ID || yes.Note != "You're friends now" {
		t.Errorf("asking back: %+v", yes)
	}
	if !isFriend(t, alice, bob) || !isFriend(t, bob, alice) {
		t.Fatal("not friends after asking back")
	}
	friends := get[[]contract.Friend](t, alice, "/friends")
	for _, f := range friends {
		if f.Person.ID == bob.UserID && (f.StatusLine == "" || !f.Activity.Valid()) {
			t.Errorf("friend row: %+v", f)
		}
	}
	var status struct {
		UserID     string `json:"user_id"`
		StatusLine string `json:"status_line"`
	}
	expectEvent(t, aws, "friend.status", 10*time.Second, &status, func(v struct {
		UserID     string `json:"user_id"`
		StatusLine string `json:"status_line"`
	}) bool {
		return v.UserID == bob.UserID
	})
	if msg := fails(t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusConflict); msg != "You're already friends." {
		t.Errorf("request to a friend: %q", msg)
	}
	fails(t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: alice.UserID}, http.StatusBadRequest)
	fails(t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: "5eed0000000000000000ffff"}, http.StatusNotFound)
	if threads := get[[]contract.ChatThread](t, alice, "/threads"); !slices.ContainsFunc(threads, func(th contract.ChatThread) bool {
		return !th.IsGroup && slices.ContainsFunc(th.Members, func(p contract.PersonRef) bool { return p.ID == bob.UserID })
	}) {
		t.Error("becoming friends did not open a DM")
	}

	// Remove, then the plain accept path.
	do(t, alice, "DELETE", "/friends/"+bob.UserID, nil).NoContent(t)
	if isFriend(t, alice, bob) || isFriend(t, bob, alice) {
		t.Fatal("still friends after removing")
	}
	ask := send[contract.FriendRequest](t, alice, "POST", "/friends/requests", contract.StartDMRequest{UserID: bob.UserID}, http.StatusCreated)
	do(t, bob, "POST", "/friends/requests/"+ask.ID+"/accept", nil).NoContent(t)
	if !isFriend(t, alice, bob) {
		t.Fatal("not friends after accepting")
	}
	do(t, bob, "POST", "/friends/requests/"+ask.ID+"/accept", nil).NoContent(t) // answering twice is harmless
	if n := len(get[[]contract.Friend](t, bob, "/friends")); n != 1 {
		t.Errorf("Bob has %d friends after accepting twice", n)
	}

	// Invites: one live link per person, reusable until it is used.
	do(t, alice, "DELETE", "/friends/"+bob.UserID, nil).NoContent(t)
	link := send[contract.URLResponse](t, alice, "POST", "/invites", nil, http.StatusCreated)
	if !strings.HasPrefix(link.URL, "https://sidequests.app/invite/") {
		t.Fatalf("invite url %q", link.URL)
	}
	if again := send[contract.URLResponse](t, alice, "POST", "/invites", nil, http.StatusOK); again.URL != link.URL {
		t.Errorf("a second invite made a new link: %s", again.URL)
	}
	code := strings.TrimPrefix(link.URL, "https://sidequests.app/invite/")
	if msg := fails(t, alice, "POST", "/invites/"+code+"/accept", nil, http.StatusBadRequest); msg != "That's your own invite link. Send it to a friend." {
		t.Errorf("own invite: %q", msg)
	}
	inviter := send[contract.Friend](t, bob, "POST", "/invites/"+code+"/accept", nil, http.StatusOK)
	if inviter.Person.ID != alice.UserID || inviter.StatusLine == "" {
		t.Errorf("accepted invite: %+v", inviter)
	}
	if !isFriend(t, bob, alice) || !isFriend(t, alice, bob) {
		t.Error("not friends after accepting the invite")
	}
	fails(t, bob, "POST", "/invites/no-such-code/accept", nil, http.StatusBadRequest)

	// A new status reaches friends as friend.status.
	busy, open := contract.StatusBusy, contract.StatusOpen
	send[contract.User](t, bob, "PATCH", "/me", contract.UserPatch{Status: &busy}, http.StatusOK)
	expectEvent(t, aws, "friend.status", 10*time.Second, &status, func(v struct {
		UserID     string `json:"user_id"`
		StatusLine string `json:"status_line"`
	}) bool {
		return v.UserID == bob.UserID && strings.Contains(strings.ToLower(v.StatusLine), "busy")
	})
	for _, f := range get[[]contract.Friend](t, alice, "/friends") {
		if f.Person.ID == bob.UserID && f.Activity != contract.ActivityBusy {
			t.Errorf("Bob is busy but his row says %s (%q)", f.Activity, f.StatusLine)
		}
	}
	send[contract.User](t, bob, "PATCH", "/me", contract.UserPatch{Status: &open}, http.StatusOK)
}

// TestDirectMessages sends messages between the two accounts and checks
// who hears what: the recipient, the sender's own devices, and nobody else.
func TestDirectMessages(t *testing.T) {
	alice, bob := people(t)
	var bystander *WS
	if config(t).DemoPassword != "" {
		bystander = dial(t, sandy(t))
	}
	aws, bws := dial(t, alice), dial(t, bob)
	dm := send[contract.ChatThread](t, alice, "POST", "/threads/dm", contract.StartDMRequest{UserID: bob.UserID}, http.StatusOK)
	if dm.IsGroup || dm.Title != bob.Name || len(dm.Members) != 2 {
		t.Errorf("DM for Alice: %+v", dm)
	}
	if theirs := send[contract.ChatThread](t, bob, "POST", "/threads/dm", contract.StartDMRequest{UserID: alice.UserID}, http.StatusOK); theirs.ID != dm.ID || theirs.Title != alice.Name {
		t.Errorf("Bob's side of the DM: %s %q", theirs.ID, theirs.Title)
	}
	do(t, bob, "POST", "/threads/"+dm.ID+"/read", nil).NoContent(t)
	unreadBefore := get[contract.ChatThread](t, bob, "/threads/"+dm.ID).Unread

	clientID := "e2e-dm-" + randomHex(4)
	msg := send[contract.Message](t, alice, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: "Coffee at 3?", ClientID: &clientID}, http.StatusCreated)
	var got messageNew
	expectEvent(t, bws, "message.new", 10*time.Second, &got, func(v messageNew) bool { return v.Message.ID == msg.ID })
	if got.ThreadID != dm.ID || got.Message.SenderName != firstName(alice) || got.Message.SenderID != alice.UserID || got.Message.Text != "Coffee at 3?" {
		t.Errorf("Bob's message.new: %+v", got)
	}
	var own messageNew
	expectEvent(t, aws, "message.new", 10*time.Second, &own, func(v messageNew) bool { return v.Message.ID == msg.ID })
	if own.Message.SenderName != "You" || own.Message.ClientID == nil || *own.Message.ClientID != clientID {
		t.Errorf("Alice's message.new: %+v", own)
	}
	var th contract.ChatThread
	expectEvent(t, bws, "thread.updated", 10*time.Second, &th, func(v contract.ChatThread) bool {
		return v.ID == dm.ID && strings.HasSuffix(v.LastMessage, "Coffee at 3?")
	})
	if th.Unread != unreadBefore+1 || th.LastMessage != firstName(alice)+": Coffee at 3?" || th.Title != alice.Name {
		t.Errorf("Bob's thread.updated: unread %d, last %q, title %q", th.Unread, th.LastMessage, th.Title)
	}
	if bystander != nil {
		expectNoEvent(t, bystander, "message.new", time.Second, has(msg.ID))
		expectNoEvent(t, bystander, "thread.updated", 0, has(dm.ID))
	}
	if list := get[[]contract.ChatThread](t, bob, "/threads"); len(list) == 0 || list[0].ID != dm.ID || list[0].Unread != unreadBefore+1 {
		t.Errorf("Bob's newest thread should be the DM with the unread message: %+v", list)
	}
	do(t, bob, "POST", "/threads/"+dm.ID+"/read", nil).NoContent(t)
	if after := get[contract.ChatThread](t, bob, "/threads/"+dm.ID); after.Unread != 0 {
		t.Errorf("unread %d after reading", after.Unread)
	}
	expectNoEvent(t, aws, "thread.read", time.Second, has(dm.ID))

	// Paging: more than a page of messages, walked back with before=.
	var sentIDs []string
	for i := 0; i < 32; i++ {
		m := send[contract.Message](t, bob, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: fmt.Sprintf("page test %02d", i)}, http.StatusCreated)
		sentIDs = append(sentIDs, m.ID)
	}
	newest := get[[]contract.Message](t, alice, "/threads/"+dm.ID+"/messages")
	if len(newest) != 30 || newest[29].ID != sentIDs[31] {
		t.Fatalf("newest page: %d messages, last %v", len(newest), newest[len(newest)-1].ID)
	}
	for i := 1; i < len(newest); i++ {
		if newest[i].SentAt.Before(newest[i-1].SentAt.Time) {
			t.Errorf("messages are not oldest first at %d", i)
		}
	}
	older := get[[]contract.Message](t, alice, "/threads/"+dm.ID+"/messages?before="+newest[0].ID)
	if len(older) == 0 || older[len(older)-1].SentAt.After(newest[0].SentAt.Time) || slices.ContainsFunc(older, func(m contract.Message) bool { return m.ID == newest[0].ID }) {
		t.Errorf("the page before the oldest: %d messages", len(older))
	}
	all := append(append([]contract.Message{}, older...), newest...)
	for _, id := range sentIDs {
		if !slices.ContainsFunc(all, func(m contract.Message) bool { return m.ID == id }) {
			t.Errorf("message %s is on neither page", id)
		}
	}
	if none := get[[]contract.Message](t, alice, "/threads/"+dm.ID+"/messages?before=no-such-message"); len(none) != 0 {
		t.Errorf("before an unknown message: %d messages", len(none))
	}
}

// TestSharedPlan: Alice plans an afternoon in Atlanta and opens it; Bob
// finds it on the Forum and joins; they share notes, a chat, a tab and an
// album; Alice books tickets (Bob sees them); Bob leaves; Alice deletes it.
func TestSharedPlan(t *testing.T) {
	alice, bob := people(t)
	c := config(t)
	tomorrow := localDay(time.Now(), c.Loc).AddDate(0, 0, 1)
	req := planRequest(techSquare, tomorrow, 12, 17, contract.RangeTransit, contract.TravelModes{contract.ModeWalk, contract.ModeMarta},
		[]string{"Food", "Art"}, 2, contract.VisibilityOpen)
	batch := generate(t, alice, req)
	if len(batch.Options) == 0 {
		t.Fatalf("no Atlanta plans for tomorrow 12–5 PM: %v", batch.Reason)
	}
	opt := batch.Options[0]
	for _, o := range batch.Options {
		if o.TotalCostCents != nil && *o.TotalCostCents > 0 {
			opt = o
			break
		}
	}
	order := stopIDs(opt.Stops)
	route := routeFor(t, alice, req, opt.ID, order)
	two := 2
	lock := contract.NewTime(req.StartTime.Add(-30 * time.Minute))
	plan := send[contract.Itinerary](t, alice, "POST", "/itineraries", contract.CreateItineraryRequest{
		Plan: req, Option: opt, StopOrder: order, Route: route, Visibility: contract.VisibilityOpen, LockAt: &lock, MaxGroupSize: &two,
	}, http.StatusCreated)
	t.Cleanup(func() { do(t, alice, "DELETE", "/itineraries/"+plan.ID, nil) })
	if plan.MaxGroupSize == nil || *plan.MaxGroupSize != 2 || plan.LockAt == nil || !plan.LockAt.Equal(lock.Time) || plan.Visibility != contract.VisibilityOpen {
		t.Errorf("open plan: max %v, lock %v, %s", plan.MaxGroupSize, plan.LockAt, plan.Visibility)
	}
	if msg := fails(t, alice, "POST", "/forum/posts/"+plan.ID+"/join-requests", nil, http.StatusBadRequest); msg != "You host this sidequest." {
		t.Errorf("joining your own plan: %q", msg)
	}
	fails(t, alice, "DELETE", "/forum/posts/"+plan.ID, nil, http.StatusBadRequest)

	near := fmt.Sprintf("?lat=%f&lng=%f&radius=10", techSquare.Coordinate.Lat, techSquare.Coordinate.Lng)
	var post contract.ForumPost
	found := false
	for _, p := range forum(t, bob, near) {
		if p.ID == plan.ID {
			post, found = p, true
		}
	}
	if !found {
		t.Fatalf("Bob does not see Alice's open plan near Tech Square")
	}
	if post.Author.ID != alice.UserID || post.IsFriend != isFriend(t, bob, alice) || post.FriendsOnly || post.SpotsLeft == nil || *post.SpotsLeft != 1 || post.Capacity == nil ||
		*post.Capacity != 2 || post.JoinStatus != contract.JoinNone || post.LockLabel == nil {
		t.Errorf("Alice's plan in Bob's feed: %+v", post)
	}
	if far := forum(t, bob, "?lat=40.7&lng=-74.0&radius=10"); slices.ContainsFunc(far, func(p contract.ForumPost) bool { return p.ID == plan.ID }) &&
		!isFriend(t, bob, alice) {
		t.Error("a stranger's open plan shows far outside the radius")
	}

	aws, bws := dial(t, alice), dial(t, bob)
	joined := send[contract.JoinResult](t, bob, "POST", "/forum/posts/"+plan.ID+"/join-requests", nil, http.StatusOK)
	if joined.Status != contract.JoinJoined || joined.ThreadID == nil {
		t.Fatalf("Bob's join: %+v", joined)
	}
	threadID := *joined.ThreadID
	var jr struct {
		PostID string             `json:"post_id"`
		From   contract.PersonRef `json:"from"`
	}
	expectEvent(t, aws, "join.request", 10*time.Second, &jr, func(v struct {
		PostID string             `json:"post_id"`
		From   contract.PersonRef `json:"from"`
	}) bool {
		return v.PostID == plan.ID
	})
	if jr.From.ID != bob.UserID || jr.From.Name != bob.Name {
		t.Errorf("join.request from %+v", jr.From)
	}
	var hostView, joinerView contract.Itinerary
	expectEvent(t, aws, "itinerary.updated", 10*time.Second, &hostView, func(v contract.Itinerary) bool { return v.ID == plan.ID && v.GoingCount == 2 })
	expectEvent(t, bws, "itinerary.updated", 10*time.Second, &joinerView, func(v contract.Itinerary) bool { return v.ID == plan.ID })
	if !hostView.IsHost || joinerView.IsHost {
		t.Errorf("itinerary.updated is rendered per viewer: host sees is_host %v, joiner %v", hostView.IsHost, joinerView.IsHost)
	}
	var thEv contract.ChatThread
	expectEvent(t, aws, "thread.updated", 10*time.Second, &thEv, func(v contract.ChatThread) bool { return v.ID == threadID })

	// Full now: a third person gets "full" and changes nothing. (Not asked
	// of the demo account when it lives on another date: from its clock the
	// plan may already have started, which answers "closed".)
	if c.DemoPassword != "" && get[contract.User](t, sandy(t), "/me").DemoDate == nil {
		full := send[contract.JoinResult](t, sandy(t), "POST", "/forum/posts/"+plan.ID+"/join-requests", nil, http.StatusOK)
		if full.Status != contract.JoinFull || full.ThreadID != nil {
			t.Errorf("joining a full plan: %+v", full)
		}
	}
	if p := planPost(t, bob, plan.ID, "&"+near[1:]); p.JoinStatus != contract.JoinJoined || *p.SpotsLeft != 0 || p.ThreadID == nil || *p.ThreadID != threadID {
		t.Errorf("Bob's post after joining: %+v", p)
	}

	// The host renames: the chat follows, members hear it.
	title := "Alice and Bob's afternoon " + randomHex(2)
	send[contract.Itinerary](t, alice, "PATCH", "/itineraries/"+plan.ID, contract.ItineraryUpdate{Title: &title}, http.StatusOK)
	expectEvent(t, bws, "itinerary.updated", 10*time.Second, &joinerView, func(v contract.Itinerary) bool { return v.ID == plan.ID && v.Title == title })
	if th := get[contract.ChatThread](t, bob, "/threads/"+threadID); th.Title != title || !th.IsGroup || len(th.Members) != 2 {
		t.Errorf("the group chat after the rename: %+v", th)
	}
	fails(t, bob, "PATCH", "/itineraries/"+plan.ID, contract.ItineraryUpdate{Title: &title}, http.StatusForbidden)
	fails(t, bob, "DELETE", "/itineraries/"+plan.ID, nil, http.StatusForbidden)
	fails(t, alice, "POST", "/itineraries/"+plan.ID+"/leave", nil, http.StatusBadRequest)

	// Notes: shared ones reach Bob, private ones do not.
	stops := stopItems(get[contract.Itinerary](t, alice, "/itineraries/"+plan.ID))
	stop := stops[0]
	shared, private := contract.NotesShared, contract.NotesPrivate
	do(t, alice, "PATCH", "/itineraries/"+plan.ID+"/items/"+stop.ID, contract.ItemNotesPatch{Notes: "Meet at the fountain.", NotesScope: &shared}).NoContent(t)
	expectEvent(t, bws, "itinerary.updated", 10*time.Second, &joinerView, func(v contract.Itinerary) bool { return v.ID == plan.ID })
	seen := get[contract.ItineraryItem](t, bob, "/events/"+stop.ID)
	if seen.Notes == nil || *seen.Notes != "Meet at the fountain." || seen.NotesScope == nil || *seen.NotesScope != contract.NotesShared {
		t.Errorf("Bob sees the shared note as %v %v", seen.Notes, seen.NotesScope)
	}
	if len(stops) > 1 {
		do(t, alice, "PATCH", "/itineraries/"+plan.ID+"/items/"+stops[1].ID, contract.ItemNotesPatch{Notes: "Only mine.", NotesScope: &private}).NoContent(t)
		if other := get[contract.ItineraryItem](t, bob, "/events/"+stops[1].ID); other.Notes != nil {
			t.Errorf("Bob sees Alice's private note: %q", *other.Notes)
		}
		if mine := get[contract.ItineraryItem](t, alice, "/events/"+stops[1].ID); mine.Notes == nil || *mine.Notes != "Only mine." {
			t.Errorf("Alice's private note: %v", mine.Notes)
		}
	}
	// Until a member picks, a stop's transit choice is the mode of the leg that reaches it.
	before := get[contract.ItineraryItem](t, alice, "/events/"+stop.ID)
	if before.TransitMode == nil {
		t.Fatalf("a stop should default to its inbound leg's transit mode")
	}
	do(t, bob, "PUT", "/itineraries/"+plan.ID+"/items/"+stop.ID+"/transit", contract.TransitSelection{Mode: contract.ModeRideshare}).NoContent(t)
	if mine := get[contract.ItineraryItem](t, alice, "/events/"+stop.ID); mine.TransitMode == nil || *mine.TransitMode != *before.TransitMode {
		t.Errorf("Bob's transit choice leaked to Alice: %v (her default was %v)", mine.TransitMode, *before.TransitMode)
	}
	if his := get[contract.ItineraryItem](t, bob, "/events/"+stop.ID); his.TransitMode == nil || *his.TransitMode != contract.ModeRideshare {
		t.Errorf("Bob's own transit choice: %v", his.TransitMode)
	}

	// The group tab: Bob pays, split two ways; Alice settles.
	group := "/groups/" + threadID
	lunch := send[contract.Expense](t, bob, "POST", group+"/expenses",
		contract.NewExpense{What: "Lunch", AmountCents: 2501, PayerID: bob.UserID, SplitAmong: []string{alice.UserID, bob.UserID}}, http.StatusCreated)
	if !slices.Equal(lunch.Shares, []int{1251, 1250}) {
		t.Errorf("2501 split two ways: %v", lunch.Shares)
	}
	var added struct {
		GroupID string           `json:"group_id"`
		Expense contract.Expense `json:"expense"`
	}
	expectEvent(t, aws, "expense.added", 10*time.Second, &added, func(v struct {
		GroupID string           `json:"group_id"`
		Expense contract.Expense `json:"expense"`
	}) bool {
		return v.Expense.ID == lunch.ID
	})
	if b, _ := balanceOf(get[[]contract.Balance](t, alice, group+"/balances"), bob.UserID); b != -1251 {
		t.Errorf("Alice's balance with Bob: %d, want -1251", b)
	}
	if a, _ := balanceOf(get[[]contract.Balance](t, bob, group+"/balances"), alice.UserID); a != 1251 {
		t.Errorf("Bob's balance with Alice: %d, want 1251", a)
	}
	if th := get[contract.ChatThread](t, alice, "/threads/"+threadID); !slices.Contains(th.Chips, "You owe $12.51") {
		t.Errorf("Alice's chips: %v", th.Chips)
	}
	fails(t, alice, "DELETE", group+"/expenses/"+lunch.ID, nil, http.StatusForbidden)
	fails(t, alice, "POST", group+"/settle", contract.SettleRequest{AmountCents: 1251}, http.StatusBadRequest) // no card yet
	card := send[contract.PaymentMethod](t, alice, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: "tok_visa_4242"}, http.StatusCreated)
	fails(t, alice, "POST", group+"/settle", contract.SettleRequest{AmountCents: 1000}, http.StatusConflict)
	do(t, alice, "POST", group+"/settle", contract.SettleRequest{AmountCents: 1251, PaymentMethodID: &card.ID}).NoContent(t)
	if b, _ := balanceOf(get[[]contract.Balance](t, alice, group+"/balances"), bob.UserID); b != 0 {
		t.Errorf("after settling Alice's balance is %d", b)
	}
	if a, _ := balanceOf(get[[]contract.Balance](t, bob, group+"/balances"), alice.UserID); a != 0 {
		t.Errorf("after settling Bob's balance is %d", a)
	}

	// The album: Alice cannot delete Bob's photo.
	photo := send[contract.GroupPhoto](t, bob, "POST", group+"/photos", photoForm(t, "photo", jpegBytes(t, color.RGBA{R: 90, G: 40, B: 160, A: 255})), http.StatusCreated)
	var pa struct {
		GroupID string              `json:"group_id"`
		Photo   contract.GroupPhoto `json:"photo"`
	}
	expectEvent(t, aws, "photo.added", 10*time.Second, &pa, func(v struct {
		GroupID string              `json:"group_id"`
		Photo   contract.GroupPhoto `json:"photo"`
	}) bool {
		return v.Photo.ID == photo.ID
	})
	if pa.Photo.ByName != firstName(bob) {
		t.Errorf("photo.added for Alice says by %q", pa.Photo.ByName)
	}
	fails(t, alice, "DELETE", group+"/photos/"+photo.ID, nil, http.StatusForbidden)

	// Tickets: Alice books one stop through the agent; Bob sees the ticket
	// on the shared plan. Without a card the agent fails at once.
	intent := send[contract.CheckoutIntent](t, alice, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: stop.ID, Quantity: 1}, http.StatusCreated)
	var cs checkoutStatus
	expectEvent(t, aws, "checkout.status", 10*time.Second, &cs, func(v checkoutStatus) bool { return v.IntentID == intent.ID })
	pollIntent(t, alice, intent.ID, contract.CheckoutAwaitingApproval)
	send[contract.CheckoutIntent](t, alice, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, http.StatusOK)
	pollIntent(t, alice, intent.ID, contract.CheckoutBooked)
	ticket := get[contract.ItineraryItem](t, alice, "/events/"+stop.ID).Ticket
	if ticket == nil {
		t.Fatal("no ticket on Alice's stop")
	}
	if bobs := get[contract.ItineraryItem](t, bob, "/events/"+stop.ID).Ticket; bobs == nil || bobs.ID != ticket.ID {
		t.Errorf("Bob does not see the ticket Alice booked on the shared stop: %+v", bobs)
	}
	expectNoEvent(t, bws, "checkout.status", time.Second, has(intent.ID))
	do(t, alice, "DELETE", "/me/payment-methods/"+card.ID, nil).NoContent(t)
	failed := send[contract.CheckoutIntent](t, alice, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: stop.ID, Quantity: 1}, http.StatusCreated)
	if failed.State != contract.CheckoutFailed || failed.FailureReason == nil || *failed.FailureReason != "Add a card in Account first." {
		t.Errorf("checkout without a card: %+v", failed)
	}
	// Someone who is not on the plan cannot check out its stops.
	if c.DemoPassword != "" {
		fails(t, sandy(t), "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: stop.ID, Quantity: 1}, http.StatusNotFound)
	}

	// Plan together on a plan post opens a DM with its host.
	pt := send[contract.ChatThread](t, bob, "POST", "/forum/posts/"+plan.ID+"/plan-together", nil, http.StatusOK)
	if pt.IsGroup || !slices.ContainsFunc(pt.Members, func(p contract.PersonRef) bool { return p.ID == alice.UserID }) {
		t.Errorf("plan together on a plan: %+v", pt)
	}

	// Bob leaves: he hears itinerary.removed, Alice sees one going again.
	do(t, bob, "POST", "/itineraries/"+plan.ID+"/leave", nil).NoContent(t)
	var gone itineraryRemoved
	expectEvent(t, bws, "itinerary.removed", 10*time.Second, &gone, func(v itineraryRemoved) bool { return v.ItineraryID == plan.ID })
	expectEvent(t, aws, "itinerary.updated", 10*time.Second, &hostView, func(v contract.Itinerary) bool { return v.ID == plan.ID && v.GoingCount == 1 })
	fails(t, bob, "GET", "/threads/"+threadID, nil, http.StatusNotFound)
	if th := get[contract.ChatThread](t, alice, "/threads/"+threadID); len(th.Members) != 1 {
		t.Errorf("the chat still lists %d members", len(th.Members))
	}

	// A plan whose joining already locked answers closed.
	pastLock := contract.NewTime(time.Now().Add(-time.Minute))
	locked := send[contract.Itinerary](t, alice, "POST", "/itineraries", contract.CreateItineraryRequest{
		Plan: req, Option: opt, StopOrder: order, Route: route, Visibility: contract.VisibilityOpen, LockAt: &pastLock,
	}, http.StatusCreated)
	if res := send[contract.JoinResult](t, bob, "POST", "/forum/posts/"+locked.ID+"/join-requests", nil, http.StatusOK); res.Status != contract.JoinClosed {
		t.Errorf("joining a locked plan: %+v", res)
	}
	do(t, alice, "DELETE", "/itineraries/"+locked.ID, nil).NoContent(t)

	// Bob joins again and Alice deletes: everything goes for both.
	send[contract.JoinResult](t, bob, "POST", "/forum/posts/"+plan.ID+"/join-requests", nil, http.StatusOK)
	do(t, alice, "DELETE", "/itineraries/"+plan.ID, nil).NoContent(t)
	expectEvent(t, bws, "itinerary.removed", 10*time.Second, &gone, func(v itineraryRemoved) bool { return v.ItineraryID == plan.ID })
	fails(t, bob, "GET", "/itineraries/"+plan.ID, nil, http.StatusNotFound)
	fails(t, alice, "GET", "/threads/"+threadID, nil, http.StatusNotFound)
	fails(t, alice, "GET", group+"/expenses", nil, http.StatusNotFound)
	do(t, nil, "GET", *photo.URL, nil).Expect(t, http.StatusNotFound)
	if slices.ContainsFunc(forum(t, bob, near), func(p contract.ForumPost) bool { return p.ID == plan.ID }) {
		t.Error("a deleted plan stays on the Forum")
	}
}

// TestRealtimeAuth checks the websocket handshake rules and the socket cap.
func TestRealtimeAuth(t *testing.T) {
	c := config(t)
	alice, _ := people(t)
	dial(t, alice) // the bearer header works (hello asserted in dial)

	// ?token= is the fallback for clients that cannot set headers.
	conn, _, err := websocket.DefaultDialer.Dial(c.WSURL+"?token="+url.QueryEscape(alice.Access), nil)
	if err != nil {
		t.Fatalf("dial with ?token=: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hello wsEvent
	if err := conn.ReadJSON(&hello); err != nil || hello.Type != "connected" {
		t.Errorf("hello over ?token=: %+v %v", hello, err)
	}
	conn.Close()

	// Five sockets per account: a sixth closes the oldest.
	var socks []*WS
	for i := 0; i < 6; i++ {
		socks = append(socks, dial(t, alice))
		time.Sleep(50 * time.Millisecond) // keep the order of arrival
	}
	select {
	case <-socks[0].done:
	case <-time.After(10 * time.Second):
		t.Error("the oldest of six sockets stayed open")
	}
	for i, w := range socks[1:] {
		select {
		case <-w.done:
			t.Errorf("socket %d closed too", i+1)
		default:
		}
	}
	res := doWith(t, nil, "GET", "/ws", nil, map[string]string{"Authorization": "Bearer garbage"})
	if res.Status != http.StatusUnauthorized {
		t.Errorf("a bad token on /ws: %s", res)
	}
	var e apiError
	if err := json.Unmarshal(res.Body, &e); err != nil || e.Message == "" {
		t.Errorf("/ws 401 body: %s", res.Body)
	}
}

// TestHealthAndRoot checks the unauthenticated surface: health, the API
// root and unknown routes answer JSON.
func TestHealthAndRoot(t *testing.T) {
	var health struct {
		OK bool `json:"ok"`
	}
	do(t, nil, "GET", "/healthz", nil).Expect(t, http.StatusOK).JSON(t, &health)
	if !health.OK {
		t.Error("healthz is not ok")
	}
	if msg := fails(t, nil, "GET", "/", nil, http.StatusNotFound); msg != "Not found." {
		t.Errorf("root: %q", msg)
	}
	fails(t, nil, "DELETE", "/healthz", nil, http.StatusMethodNotAllowed)
	fails(t, nil, "GET", "/itineraries", nil, http.StatusUnauthorized)
}
