package facebook_test

import (
	"Backend/pkg/api/facebook"
	"Backend/pkg/contract"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestConnectionStartsDisconnected(t *testing.T) {
	e := newEnv(t)
	a := e.srv.Signup(t, "Jordan Lee")
	res := e.srv.Do(t, "GET", "/integrations/facebook", nil, a).Expect(t, http.StatusOK)
	if got := strings.TrimSpace(string(res.Body)); got != `{"connected":false,"needs_reconnect":false,"declined_scopes":[]}` {
		t.Fatalf("empty connection = %s", got)
	}
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusConflict || res.Message() != facebook.MsgConnectFirst {
		t.Fatalf("import before connecting: %d %s", res.Status, res.Body)
	}
	if res := e.srv.Do(t, "DELETE", "/integrations/facebook", nil, a); res.Status != http.StatusNoContent {
		t.Fatalf("disconnect without a connection: %d %s", res.Status, res.Body)
	}
	for _, route := range [][2]string{{"GET", "/integrations/facebook"}, {"POST", "/integrations/facebook/connect"},
		{"POST", "/integrations/facebook/import"}, {"DELETE", "/integrations/facebook"}} {
		if res := e.srv.Do(t, route[0], route[1], nil, nil); res.Status != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", route[0], route[1], res.Status)
		}
	}
	if len(e.fake.Calls()) != 0 {
		t.Fatalf("Graph called: %v", e.fake.Calls())
	}
}

