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
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func send(t *testing.T, srv *testutil.Server, s *testutil.Session, threadID, text string, clientID *string) (contract.Message, int) {
	t.Helper()
	res := srv.Do(t, "POST", "/threads/"+threadID+"/messages", contract.NewMessage{Text: text, ClientID: clientID}, s)
	if res.Status != http.StatusCreated && res.Status != http.StatusOK {
		t.Fatalf("send %q: %d %s", text, res.Status, res.Body)
	}
	var m contract.Message
	res.JSON(t, &m)
	return m, res.Status
}

func threadsOf(t *testing.T, srv *testutil.Server, s *testutil.Session) []contract.ChatThread {
	t.Helper()
	var list []contract.ChatThread
	srv.Do(t, "GET", "/threads", nil, s).Expect(t, http.StatusOK).JSON(t, &list)
	return list
}

func TestDirectMessages(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Abe Able")
	b := srv.Signup(t, "Bea Bee")
	c := srv.Signup(t, "Cy Sea")

	var dm contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: b.UserID}, a).Expect(t, http.StatusOK).JSON(t, &dm)
	if dm.IsGroup || dm.Title != "Bea Bee" || len(dm.Members) != 2 || len(dm.Faces) != 1 || dm.Faces[0].ID != b.UserID ||
		dm.Subtitle != "From the Forum" || dm.LastMessage != "" || dm.LastTime != "" || dm.Unread != 0 || dm.Chips == nil || dm.AlbumTitle != nil {
		t.Fatalf("new DM: %+v", dm)
	}
	if got := eventsFor[contract.ChatThread](t, srv, b.UserID, realtime.EventThreadUpdated); len(got) != 1 || got[0].Title != "Abe Able" {
		t.Fatalf("the other person hears about a new DM: %+v", got)
	}
	var again contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: a.UserID}, b).Expect(t, http.StatusOK).JSON(t, &again)
	if again.ID != dm.ID || direct(srv, a.UserID, realtime.EventThreadUpdated) != 0 {
		t.Fatalf("the DM is one per pair: %s vs %s", again.ID, dm.ID)
	}
	if res := srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: a.UserID}, a); res.Status != http.StatusBadRequest || res.Message() != social.MsgDMSelf {
		t.Fatalf("DM with yourself: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: bson.NewObjectID().Hex()}, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: "garbage"}, a).Expect(t, http.StatusNotFound)

	// Sending: validation, client_id idempotency, per-recipient sender_name.
	for _, text := range []string{"", "   "} {
		if res := srv.Do(t, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: text}, a); res.Status != http.StatusBadRequest || res.Message() != social.MsgEmptyText {
			t.Fatalf("empty message: %d %s", res.Status, res.Body)
		}
	}
	srv.Events.Reset()
	hi, status := send(t, srv, a, dm.ID, "  hi  ", testutil.Ptr("c-1"))
	if status != http.StatusCreated || hi.Text != "hi" || hi.SenderName != "You" || hi.SenderID != a.UserID || hi.ClientID == nil || *hi.ClientID != "c-1" {
		t.Fatalf("sent: %d %+v", status, hi)
	}
	replay, status := send(t, srv, a, dm.ID, "hi", testutil.Ptr("c-1"))
	if status != http.StatusOK || replay.ID != hi.ID {
		t.Fatalf("replayed client id: %d %+v", status, replay)
	}
	if theirs, status := send(t, srv, b, dm.ID, "same id, other sender", testutil.Ptr("c-1")); status != http.StatusCreated || theirs.ID == hi.ID {
		t.Fatalf("client ids are per sender: %d %+v", status, theirs)
	}
	toB := eventsFor[realtime.MessageNewData](t, srv, b.UserID, realtime.EventMessageNew)
	toA := eventsFor[realtime.MessageNewData](t, srv, a.UserID, realtime.EventMessageNew)
	if len(toB) != 2 || toB[0].Message.SenderName != "Abe" || toB[0].ThreadID != dm.ID || toB[1].Message.SenderName != "You" ||
		len(toA) != 2 || toA[0].Message.SenderName != "You" || toA[1].Message.SenderName != "Bea" {
		t.Fatalf("message.new per recipient: A %+v, B %+v", toA, toB)
	}
	if direct(srv, a.UserID, realtime.EventThreadUpdated) != 2 || direct(srv, b.UserID, realtime.EventThreadUpdated) != 2 {
		t.Fatal("thread.updated to both members for each new message")
	}

	// Unread, last message and read.
	srv.Clock.Advance(time.Minute)
	send(t, srv, a, dm.ID, "you around?", nil)
	forB := threadsOf(t, srv, b)
	if len(forB) != 1 || forB[0].Unread != 1 || forB[0].LastMessage != "Abe: you around?" || forB[0].LastTime != httpx.Clock(srv.Clock.Now().In(ny)) {
		t.Fatalf("B's DM: %+v", forB)
	}
	if forA := threadsOf(t, srv, a); forA[0].Unread != 0 || forA[0].LastMessage != "You: you around?" {
		t.Fatalf("A's DM: %+v", forA)
	}
	srv.Events.Reset()
	srv.Do(t, "POST", "/threads/"+dm.ID+"/read", nil, b).Expect(t, http.StatusNoContent)
	if read := eventsFor[realtime.ThreadReadData](t, srv, b.UserID, realtime.EventThreadRead); len(read) != 1 || read[0].ThreadID != dm.ID {
		t.Fatalf("thread.read: %+v", read)
	}
	if direct(srv, a.UserID, realtime.EventThreadRead) != 0 || threadsOf(t, srv, b)[0].Unread != 0 {
		t.Fatal("read is the reader's own")
	}

	// Members only.
	for _, req := range []struct{ method, path string }{
		{"GET", "/threads/" + dm.ID}, {"GET", "/threads/" + dm.ID + "/messages"}, {"POST", "/threads/" + dm.ID + "/read"},
		{"GET", "/groups/" + dm.ID + "/expenses"}, {"GET", "/threads/nope"},
	} {
		srv.Do(t, req.method, req.path, nil, c).Expect(t, http.StatusNotFound)
	}
	srv.Do(t, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: "sneaky"}, c).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/groups/"+dm.ID+"/expenses", nil, a).Expect(t, http.StatusNotFound) // a DM is not a group

	// Order: most recent message first.
	var withC contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: c.UserID}, a).Expect(t, http.StatusOK).JSON(t, &withC)
	srv.Clock.Advance(time.Minute)
	send(t, srv, c, withC.ID, "new plan?", nil)
	if list := threadsOf(t, srv, a); len(list) != 2 || list[0].ID != withC.ID || list[1].ID != dm.ID || list[0].LastMessage != "Cy: new plan?" {
		t.Fatalf("order after C wrote: %+v", list)
	}
	srv.Clock.Advance(time.Minute)
	send(t, srv, b, dm.ID, "yes!", nil)
	if list := threadsOf(t, srv, a); list[0].ID != dm.ID {
		t.Fatalf("order after B wrote: %s first", list[0].ID)
	}
}

