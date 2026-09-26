package facebook

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestCategoryTripTypes(t *testing.T) {
	cases := map[string][]string{
		// The contract's table.
		"Park": {tripOutdoors}, "Outdoor Recreation": {tripOutdoors}, "Campground": {tripOutdoors},
		"Nature Preserve": {tripOutdoors}, "Beach": {tripOutdoors},
		"Hiking Trail": {tripOutdoors, tripLongWalks},
		"Restaurant":   {tripFood}, "Café": {tripFood}, "Coffee Shop": {tripFood}, "Bakery": {tripFood},
		"Food & Beverage": {tripFood}, "Food Truck": {tripFood},
		"Museum": {tripMuseums}, "Art Gallery": {tripMuseums}, "Art Museum": {tripMuseums}, "Artist": {tripMuseums},
		"History Museum": {tripMuseums}, "Science Museum": {tripMuseums},
		"Musician/Band": {tripLiveMusic}, "Concert Venue": {tripLiveMusic}, "Live Music Venue": {tripLiveMusic},
		"Music Festival": {tripLiveMusic, tripBigCrowds},
		"Bar":            {tripNightlife}, "Night Club": {tripNightlife}, "Lounge": {tripNightlife}, "Pub": {tripNightlife},
		"Brewery": {tripNightlife}, "Comedy Club": {tripNightlife},
		"Sports Team": {tripSports}, "Sports League": {tripSports}, "Stadium": {tripSports, tripBigCrowds},
		"Gym/Physical Fitness Center": {tripSports}, "Climbing Gym": {tripSports}, "Bowling Alley": {tripSports},
		"Shopping Mall": {tripShopping}, "Clothing Store": {tripShopping}, "Flea Market": {tripShopping},
		"Farmers Market": {tripShopping, tripEarlyMornings}, "Bookstore": {tripShopping}, "Vintage Store": {tripShopping},
		"Festival": {tripBigCrowds}, "Amusement Park": {tripBigCrowds}, "Theme Park": {tripBigCrowds},
		"Walking Tour": {tripLongWalks}, "Botanical Garden": {tripLongWalks, tripOutdoors}, "Neighborhood": {tripLongWalks},
		"Yoga Studio": {tripEarlyMornings}, "Running Club": {tripEarlyMornings},

		// Real category names resolved by the keyword rules, overlaps included.
		"Italian Restaurant":            {tripFood},
		"  italian   RESTAURANT ":       {tripFood},
		"Amusement & Theme Park":        {tripBigCrowds},
		"Stadium, Arena & Sports Venue": {tripSports, tripBigCrowds},
		"Skate Park":                    {tripSports},
		"Dog Park":                      {tripOutdoors},
		"RV Park":                       {tripOutdoors},
		"Beer Garden":                   {tripNightlife},
		"Juice Bar":                     {tripFood},
		"Sushi Bar":                     {tripFood},
		"Wine Bar":                      {tripNightlife},
		"Sports Bar":                    {tripNightlife, tripSports},
		"Hookah Lounge":                 {tripNightlife},
		"Martial Arts School":           {tripSports},
		"Concert Tour":                  {tripLiveMusic},
		"Landmark & Historical Place":   {tripLongWalks},
		"Ski Resort":                    {tripOutdoors},
		"Winery/Vineyard":               {tripFood},

		// Never a trip type: generic, sensitive, or only a lookalike word.
		"Local Business": nil, "Politician": nil, "Political Organization": nil, "Church": nil,
		"Religious Organization": nil, "Hospital": nil, "Health/Beauty": nil, "Business Park": nil,
		"Parking Garage": nil, "Barber Shop": nil, "Makeup Artist": nil, "": nil,
	}
	for category, want := range cases {
		got := categoryTripTypes(category)
		if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%q → %v, want %v", category, got, want)
		}
	}
}

