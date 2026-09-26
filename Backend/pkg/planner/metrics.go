package planner

import (
	"Backend/pkg/itinerary"
	"math"
	"sort"
	"strings"
)

// PlanMetrics describes one itinerary (§5.2).
type PlanMetrics struct {
	Fit           float64  `bson:"fit" json:"fit"`
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

	fit := 0.0
	cats := map[string]bool{}
	for _, s := range it.Stops {
		if c := r.Pool.Get(s.Node.Act.ID.Hex()); c != nil {
			fit += c.Raw()
		} else {
			fit += r.Cfg.Itinerary.DefaultUtility
		}
		cats[strings.ToLower(s.Node.Act.Category)] = true
	}
	m.Fit = fit / float64(len(it.Stops))

	if len(r.Spec.Facets) == 0 {
		m.Coverage = 1
	} else {
		for _, f := range r.Spec.Facets {
			covered := false
			for _, s := range it.Stops {
				if f.Covers(s.Node.Act) {
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
	m.PaceFit = clamp01(1 - math.Abs(float64(len(it.Stops)-target))/float64(target))

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
	s := w.Fit*m.Fit + w.Coverage*m.Coverage + w.Variety*m.Variety + w.PaceFit*m.PaceFit +
		w.Travel*(1-m.TravelShare) + w.Idle*(1-m.IdleShare)
	if m.LateRisk {
		s -= w.LateRisk
	}
	s -= w.OverBudget * math.Max(0, m.BudgetUse-0.9) * 10
	return clamp01(s)
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

// top3Sum is the convergence measure: the summed scores of the first three.
func top3Sum(best []ScoredPlan) float64 {
	s := 0.0
	for i, p := range best {
		if i >= 3 {
			break
		}
		s += p.Score
	}
	return s
}
