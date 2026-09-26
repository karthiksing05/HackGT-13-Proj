package planner

import (
	"regexp"
	"strings"
)

// Mood text is read deterministically: a short table of negation and
// keyword rules, no model. Negated phrases become hard excludes; positive
// keywords only add soft facets (the search embedding carries the rest).

// negationWords start a negated phrase; up to two words may sit between
// them and the thing being refused ("no loud bars", "nothing too outdoorsy").
const negationWords = `(?:no|not|avoid|without|skip|skipping|nothing|never|don'?t want|dont want|do not want|none of)`

type negationRule struct {
	synonyms string // regexp alternation
	cats     []string
	tags     []string
	exact    *regexp.Regexp // ^(?:synonyms)$, built once
}

// Each rule maps a refused thing to catalog vocabulary. Order matters only
// for logging.
var negationRules = []negationRule{
	{synonyms: `bars?|pubs?|drinks?|drinking|alcohol|booze|boozy|cocktails?`, cats: []string{"bar", "nightclub"}, tags: []string{"drinks"}},
	{synonyms: `clubs?|clubbing|nightclubs?|dancing`, cats: []string{"nightclub"}},
	{synonyms: `nightlife|late[- ]nights?|going out late`, cats: []string{"bar", "nightclub"}, tags: []string{"late_night"}},
	{synonyms: `outdoors?y?|outside|nature|hik(?:e|es|ing)|parks?|trails?`, tags: []string{"outdoor"}},
	{synonyms: `indoors?|inside`, tags: []string{"indoor"}},
	{synonyms: `art|arts|museums?|galler(?:y|ies)|exhibits?|exhibitions?`, cats: []string{"museum", "gallery"}, tags: []string{"art"}},
	{synonyms: `music|concerts?|gigs?|live bands?|shows?`, cats: []string{"live_music"}, tags: []string{"music"}},
	{synonyms: `food|eating|restaurants?|dinner|lunch|brunch|meals?`, cats: []string{"restaurant", "cafe"}, tags: []string{"food"}},
	{synonyms: `learning|lectures?|workshops?|classes|nerdy|educational`, cats: []string{"class_workshop"}, tags: []string{"learning"}},
	{synonyms: `sports?|games?|athletic|working out|workouts?`, cats: []string{"sports_event", "rec_venue"}},
	{synonyms: `touristy|tourist traps?|sightseeing`, cats: []string{"tour", "landmark"}, tags: []string{"touristy"}},
	{synonyms: `families|family stuff|kids? stuff|kid[- ]friendly|family[- ]friendly`, tags: []string{"family"}},
}

// Positive rules: text that on its own means a hard constraint.
var (
	familyRe = regexp.MustCompile(`\b(?:kids?|children|family|families|my (?:son|daughter|niece|nephew)|toddlers?|teens?)\b`)
	soberRe  = regexp.MustCompile(`\b(?:sober|alcohol[- ]free|dry night|not drinking|no drinking|no booze|no alcohol)\b`)
	freeRe   = regexp.MustCompile(`\b(?:broke|no money|for free|free (?:stuff|things|events|activities|only|options|ideas)|only free|keep it free|nothing that costs|can'?t spend|zero budget|no budget)\b`)
)

// facetRules add soft facets from positive mentions. Negated spans are
// blanked before these run, so "no bars" never suggests Nightlife.
var facetRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"Outdoors", regexp.MustCompile(`\b(?:outdoors?y?|outside|nature|hik(?:e|es|ing)|parks?|trails?|beach|sunset|sunrise|fresh air|garden|by the water|waterfront|walk along)\b`)},
	{"Food", regexp.MustCompile(`\b(?:food|eat|eating|restaurants?|dinner|lunch|brunch|snacks?|tacos?|pizza|noodles|oysters?|seafood|coffee|cafes?|bites?|hungry)\b`)},
	{"Art", regexp.MustCompile(`\b(?:art|arts|arty|museums?|galler(?:y|ies)|murals?|exhibits?|exhibitions?|creative|paint(?:ing)?)\b`)},
	{"Music", regexp.MustCompile(`\b(?:music|concerts?|gigs?|live bands?|jazz|dj|djs|band|singer)\b`)},
	{"Chill", regexp.MustCompile(`\b(?:chill|chilled|relax|relaxed|relaxing|low[- ]key|lowkey|calm|quiet|slow|lazy|easy|mellow|unwind|cozy|cosy)\b`)},
	{"Active", regexp.MustCompile(`\b(?:active|hik(?:e|es|ing)|run|running|climb|climbing|bike|biking|cycling|sporty|workout|kayak|kayaking|paddle|swim|swimming|surf|surfing|energetic|move)\b`)},
	{"Meet people", regexp.MustCompile(`\b(?:meet (?:new )?people|meet someone|new people|social|socialise|socialize|mingle|make friends|meetup|community|strangers)\b`)},
	{"Nerdy", regexp.MustCompile(`\b(?:nerdy|nerd|learn|learning|workshop|lecture|science|trivia|books?|puzzles?|board games?|history|talk|class)\b`)},
	{"Nightlife", regexp.MustCompile(`\b(?:nightlife|bars?|drinks?|clubbing|club|clubs|late night|late-night|dancing|party|cocktails?|rooftop bar|night out)\b`)},
}

