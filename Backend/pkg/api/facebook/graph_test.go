package facebook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// graphEmulator answers like graph.facebook.com for one user token and
// rejects any call whose appsecret_proof is missing or wrong, like an app
// with "Require App Secret" on. It never reaches Facebook.
type graphEmulator struct {
	t        *testing.T
	token    string
	likes    int // how many liked Pages the person has
	mu       sync.Mutex
	requests []*http.Request
}

func (g *graphEmulator) fail(w http.ResponseWriter, status, code int, msg string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q,"type":"OAuthException","code":%d,"fbtrace_id":"trace"}}`, msg, code)
}

func (g *graphEmulator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r)
	g.mu.Unlock()
	q := r.URL.Query()
	path, ok := strings.CutPrefix(r.URL.Path, "/v26.0")
	if !ok {
		g.fail(w, 400, 2500, "unknown version")
		return
	}
	if path == "/oauth/access_token" {
		if q.Get("client_id") != "app-1" || q.Get("client_secret") != testSecret {
			g.fail(w, 400, 1, "bad client")
			return
		}
		switch {
		case q.Get("code") == "the-code" && q.Get("redirect_uri") == "https://api.test/integrations/facebook/callback":
			fmt.Fprint(w, `{"access_token":"short-token","token_type":"bearer","expires_in":5400}`)
		case q.Get("grant_type") == "fb_exchange_token" && q.Get("fb_exchange_token") == "short-token":
			fmt.Fprint(w, `{"access_token":"`+g.token+`","token_type":"bearer","expires_in":"5183944"}`)
		default:
			g.fail(w, 400, 100, "This authorization code has been used.")
		}
		return
	}
	if q.Get("access_token") != g.token {
		g.fail(w, 400, 190, "Invalid OAuth access token.")
		return
	}
	if q.Get("appsecret_proof") != Proof(testSecret, g.token) {
		g.fail(w, 400, 100, "Invalid appsecret_proof provided in the API argument")
		return
	}
	switch {
	case path == "/me":
		body := `{"id":"fb-1","name":"Jordan Lee"`
		if strings.Contains(q.Get("fields"), "location") {
			body += `,"location":{"id":"110","name":"Atlanta, Georgia"}`
		}
		fmt.Fprint(w, body+"}")
	case path == "/me/permissions" && r.Method == http.MethodGet:
		fmt.Fprint(w, `{"data":[{"permission":"public_profile","status":"granted"},{"permission":"user_likes","status":"granted"},{"permission":"user_location","status":"declined"}]}`)
	case path == "/me/permissions" && r.Method == http.MethodDelete:
		fmt.Fprint(w, `{"success":true}`)
	case path == "/me/likes":
		g.page(w, q, g.likes, func(i int) string {
			return fmt.Sprintf(`{"id":"p%d","name":"Page %d","category":"Park","category_list":[{"id":"1","name":"Park"},{"id":"2","name":"Hiking Trail"}],"created_time":"2024-05-03T12:00:00+0000"}`, i, i)
		})
	case path == "/me/friends":
		g.page(w, q, 3, func(i int) string { return fmt.Sprintf(`{"id":"f%d","name":"Friend %d"}`, i, i) })
	default:
		g.fail(w, 404, 803, "unknown path "+path)
	}
}

// page serves rows [after, after+limit) with a paging.next URL on a host that
// can never resolve, carrying a stale token and proof: the client must take
// only the cursor from it.
func (g *graphEmulator) page(w http.ResponseWriter, q url.Values, total int, row func(int) string) {
	if q.Get("fields") == "" || q.Get("limit") != "100" {
		g.fail(w, 400, 100, "fields and limit must be kept on every page")
		return
	}
	start, _ := strconv.Atoi(q.Get("after"))
	end := min(total, start+100)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, row(i))
	}
	paging := ""
	if end < total {
		next := url.Values{"access_token": {"stale"}, "appsecret_proof": {"stale"}, "fields": {q.Get("fields")},
			"limit": {"100"}, "after": {strconv.Itoa(end)}}
		paging = fmt.Sprintf(`,"paging":{"cursors":{"after":"%d"},"next":%q}`, end, "https://graph.invalid/v26.0/fb-1/likes?"+next.Encode())
	}
	fmt.Fprintf(w, `{"data":[%s]%s}`, strings.Join(rows, ","), paging)
}

func newEmulated(t *testing.T, likes int) (*Client, *graphEmulator) {
	t.Helper()
	emu := &graphEmulator{t: t, token: "EAAlong-emulated", likes: likes}
	srv := httptest.NewServer(emu)
	t.Cleanup(srv.Close)
	return &Client{AppID: "app-1", AppSecret: testSecret, Version: "v26.0", BaseURL: srv.URL, HTTP: srv.Client()}, emu
}

func TestProofMatchesAnIndependentVector(t *testing.T) {
	// Computed with Python: hmac.new(b"sq-test-app-secret", b"EAAtest-token-123", sha256).hexdigest()
	if got := Proof(testSecret, "EAAtest-token-123"); got != "bb79728436414868e55fb0772f0c4bcd1dd72e204cd4fdefa056ba8d56baf204" {
		t.Fatalf("appsecret_proof = %s", got)
	}
}

func TestClientLoginAndReads(t *testing.T) {
	c, emu := newEmulated(t, 250)
	ctx := context.Background()
	short, err := c.ExchangeCode(ctx, "the-code", "https://api.test/integrations/facebook/callback")
	if err != nil || short.AccessToken != "short-token" || short.ExpiresIn != 90*time.Minute {
		t.Fatalf("ExchangeCode = %+v, %v", short, err)
	}
	long, err := c.LongLived(ctx, short.AccessToken)
	if err != nil || long.AccessToken != emu.token || long.ExpiresIn != 5183944*time.Second {
		t.Fatalf("LongLived = %+v, %v", long, err)
	}
	me, err := c.Me(ctx, long.AccessToken, true)
	if err != nil || me != (Profile{ID: "fb-1", Name: "Jordan Lee", Location: "Atlanta, Georgia"}) {
		t.Fatalf("Me = %+v, %v", me, err)
	}
	if me, _ := c.Me(ctx, long.AccessToken, false); me.Location != "" {
		t.Fatal("location read without asking for it")
	}
	perms, err := c.Permissions(ctx, long.AccessToken)
	if err != nil || len(perms) != 3 || perms[2] != (Permission{Name: "user_location", Status: "declined"}) {
		t.Fatalf("Permissions = %+v, %v", perms, err)
	}

	likes, err := c.Likes(ctx, long.AccessToken, 1000)
	if err != nil || len(likes) != 250 {
		t.Fatalf("Likes = %d, %v", len(likes), err)
	}
	first := likes[0]
	if first.ID != "p0" || first.Category != "Park" || len(first.Categories) != 2 || first.Categories[1] != "Hiking Trail" ||
		first.LikedAt == nil || !first.LikedAt.Equal(time.Date(2024, 5, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("first like = %+v", first)
	}
	if likes[249].ID != "p249" {
		t.Fatalf("last like = %+v", likes[249])
	}
	capped, err := c.Likes(ctx, long.AccessToken, 150)
	if err != nil || len(capped) != 150 {
		t.Fatalf("capped Likes = %d, %v", len(capped), err)
	}
	friends, err := c.Friends(ctx, long.AccessToken, 1000)
	if err != nil || len(friends) != 3 || friends[2] != (Friend{ID: "f2", Name: "Friend 2"}) {
		t.Fatalf("Friends = %+v, %v", friends, err)
	}
	if err := c.Revoke(ctx, long.AccessToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Every call with a user token carried the right proof (the emulator
	// refuses others), and the token endpoints never got one.
	emu.mu.Lock()
	defer emu.mu.Unlock()
	likesCalls := 0
	for _, r := range emu.requests {
		q := r.URL.Query()
		if strings.HasSuffix(r.URL.Path, "/oauth/access_token") {
			if q.Has("appsecret_proof") || q.Has("access_token") {
				t.Errorf("token endpoint got a user token: %s", r.URL.Path)
			}
			continue
		}
		if q.Get("appsecret_proof") != Proof(testSecret, emu.token) {
			t.Errorf("%s %s without a valid appsecret_proof", r.Method, r.URL.Path)
		}
		if r.URL.Path == "/v26.0/me/likes" {
			likesCalls++
		}
	}
	if likesCalls != 3+2 { // 250 Pages = 3 pages; 150 = 2 pages
		t.Errorf("likes requests = %d, want 5", likesCalls)
	}
}

func TestClientErrors(t *testing.T) {
	c, _ := newEmulated(t, 0)
	ctx := context.Background()

	_, err := c.Me(ctx, "revoked-token", false)
	if !TokenRejected(err) {
		t.Fatalf("190 not recognized: %v", err)
	}
	var ge *GraphError
	if !errors.As(err, &ge) || ge.HTTPStatus != 400 || ge.TraceID != "trace" {
		t.Fatalf("GraphError = %+v", ge)
	}
	if _, err := c.ExchangeCode(ctx, "used-code", "https://api.test/integrations/facebook/callback"); err == nil || TokenRejected(err) {
		t.Fatalf("used code: %v", err)
	}

	for code, check := range map[int]func(error) bool{17: RateLimited, 4: RateLimited, 613: RateLimited, 10: PermissionMissing, 200: PermissionMissing, 102: TokenRejected} {
		err := &GraphError{Code: code}
		if !check(err) {
			t.Errorf("code %d misclassified", code)
		}
	}
	if TokenRejected(&GraphError{Code: 100}) || RateLimited(&GraphError{Code: 190}) || PermissionMissing(fmt.Errorf("plain")) {
		t.Error("classification too broad")
	}

	// A non-JSON error page still fails the call.
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>bad gateway</html>")
	}))
	defer html.Close()
	bad := &Client{AppID: "app-1", AppSecret: testSecret, Version: "v26.0", BaseURL: html.URL, HTTP: html.Client()}
	if _, err := bad.Permissions(ctx, "tok"); !errors.As(err, &ge) || ge.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("HTML 502: %v", err)
	}
}

func TestClientErrorsNeverCarrySecrets(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	base := down.URL
	down.Close() // every request now fails in transport
	c := &Client{AppID: "app-1", AppSecret: testSecret, Version: "v26.0", BaseURL: base, HTTP: &http.Client{Timeout: time.Second}}
	ctx := context.Background()
	const token = "EAAsecret-user-token"
	_, errCode := c.ExchangeCode(ctx, "secret-code", "https://api.test/cb")
	_, errMe := c.Me(ctx, token, true)
	errRevoke := c.Revoke(ctx, token)
	for _, err := range []error{errCode, errMe, errRevoke} {
		if err == nil {
			t.Fatal("call to a closed server succeeded")
		}
		msg := err.Error()
		for _, secret := range []string{testSecret, token, "secret-code", Proof(testSecret, token)} {
			if strings.Contains(msg, secret) || strings.Contains(msg, url.QueryEscape(secret)) {
				t.Errorf("error leaks a secret: %s", msg)
			}
		}
	}
	if !strings.Contains(errMe.Error(), "GET /me") {
		t.Errorf("error should still name the call: %v", errMe)
	}
}