func TestConnectURL(t *testing.T) {
	e := newEnv(t)
	a := e.srv.Signup(t, "Jordan Lee")
	u := e.dialog(t, a, false)
	if u.Scheme != "https" || u.Host != "www.facebook.com" || u.Path != "/v26.0/dialog/oauth" {
		t.Fatalf("dialog = %s", u)
	}
	q := u.Query()
	want := map[string]string{"client_id": appID, "redirect_uri": callback, "response_type": "code",
		"scope": "public_profile,user_likes,user_location,user_friends"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("auth_type") {
		t.Error("auth_type without rerequest")
	}
	state := q.Get("state")
	sess, err := e.srv.Store.WebSessions().Peek(context.Background(), state, store.PurposeFacebookState)
	if err != nil || sess.UserID != a.UserID || sess.Provider != "facebook" || sess.Rerequest {
		t.Fatalf("state session = %+v, %v", sess, err)
	}
	if ttl := sess.ExpiresAt.Sub(sess.CreatedAt); ttl != 10*time.Minute {
		t.Fatalf("state lives %s, want 10m", ttl)
	}

	again := e.dialog(t, a, true)
	if again.Query().Get("auth_type") != "rerequest" {
		t.Fatalf("rerequest dialog = %s", again)
	}
	if again.Query().Get("state") == state {
		t.Fatal("state reused across dialogs")
	}
	if sess, _ := e.srv.Store.WebSessions().Peek(context.Background(), again.Query().Get("state"), store.PurposeFacebookState); sess == nil || !sess.Rerequest {
		t.Fatal("rerequest not recorded on the state")
	}
	// The app may send no body at all.
	if res := e.srv.DoRaw(t, "POST", "/integrations/facebook/connect", nil, map[string]string{"Authorization": a.Bearer()}); res.Status != http.StatusOK {
		t.Fatalf("connect without a body: %d %s", res.Status, res.Body)
	}
}

func TestConnectorOffWithoutAppCredentials(t *testing.T) {
	srv := testutil.New(t) // no FB_APP_ID / FB_APP_SECRET
	facebook.UseGraph(srv.Deps, facebook.NewFakeGraph())
	a := srv.Signup(t, "No Facebook")
	if res := srv.Do(t, "POST", "/integrations/facebook/connect", contract.FacebookConnectRequest{}, a); res.Status != http.StatusServiceUnavailable || res.Message() != facebook.MsgNotSetUp {
		t.Fatalf("connect: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "GET", "/integrations/facebook", nil, a); res.Status != http.StatusOK {
		t.Fatalf("connection state must still answer: %d %s", res.Status, res.Body)
	}
	form := url.Values{"signed_request": {"x.y"}}
	res := srv.DoRaw(t, "POST", "/integrations/facebook/deauthorize", strings.NewReader(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if res.Status != http.StatusServiceUnavailable {
		t.Fatalf("deauthorize without a secret: %d", res.Status)
	}
	// A state issued before the credentials went away ends in a sentence, not a 500.
	state, err := srv.Store.WebSessions().Create(context.Background(), a.UserID, store.PurposeFacebookState, "facebook", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL("/integrations/facebook/callback?code=c&state=" + state))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != appReturn+"?status=error&message=Facebook%20isn%27t%20set%20up%20on%20this%20server%20yet." {
		t.Fatalf("callback without credentials: %d %s", resp.StatusCode, loc)
	}
}

func TestCallbackConnects(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	state := e.dialog(t, a, false).Query().Get("state")
	res := e.get(t, "/integrations/facebook/callback?"+url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {state}}.Encode())
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != appReturn+"?status=connected" {
		t.Fatalf("callback: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Error("callback response may be cached")
	}
	if got := e.fake.Redirects(); len(got) != 1 || got[0] != callback {
		t.Fatalf("code exchanged with redirect_uri %v, want %s", got, callback)
	}
	if got := e.fake.Calls(); !reflect.DeepEqual(got, []string{"ExchangeCode", "LongLived", "Me", "Permissions"}) {
		t.Fatalf("Graph calls = %v", got)
	}

	acct := e.account(t, a.UserID)
	if acct.FBUserID != "fb-jordan" || acct.Name != "Jordan Lee" || acct.NeedsReconnect ||
		!reflect.DeepEqual(acct.GrantedScopes, []string{"public_profile", "user_likes", "user_location", "user_friends"}) ||
		len(acct.DeclinedScopes) != 0 {
		t.Fatalf("stored account = %+v", acct)
	}
	if !strings.HasPrefix(acct.AccessTokenEnc, "v1.") || strings.Contains(acct.AccessTokenEnc, "EAA") {
		t.Fatalf("token not sealed: %q", acct.AccessTokenEnc)
	}
	if left := acct.TokenExpiresAt.Sub(e.srv.Clock.Now()); left < 59*24*time.Hour || left > 61*24*time.Hour {
		t.Fatalf("long-lived token expires in %s", left)
	}
	conn := e.connection(t, a)
	if !conn.Connected || conn.NeedsReconnect || conn.Name == nil || *conn.Name != "Jordan Lee" || len(conn.DeclinedScopes) != 0 || conn.LastImport != nil {
		t.Fatalf("connection = %+v", conn)
	}

	// The state is single use.
	loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {state}})
	if loc != appReturn+"?status=error&message=That%20sign-in%20link%20expired.%20Try%20again." {
		t.Fatalf("reused state: %s", loc)
	}
}

func TestCallbackDenied(t *testing.T) {
	e := newEnv(t)
	a := e.srv.Signup(t, "Jordan Lee")
	state := e.dialog(t, a, false).Query().Get("state")
	loc := e.callback(t, url.Values{"error": {"access_denied"}, "error_reason": {"user_denied"},
		"error_description": {"Permissions error"}, "state": {state}})
	if loc != appReturn+"?status=denied" {
		t.Fatalf("denied: %s", loc)
	}
	if e.hasAccount(t, a.UserID) || len(e.fake.Calls()) != 0 {
		t.Fatal("a denial connected something")
	}
	if loc := e.callback(t, url.Values{"code": {"x"}, "state": {state}}); !strings.Contains(loc, "status=error") {
		t.Fatalf("state reusable after a denial: %s", loc)
	}
}

func TestCallbackRejectsUnknownAndExpiredState(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	expired := appReturn + "?status=error&message=That%20sign-in%20link%20expired.%20Try%20again."

	if loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {"made-up"}}); loc != expired {
		t.Fatalf("unknown state: %s", loc)
	}
	if loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}}); loc != expired {
		t.Fatalf("missing state: %s", loc)
	}
	state := e.dialog(t, a, false).Query().Get("state")
	e.srv.Clock.Advance(11 * time.Minute)
	if loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {state}}); loc != expired {
		t.Fatalf("expired state: %s", loc)
	}
	if strings.Contains(expired, "+") {
		t.Fatal("spaces must be %20: the app reads + literally")
	}
	if e.hasAccount(t, a.UserID) || len(e.fake.Calls()) != 0 {
		t.Fatal("a bad state reached Facebook")
	}
}

func TestCallbackFailures(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	try := func(q url.Values) string {
		q.Set("state", e.dialog(t, a, false).Query().Get("state"))
		return e.callback(t, q)
	}
	msg := func(s string) string {
		return appReturn + "?status=error&message=" + strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
	}
	if loc := try(url.Values{}); loc != msg(facebook.MsgDidntConnect) {
		t.Fatalf("no code: %s", loc)
	}
	if loc := try(url.Values{"error": {"server_error"}}); loc != msg(facebook.MsgDidntConnect) {
		t.Fatalf("other error: %s", loc)
	}
	if loc := try(url.Values{"code": {"never-issued"}}); loc != msg(facebook.MsgDidntConnect) {
		t.Fatalf("bad code: %s", loc)
	}
	e.fake.Fail("LongLived", errors.New("dial tcp: i/o timeout"))
	if loc := try(url.Values{"code": {e.fake.IssueCode("fb-jordan")}}); loc != msg(facebook.MsgNoAnswer) {
		t.Fatalf("unreachable: %s", loc)
	}
	e.fake.Fail("LongLived", &facebook.GraphError{Code: 4, Message: "Application request limit reached"})
	if loc := try(url.Values{"code": {e.fake.IssueCode("fb-jordan")}}); loc != msg(facebook.MsgBusy) {
		t.Fatalf("rate limited: %s", loc)
	}
	e.fake.Fail("LongLived", nil)
	if e.hasAccount(t, a.UserID) {
		t.Fatal("a failed login stored an account")
	}
	e.connect(t, a, "fb-jordan")
}

