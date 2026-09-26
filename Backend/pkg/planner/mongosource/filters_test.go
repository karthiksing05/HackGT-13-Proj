package mongosource

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/planner"
	"Backend/pkg/travel"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func sampleQuery() planner.CandidateQuery {
	return planner.CandidateQuery{
		Catalog: "demo_activities", City: "saltlight", Kinds: []string{"event", "place"},
		Center: travel.Point{Lat: 31.368, Lng: -81.425}, RadiusKm: 5,
		From: time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC),
		MaxTier: 1, AllowUnknownPrice: true, AgeBracket: "13_17",
		ExcludeCategories: []string{"museum"}, ExcludeTags: []string{"touristy"},
		IncludeCategories: []string{"restaurant", "cafe", "market"}, AnyTags: []string{"food"},
		ExcludeIDs:      []string{"896e6ea590424da0defda2ed", "not-an-id"},
		PlaceCategories: itinerary.PlaceCategories(),
		LimitEvents:     400, LimitPlaces: 600,
	}
}

// goldenJSON compares a filter's relaxed extended JSON with testdata/<name>.
func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := bson.MarshalExtJSON(v, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte('\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (run with -update): %v", name, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("%s changed; run with -update and review.\n%s", name, buf.String())
	}
}

// The exact §4.1 queries, reviewed by eye in testdata/.
func TestEventAndPlaceFiltersGolden(t *testing.T) {
	q := sampleQuery()
	goldenJSON(t, "event_filter.json", eventFilter(q))
	goldenJSON(t, "place_filter.json", placeFilter(q))
}

func TestPriceClauses(t *testing.T) {
	q := sampleQuery()
	q.FreeOnly = true
	goldenJSON(t, "price_free_only.json", priceClause(q))
	q.FreeOnly, q.MaxTier = false, 3
	if priceClause(q) != nil {
		t.Error("tier 3 has no price clause")
	}
	q.MaxTier, q.AllowUnknownPrice = 2, false
	or := priceClause(q)[0].Value.(bson.A)
	if len(or) != 3 {
		t.Errorf("tier 2 without unknown prices: %d branches", len(or))
	}
}

func TestAgeAndExclusionsInTheQuery(t *testing.T) {
	q := sampleQuery()
	q.AgeBracket, q.ExcludeTags, q.ExcludeCategories = "21_plus", nil, nil
	f := eventFilter(q)
	for _, e := range f {
		if e.Key == "name" || e.Key == "tags" || e.Key == "category" {
			t.Errorf("21_plus with no exclusions should not filter %s: %v", e.Key, e.Value)
		}
	}
	q.AgeBracket = "18_20"
	f = placeFilter(q)
	for _, e := range f {
		if e.Key == "category" {
			for _, c := range e.Value.(bson.D)[0].Value.([]string) {
				if c == "bar" || c == "nightclub" {
					t.Errorf("18_20 places still include %s", c)
				}
			}
		}
	}
}

func TestUnknownCatalogNeverReachesMongo(t *testing.T) {
	s := &Store{} // no database: an allow-list miss must fail before touching it
	ctx := context.Background()
	for _, name := range []string{"users", "", "activities; drop", "plan_pools"} {
		q := sampleQuery()
		q.Catalog = name
		if _, _, err := s.FindCandidates(ctx, q); !errors.Is(err, ErrUnknownCatalog) {
			t.Errorf("FindCandidates(%q): %v", name, err)
		}
		if _, err := s.FetchEmbeddings(ctx, name, []string{"896e6ea590424da0defda2ed"}); !errors.Is(err, ErrUnknownCatalog) {
			t.Errorf("FetchEmbeddings(%q): %v", name, err)
		}
		if _, err := s.GetActivities(ctx, name, nil); !errors.Is(err, ErrUnknownCatalog) {
			t.Errorf("GetActivities(%q): %v", name, err)
		}
	}
}

func TestSafeKeysAndIDs(t *testing.T) {
	for k, ok := range map[string]bool{"alt_896e6ea590424da0defda2ed_0": true, "": false, "a.b": false, "$set": false} {
		if safeKey(k) != ok {
			t.Errorf("safeKey(%q)", k)
		}
	}
	if ids := objectIDs([]string{"896e6ea590424da0defda2ed", "nope", ""}); len(ids) != 1 {
		t.Errorf("objectIDs kept %d", len(ids))
	}
	if got := union([]string{"a", "b"}, []string{"b", "c", ""}); len(got) != 3 {
		t.Errorf("union %v", got)
	}
}
