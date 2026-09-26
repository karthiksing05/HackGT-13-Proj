package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFacebookAccounts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	fb := s.Facebook()
	if _, err := fb.Account(ctx, "u1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	if _, err := fb.AccountByFBUserID(ctx, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("empty fb id: %v", err)
	}
	if err := fb.SaveAccount(ctx, &models.FacebookAccount{UserID: "u1", FBUserID: "fb1", AccessTokenEnc: "v1.x", Name: "One"}); err != nil {
		t.Fatal(err)
	}
	acct, err := fb.Account(ctx, "u1")
	if err != nil || acct.FBUserID != "fb1" || !acct.ConnectedAt.Equal(testNow) || !acct.UpdatedAt.Equal(testNow) ||
		acct.GrantedScopes == nil || acct.DeclinedScopes == nil {
		t.Fatalf("saved account = %+v, %v", acct, err)
	}
	if byFB, err := fb.AccountByFBUserID(ctx, "fb1"); err != nil || byFB.UserID != "u1" {
		t.Fatalf("by fb id = %+v, %v", byFB, err)
	}
	// One Facebook user, one SideQuests account.
	if err := fb.SaveAccount(ctx, &models.FacebookAccount{UserID: "u2", FBUserID: "fb1", AccessTokenEnc: "v1.y"}); !errors.Is(err, store.ErrFacebookTaken) || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second link of fb1: %v", err)
	}
	// Saving again replaces the document.
	if err := fb.SaveAccount(ctx, &models.FacebookAccount{UserID: "u1", FBUserID: "fb1", AccessTokenEnc: "v1.z", DeclinedScopes: []string{"user_likes"}}); err != nil {
		t.Fatal(err)
	}
	if acct, _ := fb.Account(ctx, "u1"); acct.AccessTokenEnc != "v1.z" || acct.Name != "" || !reflect.DeepEqual(acct.DeclinedScopes, []string{"user_likes"}) {
		t.Fatalf("replaced account = %+v", acct)
	}

	if err := fb.MarkNeedsReconnect(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := fb.UpdateProfile(ctx, "u1", "Renamed", []string{"public_profile"}, nil); err != nil {
		t.Fatal(err)
	}
	acct, _ = fb.Account(ctx, "u1")
	if !acct.NeedsReconnect || acct.Name != "Renamed" || acct.DeclinedScopes == nil || len(acct.DeclinedScopes) != 0 {
		t.Fatalf("after updates = %+v", acct)
	}
	if err := fb.MarkNeedsReconnect(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mark unknown: %v", err)
	}
	if err := fb.UpdateProfile(ctx, "nobody", "x", nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	// Deauthorize drops the token and the reconnect flag but keeps the link.
	userID, err := fb.Deauthorize(ctx, "fb1")
	if err != nil || userID != "u1" {
		t.Fatalf("deauthorize = %q, %v", userID, err)
	}
	if acct, _ := fb.Account(ctx, "u1"); acct.AccessTokenEnc != "" || acct.NeedsReconnect || acct.FBUserID != "fb1" {
		t.Fatalf("after deauthorize = %+v", acct)
	}
	if _, err := fb.Deauthorize(ctx, "fb-unknown"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deauthorize unknown: %v", err)
	}

	ids, err := fb.UserIDsByFBUserID(ctx, []string{"fb1", "fb-unknown"})
	if err != nil || !reflect.DeepEqual(ids, map[string]string{"fb1": "u1"}) {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	if ids, err := fb.UserIDsByFBUserID(ctx, nil); err != nil || len(ids) != 0 {
		t.Fatalf("no ids = %v, %v", ids, err)
	}
}

func TestFacebookImportsAndForget(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	fb := s.Facebook()
	user := newUser("Jordan Lee", "jordan@example.test", "jordan")
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	id := user.ID.Hex()
	if err := s.Users().SetFacebookInterests(ctx, id, []string{"Hiking", "Coffee"}); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.Users().ByID(ctx, id); !reflect.DeepEqual(u.FacebookInterests, []string{"Hiking", "Coffee"}) {
		t.Fatalf("interests = %v", u.FacebookInterests)
	}
	if err := fb.SaveAccount(ctx, &models.FacebookAccount{UserID: id, FBUserID: "fb1", AccessTokenEnc: "v1.x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Import(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no import yet: %v", err)
	}
	city := "Atlanta, Georgia"
	if err := fb.SaveImport(ctx, &models.FacebookImport{UserID: id, ImportedAt: testNow, LikedPages: 2, City: &city,
		Pages: []models.FacebookPage{{ID: "p1", Category: "Park"}, {ID: "p2", Category: "Bar"}}}); err != nil {
		t.Fatal(err)
	}
	if err := fb.SaveImport(ctx, &models.FacebookImport{UserID: id, ImportedAt: testNow, LikedPages: 1,
		Pages: []models.FacebookPage{{ID: "p3", Category: "Museum"}}}); err != nil {
		t.Fatal(err)
	}
	imp, err := fb.Import(ctx, id)
	if err != nil || imp.LikedPages != 1 || len(imp.Pages) != 1 || imp.City != nil || imp.FriendFBIDs == nil ||
		imp.SuggestedRatings == nil || imp.Interests == nil {
		t.Fatalf("replaced import = %+v, %v", imp, err)
	}

	if err := fb.Forget(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Account(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("account kept")
	}
	if _, err := fb.Import(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("import kept")
	}
	if u, _ := s.Users().ByID(ctx, id); u.FacebookInterests != nil {
		t.Fatalf("interests kept: %v", u.FacebookInterests)
	}
	// Forgetting twice, or someone without a users document, is harmless.
	if err := fb.Forget(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := fb.Forget(ctx, "not-an-object-id"); err != nil {
		t.Fatal(err)
	}
}

func TestFacebookFriendLinks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	friendships := s.Collection(store.CollFriendships)
	requests := s.Collection(store.CollFriendRequests)
	if _, err := friendships.InsertOne(ctx, models.Friendship{ID: models.FriendshipID("me", "friend"), UserIDs: []string{"friend", "me"}, CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []models.FriendRequest{
		{ID: "r-in", FromID: "asker", ToID: "me", Status: models.RequestPending},
		{ID: "r-out", FromID: "me", ToID: "asked", Status: models.RequestPending},
		// Pending both ways: the incoming one wins, whatever the order.
		{ID: "r-both-out", FromID: "me", ToID: "both", Status: models.RequestPending},
		{ID: "r-both-in", FromID: "both", ToID: "me", Status: models.RequestPending},
		{ID: "r-both2-in", FromID: "both2", ToID: "me", Status: models.RequestPending},
		{ID: "r-both2-out", FromID: "me", ToID: "both2", Status: models.RequestPending},
		// Answered requests and strangers' requests do not count.
		{ID: "r-declined", FromID: "declined", ToID: "me", Status: models.RequestDeclined},
		{ID: "r-others", FromID: "asker", ToID: "someone", Status: models.RequestPending},
	} {
		req.CreatedAt, req.UpdatedAt = testNow, testNow
		if _, err := requests.InsertOne(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	others := []string{"friend", "asker", "asked", "both", "both2", "declined", "stranger"}
	friends, pending, err := s.Facebook().FriendLinks(ctx, "me", others)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(friends, map[string]bool{"friend": true}) {
		t.Fatalf("friends = %v", friends)
	}
	got := map[string]string{}
	for id, req := range pending {
		got[id] = req.ID
	}
	want := map[string]string{"asker": "r-in", "asked": "r-out", "both": "r-both-in", "both2": "r-both2-in"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending = %v, want %v", got, want)
	}
	if f, p, err := s.Facebook().FriendLinks(ctx, "me", nil); err != nil || len(f) != 0 || len(p) != 0 {
		t.Fatalf("no others: %v %v %v", f, p, err)
	}
}
