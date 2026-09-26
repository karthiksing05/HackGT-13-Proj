package facebook

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// FakeGraph is an in-memory Graph for tests: nothing leaves the process.
// Codes and tokens map to fake Facebook users; a user whose tokens were
// expired answers every call with error 190, like Facebook does after a
// password change or when the app is removed there.
type FakeGraph struct {
	mu        sync.Mutex
	seq       int
	users     map[string]*FakeUser // by Facebook id
	codes     map[string]string    // unused code → user id
	tokens    map[string]string    // token → user id
	expired   map[string]bool      // user id → every token rejected
	revoked   map[string]bool      // user id → Revoke was called
	failures  map[string]error     // method → error to answer with
	calls     []string
	redirects []string            // redirect_uri of every code exchange
	hook      func(method string) // runs as each call starts (SetHook)
}

// FakeUser is what the fake Graph knows about one person.
type FakeUser struct {
	ID          string
	Name        string
	Location    string            // "" = no city
	Permissions map[string]string // scope → granted | declined; nil = all granted
	Likes       []Like
	Friends     []Friend
}

// NewFakeGraph returns an empty fake.
func NewFakeGraph() *FakeGraph {
	return &FakeGraph{
		users: map[string]*FakeUser{}, codes: map[string]string{}, tokens: map[string]string{},
		expired: map[string]bool{}, revoked: map[string]bool{}, failures: map[string]error{},
	}
}

// AddUser registers (or replaces) a person.
func (f *FakeGraph) AddUser(u FakeUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.ID] = &u
	delete(f.expired, u.ID)
}

// SetLikes replaces a person's liked Pages.
func (f *FakeGraph) SetLikes(fbUserID string, likes []Like) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u := f.users[fbUserID]; u != nil {
		u.Likes = likes
	}
}

// IssueCode is the code the Login dialog would hand the callback after the
// person signs in and agrees.
func (f *FakeGraph) IssueCode(fbUserID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	code := fmt.Sprintf("code-%d-%s", f.seq, fbUserID)
	f.codes[code] = fbUserID
	return code
}

// ExpireTokens makes Facebook reject every token of the person (error 190)
// until AddUser registers them again.
func (f *FakeGraph) ExpireTokens(fbUserID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired[fbUserID] = true
}

// Fail makes every call of method ("ExchangeCode", "Likes", …) answer err;
// nil clears it.
func (f *FakeGraph) Fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.failures, method)
		return
	}
	f.failures[method] = err
}

// SetHook runs fn with the method name as every call starts, e.g. to make
// something happen while an import is reading Facebook. fn runs under the
// fake's lock and must not call the fake.
func (f *FakeGraph) SetHook(fn func(method string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hook = fn
}

// Revoked reports whether Revoke ran for the person.
func (f *FakeGraph) Revoked(fbUserID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[fbUserID]
}

// Calls lists the methods called so far, in order.
func (f *FakeGraph) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Redirects lists the redirect_uri of every code exchange.
func (f *FakeGraph) Redirects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.redirects)
}

// Tokens lists every token handed out, so tests can check none leaks.
func (f *FakeGraph) Tokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.tokens))
	for token := range f.tokens {
		out = append(out, token)
	}
	return out
}

// TokenError is the answer Facebook gives for an expired or revoked token.
func TokenError() *GraphError {
	return &GraphError{HTTPStatus: 400, Code: 190, Subcode: 463, Type: "OAuthException",
		Message: "Error validating access token: Session has expired."}
}

// begin records a call and returns the configured failure, if any.
func (f *FakeGraph) begin(method string) error {
	f.calls = append(f.calls, method)
	if f.hook != nil {
		f.hook(method)
	}
	return f.failures[method]
}

// user resolves a token (caller holds mu).
func (f *FakeGraph) user(token string) (*FakeUser, error) {
	id, ok := f.tokens[token]
	if !ok || f.expired[id] || f.users[id] == nil {
		return nil, TokenError()
	}
	return f.users[id], nil
}

func (f *FakeGraph) issue(fbUserID, prefix string, ttl time.Duration) Token {
	f.seq++
	token := fmt.Sprintf("%s-%d-%s", prefix, f.seq, fbUserID)
	f.tokens[token] = fbUserID
	return Token{AccessToken: token, ExpiresIn: ttl}
}

func (f *FakeGraph) ExchangeCode(_ context.Context, code, redirectURI string) (Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("ExchangeCode"); err != nil {
		return Token{}, err
	}
	f.redirects = append(f.redirects, redirectURI)
	id, ok := f.codes[code]
	if !ok {
		return Token{}, &GraphError{HTTPStatus: 400, Code: 100, Type: "OAuthException",
			Message: "This authorization code has been used."}
	}
	delete(f.codes, code)
	return f.issue(id, "EAAshort", 2*time.Hour), nil
}

func (f *FakeGraph) LongLived(_ context.Context, token string) (Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("LongLived"); err != nil {
		return Token{}, err
	}
	u, err := f.user(token)
	if err != nil {
		return Token{}, err
	}
	return f.issue(u.ID, "EAAlong", 60*24*time.Hour), nil
}

func (f *FakeGraph) Me(_ context.Context, token string, withLocation bool) (Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("Me"); err != nil {
		return Profile{}, err
	}
	u, err := f.user(token)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{ID: u.ID, Name: u.Name}
	if withLocation && u.granted("user_location") {
		p.Location = u.Location
	}
	return p, nil
}

func (f *FakeGraph) Permissions(_ context.Context, token string) ([]Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("Permissions"); err != nil {
		return nil, err
	}
	u, err := f.user(token)
	if err != nil {
		return nil, err
	}
	out := []Permission{}
	for _, scope := range requestedScopes {
		status := "granted"
		if !u.granted(scope) {
			status = "declined"
		}
		out = append(out, Permission{Name: scope, Status: status})
	}
	return out, nil
}

func (f *FakeGraph) Likes(_ context.Context, token string, limit int) ([]Like, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("Likes"); err != nil {
		return nil, err
	}
	u, err := f.user(token)
	if err != nil {
		return nil, err
	}
	if !u.granted("user_likes") {
		return nil, &GraphError{HTTPStatus: 403, Code: 200, Type: "OAuthException", Message: "(#200) Requires user_likes permission"}
	}
	return slices.Clone(u.Likes[:min(limit, len(u.Likes))]), nil
}

func (f *FakeGraph) Friends(_ context.Context, token string, limit int) ([]Friend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("Friends"); err != nil {
		return nil, err
	}
	u, err := f.user(token)
	if err != nil {
		return nil, err
	}
	if !u.granted("user_friends") {
		return nil, &GraphError{HTTPStatus: 403, Code: 200, Type: "OAuthException", Message: "(#200) Requires user_friends permission"}
	}
	return slices.Clone(u.Friends[:min(limit, len(u.Friends))]), nil
}

func (f *FakeGraph) Revoke(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.begin("Revoke"); err != nil {
		return err
	}
	u, err := f.user(token)
	if err != nil {
		return err
	}
	f.revoked[u.ID] = true
	f.expired[u.ID] = true
	return nil
}

// granted reports whether the person granted scope (all are, by default).
func (u *FakeUser) granted(scope string) bool {
	if u.Permissions == nil {
		return true
	}
	status, ok := u.Permissions[scope]
	return !ok || status == "granted"
}