var (
	negationRe     = buildNegationRe()
	whitespaceRe   = regexp.MustCompile(`\s+`)
	mixedPunctRe   = regexp.MustCompile("[’‘`]")
	standaloneFree = regexp.MustCompile(`^\s*(?:free|free stuff|something free)\s*[.!]?\s*$`)
)

func buildNegationRe() *regexp.Regexp {
	var alts []string
	for i := range negationRules {
		r := &negationRules[i]
		r.exact = regexp.MustCompile(`^(?:` + r.synonyms + `)$`)
		alts = append(alts, r.synonyms)
	}
	return regexp.MustCompile(`\b` + negationWords + `\s+(?:\S+\s+){0,2}?(` + strings.Join(alts, "|") + `)\b`)
}

// ExtractMoodConstraints reads the mood text and quick picks. Hard
// constraints come from negations ("no bars"), family mentions, sobriety
// and "free"; facets come from the quick picks (in request order) and then
// from positive keywords in the text. Deterministic and cheap enough to log.
func ExtractMoodConstraints(mood string, picks []string) (HardConstraints, []Facet) {
	var hard HardConstraints
	var facets []Facet
	have := map[string]bool{}
	addFacet := func(f Facet) {
		if !have[f.Name] {
			have[f.Name] = true
			facets = append(facets, f)
		}
	}
	for _, p := range picks {
		if f, ok := FacetByName(p); ok {
			addFacet(f)
		}
	}

	text := normalizeMood(mood)
	if text == "" {
		return hard, facets
	}

	// Negations first; the matched spans are blanked so they cannot also
	// read as a positive wish.
	blanked := text
	for _, m := range negationRe.FindAllStringSubmatchIndex(text, -1) {
		phrase := text[m[2]:m[3]]
		for _, r := range negationRules {
			if r.exact.MatchString(phrase) {
				hard.ExcludeCategories = append(hard.ExcludeCategories, r.cats...)
				hard.ExcludeTags = append(hard.ExcludeTags, r.tags...)
				break
			}
		}
		blanked = blanked[:m[0]] + strings.Repeat(" ", m[1]-m[0]) + blanked[m[1]:]
	}

	if familyRe.MatchString(blanked) {
		hard.ExcludeTags = append(hard.ExcludeTags, "21_plus")
		hard.ExcludeCategories = append(hard.ExcludeCategories, "bar", "nightclub")
	}
	if soberRe.MatchString(text) {
		hard.ExcludeTags = append(hard.ExcludeTags, "drinks")
		hard.ExcludeCategories = append(hard.ExcludeCategories, "bar", "nightclub")
	}
	if freeRe.MatchString(text) || standaloneFree.MatchString(text) {
		hard.FreeOnly = true
	}
	hard.ExcludeCategories = uniqueStrings(hard.ExcludeCategories)
	hard.ExcludeTags = uniqueStrings(hard.ExcludeTags)
	sortStrings(hard.ExcludeCategories)
	sortStrings(hard.ExcludeTags)

	for _, fr := range facetRules {
		if !fr.re.MatchString(blanked) {
			continue
		}
		f, _ := FacetByName(fr.name)
		// A facet the user refused outright is not suggested back.
		if facetRefused(f, hard) {
			continue
		}
		addFacet(f)
	}
	return hard, facets
}

// facetRefused is true when every category and tag of the facet is excluded,
// so scoring for it would be pointless.
func facetRefused(f Facet, hard HardConstraints) bool {
	for _, c := range f.Cats {
		if !hard.excludesCategory(c) {
			return false
		}
	}
	for _, t := range f.Tags {
		if !containsString(hard.ExcludeTags, t) {
			return false
		}
	}
	return len(f.Cats)+len(f.Tags) > 0
}

func normalizeMood(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = mixedPunctRe.ReplaceAllString(s, "'")
	return whitespaceRe.ReplaceAllString(s, " ")
}
