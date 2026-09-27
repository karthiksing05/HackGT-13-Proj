package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"fmt"
	"strings"
	"time"
)

// Leg modes the app can show (its TravelMode enum, minus "uber", which the
// planner never produces).
var appLegModes = map[string]bool{"walk": true, "marta": true, "drive": true, "rideshare": true}

// legSlack is how much shorter a leg can really be than its minutes say:
// they are rounded to the nearest minute.
const legSlack = 30 * time.Second

// CheckOption lists every way an option breaks the guarantees of
// planner.md §7 for spec, the effective spec (relaxations applied). lookup
// returns the user's catalog document behind an activity id. An empty
// result means the option is sound. Generate runs it on every option it
// renders (with the run's optimizer settings); tests run it on every page.
// The must-see picks (spec.MustInclude) must all be in the option; they are
// exempt from what they bypass (picks.go): the range on their own legs,
// the per-stop price rules, the exclusions and sharing a category with
// each other, and the budget's total grows to what they cost when that is
// more. Nothing is on top of the user's busy blocks (spec.Busy): no visit,
// and no leg as the saved plan lays them out (itinerary/busy.go).
func CheckOption(spec *PlanSpec, opt *Option, lookup func(id string) (*models.Activity, bool)) []string {
	return checkOption(spec, opt, lookup, itinerary.DefaultConfig())
}

