package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// examplesDir holds the dumps of the app's ContractTests (docs/api/examples),
// the truth for every wire shape.
const examplesDir = "../../../docs/api/examples"

// examples maps each dump to a fresh pointer of the Go type it must round-trip
// through. A ".<variant>" suffix (PlanBatch.dag) selects the same type; aliases
// map differently named dumps.
var examples = map[string]func() any{
	"ActivityHits":           func() any { return &[]ActivityHit{} },
	"AuthResponse":           func() any { return &AuthResponse{} },
	"CalendarDays":           func() any { return &[]CalendarDay{} },
	"ChatThread":             func() any { return &ChatThread{} },
	"CheckoutIntent":         func() any { return &CheckoutIntent{} },
	"CheckoutPlan":           func() any { return &CheckoutPlan{} },
	"CheckoutRun":            func() any { return &CheckoutRun{} },
	"CreateCheckoutRun":      func() any { return &CreateCheckoutRun{} },
	"CreateItineraryRequest": func() any { return &CreateItineraryRequest{} },
	"FacebookConnection":     func() any { return &FacebookConnection{} },
	"FacebookImport":         func() any { return &FacebookImport{} },
	"ForumPosts":             func() any { return &[]ForumPost{} },
	"FriendRequests":         func() any { return &[]FriendRequest{} },
	"Friends":                func() any { return &[]Friend{} },
	"GroupLedger":            func() any { return &GroupLedger{} },
	"GroupPhotos":            func() any { return &[]GroupPhoto{} },
	"Integrations":           func() any { return &[]Integration{} },
	"Itinerary":              func() any { return &Itinerary{} },
	"ItineraryItem":          func() any { return &ItineraryItem{} },
	"ItineraryUpdate":        func() any { return &ItineraryUpdate{} },
	"JoinResult":             func() any { return &JoinResult{} },
	"Message":                func() any { return &Message{} },
	"Messages":               func() any { return &[]Message{} },
	"MyFreePost":             func() any { return &MyFreePost{} },
	"NewExpense":             func() any { return &NewExpense{} },
	"OutgoingFriendRequest":  func() any { return &FriendRequest{} },
	"PastEvents":             func() any { return &[]PastEvent{} },
	"PastInsights":           func() any { return &PastInsights{} },
	"PaymentMethods":         func() any { return &[]PaymentMethod{} },
	"PlanAlternatives":       func() any { return &[]PlanAlternative{} },
	"PlanBatch":              func() any { return &PlanBatch{} },
	"PlanRequest":            func() any { return &PlanRequest{} },
	"Preferences":            func() any { return &Preferences{} },
	"Rating":                 func() any { return &Rating{} },
	"RouteRequest":           func() any { return &RouteRequest{} },
	"RouteResult":            func() any { return &RouteResult{} },
	"SearchResults":          func() any { return &SearchResults{} },
	"SignupRequest":          func() any { return &SignupRequest{} },
	"TasteProfile":           func() any { return &TasteProfile{} },
	"Ticket":                 func() any { return &Ticket{} },
	"TransitOptions":         func() any { return &[]TransitOption{} },
	"User":                   func() any { return &User{} },
	"UserPatch":              func() any { return &UserPatch{} },
	"UserSearchResults":      func() any { return &[]UserSearchResult{} },
}

// aliases are dumps whose name is not the type name.
var aliases = map[string]string{
	"UserHomeBase": "User",
}

// typeName strips a ".<variant>" suffix and applies aliases.
func typeName(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	if alias, ok := aliases[name]; ok {
		return alias
	}
	return name
}

// skipped dumps are not one wire struct; each entry says why.
var skipped = map[string]string{
	"index":       "manifest of the examples, not a payload",
	"ForumQuery":  "app-side query object; GET /forum/posts takes URL parameters (lat, lng, area, radius, scope, type, when, max_dist, cost, tags, open_only, sort)",
	"NewFreePost": "app-side model; LiveAPIClient flattens it into the NewForumPost body (type, visibility, until, lat, lng, area_label, radius_mi)",
}

// canonical re-serializes JSON with sorted keys and Go's number formatting so
// two documents compare by value.
func canonical(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("canonical: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return string(out)
}

func TestExamplesRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(examplesDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples in %s: %v", examplesDir, err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		if why, ok := skipped[name]; ok {
			t.Logf("skip %s: %s", name, why)
			continue
		}
		fresh, ok := examples[typeName(name)]
		if !ok {
			t.Errorf("%s.json has no Go type in the examples map", name)
			continue
		}
		seen[typeName(name)] = true
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip(t, raw, fresh())
		})
	}
	for name := range examples {
		if !seen[name] {
			t.Errorf("examples map names %s but docs/api/examples/%s.json does not exist", name, name)
		}
	}
}

// roundTrip decodes raw into target with unknown keys refused, re-encodes
// it and compares the two documents by value.
func roundTrip(t *testing.T, raw []byte, target any) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		t.Fatalf("decode: %v", err)
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got, want := canonical(t, encoded), canonical(t, raw); got != want {
		t.Errorf("round trip differs\n got: %s\nwant: %s", got, want)
	}
	return encoded
}

