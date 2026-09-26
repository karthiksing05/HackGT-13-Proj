package handlers

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"Backend/pkg/util"
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Options returned by /plans/generate and by each /plans/generate/more page.
const (
	firstPageOptions = 3
	morePageOptions  = 2
	planPoolTTL      = 2 * time.Hour
)

var (
	plannerConfig = itinerary.DefaultConfig()
	// Swap for a cached or routed provider later; the optimizer only sees the interface.
	travelProvider travel.Provider = travel.Heuristic{}
)

// planPools keeps the optimizer's remaining options between "generate" and
// "more", and each option's window so /plans/route can re-time it. In
// memory, like the store's plan options; lost on restart.
var planPools = struct {
	sync.Mutex
	pools   map[string]*planPool  // by cursor
	windows map[string]planWindow // by option id
}{pools: map[string]*planPool{}, windows: map[string]planWindow{}}

type planPool struct {
	remaining []models.PlanOption
	created   time.Time
}

type planWindow struct {
	window  itinerary.Window
	created time.Time
}

func savePlanPool(w itinerary.Window, rest []models.PlanOption, all []models.PlanOption) string {
	planPools.Lock()
	defer planPools.Unlock()
	now := time.Now()
	for k, p := range planPools.pools {
		if now.Sub(p.created) > planPoolTTL {
			delete(planPools.pools, k)
		}
	}
	for k, pw := range planPools.windows {
		if now.Sub(pw.created) > planPoolTTL {
			delete(planPools.windows, k)
		}
	}
	for _, o := range all {
		planPools.windows[o.ID] = planWindow{window: w, created: now}
	}
	if len(rest) == 0 {
		return ""
	}
	cursor := "dag_" + util.GenerateID()
	planPools.pools[cursor] = &planPool{remaining: rest, created: now}
	return cursor
}

// nextFromPlanPool returns the next page for a DAG cursor. ok is false when
// the cursor didn't come from the optimizer.
func nextFromPlanPool(cursor string, n int) (page []models.PlanOption, next string, ok bool) {
	planPools.Lock()
	defer planPools.Unlock()
	p, found := planPools.pools[cursor]
	if !found {
		return nil, "", strings.HasPrefix(cursor, "dag_")
	}
	delete(planPools.pools, cursor)
	if n > len(p.remaining) {
		n = len(p.remaining)
	}
	page, rest := p.remaining[:n], p.remaining[n:]
	if len(rest) > 0 {
		next = "dag_" + util.GenerateID()
		planPools.pools[next] = &planPool{remaining: rest, created: p.created}
	}
	return page, next, true
}

func planWindowFor(optionID string) (itinerary.Window, bool) {
	planPools.Lock()
	defer planPools.Unlock()
	pw, ok := planPools.windows[optionID]
	return pw.window, ok
}

// generateDAGPlans runs the itinerary optimizer on ranked candidates. It
// returns the first page of options, a cursor for the rest, and a short
// reason when it found nothing.
func generateDAGPlans(ctx context.Context, req models.PlanGenerateRequest, acts []models.Activity, user *models.User) ([]models.PlanOption, string, string) {
	started := time.Now()
	w, err := itinerary.FromRequest(req, acts, user, started, plannerConfig)
	if err != nil {
		return nil, "", "invalid_request: " + err.Error()
	}

	nodes, drops := itinerary.BuildNodes(w, acts, plannerConfig)
	graph := itinerary.BuildGraph(ctx, w, nodes, travelProvider, plannerConfig)
	pool := itinerary.Diverse(itinerary.Solve(graph, w, plannerConfig), plannerConfig.Mu)

	edges := 0
	for _, in := range graph.In {
		edges += len(in)
	}
	log.Info().
		Int("candidates", len(acts)).
		Int("nodes", len(nodes)).
		Int("edges", edges).
		Int("itineraries", len(pool)).
		Interface("drops", dropCounts(drops)).
		Dur("took", time.Since(started)).
		Msg("itinerary optimizer")

	if len(pool) == 0 {
		if len(nodes) == 0 {
			return nil, "", "no_candidates_fit_window"
		}
		return nil, "", "no_feasible_itinerary"
	}

	options := make([]models.PlanOption, 0, len(pool))
	for _, it := range pool {
		opt := toPlanOption(it, w)
		store.GlobalStore.SavePlanOption(&opt)
		options = append(options, opt)
	}
	n := firstPageOptions
	if n > len(options) {
		n = len(options)
	}
	cursor := savePlanPool(w, options[n:], options)
	return options[:n], cursor, ""
}

func dropCounts(drops []itinerary.Drop) map[string]int {
	out := map[string]int{}
	for _, d := range drops {
		out[d.Reason]++
	}
	return out
}

