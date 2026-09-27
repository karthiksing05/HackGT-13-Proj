package itineraries_test

import (
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"testing"
)

func search(t *testing.T, srv *testutil.Server, sess *testutil.Session, q string) contract.SearchResults {
	t.Helper()
	var out contract.SearchResults
	srv.Do(t, "GET", "/search?q="+q, nil, sess).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func titles(its []contract.Itinerary) []string {
	var out []string
	for _, it := range its {
		out = append(out, it.Title)
	}
	return out
}

func TestSearch(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(exampleClock))
	seedCatalog(t, srv, store.ActivityCollection, atlanta)
	ctx := context.Background()
	a := srv.Signup(t, "Alice Search")
	b := srv.Signup(t, "Bob Search")
	maya := srv.Signup(t, "Maya Rivera")
	mateo := srv.Signup(t, "Mateo Diaz")
	mara := srv.Signup(t, "Mara Lee")
	max := srv.Signup(t, "Max Stone")
	now := srv.Clock.Now()
	friendship := models.Friendship{ID: models.FriendshipID(a.UserID, maya.UserID), UserIDs: []string{a.UserID, maya.UserID}, CreatedAt: now}
	outgoing := models.FriendRequest{ID: store.NewID(), FromID: a.UserID, ToID: mateo.UserID, Status: models.RequestPending, CreatedAt: now, UpdatedAt: now}
	incoming := models.FriendRequest{ID: store.NewID(), FromID: mara.UserID, ToID: a.UserID, Status: models.RequestPending, CreatedAt: now, UpdatedAt: now}
	declined := models.FriendRequest{ID: store.NewID(), FromID: a.UserID, ToID: max.UserID, Status: models.RequestDeclined, CreatedAt: now, UpdatedAt: now}
	if _, err := srv.Store.Collection(store.CollFriendships).InsertOne(ctx, friendship); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store.Collection(store.CollFriendRequests).InsertMany(ctx, []any{outgoing, incoming, declined}); err != nil {
		t.Fatal(err)
	}
	rooftop := create(t, srv, a, exampleRequest(t))
	park := create(t, srv, a, plan("Park day", contract.VisibilityJustMe,
		stopSpec{id: "p0", title: "Piedmont Park loop", start: at(9, 25, 15, 0), minutes: 60}))
	create(t, srv, b, plan("Rooftop party", contract.VisibilityJustMe,
		stopSpec{id: "q0", title: "Somebody's roof", start: at(9, 25, 20, 0), minutes: 60}))

	res := srv.Do(t, "GET", "/search?q=%20", nil, a).Expect(t, http.StatusOK)
	if got := canonicalJSON(t, res.Body); got != `{"people":[],"places":[],"posts":[],"sidequests":[]}` {
		t.Fatalf("empty query: %s", got)
	}

	// Sidequests: the viewer's own active plans, by title or a stop's title, any case.
	if got := search(t, srv, a, "ROOFTOP"); len(got.Sidequests) != 1 || got.Sidequests[0].ID != rooftop.ID || !got.Sidequests[0].IsHost {
		t.Fatalf("rooftop: %v", titles(got.Sidequests))
	}
	if got := search(t, srv, a, "piedmont"); len(got.Sidequests) != 1 || got.Sidequests[0].ID != park.ID {
		t.Fatalf("by stop title: %v", titles(got.Sidequests))
	}
	if got := search(t, srv, a, "marta"); len(got.Sidequests) != 0 {
		t.Fatalf("transit legs are not searched: %v", titles(got.Sidequests))
	}

	// People as /users/search: handle or name word, never yourself, with the relation.
	got := search(t, srv, a, "ma")
	want := map[string]contract.FriendRelation{
		mara.UserID: contract.RelationIncoming, mateo.UserID: contract.RelationOutgoing,
		max.UserID: contract.RelationNone, maya.UserID: contract.RelationFriend,
	}
	if len(got.People) != len(want) {
		t.Fatalf("people for ma: %+v", got.People)
	}
	for _, p := range got.People {
		if want[p.Person.ID] != p.Relation {
			t.Errorf("%s: relation %s, want %s", p.Person.Name, p.Relation, want[p.Person.ID])
		}
		switch p.Person.ID {
		case mateo.UserID:
			if p.RequestID == nil || *p.RequestID != outgoing.ID {
				t.Errorf("outgoing request id: %v", p.RequestID)
			}
		case mara.UserID:
			if p.RequestID == nil || *p.RequestID != incoming.ID {
				t.Errorf("incoming request id: %v", p.RequestID)
			}
		default:
			if p.RequestID != nil {
				t.Errorf("%s has a request id", p.Person.Name)
			}
		}
	}
	if got := search(t, srv, a, "alice"); len(got.People) != 0 {
		t.Fatalf("you never find yourself: %+v", got.People)
	}

	// Places from the viewer's catalog, nearest to the home base, at most 5.
	setHome(t, srv, a, "Home · North Ave Apts", 33.7710, -84.3918)
	if got := search(t, srv, a, "park"); len(got.Places) != 4 || got.Places[0].Name != "Centennial Olympic Park" {
		t.Fatalf("places: %v", placeNames(got.Places))
	}

	// Forum posts come from the social area once it is installed.
	if got := search(t, srv, a, "krog"); len(got.Posts) != 0 {
		t.Fatalf("posts without the social views: %+v", got.Posts)
	}
	itineraries.UseSocial(&socialRecorder{})
	t.Cleanup(func() { itineraries.UseSocial(nil) })
	if got := search(t, srv, a, "krog"); len(got.Posts) != 1 || got.Posts[0].Title == nil || *got.Posts[0].Title != "Found: krog" {
		t.Fatalf("posts through the social views: %+v", got.Posts)
	}
	srv.Do(t, "GET", "/search?q=krog", nil, nil).Expect(t, http.StatusUnauthorized)
}