func TestDeclinedScopesAndRerequest(t *testing.T) {
	e := newEnv(t)
	u := jordan()
	u.Permissions = map[string]string{"user_likes": "declined", "user_friends": "declined"}
	e.fake.AddUser(u)
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	if got := e.connection(t, a).DeclinedScopes; !reflect.DeepEqual(got, []string{"user_likes", "user_friends"}) {
		t.Fatalf("declined = %v", got)
	}
	imp := e.importNow(t, a)
	if imp.LikedPages != 0 || len(imp.SuggestedRatings) != 0 || len(imp.Interests) != 0 || len(imp.FriendsOnApp) != 0 ||
		imp.HomeArea == nil || *imp.HomeArea != "Atlanta, Georgia" {
		t.Fatalf("import without likes or friends = %+v", imp)
	}
	for _, call := range e.fake.Calls() {
		if call == "Likes" || call == "Friends" {
			t.Fatalf("read %s without the permission", call)
		}
	}

	// "Share them": the dialog asks again, and the new grant clears the list.
	if e.dialog(t, a, true).Query().Get("auth_type") != "rerequest" {
		t.Fatal("rerequest missing")
	}
	e.fake.AddUser(jordan())
	e.connect(t, a, "fb-jordan")
	if got := e.connection(t, a).DeclinedScopes; len(got) != 0 {
		t.Fatalf("declined after granting = %v", got)
	}
	if imp := e.importNow(t, a); imp.LikedPages != 21 {
		t.Fatalf("liked pages after granting = %d", imp.LikedPages)
	}
}

func TestImport(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	e.fake.AddUser(facebook.FakeUser{ID: "fb-priya", Name: "Priya K."})
	e.fake.AddUser(facebook.FakeUser{ID: "fb-chris", Name: "Chris N."})
	a := e.srv.Signup(t, "Jordan Lee")
	priya := e.srv.Signup(t, "Priya K.")
	chris := e.srv.Signup(t, "Chris N.")
	e.connect(t, a, "fb-jordan")
	e.connect(t, priya, "fb-priya")
	e.connect(t, chris, "fb-chris")
	e.befriend(t, a.UserID, priya.UserID)
	chrisAsked := e.request(t, chris.UserID, a.UserID)

	before := e.prof.Refreshes(a.UserID)
	res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a).Expect(t, http.StatusOK)
	var imp contract.FacebookImport
	res.JSON(t, &imp)

	if imp.LikedPages != 21 {
		t.Errorf("liked_pages = %d", imp.LikedPages)
	}
	wantRatings := contract.Ratings{"outdoors": 5, "food": 4, "long_walks": 3, "live_music": 3}
	if !reflect.DeepEqual(imp.SuggestedRatings, wantRatings) {
		t.Errorf("suggested_ratings = %v, want %v", imp.SuggestedRatings, wantRatings)
	}
	wantInterests := []string{"Bands", "Parks", "Art", "Coffee", "Hiking", "Baked goods", "Bars", "Beaches"}
	if !reflect.DeepEqual(imp.Interests, wantInterests) {
		t.Errorf("interests = %v, want %v", imp.Interests, wantInterests)
	}
	if imp.HomeArea == nil || *imp.HomeArea != "Atlanta, Georgia" {
		t.Errorf("home_area = %v", imp.HomeArea)
	}
	if !imp.ImportedAt.Equal(e.srv.Clock.Now().Truncate(time.Second)) {
		t.Errorf("imported_at = %s, want %s", imp.ImportedAt, e.srv.Clock.Now())
	}
	// Friends on the app, by name, with how they relate to Jordan here; Sam
	// never connected SideQuests and is left out.
	if len(imp.FriendsOnApp) != 2 {
		t.Fatalf("friends_on_app = %+v", imp.FriendsOnApp)
	}
	c, p := imp.FriendsOnApp[0], imp.FriendsOnApp[1]
	if c.Person.ID != chris.UserID || c.Person.Name != "Chris N." || c.Person.Initials != "CN" || c.Relation != contract.RelationIncoming ||
		c.RequestID == nil || *c.RequestID != chrisAsked {
		t.Errorf("Chris = %+v", c)
	}
	if p.Person.ID != priya.UserID || p.Relation != contract.RelationFriend || p.RequestID != nil {
		t.Errorf("Priya = %+v", p)
	}
	// Wire shape: snake_case keys the app decodes, canonical trip types.
	var wire map[string]any
	if err := json.Unmarshal(res.Body, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"imported_at", "liked_pages", "suggested_ratings", "interests", "home_area", "friends_on_app"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("wire key %s missing: %s", key, res.Body)
		}
	}
	for key := range imp.SuggestedRatings {
		if !contract.IsTripType(key) {
			t.Errorf("suggested key %q is not a trip type", key)
		}
	}

	// Stored raw for the ML stack, interests on the user, profile refreshed.
	stored, err := e.srv.Store.Facebook().Import(context.Background(), a.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Pages) != 21 || stored.Pages[11].CategoryList[1] != "Bakery" || stored.City == nil || *stored.City != "Atlanta, Georgia" ||
		!reflect.DeepEqual(stored.FriendFBIDs, []string{"fb-priya", "fb-chris", "fb-sam"}) || stored.SuggestedRatings["outdoors"] != 5 {
		t.Errorf("stored import = %+v", stored)
	}
	if got := e.interests(t, a.UserID); !reflect.DeepEqual(got, wantInterests) {
		t.Errorf("users.facebookInterests = %v", got)
	}
	if e.prof.Refreshes(a.UserID) < before+1 {
		t.Error("import did not refresh the taste profile")
	}

	// GET shows the same import, with relations as they are now.
	e.befriend(t, a.UserID, chris.UserID)
	if _, err := e.srv.Store.Collection(store.CollFriendRequests).DeleteOne(context.Background(), map[string]string{"_id": chrisAsked}); err != nil {
		t.Fatal(err)
	}
	conn := e.connection(t, a)
	if conn.LastImport == nil || conn.LastImport.LikedPages != 21 || !reflect.DeepEqual(conn.LastImport.Interests, wantInterests) {
		t.Fatalf("last_import = %+v", conn.LastImport)
	}
	if got := conn.LastImport.FriendsOnApp[0]; got.Relation != contract.RelationFriend || got.RequestID != nil {
		t.Fatalf("Chris after accepting = %+v", got)
	}

	// Priya's own view: Jordan is a friend; a new request from Priya shows
	// as outgoing for her and incoming for Jordan… once she imports.
	e.fake.AddUser(facebook.FakeUser{ID: "fb-priya", Name: "Priya K.", Friends: []facebook.Friend{{ID: "fb-jordan"}, {ID: "fb-chris"}}})
	e.connect(t, priya, "fb-priya")
	priyaAsked := e.request(t, priya.UserID, chris.UserID)
	pi := e.importNow(t, priya)
	if len(pi.FriendsOnApp) != 2 || pi.FriendsOnApp[0].Person.ID != chris.UserID || pi.FriendsOnApp[0].Relation != contract.RelationOutgoing ||
		*pi.FriendsOnApp[0].RequestID != priyaAsked || pi.FriendsOnApp[1].Person.ID != a.UserID || pi.FriendsOnApp[1].Relation != contract.RelationFriend {
		t.Fatalf("Priya's friends_on_app = %+v", pi.FriendsOnApp)
	}
	if len(pi.SuggestedRatings) != 0 || len(pi.Interests) != 0 || pi.HomeArea != nil {
		t.Fatalf("Priya's empty import = %+v", pi)
	}
}

