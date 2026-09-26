package facebook

import (
	"Backend/pkg/models"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Suggested ratings and interests from liked Pages (API_CONTRACT.md,
// "Backend work: Facebook connector", step 4). A Page counts once for every
// trip type one of its categories maps to; a trip type is suggested only with
// at least minPagesPerType Pages behind it, rated
// clamp(1, 5, round(1 + 4·count/maxCount)). The ML team can replace this.

// Trip types are the app's canonical rating keys (contract.TripTypes).
const (
	tripOutdoors      = "outdoors"
	tripFood          = "food"
	tripMuseums       = "museums"
	tripLiveMusic     = "live_music"
	tripNightlife     = "nightlife"
	tripSports        = "sports"
	tripShopping      = "shopping"
	tripBigCrowds     = "big_crowds"
	tripEarlyMornings = "early_mornings"
	tripLongWalks     = "long_walks"
)

const (
	minPagesPerType = 3
	minInterests    = 3
	maxInterests    = 8
)

// exactCategories maps Facebook Page categories (lowercased) to trip types;
// it holds the contract's table plus the names that need more than one type
// or would be misread by the keyword rules.
var exactCategories = map[string][]string{
	"park":               {tripOutdoors},
	"state park":         {tripOutdoors},
	"national park":      {tripOutdoors},
	"dog park":           {tripOutdoors},
	"outdoor recreation": {tripOutdoors},
	"campground":         {tripOutdoors},
	"nature preserve":    {tripOutdoors},
	"beach":              {tripOutdoors},
	"hiking trail":       {tripOutdoors, tripLongWalks},

	"restaurant":           {tripFood},
	"café":                 {tripFood},
	"cafe":                 {tripFood},
	"coffee shop":          {tripFood},
	"bakery":               {tripFood},
	"food & beverage":      {tripFood},
	"food truck":           {tripFood},
	"juice bar":            {tripFood},
	"smoothie & juice bar": {tripFood},
	"salad bar":            {tripFood},
	"bar & grill":          {tripFood, tripNightlife},
	"gastropub":            {tripFood, tripNightlife},

	"museum":         {tripMuseums},
	"art museum":     {tripMuseums},
	"art gallery":    {tripMuseums},
	"artist":         {tripMuseums},
	"history museum": {tripMuseums},
	"science museum": {tripMuseums},

	"musician/band":    {tripLiveMusic},
	"musician":         {tripLiveMusic},
	"band":             {tripLiveMusic},
	"concert venue":    {tripLiveMusic},
	"live music venue": {tripLiveMusic},
	"music venue":      {tripLiveMusic},
	"music festival":   {tripLiveMusic, tripBigCrowds},
	"jazz club":        {tripLiveMusic, tripNightlife},

	"bar":         {tripNightlife},
	"night club":  {tripNightlife},
	"lounge":      {tripNightlife},
	"pub":         {tripNightlife},
	"brewery":     {tripNightlife},
	"comedy club": {tripNightlife},
	"sports bar":  {tripNightlife, tripSports},

	"sports team":                   {tripSports},
	"professional sports team":      {tripSports},
	"amateur sports team":           {tripSports},
	"school sports team":            {tripSports},
	"sports league":                 {tripSports},
	"stadium":                       {tripSports, tripBigCrowds},
	"arena":                         {tripSports, tripBigCrowds},
	"stadium, arena & sports venue": {tripSports, tripBigCrowds},
	"gym/physical fitness center":   {tripSports},
	"climbing gym":                  {tripSports},
	"bowling alley":                 {tripSports},

	"shopping mall":              {tripShopping},
	"shopping & retail":          {tripShopping},
	"clothing store":             {tripShopping},
	"farmers market":             {tripShopping, tripEarlyMornings},
	"flea market":                {tripShopping},
	"bookstore":                  {tripShopping},
	"book store":                 {tripShopping},
	"vintage store":              {tripShopping},
	"thrift & consignment store": {tripShopping},

	"festival":               {tripBigCrowds},
	"amusement park":         {tripBigCrowds},
	"theme park":             {tripBigCrowds},
	"amusement & theme park": {tripBigCrowds},
	"water park":             {tripBigCrowds},

	"walking tour":                {tripLongWalks},
	"botanical garden":            {tripLongWalks, tripOutdoors},
	"neighborhood":                {tripLongWalks},
	"tour agency":                 {tripLongWalks},
	"landmark & historical place": {tripLongWalks},

	"yoga studio":       {tripEarlyMornings},
	"running club":      {tripEarlyMornings},
	"pilates studio":    {tripEarlyMornings},
	"meditation center": {tripEarlyMornings},
}

// keywordRule maps a category containing any of words (whole words, in
// order) to types; nil types means the category never counts.
type keywordRule struct {
	words []string
	types []string
}

// keywordRules cover the rest of Facebook's ~1,500 categories ("Italian
// Restaurant", "Hookah Lounge"). The first rule with a matching word wins, so
// the order resolves overlaps ("Skate Park" is sports, "Beer Garden"
// nightlife, "Business Park" nothing).
var keywordRules = []keywordRule{
	// Sensitive or unrelated to going out: never a trip type or an interest.
	{words: []string{"politician", "political", "politics", "government", "religious", "religion", "church", "mosque",
		"synagogue", "temple", "hospital", "medical", "clinic", "doctor", "dentist", "pharmacy", "health", "therapist",
		"counselor", "business park", "office park", "industrial park", "trailer park"}},
	{words: []string{"music festival"}, types: []string{tripLiveMusic, tripBigCrowds}},
	{words: []string{"festival", "theme park", "amusement", "water park", "fairground", "county fair", "state fair",
		"carnival", "convention center"}, types: []string{tripBigCrowds}},
	{words: []string{"musician", "band", "concert", "music venue", "live music", "orchestra", "symphony", "choir", "opera"},
		types: []string{tripLiveMusic}},
	{words: []string{"museum", "gallery", "sculpture", "exhibit", "exhibition", "planetarium", "aquarium"},
		types: []string{tripMuseums}},
	{words: []string{"yoga", "pilates", "running", "run club", "meditation", "barre"}, types: []string{tripEarlyMornings}},
	{words: []string{"martial arts", "sports", "sport", "team", "league", "stadium", "arena", "gym", "fitness", "athlete",
		"golf", "tennis", "soccer", "basketball", "baseball", "football", "hockey", "bowling", "climbing", "boxing",
		"skate", "skating", "racetrack"}, types: []string{tripSports}},
	{words: []string{"restaurant", "café", "cafe", "coffee", "bakery", "food", "pizza", "pizzeria", "diner", "brunch",
		"breakfast", "dessert", "ice cream", "donut", "doughnut", "tea", "juice", "smoothie", "deli", "bagel", "sandwich",
		"steakhouse", "barbecue", "bbq", "burger", "taco", "sushi", "ramen", "noodle", "salad", "seafood", "oyster",
		"creperie", "patisserie", "chocolate", "winery", "eatery", "bistro", "brasserie", "caterer"},
		types: []string{tripFood}},
	{words: []string{"bar", "pub", "night club", "nightclub", "nightlife", "lounge", "brewery", "brewpub", "taproom",
		"distillery", "comedy", "comedian", "karaoke", "cocktail", "speakeasy", "tavern", "beer garden", "hookah",
		"dance club"}, types: []string{tripNightlife}},
	{words: []string{"hiking", "trail", "trails", "greenway"}, types: []string{tripOutdoors, tripLongWalks}},
	{words: []string{"park", "parks", "beach", "lake", "campground", "camping", "nature", "outdoor", "outdoors",
		"mountain", "river", "forest", "preserve", "wildlife", "garden", "gardens", "ski", "kayak", "kayaking", "canoe",
		"surf", "surfing", "fishing", "marina", "zoo", "island", "waterfall", "canyon"}, types: []string{tripOutdoors}},
	{words: []string{"shopping", "mall", "boutique", "clothing", "apparel", "bookstore", "book store", "flea market",
		"thrift", "vintage", "consignment", "antique", "antiques", "record store", "gift shop", "market", "outlet",
		"department store", "shoe store", "jewelry"}, types: []string{tripShopping}},
	{words: []string{"walking tour", "tour", "tours", "sightseeing", "neighborhood", "landmark", "historical", "monument",
		"scenic", "boardwalk", "promenade", "waterfront", "pier"}, types: []string{tripLongWalks}},
}

// interestLabels are the plain words shown for common categories ("Coffee
// Shop" → "Coffee"). Other categories become interests only when they map
// to a trip type, so generic ("Local Business") and sensitive categories
// never show up.
var interestLabels = map[string]string{
	"hiking trail": "Hiking", "outdoor recreation": "The outdoors", "park": "Parks", "state park": "Parks",
	"national park": "National parks", "dog park": "Dogs", "beach": "Beaches", "campground": "Camping",
	"nature preserve": "Nature", "lake": "Lakes", "mountain": "Mountains", "botanical garden": "Gardens",
	"garden": "Gardens", "zoo": "Zoos", "aquarium": "Aquariums", "ski resort": "Skiing",

	"restaurant": "Restaurants", "café": "Coffee", "cafe": "Coffee", "coffee shop": "Coffee", "coffee roaster": "Coffee",
	"tea room": "Tea", "bakery": "Baked goods", "dessert shop": "Desserts", "ice cream shop": "Ice cream",
	"donut shop": "Donuts", "food truck": "Street food", "food stand": "Street food", "food & beverage": "Food",
	"food & beverage company": "Food", "pizza place": "Pizza", "breakfast & brunch restaurant": "Brunch",
	"fast food restaurant": "Fast food", "vegetarian/vegan restaurant": "Vegetarian food", "winery/vineyard": "Wine",
	"wine bar": "Wine",

	"museum": "Museums", "art museum": "Art", "art gallery": "Art", "artist": "Art", "history museum": "History",
	"science museum": "Science",

	"musician/band": "Bands", "musician": "Musicians", "band": "Bands", "concert venue": "Concerts",
	"live music venue": "Live music", "music venue": "Live music", "music festival": "Music festivals",
	"festival": "Festivals", "jazz club": "Jazz",

	"bar": "Bars", "pub": "Pubs", "irish pub": "Pubs", "night club": "Nightlife", "dance club": "Dancing",
	"lounge": "Lounges", "brewery": "Craft beer", "cocktail bar": "Cocktails", "comedy club": "Comedy",
	"comedian": "Comedy", "karaoke": "Karaoke", "sports bar": "Sports bars",

	"sports team": "Sports", "professional sports team": "Sports", "amateur sports team": "Sports",
	"school sports team": "Sports", "sports league": "Sports", "athlete": "Sports", "stadium": "Live sports",
	"arena": "Live sports", "stadium, arena & sports venue": "Live sports", "gym/physical fitness center": "Fitness",
	"climbing gym": "Climbing", "bowling alley": "Bowling", "golf course & country club": "Golf",

	"shopping mall": "Shopping", "shopping & retail": "Shopping", "shopping district": "Shopping",
	"clothing store": "Fashion", "clothing (brand)": "Fashion", "farmers market": "Farmers markets",
	"flea market": "Flea markets", "bookstore": "Books", "book store": "Books", "book": "Books", "author": "Books",
	"vintage store": "Vintage", "thrift & consignment store": "Vintage", "record store": "Records",

	"amusement park": "Theme parks", "theme park": "Theme parks", "amusement & theme park": "Theme parks",
	"water park": "Water parks",

	"walking tour": "Walking tours", "tour agency": "Tours", "neighborhood": "Neighborhoods",
	"landmark & historical place": "Landmarks", "historical place": "History",

	"yoga studio": "Yoga", "pilates studio": "Pilates", "meditation center": "Meditation", "running club": "Running",

	"video game": "Video games", "games/toys": "Games", "board game": "Board games", "movie": "Movies",
	"movie theater": "Movies", "tv show": "TV", "podcast": "Podcasts", "photographer": "Photography",
	"dance studio": "Dance", "theatre": "Theater", "theater": "Theater", "performing arts": "Theater",
	"travel company": "Travel", "travel agency": "Travel", "arcade": "Arcades", "escape game room": "Escape rooms",
}

// normalizeCategory lowercases and collapses whitespace.
func normalizeCategory(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// categoryWords is s as " word word " (letters and digits only), for
// whole-word matching.
func categoryWords(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	gap := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			gap = false
		} else if !gap {
			b.WriteByte(' ')
			gap = true
		}
	}
	if !gap {
		b.WriteByte(' ')
	}
	return b.String()
}

// matchRule is the first keyword rule matching a normalized category.
func matchRule(norm string) (keywordRule, bool) {
	w := categoryWords(norm)
	for _, rule := range keywordRules {
		for _, word := range rule.words {
			if strings.Contains(w, " "+word+" ") {
				return rule, true
			}
		}
	}
	return keywordRule{}, false
}

// categoryTripTypes maps one Page category to trip types (nil: none).
func categoryTripTypes(category string) []string {
	norm := normalizeCategory(category)
	if norm == "" {
		return nil
	}
	if types, ok := exactCategories[norm]; ok {
		return types
	}
	if rule, ok := matchRule(norm); ok {
		return rule.types
	}
	return nil
}

// pageCategories is the Page's category plus its category list, normalized
// and without repeats.
func pageCategories(p models.FacebookPage) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range append([]string{p.Category}, p.CategoryList...) {
		if norm := normalizeCategory(c); norm != "" && !seen[norm] {
			seen[norm] = true
			out = append(out, norm)
		}
	}
	return out
}

