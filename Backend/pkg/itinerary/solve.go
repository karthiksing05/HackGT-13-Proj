package itinerary

import (
	"Backend/pkg/travel"
	"math/bits"
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

func (m mask) and(o mask) mask     { return mask{m[0] & o[0], m[1] & o[1]} }
func (m mask) count() int          { return bits.OnesCount64(m[0]) + bits.OnesCount64(m[1]) }
func (m mask) covers(o mask) bool  { return m.and(o) == o }
func (m mask) before(o mask) bool  { return m[1] < o[1] || (m[1] == o[1] && m[0] < o[0]) }
func (m mask) without(o mask) mask { return mask{m[0] &^ o[0], m[1] &^ o[1]} }

// requirement is what the required visits (Config.Required) ask of a path:
// their series, their categories (which no other stop may use), and the
// latest start of each, after which a path that skipped it is dead.
type requirement struct {
	series mask
	cats   mask
	count  int
	last   []lastVisit
	// ok is false when no itinerary can make every required visit: one
	// has no visit in the graph, or two share a series.
	ok bool
}

type lastVisit struct {
	bit   int
	start time.Time
}

func requirementOf(nodes []Node, required []string) requirement {
	var r requirement
	want := map[string]bool{}
	for _, id := range required {
		want[id] = true
	}
	found := map[string]bool{}
	latest := map[int]time.Time{}
	var order []int
	for i := range nodes {
		n := &nodes[i]
		if !n.Required {
			continue
		}
		found[n.Act.ID.Hex()] = true
		r.series = r.series.with(n.series)
		if n.category >= 0 {
			r.cats = r.cats.with(n.category)
		}
		if t, seen := latest[n.series]; !seen || n.Start.After(t) {
			if !seen {
				order = append(order, n.series)
			}
			latest[n.series] = n.Start
		}
	}
	for _, bit := range order {
		r.last = append(r.last, lastVisit{bit: bit, start: latest[bit]})
	}
	r.count = r.series.count()
	r.ok = len(found) == r.count
	for id := range want {
		r.ok = r.ok && found[id]
	}
	return r
}

// open reports whether a path that has visited series with stops stops,
// free again at free, can still make every required visit it lacks: there
// is room under the stop cap, and each lacking one still starts later.
func (r requirement) open(series mask, stops int, free time.Time, maxStops int) bool {
	if stops+r.series.without(series).count() > maxStops {
		return false
	}
	for _, v := range r.last {
		if !series.has(v.bit) && v.start.Before(free) {
			return false
		}
	}
	return true
}

// maxGroups bounds how many groups of partial paths (by the required
// visits they have made) one node keeps.
const maxGroups = 16

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
// With required visits (Config.Required) only paths that make all of them
// finish: a path that can no longer make one is dropped as soon as it
// can't, and each node keeps K paths per set of required visits made, so
// that without the per-path rules the result stays exact.
// Returns up to cfg.PoolSize itineraries, best first.
func Solve(g *Graph, w Window, cfg Config) []Itinerary {
	pace := cfg.Pace(w.Pace)
	n := len(g.Nodes)
	req := requirementOf(g.Nodes, cfg.Required)
	if !req.ok {
		return nil
	}
	maxStops := max(pace.MaxStops, req.count)
	labels := make([][]*label, n)
	root := &label{node: Source, cats: req.cats}

	extend := func(from *label, e *Edge, j int) *label {
		node := &g.Nodes[j]
		if from.stops >= maxStops || from.series.has(node.series) {
			return nil
		}
		cats := from.cats
		if node.category >= 0 && !node.Required { // a required visit's category is taken from the start
			if cats.has(node.category) {
				return nil
			}
			cats = cats.with(node.category)
		}
		cost := from.cost + node.CostCents
		if w.BudgetCents > 0 && cost > w.BudgetCents {
			return nil
		}
		series := from.series.with(node.series)
		if req.count > 0 && !req.open(series, from.stops+1, node.End.Add(cfg.Buffer), maxStops) {
			return nil
		}
		return &label{
			utility: from.utility + node.Utility - e.Penalty,
			cost:    cost,
			stops:   from.stops + 1,
			series:  series,
			cats:    cats,
			node:    j,
			edge:    e,
			prev:    from,
		}
	}

	for j := range n {
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
		labels[j] = keepBest(dominant(next), cfg.K, req)
	}

	var finished []*label
	sinkEdges := g.In[g.Sink()]
	for ei := range sinkEdges {
		e := &sinkEdges[ei]
		for _, l := range labels[e.From] {
			if !l.series.covers(req.series) {
				continue
			}
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

// keepBest is topK per group of paths that made the same required visits
// (all of them one group without any): a path that still owes a visit
// never crowds out one that made it, nor the other way round. Groups
// closer to complete come first, at most maxGroups of them.
func keepBest(ls []*label, k int, req requirement) []*label {
	if req.count == 0 {
		return topK(ls, k)
	}
	groups := map[mask][]*label{}
	var keys []mask
	for _, l := range ls {
		key := l.series.and(req.series)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], l)
	}
	sort.Slice(keys, func(a, b int) bool {
		if ca, cb := keys[a].count(), keys[b].count(); ca != cb {
			return ca > cb
		}
		return keys[a].before(keys[b])
	})
	if len(keys) > maxGroups {
		keys = keys[:maxGroups]
	}
	var out []*label
	for _, key := range keys {
		out = append(out, topK(groups[key], k)...)
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
