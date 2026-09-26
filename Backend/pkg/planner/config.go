// Package planner turns a plan request into itinerary options: it
// normalises the request, retrieves candidates with guaranteed filters,
// scores them, runs the iterative DAG loop over pkg/itinerary, persists the
// pool for paging, re-routing, alternatives and saving, and renders the
// app's shapes. Data access, embeddings and ML go through small interfaces
// (CandidateSource, EmbeddingSource, Scorer, SearchVectorizer, PoolStore,
// Clock) so every stage runs against fakes in tests.
package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"os"
	"strconv"
	"strings"
	"time"
)

// ScoreWeights are the plan Score terms (§5.2 of the design).
type ScoreWeights struct {
	Fit        float64
	Coverage   float64
	Variety    float64
	PaceFit    float64
	Travel     float64
	Idle       float64
	LateRisk   float64
	OverBudget float64
}

// Config holds the planner's knobs. FromEnv reads them from PLANNER_*;
// DefaultConfig gives the documented defaults.
type Config struct {
	Mode string // PLANNER: auto | dag | legacy (the handler's choice; app-shape requests never fall back)

	SoftBudget  time.Duration // stop starting new rounds after this
	HardTimeout time.Duration // the request context's deadline
	Rounds      int
	Epsilon     float64 // minimum top-3 score gain to keep iterating

	ShortlistN     int
	FacetQuota     int
	CategoryCap    int
	PhaseAEvents   int
	PhaseAPlaces   int
	EmbedFetchCap  int
	ExpansionLimit int
	MaxExpansions  int // expansions per round

	// MinPlaceRating drops places rated below it, and unrated places other
	// than hikes (trails rarely have ratings).
	MinPlaceRating float64
	// MinCandidates: when fewer candidates than this fit, the range is
	// relaxed once (twice the search radius) before scoring.
	MinCandidates int
	// SolverEvents and SolverPlaces cap what one solve sees: the best
	// series of each kind by utility.
	SolverEvents int
	SolverPlaces int

	MLTimeout     time.Duration
	SearchTimeout time.Duration // the search-profile call (ML_SEARCH_TIMEOUT_MS on the ML side)
	Jev           string        // off | async | sync
	JevTimeout    time.Duration
	JevTopK       int

	SearchWeight  float64
	DislikeLambda float64

	RangeKm      map[string]float64 // walkable, transit, anywhere
	WalkLegCapKm float64
	CitySnapKm   float64

	PoolTTL time.Duration
	RunTTL  time.Duration

	SeriesCap int
	Debug     bool

	FirstPage int // options on /plans/generate
	MorePage  int // options per /plans/generate/more page

	Weights   ScoreWeights
	Itinerary itinerary.Config
}

// DefaultConfig is the documented default for every knob.
func DefaultConfig() Config {
	it := itinerary.DefaultConfig()
	return Config{
		Mode:           "auto",
		SoftBudget:     5000 * time.Millisecond,
		HardTimeout:    9000 * time.Millisecond,
		Rounds:         3,
		Epsilon:        0.02,
		ShortlistN:     120,
		FacetQuota:     15,
		CategoryCap:    25,
		PhaseAEvents:   400,
		PhaseAPlaces:   600,
		EmbedFetchCap:  800,
		ExpansionLimit: 40,
		MaxExpansions:  2,
		MinPlaceRating: 4.0,
		MinCandidates:  5,
		SolverEvents:   25,
		SolverPlaces:   25,
		MLTimeout:      3000 * time.Millisecond,
		SearchTimeout:  5000 * time.Millisecond,
		Jev:            "async",
		JevTimeout:     45000 * time.Millisecond,
		JevTopK:        20,
		SearchWeight:   0.6,
		DislikeLambda:  0.5,
		RangeKm:        map[string]float64{"walkable": 2, "transit": 10, "anywhere": 25},
		WalkLegCapKm:   4,
		CitySnapKm:     60,
		PoolTTL:        6 * time.Hour,
		RunTTL:         72 * time.Hour,
		SeriesCap:      it.SeriesCap,
		FirstPage:      3,
		MorePage:       2,
		Weights: ScoreWeights{
			Fit: 0.45, Coverage: 0.15, Variety: 0.10, PaceFit: 0.10,
			Travel: 0.05, Idle: 0.05, LateRisk: 0.05, OverBudget: 0.05,
		},
		Itinerary: it,
	}
}