// pageTripTypes is every trip type any of the Page's categories maps to.
func pageTripTypes(p models.FacebookPage) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range pageCategories(p) {
		for _, t := range categoryTripTypes(c) {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// suggestRatings counts Pages per trip type and rates the types with at
// least minPagesPerType of them.
func suggestRatings(pages []models.FacebookPage) map[string]int {
	counts := map[string]int{}
	for _, p := range pages {
		for _, t := range pageTripTypes(p) {
			counts[t]++
		}
	}
	maxCount := 0
	for _, n := range counts {
		maxCount = max(maxCount, n)
	}
	out := map[string]int{}
	for t, n := range counts {
		if n >= minPagesPerType {
			out[t] = rating(n, maxCount)
		}
	}
	return out
}

// rating is clamp(1, 5, round(1 + 4·count/maxCount)).
func rating(count, maxCount int) int {
	if maxCount <= 0 {
		return 1
	}
	r := int(math.Round(1 + 4*float64(count)/float64(maxCount)))
	return min(5, max(1, r))
}

// interestFor is the plain words for one category, "" when it is not an
// interest (generic, sensitive, or unrelated to going out).
func interestFor(norm string) string {
	if label, ok := interestLabels[norm]; ok {
		return label
	}
	if len(categoryTripTypes(norm)) == 0 {
		return ""
	}
	// "Italian Restaurant" → "Italian food"
	if cuisine, ok := strings.CutSuffix(norm, " restaurant"); ok {
		cuisine, _, _ = strings.Cut(cuisine, "/")
		if cuisine = strings.TrimSpace(cuisine); cuisine != "" {
			return sentenceCase(cuisine + " food")
		}
	}
	return sentenceCase(cleanCategory(norm))
}

// cleanCategory drops parenthesized parts and alternatives after "/"
// ("clothing (brand)" → "clothing", "musician/band" → "musician").
func cleanCategory(norm string) string {
	if i := strings.Index(norm, "("); i >= 0 {
		norm = norm[:i]
	}
	norm, _, _ = strings.Cut(norm, "/")
	return strings.Join(strings.Fields(norm), " ")
}

func sentenceCase(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// interestsFor is the 3–8 interests most Pages share, most common first
// (ties alphabetical); fewer than three distinct ones say too little, so the
// answer is then empty.
func interestsFor(pages []models.FacebookPage) []string {
	counts := map[string]int{}
	for _, p := range pages {
		seen := map[string]bool{}
		for _, c := range pageCategories(p) {
			if label := interestFor(c); label != "" && !seen[label] {
				seen[label] = true
				counts[label]++
			}
		}
	}
	if len(counts) < minInterests {
		return []string{}
	}
	labels := make([]string, 0, len(counts))
	for label := range counts {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool {
		if counts[labels[i]] != counts[labels[j]] {
			return counts[labels[i]] > counts[labels[j]]
		}
		return labels[i] < labels[j]
	})
	return labels[:min(maxInterests, len(labels))]
}