func TestMessagePages(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Pam Pager")
	b := srv.Signup(t, "Quin Pager")
	var dm contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: b.UserID}, a).Expect(t, http.StatusOK).JSON(t, &dm)
	sent := map[string]string{}
	for i := 1; i <= 35; i++ {
		srv.Clock.Advance(time.Second)
		m, _ := send(t, srv, a, dm.ID, fmt.Sprintf("m%d", i), nil)
		sent[m.Text] = m.ID
	}
	for i := 1; i <= 3; i++ { // same instant: the ids keep the order
		m, _ := send(t, srv, b, dm.ID, fmt.Sprintf("x%d", i), nil)
		sent[m.Text] = m.ID
	}
	texts := func(msgs []contract.Message) string {
		out := make([]string, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, m.Text)
		}
		return strings.Join(out, ",")
	}
	res := srv.Do(t, "GET", "/threads/"+dm.ID+"/messages", nil, a).Expect(t, http.StatusOK)
	if !strings.HasPrefix(string(res.Body), "[") {
		t.Fatalf("messages must be a bare array: %.40s", res.Body)
	}
	var page []contract.Message
	res.JSON(t, &page)
	want := make([]string, 0, 30)
	for i := 9; i <= 35; i++ {
		want = append(want, fmt.Sprintf("m%d", i))
	}
	want = append(want, "x1", "x2", "x3")
	if texts(page) != strings.Join(want, ",") {
		t.Fatalf("newest page: %s", texts(page))
	}
	if page[29].SenderName != "Quin" || page[0].SenderName != "You" {
		t.Fatalf("sender names for A: %+v / %+v", page[0], page[29])
	}
	srv.Do(t, "GET", "/threads/"+dm.ID+"/messages?before="+sent["m9"], nil, a).Expect(t, http.StatusOK).JSON(t, &page)
	if texts(page) != "m1,m2,m3,m4,m5,m6,m7,m8" {
		t.Fatalf("older page: %s", texts(page))
	}
	srv.Do(t, "GET", "/threads/"+dm.ID+"/messages?before="+sent["x2"], nil, b).Expect(t, http.StatusOK).JSON(t, &page)
	if len(page) != 30 || page[29].Text != "x1" || page[28].Text != "m35" {
		t.Fatalf("page before a same-instant message: %s", texts(page))
	}
	for _, before := range []string{sent["m1"], "unknown"} {
		srv.Do(t, "GET", "/threads/"+dm.ID+"/messages?before="+before, nil, a).Expect(t, http.StatusOK).JSON(t, &page)
		if len(page) != 0 {
			t.Fatalf("before %s: %s", before, texts(page))
		}
	}
}