func TestImportReplacesThePreviousOne(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	e.fake.SetLikes("fb-jordan", []facebook.Like{like("x", "Just a park", "Park")})
	e.srv.Clock.Advance(time.Hour)
	imp := e.importNow(t, a)
	if imp.LikedPages != 1 || len(imp.SuggestedRatings) != 0 || len(imp.Interests) != 0 {
		t.Fatalf("second import = %+v", imp)
	}
	stored, err := e.srv.Store.Facebook().Import(context.Background(), a.UserID)
	if err != nil || len(stored.Pages) != 1 || !stored.ImportedAt.Equal(e.srv.Clock.Now()) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if got := e.interests(t, a.UserID); len(got) != 0 {
		t.Fatalf("stale interests kept: %v", got)
	}
}

func TestImportTokenRejected(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)

	e.fake.ExpireTokens("fb-jordan") // password changed on Facebook
	res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a)
	if res.Status != http.StatusConflict || res.Message() != facebook.MsgReconnect {
		t.Fatalf("190: %d %s (must be 409, never 401)", res.Status, res.Body)
	}
	conn := e.connection(t, a)
	if !conn.Connected || !conn.NeedsReconnect || conn.LastImport == nil {
		t.Fatalf("after 190 = %+v", conn)
	}
	// The next import says so again without asking Facebook.
	calls := len(e.fake.Calls())
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusConflict || res.Message() != facebook.MsgReconnect {
		t.Fatalf("second import: %d %s", res.Status, res.Body)
	}
	if len(e.fake.Calls()) != calls {
		t.Fatal("Graph called although the token is known to be rejected")
	}
	// The session in SideQuests is untouched.
	e.srv.Do(t, "GET", "/me", nil, a).Expect(t, http.StatusOK)

	// Reconnecting clears it.
	e.fake.AddUser(jordan())
	e.connect(t, a, "fb-jordan")
	if conn := e.connection(t, a); conn.NeedsReconnect || !conn.Connected {
		t.Fatalf("after reconnecting = %+v", conn)
	}
	e.importNow(t, a)
}

