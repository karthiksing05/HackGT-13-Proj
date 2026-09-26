package testutil

import (
	"Backend/pkg/api"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/realtime"
	"Backend/pkg/router"
	"Backend/pkg/store"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// quietLogs silences the server log during tests unless SQ_TEST_LOG=1.
var quietLogs sync.Once

// TimeZone is the X-Time-Zone every helper request carries.
const TimeZone = "America/New_York"

// Clock is a frozen, settable clock shared by the server, store and JWTs.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func NewClock(t time.Time) *Clock { return &Clock{t: t} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Server is the whole API behind httptest with a real hub on /ws and a
// Recorder in front of it, a fresh database and a frozen clock.
type Server struct {
	HTTP   *httptest.Server
	Deps   *api.Deps
	Store  *store.Store
	Cfg    *config.Config
	Hub    *realtime.Hub      // serves /ws
	Events *realtime.Recorder // every event handlers emitted (also forwarded to Hub)
	Clock  *Clock
}

type serverOptions struct {
	cfg     func(*config.Config)
	planner api.Planner
	now     time.Time
}

// Option customizes New.
type Option func(*serverOptions)

// WithConfig edits the test config before validation.
func WithConfig(fn func(*config.Config)) Option { return func(o *serverOptions) { o.cfg = fn } }

// WithPlanner wires a planner (nil = 503 on /plans/*).
func WithPlanner(p api.Planner) Option { return func(o *serverOptions) { o.planner = p } }

// WithNow pins the clock. The default is the real time at start, frozen;
// keep pinned constants near the present because TTL indexes delete by
// wall clock.
func WithNow(t time.Time) Option { return func(o *serverOptions) { o.now = t } }

// New builds a server; everything is torn down with the test.
func New(t testing.TB, opts ...Option) *Server {
	t.Helper()
	quietLogs.Do(func() {
		if os.Getenv("SQ_TEST_LOG") != "1" {
			log.Logger = zerolog.Nop()
		}
	})
	o := serverOptions{now: time.Now().UTC().Truncate(time.Second)}
	for _, opt := range opts {
		opt(&o)
	}
	clock := NewClock(o.now)
	db := DB(t)
	cfg := &config.Config{
		AppEnv:            "dev",
		HTTPAddr:          "127.0.0.1:0",
		PublicBaseURL:     "http://api.test",
		MongoURI:          MongoURI(),
		MongoDB:           db.Name(),
		JWTSecret:         "test-secret-test-secret-test-secret-0000",
		AccessTokenTTL:    time.Hour,
		RefreshTokenTTL:   720 * time.Hour,
		MLServiceURL:      "http://127.0.0.1:1",
		MLRerankTopK:      12,
		Planner:           "dag",
		FBGraphVersion:    "v26.0",
		DemoTZ:            TimeZone,
		DevResetCodes:     true,
		CheckoutStepDelay: 0,
		MaxPhotoBytes:     2 << 20,
		MaxJSONBytes:      1 << 20,
		DisableRateLimits: true,
	}
	if o.cfg != nil {
		o.cfg(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config: %v", err)
	}
	s := store.New(db, clock.Now)
	if err := s.EnsureIndexes(t.Context()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	deps := &api.Deps{Store: s, Cfg: cfg, Now: clock.Now, Planner: o.planner}
	hub := realtime.NewHub(deps.Auth().UserFromToken)
	rec := realtime.NewRecorder(hub)
	deps.Hub = rec
	srv := httptest.NewServer(router.New(deps, hub))
	t.Cleanup(func() {
		srv.Close()
		hub.Close()
	})
	return &Server{HTTP: srv, Deps: deps, Store: s, Cfg: cfg, Hub: hub, Events: rec, Clock: clock}
}

// URL is the absolute URL of an API path.
func (s *Server) URL(path string) string { return s.HTTP.URL + path }

// WSURL is the websocket URL of /ws.
func (s *Server) WSURL() string { return "ws" + strings.TrimPrefix(s.HTTP.URL, "http") + "/ws" }

// Session is a signed-in test user.
type Session struct {
	UserID   string
	Access   string
	Refresh  string
	Email    string
	Password string
	User     contract.User
}

// Response is a completed request.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v, failing the test on a decode error.
func (r *Response) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %d response %q: %v", r.Status, r.Body, err)
	}
}

// Message is the {message} of an error body ("" otherwise).
func (r *Response) Message() string {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(r.Body, &body)
	return body.Message
}

// Do sends a JSON request (body nil = no body) with X-Time-Zone and, when
// sess is set, its bearer token.
func (s *Server) Do(t testing.TB, method, path string, body any, sess *Session) *Response {
	t.Helper()
	var reader io.Reader
	headers := map[string]string{}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
		headers["Content-Type"] = "application/json"
	}
	if sess != nil {
		headers["Authorization"] = "Bearer " + sess.Access
	}
	return s.DoRaw(t, method, path, reader, headers)
}

// DoRaw sends any body (multipart, empty) with X-Time-Zone plus headers.
func (s *Server) DoRaw(t testing.TB, method, path string, body io.Reader, headers map[string]string) *Response {
	t.Helper()
	req, err := http.NewRequest(method, s.URL(path), body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Time-Zone", TimeZone)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := s.HTTP.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: raw}
}

// Expect fails unless the response has the wanted status.
func (r *Response) Expect(t testing.TB, status int) *Response {
	t.Helper()
	if r.Status != status {
		t.Fatalf("expected %d, got %d: %s", status, r.Status, r.Body)
	}
	return r
}

// Signup creates a user with a unique email and the fixture password.
func (s *Server) Signup(t testing.TB, name string) *Session {
	t.Helper()
	return s.SignupWith(t, SignupRequest(name))
}

// SignupWith creates a user from a full request.
func (s *Server) SignupWith(t testing.TB, req contract.SignupRequest) *Session {
	t.Helper()
	var auth contract.AuthResponse
	s.Do(t, "POST", "/auth/signup", req, nil).Expect(t, http.StatusCreated).JSON(t, &auth)
	return &Session{UserID: auth.User.ID, Access: auth.Tokens.AccessToken, Refresh: auth.Tokens.RefreshToken,
		Email: req.Email, Password: req.Password, User: auth.User}
}

// Login signs an existing user in.
func (s *Server) Login(t testing.TB, email, password string) *Session {
	t.Helper()
	var auth contract.AuthResponse
	s.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: email, Password: password}, nil).Expect(t, http.StatusOK).JSON(t, &auth)
	return &Session{UserID: auth.User.ID, Access: auth.Tokens.AccessToken, Refresh: auth.Tokens.RefreshToken,
		Email: email, Password: password, User: auth.User}
}

// Bearer is the Authorization header value of a session.
func (sess *Session) Bearer() string { return fmt.Sprintf("Bearer %s", sess.Access) }
