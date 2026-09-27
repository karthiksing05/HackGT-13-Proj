package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"net/http"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// A reseed takes Sandy off Marin's plan and, with it, out of the plan's
// group chat that joining made; joining again brings the same chat back.
func TestSeedDemoTakesJoinersOutOfThePlanChat(t *testing.T) {
	srv, cfg := seedServer(t)
	st := srv.Store
	if _, err := seedDemo(t.Context(), st, cfg, nil, srv.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	sess := srv.Login(t, sandy.email, demoPassword)
	var joined contract.JoinResult
	srv.Do(t, "POST", "/forum/posts/"+seedOpenPlanID+"/join-requests", nil, sess).Expect(t, http.StatusOK).JSON(t, &joined)
	if joined.Status != contract.JoinJoined || joined.ThreadID == nil {
		t.Fatalf("join: %+v", joined)
	}
	chat := *joined.ThreadID
	srv.Do(t, "GET", "/threads/"+chat, nil, sess).Expect(t, http.StatusOK)

	if _, err := seedDemo(t.Context(), st, cfg, nil, srv.Clock.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	th := findDoc[models.Thread](t, st, store.CollThreads, bson.M{"_id": chat})
	if !sameSet(th.MemberIDs, []string{marin.id.Hex(), theo.id.Hex()}) {
		t.Fatalf("the plan's chat after a reseed: members %v", th.MemberIDs)
	}
	srv.Do(t, "GET", "/threads/"+chat, nil, sess).Expect(t, http.StatusNotFound)
	var threads []contract.ChatThread
	srv.Do(t, "GET", "/threads", nil, sess).Expect(t, http.StatusOK).JSON(t, &threads)
	if slices.ContainsFunc(threads, func(c contract.ChatThread) bool { return c.ID == chat }) {
		t.Fatal("Groups still lists the chat of a plan Sandy is not on")
	}

	srv.Do(t, "POST", "/forum/posts/"+seedOpenPlanID+"/join-requests", nil, sess).Expect(t, http.StatusOK).JSON(t, &joined)
	if joined.Status != contract.JoinJoined || joined.ThreadID == nil || *joined.ThreadID != chat {
		t.Fatalf("joining again: %+v (the chat was %s)", joined, chat)
	}
}
