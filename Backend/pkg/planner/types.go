package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/travel"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Place is a named point; HasCoord is false when only the name is known.
type Place struct {
	Name     string  `bson:"name" json:"name"`
	Lat      float64 `bson:"lat" json:"lat"`
	Lng      float64 `bson:"lng" json:"lng"`
	HasCoord bool    `bson:"hasCoord" json:"has_coord"`
}

// Point returns the coordinate, or nil when there is none.
func (p Place) Point() *travel.Point {
	if !p.HasCoord {
		return nil
	}
	return &travel.Point{Lat: p.Lat, Lng: p.Lng}
}

// PlaceAt builds a Place with a coordinate.
func PlaceAt(name string, p travel.Point) Place {
	return Place{Name: name, Lat: p.Lat, Lng: p.Lng, HasCoord: true}
}

// UserPrefs is the part of the user's preferences the planner reads.
type UserPrefs struct {
	Pace       string   // relaxed | balanced | packed (the user's usual pace; the request's wins)
	Flexible   bool     // "a bit over is OK": allows the budget relax step
	PreferFree bool     // soft; the budget picker decides FreeOnly
	AvoidTags  []string // taste.avoidTags: hard excludes
}

// UserContext is what the planner knows about the caller. The service
// layer builds it from the user document; the planner never reads Mongo
// user documents itself.
type UserContext struct {
	ID                string
	Catalog           string // activities | demo_activities
	City              string // catalog city slug, when known
	HomeBase          *Place // where plans start by default; the snap target in its city
	AgeBracket        string // 13_17 | 18_20 | 21_plus, or the contract's under_13 | teen | under_21 | adult
	Prefs             UserPrefs
	PositiveEmbedding []float64
	NegativeEmbedding []float64
	PositiveText      string // the profile texts the reranker reads; no rerank without one
	NegativeText      string
	LastLocation      *travel.Point
}

// NormalizeAgeBracket maps both vocabularies onto the catalog's. Unknown
// non-empty values are treated as the youngest bracket, which is the safe
// direction; empty means the default of 21_plus.
func NormalizeAgeBracket(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "21_plus", "adult":
		return "21_plus"
	case "18_20", "under_21":
		return "18_20"
	default:
		return "13_17"
	}
}

// Request is the planner's own view of the app's PlanRequest (and of the
// legacy shape, marked with Legacy). Instants are UTC; the legacy shape
// carries local date and clock strings that BuildSpec resolves in the
// plan's time zone.
type Request struct {
	Start, End Place
	Date       time.Time
	StartTime  time.Time
	BackBy     time.Time
	Range      string // walkable | transit | anywhere
	Ride       string // drive | cover | none
	OpenSeats  *int
	MoodText   string
	Tags       []string
	Budget     int // 0 free .. 3
	Who        string
	Pace       string
	Modes      []string

	// Legacy shape (Backend/ITINERARY_PLANNER.md).
	Legacy       bool
	LegacyDate   string // "2026-09-26"
	LegacyStart  string // "18:00"
	LegacyBackBy string // "23:00"
	RangeKm      float64
	BudgetCents  int64

	Raw json.RawMessage
}

// HardConstraints are filters every candidate must pass. They come from the
// mood text, the user's taste and the budget; age rules are applied by
// bracket in the query itself.
type HardConstraints struct {
	ExcludeCategories []string `bson:"excludeCategories" json:"exclude_categories"`
	ExcludeTags       []string `bson:"excludeTags" json:"exclude_tags"`
	RequireTags       []string `bson:"requireTags,omitempty" json:"require_tags,omitempty"`
	FreeOnly          bool     `bson:"freeOnly" json:"free_only"`
}

func (h HardConstraints) excludesCategory(c string) bool {
	return containsString(h.ExcludeCategories, c)
}

func (h HardConstraints) excludesAnyTag(tags []string) bool {
	for _, t := range tags {
		if containsString(h.ExcludeTags, t) {
			return true
		}
	}
	return false
}

// PlanSpec is the normalised request (§3 of the design).
type PlanSpec struct {
	UserID  string
	Catalog string
	City    string
	TZ      *time.Location

	Start, End         *travel.Point
	StartName, EndName string

	From, BackBy time.Time
	LocalDate    string

	Mode       travel.Mode
	DriveLabel string
	Range      string
	MaxLegKm   float64

	Budget Budget
	Pace   string // chill | balanced | packed

	Who       string
	OpenSeats int

	MoodText   string
	QuickPicks []string
	Facets     []Facet
	Hard       HardConstraints

	AgeBracket string
	AvoidTags  []string
	Flexible   bool

	Raw          json.RawMessage
	SnappedStart bool
}

// Window is the spec in the optimizer's terms.
func (s *PlanSpec) Window() itinerary.Window {
	return itinerary.Window{
		From: s.From, BackBy: s.BackBy, TZ: s.TZ,
		Start: s.Start, End: s.End,
		Mode: s.Mode, DriveLabel: s.DriveLabel, MaxLegKm: s.MaxLegKm,
		BudgetCents: s.Budget.TotalCents, Pace: s.Pace,
	}
}

// RequestError is a request the planner cannot serve; handlers answer 400
// with the message.
type RequestError struct{ Reason string }

func (e *RequestError) Error() string { return "invalid_request: " + e.Reason }

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortStrings(s []string) { sort.Strings(s) }

// uniqueStrings keeps the first occurrence of each value, in order.
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
