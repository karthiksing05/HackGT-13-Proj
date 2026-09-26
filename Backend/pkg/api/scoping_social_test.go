package api_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"testing"
	"time"
)

// backend-C's cross-user cases: B never reaches A's threads, groups, posts
// or friend requests (404, as if they did not exist).
func init() {
	scopes = append(scopes,
		scope{"GET /threads/{id} of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "GET", "/threads/" + dmOfA(t, srv, a).ID, nil
		}, http.StatusNotFound},
		scope{"GET /threads/{id}/messages of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "GET", "/threads/" + dmOfA(t, srv, a).ID + "/messages", nil
		}, http.StatusNotFound},
		scope{"POST /threads/{id}/messages in A's DM", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "POST", "/threads/" + dmOfA(t, srv, a).ID + "/messages", contract.NewMessage{Text: "hi"}
		}, http.StatusNotFound},
		scope{"POST /threads/{id}/read of A's DM", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "POST", "/threads/" + dmOfA(t, srv, a).ID + "/read", nil
		}, http.StatusNotFound},
		scope{"GET /groups/{id}/expenses of A's group", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "GET", "/groups/" + groupOfA(t, srv, a) + "/expenses", nil
		}, http.StatusNotFound},
		scope{"GET /groups/{id}/photos of A's group", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "GET", "/groups/" + groupOfA(t, srv, a) + "/photos", nil
		}, http.StatusNotFound},
		scope{"POST /groups/{id}/settle in A's group", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			return "POST", "/groups/" + groupOfA(t, srv, a) + "/settle", contract.SettleRequest{}
		}, http.StatusNotFound},
		scope{"DELETE /groups/{id}/expenses/{expenseId} of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			groupID := groupOfA(t, srv, a)
			var exp contract.Expense
			srv.Do(t, "POST", "/groups/"+groupID+"/expenses", contract.NewExpense{What: "Tacos", AmountCents: 1200, PayerID: a.UserID,
				SplitAmong: []string{a.UserID}}, a).Expect(t, http.StatusCreated).JSON(t, &exp)
			return "DELETE", "/groups/" + groupID + "/expenses/" + exp.ID, nil
		}, http.StatusNotFound},
		scope{"DELETE /forum/posts/{id} of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			lat, lng := 33.7838, -84.3833
			var post contract.MyFreePost
			srv.Do(t, "POST", "/forum/posts", contract.NewForumPost{Type: contract.PostFreeNow, Visibility: contract.PostEveryone,
				Lat: &lat, Lng: &lng}, a).Expect(t, http.StatusCreated).JSON(t, &post)
			return "DELETE", "/forum/posts/" + post.ID, nil
		}, http.StatusNotFound},
		scope{"DELETE /friends/requests/{id} sent by A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			other := srv.Signup(t, "Cara Scope")
			var req contract.FriendRequest
			srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: other.UserID}, a).Expect(t, http.StatusCreated).JSON(t, &req)
			return "DELETE", "/friends/requests/" + req.ID, nil
		}, http.StatusNotFound},
		scope{"POST /friends/requests/{id}/accept of a request to A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
			other := srv.Signup(t, "Dino Scope")
			var req contract.FriendRequest
			srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: a.UserID}, other).Expect(t, http.StatusCreated).JSON(t, &req)
			return "POST", "/friends/requests/" + req.ID + "/accept", nil
		}, http.StatusNotFound},
	)
}

// dmOfA is a DM between A and a third person.
func dmOfA(t *testing.T, srv *testutil.Server, a *testutil.Session) contract.ChatThread {
	t.Helper()
	other := srv.Signup(t, "Eli Scope")
	var th contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", contract.StartDMRequest{UserID: other.UserID}, a).Expect(t, http.StatusOK).JSON(t, &th)
	return th
}

// groupOfA is the group chat of a plan A hosts.
func groupOfA(t *testing.T, srv *testutil.Server, a *testutil.Session) string {
	t.Helper()
	now := srv.Clock.Now()
	it := &models.Itinerary{ID: store.NewID(), HostID: a.UserID, MemberIDs: []string{a.UserID}, Title: "Scoped plan",
		Start: now.Add(time.Hour), BackBy: now.Add(3 * time.Hour), Visibility: models.VisibilityFriends,
		Items: []models.ItineraryItem{}, Status: models.ItineraryActive, CreatedAt: now, UpdatedAt: now}
	if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	th, _, err := srv.Store.Threads().EnsureGroup(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	return th.ID
}
