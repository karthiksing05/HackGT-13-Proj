//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---- configuration -------------------------------------------------------------

type suiteConfig struct {
	BaseURL      string
	WSURL        string
	DemoEmail    string
	DemoPassword string
	TimeZone     string
	Loc          *time.Location
}

var (
	confOnce sync.Once
	conf     suiteConfig
	confErr  error
)

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// config reads the E2E_* settings; without E2E_BASE_URL the test skips.
func config(t testing.TB) suiteConfig {
	t.Helper()
	confOnce.Do(func() {
		conf.BaseURL = strings.TrimRight(envOr("E2E_BASE_URL", ""), "/")
		conf.DemoEmail = envOr("E2E_DEMO_EMAIL", "demo@sidequestz.tech")
		conf.DemoPassword = os.Getenv("E2E_DEMO_PASSWORD")
		conf.TimeZone = envOr("E2E_TIME_ZONE", "America/New_York")
		conf.Loc, confErr = time.LoadLocation(conf.TimeZone)
		ws := strings.Replace(conf.BaseURL, "https://", "wss://", 1)
		ws = strings.Replace(ws, "http://", "ws://", 1)
		conf.WSURL = envOr("E2E_WS_URL", ws+"/ws")
	})
	if conf.BaseURL == "" {
		t.Skip("set E2E_BASE_URL (and E2E_DEMO_PASSWORD) to run the end-to-end suite")
	}
	if confErr != nil {
		t.Fatalf("E2E_TIME_ZONE: %v", confErr)
	}
	return conf
}

// ---- HTTP ------------------------------------------------------------------------

// httpClient never follows redirects: the hosted pages answer 302 to
// sidequestz:// links, which the tests assert instead of following.
var httpClient = &http.Client{
	Timeout:       90 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Session is a signed-in account.
type Session struct {
	Label    string
	UserID   string
	Name     string
	Email    string
	Password string
	Access   string
	Refresh  string
	User     contract.User
}

// Response is one finished request.
type Response struct {
	Method string
	Path   string
	Status int
	Header http.Header
	Body   []byte
}

// form is a multipart body (photo uploads).
type form struct {
	body        []byte
	contentType string
}

// rawJSON is a body sent verbatim.
type rawJSON string

// do sends a request as s (nil: anonymous) with the headers the app sends.
// A 429 on /auth/* (the per-IP and per-email limits) is waited out.
func do(t testing.TB, s *Session, method, path string, body any) *Response {
	t.Helper()
	return doWith(t, s, method, path, body, nil)
}

func doWith(t testing.TB, s *Session, method, path string, body any, headers map[string]string) *Response {
	t.Helper()
	c := config(t)
	var data []byte
	contentType := ""
	switch b := body.(type) {
	case nil:
	case rawJSON:
		data, contentType = []byte(b), "application/json"
	case *form:
		data, contentType = b.body, b.contentType
	case url.Values:
		data, contentType = []byte(b.Encode()), "application/x-www-form-urlencoded"
	default:
		var err error
		if data, err = json.Marshal(b); err != nil {
			t.Fatalf("%s %s: encode body: %v", method, path, err)
		}
		contentType = "application/json"
	}
	target := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		target = c.BaseURL + path
	}
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if data != nil {
			reader = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, target, reader)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		req.Header.Set("X-Time-Zone", c.TimeZone)
		req.Header.Set("Accept", "application/json")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if s != nil && s.Access != "" {
			req.Header.Set("Authorization", "Bearer "+s.Access)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatalf("%s %s: read body: %v", method, path, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && strings.HasPrefix(path, "/auth/") && attempt < 15 {
			t.Logf("%s %s: rate limited, waiting", method, path)
			time.Sleep(6 * time.Second)
			continue
		}
		return &Response{Method: method, Path: path, Status: res.StatusCode, Header: res.Header, Body: raw}
	}
}

func (r *Response) String() string {
	body := string(r.Body)
	if len(body) > 600 {
		body = body[:600] + "…"
	}
	return fmt.Sprintf("%s %s → %d %s", r.Method, r.Path, r.Status, body)
}

// Expect fails the test unless the status is one of want.
func (r *Response) Expect(t testing.TB, want ...int) *Response {
	t.Helper()
	if !slices.Contains(want, r.Status) {
		t.Fatalf("want %v: %s", want, r)
	}
	return r
}

// offsetTime finds timestamps written with a zone offset instead of Z.
var offsetTime = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?[+-]\d{2}:\d{2}"`)

// JSON decodes the body strictly into v (unknown keys and trailing data
// fail), then checks every contract enum in it and that times are UTC.
func (r *Response) JSON(t testing.TB, v any) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s: content type %q, want application/json", r, ct)
	}
	if err := strictDecode(r.Body, v); err != nil {
		t.Fatalf("%s: strict decode into %T: %v", r, v, err)
	}
	if m := offsetTime.Find(r.Body); m != nil {
		t.Errorf("%s %s: time %s is not UTC (the contract writes …Z)", r.Method, r.Path, m)
	}
	checkEnums(t, r.Method+" "+r.Path, reflect.ValueOf(v), false)
}