func TestImportTokenPastItsExpiry(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.srv.Clock.Advance(61 * 24 * time.Hour)
	a = e.srv.Login(t, a.Email, a.Password)
	calls := len(e.fake.Calls())
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusConflict || res.Message() != facebook.MsgReconnect {
		t.Fatalf("expired long-lived token: %d %s", res.Status, res.Body)
	}
	if len(e.fake.Calls()) != calls || !e.account(t, a.UserID).NeedsReconnect {
		t.Fatal("an expired token must flag the connection without calling Graph")
	}
}

func TestImportGraphFailures(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")

	e.fake.Fail("Likes", errors.New("read: connection reset by peer"))
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusBadGateway || res.Message() != facebook.MsgNoAnswer {
		t.Fatalf("unreachable: %d %s", res.Status, res.Body)
	}
	e.fake.Fail("Likes", &facebook.GraphError{Code: 17, Message: "User request limit reached"})
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusServiceUnavailable || res.Message() != facebook.MsgBusy {
		t.Fatalf("rate limited: %d %s", res.Status, res.Body)
	}
	// A permission error on one edge reads as that scope being declined.
	e.fake.Fail("Likes", &facebook.GraphError{Code: 200, Message: "(#200) Requires user_likes permission"})
	imp := e.importNow(t, a)
	if imp.LikedPages != 0 || len(imp.FriendsOnApp) != 0 {
		t.Fatalf("import without likes = %+v", imp)
	}
	if got := e.connection(t, a).DeclinedScopes; !reflect.DeepEqual(got, []string{"user_likes"}) {
		t.Fatalf("declined after a permission error = %v", got)
	}
	e.fake.Fail("Likes", nil)
	if e.account(t, a.UserID).NeedsReconnect {
		t.Fatal("a non-token failure asked for a new sign-in")
	}
}

func TestImportSucceedsWhenTheProfileRefreshFails(t *testing.T) {
	e := newEnv(t)
	e.prof.Err = errors.New("ml service down")
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	before := e.prof.Refreshes(a.UserID)
	if imp := e.importNow(t, a); imp.LikedPages != 21 {
		t.Fatalf("import = %+v", imp)
	}
	if e.prof.Refreshes(a.UserID) < before+1 {
		t.Fatal("refresh not attempted")
	}
	if got := e.interests(t, a.UserID); len(got) == 0 {
		t.Fatal("interests not stored when the refresh failed")
	}
}

func TestDisconnect(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	refreshes := e.prof.Refreshes(a.UserID)

	e.srv.Do(t, "DELETE", "/integrations/facebook", nil, a).Expect(t, http.StatusNoContent)
	if !e.fake.Revoked("fb-jordan") {
		t.Fatal("DELETE /me/permissions not sent")
	}
	if e.hasAccount(t, a.UserID) || e.hasImport(t, a.UserID) || len(e.interests(t, a.UserID)) != 0 {
		t.Fatal("connection, import or interests left behind")
	}
	e.prof.WaitRefreshes(t, a.UserID, refreshes+1, time.Second)
	if conn := e.connection(t, a); conn.Connected || conn.LastImport != nil || conn.Name != nil {
		t.Fatalf("after disconnect = %+v", conn)
	}
	e.srv.Do(t, "DELETE", "/integrations/facebook", nil, a).Expect(t, http.StatusNoContent)

	// Facebook failing to revoke does not keep the data.
	e.fake.AddUser(jordan())
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	e.fake.Fail("Revoke", errors.New("timeout"))
	e.srv.Do(t, "DELETE", "/integrations/facebook", nil, a).Expect(t, http.StatusNoContent)
	if e.hasAccount(t, a.UserID) || e.hasImport(t, a.UserID) {
		t.Fatal("revoke failure kept the connection")
	}
}

func TestDeauthorize(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)

	// Bad or missing signatures change nothing.
	for name, raw := range map[string]string{
		"missing":      "",
		"wrong secret": mustSign(t, map[string]any{"algorithm": "HMAC-SHA256", "user_id": "fb-jordan"}, "not-the-secret"),
		"garbage":      "abc.def",
	} {
		if res := e.signed(t, "/integrations/facebook/deauthorize", raw); res.Status != http.StatusBadRequest || res.Message() != facebook.MsgBadSignature {
			t.Fatalf("%s signature: %d %s", name, res.Status, res.Body)
		}
	}
	if !e.connection(t, a).Connected {
		t.Fatal("an unsigned request disconnected")
	}

	res := e.signed(t, "/integrations/facebook/deauthorize", signFor(t, "fb-jordan")).Expect(t, http.StatusOK)
	if strings.TrimSpace(string(res.Body)) != "{}" {
		t.Fatalf("deauthorize body = %s", res.Body)
	}
	acct := e.account(t, a.UserID)
	if acct.AccessTokenEnc != "" || acct.FBUserID != "fb-jordan" {
		t.Fatalf("after deauthorize = %+v (token gone, link kept for a later deletion request)", acct)
	}
	if conn := e.connection(t, a); conn.Connected || conn.NeedsReconnect || conn.LastImport != nil {
		t.Fatalf("connection after deauthorize = %+v", conn)
	}
	if !e.hasImport(t, a.UserID) {
		t.Fatal("deauthorize alone must not delete the import")
	}
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a); res.Status != http.StatusConflict || res.Message() != facebook.MsgConnectFirst {
		t.Fatalf("import after deauthorize: %d %s", res.Status, res.Body)
	}
	// Unknown Facebook users are fine; Facebook only needs the 200.
	e.signed(t, "/integrations/facebook/deauthorize", signFor(t, "fb-nobody")).Expect(t, http.StatusOK)

	// Connecting again works as usual.
	e.fake.AddUser(jordan())
	e.connect(t, a, "fb-jordan")
	if !e.connection(t, a).Connected {
		t.Fatal("reconnect after deauthorize failed")
	}
}