// TestActivityHitShape pins GET /activities/search's hit as the server
// writes it (catalog hex ids; an event with every field, a free place, a
// place with neither price nor distance) and PlanRequest.must_include,
// which stays out of the body when there are no picks.
func TestActivityHitShape(t *testing.T) {
	hits := `[
	  {"id": "9bdbbc5e9aab712a5b769f72", "title": "Sunset Jazz on Pier Nine", "kind": "event", "category": "live_music",
	   "subtitle": "Live music · 6:30 PM · 0.7 mi", "place": {"name": "Pier Nine Bandstand", "coordinate": {"lat": 31.376524, "lng": -81.41746}},
	   "start": "2026-09-26T22:30:00Z", "end": "2026-09-27T01:00:00Z", "price_cents": 1200, "distance_mi": 0.7},
	  {"id": "62248db0069b1dc732103903", "title": "Seaside Market Hall", "kind": "place", "category": "market",
	   "subtitle": "Market · 0.2 mi", "place": {"name": "Seaside Market Hall", "coordinate": {"lat": 31.365972, "lng": -81.428348}},
	   "price_cents": 0, "distance_mi": 0.2},
	  {"id": "5f00000000000000000000ab", "title": "Anchor Park", "kind": "place", "category": "park",
	   "subtitle": "Park", "place": {"name": "Anchor Park", "coordinate": {"lat": 31.374748, "lng": -81.4207}}}
	]`
	var out []ActivityHit
	roundTrip(t, []byte(hits), &out)
	if len(out) != 3 || out[0].Kind != StopKindEvent || out[0].Start == nil || out[0].End == nil || out[1].Start != nil ||
		out[1].PriceCents == nil || *out[1].PriceCents != 0 || out[2].PriceCents != nil || out[2].DistanceMi != nil {
		t.Errorf("decoded hits %+v", out)
	}

	var req PlanRequest
	roundTrip(t, []byte(`{"start": {"name": "Home"}, "end": {"name": "Home"}, "date": "2026-09-26T22:00:00Z",
	  "start_time": "2026-09-26T22:00:00Z", "back_by": "2026-09-27T03:00:00Z", "range": "walkable", "ride": "none",
	  "mood_text": "", "tags": [], "budget": 2, "who": "friends", "pace": "balanced", "modes": ["walk"],
	  "must_include": ["9bdbbc5e9aab712a5b769f72", "62248db0069b1dc732103903"]}`), &req)
	if len(req.MustInclude) != 2 {
		t.Errorf("must_include %v", req.MustInclude)
	}
	if empty, _ := json.Marshal(PlanRequest{}); strings.Contains(string(empty), "must_include") {
		t.Errorf("no picks must leave must_include out: %s", empty)
	}
}

// TestActivityDetailShape pins GET /activities/{id} as the server writes it:
// an event with every field, and a place with only the keys that are
// always there (the labels, the place, tags as []).
func TestActivityDetailShape(t *testing.T) {
	event := `{"id": "9bdbbc5e9aab712a5b769f72", "title": "Sunset Jazz on Pier Nine", "kind": "event", "category": "live_music",
	  "category_label": "Live music", "summary": "Brass and sunsets on the pier.",
	  "description": "Brass and sunsets on the pier. Bring a blanket; the bandstand has a few benches.",
	  "venue_name": "Pier Nine Bandstand", "address": "9 Pier Rd, Saltlight Harbor, GA 31991",
	  "place": {"name": "Pier Nine Bandstand", "coordinate": {"lat": 31.376524, "lng": -81.41746}},
	  "start": "2026-09-26T22:30:00Z", "end": "2026-09-27T01:00:00Z", "price_cents": 1200, "price_label": "$12–$20",
	  "rating": 4.6, "rating_count": 1204, "url": "https://saltlight.example/jazz",
	  "ticket_url": "https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets", "image_url": "https://saltlight.example/jazz.jpg",
	  "tags": ["music", "outdoor"]}`
	var ev ActivityDetail
	roundTrip(t, []byte(event), &ev)
	if ev.Kind != StopKindEvent || ev.Start == nil || ev.End == nil || ev.HoursLine != nil || ev.PriceCents == nil || *ev.PriceCents != 1200 ||
		ev.Rating == nil || ev.RatingCount == nil || ev.TicketURL == nil || len(ev.Tags) != 2 || ev.Place.Coordinate == nil {
		t.Errorf("event %+v", ev)
	}

	place := `{"id": "62248db0069b1dc732103903", "title": "Seaside Market Hall", "kind": "place", "category": "market",
	  "category_label": "Market", "place": {"name": "Seaside Market Hall"}, "hours_line": "Closed that day",
	  "price_label": "Price unknown", "tags": []}`
	var pl ActivityDetail
	encoded := roundTrip(t, []byte(place), &pl)
	if pl.Kind != StopKindPlace || pl.HoursLine == nil || pl.PriceCents != nil || pl.Tags == nil || pl.Start != nil {
		t.Errorf("place %+v", pl)
	}
	for _, key := range []string{"summary", "address", "price_cents", "rating", "url", "start"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Errorf("%s written without a value: %s", key, encoded)
		}
	}
}

