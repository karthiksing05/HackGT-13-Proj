package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// demoEvening is the scenario the golden files use: Sandy, Saturday
// evening in Saltlight, walking, $$, "Outdoors" + "Food".
func demoEvening(t *testing.T) (*testPlanner, Batch, PlanSpec) {
	t.Helper()
	tp := newTestPlanner(saltlight(t), testConfig())
	o := defaultReq()
	o.tags, o.mood = []string{"Outdoors", "Food"}, "something chill outside, then food"
	b, spec := tp.generate(t, sandy(), o)
	if len(b.Options) < 3 {
		t.Fatalf("demo evening: %d options (%s)", len(b.Options), b.Reason)
	}
	return tp, b, spec
}

func stopIDs(stops []Stop) []string {
	out := make([]string, len(stops))
	for i, s := range stops {
		out[i] = s.ID
	}
	return out
}

func TestPlanBatchGolden(t *testing.T) {
	_, b, _ := demoEvening(t)
	golden(t, "plan_batch.json", b)
}

// TestPlanBatchWireShape checks the app's required keys on the JSON itself.
func TestPlanBatchWireShape(t *testing.T) {
	_, b, _ := demoEvening(t)
	raw, _ := json.Marshal(b)
	var doc struct {
		Options []map[string]json.RawMessage `json:"options"`
		Cursor  *string                      `json:"cursor"`
		Done    *bool                        `json:"done"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Done == nil || *doc.Done || doc.Cursor == nil {
		t.Fatal("done and cursor are required on a first page with more to come")
	}
	for _, opt := range doc.Options {
		for _, k := range []string{"id", "name", "tag", "meta", "stops", "legs"} {
			if _, ok := opt[k]; !ok {
				t.Errorf("option lacks %q", k)
			}
		}
		var stops []map[string]json.RawMessage
		_ = json.Unmarshal(opt["stops"], &stops)
		for _, s := range stops {
			for _, k := range []string{"id", "title", "subtitle", "place", "duration_minutes", "price_cents", "arrive_time", "depart_time", "activity_id"} {
				if _, ok := s[k]; !ok {
					t.Errorf("stop lacks %q", k)
				}
			}
			var place struct {
				Name       string `json:"name"`
				Coordinate *struct {
					Lat, Lng float64
				} `json:"coordinate"`
			}
			if err := json.Unmarshal(s["place"], &place); err != nil || place.Name == "" || place.Coordinate == nil {
				t.Errorf("stop place %s", s["place"])
			}
		}
		var legs []struct {
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(opt["legs"], &legs)
		for _, l := range legs {
			if !appLegModes[l.Mode] {
				t.Errorf("leg mode %q", l.Mode)
			}
		}
	}
}

// The contract's own PlanBatch example decodes into the planner's types
// with every required field in place.
func TestContractPlanBatchDecodes(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/api/examples/PlanBatch.json")
	if err != nil {
		t.Skipf("contract example missing: %v", err)
	}
	var b Batch
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Options) != 3 || b.Cursor != "batch-0" || b.Done {
		t.Fatalf("decoded %+v", b)
	}
	s := b.Options[0].Stops[0]
	if b.Options[0].Name != "Rooftop + murals" || b.Options[0].Tag != "Best match" || s.Title != "Skyline Park rooftop" ||
		s.Subtitle != "Games + views · $" || s.DurationMinutes != 80 || !s.Place.HasCoord || s.Place.Lat != 33.7727 {
		t.Errorf("option 0 = %+v", b.Options[0])
	}
}

func TestShortNameAndOptionName(t *testing.T) {
	for in, want := range map[string]string{
		"The Crow's Nest Rooftop":                    "Crow's Nest Rooftop",
		"Oracle of the Deep: Late-Night Data Seance": "Oracle",
		"Sunset Jazz on Pier Nine":                   "Sunset Jazz",
		"Jazz at the Pier":                           "Jazz",
		"Harbor Lights - Winter Edition":             "Harbor Lights",
		"Museum":                                     "Museum",
		"The":                                        "The",
		"Brine and Bivalve Oyster Bar":               "Brine and Bivalve",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
	stops := []Stop{{Title: "The Crow's Nest Rooftop"}, {Title: "Anchor Park"}, {Title: "Low Tide Taqueria"}, {Title: "Pier Nine"}}
	for n, want := range []string{"Sidequest", "Crow's Nest Rooftop", "Crow's Nest Rooftop + Anchor Park",
		"Crow's Nest Rooftop + Anchor Park + 1 more", "Crow's Nest Rooftop + Anchor Park + 2 more"} {
		if got := optionName(stops[:n]); got != want {
			t.Errorf("%d stops: %q", n, got)
		}
	}
}

func TestLegModesStayInTheAppEnum(t *testing.T) {
	cases := []struct {
		mode  travel.Mode
		label string
		want  string
	}{
		{travel.Walk, "drive", "walk"}, {travel.Transit, "drive", "marta"},
		{travel.Drive, "drive", "drive"}, {travel.Drive, "rideshare", "rideshare"}, {"", "drive", "walk"},
	}
	for _, c := range cases {
		if got := legMode(c.mode, c.label); got != c.want || !appLegModes[got] {
			t.Errorf("legMode(%s, %s) = %s", c.mode, c.label, got)
		}
	}
	for mode, verb := range map[string]string{"walk": "Walk", "marta": "MARTA", "drive": "Drive", "rideshare": "Rideshare", "uber": "Uber"} {
		if legVerb(mode) != verb {
			t.Errorf("legVerb(%s) = %s", mode, legVerb(mode))
		}
	}
}

func TestSubtitleMetaAndTags(t *testing.T) {
	jazz := synthEvent("Jazz", "live_music", seasideMkt, localAt(18, 30), 90, []string{"music"}, priceOf(12))
	s := stopFromActivity(&jazz, ny)
	if s.Subtitle != "Live music · $ · 6:30 PM" || !s.PriceKnown || *s.PriceCents != 1200 {
		t.Errorf("event stop: %q %v", s.Subtitle, s.PriceCents)
	}
	park := synthPlace("Park", "park", seasideMkt, nil, nil, nil)
	s = stopFromActivity(&park, ny)
	if s.Subtitle != "Park" || s.PriceKnown || s.PriceCents != nil {
		t.Errorf("unknown price: %q %v", s.Subtitle, s.PriceCents)
	}
	if categoryLabel("sports_event") != "Sports" || categoryLabel("board_games") != "Board Games" || categoryLabel("") != "Sidequest" {
		t.Error("category labels")
	}

	free := int64(0)
	cheap := int64(1200)
	walk := &PlanSpec{Mode: travel.Walk, DriveLabel: "drive", TZ: ny}
	transit := &PlanSpec{Mode: travel.Transit, DriveLabel: "drive", TZ: ny}
	opt := Option{CostKnown: true, Stops: []Stop{{PriceCents: &free, PriceKnown: true, TierKnown: true}, {PriceCents: &free, PriceKnown: true, TierKnown: true}},
		Legs: []Leg{{Mode: "walk"}, {Mode: "walk"}, {Mode: "walk"}}}
	if got := optionMeta(opt, walk, 1.609344*1.2); got != "Free · 1.2 mi walking · 2 stops" {
		t.Errorf("meta %q", got)
	}
	opt = Option{Stops: []Stop{{PriceCents: &cheap, PriceKnown: true, Tier: 1, TierKnown: true}, {}},
		Legs: []Leg{{Mode: "marta"}, {Mode: "walk"}, {Mode: "marta"}}}
	if got := optionMeta(opt, transit, 1.609344*2.34); got != "~$ · 2.3 mi by transit · 2 transit legs" {
		t.Errorf("meta %q", got)
	}
	opt = Option{Stops: []Stop{{}}, Legs: []Leg{{Mode: "rideshare"}, {Mode: "walk"}}}
	if got := optionMeta(opt, &PlanSpec{Mode: travel.Drive, DriveLabel: "rideshare"}, 8); got != "5.0 mi by rideshare · 1 rideshare leg" {
		t.Errorf("unknown prices leave the price out: %q", got)
	}

	used := map[string]bool{}
	if optionTag(0, Option{}, used, PlanSpec{}) != "Best match" {
		t.Error("first option is the best match")
	}
	outdoorStops := []Stop{{Category: "park"}, {Category: "garden"}}
	if got := optionTag(1, Option{CostKnown: true, Stops: outdoorStops}, used, PlanSpec{}); got != "Free" {
		t.Errorf("free option tag %q", got)
	}
	used["Free"] = true
	if got := optionTag(2, Option{CostKnown: true, Stops: outdoorStops}, used, PlanSpec{}); got != "Outdoors" {
		t.Errorf("second free outdoor option: %q", got)
	}
	used["Outdoors"], used["Chill"] = true, true
	three := []Stop{{Category: "comedy"}, {Category: "tour"}, {Category: "museum", Tags: []string{"art"}}}
	if got := optionTag(3, Option{Stops: three}, used, PlanSpec{}); got != "Culture" {
		t.Errorf("tag %q", got)
	}
	used["Culture"] = true
	if got := optionTag(4, Option{Stops: three}, used, PlanSpec{}); got != "Different vibe" {
		t.Errorf("fallback tag %q", got)
	}
}

func TestRenderedStopsCarryTheActivity(t *testing.T) {
	tp, b, spec := demoEvening(t)
	catalog := catalogByID(saltlight(t))
	for _, opt := range tp.allOptions(t, sandy(), b) {
		assertGuarantees(t, spec, opt, catalog)
		var total int64
		known := true
		for i, s := range opt.Stops {
			a := catalog[s.ActivityID]
			if s.Title != strings.TrimSpace(a.Name) || s.Kind != a.Kind || s.Category != a.Category || s.Order != i {
				t.Errorf("stop %s does not describe %s", s.ID, a.Name)
			}
			if s.DurationMinutes != int(s.Depart.Sub(s.Arrive).Minutes()) {
				t.Errorf("duration %d for %v–%v", s.DurationMinutes, s.Arrive, s.Depart)
			}
			if s.PriceKnown {
				total += *s.PriceCents
			} else {
				known = false
			}
		}
		if opt.TotalCostCents != total || opt.CostKnown != known {
			t.Errorf("%s total %d/%v, want %d/%v", opt.ID, opt.TotalCostCents, opt.CostKnown, total, known)
		}
		if opt.TotalDurationMin != int(opt.Arrival.Sub(opt.Depart).Minutes()) {
			t.Errorf("%s duration", opt.ID)
		}
	}
	_ = models.Activity{}
}