// FromEnv is DefaultConfig overridden by the PLANNER_* variables.
func FromEnv() Config {
	c := DefaultConfig()
	switch m := os.Getenv("PLANNER"); m {
	case "dag", "legacy", "auto":
		c.Mode = m
	}
	c.SoftBudget = envMillis("PLANNER_SOFT_BUDGET_MS", c.SoftBudget)
	c.HardTimeout = envMillis("PLANNER_HARD_TIMEOUT_MS", c.HardTimeout)
	c.Rounds = envInt("PLANNER_ROUNDS", c.Rounds)
	c.Epsilon = envFloat("PLANNER_EPSILON", c.Epsilon)
	c.ShortlistN = envInt("PLANNER_SHORTLIST_N", c.ShortlistN)
	c.FacetQuota = envInt("PLANNER_FACET_QUOTA", c.FacetQuota)
	c.CategoryCap = envInt("PLANNER_CATEGORY_CAP", c.CategoryCap)
	c.PhaseAEvents = envInt("PLANNER_PHASE_A_EVENTS", c.PhaseAEvents)
	c.PhaseAPlaces = envInt("PLANNER_PHASE_A_PLACES", c.PhaseAPlaces)
	c.EmbedFetchCap = envInt("PLANNER_EMBED_FETCH_CAP", c.EmbedFetchCap)
	c.ExpansionLimit = envInt("PLANNER_EXPANSION_LIMIT", c.ExpansionLimit)
	c.MaxExpansions = envInt("PLANNER_MAX_EXPANSIONS", c.MaxExpansions)
	c.MinPlaceRating = envFloat("PLANNER_MIN_PLACE_RATING", c.MinPlaceRating)
	c.MinCandidates = envInt("PLANNER_MIN_CANDIDATES", c.MinCandidates)
	c.SolverEvents = envInt("PLANNER_SOLVER_EVENTS", c.SolverEvents)
	c.SolverPlaces = envInt("PLANNER_SOLVER_PLACES", c.SolverPlaces)
	c.MLTimeout = envMillis("PLANNER_ML_TIMEOUT_MS", c.MLTimeout)
	c.SearchTimeout = envMillis("PLANNER_SEARCH_TIMEOUT_MS", c.SearchTimeout)
	switch j := strings.ToLower(os.Getenv("PLANNER_JEV")); j {
	case "off", "async", "sync":
		c.Jev = j
	}
	c.JevTimeout = envMillis("PLANNER_JEV_TIMEOUT_MS", c.JevTimeout)
	c.JevTopK = envInt("PLANNER_JEV_TOP_K", c.JevTopK)
	c.SearchWeight = envFloat("PLANNER_SEARCH_WEIGHT", c.SearchWeight)
	c.DislikeLambda = envFloat("PLANNER_DISLIKE_LAMBDA", c.DislikeLambda)
	if v := os.Getenv("PLANNER_RANGE_KM"); v != "" {
		c.RangeKm = parseRangeKm(v, c.RangeKm)
	}
	c.WalkLegCapKm = envFloat("PLANNER_WALK_LEG_CAP_KM", c.WalkLegCapKm)
	c.CitySnapKm = envFloat("PLANNER_CITY_SNAP_KM", c.CitySnapKm)
	c.PoolTTL = time.Duration(envFloat("PLANNER_POOL_TTL_H", c.PoolTTL.Hours()) * float64(time.Hour))
	c.RunTTL = time.Duration(envFloat("PLANNER_RUN_TTL_H", c.RunTTL.Hours()) * float64(time.Hour))
	c.SeriesCap = envInt("PLANNER_SERIES_CAP", c.SeriesCap)
	c.Itinerary.SeriesCap = c.SeriesCap
	c.Debug = os.Getenv("PLANNER_DEBUG") == "1"
	return c
}

