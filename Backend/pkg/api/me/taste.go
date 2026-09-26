package me

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"math"
	"net/http"
)

// Each taste bar blends what the person told us (a 1–5 rating over 5) with
// what their ratings of past stops taught us (users.taste.tags, 0…1):
// value = clamp(0.6·said + 0.4·learned), two decimals. A rating or tag that
// is missing reads 0.5 (backend-contract §4 A).
const (
	saidWeight    = 0.6
	learnedWeight = 0.4
	neutral       = 0.5
)

// tasteBar is one bar of Account › Taste: how to read what was said, and the
// taste tags that carry what was learned (the first one present counts).
type tasteBar struct {
	label string
	said  func(contract.Preferences) float64
	tags  []string
}

var tasteBars = []tasteBar{
	{label: "Outdoors", said: rated("outdoors"), tags: []string{"outdoors"}},
	{label: "Food", said: rated("food"), tags: []string{"food"}},
	{label: "Art", said: rated("museums"), tags: []string{"museums", "art"}},
	{label: "Social", said: social, tags: []string{"social", "big_crowds"}},
	{label: "Nightlife", said: rated("nightlife"), tags: []string{"nightlife"}},
}

// TasteProfile is GET /me/taste-profile → TasteProfile.
func (h *H) TasteProfile(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tasteProfile(user.Prefs, user.Taste))
}

// tasteProfile computes the bars from the preferences as the app shows them
// (the defaults before the first save) and the learned taste tags.
func tasteProfile(prefs models.UserPrefs, taste models.UserTaste) contract.TasteProfile {
	shown := preferencesView(prefs)
	bars := make([]contract.TasteBar, 0, len(tasteBars))
	for _, bar := range tasteBars {
		learned := neutral
		for _, tag := range bar.tags {
			if v, ok := taste.Tags[tag]; ok && !math.IsNaN(v) {
				learned = clamp01(v)
				break
			}
		}
		value := clamp01(saidWeight*bar.said(shown) + learnedWeight*learned)
		bars = append(bars, contract.TasteBar{Label: bar.label, Value: math.Round(value*100) / 100})
	}
	return contract.TasteProfile{Bars: bars}
}

// rated reads one trip type's rating as a share of 5.
func rated(key string) func(contract.Preferences) float64 {
	return func(p contract.Preferences) float64 { return ratingShare(p.Ratings, key) }
}

// social averages the usual company (anything but solo reads 0.7, solo 0.3)
// with how much they like big crowds.
func social(p contract.Preferences) float64 {
	company := 0.7
	if p.Company == contract.CompanySolo {
		company = 0.3
	}
	return (company + ratingShare(p.Ratings, "big_crowds")) / 2
}

func ratingShare(ratings contract.Ratings, key string) float64 {
	if v, ok := ratings[key]; ok && v >= 1 && v <= 5 {
		return float64(v) / 5
	}
	return neutral
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }
