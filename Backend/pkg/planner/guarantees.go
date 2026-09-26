package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"fmt"
	"strings"
	"time"
)

// Leg modes the app can show (its TravelMode enum, minus "uber", which the
// planner never produces).
var appLegModes = map[string]bool{"walk": true, "marta": true, "drive": true, "rideshare": true}

// CheckOption lists every way an option breaks the guarantees of
// planner.md §7 for spec, the effective spec (relaxations applied). lookup
// returns the user's catalog document behind an activity id. An empty
// result means the option is sound. Generate runs it on every option it
// renders; tests run it on every page.
func CheckOption(spec *PlanSpec, opt *Option, lookup func(id string) (*models.Activity, bool)) []string {
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
	series := map[string]bool{}
	cats := map[string]bool{}
	var known int64
	for i, s := range opt.Stops {
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
		dropIn := a.Attendance != nil && *a.Attendance == "drop_in"
		if a.Kind == "event" && !dropIn && a.Start != nil && !(a.End != nil && a.End.Sub(*a.Start) > 6*time.Hour) {
			p75, _ := visitLengths(a)
			if a.Start.Add(maxDuration(s.Depart.Sub(s.Arrive), p75)).After(spec.BackBy) && !opt.LateFlag {
				fail("stop %d could run past back-by without late_flag", i)
			}
		}
		if s.PriceKnown {
			if s.PriceCents == nil {
				fail("stop %d has a known price but no price_cents", i)
			} else {
				known += *s.PriceCents
				if spec.Budget.FreeOnly && *s.PriceCents != 0 {
					fail("stop %d costs %d on a free-only plan", i, *s.PriceCents)
				}
			}
		} else {
			if s.PriceCents != nil {
				fail("stop %d has price_cents without a known price", i)
			}
			if spec.Budget.FreeOnly && (a.Price != nil || !freeIfUnknownCategories[a.Category]) {
				fail("stop %d has an unknown price on a free-only plan", i)
			}
		}
		if spec.Budget.Tier < 3 && s.TierKnown && s.Tier > spec.Budget.Tier {
			fail("stop %d tier %d over budget tier %d", i, s.Tier, spec.Budget.Tier)
		}
		if age.Blocks(a) {
			fail("stop %d %q is age-gated for %s", i, a.Name, spec.AgeBracket)
		}
		if spec.Hard.excludesCategory(a.Category) || spec.Hard.excludesAnyTag(a.Tags) || tagsIntersect(a.Tags, spec.AvoidTags) {
			fail("stop %d %q hits an exclusion", i, a.Name)
		}
		if series[s.SeriesKey] {
			fail("series %s repeated", s.SeriesKey)
		}
		series[s.SeriesKey] = true
		if c := strings.ToLower(a.Category); c != "" && c != "other" {
			if cats[c] {
				fail("category %s repeated", c)
			}
			cats[c] = true
		}
		if i > 0 {
			prev := opt.Stops[i-1]
			if d := travel.HaversineKm(travel.Point{Lat: prev.Place.Lat, Lng: prev.Place.Lng}, travel.Point{Lat: s.Place.Lat, Lng: s.Place.Lng}); d > spec.MaxLegKm+1e-9 {
				fail("leg into stop %d is %.2f km > %.2f", i, d, spec.MaxLegKm)
			}
			if s.Arrive.Before(prev.Depart) {
				fail("stop %d starts before stop %d ends", i, i-1)
			}
		}
	}
	if spec.Budget.TotalCents > 0 && known > spec.Budget.TotalCents {
		fail("known costs %d exceed %d", known, spec.Budget.TotalCents)
	}
	first, last := opt.Stops[0], opt.Stops[len(opt.Stops)-1]
	if spec.Start != nil {
		if d := travel.HaversineKm(*spec.Start, travel.Point{Lat: first.Place.Lat, Lng: first.Place.Lng}); d > 2*spec.MaxLegKm+1e-9 {
			fail("first leg %.2f km > 2×%.2f", d, spec.MaxLegKm)
		}
	}
	if spec.End != nil {
		if d := travel.HaversineKm(travel.Point{Lat: last.Place.Lat, Lng: last.Place.Lng}, *spec.End); d > 2*spec.MaxLegKm+1e-9 {
			fail("last leg %.2f km > 2×%.2f", d, spec.MaxLegKm)
		}
	}
	if opt.Depart.Before(spec.From) || opt.Arrival.After(spec.BackBy) {
		fail("leaves %v, back %v, window %v–%v", opt.Depart, opt.Arrival, spec.From, spec.BackBy)
	}
	return out
}