func mustSign(t *testing.T, payload any, secret string) string {
	t.Helper()
	raw, err := facebook.SignRequest(payload, secret)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDataDeletion(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	// Facebook usually deauthorizes first; the deletion still finds the person.
	e.signed(t, "/integrations/facebook/deauthorize", signFor(t, "fb-jordan")).Expect(t, http.StatusOK)
	refreshes := e.prof.Refreshes(a.UserID)

	if res := e.signed(t, "/integrations/facebook/data-deletion", "x.y"); res.Status != http.StatusBadRequest {
		t.Fatalf("unsigned deletion: %d", res.Status)
	}
	var out contract.DataDeletionResponse
	e.signed(t, "/integrations/facebook/data-deletion", signFor(t, "fb-jordan")).Expect(t, http.StatusOK).JSON(t, &out)
	if out.ConfirmationCode == "" || out.URL != "http://api.test/integrations/facebook/deletion-status?code="+out.ConfirmationCode {
		t.Fatalf("deletion answer = %+v", out)
	}
	if e.hasAccount(t, a.UserID) || e.hasImport(t, a.UserID) || len(e.interests(t, a.UserID)) != 0 {
		t.Fatal("data left after a deletion request")
	}
	e.prof.WaitRefreshes(t, a.UserID, refreshes+1, time.Second)

	page := e.get(t, "/integrations/facebook/deletion-status?code="+url.QueryEscape(out.ConfirmationCode))
	body, _ := io.ReadAll(page.Body)
	if page.StatusCode != http.StatusOK || !strings.HasPrefix(page.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(string(body), out.ConfirmationCode) || !strings.Contains(string(body), "Your Facebook data is deleted") {
		t.Fatalf("status page: %d %s", page.StatusCode, body)
	}
	if page.Header.Get("Cache-Control") != "no-store" || page.Header.Get("Content-Security-Policy") == "" {
		t.Error("status page headers missing")
	}
	for _, code := range []string{"unknown-code", "", "<script>alert(1)</script>", strings.Repeat("x", 200)} {
		res := e.get(t, "/integrations/facebook/deletion-status?code="+url.QueryEscape(code))
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "couldn't find that request") || strings.Contains(string(body), "<script>") {
			t.Fatalf("code %q: %d %s", code, res.StatusCode, body)
		}
	}
	// A deletion for someone who never connected still gets a code.
	var other contract.DataDeletionResponse
	e.signed(t, "/integrations/facebook/data-deletion", signFor(t, "fb-stranger")).Expect(t, http.StatusOK).JSON(t, &other)
	if other.ConfirmationCode == "" || other.ConfirmationCode == out.ConfirmationCode {
		t.Fatalf("stranger's code = %+v", other)
	}
	// Codes answer for 90 days.
	e.srv.Clock.Advance(91 * 24 * time.Hour)
	if res := e.get(t, "/integrations/facebook/deletion-status?code="+out.ConfirmationCode); res.StatusCode != http.StatusNotFound {
		t.Fatalf("code after 91 days: %d", res.StatusCode)
	}
}

func TestLinkedToAnotherAccount(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	b := e.srv.Signup(t, "Jordan Second")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)

	state := e.dialog(t, b, false).Query().Get("state")
	loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {state}})
	if loc != appReturn+"?status=error&message="+strings.ReplaceAll(url.QueryEscape(facebook.MsgLinkedElsewhere), "+", "%20") {
		t.Fatalf("second account: %s", loc)
	}
	if e.hasAccount(t, b.UserID) || e.account(t, a.UserID).AccessTokenEnc == "" {
		t.Fatal("a live link moved to another account")
	}

	// Once Facebook deauthorized the first link, a new sign-in takes it over
	// and what was imported for the first account goes with it.
	e.signed(t, "/integrations/facebook/deauthorize", signFor(t, "fb-jordan")).Expect(t, http.StatusOK)
	e.connect(t, b, "fb-jordan")
	if e.hasAccount(t, a.UserID) || e.hasImport(t, a.UserID) || len(e.interests(t, a.UserID)) != 0 {
		t.Fatal("the released link kept its data")
	}
	if e.account(t, b.UserID).FBUserID != "fb-jordan" {
		t.Fatal("link not taken over")
	}
}

func TestRefusedLinkChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	e.fake.AddUser(facebook.FakeUser{ID: "fb-bob", Name: "Bob B."})
	a := e.srv.Signup(t, "Jordan Lee")
	b := e.srv.Signup(t, "Bob B.")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	e.connect(t, b, "fb-bob")
	interests := e.interests(t, a.UserID)

	// Jordan signs in to Facebook as Bob, who is connected to B: refused,
	// and Jordan keeps the link and the import they had.
	state := e.dialog(t, a, false).Query().Get("state")
	loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-bob")}, "state": {state}})
	if !strings.Contains(loc, "status=error") || !strings.Contains(loc, "another%20SideQuests%20account") {
		t.Fatalf("refused link: %s", loc)
	}
	if acct := e.account(t, a.UserID); acct.FBUserID != "fb-jordan" || acct.AccessTokenEnc == "" {
		t.Fatalf("A's link changed: %+v", acct)
	}
	if !e.hasImport(t, a.UserID) || !reflect.DeepEqual(e.interests(t, a.UserID), interests) || len(interests) == 0 {
		t.Fatal("a refused link deleted A's import")
	}
	if e.account(t, b.UserID).FBUserID != "fb-bob" {
		t.Fatal("B lost its link")
	}
}

func TestImportDoesNotUndoADeletion(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	// Facebook's data-deletion request lands while the import reads Facebook.
	e.fake.SetHook(func(method string) {
		if method == "Friends" {
			if err := e.srv.Store.Facebook().Forget(context.Background(), a.UserID); err != nil {
				t.Error(err)
			}
		}
	})
	res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a)
	if res.Status != http.StatusConflict || res.Message() != facebook.MsgConnectFirst {
		t.Fatalf("import racing a deletion: %d %s", res.Status, res.Body)
	}
	if e.hasImport(t, a.UserID) || e.hasAccount(t, a.UserID) || len(e.interests(t, a.UserID)) != 0 {
		t.Fatal("the import brought deleted data back")
	}

	// Same for Facebook's deauthorize: the token is gone, so the new read is
	// not saved over the import the person had.
	e.fake.SetHook(nil)
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	e.fake.SetLikes("fb-jordan", []facebook.Like{like("x", "Just a park", "Park")})
	e.fake.SetHook(func(method string) {
		if method == "Friends" {
			if _, err := e.srv.Store.Facebook().Deauthorize(context.Background(), "fb-jordan"); err != nil {
				t.Error(err)
			}
		}
	})
	res = e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a)
	if res.Status != http.StatusConflict || res.Message() != facebook.MsgConnectFirst {
		t.Fatalf("import racing a deauthorize: %d %s", res.Status, res.Body)
	}
	if stored, err := e.srv.Store.Facebook().Import(context.Background(), a.UserID); err != nil || stored.LikedPages != 21 {
		t.Fatalf("the import after a deauthorize replaced the stored one: %+v, %v", stored, err)
	}
}

func TestCallbackForAUserWhoIsGone(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	state := e.dialog(t, a, false).Query().Get("state")
	oid, err := bson.ObjectIDFromHex(a.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Store.Collection(store.CollUsers).DeleteOne(context.Background(), bson.M{"_id": oid}); err != nil {
		t.Fatal(err)
	}
	loc := e.callback(t, url.Values{"code": {e.fake.IssueCode("fb-jordan")}, "state": {state}})
	if loc != appReturn+"?status=error&message=Facebook%20didn%27t%20connect.%20Try%20again." {
		t.Fatalf("callback for a deleted user: %s", loc)
	}
	if e.hasAccount(t, a.UserID) || len(e.fake.Calls()) != 0 {
		t.Fatal("a deleted user got a Facebook connection")
	}
}

func TestSwitchingFacebookAccountsDropsTheOldImport(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	e.fake.AddUser(facebook.FakeUser{ID: "fb-other", Name: "Other Profile"})
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)
	e.connect(t, a, "fb-other")
	if e.hasImport(t, a.UserID) || len(e.interests(t, a.UserID)) != 0 {
		t.Fatal("the previous Facebook account's import survived")
	}
	if acct := e.account(t, a.UserID); acct.FBUserID != "fb-other" || acct.Name != "Other Profile" {
		t.Fatalf("account = %+v", acct)
	}
	// Reconnecting the same account keeps its import.
	e.importNow(t, a)
	e.connect(t, a, "fb-other")
	if !e.hasImport(t, a.UserID) {
		t.Fatal("reconnecting the same account dropped the import")
	}
}