// NoContent checks a 204 with an empty body.
func (r *Response) NoContent(t testing.TB) {
	t.Helper()
	r.Expect(t, http.StatusNoContent)
	if len(bytes.TrimSpace(r.Body)) != 0 {
		t.Errorf("%s: 204 with a body", r)
	}
}

// apiError is every non-2xx body: {"error": "<code>", "message": "<sentence>"}.
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Fail decodes the error body and returns its sentence.
func (r *Response) Fail(t testing.TB) string {
	t.Helper()
	var e apiError
	if err := strictDecode(r.Body, &e); err != nil {
		t.Fatalf("%s: error body: %v", r, err)
	}
	if e.Error == "" || e.Message == "" {
		t.Errorf("%s: error body without code or message", r)
	}
	if code := strings.ToLower(strings.ReplaceAll(http.StatusText(r.Status), " ", "_")); e.Error != code {
		t.Errorf("%s: error code %q, want %q", r, e.Error, code)
	}
	return e.Message
}

func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

// get is GET path → 200 decoded into T.
func get[T any](t testing.TB, s *Session, path string) T {
	t.Helper()
	var v T
	do(t, s, "GET", path, nil).Expect(t, http.StatusOK).JSON(t, &v)
	return v
}

// send is method path body → status decoded into T.
func send[T any](t testing.TB, s *Session, method, path string, body any, status int) T {
	t.Helper()
	var v T
	do(t, s, method, path, body).Expect(t, status).JSON(t, &v)
	return v
}

// fails is method path body → status with an error sentence, returned.
func fails(t testing.TB, s *Session, method, path string, body any, status int) string {
	t.Helper()
	return do(t, s, method, path, body).Expect(t, status).Fail(t)
}

// ---- enum and shape checks --------------------------------------------------------

type valider interface{ Valid() bool }

var contractTime = reflect.TypeOf(contract.Time{})

// checkEnums walks a decoded value and asks every contract enum it meets
// whether it is one the app knows. An omitted optional enum (zero value on
// an omitempty field) is fine.
func checkEnums(t testing.TB, where string, v reflect.Value, omitEmpty bool) {
	t.Helper()
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			checkEnums(t, where, v.Elem(), false)
		}
		return
	}
	if v.Type().PkgPath() == "Backend/pkg/contract" && v.CanInterface() {
		if e, ok := v.Interface().(valider); ok && !(omitEmpty && v.IsZero()) && !e.Valid() {
			t.Errorf("%s: %v is not a value the app knows (%s)", where, v.Interface(), v.Type().Name())
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == contractTime {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			checkEnums(t, where+"."+name, v.Field(i), strings.Contains(opts, "omitempty"))
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			checkEnums(t, fmt.Sprintf("%s[%d]", where, i), v.Index(i), false)
		}
	}
}

// checkPerson asserts a PersonRef is fully rendered.
func checkPerson(t testing.TB, where string, p contract.PersonRef) {
	t.Helper()
	if p.ID == "" || p.Name == "" || p.Initials == "" || !strings.HasPrefix(p.ColorHex, "#") {
		t.Errorf("%s: incomplete person %+v", where, p)
	}
}

// ---- accounts ---------------------------------------------------------------------

var accountSeq atomic.Int64

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// signup creates a fresh adult account (Atlanta catalog) named "<first> TesterXXXX".
func signup(t testing.TB, first string) *Session {
	t.Helper()
	n := accountSeq.Add(1)
	email := fmt.Sprintf("e2e.%d.%s.%d@example.test", time.Now().Unix(), randomHex(3), n)
	password := "wander-" + randomHex(4) + "9"
	name := first + " Tester" + strings.ToUpper(randomHex(2))
	born := contract.NewTime(time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC))
	var out contract.AuthResponse
	do(t, nil, "POST", "/auth/signup", contract.SignupRequest{Name: name, Email: email, Password: password, DateOfBirth: &born}).
		Expect(t, http.StatusCreated).JSON(t, &out)
	s := &Session{Label: first, UserID: out.User.ID, Name: name, Email: email, Password: password,
		Access: out.Tokens.AccessToken, Refresh: out.Tokens.RefreshToken, User: out.User}
	if s.UserID == "" || s.Access == "" || s.Refresh == "" {
		t.Fatalf("signup %s: missing id or tokens", first)
	}
	return s
}

