package facebook_test

import (
	"Backend/pkg/api/facebook"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	appID     = "1234567890"
	appSecret = "sq-test-app-secret"
	callback  = "http://api.test/integrations/facebook/callback"
	appReturn = "sidequestz://integrations/facebook"
)

// env is a full API server with Facebook configured, a fake Graph behind it
// (nothing reaches Facebook) and a profile recorder.
type env struct {
	srv  *testutil.Server
	fake *facebook.FakeGraph
	prof *testutil.ProfilesRecorder
}

func newEnv(t *testing.T) *env {
	t.Helper()
	prof := &testutil.ProfilesRecorder{}
	srv := testutil.New(t, testutil.WithProfiles(prof), testutil.WithConfig(func(c *config.Config) {
		c.FBAppID, c.FBAppSecret = appID, appSecret
	}))
	fake := facebook.NewFakeGraph()
	facebook.UseGraph(srv.Deps, fake)
	return &env{srv: srv, fake: fake, prof: prof}
}

// get sends a GET without following redirects (the callback answers 302 to
// the app's URL scheme).
func (e *env) get(t *testing.T, path string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(e.srv.URL(path))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// dialog asks for the Login dialog and returns its URL.
func (e *env) dialog(t *testing.T, sess *testutil.Session, rerequest bool) *url.URL {
	t.Helper()
	var out contract.URLResponse
	e.srv.Do(t, "POST", "/integrations/facebook/connect", contract.FacebookConnectRequest{Rerequest: rerequest}, sess).
		Expect(t, http.StatusOK).JSON(t, &out)
	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("dialog url %q: %v", out.URL, err)
	}
	return u
}

// callback hits the callback with query and returns the raw Location.
func (e *env) callback(t *testing.T, q url.Values) string {
	t.Helper()
	res := e.get(t, "/integrations/facebook/callback?"+q.Encode())
	if res.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("callback answered %d: %s", res.StatusCode, body)
	}
	return res.Header.Get("Location")
}

// connect runs the whole Login for sess as the fake Facebook user.
func (e *env) connect(t *testing.T, sess *testutil.Session, fbUserID string) {
	t.Helper()
	state := e.dialog(t, sess, false).Query().Get("state")
	loc := e.callback(t, url.Values{"code": {e.fake.IssueCode(fbUserID)}, "state": {state}})
	if loc != appReturn+"?status=connected" {
		t.Fatalf("connect %s: redirected to %s", fbUserID, loc)
	}
}

func (e *env) connection(t *testing.T, sess *testutil.Session) contract.FacebookConnection {
	t.Helper()
	var conn contract.FacebookConnection
	e.srv.Do(t, "GET", "/integrations/facebook", nil, sess).Expect(t, http.StatusOK).JSON(t, &conn)
	return conn
}

func (e *env) importNow(t *testing.T, sess *testutil.Session) contract.FacebookImport {
	t.Helper()
	var imp contract.FacebookImport
	e.srv.Do(t, "POST", "/integrations/facebook/import", nil, sess).Expect(t, http.StatusOK).JSON(t, &imp)
	return imp
}

// signed posts a form signed_request to one of Facebook's callbacks.
func (e *env) signed(t *testing.T, path, signedRequest string) *testutil.Response {
	t.Helper()
	form := url.Values{"signed_request": {signedRequest}}
	return e.srv.DoRaw(t, "POST", path, strings.NewReader(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func signFor(t *testing.T, fbUserID string) string {
	t.Helper()
	raw, err := facebook.SignRequest(map[string]any{"algorithm": "HMAC-SHA256", "issued_at": time.Now().Unix(), "user_id": fbUserID}, appSecret)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (e *env) account(t *testing.T, userID string) *models.FacebookAccount {
	t.Helper()
	acct, err := e.srv.Store.Facebook().Account(context.Background(), userID)
	if err != nil {
		t.Fatalf("account of %s: %v", userID, err)
	}
	return acct
}

func (e *env) hasAccount(t *testing.T, userID string) bool {
	t.Helper()
	_, err := e.srv.Store.Facebook().Account(context.Background(), userID)
	return err == nil
}

func (e *env) hasImport(t *testing.T, userID string) bool {
	t.Helper()
	_, err := e.srv.Store.Facebook().Import(context.Background(), userID)
	return err == nil
}

func (e *env) interests(t *testing.T, userID string) []string {
	t.Helper()
	u, err := e.srv.Store.Users().ByID(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return u.FacebookInterests
}

// befriend stores a friendship the way pkg/models/social.go defines it.
func (e *env) befriend(t *testing.T, a, b string) {
	t.Helper()
	pair := []string{a, b}
	if b < a {
		pair = []string{b, a}
	}
	_, err := e.srv.Store.Collection(store.CollFriendships).InsertOne(context.Background(),
		models.Friendship{ID: models.FriendshipID(a, b), UserIDs: pair, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
}

// request stores a pending friend request from → to and returns its id.
func (e *env) request(t *testing.T, from, to string) string {
	t.Helper()
	id := store.NewID()
	now := time.Now().UTC()
	_, err := e.srv.Store.Collection(store.CollFriendRequests).InsertOne(context.Background(),
		models.FriendRequest{ID: id, FromID: from, ToID: to, Status: models.RequestPending, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// rawAccount is the stored document as Mongo holds it.
func (e *env) rawAccount(t *testing.T, userID string) bson.M {
	t.Helper()
	var doc bson.M
	if err := e.srv.Store.Collection(store.CollFacebookAccounts).FindOne(context.Background(), bson.M{"_id": userID}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func like(id, name, category string, list ...string) facebook.Like {
	return facebook.Like{ID: id, Name: name, Category: category, Categories: list}
}

// jordan likes 21 Pages: outdoors 6 (max), food 5, long walks 3, live music
// 3, nightlife 2 and museums 2 (both below the three-Page rule), plus a
// politician and a generic business that count for nothing.
func jordan() facebook.FakeUser {
	return facebook.FakeUser{
		ID: "fb-jordan", Name: "Jordan Lee", Location: "Atlanta, Georgia",
		Likes: []facebook.Like{
			like("1", "Piedmont Park", "Park"), like("2", "Freedom Park", "Park"), like("3", "Grant Park", "Park"),
			like("4", "Beltline Eastside Trail", "Hiking Trail"), like("5", "Arabia Mountain", "Hiking Trail"),
			like("6", "Tybee Beach", "Beach"), like("7", "Atlanta Walking Tours", "Walking Tour"),
			like("8", "Octane Coffee", "Coffee Shop"), like("9", "Chrome Yellow", "Coffee Shop"),
			like("10", "Tex's Tacos", "Food Truck"), like("11", "Antico Pizza", "Italian Restaurant"),
			like("12", "Corner Bakery Co", "Local Business", "Local Business", "Bakery"),
			like("13", "The Indie Band", "Musician/Band"), like("14", "Another Band", "Musician/Band"),
			like("15", "Third Band", "Musician/Band"),
			like("16", "Sister Louisa's", "Bar"), like("17", "Monday Night Brewing", "Brewery"),
			like("18", "Whitespace", "Art Gallery"), like("19", "MINT Gallery", "Art Gallery"),
			like("20", "Some Politician", "Politician"), like("21", "Acme Holdings", "Local Business"),
		},
		Friends: []facebook.Friend{{ID: "fb-priya", Name: "Priya K."}, {ID: "fb-chris", Name: "Chris N."}, {ID: "fb-sam", Name: "Sam Not On App"}},
	}
}
