package planner

import (
	"Backend/pkg/httpx"
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Must-see picks (PlanRequest.must_include): catalog activities the user
// wants in every option. A pick skips the soft rules, the quality bar, the
// shortlist and its quotas, the one-stop-per-category rule, the retrieval
// radius and the per-leg range, the per-activity budget and the mood's and
// the user's exclusions, but not the hard ones: it is in the user's
// catalog, on the plan's day and not over, open or happening inside the
// window (drop-in stay rules apply), reachable with real travel time, and
// allowed for the user's age. The solver makes every pick a required visit
// (itinerary.Config.Required), so every option on every page has them all.

// MaxMustInclude is how many distinct picks one request may carry.
const MaxMustInclude = 10

// ErrTooManyPicks is more than MaxMustInclude distinct picks (a 400).
var ErrTooManyPicks = errors.New("planner: more than 10 must-see picks")

// Reasons of an empty batch the picks cause: a pick that can't be had
// (named by its title, "a pick" for an unknown id) or picks that can't all
// fit in the window.
const (
	ReasonPickNoFit       = "must_include_no_fit"
	ReasonPickUnavailable = "must_include_unavailable: "
	unknownPickTitle      = "a pick"
	pickSource            = "must_include"
)

func pickUnavailable(title string) string { return ReasonPickUnavailable + title }

// normalizePicks trims and lower-cases the ids (ObjectID hex) and drops
// repeats, keeping the request's order. A malformed id stays: it counts as
// unavailable.
func normalizePicks(ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// PickProblem says why a catalog activity can't be a must-see pick on the
// local days [from, to) for someone of ageBracket at the business time now,
// "" when it can: it is an event or a place with a location, an outing
// rather than an add-on listing, allowed for the age, and on those days:
// an event running then and not over by now, a place of a category plans
// visit that is open then (without weekly hours its category's default
// hours; bars, nightclubs, breweries and tours need real ones). GET
// /activities/search offers only activities without a problem, and a plan
// reports a pick with one as unavailable.
func PickProblem(a *models.Activity, from, to, now time.Time, tz *time.Location, ageBracket string) string {
	return pickProblem(a, from, to, now, tz, ageBracket, itinerary.DefaultConfig())
}

func pickProblem(a *models.Activity, from, to, now time.Time, tz *time.Location, ageBracket string, itCfg itinerary.Config) string {
	if _, ok := activityPoint(a); !ok {
		return "no_location"
	}
	if isAddOnListing(a) {
		return "add_on_listing"
	}
	if AgeRulesFor(ageBracket).Blocks(a) {
		return "age_gate"
	}
	switch a.Kind {
	case "event":
		st, ok := itinerary.StayFor(a, itCfg)
		switch {
		case !ok:
			return "no_start_time"
		case !st.Start.Before(to) || !st.End.After(from):
			return "not_on_this_day"
		case !st.End.After(now):
			return "over"
		}
	case "place":
		if !itinerary.IsPlaceCategory(a.Category) {
			return "category_excluded"
		}
		open, ok := itinerary.OpenIntervals(a.WeeklyHours, a.Category, activityZone(a, tz), from, to)
		switch {
		case !ok:
			return "no_hours"
		case len(open) == 0:
			return "closed_that_day"
		}
	default:
		return "not_an_outing"
	}
	return ""
}

// activityZone is the activity's own time zone, else tz.
func activityZone(a *models.Activity, tz *time.Location) *time.Location {
	if a.Timezone != "" {
		if l, err := time.LoadLocation(a.Timezone); err == nil {
			return l
		}
	}
	return tz
}

// planDays is [from, to): the local days the window touches.
func planDays(spec *PlanSpec) (time.Time, time.Time) {
	from := httpx.LocalMidnight(spec.From, spec.TZ)
	to := httpx.LocalMidnight(spec.BackBy.Add(-time.Nanosecond), spec.TZ).AddDate(0, 0, 1)
	return from, to
}

func pickTitle(a *models.Activity) string {
	if t := strings.TrimSpace(a.Name); t != "" {
		return t
	}
	return unknownPickTitle
}

// resolvePicks loads the picks from the user's catalog. The first one, in
// request order, that is unknown (a malformed or foreign id) or has a
// PickProblem names the reason for an empty batch; otherwise they become
// the run's picks, and the solver's budget grows to what they cost when
// that is more (the rest of the plan then adds nothing with a price).
func (p *Planner) resolvePicks(ctx context.Context, run *Run) (string, error) {
	spec := &run.Spec
	if p.Lookup == nil {
		return "", errors.New("planner: must-see picks need an ActivityLookup")
	}
	var valid []string
	for _, id := range spec.MustInclude {
		if _, err := bson.ObjectIDFromHex(id); err == nil {
			valid = append(valid, id)
		}
	}
	docs, err := p.Lookup.GetActivities(ctx, spec.Catalog, valid)
	if err != nil {
		return "", fmt.Errorf("planner: must-see picks: %w", err)
	}
	byID := make(map[string]*models.Activity, len(docs))
	for i := range docs {
		byID[docs[i].ID.Hex()] = &docs[i]
	}
	from, to := planDays(spec)
	run.picks = map[string]bool{}
	for _, id := range spec.MustInclude {
		a, ok := byID[id]
		problem := "not_in_catalog"
		if ok {
			problem = pickProblem(a, from, to, spec.Now, spec.TZ, spec.AgeBracket, run.ItCfg)
		}
		if problem != "" {
			log.Info().Str("run", run.ID).Str("pick", id).Str("problem", problem).Msg("planner: must-see pick unavailable")
			if !ok {
				return pickUnavailable(unknownPickTitle), nil
			}
			return pickUnavailable(pickTitle(a)), nil
		}
		act := *a
		act.Embedding, act.EmbeddingText = nil, nil // fetched with the others' in phase B
		c := newCandidate(act)
		c.Source = pickSource
		run.Picks = append(run.Picks, c)
		run.picks[id] = true
		run.pickCost += itinerary.CostCents(&act)
	}
	run.Window.BudgetCents = run.windowBudget()
	return "", nil
}

// picksFit is the early answer to must_include_no_fit: whether a plan of the
// picks alone fits the window with travel. Other stops only add
// constraints, so without one nothing fits; with one, its itineraries back
// the loop up (fallbackToPicks).
func (p *Planner) picksFit(ctx context.Context, run *Run) bool {
	acts := make([]models.Activity, len(run.Picks))
	for i, c := range run.Picks {
		acts[i] = c.Act
	}
	itCfg := run.ItCfg
	itCfg.Required = run.pickIDs()
	nodes, _ := itinerary.BuildNodes(run.Window, acts, itCfg)
	g := itinerary.BuildGraph(ctx, run.Window, nodes, p.Travel, itCfg)
	run.pickOnly = itinerary.Solve(g, run.Window, itCfg)
	return len(run.pickOnly) > 0
}

// fallbackToPicks puts the picks-only plans in when the loop found none
// (the K-best search keeps them in principle; this makes it certain).
func (r *Run) fallbackToPicks() {
	if len(r.Best) > 0 || len(r.pickOnly) == 0 {
		return
	}
	scored := make([]ScoredPlan, 0, len(r.pickOnly))
	for _, it := range r.pickOnly {
		scored = append(scored, r.evaluate(it))
	}
	r.Best = mergeBest(nil, scored, r.Cfg.Itinerary.PoolSize, r.ScoreMu)
}

// isPick reports whether the activity id is one of the run's picks.
func (r *Run) isPick(id string) bool { return r.picks[id] }

// pickIDs are the picks' ids in request order.
func (r *Run) pickIDs() []string {
	out := make([]string, len(r.Picks))
	for i, c := range r.Picks {
		out[i] = c.ID
	}
	return out
}

// windowBudget is the solver's budget: the plan's total, or what the picks
// cost when that is more (an unlimited budget stays unlimited).
func (r *Run) windowBudget() int64 {
	b := r.Spec.Budget.TotalCents
	if b > 0 && r.pickCost > b {
		return r.pickCost
	}
	return b
}

// pickScore is a pick's score as the solver and the metrics see it: at
// least the neutral score of an unscored stop, so a pick below the bar
// still counts as a stop the plan wants (and is stayed at properly).
func (r *Run) pickScore(v float64) float64 {
	if def := r.Cfg.Itinerary.DefaultUtility; v < def {
		return def
	}
	return v
}
