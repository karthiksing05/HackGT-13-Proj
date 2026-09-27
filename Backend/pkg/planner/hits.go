package planner

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// HitSearch is GET /activities/search, Create's must-see search, in
// planner terms.
type HitSearch struct {
	Query      string
	Near       *travel.Point  // the plan's start; nil leaves distances out
	From, To   time.Time      // the local day searched
	Now        time.Time      // business time: events over by then are left out
	TZ         *time.Location // X-Time-Zone, for the times in subtitles
	AgeBracket string
	Limit      int
}

// How a hit matches a non-empty query, best first: its name starts with
// the query, a word of its name does, or only something else contains it
// (the venue, the category, a tag, the inside of a word).
const (
	matchName = iota
	matchWord
	matchOther
)

// suggestRadiusKm: without a query, the best-rated places within this of
// Near are suggested first.
const suggestRadiusKm = 5

// ActivityHits is the must-see search's answer from catalog documents
// (store's Catalog.SearchActivities): the ones that match the query and can
// be picks on the day (PickProblem: on it and not over, open, allowed for
// the age), at most Limit, ranked:
//
//   - with a query: names that start with it, then names with a word that
//     does, then the other matches; within each, events by start, then
//     places by distance from Near (by rating without it);
//   - without one (suggestions): the day's events by start, then places,
//     the best-rated within 5 km of Near first, then the others by distance
//     (by rating alone without Near).
//
// Each hit's subtitle is "{Category} · {event start} · {distance}", the
// start in TZ and the distance only with Near.
func ActivityHits(acts []models.Activity, s HitSearch) []contract.ActivityHit {
	q := strings.ToLower(strings.TrimSpace(s.Query))
	type ranked struct {
		a      *models.Activity
		pt     travel.Point
		tier   int
		distKm float64
		start  time.Time
	}
	var list []ranked
	seen := map[string]bool{}
	for i := range acts {
		a := &acts[i]
		if seen[a.ID.Hex()] || PickProblem(a, s.From, s.To, s.Now, s.TZ, s.AgeBracket) != "" {
			continue
		}
		tier, ok := matchTier(a, q)
		if !ok {
			continue
		}
		seen[a.ID.Hex()] = true
		pt, _ := activityPoint(a) // PickProblem required one
		r := ranked{a: a, pt: pt, tier: tier}
		if s.Near != nil {
			r.distKm = travel.HaversineKm(*s.Near, pt)
		}
		if a.Start != nil {
			r.start = a.Start.UTC()
		}
		list = append(list, r)
	}
	sort.SliceStable(list, func(i, j int) bool {
		x, y := list[i], list[j]
		if x.tier != y.tier {
			return x.tier < y.tier
		}
		if xe, ye := x.a.Kind == "event", y.a.Kind == "event"; xe != ye {
			return xe
		}
		if x.a.Kind == "event" && !x.start.Equal(y.start) {
			return x.start.Before(y.start)
		}
		if x.a.Kind == "place" {
			switch {
			case s.Near == nil:
				if rx, ry := floatOrNeg(x.a.Rating), floatOrNeg(y.a.Rating); rx != ry {
					return rx > ry
				}
				if px, py := floatOrNeg(x.a.Popularity), floatOrNeg(y.a.Popularity); px != py {
					return px > py
				}
			case q == "":
				if nx, ny := x.distKm <= suggestRadiusKm, y.distKm <= suggestRadiusKm; nx != ny {
					return nx
				}
				if rx, ry := floatOrNeg(x.a.Rating), floatOrNeg(y.a.Rating); x.distKm <= suggestRadiusKm && rx != ry {
					return rx > ry
				}
			}
		}
		if s.Near != nil && x.distKm != y.distKm {
			return x.distKm < y.distKm
		}
		if nx, ny := strings.ToLower(x.a.Name), strings.ToLower(y.a.Name); nx != ny {
			return nx < ny
		}
		return x.a.ID.Hex() < y.a.ID.Hex()
	})
	if s.Limit > 0 && len(list) > s.Limit {
		list = list[:s.Limit]
	}
	out := make([]contract.ActivityHit, 0, len(list))
	for _, r := range list {
		out = append(out, activityHit(r.a, r.pt, r.distKm, s))
	}
	return out
}

// matchTier places a document against the lower-cased query; ok is false
// when nothing of it contains the query (an empty query matches all). A
// category or tag also matches with the query's spaces as underscores.
func matchTier(a *models.Activity, q string) (int, bool) {
	if q == "" {
		return matchOther, true
	}
	name := strings.ToLower(strings.TrimSpace(a.Name))
	best, ok := matchOther, false
	for i := 0; i <= len(name)-len(q); {
		k := strings.Index(name[i:], q)
		if k < 0 {
			break
		}
		at := i + k
		ok = true
		if at == 0 {
			return matchName, true
		}
		if prev, _ := utf8.DecodeLastRuneInString(name[:at]); !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
			best = matchWord
		}
		i = at + 1
	}
	if ok {
		return best, true
	}
	slug := strings.Join(strings.Fields(q), "_")
	if strings.Contains(strings.ToLower(strValue(a.VenueName)), q) || strings.Contains(strings.ToLower(a.Category), slug) {
		return matchOther, true
	}
	for _, t := range a.Tags {
		if strings.Contains(strings.ToLower(t), slug) {
			return matchOther, true
		}
	}
	return 0, false
}

// activityHit renders one result.
func activityHit(a *models.Activity, pt travel.Point, distKm float64, s HitSearch) contract.ActivityHit {
	hit := contract.ActivityHit{
		ID: a.ID.Hex(), Title: strings.TrimSpace(a.Name), Kind: contract.PlanStopKind(a.Kind), Category: a.Category,
		Place: PlaceToContract(PlaceAt(placeName(a), pt)),
	}
	parts := []string{categoryLabel(a.Category)}
	if a.Kind == "event" && a.Start != nil {
		hit.Start = contract.Ptr(a.Start.UTC())
		if a.End != nil && a.End.After(*a.Start) {
			hit.End = contract.Ptr(a.End.UTC())
		}
		if tz := s.TZ; tz != nil {
			start := a.Start.In(tz)
			if start.Format("2006-01-02") == s.From.In(tz).Format("2006-01-02") {
				parts = append(parts, start.Format("3:04 PM"))
			} else {
				parts = append(parts, start.Format("Mon 3:04 PM")) // began on an earlier day
			}
		}
	}
	if cents, known := activityCost(a); known {
		v := int(cents)
		hit.PriceCents = &v
	}
	if s.Near != nil {
		mi := distKm / kmPerMile
		v := math.Round(mi*10) / 10
		hit.DistanceMi = &v
		parts = append(parts, httpx.Miles(mi))
	}
	hit.Subtitle = strings.Join(parts, " · ")
	return hit
}
