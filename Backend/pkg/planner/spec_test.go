package planner

import (
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestParseAppRequestFromContractExample(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/api/examples/PlanRequest.json")
	if err != nil {
		t.Skipf("contract example missing: %v", err)
	}
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, ny)
	user := &UserContext{ID: "u1", Catalog: "activities", City: "atlanta", AgeBracket: "adult"}
	spec, err := ParsePlanRequest(raw, "America/New_York", now, user, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Catalog != "activities" || spec.City != "atlanta" || spec.TZ.String() != "America/New_York" {
		t.Errorf("catalog/city/tz = %s/%s/%s", spec.Catalog, spec.City, spec.TZ)
	}
	if *spec.Start != (travel.Point{Lat: 33.7766, Lng: -84.389}) || spec.StartName != "Tech Square (current location)" {
		t.Errorf("start = %+v %q", spec.Start, spec.StartName)
	}
	if spec.End.Lat != 33.771 || spec.EndName != "Home · North Ave Apts" {
		t.Errorf("end = %+v %q", spec.End, spec.EndName)
	}
	if !spec.From.Equal(time.Date(2026, 9, 25, 18, 10, 0, 0, time.UTC)) || !spec.BackBy.Equal(time.Date(2026, 9, 25, 22, 30, 0, 0, time.UTC)) {
		t.Errorf("window = %v .. %v", spec.From, spec.BackBy)
	}
	if spec.LocalDate != "2026-09-25" {
		t.Errorf("local date = %s", spec.LocalDate)
	}
	if spec.Mode != travel.Transit || spec.MaxLegKm != 10 || spec.Range != "transit" {
		t.Errorf("mode %s, max leg %.0f, range %s", spec.Mode, spec.MaxLegKm, spec.Range)
	}
	if spec.Budget != (Budget{Tier: 1, TotalCents: 2500}) {
		t.Errorf("budget = %+v", spec.Budget)
	}
	if spec.Pace != "balanced" || spec.Who != "friends" {
		t.Errorf("pace %s who %s", spec.Pace, spec.Who)
	}
	if len(spec.QuickPicks) != 3 || spec.QuickPicks[2] != "Meet people" {
		t.Errorf("picks = %v", spec.QuickPicks)
	}
	names := facetNames(spec.Facets)
	if len(names) < 3 || names[0] != "Outdoors" || names[1] != "Food" || names[2] != "Meet people" {
		t.Errorf("facets = %v", names)
	}
	// "Something chill and outside, then cheap food after." adds Chill.
	if !containsString(names, "Chill") {
		t.Errorf("mood should add the Chill facet: %v", names)
	}
	if spec.AgeBracket != "21_plus" || spec.SnappedStart {
		t.Errorf("age %s snapped %v", spec.AgeBracket, spec.SnappedStart)
	}
	if string(spec.Raw) != string(raw) {
		t.Error("raw body should be kept verbatim")
	}
}

func facetNames(fs []Facet) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

func TestParseAppRequestMappings(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name          string
		o             reqOpts
		wantMode      travel.Mode
		wantLabel     string
		wantLeg       float64
		wantBudget    Budget
		wantPace      string
		wantWindowLen time.Duration
	}{
		{"walkable none walk", withOpts(func(o *reqOpts) { o.rng, o.ride, o.modes = "walkable", "none", []string{"walk"} }), travel.Walk, "drive", 2, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"anywhere walk capped", withOpts(func(o *reqOpts) { o.rng, o.ride, o.modes = "anywhere", "none", []string{"walk"} }), travel.Walk, "drive", 4, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"transit marta", withOpts(func(o *reqOpts) { o.rng, o.ride, o.modes = "transit", "none", []string{"marta", "walk"} }), travel.Transit, "drive", 10, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"drive anywhere", withOpts(func(o *reqOpts) { o.rng, o.ride = "anywhere", "drive" }), travel.Drive, "drive", 25, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"cover rideshare", withOpts(func(o *reqOpts) { o.rng, o.ride = "transit", "cover" }), travel.Drive, "rideshare", 10, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"none ignores drive in modes", withOpts(func(o *reqOpts) { o.ride, o.modes = "none", []string{"drive", "uber"} }), travel.Walk, "drive", 2, BudgetForLevel(2), "balanced", 5 * time.Hour},
		{"free budget relaxed pace", withOpts(func(o *reqOpts) { o.budget, o.pace = 0, "relaxed" }), travel.Walk, "drive", 2, BudgetForLevel(0), "chill", 5 * time.Hour},
		{"$$$ packed", withOpts(func(o *reqOpts) { o.budget, o.pace = 3, "packed" }), travel.Walk, "drive", 2, BudgetForLevel(3), "packed", 5 * time.Hour},
		{"back_by before start is next day", withOpts(func(o *reqOpts) {
			o.from, o.backBy = time.Date(2026, 9, 26, 21, 0, 0, 0, ny), time.Date(2026, 9, 26, 2, 0, 0, 0, ny)
		}), travel.Walk, "drive", 2, BudgetForLevel(2), "balanced", 5 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := ParsePlanRequest(appRequestJSON(c.o), "America/New_York", testNow, sandy(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if spec.Mode != c.wantMode || spec.DriveLabel != c.wantLabel || spec.MaxLegKm != c.wantLeg {
				t.Errorf("mode %s/%s leg %.1f, want %s/%s %.1f", spec.Mode, spec.DriveLabel, spec.MaxLegKm, c.wantMode, c.wantLabel, c.wantLeg)
			}
			if spec.Budget != c.wantBudget || spec.Pace != c.wantPace {
				t.Errorf("budget %+v pace %s", spec.Budget, spec.Pace)
			}
			if got := spec.BackBy.Sub(spec.From); got != c.wantWindowLen {
				t.Errorf("window length %v, want %v", got, c.wantWindowLen)
			}
		})
	}
}

func withOpts(f func(o *reqOpts)) reqOpts {
	o := defaultReq()
	f(&o)
	return o
}

func TestParseAppRequestFallbacksAndErrors(t *testing.T) {
	cfg := testConfig()
	// No coordinates: last location wins, then the city's default point.
	body := []byte(`{"start":{"name":"Somewhere"},"end":{"name":"Home"},"start_time":"2026-09-26T22:00:00Z","back_by":"2026-09-27T03:00:00Z","range":"walkable","ride":"none","budget":1,"pace":"balanced","who":"just_me","modes":["walk"],"mood_text":"","tags":[]}`)
	u := sandy()
	u.HomeBase = &Place{Name: "Lighthouse Point", Lat: 31.3902, Lng: -81.4102, HasCoord: true}
	u.LastLocation = &travel.Point{Lat: 31.3801, Lng: -81.4301}
	spec, err := ParsePlanRequest(body, "", testNow, u, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The home base comes first.
	if spec.Start.Lat != 31.3902 || spec.StartName != "Lighthouse Point" || spec.EndName != "Lighthouse Point" {
		t.Errorf("home base start: %+v %q", spec.Start, spec.StartName)
	}
	u.HomeBase = nil
	spec, err = ParsePlanRequest(body, "", testNow, u, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Names follow the points used: no end coordinate means a round trip.
	if *spec.Start != *u.LastLocation || *spec.End != *u.LastLocation || spec.StartName != "Current location" || spec.EndName != "Current location" {
		t.Errorf("start %+v %q end %+v %q", spec.Start, spec.StartName, spec.End, spec.EndName)
	}
	u.LastLocation = nil
	spec, err = ParsePlanRequest(body, "", testNow, u, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if *spec.Start != seasideMkt || spec.StartName != "Seaside Market Square" {
		t.Errorf("city default start: %+v %q", spec.Start, spec.StartName)
	}

	// Expired window.
	_, err = ParsePlanRequest(appRequestJSON(withOpts(func(o *reqOpts) {
		o.from, o.backBy = time.Date(2026, 9, 25, 18, 0, 0, 0, ny), time.Date(2026, 9, 25, 22, 0, 0, 0, ny)
	})), "", testNow, sandy(), cfg)
	var re *RequestError
	if !errors.As(err, &re) || re.Reason != "window already ended" {
		t.Errorf("expired window: %v", err)
	}
	// Missing times and bad JSON.
	if _, err := ParsePlanRequest([]byte(`{"start":{"name":"x"}}`), "", testNow, sandy(), cfg); !errors.As(err, &re) {
		t.Errorf("missing times: %v", err)
	}
	if _, err := ParsePlanRequest([]byte(`{`), "", testNow, sandy(), cfg); !errors.As(err, &re) {
		t.Errorf("bad json: %v", err)
	}
	// Time zone: no city → header; bad header → default.
	nobody := &UserContext{Catalog: "activities"}
	far := withOpts(func(o *reqOpts) {
		o.start, o.end = travel.Point{Lat: 52.52, Lng: 13.405}, travel.Point{Lat: 52.52, Lng: 13.405}
	})
	spec, err = ParsePlanRequest(appRequestJSON(far), "Europe/Paris", testNow, nobody, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if spec.City != "berlin" || spec.TZ.String() != "Europe/Berlin" {
		t.Errorf("nearest city/tz = %s/%s", spec.City, spec.TZ)
	}
	// Far from every catalog city: the city stays unknown (no city filter)
	// and the header's zone is used.
	mid := withOpts(func(o *reqOpts) { o.start, o.end = travel.Point{Lat: 10, Lng: 10}, travel.Point{Lat: 10, Lng: 10} })
	spec, _ = ParsePlanRequest(appRequestJSON(mid), "Europe/Paris", testNow, nobody, cfg)
	if spec.TZ.String() != "Europe/Paris" || spec.City != "" {
		t.Errorf("header tz / unknown city = %s/%q", spec.TZ, spec.City)
	}
	spec, _ = ParsePlanRequest(appRequestJSON(mid), "Not/AZone", testNow, nobody, cfg)
	if spec.TZ.String() != "America/New_York" {
		t.Errorf("default tz = %s", spec.TZ)
	}
}

func TestSnapRuleForDemoUser(t *testing.T) {
	// Sandy's phone is in Atlanta; her catalog is Saltlight.
	o := withOpts(func(o *reqOpts) { o.start, o.end = techSquare, techSquare })
	spec, err := ParsePlanRequest(appRequestJSON(o), "", testNow, sandy(), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !spec.SnappedStart || *spec.Start != seasideMkt || *spec.End != seasideMkt || spec.StartName != "Seaside Market Square" {
		t.Errorf("snap: %+v %+v %v", spec.Start, spec.End, spec.SnappedStart)
	}
	// A home base in the city is the snap target; one outside it is not.
	lighthouse := travel.Point{Lat: 31.3902, Lng: -81.4102}
	withHome := sandy()
	withHome.HomeBase = &Place{Name: "Lighthouse Point", Lat: lighthouse.Lat, Lng: lighthouse.Lng, HasCoord: true}
	spec, _ = ParsePlanRequest(appRequestJSON(o), "", testNow, withHome, testConfig())
	if !spec.SnappedStart || *spec.Start != lighthouse || spec.StartName != "Lighthouse Point" {
		t.Errorf("snap to the home base: %+v %q", spec.Start, spec.StartName)
	}
	withHome.HomeBase = &Place{Name: "Midtown", Lat: techSquare.Lat, Lng: techSquare.Lng, HasCoord: true}
	spec, _ = ParsePlanRequest(appRequestJSON(o), "", testNow, withHome, testConfig())
	if !spec.SnappedStart || *spec.Start != seasideMkt {
		t.Errorf("a home base outside the city is not a snap target: %+v", spec.Start)
	}
	// A user without a city is never snapped.
	u := sandy()
	u.City = ""
	spec, _ = ParsePlanRequest(appRequestJSON(o), "", testNow, u, testConfig())
	if spec.SnappedStart || *spec.Start != techSquare || spec.City != "atlanta" {
		t.Errorf("no-city user: %+v snapped=%v city=%s", spec.Start, spec.SnappedStart, spec.City)
	}
}

func TestParseLegacyRequestParity(t *testing.T) {
	body := []byte(`{"start_location":"33.7766,-84.3890","end_location":"33.7710,-84.3918","date":"2026-09-26","start_time":"18:00","back_by_time":"23:00","range_km":3,"ride_choice":"none","travel_modes":["walk","marta"],"budget_cents":4000,"pace":"balanced","mood_text":"Something chill, then live music","tags":["music"]}`)
	user := &UserContext{ID: "u", Catalog: "activities", City: "atlanta"}
	spec, err := ParsePlanRequest(body, "", time.Date(2026, 9, 26, 12, 0, 0, 0, ny), user, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !spec.From.Equal(time.Date(2026, 9, 26, 18, 0, 0, 0, ny)) || !spec.BackBy.Equal(time.Date(2026, 9, 26, 23, 0, 0, 0, ny)) {
		t.Errorf("window = %v .. %v", spec.From.In(ny), spec.BackBy.In(ny))
	}
	if spec.Mode != travel.Transit || spec.MaxLegKm != 3 || spec.Budget != (Budget{Tier: 3, TotalCents: 4000}) {
		t.Errorf("mode %s leg %.0f budget %+v", spec.Mode, spec.MaxLegKm, spec.Budget)
	}
	if *spec.Start != techSquare || spec.End.Lat != 33.7710 {
		t.Errorf("points: %+v %+v", spec.Start, spec.End)
	}
	names := facetNames(spec.Facets)
	if len(names) < 2 || names[0] != "Music" || !containsString(names, "Chill") {
		t.Errorf("facets = %v", names)
	}
	// Legacy with drive in travel_modes keeps the old rule.
	body2 := []byte(`{"start_location":"33.7766,-84.3890","date":"2026-09-26","start_time":"18:00","back_by_time":"23:00","travel_modes":["walk","drive"]}`)
	spec, err = ParsePlanRequest(body2, "", time.Date(2026, 9, 26, 12, 0, 0, 0, ny), user, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != travel.Drive || spec.MaxLegKm != 25 || spec.Budget.Tier != 3 || spec.Budget.TotalCents != 0 {
		t.Errorf("legacy defaults: mode %s leg %.0f budget %+v", spec.Mode, spec.MaxLegKm, spec.Budget)
	}
	if _, err := ParsePlanRequest([]byte(`{"start_location":"x","date":"tomorrow"}`), "", testNow, user, testConfig()); err == nil {
		t.Error("malformed legacy date should fail")
	}
}

func TestPlaceJSONShape(t *testing.T) {
	b, _ := json.Marshal(PlaceAt("Tech Square", techSquare))
	if string(b) != `{"name":"Tech Square","coordinate":{"lat":33.7766,"lng":-84.389}}` {
		t.Errorf("place json = %s", b)
	}
	b, _ = json.Marshal(Place{Name: "Dropped pin"})
	if string(b) != `{"name":"Dropped pin"}` {
		t.Errorf("place json = %s", b)
	}
	var p Place
	if err := json.Unmarshal([]byte(`{"name":"X","coordinate":{"lat":1,"lng":2}}`), &p); err != nil || !p.HasCoord || p.Lat != 1 {
		t.Errorf("decode: %+v %v", p, err)
	}
}

func TestAgeAndCatalogNormalisation(t *testing.T) {
	for in, want := range map[string]string{"": "21_plus", "adult": "21_plus", "21_plus": "21_plus", "under_21": "18_20", "18_20": "18_20", "teen": "13_17", "13_17": "13_17", "under_13": "13_17", "weird": "13_17"} {
		if got := NormalizeAgeBracket(in); got != want {
			t.Errorf("age %q → %s, want %s", in, got, want)
		}
	}
	if c, ok := NormalizeCatalog("demo_activities"); !ok || c != "demo_activities" {
		t.Error("demo catalog")
	}
	if _, ok := NormalizeCatalog("users"); ok {
		t.Error("only allow-listed catalogs")
	}
	if c, ok := NormalizeCatalog(""); !ok || c != store.DefaultCatalog {
		t.Error("empty catalog defaults")
	}
}
