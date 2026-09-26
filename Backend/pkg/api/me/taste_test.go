package me

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"math"
	"testing"
)

func bars(p contract.TasteProfile) map[string]float64 {
	out := map[string]float64{}
	for _, b := range p.Bars {
		out[b.Label] = b.Value
	}
	return out
}

func TestTasteProfileMath(t *testing.T) {
	// Nothing said, nothing learned: the defaults (small group → Social 0.7
	// company, 0.5 crowds) and 0.5 everywhere else.
	fresh := tasteProfile(models.UserPrefs{}, models.UserTaste{})
	if len(fresh.Bars) != 5 {
		t.Fatalf("bars: %+v", fresh.Bars)
	}
	for i, label := range []string{"Outdoors", "Food", "Art", "Social", "Nightlife"} {
		if fresh.Bars[i].Label != label {
			t.Fatalf("bar %d is %q, want %q", i, fresh.Bars[i].Label, label)
		}
	}
	if got := bars(fresh); got["Outdoors"] != 0.5 || got["Art"] != 0.5 || got["Nightlife"] != 0.5 || got["Social"] != 0.56 {
		t.Fatalf("fresh: %v", got)
	}

	// What was said only: 0.6·rating/5 + 0.4·0.5.
	said := models.UserPrefs{Company: "small_group", Ratings: map[string]int{"outdoors": 5, "food": 4, "museums": 3, "nightlife": 2}}
	if got := bars(tasteProfile(said, models.UserTaste{})); got["Outdoors"] != 0.8 || got["Food"] != 0.68 || got["Art"] != 0.56 || got["Nightlife"] != 0.44 || got["Social"] != 0.56 {
		t.Fatalf("said only: %v", got)
	}

	// Said + learned reproduces the contract example (docs/api/examples/TasteProfile.json).
	learned := models.UserTaste{Tags: map[string]float64{"outdoors": 0.55, "food": 0.55, "museums": 0.475, "social": 0.7, "nightlife": 0.1}}
	want := map[string]float64{"Outdoors": 0.82, "Food": 0.7, "Art": 0.55, "Social": 0.64, "Nightlife": 0.28}
	if got := bars(tasteProfile(said, learned)); !equalBars(got, want) {
		t.Fatalf("example: %v, want %v", got, want)
	}

	// Solo and a love of crowds; the fallback tag names; clamping; NaN ignored.
	solo := models.UserPrefs{Company: "solo", Ratings: map[string]int{"big_crowds": 5}}
	tags := models.UserTaste{Tags: map[string]float64{"art": 1, "big_crowds": 0, "outdoors": 7, "food": -2, "nightlife": math.NaN()}}
	got := bars(tasteProfile(solo, tags))
	want = map[string]float64{
		"Social":    0.39, // 0.6·mean(0.3, 1) + 0.4·0
		"Art":       0.7,  // 0.6·0.5 + 0.4·1 (the "art" tag)
		"Outdoors":  0.7,  // learned clamps to 1
		"Food":      0.3,  // learned clamps to 0
		"Nightlife": 0.5,  // NaN reads as missing
	}
	if !equalBars(got, want) {
		t.Fatalf("edge cases: %v, want %v", got, want)
	}
	// "social" wins over the "big_crowds" fallback when both exist.
	both := models.UserTaste{Tags: map[string]float64{"social": 1, "big_crowds": 0}}
	if got := bars(tasteProfile(solo, both)); got["Social"] != 0.79 {
		t.Fatalf("social tag precedence: %v", got["Social"])
	}
}

func equalBars(got, want map[string]float64) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