func TestEveryMappedTypeIsCanonical(t *testing.T) {
	check := func(where string, types []string) {
		for _, tt := range types {
			if !contract.IsTripType(tt) {
				t.Errorf("%s maps to %q, not a canonical trip type", where, tt)
			}
		}
	}
	for category, types := range exactCategories {
		if normalizeCategory(category) != category {
			t.Errorf("exact key %q is not normalized", category)
		}
		check(category, types)
	}
	for i, rule := range keywordRules {
		check(fmt.Sprintf("rule %d", i), rule.types)
	}
	for category := range interestLabels {
		if normalizeCategory(category) != category {
			t.Errorf("interest key %q is not normalized", category)
		}
	}
}

func TestRatingFormula(t *testing.T) {
	cases := []struct{ count, maxCount, want int }{
		{10, 10, 5}, {6, 6, 5}, {3, 6, 3}, {5, 6, 4}, {3, 8, 3}, {3, 12, 2}, {3, 30, 1}, {3, 24, 2},
		{0, 0, 1}, {7, 10, 4}, {1, 16, 1},
	}
	for _, c := range cases {
		if got := rating(c.count, c.maxCount); got != c.want {
			t.Errorf("rating(%d, %d) = %d, want %d", c.count, c.maxCount, got, c.want)
		}
	}
}

func pagesOf(categories ...string) []models.FacebookPage {
	out := make([]models.FacebookPage, 0, len(categories))
	for i, c := range categories {
		out = append(out, models.FacebookPage{ID: fmt.Sprint(i), Name: c, Category: c})
	}
	return out
}

func TestSuggestRatingsKeepsTypesWithThreePages(t *testing.T) {
	pages := pagesOf(
		"Park", "Park", "Park", "Hiking Trail", "Beach", "Park", // outdoors 6 (max), long walks 1
		"Coffee Shop", "Bakery", "Food Truck", // food 3
		"Bar", "Pub", // nightlife 2: below the threshold
		"Stadium", "Sports Team", "Climbing Gym", "Bowling Alley", // sports 4, big crowds 1
		"Local Business", "Politician",
	)
	// A Page counts once per type, even when two of its categories map to it.
	pages = append(pages, models.FacebookPage{ID: "multi", Category: "Local Business", CategoryList: []string{"Local Business", "Café", "Coffee Shop"}})
	got := suggestRatings(pages)
	want := map[string]int{tripOutdoors: 5, tripFood: 4, tripSports: 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %v, want %v", got, want)
	}
	if got := suggestRatings(pagesOf("Park", "Bar")); len(got) != 0 {
		t.Fatalf("two Pages suggested %v", got)
	}
	if got := suggestRatings(nil); got == nil || len(got) != 0 {
		t.Fatalf("no Pages: %v", got)
	}
}

func TestInterests(t *testing.T) {
	pages := pagesOf(
		"Hiking Trail", "Hiking Trail", "Hiking Trail",
		"Coffee Shop", "Café", "Coffee Shop",
		"Musician/Band", "Musician/Band",
		"Italian Restaurant", "Vegetarian/Vegan Restaurant",
		"Food Truck", "Park", "Art Gallery", "Yoga Studio", "Board Game",
		"Local Business", "Politician", "Church", "Hospital", "Insurance Agent",
	)
	got := interestsFor(pages)
	want := []string{"Coffee", "Hiking", "Bands", "Art", "Board games", "Italian food", "Parks", "Street food"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interests = %v, want %v", got, want)
	}
	if len(got) > maxInterests {
		t.Fatalf("more than %d interests", maxInterests)
	}
	for _, sensitive := range []string{"Politician", "Church", "Hospital", "Local business", "Insurance agent"} {
		if slices.Contains(got, sensitive) {
			t.Errorf("%q must never be an interest", sensitive)
		}
	}
	// Fewer than three distinct interests say too little.
	if got := interestsFor(pagesOf("Coffee Shop", "Café", "Park", "Local Business")); len(got) != 0 {
		t.Fatalf("two interests returned %v", got)
	}
	if got := interestsFor(pagesOf("Coffee Shop", "Park", "Bar")); !reflect.DeepEqual(got, []string{"Bars", "Coffee", "Parks"}) {
		t.Fatalf("three interests: %v", got)
	}
	// The fallback words for a mapped category without a label.
	if got := interestFor("hookah lounge"); got != "Hookah lounge" {
		t.Fatalf("fallback label %q", got)
	}
}