func TestGroupThreadView(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 2026-09-26 12:00 New York
	a := srv.Signup(t, "Gus Group")
	b := srv.Signup(t, "Maya Mate")
	c := srv.Signup(t, "Dev Mate")
	th := group(t, srv, a, b, c) // the plan was Thursday at noon
	srv.Do(t, "POST", "/groups/"+th.ID+"/expenses", contract.NewExpense{What: "Coffee", AmountCents: 900, PayerID: b.UserID,
		SplitAmong: []string{a.UserID, b.UserID, c.UserID}}, b).Expect(t, http.StatusCreated)
	send(t, srv, b, th.ID, "photo dump in the album after pls", nil)

	var got contract.ChatThread
	srv.Do(t, "GET", "/threads/"+th.ID, nil, a).Expect(t, http.StatusOK).JSON(t, &got)
	if !got.IsGroup || got.Title != "Krog St dinner crew" || got.Subtitle != "3 people · Sep 24 12 PM" ||
		len(got.Faces) != 2 || got.Faces[0].ID != b.UserID || got.Faces[1].ID != c.UserID ||
		strings.Join(got.Chips, "|") != "3 people|You owe $3|0 photos" || got.Unread != 1 ||
		got.AlbumTitle == nil || *got.AlbumTitle != "Krog St dinner crew · Sep 24" || got.AlbumSubtitle == nil || *got.AlbumSubtitle != "0 photos · 3 people" ||
		got.LastMessage != "Maya: photo dump in the album after pls" || got.LastTime != "12:00 PM" {
		t.Fatalf("group for A: %+v", got)
	}
	srv.Do(t, "GET", "/threads/"+th.ID, nil, b).Expect(t, http.StatusOK).JSON(t, &got)
	if got.Chips[1] != "You're owed $6" || got.Faces[0].ID != a.UserID || got.LastMessage != "You: photo dump in the album after pls" {
		t.Fatalf("group for B: %+v", got)
	}
	srv.Clock.Advance(24 * time.Hour)
	a = srv.Login(t, a.Email, a.Password) // the access token outlived the jump
	srv.Do(t, "GET", "/threads/"+th.ID, nil, a).Expect(t, http.StatusOK).JSON(t, &got)
	if got.LastTime != "Sat" {
		t.Fatalf("yesterday's message reads as its weekday: %q", got.LastTime)
	}
	srv.Clock.Advance(7 * 24 * time.Hour)
	a = srv.Login(t, a.Email, a.Password)
	srv.Do(t, "GET", "/threads/"+th.ID, nil, a).Expect(t, http.StatusOK).JSON(t, &got)
	if got.LastTime != "Sep 26" {
		t.Fatalf("an older message reads as its date: %q", got.LastTime)
	}
}

func TestThreadWithNullMapsStillWorks(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Nell Null")
	b := srv.Signup(t, "Nico Null")
	// A thread written elsewhere with unread/readAt as null (nil Go maps).
	th := &models.Thread{ID: store.NewID(), MemberIDs: []string{a.UserID, b.UserID}, DMKey: models.DMKey(a.UserID, b.UserID),
		CreatedBy: a.UserID, LastMessageAt: srv.Clock.Now(), CreatedAt: srv.Clock.Now(), UpdatedAt: srv.Clock.Now()}
	if _, err := srv.Store.Collection(store.CollThreads).InsertOne(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	send(t, srv, a, th.ID, "hello", nil)
	if list := threadsOf(t, srv, b); len(list) != 1 || list[0].Unread != 1 {
		t.Fatalf("unread after repair: %+v", list)
	}
	srv.Do(t, "POST", "/threads/"+th.ID+"/read", nil, b).Expect(t, http.StatusNoContent)
}