// loginAs signs in with email and password.
func loginAs(t testing.TB, label, email, password string) *Session {
	t.Helper()
	var out contract.AuthResponse
	do(t, nil, "POST", "/auth/login", contract.LoginRequest{Email: email, Password: password}).Expect(t, http.StatusOK).JSON(t, &out)
	return &Session{Label: label, UserID: out.User.ID, Name: out.User.Name, Email: email, Password: password,
		Access: out.Tokens.AccessToken, Refresh: out.Tokens.RefreshToken, User: out.User}
}

// setupPrefs is a complete Preferences body for fresh accounts (setup's last step).
func setupPrefs() contract.Preferences {
	return contract.Preferences{
		Ratings: contract.Ratings{"outdoors": 5, "food": 4, "live_music": 4, "museums": 3, "nightlife": 2, "big_crowds": 1},
		Company: contract.CompanySmallGroup, Pace: contract.PaceBalanced, Spend: contract.SpendUnder15,
		Flexibility: contract.FlexBitOverOK, SplitStyle: contract.SplitEqually, PreferFree: true,
		Answers: contract.Answers{
			"perfect_afternoon": "A walk in a park, a food hall and a small concert.",
			"never_do":          "Packed clubs.",
			"plan_around":       "Classes and the weekend market.",
		},
		InstantCheckoutLimitCents: 5000,
	}
}

var (
	sandyMu   sync.Mutex
	sandyOnce *Session

	pairMu     sync.Mutex
	pairA      *Session
	pairB      *Session
	pairFailed bool
)

// sandy is the demo account, signed in once per run.
func sandy(t testing.TB) *Session {
	t.Helper()
	c := config(t)
	if c.DemoPassword == "" {
		t.Skip("set E2E_DEMO_PASSWORD to run the demo account's tests")
	}
	sandyMu.Lock()
	defer sandyMu.Unlock()
	if sandyOnce == nil {
		sandyOnce = loginAs(t, "Sandy", c.DemoEmail, c.DemoPassword)
	}
	return sandyOnce
}

// people are the two fresh accounts the social tests share (Alice and
// Bob), each through setup (preferences saved).
func people(t testing.TB) (*Session, *Session) {
	t.Helper()
	config(t)
	pairMu.Lock()
	defer pairMu.Unlock()
	if pairFailed {
		t.Fatal("the shared accounts could not be created earlier in this run")
	}
	if pairA == nil {
		pairFailed = true
		a, b := signup(t, "Alice"), signup(t, "Bob")
		for _, s := range []*Session{a, b} {
			do(t, s, "PUT", "/me/preferences", setupPrefs()).Expect(t, http.StatusOK)
		}
		pairA, pairB, pairFailed = a, b, false
	}
	return pairA, pairB
}

// ---- time -----------------------------------------------------------------------

// localDay is midnight of t's day in the suite's time zone.
func localDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// at is hh:mm on day (a local midnight).
func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}

// demoToday is the demo account's "today": /me's demo_date when the server
// runs the demo on a fixed date, else the real date in the suite's zone.
func demoToday(t testing.TB, s *Session) time.Time {
	t.Helper()
	c := config(t)
	res := do(t, s, "GET", "/me", nil).Expect(t, http.StatusOK)
	var loose map[string]any
	if err := json.Unmarshal(res.Body, &loose); err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	if day, ok := loose["demo_date"].(string); ok && day != "" {
		parsed, err := contract.ParseTime(day)
		if err != nil {
			t.Fatalf("GET /me demo_date %q: %v", day, err)
		}
		y, m, d := parsed.Date()
		if len(day) > 10 {
			y, m, d = parsed.In(c.Loc).Date()
		}
		return time.Date(y, m, d, 0, 0, 0, 0, c.Loc)
	}
	return localDay(time.Now(), c.Loc)
}

// businessNow is now as the server lives it for s: the real time of day on
// the account's demo date (the real now without one). Times the app sends
// for "now"-relative things (a free-now post's until) are in this clock.
func businessNow(t testing.TB, s *Session) time.Time {
	t.Helper()
	c := config(t)
	day := demoToday(t, s)
	now := time.Now().In(c.Loc)
	return time.Date(day.Year(), day.Month(), day.Day(), now.Hour(), now.Minute(), now.Second(), 0, c.Loc)
}

