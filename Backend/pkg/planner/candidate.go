package planner

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"sort"
	"sync"
)

// Candidate is an activity that passed the guaranteed filters, with every
// score the planner has for it. Act is the light projection; its Embedding
// is filled after phase B.
type Candidate struct {
	Act    models.Activity
	ID     string // Act.ID.Hex()
	Kind   string // event | place
	Point  travel.Point
	Facets []string // "kind:event", "cat:live_music", "tag:outdoor", …

	Cos     float64 // dot(q, e) − λ·max(0, dot(neg, e)); 0 without vectors
	Dislike float64 // max(0, dot(neg, e))
	Prior   float64
	CosRank float64 // 1 for the best of the shortlist, 0 for the worst
	HasVec  bool

	ML  *float64 // classifier score 0..1
	Jev *float64 // reranker score 0..4

	Source string // "retrieval" | "expand:<kind>[:facet]"
	Round  int

	CostCents  int64
	PriceKnown bool
	Tier       int
	TierKnown  bool

	Text string // embedding text, fetched only for rerank candidates
}

// Raw is the candidate's score on a 0..1 scale: the reranker when it ran,
// else the classifier, else a blend of cosine rank and prior. It never
// includes facet boosts, which only steer the solver.
func (c *Candidate) Raw() float64 {
	switch {
	case c.Jev != nil:
		return clamp01(*c.Jev / 4)
	case c.ML != nil:
		return clamp01(*c.ML)
	}
	return clamp01(0.6*c.CosRank + 0.4*c.Prior)
}

// inherit copies the scores of the series representative rep: siblings
// are the same experience at another time.
func (c *Candidate) inherit(rep *Candidate) {
	c.Cos, c.Dislike, c.Prior, c.CosRank, c.HasVec = rep.Cos, rep.Dislike, rep.Prior, rep.CosRank, rep.HasVec
	c.ML, c.Jev = copyScore(rep.ML), copyScore(rep.Jev)
	c.Source, c.Round = rep.Source, rep.Round
}

func copyScore(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

// covers reports whether the candidate satisfies a facet.
func (c *Candidate) covers(f Facet) bool {
	return f.Covers(&c.Act)
}

// coversAny is the number of the given facets the candidate covers.
func (c *Candidate) coversCount(facets []Facet) int {
	n := 0
	for _, f := range facets {
		if c.covers(f) {
			n++
		}
	}
	return n
}

func newCandidate(a models.Activity) *Candidate {
	c := &Candidate{Act: a, ID: a.ID.Hex(), Kind: a.Kind}
	if p, ok := activityPoint(&a); ok {
		c.Point = p
	}
	c.CostCents, c.PriceKnown = activityCost(&a)
	c.Tier, c.TierKnown = activityTier(&a)
	c.Facets = append(c.Facets, "kind:"+a.Kind)
	if a.Category != "" {
		c.Facets = append(c.Facets, "cat:"+a.Category)
	}
	tags := append([]string(nil), a.Tags...)
	sort.Strings(tags)
	for _, t := range tags {
		c.Facets = append(c.Facets, "tag:"+t)
	}
	return c
}

func activityPoint(a *models.Activity) (travel.Point, bool) {
	if len(a.Location.Coordinates) < 2 {
		return travel.Point{}, false
	}
	lng, lat := a.Location.Coordinates[0], a.Location.Coordinates[1]
	if (lat == 0 && lng == 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return travel.Point{}, false
	}
	return travel.Point{Lat: lat, Lng: lng}, true
}

// Pool is the run's candidate set: insertion-ordered and keyed by id, so a
// solve always sees the same activities in the same order.
type Pool struct {
	mu    sync.Mutex
	byID  map[string]*Candidate
	order []string
}

func NewPool() *Pool {
	return &Pool{byID: map[string]*Candidate{}}
}

// Add puts a candidate in the pool; false when its id is already there.
func (p *Pool) Add(c *Candidate) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.byID[c.ID]; ok {
		return false
	}
	p.byID[c.ID] = c
	p.order = append(p.order, c.ID)
	return true
}

func (p *Pool) Get(id string) *Candidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byID[id]
}

func (p *Pool) Has(id string) bool { return p.Get(id) != nil }

func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}

// List returns the candidates in insertion order.
func (p *Pool) List() []*Candidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Candidate, len(p.order))
	for i, id := range p.order {
		out[i] = p.byID[id]
	}
	return out
}

// IDs returns the candidate ids in insertion order.
func (p *Pool) IDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.order...)
}

// Activities returns copies of the activities for the optimizer, without
// their embeddings (the solver never needs them).
func (p *Pool) Activities() []models.Activity {
	list := p.List()
	out := make([]models.Activity, len(list))
	for i, c := range list {
		a := c.Act
		a.Embedding = nil
		out[i] = a
	}
	return out
}
