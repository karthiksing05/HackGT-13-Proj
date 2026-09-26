package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// MsgInsightsNudge is the headline before any 4–5 star rating.
const MsgInsightsNudge = "Rate a few sidequests and we'll show what your favorites have in common."

// insightsMaxEvents bounds the history the insights read.
const insightsMaxEvents = 500

// Insights is GET /me/insights → PastInsights: what the viewer's 4–5 star
// past stops have in common (time of day in X-Time-Zone, company, area,
// the top rated one, their rating tags).
func (h *H) Insights(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), api.UserID(r)
	rows, err := h.d.Store.Itineraries().PastStops(ctx, store.PastStopsQuery{UserID: uid, Now: h.d.BusinessNow(ctx), Limit: insightsMaxEvents})
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	past, err := h.pastEvents(ctx, uid, rows)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, insights(past, httpx.TZ(r)))
}

// insights is the port of MockAPIClient.pastInsights over past events
// listed most recent first: liked = rated 4 or 5; each highlight is the
// most common value with ties going to the alphabetically first; the top
// rated is the first liked event with the most stars; tags by count, then
// name; based_on counts every rated event (0 with nothing liked).
func insights(past []contract.PastEvent, tz *time.Location) contract.PastInsights {
	var liked []contract.PastEvent
	rated := 0
	for _, e := range past {
		if e.Rating == nil {
			continue
		}
		rated++
		if e.Rating.Stars >= 4 {
			liked = append(liked, e)
		}
	}
	if len(liked) == 0 {
		return contract.PastInsights{Headline: MsgInsightsNudge, Highlights: []contract.PastInsight{}, TopTags: []string{}}
	}
	n := len(liked)
	of := func(count int) *string {
		s := fmt.Sprintf("%d of your %d favorite", count, n)
		if n != 1 {
			s += "s"
		}
		return &s
	}
	var times, companies, areas, tags []string
	best := liked[0]
	for _, e := range liked {
		times = append(times, timeOfDay(e.Date.In(tz)))
		if e.Company == "solo" {
			companies = append(companies, "On your own")
		} else {
			companies = append(companies, "With friends")
		}
		areas = append(areas, e.Place)
		tags = append(tags, e.Rating.Tags...)
		if e.Rating.Stars > best.Rating.Stars {
			best = e
		}
	}
	when, whenCount := mostCommon(times)
	company, companyCount := mostCommon(companies)
	area, areaCount := mostCommon(areas)
	who := "with friends"
	if company == "On your own" {
		who = "on your own"
	}
	bestDetail := fmt.Sprintf("%d stars", best.Rating.Stars)
	symbol := func(s string) *string { return &s }
	return contract.PastInsights{
		Headline: fmt.Sprintf("You like %s sidequests %s.", strings.TrimSuffix(strings.ToLower(when), "s"), who),
		Highlights: []contract.PastInsight{
			{ID: "time", Title: "Favorite time", Value: when, Detail: of(whenCount), Symbol: symbol("moon.stars")},
			{ID: "company", Title: "Company", Value: company, Detail: of(companyCount), Symbol: symbol("person.2")},
			{ID: "area", Title: "Favorite area", Value: area, Detail: of(areaCount), Symbol: symbol("mappin.and.ellipse")},
			{ID: "best", Title: "Top rated", Value: best.Title, Detail: &bestDetail, Symbol: symbol("star")},
		},
		TopTags: mostCommonTags(tags),
		BasedOn: rated,
	}
}

// timeOfDay buckets a local time: before noon, before 5 PM, after.
func timeOfDay(t time.Time) string {
	switch h := t.Hour(); {
	case h < 12:
		return "Mornings"
	case h < 17:
		return "Afternoons"
	}
	return "Evenings"
}

// mostCommon is the most frequent value; ties go to the alphabetically first.
func mostCommon(values []string) (string, int) {
	counts := map[string]int{}
	for _, v := range values {
		counts[v]++
	}
	best, bestCount := "", 0
	for v, c := range counts {
		if c > bestCount || (c == bestCount && v < best) {
			best, bestCount = v, c
		}
	}
	return best, bestCount
}

// mostCommonTags is every tag, most frequent first, ties by name.
func mostCommonTags(tags []string) []string {
	counts := map[string]int{}
	for _, t := range tags {
		counts[t]++
	}
	out := make([]string, 0, len(counts))
	for t := range counts {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