func toPlanOption(it itinerary.Itinerary, w itinerary.Window) models.PlanOption {
	stops := make([]models.PlanStop, len(it.Stops))
	for i, s := range it.Stops {
		node := s.Node
		stop := activityToPlanStop(*node.Act, i)
		start, end := node.Start, node.End
		stop.ArriveTime = &start
		stop.DepartTime = &end
		stop.DurationMin = int(end.Sub(start).Minutes())
		stop.EstimatedCostCents = node.CostCents
		stop.Kind = node.Act.Kind
		stop.Flexible = node.Flexible
		stops[i] = stop
	}

	legs := make([]models.PlanLeg, len(it.Legs))
	var summaries []string
	totalKm := 0.0
	for i, leg := range it.Legs {
		from, to := "start", "end"
		if i > 0 {
			from = stops[i-1].ID
		}
		if i < len(stops) {
			to = stops[i].ID
		}
		mode := legModeLabel(leg.Mode, w)
		mins := int(math.Round(leg.Duration.Minutes()))
		legs[i] = models.PlanLeg{
			FromStopID:  from,
			ToStopID:    to,
			Mode:        mode,
			DurationMin: mins,
			DistanceKm:  math.Round(leg.DistanceKm*100) / 100,
		}
		totalKm += leg.DistanceKm
		summaries = append(summaries, fmt.Sprintf("%s %d min", strings.Title(mode), mins))
	}

	var title string
	switch len(stops) {
	case 1:
		title = fmt.Sprintf("%s Experience", stops[0].Name)
	case 2:
		title = fmt.Sprintf("%s & %s Quest", stops[0].Name, stops[1].Name)
	default:
		title = fmt.Sprintf("%s, %s & %d more", stops[0].Name, stops[1].Name, len(stops)-2)
	}

	stopWord := "stops"
	if len(stops) == 1 {
		stopWord = "stop"
	}
	clock := func(t time.Time) string { return t.In(w.TZ).Format("3:04 PM") }
	summary := fmt.Sprintf("%d %s · %s–%s · %.1f km %s",
		len(stops), stopWord, clock(it.Stops[0].Node.Start), clock(it.Stops[len(it.Stops)-1].Node.End),
		totalKm, travelPhrase(w))

	return models.PlanOption{
		ID:               util.GenerateID(),
		Title:            title,
		Summary:          summary,
		Stops:            stops,
		Legs:             legs,
		RouteSummary:     strings.Join(summaries, " → "),
		TotalCostCents:   it.CostCents,
		TotalDurationMin: int(it.Arrival.Sub(it.Depart).Minutes()),
		LateFlag:         it.LateRisk,
		ArrivalTime:      it.Arrival,
	}
}

func legModeLabel(m travel.Mode, w itinerary.Window) string {
	if m == travel.Drive {
		return w.DriveLabel
	}
	return string(m)
}

func travelPhrase(w itinerary.Window) string {
	switch w.Mode {
	case travel.Transit:
		return "by transit"
	case travel.Drive:
		if w.DriveLabel == "rideshare" {
			return "by rideshare"
		}
		return "by car"
	}
	return "walking"
}

// routeDAGPlan re-times a reordered DAG option. ok is false when the option
// didn't come from the optimizer.
func routeDAGPlan(ctx context.Context, req models.PlanRouteRequest, stops []models.PlanStop) (map[string]interface{}, bool) {
	w, ok := planWindowFor(req.OptionID)
	if !ok || len(stops) == 0 {
		return nil, false
	}
	if req.Ride != "" || len(req.Modes) > 0 {
		mode, label := itinerary.ResolveMode(req.Ride, req.Modes)
		w.Mode, w.DriveLabel = mode, label
	}
	ev := itinerary.Evaluate(ctx, w, stops, travelProvider)

	legs := make([]models.PlanLeg, len(ev.Legs))
	for i, leg := range ev.Legs {
		from, to := "start", "end"
		if i > 0 {
			from = stops[i-1].ID
		}
		if i < len(stops) {
			to = stops[i].ID
		}
		legs[i] = models.PlanLeg{
			FromStopID:  from,
			ToStopID:    to,
			Mode:        legModeLabel(leg.Mode, w),
			DurationMin: int(math.Round(leg.Duration.Minutes())),
			DistanceKm:  math.Round(leg.DistanceKm*100) / 100,
		}
	}
	type stopTime struct {
		StopID     string    `json:"stop_id"`
		ArriveTime time.Time `json:"arrive_time"`
		DepartTime time.Time `json:"depart_time"`
	}
	times := make([]stopTime, len(stops))
	for i := range stops {
		times[i] = stopTime{StopID: stops[i].ID, ArriveTime: ev.Starts[i], DepartTime: ev.Ends[i]}
	}
	return map[string]interface{}{
		"option_id":          req.OptionID,
		"recalculated_legs":  legs,
		"stop_times":         times,
		"total_duration_min": int(ev.Arrival.Sub(ev.Depart).Minutes()),
		"arrival":            ev.Arrival,
		"late_flag":          ev.MinutesLate > 0,
		"minutes_late":       ev.MinutesLate,
		"broken_at":          ev.BrokenAt,
	}, true
}
