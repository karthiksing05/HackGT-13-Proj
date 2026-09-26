package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"a","extra":1}`))
	if !Decode(w, r, &body) || body.Name != "a" {
		t.Fatalf("unknown keys must be allowed: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":`))
	if Decode(w, r, &body) || w.Code != 400 {
		t.Fatalf("syntax error not a 400: %d", w.Code)
	}
	var eb ErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil || eb.Error != "bad_request" || eb.Message != GenericBadRequest {
		t.Fatalf("bad error body: %s", w.Body)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/", strings.NewReader(""))
	if Decode(w, r, &body) || w.Code != 400 {
		t.Fatalf("empty body must be a 400 for Decode: %d", w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/", strings.NewReader(""))
	if !DecodeOptional(w, r, &body) {
		t.Fatalf("empty body must pass DecodeOptional: %d", w.Code)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"`+strings.Repeat("x", MaxJSONBytes+10)+`"}`))
	if Decode(w, r, &body) || w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body not a 413: %d", w.Code)
	}
}

func TestFailMapsErrors(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	Fail(w, r, Wrap(http.StatusConflict, "The balance changed.", errors.New("cause")))
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"conflict"`) || !strings.Contains(w.Body.String(), "The balance changed.") {
		t.Fatalf("StatusError not mapped: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	Fail(w, r, errors.New("boom"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("plain error must be an opaque 500: %d %s", w.Code, w.Body)
	}
	if Code(http.StatusNotImplemented) != "not_implemented" || Code(http.StatusInternalServerError) != "internal_server_error" {
		t.Fatal("Code wrong")
	}
}

func TestTZ(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if TZ(r).String() != DefaultTimeZone {
		t.Fatalf("default zone wrong: %s", TZ(r))
	}
	r.Header.Set("X-Time-Zone", "Europe/Berlin")
	if TZ(r).String() != "Europe/Berlin" {
		t.Fatalf("header ignored: %s", TZ(r))
	}
	r.Header.Set("X-Time-Zone", "Mars/Olympus")
	if TZ(r).String() != DefaultTimeZone {
		t.Fatalf("unknown zone must fall back: %s", TZ(r))
	}
	ny := Location("America/New_York")
	at := time.Date(2026, 9, 26, 3, 30, 0, 0, time.UTC) // 23:30 the day before in New York
	if DayKey(at, ny) != "2026-09-25" || LocalMidnight(at, ny).UTC().Format(time.RFC3339) != "2026-09-25T04:00:00Z" {
		t.Fatalf("day helpers wrong: %s %s", DayKey(at, ny), LocalMidnight(at, ny))
	}
}

func TestCursors(t *testing.T) {
	c := EncodeCursor("m-42|2026")
	if got, ok := DecodeCursor(c); !ok || got != "m-42|2026" {
		t.Fatalf("round trip: %q %v", got, ok)
	}
	if _, ok := DecodeCursor("***"); ok {
		t.Fatal("garbage accepted")
	}
	if NextCursor("") != nil || *NextCursor("x") != EncodeCursor("x") {
		t.Fatal("NextCursor wrong")
	}
	r := httptest.NewRequest("GET", "/?limit=500", nil)
	if Limit(r, 50, 100) != 100 {
		t.Fatal("limit not clamped")
	}
}

func TestFormatting(t *testing.T) {
	ny := Location("America/New_York")
	at := func(h, m int) time.Time { return time.Date(2026, 9, 26, h, m, 0, 0, ny) }
	cases := []struct{ got, want string }{
		{Clock(at(17, 12)), "5:12 PM"},
		{Clock(at(17, 0)), "5:00 PM"},
		{ClockShort(at(18, 0)), "6 PM"},
		{ClockShort(at(18, 30)), "6:30 PM"},
		{TimeRange(at(17, 30), at(20, 0)), "5:30–8 PM"},
		{TimeRange(at(6, 0), at(10, 0)), "6–10 AM"},
		{TimeRange(at(11, 0), at(13, 0)), "11 AM–1 PM"},
		{MonthDay(at(0, 0)), "Sep 26"},
		{Weekday(at(0, 0)), "Sat"},
		{DayLabel(at(9, 0), at(20, 0)), "Today"},
		{DayLabel(at(9, 0).AddDate(0, 0, 1), at(20, 0)), "Tomorrow"},
		{DayLabel(at(9, 0).AddDate(0, 0, 3), at(20, 0)), "Tue"},
		{DayLabel(at(9, 0).AddDate(0, 0, 9), at(20, 0)), "Oct 5"},
		{Dollars(900), "$9"},
		{Dollars(950), "$9.50"},
		{Dollars(-400), "-$4"},
		{Plural(1, "person", "people"), "1 person"},
		{Plural(3, "person", "people"), "3 people"},
		{Miles(0.4), "0.4 mi"},
		{Miles(2), "2 mi"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(60, 3)
	l.SetClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("burst request %d refused", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th request allowed")
	}
	if !l.Allow("b") {
		t.Fatal("other key limited")
	}
	now = now.Add(time.Second)
	if !l.Allow("a") {
		t.Fatal("token did not refill after 1 s at 60/min")
	}
	h := l.Middleware(func(r *http.Request) string { return ClientIP(r, false) })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || !strings.Contains(w.Body.String(), TooManyRequests) {
		t.Fatalf("expected 429, got %d %s", w.Code, w.Body)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")
	if ClientIP(r, false) != "10.0.0.1" || ClientIP(r, true) != "203.0.113.9" {
		t.Fatalf("ClientIP wrong: %s %s", ClientIP(r, false), ClientIP(r, true))
	}
}

func TestRecover(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("nope") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "internal_server_error") {
		t.Fatalf("panic not turned into a 500: %d %s", w.Code, w.Body)
	}
}
