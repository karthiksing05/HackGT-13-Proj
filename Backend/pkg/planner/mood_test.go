package planner

import (
	"reflect"
	"testing"
)

func TestMoodNegationsBecomeHardExcludes(t *testing.T) {
	cases := []struct {
		mood      string
		wantCats  []string
		wantTags  []string
		wantFacet []string
	}{
		{"no bars please", []string{"bar", "nightclub"}, []string{"drinks"}, nil},
		{"Nothing outdoors, it might rain", nil, []string{"outdoor"}, nil},
		{"avoid loud clubs", []string{"nightclub"}, nil, nil},
		{"skip the museums today", []string{"gallery", "museum"}, []string{"art"}, nil},
		{"don't want live music", []string{"live_music"}, []string{"music"}, nil},
		{"no bars, but live music would be great", []string{"bar", "nightclub"}, []string{"drinks"}, []string{"Music"}},
		{"something chill and outside, then cheap food after", nil, nil, []string{"Outdoors", "Food", "Chill"}},
	}
	for _, c := range cases {
		hard, facets := ExtractMoodConstraints(c.mood, nil)
		if !reflect.DeepEqual(hard.ExcludeCategories, c.wantCats) && !(len(hard.ExcludeCategories) == 0 && len(c.wantCats) == 0) {
			t.Errorf("%q: cats %v, want %v", c.mood, hard.ExcludeCategories, c.wantCats)
		}
		if !reflect.DeepEqual(hard.ExcludeTags, c.wantTags) && !(len(hard.ExcludeTags) == 0 && len(c.wantTags) == 0) {
			t.Errorf("%q: tags %v, want %v", c.mood, hard.ExcludeTags, c.wantTags)
		}
		got := facetNames(facets)
		if len(c.wantFacet) == 0 && len(got) != 0 {
			t.Errorf("%q: unexpected facets %v", c.mood, got)
		}
		for _, f := range c.wantFacet {
			if !containsString(got, f) {
				t.Errorf("%q: facets %v missing %s", c.mood, got, f)
			}
		}
		if hard.FreeOnly {
			t.Errorf("%q should not be free-only", c.mood)
		}
	}
}

func TestMoodFreeFamilySober(t *testing.T) {
	for _, mood := range []string{"I'm broke", "free stuff only", "for free", "keep it free", "free"} {
		if hard, _ := ExtractMoodConstraints(mood, nil); !hard.FreeOnly {
			t.Errorf("%q should be free-only", mood)
		}
	}
	for _, mood := range []string{"I'm free after 6", "free afternoon", "freedom park"} {
		if hard, _ := ExtractMoodConstraints(mood, nil); hard.FreeOnly {
			t.Errorf("%q should not be free-only", mood)
		}
	}
	hard, _ := ExtractMoodConstraints("something for the kids", nil)
	if !containsString(hard.ExcludeTags, "21_plus") || !containsString(hard.ExcludeCategories, "bar") || !containsString(hard.ExcludeCategories, "nightclub") {
		t.Errorf("family: %+v", hard)
	}
	hard, facets := ExtractMoodConstraints("sober night out", nil)
	if !containsString(hard.ExcludeTags, "drinks") || !containsString(hard.ExcludeCategories, "bar") {
		t.Errorf("sober: %+v", hard)
	}
	// A sober night out can still be live music or comedy: Nightlife stays soft.
	if !containsString(facetNames(facets), "Nightlife") {
		t.Errorf("sober night out facets: %v", facetNames(facets))
	}
	// A facet whose every category and tag is refused is not suggested back.
	hard, facets = ExtractMoodConstraints("no museums or art, maybe murals", nil)
	if !containsString(hard.ExcludeCategories, "museum") || !containsString(hard.ExcludeTags, "art") {
		t.Errorf("no museums: %+v", hard)
	}
	if containsString(facetNames(facets), "Art") {
		t.Errorf("a refused facet came back: %v", facetNames(facets))
	}
}

func TestMoodUnrelatedTextAndPicks(t *testing.T) {
	hard, facets := ExtractMoodConstraints("Surprise me!", []string{"Nerdy", "nerdy", "meet_people", "Unknown pick"})
	if len(hard.ExcludeCategories)+len(hard.ExcludeTags) != 0 || hard.FreeOnly {
		t.Errorf("unrelated text produced %+v", hard)
	}
	if got := facetNames(facets); !reflect.DeepEqual(got, []string{"Nerdy", "Meet people"}) {
		t.Errorf("picks → facets %v", got)
	}
	hard, facets = ExtractMoodConstraints("", nil)
	if len(facets) != 0 || hard.FreeOnly {
		t.Errorf("empty mood: %+v %v", hard, facets)
	}
	// Deterministic: same input, same output.
	a1, f1 := ExtractMoodConstraints("no bars, chill, outside, then food", []string{"Music"})
	a2, f2 := ExtractMoodConstraints("no bars, chill, outside, then food", []string{"Music"})
	if !reflect.DeepEqual(a1, a2) || !reflect.DeepEqual(f1, f2) {
		t.Error("mood extraction must be deterministic")
	}
}

func TestFacetCoversAndTable(t *testing.T) {
	f, ok := FacetByName("Meet people")
	if !ok || f.Name != "Meet people" {
		t.Fatal("facet lookup")
	}
	for _, alias := range []string{"meet_people", "MEET PEOPLE", " meet-people "} {
		if g, ok := FacetByName(alias); !ok || g.Name != f.Name {
			t.Errorf("alias %q", alias)
		}
	}
	if _, ok := FacetByName("Sleeping"); ok {
		t.Error("unknown facet")
	}
	if len(facetTable) != 9 {
		t.Errorf("facet table has %d entries", len(facetTable))
	}
}
