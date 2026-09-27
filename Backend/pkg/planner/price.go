package planner

import (
	"Backend/pkg/models"
	"math"
	"regexp"
	"slices"
)

// A missing price is unknown, never free. The ingested documents carry
// either a full price object (min/max/tier/isFree) or null; the legacy seed
// used cents. These helpers are the one place that reads the price.

// activityCost returns the cheapest known price in cents. known is false
// when the document states no amount: no price at all, or only a tier.
func activityCost(a *models.Activity) (cents int64, known bool) {
	p := a.Price
	if p == nil {
		return 0, false
	}
	switch {
	case p.Min != nil && !math.IsNaN(*p.Min) && !math.IsInf(*p.Min, 0):
		if *p.Min <= 0 {
			return 0, true
		}
		return int64(math.Round(*p.Min * 100)), true
	case p.IsFree:
		return 0, true
	case p.Cents > 0:
		return p.Cents, true
	}
	return 0, false
}

// activityTier returns the price tier (0 free .. 4). known is false when
// nothing in the document says what it costs.
func activityTier(a *models.Activity) (tier int, known bool) {
	p := a.Price
	if p == nil {
		return 0, false
	}
	switch {
	case p.Tier > 0:
		return p.Tier, true
	case p.Min != nil:
		return tierForAmount(*p.Min), true
	case p.IsFree:
		return 0, true
	case p.Cents > 0:
		return tierForAmount(float64(p.Cents) / 100), true
	}
	return 0, false
}

func tierForAmount(units float64) int {
	for t, bound := range tierBounds {
		if units <= bound {
			return t
		}
	}
	return len(tierBounds)
}

// isKnownFree is true only when the document says so.
func isKnownFree(a *models.Activity) bool {
	c, known := activityCost(a)
	return known && c == 0
}

// Categories that are free unless the document says otherwise: with an
// unknown price these still count for a free-only plan.
var freeIfUnknownCategories = map[string]bool{
	"park": true, "hike": true, "landmark": true, "viewpoint": true,
	"garden": true, "market": true, "community_event": true,
}

// FreeIfUnknownCategories lists the categories a free-only plan keeps when
// the price is unknown, sorted.
func FreeIfUnknownCategories() []string {
	out := make([]string, 0, len(freeIfUnknownCategories))
	for c := range freeIfUnknownCategories {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Ticketmaster add-on listings that are not an outing.
var addOnListingRe = regexp.MustCompile(`(?i)parking|express entry|not a concert ticket`)

func isAddOnListing(a *models.Activity) bool {
	return addOnListingRe.MatchString(a.Name)
}

// Price symbol for a tier: "Free", "$", "$$", "$$$".
func tierSymbol(tier int) string {
	switch {
	case tier <= 0:
		return "Free"
	case tier == 1:
		return "$"
	case tier == 2:
		return "$$"
	}
	return "$$$"
}

// Name patterns that mark adults-only listings, matched case-insensitively
// (the same source goes into the Mongo $regex with option "i").
const (
	adultName21 = `21\+`
	adultName18 = `(?:18\+|21\+)`
)

var (
	name21Re = regexp.MustCompile(`(?i)` + adultName21)
	name18Re = regexp.MustCompile(`(?i)` + adultName18)
)

// AgeRules are the exclusions an age bracket implies. The store turns them
// into query conditions and the planner re-checks them with Blocks, so the
// two can never disagree.
type AgeRules struct {
	ExcludeTags       []string
	ExcludeCategories []string
	NamePattern       string // matched case-insensitively; empty for none
	nameRe            *regexp.Regexp
}

// AgeRulesFor follows §4.1: 21_plus filters nothing; 18_20 drops 21+ tags,
// bars, nightclubs and "21+" names; 13_17 also drops "18+" names and
// anything tagged drinks.
func AgeRulesFor(bracket string) AgeRules {
	switch NormalizeAgeBracket(bracket) {
	case "18_20":
		return AgeRules{ExcludeTags: []string{"21_plus"}, ExcludeCategories: []string{"bar", "brewery", "nightclub"}, NamePattern: adultName21, nameRe: name21Re}
	case "13_17":
		return AgeRules{ExcludeTags: []string{"21_plus", "drinks"}, ExcludeCategories: []string{"bar", "brewery", "nightclub"}, NamePattern: adultName18, nameRe: name18Re}
	}
	return AgeRules{}
}

// Blocks reports whether the bracket may not see the activity.
func (r AgeRules) Blocks(a *models.Activity) bool {
	if slices.Contains(r.ExcludeCategories, a.Category) {
		return true
	}
	for _, t := range a.Tags {
		if slices.Contains(r.ExcludeTags, t) {
			return true
		}
	}
	return r.nameRe != nil && r.nameRe.MatchString(a.Name)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
