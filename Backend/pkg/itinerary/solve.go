package itinerary

import (
	"Backend/pkg/travel"
	"sort"
	"time"
)

// Stop is one scheduled visit in an itinerary.
type Stop struct {
	Node   *Node
	Arrive time.Time // when the user gets there; the visit runs Node.Start..Node.End
}

// Itinerary is a feasible path from the start point to the end point.
// Legs has len(Stops)+1 entries: start -> first stop, between stops, last
// stop -> end.
type Itinerary struct {
	Stops     []Stop
	Legs      []travel.Leg
	Utility   float64
	TravelMin int
	WaitMin   int // idle time between stops
	CostCents int64
	Depart    time.Time // leave the start point
	Arrival   time.Time // reach the end point
	LateRisk  bool      // a visit running to its 75th-percentile length breaks the plan
}

// mask is a set of up to maxMaskBits bit indices (series or categories).
type mask [2]uint64

func (m mask) has(i int) bool { return m[i>>6]&(1<<uint(i&63)) != 0 }

func (m mask) with(i int) mask {
	m[i>>6] |= 1 << uint(i&63)
	return m
}

type label struct {
	utility float64
	cost    int64
	stops   int
	series  mask
	cats    mask
	node    int
	edge    *Edge
	prev    *label
}

// Solve keeps the K best partial paths at each node, visiting nodes in start
// order so every predecessor is finished first. Without the per-path rules
// (one visit per series, one stop per category, stop cap, budget) this is
// exact longest-path DP on a DAG; with them it's a K-best approximation.
// Returns up to cfg.PoolSize itineraries, best first.
func Solve(g *Graph, w Window, cfg Config) []Itinerary {
	pace := cfg.Pace(w.Pace)
	n := len(g.Nodes)
	labels := make([][]*label, n)
	root := &label{node: Source}

	extend := func(from *label, e *Edge, j int) *label {
		node := &g.Nodes[j]
		if from.stops >= pace.MaxStops || from.series.has(node.series) {
			return nil
		}
		cats := from.cats
		if node.category >= 0 {
			if cats.has(node.category) {
				return nil
			}
			cats = cats.with(node.category)
		}
		cost := from.cost + node.CostCents
		if w.BudgetCents > 0 && cost > w.BudgetCents {
			return nil
		}
		return &label{
			utility: from.utility + node.Utility - e.Penalty,
			cost:    cost,
			stops:   from.stops + 1,
			series:  from.series.with(node.series),
			cats:    cats,
			node:    j,
			edge:    e,
			prev:    from,
		}
	}

	for j := 0; j < n; j++ {
		var next []*label
		for ei := range g.In[j] {
			e := &g.In[j][ei]
			from := []*label{root}
			if e.From != Source {
				from = labels[e.From]
			}
			for _, l := range from {
				if nl := extend(l, e, j); nl != nil {
					next = append(next, nl)
				}
			}
		}
		labels[j] = topK(dominant(next), cfg.K)
	}

	var finished []*label
	sinkEdges := g.In[g.Sink()]
	for ei := range sinkEdges {
		e := &sinkEdges[ei]
		for _, l := range labels[e.From] {
			finished = append(finished, &label{
				utility: l.utility - e.Penalty,
				cost:    l.cost,
				stops:   l.stops,
				series:  l.series,
				node:    g.Sink(),
				edge:    e,
				prev:    l,
			})
		}
	}
	finished = topK(finished, cfg.PoolSize)

	out := make([]Itinerary, 0, len(finished))
	for _, l := range finished {
		out = append(out, build(g, w, cfg, l))
	}
	return out
}

// dominant drops partial paths that another path into the same node beats
// with the same visited series, categories, cost and stop count: every
// extension open to one is open to the other, so the lower utility can
// never win. (Without it the K slots fill up with the same stops at
// shifted times.)
func dominant(ls []*label) []*label {
	type key struct {
		series, cats mask
		cost         int64
		stops        int
	}
	best := make(map[key]int, len(ls))
	out := ls[:0]
	for _, l := range ls {
		k := key{l.series, l.cats, l.cost, l.stops}
		if i, ok := best[k]; ok {
			if l.utility > out[i].utility {
				out[i] = l
			}
			continue
		}
		best[k] = len(out)
		out = append(out, l)
	}
	return out
}

func topK(ls []*label, k int) []*label {
	sort.SliceStable(ls, func(a, b int) bool {
		if ls[a].utility != ls[b].utility {
			return ls[a].utility > ls[b].utility
		}
		return ls[a].stops > ls[b].stops
	})
	if len(ls) > k {
		ls = ls[:k]
	}
	return ls
}

// build walks a finished label back to the start and lays out the times.
func build(g *Graph, w Window, cfg Config, sink *label) Itinerary {
	var chain []*label // stops in reverse
	for l := sink.prev; l != nil && l.node != Source; l = l.prev {
		chain = append(chain, l)
	}
	it := Itinerary{Utility: sink.utility, CostCents: sink.cost}
	for i := len(chain) - 1; i >= 0; i-- {
		l := chain[i]
		node := &g.Nodes[l.node]
		it.Stops = append(it.Stops, Stop{Node: node, Arrive: node.Start.Add(-l.edge.Wait)})
		it.Legs = append(it.Legs, l.edge.Leg)
		if l.edge.From != Source {
			it.WaitMin += int(l.edge.Wait.Minutes())
		}
	}
	it.Legs = append(it.Legs, sink.edge.Leg)
	for _, leg := range it.Legs {
		it.TravelMin += int(leg.Duration.Minutes())
	}

	// Leave the start point so as to arrive a buffer before the first visit,
	// rather than waiting there.
	first := it.Stops[0]
	depart := first.Node.Start.Add(-it.Legs[0].Duration - cfg.Buffer)
	if depart.Before(w.From) {
		depart = w.From
	}
	it.Depart = depart
	it.Stops[0].Arrive = depart.Add(it.Legs[0].Duration)

	last := it.Stops[len(it.Stops)-1].Node
	it.Arrival = last.End.Add(it.Legs[len(it.Legs)-1].Duration)

	for i, s := range it.Stops {
		leg := it.Legs[i+1]
		limit := w.BackBy
		if i+1 < len(it.Stops) {
			limit = it.Stops[i+1].Node.Start
		}
		if s.Node.P75End.Add(leg.Duration).After(limit) {
			it.LateRisk = true
		}
	}
	return it
}