// parseRangeKm accepts "walkable=2,transit=10,anywhere=25" or "2,10,25".
func parseRangeKm(v string, def map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, d := range def {
		out[k] = d
	}
	order := []string{"walkable", "transit", "anywhere"}
	for i, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, num := "", part
		if k, n, ok := strings.Cut(part, "="); ok {
			key, num = strings.TrimSpace(k), strings.TrimSpace(n)
		} else if i < len(order) {
			key = order[i]
		}
		if f, err := strconv.ParseFloat(num, 64); err == nil && f > 0 && key != "" {
			out[key] = f
		}
	}
	return out
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envMillis(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return def
}

// --- facets ---------------------------------------------------------------

// Facet is a soft "what kind of outing" request from a quick pick or the
// mood text. A stop covers it when its category is one of Cats or it
// shares a tag with Tags.
type Facet struct {
	Name string   `bson:"name" json:"name"`
	Tags []string `bson:"tags" json:"tags"`
	Cats []string `bson:"cats" json:"cats"`
}

// facetTable maps the app's quick picks to catalog vocabulary, in the order
// facets are reported.
var facetTable = []Facet{
	{Name: "Outdoors", Tags: []string{"outdoor", "nature"}, Cats: []string{"park", "garden", "hike", "viewpoint"}},
	{Name: "Food", Tags: []string{"food"}, Cats: []string{"restaurant", "cafe", "market", "bakery", "dessert", "food_hall"}},
	{Name: "Art", Tags: []string{"art"}, Cats: []string{"gallery", "museum"}},
	{Name: "Music", Tags: []string{"music"}, Cats: []string{"live_music"}},
	{Name: "Chill", Tags: []string{"low_energy"}},
	{Name: "Active", Tags: []string{"active", "high_energy"}, Cats: []string{"hike", "rec_venue", "sports_event"}},
	{Name: "Meet people", Tags: []string{"group"}, Cats: []string{"community_event", "class_workshop", "festival"}},
	{Name: "Nerdy", Tags: []string{"learning"}, Cats: []string{"class_workshop", "museum", "tour"}},
	{Name: "Nightlife", Tags: []string{"late_night", "drinks"}, Cats: []string{"bar", "nightclub", "brewery", "live_music", "comedy"}},
}

// FacetByName finds a quick pick's facet; matching ignores case, spaces
// and underscores ("meet_people" is "Meet people").
func FacetByName(name string) (Facet, bool) {
	key := facetKey(name)
	for _, f := range facetTable {
		if facetKey(f.Name) == key {
			return f, true
		}
	}
	return Facet{}, false
}

func facetKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	return strings.ReplaceAll(s, " ", "")
}

// Covers reports whether an activity satisfies the facet.
func (f Facet) Covers(a *models.Activity) bool {
	for _, c := range f.Cats {
		if a.Category == c {
			return true
		}
	}
	for _, t := range f.Tags {
		for _, at := range a.Tags {
			if at == t {
				return true
			}
		}
	}
	return false
}

// --- budgets ----------------------------------------------------------------

// Budget is the app's 0..3 budget level in planner terms. TotalCents 0 means
// unlimited; Tier 3 means every price tier is fine.
type Budget struct {
	Tier       int   `bson:"tier" json:"tier"`
	TotalCents int64 `bson:"totalCents" json:"total_cents"`
	FreeOnly   bool  `bson:"freeOnly" json:"free_only"`
}

// BudgetForLevel maps the app's budget picker: Free, $, $$, $$$.
func BudgetForLevel(level int) Budget {
	switch {
	case level <= 0:
		return Budget{Tier: 0, FreeOnly: true}
	case level == 1:
		return Budget{Tier: 1, TotalCents: 2500}
	case level == 2:
		return Budget{Tier: 2, TotalCents: 7000}
	}
	return Budget{Tier: 3}
}

