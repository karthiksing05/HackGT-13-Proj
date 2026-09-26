package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Run is the state of one Generate call.
type Run struct {
	ID     string
	User   *UserContext
	Spec   PlanSpec
	Window itinerary.Window
	Cfg    Config
	ItCfg  itinerary.Config

	QV         QueryVector
	SearchText string
	SearchEmb  []float64
	Pool       *Pool

	Best    []ScoredPlan
	Boosts  map[string]float64
	Relaxed []string
	Log     PlanRun

	Deadline time.Time
	RadiusKm float64
	K        int
	MaxSlots int
	Mu       float64

	// Dropped holds ids the classifier left out of its answer: they stay
	// out of the pool for the whole run, expansions included.
	Dropped map[string]bool

	ladder        int  // few_plans relax steps taken
	facetsDropped bool // soft mood facets removed
	startedAt     time.Time
}

// relaxRange widens the search radius and the per-leg range by half, once
// per run; walking legs stay within the walk cap. It reports whether it
// did anything.
func (r *Run) relaxRange() bool {
	if containsString(r.Relaxed, "range") {
		return false
	}
	r.RadiusKm = minFloat(30, r.RadiusKm*1.5)
	r.Spec.MaxLegKm *= 1.5
	if r.Spec.Mode == travel.Walk && r.Cfg.WalkLegCapKm > 0 && r.Spec.MaxLegKm > r.Cfg.WalkLegCapKm {
		r.Spec.MaxLegKm = maxFloat(r.Cfg.WalkLegCapKm, r.Window.MaxLegKm)
	}
	r.Window.MaxLegKm = r.Spec.MaxLegKm
	r.Relaxed = appendUnique(r.Relaxed, "range")
	return true
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// droppedIDs lists the classifier's dropped ids in a fixed order.
func (r *Run) droppedIDs() []string {
	out := make([]string, 0, len(r.Dropped))
	for id := range r.Dropped {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func appendUnique(list []string, s string) []string {
	if containsString(list, s) {
		return list
	}
	return append(list, s)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// utilityFn is the solver's utility hook: the candidate's raw score plus
// facet boosts (capped), read from the pool without touching activities.
func (r *Run) utilityFn() func(a *models.Activity) float64 {
	pool, boosts, facets, def := r.Pool, r.Boosts, r.Spec.Facets, r.Cfg.Itinerary.DefaultUtility
	return func(a *models.Activity) float64 {
		c := pool.Get(a.ID.Hex())
		if c == nil {
			return def
		}
		u := c.Raw()
		if len(boosts) > 0 {
			boost := 0.0
			for _, f := range facets {
				if b := boosts[f.Name]; b > 0 && c.covers(f) {
					boost += b
				}
			}
			u += minFloat(boost, 0.15)
		}
		return u
	}
}

// solveRound builds nodes and the graph from the whole pool and solves.
func (p *Planner) solveRound(ctx context.Context, run *Run, round int) ([]ScoredPlan, RoundLog) {
	rl := RoundLog{Round: round, K: run.K, Mu: run.Mu, Boosts: copyBoosts(run.Boosts), PoolSize: run.Pool.Len(),
		Top3: []TopLog{}, Issues: []IssueLog{}, Expansions: []ExpansionLog{}, Adapted: []string{}}
	itCfg := run.ItCfg
	itCfg.K = run.K
	itCfg.MaxSlots = run.MaxSlots
	itCfg.Mu = run.Mu
	itCfg.Utility = run.utilityFn()

	t := p.Clock.Now()
	nodes, drops := itinerary.BuildNodes(run.Window, run.Pool.Activities(), itCfg)
	g := itinerary.BuildGraph(ctx, run.Window, nodes, p.Travel, itCfg)
	its := itinerary.Diverse(itinerary.Solve(g, run.Window, itCfg), itCfg.Mu)
	rl.SolveMs = p.msSince(t)

	rl.Nodes = len(nodes)
	for _, in := range g.In {
		rl.Edges += len(in)
	}
	rl.Itineraries = len(its)
	rl.Drops = map[string]int{}
	for _, d := range drops {
		rl.Drops[d.Reason]++
	}
	scored := make([]ScoredPlan, 0, len(its))
	for _, it := range its {
		scored = append(scored, run.evaluate(it))
	}
	return scored, rl
}

func copyBoosts(b map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// runLoop is §5.3: solve, evaluate, merge, diagnose, adapt, expand, with
// the documented stop rules.
func (p *Planner) runLoop(ctx context.Context, run *Run) {
	cfg := run.Cfg
	for r := 0; r < cfg.Rounds; r++ {
		scored, rl := p.solveRound(ctx, run, r)
		prev := top3Sum(run.Best)
		run.Best = mergeBest(run.Best, scored, cfg.Itinerary.PoolSize, run.Mu)
		for i, sp := range run.Best {
			if i >= 3 {
				break
			}
			rl.Top3 = append(rl.Top3, TopLog{Signature: sp.Signature, Score: round5(sp.Score), Metrics: sp.Metrics})
		}
		rl.Top3Sum = round5(top3Sum(run.Best))
		run.Log.Rounds = append(run.Log.Rounds, rl)
		cur := &run.Log.Rounds[len(run.Log.Rounds)-1]

		switch {
		case r == cfg.Rounds-1:
			cur.Stop = "last_round"
			return
		case ctx.Err() != nil:
			cur.Stop = "context:" + ctx.Err().Error()
			return
		case p.Clock.Now().After(run.Deadline):
			cur.Stop = "soft_budget"
			return
		}
		issues := diagnose(run)
		for _, is := range issues {
			cur.Issues = append(cur.Issues, is.log())
		}
		if len(issues) == 0 {
			cur.Stop = "converged"
			return
		}
		ladderBefore := run.ladder
		adapted := adapt(run, issues, cur)
		n := cfg.MaxExpansions
		if n > len(issues) {
			n = len(issues)
		}
		added := p.expand(ctx, run, issues[:n], r+1, cur)
		if added == 0 && !adapted {
			cur.Stop = "nothing_to_add"
			return
		}
		// Converged: no gain and nothing new. A relax-ladder step taken just
		// now still gets its round, or it would be reported but never tried.
		if r > 0 && top3Sum(run.Best)-prev < cfg.Epsilon && added == 0 && run.ladder == ladderBefore {
			cur.Stop = "epsilon"
			return
		}
	}
}

// --- diagnose ---------------------------------------------------------------

// Issue is one thing wrong with the current top plans.
type Issue struct {
	Kind     string
	Facet    string
	StopRef  string // "<activityId>" of the stop concerned
	Category string
	Slot     *TimeSlot
	Anchor   *travel.Point
}

func (i Issue) log() IssueLog {
	return IssueLog{Kind: i.Kind, Facet: i.Facet, StopRef: i.StopRef, Category: i.Category, Slot: i.Slot}
}

func (i Issue) key() string { return i.Kind + ":" + i.Facet + ":" + i.StopRef }

// diagnose reads the top three plans and lists their issues in a fixed
// order: missing plans, uncovered facets, pace, gaps, weak and shared
// stops.
func diagnose(run *Run) []Issue {
	var issues []Issue
	seen := map[string]bool{}
	add := func(is Issue) {
		if !seen[is.key()] {
			seen[is.key()] = true
			issues = append(issues, is)
		}
	}
	top := run.Best
	if len(top) > 3 {
		top = top[:3]
	}
	switch {
	case len(top) == 0:
		add(Issue{Kind: "no_plans"})
	case len(top) < 3:
		add(Issue{Kind: "few_plans"})
	}
	for _, f := range run.Spec.Facets {
		covered := false
		for _, sp := range top {
			for _, s := range sp.It.Stops {
				if f.Covers(s.Node.Act) {
					covered = true
					break
				}
			}
		}
		if !covered {
			add(Issue{Kind: "uncovered_facet", Facet: f.Name})
		}
	}
	if len(top) == 0 {
		return issues
	}
	best := top[0].It
	pace := run.ItCfg.Pace(run.Spec.Pace)
	target := paceTarget(run.Spec.Pace)
	for i := 1; i < len(best.Stops); i++ {
		s := best.Stops[i]
		if wait := s.Node.Start.Sub(s.Arrive); wait > pace.MaxWait/2 {
			prev := best.Stops[i-1].Node
			mid := travel.Point{Lat: (prev.Loc.Lat + s.Node.Loc.Lat) / 2, Lng: (prev.Loc.Lng + s.Node.Loc.Lng) / 2}
			add(Issue{Kind: "idle_gap", StopRef: s.Node.Act.ID.Hex(), Slot: &TimeSlot{From: prev.End, To: s.Node.Start}, Anchor: &mid})
			break
		}
	}
	first, last := best.Stops[0], best.Stops[len(best.Stops)-1]
	if len(best.Stops) < pace.MaxStops {
		if first.Node.Start.Sub(run.Window.From) > 60*time.Minute {
			anchor := *run.Window.Start
			add(Issue{Kind: "head_gap", Slot: &TimeSlot{From: run.Window.From, To: first.Node.Start}, Anchor: &anchor})
		}
		if run.Window.BackBy.Sub(best.Arrival) > 75*time.Minute {
			anchor := last.Node.Loc
			add(Issue{Kind: "tail_gap", Slot: &TimeSlot{From: last.Node.End, To: run.Window.BackBy}, Anchor: &anchor})
		}
	}
	for _, s := range best.Stops {
		c := run.Pool.Get(s.Node.Act.ID.Hex())
		if c != nil && c.Raw() < 0.45 {
			loc := s.Node.Loc
			add(Issue{Kind: "weak_stop", StopRef: c.ID, Category: s.Node.Act.Category,
				Slot: &TimeSlot{From: s.Node.Start.Add(-30 * time.Minute), To: s.Node.End.Add(30 * time.Minute)}, Anchor: &loc})
			break
		}
	}
	if len(top) >= 2 {
		shared := seriesSet(top[0].It)
		for _, sp := range top[1:] {
			next := seriesSet(sp.It)
			for k := range shared {
				if !next[k] {
					delete(shared, k)
				}
			}
		}
		if len(shared) > 0 {
			keys := make([]string, 0, len(shared))
			for k := range shared {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, s := range best.Stops {
				if s.Node.SeriesKey == keys[0] {
					loc := s.Node.Loc
					add(Issue{Kind: "shared_stop", StopRef: s.Node.Act.ID.Hex(), Category: s.Node.Act.Category,
						Slot: &TimeSlot{From: s.Node.Start, To: s.Node.End}, Anchor: &loc})
					break
				}
			}
		}
	}
	if len(best.Stops) < target-1 {
		add(Issue{Kind: "under_pace"})
	}
	return issues
}

func seriesSet(it itinerary.Itinerary) map[string]bool {
	out := map[string]bool{}
	for _, s := range it.Stops {
		out[s.Node.SeriesKey] = true
	}
	return out
}

// --- adapt ---------------------------------------------------------------------

// adapt changes solver settings and relaxations without any I/O. It
// reports whether anything changed.
func adapt(run *Run, issues []Issue, rl *RoundLog) bool {
	changed := false
	note := func(s string) {
		rl.Adapted = append(rl.Adapted, s)
		changed = true
	}
	for _, is := range issues {
		switch is.Kind {
		case "no_plans", "few_plans":
			if step := run.relaxLadder(); step != "" {
				note(step)
			}
		case "uncovered_facet":
			if run.Boosts[is.Facet] < 0.10 {
				run.Boosts[is.Facet] = 0.10
				note("boost:" + is.Facet)
			}
		case "shared_stop":
			run.Mu += 0.25
			note(fmt.Sprintf("mu:%.2f", run.Mu))
		case "under_pace":
			if run.K < 24 {
				run.K = 24
				note("k:24")
			}
		}
	}
	return changed
}

// relaxLadder takes the next step of the few-plans ladder: a wider search
// (K and slots), a longer range, a bigger budget when the user is flexible,
// then dropping the mood's soft facets.
func (r *Run) relaxLadder() string {
	for {
		step := r.ladder
		r.ladder++
		switch step {
		case 0:
			if r.K < 32 || r.MaxSlots < 18 {
				r.K, r.MaxSlots = maxInt(r.K, 32), maxInt(r.MaxSlots, 18)
				return "k:32,slots:18"
			}
		case 1:
			if r.relaxRange() {
				return "range"
			}
		case 2:
			// Only a priced budget moves up a tier: a free-only plan stays
			// free, whatever the user's flexibility.
			if r.Spec.Flexible && !r.Spec.Budget.FreeOnly && r.Spec.Budget.Tier < 3 {
				r.Spec.Budget = BudgetForLevel(r.Spec.Budget.Tier + 1)
				r.Window.BudgetCents = r.Spec.Budget.TotalCents
				r.Relaxed = appendUnique(r.Relaxed, "budget")
				return "budget"
			}
		case 3:
			if !r.facetsDropped {
				r.facetsDropped = true
				var kept []Facet
				for _, f := range r.Spec.Facets {
					for _, pick := range r.Spec.QuickPicks {
						if pf, ok := FacetByName(pick); ok && pf.Name == f.Name {
							kept = append(kept, f)
							break
						}
					}
				}
				if len(kept) != len(r.Spec.Facets) {
					r.Spec.Facets = kept
					r.Relaxed = appendUnique(r.Relaxed, "mood_soft")
					return "mood_soft"
				}
			}
		default:
			return ""
		}
	}
}

// --- expand ------------------------------------------------------------------

type expansion struct {
	issue  Issue
	query  CandidateQuery
	opts   feasibleOpts
	topK   int
	source string
}

// expansionQuery turns an issue into a query (§5.3's table) on top of the
// base filters; ok is false when the issue has no query.
func (p *Planner) expansionQuery(run *Run, is Issue) (expansion, bool) {
	cfg := run.Cfg
	spec := &run.Spec
	q := baseQuery(spec, cfg, run.RadiusKm)
	q.ExcludeIDs = append(run.Pool.IDs(), run.droppedIDs()...)
	q.LimitEvents, q.LimitPlaces = cfg.ExpansionLimit, cfg.ExpansionLimit
	ex := expansion{issue: is, topK: 20, source: "expand:" + is.Kind}
	slot := func(s *TimeSlot) {
		if s != nil {
			q.From, q.To = maxTime(s.From, spec.From), minTime(s.To, spec.BackBy)
		}
	}
	switch is.Kind {
	case "no_plans", "few_plans":
		q.LimitEvents, q.LimitPlaces = cfg.PhaseAEvents, cfg.PhaseAPlaces
		ex.topK = cfg.ExpansionLimit
	case "uncovered_facet":
		f, ok := FacetByName(is.Facet)
		if !ok {
			return ex, false
		}
		q.IncludeCategories = placeCategoriesAndEvents(f.Cats, spec.Hard.ExcludeCategories)
		q.AnyTags = f.Tags
		ex.source += ":" + f.Name
	case "idle_gap", "head_gap", "tail_gap":
		if is.Anchor == nil || is.Slot == nil {
			return ex, false
		}
		q.Center, q.RadiusKm = *is.Anchor, spec.MaxLegKm
		slot(is.Slot)
		ex.opts.minOpen = 30 * time.Minute
	case "weak_stop":
		if is.Anchor == nil || is.Slot == nil || is.Category == "" {
			return ex, false
		}
		q.Center, q.RadiusKm = *is.Anchor, spec.MaxLegKm
		q.IncludeCategories = []string{is.Category}
		slot(is.Slot)
		q.LimitEvents, q.LimitPlaces = 30, 30
	case "shared_stop":
		if is.Anchor == nil || is.Slot == nil {
			return ex, false
		}
		q.Center, q.RadiusKm = *is.Anchor, spec.MaxLegKm
		slot(is.Slot)
		if is.Category != "" {
			q.ExcludeCategories = append(q.ExcludeCategories, is.Category)
			q.PlaceCategories = placeCategoriesMinus(q.ExcludeCategories)
		}
	case "under_pace":
		// Places open in the half of the window the best plan leaves empty.
		q.Kinds = []string{"place"}
		mid := spec.From.Add(spec.BackBy.Sub(spec.From) / 2)
		if len(run.Best) > 0 {
			best := run.Best[0].It
			if best.Stops[0].Node.Start.Sub(spec.From) >= spec.BackBy.Sub(best.Arrival) {
				q.From, q.To = spec.From, mid
			} else {
				q.From, q.To = mid, spec.BackBy
			}
		}
		ex.opts.minOpen = 30 * time.Minute
	default:
		return ex, false
	}
	if !q.To.After(q.From) {
		return ex, false
	}
	ex.query = q
	return ex, true
}

// placeCategoriesAndEvents keeps a facet's categories (minus exclusions);
// event categories pass through since the kind filter is what limits them.
func placeCategoriesAndEvents(cats, excluded []string) []string {
	var out []string
	for _, c := range cats {
		if !containsString(excluded, c) {
			out = append(out, c)
		}
	}
	return out
}

// expand runs the issues' queries in parallel, checks feasibility, fetches
// vectors, keeps the cosine top-k of each, scores everything in one
// classifier call and adds the survivors to the pool in a fixed order.
func (p *Planner) expand(ctx context.Context, run *Run, issues []Issue, round int, rl *RoundLog) int {
	var exps []expansion
	for _, is := range issues {
		if ex, ok := p.expansionQuery(run, is); ok {
			exps = append(exps, ex)
		}
	}
	if len(exps) == 0 {
		return 0
	}
	type result struct {
		events, places []models.Activity
		err            error
	}
	results := make([]result, len(exps))
	var wg sync.WaitGroup
	t := p.Clock.Now()
	for i := range exps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev, pl, err := p.Source.FindCandidates(ctx, exps[i].query)
			results[i] = result{ev, pl, err}
		}(i)
	}
	wg.Wait()
	rl.MongoMs += p.msSince(t)

	var all []*Candidate
	logs := make([]ExpansionLog, len(exps))
	perIssue := make([][]*Candidate, len(exps))
	for i, ex := range exps {
		lg := ExpansionLog{Kind: ex.issue.Kind, Facet: ex.issue.Facet}
		res := results[i]
		if res.err != nil {
			lg.Err = res.err.Error()
			logs[i] = lg
			continue
		}
		acts := append(res.events, res.places...)
		lg.Fetched = len(acts)
		drops := map[string]int{}
		cands := Feasible(&run.Spec, &ex.query, run.ItCfg, acts, drops, ex.opts)
		var fresh []*Candidate
		for _, c := range cands {
			if !run.Pool.Has(c.ID) {
				fresh = append(fresh, c)
			}
		}
		lg.Feasible = len(fresh)
		perIssue[i] = fresh
		all = append(all, fresh...)
		logs[i] = lg
	}
	if len(all) > 0 {
		t = p.Clock.Now()
		if err := p.fetchEmbeddings(ctx, run, dedupeCandidates(all)); err != nil {
			for i := range logs {
				logs[i].Err = err.Error()
			}
			rl.Expansions = append(rl.Expansions, logs...)
			return 0
		}
		rl.MongoMs += p.msSince(t)
	}

	// Cosine top-k per issue, merged in issue order then by id, deduped.
	var union []*Candidate
	seen := map[string]bool{}
	for i, ex := range exps {
		top := cosineTop(perIssue[i], run.QV, &run.Spec, run.Cfg, ex.topK)
		sort.SliceStable(top, func(a, b int) bool { return top[a].ID < top[b].ID })
		for _, c := range top {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			c.Source, c.Round = ex.source, round
			union = append(union, c)
		}
	}
	if len(union) == 0 {
		rl.Expansions = append(rl.Expansions, logs...)
		return 0
	}
	t = p.Clock.Now()
	kept, mode := p.scoreCandidates(ctx, run, union, 2*time.Second)
	rl.MLMs += p.msSince(t)
	if mode != "classifier" && run.Log.ML.Mode == "classifier" {
		run.Log.ML.Mode = "classifier;expansion:" + mode
	}
	added := 0
	for _, c := range kept {
		if run.Pool.Add(c) {
			added++
			run.Log.Shortlist = append(run.Log.Shortlist, shortlistEntry(c))
			for i := range exps {
				if c.Source == exps[i].source {
					logs[i].Added++
				}
			}
		}
	}
	rl.Expansions = append(rl.Expansions, logs...)
	return added
}

func dedupeCandidates(in []*Candidate) []*Candidate {
	seen := map[string]bool{}
	var out []*Candidate
	for _, c := range in {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}