// waitFor polls fn until it reports done or the timeout passes.
func waitFor(t testing.TB, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ---- files ------------------------------------------------------------------------

// jpegBytes is a small real JPEG in the given color.
func jpegBytes(t testing.TB, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// photoForm is a multipart body with one file field.
func photoForm(t testing.TB, field string, data []byte) *form {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &form{body: buf.Bytes(), contentType: w.FormDataContentType()}
}

// ---- websocket --------------------------------------------------------------------

type wsEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// WS is one realtime connection; received events are kept so a test can
// expect them in any order.
type WS struct {
	label  string
	conn   *websocket.Conn
	mu     sync.Mutex
	events []wsEvent
	used   []bool
	signal chan struct{}
	done   chan struct{}
	err    error
}

// dial opens /ws as s with the bearer header and waits for the hello.
func dial(t testing.TB, s *Session) *WS {
	t.Helper()
	c := config(t)
	header := http.Header{"Authorization": {"Bearer " + s.Access}, "X-Time-Zone": {c.TimeZone}}
	conn, res, err := websocket.DefaultDialer.Dial(c.WSURL, header)
	if err != nil {
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		t.Fatalf("dial %s as %s: %v (status %d)", c.WSURL, s.Label, err, status)
	}
	w := &WS{label: s.Label, conn: conn, signal: make(chan struct{}, 1), done: make(chan struct{})}
	go w.read()
	t.Cleanup(w.Close)
	var hello struct{}
	expectEvent(t, w, "connected", 10*time.Second, &hello, nil)
	return w
}

func (w *WS) read() {
	defer close(w.done)
	for {
		_, data, err := w.conn.ReadMessage()
		if err != nil {
			w.mu.Lock()
			w.err = err
			w.mu.Unlock()
			return
		}
		var ev wsEvent
		if err := strictDecode(data, &ev); err != nil {
			ev = wsEvent{Type: "!malformed", Data: data}
		}
		w.mu.Lock()
		w.events = append(w.events, ev)
		w.used = append(w.used, false)
		w.mu.Unlock()
		select {
		case w.signal <- struct{}{}:
		default:
		}
	}
}

// Close ends the connection.
func (w *WS) Close() { w.conn.Close() }

// take finds and consumes the first unused event of typ that match accepts.
func (w *WS) take(typ string, match func(json.RawMessage) bool) (json.RawMessage, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, ev := range w.events {
		if w.used[i] || ev.Type != typ {
			continue
		}
		if match == nil || match(ev.Data) {
			w.used[i] = true
			return ev.Data, true
		}
	}
	return nil, false
}

func (w *WS) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.events))
	for i, ev := range w.events {
		out[i] = ev.Type
	}
	return out
}

// expectEvent waits for an event of typ whose data decodes strictly into out
// and satisfies match (nil: any).
func expectEvent[T any](t testing.TB, w *WS, typ string, timeout time.Duration, out *T, match func(T) bool) {
	t.Helper()
	var decodeErr error
	accept := func(raw json.RawMessage) bool {
		var v T
		if err := strictDecode(raw, &v); err != nil {
			decodeErr = fmt.Errorf("%s data %s: %w", typ, raw, err)
			return false
		}
		if match != nil && !match(v) {
			return false
		}
		*out = v
		return true
	}
	deadline := time.After(timeout)
	for {
		if raw, ok := w.take(typ, accept); ok {
			checkEnums(t, w.label+" ws "+typ, reflect.ValueOf(out), false)
			if m := offsetTime.Find(raw); m != nil {
				t.Errorf("ws %s: time %s is not UTC", typ, m)
			}
			return
		}
		select {
		case <-w.signal:
		case <-w.done:
			if _, ok := w.take(typ, accept); ok {
				return
			}
			t.Fatalf("%s's socket closed (%v) before %s arrived; got %v", w.label, w.err, typ, w.seen())
		case <-deadline:
			if decodeErr != nil {
				t.Fatalf("%s: %v", w.label, decodeErr)
			}
			t.Fatalf("%s never got %s within %s; got %v", w.label, typ, timeout, w.seen())
		}
	}
}

// expectNoEvent asserts no unused event of typ that match accepts arrives
// within d.
func expectNoEvent(t testing.TB, w *WS, typ string, d time.Duration, match func(json.RawMessage) bool) {
	t.Helper()
	deadline := time.After(d)
	for {
		if raw, ok := w.take(typ, match); ok {
			t.Errorf("%s got an unexpected %s: %s", w.label, typ, raw)
			return
		}
		select {
		case <-w.signal:
		case <-w.done:
			return
		case <-deadline:
			return
		}
	}
}

// has reports whether raw JSON contains the quoted string s.
func has(s string) func(json.RawMessage) bool {
	quoted, _ := json.Marshal(s)
	return func(raw json.RawMessage) bool { return bytes.Contains(raw, quoted) }
}
