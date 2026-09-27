package social_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// taste is a unit-ish vector whose first value is what the fake ML service
// reads as the match: 0.8 → 80%.
func taste(match float64) []float64 {
	v := make([]float64, ml.Dim)
	v[0], v[1] = match, 1
	return v
}

// fakeMatchML answers the match routes from each vector's first value:
// users by the candidate's, itineraries by the mean over their stops.
func fakeMatchML(t *testing.T, srv *testutil.Server) *int {
	t.Helper()
	calls := new(int)
	type vecs struct {
		ID       string    `json:"id"`
		Positive []float64 `json:"positive_embedding"`
		Events   []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"events"`
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var body struct {
			Candidates  []vecs `json:"candidates"`
			Itineraries []vecs `json:"itineraries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("fake ML: %v", err)
		}
		type result struct {
			ID      string  `json:"id"`
			Score   float64 `json:"score"`
			Percent int     `json:"percent"`
		}
		var out []result
		switch r.URL.Path {
		case "/v1/compatibility/users":
			for _, c := range body.Candidates {
				out = append(out, result{c.ID, c.Positive[0], int(math.Round(100 * c.Positive[0]))})
			}
		case "/v1/compatibility/itineraries":
			for _, it := range body.Itineraries {
				sum := 0.0
				for _, e := range it.Events {
					sum += e.Embedding[0]
				}
				mean := sum / float64(len(it.Events))
				out = append(out, result{it.ID, mean, int(math.Round(100 * mean))})
			}
		default:
			http.NotFound(w, r)
			return
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": out, "model_version": "fake"})
	}))
	t.Cleanup(fake.Close)
	o := ml.DefaultOptions()
	o.BaseURL = fake.URL
	srv.Deps.ML = ml.NewClientWithOptions(o)
	return calls
}

func setTaste(t *testing.T, srv *testutil.Server, s *testutil.Session, match float64, extra bson.M) {
	t.Helper()
	set := bson.M{"positiveEmbedding": taste(match)}
	for k, v := range extra {
		set[k] = v
	}
	setUser(t, srv, s, set)
}

func suggested(t *testing.T, srv *testutil.Server, s *testutil.Session) []contract.PersonSuggestion {
	t.Helper()
	var out []contract.PersonSuggestion
	srv.Do(t, "GET", "/people/suggested", nil, s).Expect(t, http.StatusOK).JSON(t, &out)
	return out
}

func TestSuggestPeople(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Sugg Viewer")
	closeMatch := srv.Signup(t, "Close Match")
	far := srv.Signup(t, "Far Match")
	friend := srv.Signup(t, "Old Friend")
	bot := srv.Signup(t, "Bot Match")
	srv.Signup(t, "No Taste") // no taste profile: never suggested

	// Without ML wired: 503.
	if res := srv.Do(t, "GET", "/people/suggested", nil, viewer); res.Status != http.StatusServiceUnavailable {
		t.Fatalf("no ML: status %d", res.Status)
	}
	calls := fakeMatchML(t, srv)

	// A viewer without a taste profile gets an empty list and no ML call.
	if got := suggested(t, srv, viewer); len(got) != 0 || *calls != 0 {
		t.Fatalf("no viewer taste: %v, %d calls", got, *calls)
	}

	setTaste(t, srv, viewer, 0.5, nil)
	setTaste(t, srv, closeMatch, 0.9, nil)
	setTaste(t, srv, far, 0.3, nil)
	setTaste(t, srv, friend, 0.95, nil)
	setTaste(t, srv, bot, 0.99, bson.M{"roles": []string{"bot"}})
	befriend(t, srv, viewer, friend)
	var req contract.FriendRequest
	srv.Do(t, "POST", "/friends/requests", contract.StartDMRequest{UserID: far.UserID}, viewer).Expect(t, http.StatusCreated).JSON(t, &req)

	got := suggested(t, srv, viewer)
	if len(got) != 2 || got[0].Person.ID != closeMatch.UserID || got[1].Person.ID != far.UserID {
		t.Fatalf("suggestions: %+v", got)
	}
	if got[0].Compatibility != 90 || got[0].Relation != contract.RelationNone {
		t.Fatalf("closeMatch match: %+v", got[0])
	}
	if got[1].Compatibility != 30 || got[1].Relation != contract.RelationOutgoing || got[1].RequestID == nil || *got[1].RequestID != req.ID {
		t.Fatalf("far match: %+v", got[1])
	}
}

func TestForumForYou(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed))
	viewer := srv.Signup(t, "Forum Viewer")
	host := srv.Signup(t, "Forum Host")
	setUser(t, srv, viewer, home(midtown))

	activity := func(match float64) string {
		id := bson.NewObjectID()
		if _, err := srv.Store.Collection(store.CollActivities).InsertOne(context.Background(),
			bson.M{"_id": id, "name": "Spot", "embedding": taste(match)}); err != nil {
			t.Fatal(err)
		}
		return id.Hex()
	}
	withStops := func(ids ...string) func(*models.Itinerary) {
		return func(it *models.Itinerary) {
			n := 0
			for i := range it.Items {
				if it.Items[i].Kind == models.ItemStop && n < len(ids) {
					it.Items[i].ActivityID = ids[n]
					n++
				}
			}
		}
	}
	soon := insertPlan(t, srv, plan(host.UserID, testutil.Fixed.Add(time.Hour), techSquare, withStops(activity(0.2), activity(0.4))))
	best := insertPlan(t, srv, plan(host.UserID, testutil.Fixed.Add(3*time.Hour), techSquare, withStops(activity(0.9), activity(0.7))))
	unscored := insertPlan(t, srv, plan(host.UserID, testutil.Fixed.Add(2*time.Hour), techSquare))

	// No ML: the feed still works, without matches, and for_you falls back to soonest.
	posts := feed(t, srv, viewer, midtown, "sort=for_you")
	if got := ids(posts); len(got) != 3 || got[0] != soon.ID || posts[0].Compatibility != nil {
		t.Fatalf("no ML: %v", got)
	}

	fakeMatchML(t, srv)
	// No viewer taste yet: still no matches.
	if posts := feed(t, srv, viewer, midtown, ""); find(posts, best.ID).Compatibility != nil {
		t.Fatal("match without a viewer taste profile")
	}

	setTaste(t, srv, viewer, 0.5, nil)
	posts = feed(t, srv, viewer, midtown, "sort=for_you")
	if got := ids(posts); len(got) != 3 || got[0] != best.ID || got[1] != soon.ID || got[2] != unscored.ID {
		t.Fatalf("for_you order: %v", got)
	}
	if c := posts[0].Compatibility; c == nil || *c != 80 {
		t.Fatalf("best match: %v", c)
	}
	if c := posts[1].Compatibility; c == nil || *c != 30 {
		t.Fatalf("soon match: %v", c)
	}
	if posts[2].Compatibility != nil {
		t.Fatal("a plan without catalog stops has a match")
	}

	// Other sorts keep their order but still show the match.
	posts = feed(t, srv, viewer, midtown, "")
	if got := ids(posts); got[0] != soon.ID || posts[0].Compatibility == nil {
		t.Fatalf("soonest: %v", got)
	}
}