// Upper bounds (whole currency units) of price tiers 0..3; above the last is tier 4.
var tierBounds = []float64{0, 15, 40, 80}

// --- cities -----------------------------------------------------------------

// City is a catalog city: its centre and time zone, plus the point plans
// start from when the request has no usable coordinate.
type City struct {
	Slug      string
	Name      string
	TZ        string
	Center    travel.Point
	Start     travel.Point
	StartName string
}

// cities mirrors dataingestion/cities/*.yaml. Saltlight's default start is
// Seaside Market Square; the others start at the city centre.
var cities = map[string]City{
	"atlanta":   {Slug: "atlanta", Name: "Atlanta", TZ: "America/New_York", Center: travel.Point{Lat: 33.7490, Lng: -84.3880}, Start: travel.Point{Lat: 33.7490, Lng: -84.3880}, StartName: "Downtown Atlanta"},
	"seattle":   {Slug: "seattle", Name: "Seattle", TZ: "America/Los_Angeles", Center: travel.Point{Lat: 47.6062, Lng: -122.3321}, Start: travel.Point{Lat: 47.6062, Lng: -122.3321}, StartName: "Downtown Seattle"},
	"sf":        {Slug: "sf", Name: "San Francisco", TZ: "America/Los_Angeles", Center: travel.Point{Lat: 37.7879, Lng: -122.4075}, Start: travel.Point{Lat: 37.7879, Lng: -122.4075}, StartName: "Union Square"},
	"nyc":       {Slug: "nyc", Name: "New York", TZ: "America/New_York", Center: travel.Point{Lat: 40.7359, Lng: -73.9911}, Start: travel.Point{Lat: 40.7359, Lng: -73.9911}, StartName: "Union Square"},
	"berlin":    {Slug: "berlin", Name: "Berlin", TZ: "Europe/Berlin", Center: travel.Point{Lat: 52.5200, Lng: 13.4050}, Start: travel.Point{Lat: 52.5200, Lng: 13.4050}, StartName: "Mitte"},
	"saltlight": {Slug: "saltlight", Name: "Saltlight Harbor", TZ: "America/New_York", Center: travel.Point{Lat: 31.3700, Lng: -81.4250}, Start: travel.Point{Lat: 31.3680, Lng: -81.4250}, StartName: "Seaside Market Square"},
}

// CityBySlug looks a city up by its catalog slug.
func CityBySlug(slug string) (City, bool) {
	c, ok := cities[strings.ToLower(strings.TrimSpace(slug))]
	return c, ok
}

// NearestCity finds the city whose centre is within maxKm of p; ties go to
// the closest, then the alphabetically first slug.
func NearestCity(p travel.Point, maxKm float64) (City, bool) {
	var best City
	bestKm := -1.0
	for _, slug := range citySlugs() {
		c := cities[slug]
		if d := travel.HaversineKm(p, c.Center); d <= maxKm && (bestKm < 0 || d < bestKm) {
			best, bestKm = c, d
		}
	}
	return best, bestKm >= 0
}

func citySlugs() []string {
	out := make([]string, 0, len(cities))
	for s := range cities {
		out = append(out, s)
	}
	sortStrings(out)
	return out
}

// Catalog names that may be used as collection names.
var catalogs = map[string]string{"activities": "atlanta", "demo_activities": "saltlight"}

// NormalizeCatalog validates a user's catalog against the allow-list; ok is
// false for anything else (callers fall back to "activities").
func NormalizeCatalog(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "activities", true
	}
	_, ok := catalogs[name]
	return name, ok
}

// DefaultCityForCatalog is the city a catalog belongs to when the user has
// none set: the demo catalog is Saltlight, the live one Atlanta.
func DefaultCityForCatalog(catalog string) string {
	if c, ok := catalogs[catalog]; ok {
		return c
	}
	return "atlanta"
}