func TestTimeFormats(t *testing.T) {
	cases := map[string]string{
		`"2026-09-25T18:10:00Z"`:             "2026-09-25T18:10:00Z",
		`"2026-09-25T19:00:00.123456+00:00"`: "2026-09-25T19:00:00Z",
		`"2026-09-25T14:10:00-04:00"`:        "2026-09-25T18:10:00Z",
		`"2026-09-25"`:                       "2026-09-25T00:00:00Z",
	}
	for in, want := range cases {
		var tm Time
		if err := json.Unmarshal([]byte(in), &tm); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		out, _ := json.Marshal(tm)
		if string(out) != `"`+want+`"` {
			t.Errorf("%s → %s, want %s", in, out, want)
		}
	}
	var tm Time
	if err := json.Unmarshal([]byte(`"tomorrow"`), &tm); err == nil {
		t.Error("garbage accepted")
	}
	var holder struct {
		Required Time  `json:"required"`
		Optional *Time `json:"optional,omitempty"`
		Nullable *Time `json:"nullable"`
	}
	if err := json.Unmarshal([]byte(`{"required":"2026-09-25T18:10:00Z","optional":null,"nullable":null}`), &holder); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(holder)
	if string(out) != `{"required":"2026-09-25T18:10:00Z","nullable":null}` {
		t.Errorf("pointer semantics wrong: %s", out)
	}
	zero, _ := json.Marshal(struct {
		T Time `json:"t"`
	}{})
	if string(zero) != `{"t":"0001-01-01T00:00:00Z"}` {
		t.Errorf("zero non-pointer must still be present: %s", zero)
	}
	if NewTime(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)).Std().Hour() != 12 {
		t.Error("Std lost the value")
	}
}

func TestKeyNormalization(t *testing.T) {
	for in, want := range map[string]string{
		"liveMusic": "live_music", "live_music": "live_music", "BigCrowds": "big_crowds",
		"earlyMornings": "early_mornings", "food": "food", "perfectAfternoon": "perfect_afternoon",
		"neverDo": "never_do", "planAround": "plan_around", "15ToForty": "15_to_forty",
	} {
		if got := CanonicalKey(in); got != want {
			t.Errorf("CanonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
	var p Preferences
	if err := json.Unmarshal([]byte(`{"ratings":{"liveMusic":4,"outdoors":5},"answers":{"perfectAfternoon":"walks"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Ratings["live_music"] != 4 || p.Answers["perfect_afternoon"] != "walks" {
		t.Errorf("keys not normalized: %+v", p)
	}
	if !p.Ratings.Validate() {
		t.Error("valid ratings rejected")
	}
	if (Ratings{"outdoors": 6}).Validate() || (Ratings{"skiing": 3}).Validate() {
		t.Error("invalid ratings accepted")
	}
	out, _ := json.Marshal(Preferences{})
	if !strings.Contains(string(out), `"ratings":{}`) || !strings.Contains(string(out), `"answers":{}`) {
		t.Errorf("nil maps must write {}: %s", out)
	}
	modes, _ := json.Marshal(PlanRequest{Modes: TravelModes{ModeWalk, ModeMarta}})
	if !strings.Contains(string(modes), `"modes":["marta","walk"]`) {
		t.Errorf("modes not sorted: %s", modes)
	}
	empty, _ := json.Marshal(PlanRequest{})
	if !strings.Contains(string(empty), `"modes":[]`) {
		t.Errorf("nil modes must write []: %s", empty)
	}
}

func TestDefaultPreferencesValidate(t *testing.T) {
	p := DefaultPreferences()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Preferences{Company: "crowd"}
	if err := bad.Validate(); err == nil {
		t.Fatal("bad company accepted")
	}
	partial := Preferences{Ratings: Ratings{"food": 3}}
	if err := partial.Validate(); err != nil || partial.Company != CompanySmallGroup || partial.Answers == nil {
		t.Fatalf("partial preferences not defaulted: %v %+v", err, partial)
	}
}

func TestTimeBSON(t *testing.T) {
	type doc struct {
		At   Time  `bson:"at"`
		Then *Time `bson:"then,omitempty"`
	}
	in := doc{At: NewTime(time.Date(2026, 9, 26, 18, 30, 0, 0, time.UTC))}
	raw, err := bson.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var generic bson.M
	if err := bson.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["at"].(bson.DateTime); !ok {
		t.Fatalf("Time must store as a BSON datetime, got %T", generic["at"])
	}
	var out doc
	if err := bson.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.At.Equal(in.At.Time) || out.Then != nil {
		t.Fatalf("bson round trip changed the value: %+v", out)
	}
}
