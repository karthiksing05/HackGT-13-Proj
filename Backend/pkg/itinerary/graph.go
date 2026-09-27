package itinerary

import (
	"Backend/pkg/travel"
	"context"
	"time"
)

// Source is the edge origin for "leave from the start point".
const Source = -1

// Edge is a feasible move into a node (or into the sink).
type Edge struct {
	From    int // node index, or Source
	Leg     travel.Leg
	Wait    time.Duration // idle time after arriving, before the visit starts
	Penalty float64
}

// Graph holds the nodes sorted by start time and, for each node, the edges
// arriving at it. In[len(Nodes)] holds the edges into the sink (the user's
// end point).
type Graph struct {
	Nodes []Node
	In    [][]Edge
}

func (g *Graph) Sink() int { return len(g.Nodes) }

type edgeCandidate struct {
	from, to int // to == len(nodes) for the sink
	pair     travel.Pair
}

// BuildGraph finds every feasible transition. Pairs are pruned with cheap
// checks (time order, same series or category, straight-line distance, a
// travel-time lower bound) before one batched call to the provider. Legs
// into and out of a required visit are not held to the range (only to the
// clock), and two required visits may have any wait between them. No leg
// runs into a busy block (busy.go), and time inside one is not waiting.
func BuildGraph(ctx context.Context, w Window, nodes []Node, tp travel.Provider, cfg Config) *Graph {
	pace := cfg.Pace(w.Pace)
	n := len(nodes)
	g := &Graph{Nodes: nodes, In: make([][]Edge, n+1)}

	// Beyond this gap no leg within range can bring the wait under MaxWait.
	far := travel.Point{Lat: w.MaxLegKm / 111.0}
	maxLeg := 2 * travel.Estimate(travel.Point{}, far, w.Mode).Duration
	cutoff := pace.MaxWait + cfg.Buffer + maxLeg

	var cands []edgeCandidate
	pairs := map[travel.Pair]bool{}
	add := func(c edgeCandidate) {
		cands = append(cands, c)
		pairs[c.pair] = true
	}

	var required []int // indices of the required visits, in start order
	for i := range nodes {
		if nodes[i].Required {
			required = append(required, i)
		}
	}
	pair := func(i, j int) {
		a, b := &nodes[i], &nodes[j]
		if b.Start.Sub(a.End) < cfg.Buffer || a.series == b.series {
			return
		}
		if a.category >= 0 && a.category == b.category && !(a.Required && b.Required) {
			return
		}
		if !a.Required && !b.Required && travel.HaversineKm(a.Loc, b.Loc) > w.MaxLegKm {
			return
		}
		if LegInto(w.Busy, a.End, b.Start).Add(travel.LowerBound(a.Loc, b.Loc, w.Mode) + cfg.Buffer).After(b.Start) {
			return
		}
		add(edgeCandidate{from: i, to: j, pair: travel.Pair{From: a.Loc, To: b.Loc}})
	}
	for i := range n {
		j := i + 1
		for ; j < n && FreeBetween(w.Busy, nodes[i].End, nodes[j].Start) <= cutoff; j++ {
			pair(i, j) // later nodes start even later
		}
		// Past the cutoff only a leg longer than the range, or a wait
		// between two required visits, can still connect: both involve one.
		if nodes[i].Required {
			for ; j < n; j++ {
				pair(i, j)
			}
			continue
		}
		for _, k := range required {
			if k >= j {
				pair(i, k)
			}
		}
	}

	for j := range n {
		b := &nodes[j]
		setOff := LegInto(w.Busy, w.From, b.Start)
		if w.Start == nil {
			g.In[j] = append(g.In[j], Edge{From: Source, Wait: b.Start.Sub(setOff), Penalty: firstWaitPenalty(b.Start.Sub(setOff), pace, cfg)})
			continue
		}
		if !b.Required && travel.HaversineKm(*w.Start, b.Loc) > w.MaxLegKm {
			continue
		}
		if setOff.Add(travel.LowerBound(*w.Start, b.Loc, w.Mode)).After(b.Start) {
			continue
		}
		add(edgeCandidate{from: Source, to: j, pair: travel.Pair{From: *w.Start, To: b.Loc}})
	}

	for i := range n {
		a := &nodes[i]
		if w.End == nil {
			g.In[n] = append(g.In[n], Edge{From: i})
			continue
		}
		if !a.Required && travel.HaversineKm(a.Loc, *w.End) > w.MaxLegKm {
			continue
		}
		if lower := travel.LowerBound(a.Loc, *w.End, w.Mode); LegAfter(w.Busy, a.End, lower).Add(lower).After(w.BackBy) {
			continue
		}
		add(edgeCandidate{from: i, to: n, pair: travel.Pair{From: a.Loc, To: *w.End}})
	}

	legs := lookupLegs(ctx, tp, pairs, w.Mode)

	for _, c := range cands {
		leg := legs[c.pair]
		legMin := leg.Duration.Minutes()
		switch {
		case c.to == n: // into the sink
			if LegAfter(w.Busy, nodes[c.from].End, leg.Duration).Add(leg.Duration).After(w.BackBy) {
				continue
			}
			g.In[n] = append(g.In[n], Edge{From: c.from, Leg: leg, Penalty: cfg.LambdaTravel * legMin})
		case c.from == Source:
			b := &nodes[c.to]
			wait := b.Start.Sub(LegInto(w.Busy, w.From, b.Start).Add(leg.Duration))
			if wait < 0 {
				continue
			}
			g.In[c.to] = append(g.In[c.to], Edge{
				From: Source, Leg: leg, Wait: wait,
				Penalty: cfg.LambdaTravel*legMin + firstWaitPenalty(wait, pace, cfg),
			})
		default:
			a, b := &nodes[c.from], &nodes[c.to]
			// wait is at b, after the leg; idle is all the free time between
			// the two visits that isn't travel (the same without busy blocks).
			wait := b.Start.Sub(LegInto(w.Busy, a.End, b.Start).Add(leg.Duration))
			idle := FreeBetween(w.Busy, a.End, b.Start) - leg.Duration
			if wait < cfg.Buffer || (idle > pace.MaxWait && !(a.Required && b.Required)) {
				continue
			}
			g.In[c.to] = append(g.In[c.to], Edge{
				From: c.from, Leg: leg, Wait: wait,
				Penalty: cfg.LambdaTravel*legMin + pace.LambdaWait*(idle-cfg.Buffer).Minutes(),
			})
		}
	}
	return g
}

// Waiting before the first stop is weighted by cfg.FirstWaitWeight: the
// user is still at home (or wherever they started).
func firstWaitPenalty(wait time.Duration, pace PaceProfile, cfg Config) float64 {
	if wait < 0 {
		return 0
	}
	return cfg.FirstWaitWeight * pace.LambdaWait * wait.Minutes()
}

// lookupLegs asks the provider for every pair at once and falls back to the
// heuristic for anything it can't answer.
func lookupLegs(ctx context.Context, tp travel.Provider, pairs map[travel.Pair]bool, mode travel.Mode) map[travel.Pair]travel.Leg {
	list := make([]travel.Pair, 0, len(pairs))
	for p := range pairs {
		list = append(list, p)
	}
	var legs map[travel.Pair]travel.Leg
	if tp != nil && len(list) > 0 {
		legs, _ = tp.Legs(ctx, list, mode)
	}
	if legs == nil {
		legs = make(map[travel.Pair]travel.Leg, len(list))
	}
	for _, p := range list {
		if _, ok := legs[p]; !ok {
			legs[p] = travel.Estimate(p.From, p.To, mode)
		}
	}
	return legs
}