func TestTokensNeverLeaveTheServerInPlaintext(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	a := e.srv.Signup(t, "Jordan Lee")
	e.connect(t, a, "fb-jordan")
	bodies := []string{
		string(e.srv.Do(t, "GET", "/integrations/facebook", nil, a).Body),
		string(e.srv.Do(t, "POST", "/integrations/facebook/import", nil, a).Body),
		string(e.srv.Do(t, "GET", "/integrations/facebook", nil, a).Body),
	}
	raw, err := json.Marshal(e.rawAccount(t, a.UserID))
	if err != nil {
		t.Fatal(err)
	}
	tokens := e.fake.Tokens()
	if len(tokens) < 2 {
		t.Fatalf("tokens = %v", tokens)
	}
	for _, token := range tokens {
		for _, body := range append(bodies, string(raw)) {
			if strings.Contains(body, token) {
				t.Fatalf("token %s leaked into %s", token, body)
			}
		}
	}
}

func TestEveryFacebookRouteIsServedHere(t *testing.T) {
	// facebook.Register runs before integrations.Register, whose
	// {provider:google|outlook} routes must never swallow these.
	e := newEnv(t)
	a := e.srv.Signup(t, "Jordan Lee")
	routes := []struct {
		method, path string
		auth         bool
		want         int
	}{
		{"GET", "/integrations/facebook", true, http.StatusOK},
		{"DELETE", "/integrations/facebook", true, http.StatusNoContent},
		{"POST", "/integrations/facebook/connect", true, http.StatusOK},
		{"POST", "/integrations/facebook/import", true, http.StatusConflict},
		{"POST", "/integrations/facebook/deauthorize", false, http.StatusBadRequest},
		{"POST", "/integrations/facebook/data-deletion", false, http.StatusBadRequest},
		{"GET", "/integrations/facebook/deletion-status", false, http.StatusNotFound},
	}
	for _, rt := range routes {
		var sess *testutil.Session
		if rt.auth {
			sess = a
		}
		if res := e.srv.Do(t, rt.method, rt.path, nil, sess); res.Status != rt.want {
			t.Errorf("%s %s answered %d %s, want %d", rt.method, rt.path, res.Status, res.Body, rt.want)
		}
	}
	// The callback always answers with a redirect to the app.
	if res := e.get(t, "/integrations/facebook/callback"); res.StatusCode != http.StatusFound ||
		!strings.HasPrefix(res.Header.Get("Location"), appReturn+"?status=error") {
		t.Errorf("callback answered %d %s", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestScoping(t *testing.T) {
	e := newEnv(t)
	e.fake.AddUser(jordan())
	e.fake.AddUser(facebook.FakeUser{ID: "fb-bob", Name: "Bob B."})
	a := e.srv.Signup(t, "Jordan Lee")
	b := e.srv.Signup(t, "Bob B.")
	e.connect(t, a, "fb-jordan")
	e.importNow(t, a)

	// B sees nothing of A's connection and cannot import with it.
	if conn := e.connection(t, b); conn.Connected || conn.LastImport != nil || conn.Name != nil {
		t.Fatalf("B sees %+v", conn)
	}
	if res := e.srv.Do(t, "POST", "/integrations/facebook/import", nil, b); res.Status != http.StatusConflict {
		t.Fatalf("B imported with A's connection: %d", res.Status)
	}
	// B disconnecting leaves A connected, and A's Facebook untouched.
	e.srv.Do(t, "DELETE", "/integrations/facebook", nil, b).Expect(t, http.StatusNoContent)
	if !e.connection(t, a).Connected || e.fake.Revoked("fb-jordan") || !e.hasImport(t, a.UserID) {
		t.Fatal("B's disconnect touched A")
	}
	// A state belongs to whoever asked for it: B's state links B, not A.
	e.connect(t, b, "fb-bob")
	if e.account(t, b.UserID).FBUserID != "fb-bob" || e.account(t, a.UserID).FBUserID != "fb-jordan" {
		t.Fatal("links crossed")
	}
	// Facebook's callbacks act on the named Facebook user only.
	e.signed(t, "/integrations/facebook/deauthorize", signFor(t, "fb-bob")).Expect(t, http.StatusOK)
	if !e.connection(t, a).Connected || e.connection(t, b).Connected {
		t.Fatal("deauthorizing B affected A")
	}
	var del contract.DataDeletionResponse
	e.signed(t, "/integrations/facebook/data-deletion", signFor(t, "fb-bob")).Expect(t, http.StatusOK).JSON(t, &del)
	if !e.hasImport(t, a.UserID) || !e.hasAccount(t, a.UserID) || e.hasAccount(t, b.UserID) {
		t.Fatal("B's deletion request touched A")
	}
	// A's view of B depends on A: B is not a friend of A.
	if imp := e.importNow(t, a); len(imp.FriendsOnApp) != 0 {
		t.Fatalf("friends_on_app lists people who are not Facebook friends: %+v", imp.FriendsOnApp)
	}
	var seen []string
	for _, acct := range []string{a.UserID, b.UserID} {
		if e.hasAccount(t, acct) {
			seen = append(seen, acct)
		}
	}
	if !slices.Equal(seen, []string{a.UserID}) {
		t.Fatalf("accounts = %v", seen)
	}
}