// checkOption is CheckOption with the optimizer settings that decide how
// an event may be attended.
func checkOption(spec *PlanSpec, opt *Option, lookup func(id string) (*models.Activity, bool), itCfg itinerary.Config) []string {
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}
	if len(opt.Stops) == 0 {
		fail("no stops")
		return out
	}
	if len(opt.Legs) != len(opt.Stops)+1 {
		fail("%d legs for %d stops", len(opt.Legs), len(opt.Stops))
	} else {
		if opt.Legs[0].FromStopID != "start" || opt.Legs[len(opt.Legs)-1].ToStopID != "end" {
			fail("legs must run start → … → end")
		}
		for i, l := range opt.Legs {
			if !appLegModes[l.Mode] {
				fail("leg %d mode %q not in the app enum", i, l.Mode)
			}
			if i > 0 && l.FromStopID != opt.Stops[i-1].ID {
				fail("leg %d from %s, want %s", i, l.FromStopID, opt.Stops[i-1].ID)
			}
			if i < len(opt.Stops) && l.ToStopID != opt.Stops[i].ID {
				fail("leg %d to %s, want %s", i, l.ToStopID, opt.Stops[i].ID)
			}
		}
	}
	age := AgeRulesFor(spec.AgeBracket)
	picks := map[string]bool{}
	for _, id := range spec.MustInclude {
		picks[id] = true
	}
	pickCats := map[string]bool{} // no other stop may take a pick's category
	for _, s := range opt.Stops {
		if a, ok := lookup(s.ActivityID); ok && picks[s.ActivityID] {
			pickCats[strings.ToLower(a.Category)] = true
		}
	}
	series := map[string]bool{}
	cats := map[string]bool{}
	visited := map[string]bool{}
	var known, knownPicks int64
	for i, s := range opt.Stops {
		visited[s.ActivityID] = true
		pick := picks[s.ActivityID]
		a, ok := lookup(s.ActivityID)
		if !ok {
			fail("stop %d activity %s is not in the user's catalog", i, s.ActivityID)
			continue
		}
		if s.ID != StopID(s.ActivityID, i) {
			fail("stop %d id %s", i, s.ID)
		}
		if s.Arrive.Before(spec.From) {
			fail("stop %d arrives %v before %v", i, s.Arrive, spec.From)
		}
		if s.Depart.After(spec.BackBy) {
			fail("stop %d departs %v after %v", i, s.Depart, spec.BackBy)
		}
		if st, ok := itinerary.StayFor(a, itCfg); ok && a.Kind == "event" {
			switch st.Kind {
			case itinerary.StayWhole:
				p75, _ := visitLengths(a)
				if a.Start.Add(maxDuration(s.Depart.Sub(s.Arrive), p75)).After(spec.BackBy) && !opt.LateFlag {
					fail("stop %d could run past back-by without late_flag", i)
				}
			case itinerary.StayClipped:
				// Joined at most LateArrival late, left by the end, and long
				// enough to count.
				if s.Arrive.Before(st.Start) || s.Arrive.After(st.Start.Add(itinerary.LateArrival)) {
					fail("stop %d joins %v, outside %v + %v", i, s.Arrive, st.Start, itinerary.LateArrival)
				}
				if s.Depart.After(st.End) {
					fail("stop %d leaves %v after the event ends %v", i, s.Depart, st.End)
				}
				if s.Depart.Sub(s.Arrive) < st.MinStay {
					fail("stop %d stays %v, under %v", i, s.Depart.Sub(s.Arrive), st.MinStay)
				}
			}
		}
		if s.PriceKnown {
			if s.PriceCents == nil {
				fail("stop %d has a known price but no price_cents", i)
			} else {
				known += *s.PriceCents
				if pick {
					knownPicks += *s.PriceCents
				} else if spec.Budget.FreeOnly && *s.PriceCents != 0 {
					fail("stop %d costs %d on a free-only plan", i, *s.PriceCents)
				}
			}
		} else {
			if s.PriceCents != nil {
				fail("stop %d has price_cents without a known price", i)
			}
			if !pick && spec.Budget.FreeOnly && (a.Price != nil || !freeIfUnknownCategories[a.Category]) {
				fail("stop %d has an unknown price on a free-only plan", i)
			}
		}
		if !pick && spec.Budget.Tier < 3 && s.TierKnown && s.Tier > spec.Budget.Tier {
			fail("stop %d tier %d over budget tier %d", i, s.Tier, spec.Budget.Tier)
		}
		if age.Blocks(a) {
			fail("stop %d %q is age-gated for %s", i, a.Name, spec.AgeBracket)
		}
		if !pick && (spec.Hard.excludesCategory(a.Category) || spec.Hard.excludesAnyTag(a.Tags) || tagsIntersect(a.Tags, spec.AvoidTags)) {
			fail("stop %d %q hits an exclusion", i, a.Name)
		}
		if series[s.SeriesKey] {
			fail("series %s repeated", s.SeriesKey)
		}
		series[s.SeriesKey] = true
		if c := strings.ToLower(a.Category); c != "" && c != "other" && !pick {
			if cats[c] || pickCats[c] {
				fail("category %s repeated", c)
			}
			cats[c] = true
		}
		if i > 0 {
			prev := opt.Stops[i-1]
			if d := travel.HaversineKm(travel.Point{Lat: prev.Place.Lat, Lng: prev.Place.Lng}, travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}); d > spec.MaxLegKm+1e-9 && !pick && !picks[prev.ActivityID] {
				fail("leg into stop %d is %.2f km > %.2f", i, d, spec.MaxLegKm)
			}
			if s.Arrive.Before(prev.Depart) {
				fail("stop %d starts before stop %d ends", i, i-1)
			}
		}
	}
	if limit := max(spec.Budget.TotalCents, knownPicks); spec.Budget.TotalCents > 0 && known > limit {
		fail("known costs %d exceed %d", known, limit)
	}
	first, last := opt.Stops[0], opt.Stops[len(opt.Stops)-1]
	if spec.Start != nil && !picks[first.ActivityID] {
		if d := travel.HaversineKm(*spec.Start, travel.Point{Lat: first.Place.Lat, Lng: first.Place.Lng}); d > spec.MaxLegKm+1e-9 {
			fail("first leg %.2f km > %.2f", d, spec.MaxLegKm)
		}
	}
	if spec.End != nil && !picks[last.ActivityID] {
		if d := travel.HaversineKm(travel.Point{Lat: last.Place.Lat, Lng: last.Place.Lng}, *spec.End); d > spec.MaxLegKm+1e-9 {
			fail("last leg %.2f km > %.2f", d, spec.MaxLegKm)
		}
	}
	if opt.Depart.Before(spec.From) || opt.Arrival.After(spec.BackBy) {
		fail("leaves %v, back %v, window %v–%v", opt.Depart, opt.Arrival, spec.From, spec.BackBy)
	}
	for _, id := range spec.MustInclude {
		if !visited[id] {
			fail("must-see %s is not in the option", id)
		}
	}
	checkBusy(spec, opt, fail)
	return out
}

// checkBusy lists what an option puts on top of the user's busy blocks.
func checkBusy(spec *PlanSpec, opt *Option, fail func(format string, args ...any)) {
	if len(spec.Busy) == 0 {
		return
	}
	for i, s := range opt.Stops {
		if itinerary.Clashes(spec.Busy, s.Arrive, s.Depart) {
			fail("stop %d (%v–%v) is on top of a busy block", i, s.Arrive, s.Depart)
		}
	}
	if len(opt.Legs) != len(opt.Stops)+1 {
		return
	}
	free := opt.Depart
	for i, s := range opt.Stops {
		leg := max(0, time.Duration(opt.Legs[i].Minutes)*time.Minute-legSlack)
		if setOff := itinerary.LegInto(spec.Busy, free, s.Arrive); setOff.Add(leg).After(s.Arrive) || itinerary.Clashes(spec.Busy, setOff, setOff.Add(leg)) {
			fail("the leg into stop %d runs into a busy block", i)
		}
		free = s.Depart
	}
	home := max(0, time.Duration(opt.Legs[len(opt.Stops)].Minutes)*time.Minute-legSlack)
	if itinerary.LegAfter(spec.Busy, free, home).Add(home).After(spec.BackBy) {
		fail("the way back only fits after back-by, around the busy blocks")
	}
}
