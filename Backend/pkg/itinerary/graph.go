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

// Legs to and from the user's start and end points may be longer than the
// per-leg range between stops.
const depotLegFactor = 2.0

// BuildGraph finds every feasible transition. Pairs are pruned with cheap
// checks (time order, same series or category, straight-line distance, a
// travel-time lower bound) before one batched call to the provider.
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

	for i := 0; i < n; i++ {
		a := &nodes[i]
		for j := i + 1; j < n; j++ {
			b := &nodes[j]
			gap := b.Start.Sub(a.End)
			if gap > cutoff {
				break // later nodes start even later
			}
			if gap < cfg.Buffer || a.series == b.series {
				continue
			}
			if a.category >= 0 && a.category == b.category {
				continue
			}
			if travel.HaversineKm(a.Loc, b.Loc) > w.MaxLegKm {
				continue
			}
			if a.End.Add(travel.LowerBound(a.Loc, b.Loc, w.Mode) + cfg.Buffer).After(b.Start) {
				continue
			}
			add(edgeCandidate{from: i, to: j, pair: travel.Pair{From: a.Loc, To: b.Loc}})
		}
	}

	for j := 0; j < n; j++ {
		b := &nodes[j]
		if w.Start == nil {
			g.In[j] = append(g.In[j], Edge{From: Source, Wait: b.Start.Sub(w.From), Penalty: firstWaitPenalty(b.Start.Sub(w.From), pace)})
			continue
		}
		if travel.HaversineKm(*w.Start, b.Loc) > w.MaxLegKm*depotLegFactor {
			continue
		}
		if w.From.Add(travel.LowerBound(*w.Start, b.Loc, w.Mode)).After(b.Start) {
			continue
		}
		add(edgeCandidate{from: Source, to: j, pair: travel.Pair{From: *w.Start, To: b.Loc}})
	}

	for i := 0; i < n; i++ {
		a := &nodes[i]
		if w.End == nil {
			g.In[n] = append(g.In[n], Edge{From: i})
			continue
		}
		if travel.HaversineKm(a.Loc, *w.End) > w.MaxLegKm*depotLegFactor {
			continue
		}
		if a.End.Add(travel.LowerBound(a.Loc, *w.End, w.Mode)).After(w.BackBy) {
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
			if nodes[c.from].End.Add(leg.Duration).After(w.BackBy) {
				continue
			}
			g.In[n] = append(g.In[n], Edge{From: c.from, Leg: leg, Penalty: cfg.LambdaTravel * legMin})
		case c.from == Source:
			b := &nodes[c.to]
			wait := b.Start.Sub(w.From.Add(leg.Duration))
			if wait < 0 {
				continue
			}
			g.In[c.to] = append(g.In[c.to], Edge{
				From: Source, Leg: leg, Wait: wait,
				Penalty: cfg.LambdaTravel*legMin + firstWaitPenalty(wait, pace),
			})
		default:
			a, b := &nodes[c.from], &nodes[c.to]
			wait := b.Start.Sub(a.End.Add(leg.Duration))
			if wait < cfg.Buffer || wait > pace.MaxWait {
				continue
			}
			idle := (wait - cfg.Buffer).Minutes()
			g.In[c.to] = append(g.In[c.to], Edge{
				From: c.from, Leg: leg, Wait: wait,
				Penalty: cfg.LambdaTravel*legMin + pace.LambdaWait*idle,
			})
		}
	}
	return g
}

// Waiting before the first stop costs half as much: the user is still at
// home (or wherever they started).
func firstWaitPenalty(wait time.Duration, pace PaceProfile) float64 {
	if wait < 0 {
		return 0
	}
	return 0.5 * pace.LambdaWait * wait.Minutes()
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
