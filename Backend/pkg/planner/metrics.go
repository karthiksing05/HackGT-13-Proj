package planner

import (
	"Backend/pkg/itinerary"
	"math"
	"sort"
	"strings"
)

// PlanMetrics describes one itinerary (§5.2).
type PlanMetrics struct {
	Fit float64 `bson:"fit" json:"fit"` // mean score of the stops
	// Fill is how much above-the-bar value the plan holds against the
	// pace's target: the stops' solver scores past Tau, rescaled to 0..1,
	// summed and divided by the target stop count. Stops at or below the
	// bar (weak stops) add nothing, so padding a plan never fills it.
	Fill          float64  `bson:"fill" json:"fill"`
	GoodStops     int      `bson:"goodStops" json:"good_stops"`
	WeakStops     int      `bson:"weakStops" json:"weak_stops"`
	Coverage      float64  `bson:"coverage" json:"coverage"`
	Variety       float64  `bson:"variety" json:"variety"`
	PaceFit       float64  `bson:"paceFit" json:"pace_fit"`
	TravelShare   float64  `bson:"travelShare" json:"travel_share"`
	IdleShare     float64  `bson:"idleShare" json:"idle_share"`
	BudgetUse     float64  `bson:"budgetUse" json:"budget_use"`
	LateRisk      bool     `bson:"lateRisk" json:"late_risk"`
	Stops         int      `bson:"stops" json:"stops"`
	CoveredFacets []string `bson:"coveredFacets" json:"covered_facets"`
	MissingFacets []string `bson:"missingFacets" json:"missing_facets"`
}

// ScoredPlan is a solver result with its metrics and score.
type ScoredPlan struct {
	It        itinerary.Itinerary
	Metrics   PlanMetrics
	Score     float64
	Signature string // sorted series keys
}

// evaluate computes the metrics and Score of an itinerary for this run.
func (r *Run) evaluate(it itinerary.Itinerary) ScoredPlan {
	m := PlanMetrics{Stops: len(it.Stops), LateRisk: it.LateRisk, CoveredFacets: []string{}, MissingFacets: []string{}}
	if len(it.Stops) == 0 {
		return ScoredPlan{It: it, Metrics: m, Signature: signature(it)}
	}

	fit, fill := 0.0, 0.0
	tau := r.Cfg.Itinerary.Tau
	base := r.utilityFn() // the solver's view: raw score plus facet boosts
	good := make([]bool, len(it.Stops))
	cats := map[string]bool{}
	for i, s := range it.Stops {
		raw := r.Cfg.Itinerary.DefaultUtility
		if c := r.Pool.Get(s.Node.Act.ID.Hex()); c != nil {
			raw = c.Raw()
		}
		fit += raw
		if b := clamp01(base(s.Node.Act)); b > tau && tau < 1 {
			good[i] = true
			m.GoodStops++
			fill += (b - tau) / (1 - tau)
		} else {
			m.WeakStops++
		}
		cats[strings.ToLower(s.Node.Act.Category)] = true
	}
	m.Fit = fit / float64(len(it.Stops))

	// A facet counts as covered only by a stop above the bar: a weak match
	// nearby does not answer the request.
	if len(r.Spec.Facets) == 0 {
		m.Coverage = 1
	} else {
		for _, f := range r.Spec.Facets {
			covered := false
			for i, s := range it.Stops {
				if good[i] && f.Covers(s.Node.Act) {
					covered = true
					break
				}
			}
			if covered {
				m.CoveredFacets = append(m.CoveredFacets, f.Name)
			} else {
				m.MissingFacets = append(m.MissingFacets, f.Name)
			}
		}
		m.Coverage = float64(len(m.CoveredFacets)) / float64(len(r.Spec.Facets))
	}
	m.Variety = float64(len(cats)) / float64(len(it.Stops))

	target := paceTarget(r.Spec.Pace)
	m.Fill = clamp01(fill / float64(target))
	m.PaceFit = clamp01(1 - math.Abs(float64(m.GoodStops-target))/float64(target))

	if span := it.Arrival.Sub(it.Depart).Minutes(); span > 0 {
		m.TravelShare = clamp01(float64(it.TravelMin) / span)
	}
	if win := r.Window.BackBy.Sub(r.Window.From).Minutes(); win > 0 {
		m.IdleShare = clamp01(float64(it.WaitMin) / win)
	}
	if r.Spec.Budget.TotalCents > 0 {
		m.BudgetUse = float64(it.CostCents) / float64(r.Spec.Budget.TotalCents)
	}
	return ScoredPlan{It: it, Metrics: m, Score: planScore(m, r.Cfg.Weights), Signature: signature(it)}
}

// planScore is the weighted sum of §5.2, clamped to 0..1.
func planScore(m PlanMetrics, w ScoreWeights) float64 {
	s := w.Fit*m.Fit + w.Fill*m.Fill + w.Coverage*m.Coverage + w.Variety*m.Variety + w.PaceFit*m.PaceFit +
		w.Travel*(1-m.TravelShare) + w.Idle*(1-m.IdleShare)
	if m.LateRisk {
		s -= w.LateRisk
	}
	s -= w.WeakStop * float64(m.WeakStops)
	s -= w.OverBudget * math.Max(0, m.BudgetUse-0.9) * 10
	return clamp01(s)
}

// preferStrong leaves out plans with a stop at or below the bar when at
// least keep plans have none, so weak stops only appear when nothing better
// fills the page. It returns how many it left out.
func preferStrong(plans []ScoredPlan, keep int) ([]ScoredPlan, int) {
	strong := 0
	for _, p := range plans {
		if p.Metrics.WeakStops == 0 {
			strong++
		}
	}
	if keep <= 0 || strong < keep || strong == len(plans) {
		return plans, 0
	}
	out := make([]ScoredPlan, 0, strong)
	for _, p := range plans {
		if p.Metrics.WeakStops == 0 {
			out = append(out, p)
		}
	}
	return out, len(plans) - len(out)
}

// withinTravelShare leaves out plans that spend more than max of their
// time travelling, as long as at least one plan does not. It returns how
// many it left out.
func withinTravelShare(plans []ScoredPlan, max float64) ([]ScoredPlan, int) {
	if max <= 0 {
		return plans, 0
	}
	fine := 0
	for _, p := range plans {
		if p.Metrics.TravelShare <= max {
			fine++
		}
	}
	if fine == 0 || fine == len(plans) {
		return plans, 0
	}
	out := make([]ScoredPlan, 0, fine)
	for _, p := range plans {
		if p.Metrics.TravelShare <= max {
			out = append(out, p)
		}
	}
	return out, len(plans) - len(out)
}

// signature identifies a plan by the series it visits, order-free.
func signature(it itinerary.Itinerary) string {
	keys := make([]string, 0, len(it.Stops))
	for _, s := range it.Stops {
		keys = append(keys, s.Node.SeriesKey)
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// mergeBest unions plans by signature (keeping the higher score), sorts by
// (score desc, signature asc), keeps poolSize, and orders the result so
// each next plan is the best after a diversity penalty on the score.
func mergeBest(best, fresh []ScoredPlan, poolSize int, mu float64) []ScoredPlan {
	bySig := map[string]ScoredPlan{}
	for _, list := range [][]ScoredPlan{best, fresh} {
		for _, p := range list {
			if cur, ok := bySig[p.Signature]; !ok || p.Score > cur.Score {
				bySig[p.Signature] = p
			}
		}
	}
	all := make([]ScoredPlan, 0, len(bySig))
	for _, p := range bySig {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].Signature < all[j].Signature
	})
	if poolSize > 0 && len(all) > poolSize {
		all = all[:poolSize]
	}
	return diverseOrder(all, mu)
}

// diverseOrder is itinerary.Diverse on Score: greedy, first maximum wins,
// so the order is a pure function of the input order.
func diverseOrder(plans []ScoredPlan, mu float64) []ScoredPlan {
	sets := make([]map[string]bool, len(plans))
	for i, p := range plans {
		sets[i] = map[string]bool{}
		for _, s := range p.It.Stops {
			sets[i][s.Node.SeriesKey] = true
		}
	}
	used := make([]bool, len(plans))
	out := make([]ScoredPlan, 0, len(plans))
	var chosen []int
	for len(out) < len(plans) {
		best, bestScore := -1, 0.0
		for i := range plans {
			if used[i] {
				continue
			}
			overlap := 0.0
			for _, k := range chosen {
				if o := jaccard(sets[i], sets[k]); o > overlap {
					overlap = o
				}
			}
			score := plans[i].Score - mu*overlap
			if best < 0 || score > bestScore {
				best, bestScore = i, score
			}
		}
		used[best] = true
		chosen = append(chosen, best)
		out = append(out, plans[best])
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// top3Sum is the convergence measure: the summed scores of the three best
// plans by score (not the diversity order, which may put a lower score in
// the top three). mergeBest only ever adds plans or raises a signature's
// score, so this never decreases from round to round.
func top3Sum(best []ScoredPlan) float64 {
	scores := make([]float64, len(best))
	for i, p := range best {
		scores[i] = p.Score
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	s := 0.0
	for i := 0; i < len(scores) && i < 3; i++ {
		s += scores[i]
	}
	return s
}
