package itineraries

import (
	"Backend/pkg/models"
	"slices"
)

// earlyMorningHour: a stop that starts before 9 AM (plan time zone) speaks
// to early_mornings.
const earlyMorningHour = 9

// tasteByCategory maps catalog categories (dataingestion) to taste tags:
// the trip-type keys of Preferences.ratings, plus social, which the Social
// bar of GET /me/taste-profile reads before big_crowds.
var tasteByCategory = map[string][]string{
	"park":            {"outdoors"},
	"garden":          {"outdoors"},
	"viewpoint":       {"outdoors"},
	"zoo_aquarium":    {"outdoors"},
	"hike":            {"outdoors", "long_walks"},
	"tour":            {"museums", "long_walks"},
	"museum":          {"museums"},
	"gallery":         {"museums"},
	"landmark":        {"museums"},
	"theater":         {"museums"},
	"restaurant":      {"food"},
	"cafe":            {"food"},
	"market":          {"food", "shopping"},
	"shopping":        {"shopping"},
	"live_music":      {"live_music"},
	"bar":             {"nightlife"},
	"nightclub":       {"nightlife", "big_crowds"},
	"comedy":          {"nightlife"},
	"sports_event":    {"sports", "big_crowds"},
	"rec_venue":       {"sports"},
	"festival":        {"social", "big_crowds", "live_music"},
	"community_event": {"social"},
	"class_workshop":  {"social"},
}

// tasteByTag maps catalog tags to trip-type keys.
var tasteByTag = map[string][]string{
	"outdoor":    {"outdoors"},
	"outdoors":   {"outdoors"},
	"nature":     {"outdoors"},
	"hiking":     {"outdoors", "long_walks"},
	"food":       {"food"},
	"art":        {"museums"},
	"music":      {"live_music"},
	"late_night": {"nightlife"},
	"drinks":     {"nightlife"},
	"21_plus":    {"nightlife"},
	"group":      {"social"},
}

// ratingTagTaste is what the app's rating tags (Rating.tagOptions) say on
// their own: "Too crowded" pulls big_crowds to 0, "Great people" pulls
// social to 1.
var ratingTagTaste = map[string]struct {
	key    string
	target float64
}{
	"too crowded":  {"big_crowds", 0},
	"great people": {"social", 1},
}

// tasteKeys is the trip types a catalog activity speaks to, in a stable order.
func tasteKeys(a *models.Activity) []string {
	var keys []string
	add := func(ks []string) {
		for _, k := range ks {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	add(tasteByCategory[a.Category])
	for _, tag := range a.Tags {
		add(tasteByTag[tag])
	}
	return keys
}
