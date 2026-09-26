package planner

import (
	"math"
	"sort"
)

// QueryVector is what candidates are compared against: the user's positive
// vector blended with the request's search vector, plus the negative
// vector when the user has one. Source records which of them existed; with
// "none" the shortlist falls back to priors. No vector is ever invented.
type QueryVector struct {
	Q      []float64
	Neg    []float64
	HasNeg bool
	Source string // "user+search" | "user" | "search" | "none"
}

// BuildQueryVector blends q = l2norm((1−w)·pos + w·search). A missing
// positive vector leaves the search vector alone; both missing gives
// Source "none".
func BuildQueryVector(user *UserContext, searchEmb []float64, cfg Config) QueryVector {
	var pos, neg []float64
	if user != nil {
		if !isZeroVec(user.PositiveEmbedding) {
			pos = user.PositiveEmbedding
		}
		if !isZeroVec(user.NegativeEmbedding) {
			neg = user.NegativeEmbedding
		}
	}
	search := searchEmb
	if isZeroVec(search) {
		search = nil
	}
	if pos != nil && search != nil && len(pos) != len(search) {
		search = nil // dimensions disagree: trust the stored profile
	}
	qv := QueryVector{Source: "none"}
	switch {
	case pos != nil && search != nil:
		w := cfg.SearchWeight
		q := make([]float64, len(pos))
		for i := range pos {
			q[i] = (1-w)*pos[i] + w*search[i]
		}
		qv.Q, qv.Source = l2norm(q), "user+search"
	case pos != nil:
		qv.Q, qv.Source = l2norm(pos), "user"
	case search != nil:
		qv.Q, qv.Source = l2norm(search), "search"
	}
	if qv.Q != nil && neg != nil && len(neg) == len(qv.Q) {
		qv.Neg, qv.HasNeg = l2norm(neg), true
	}
	return qv
}

// Prior is the vector-free score: rating, popularity and facet match, each
// falling back to a neutral value when unknown.
func Prior(c *Candidate, facets []Facet) float64 {
	rating := 0.4
	if c.Act.Rating != nil && !math.IsNaN(*c.Act.Rating) {
		rating = clamp01((*c.Act.Rating - 3) / 2)
	}
	pop := 0.3
	if c.Act.Popularity != nil && !math.IsNaN(*c.Act.Popularity) {
		pop = clamp01(*c.Act.Popularity)
	}
	facet := 0.5
	if len(facets) > 0 {
		facet = 0
		if c.coversCount(facets) > 0 {
			facet = 1
		}
	}
	return clamp01(0.5*rating + 0.2*pop + 0.3*facet)
}

// scoreForShortlist fills Cos, Dislike and Prior and returns the sort key.
// Candidates without a vector rank after every candidate with one (by
// prior among themselves); with Source "none" everyone ranks by prior.
func scoreForShortlist(c *Candidate, qv QueryVector, facets []Facet, cfg Config) (key float64, embedded bool) {
	c.Prior = Prior(c, facets)
	if qv.Source == "none" || len(qv.Q) == 0 {
		return c.Prior, false
	}
	e := c.Act.Embedding
	if len(e) != len(qv.Q) || isZeroVec(e) {
		c.Cos, c.Dislike, c.HasVec = 0, 0, false
		return c.Prior, false
	}
	c.HasVec = true
	cos := dot(qv.Q, e)
	if qv.HasNeg {
		c.Dislike = math.Max(0, dot(qv.Neg, e))
	}
	c.Cos = cos - cfg.DislikeLambda*c.Dislike
	bonus := 0.05 * float64(c.coversCount(facets))
	return c.Cos + bonus, true
}

// Shortlist orders candidates by (score desc, id asc) and takes N of them
// with quotas: at least FacetQuota per requested facet and 30 % events when
// available, at most CategoryCap per category. It sets CosRank on the
// result (1 for the first, 0 for the last).
func Shortlist(cands []*Candidate, qv QueryVector, spec *PlanSpec, cfg Config) []*Candidate {
	type ranked struct {
		c        *Candidate
		key      float64
		embedded bool
	}
	all := make([]ranked, len(cands))
	for i, c := range cands {
		key, emb := scoreForShortlist(c, qv, spec.Facets, cfg)
		all[i] = ranked{c: c, key: key, embedded: emb}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.embedded != b.embedded {
			return a.embedded
		}
		if a.key != b.key {
			return a.key > b.key
		}
		return a.c.ID < b.c.ID
	})

	n := cfg.ShortlistN
	if n <= 0 || n > len(all) {
		n = len(all)
	}
	taken := make([]bool, len(all))
	perCat := map[string]int{}
	count := 0
	take := func(i int) bool {
		if taken[i] || count >= n {
			return false
		}
		cat := all[i].c.Act.Category
		if cfg.CategoryCap > 0 && perCat[cat] >= cfg.CategoryCap {
			return false
		}
		taken[i] = true
		perCat[cat]++
		count++
		return true
	}

	// Quotas first, in list order, so the best of each facet get in.
	for _, f := range spec.Facets {
		got := 0
		for i := range all {
			if got >= cfg.FacetQuota {
				break
			}
			if all[i].c.covers(f) && !taken[i] && take(i) {
				got++
			} else if all[i].c.covers(f) && taken[i] {
				got++
			}
		}
	}
	eventQuota := int(math.Ceil(0.3 * float64(n)))
	events := 0
	for i := range all {
		if all[i].c.Kind == "event" && taken[i] {
			events++
		}
	}
	for i := range all {
		if events >= eventQuota {
			break
		}
		if all[i].c.Kind == "event" && !taken[i] && take(i) {
			events++
		}
	}
	for i := range all {
		if count >= n {
			break
		}
		take(i)
	}

	out := make([]*Candidate, 0, count)
	for i := range all {
		if taken[i] {
			out = append(out, all[i].c)
		}
	}
	for i, c := range out {
		if len(out) > 1 {
			c.CosRank = 1 - float64(i)/float64(len(out)-1)
		} else {
			c.CosRank = 1
		}
	}
	return out
}

// cosineTop ranks candidates by cosine against the query vector (prior
// when there is none) and keeps the first k. Used by expansions.
func cosineTop(cands []*Candidate, qv QueryVector, spec *PlanSpec, cfg Config, k int) []*Candidate {
	saved := cfg
	saved.ShortlistN = k
	saved.FacetQuota = 0
	saved.CategoryCap = 0
	specNoFacets := *spec
	specNoFacets.Facets = nil
	out := Shortlist(cands, qv, &specNoFacets, saved)
	// Shortlist assigned CosRank within this small set; expansions keep the
	// rank they would have had in the main shortlist instead.
	for _, c := range out {
		c.CosRank = expansionCosRank(c)
	}
	return out
}

// expansionCosRank places an expansion candidate on the shortlist's scale:
// cosine similarity is mapped from [0, 0.6] onto [0, 1], which brackets the
// scores real vectors produce; prior-only candidates use the prior.
func expansionCosRank(c *Candidate) float64 {
	if !c.HasVec {
		return c.Prior
	}
	return clamp01(c.Cos / 0.6)
}

func dot(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	s := 0.0
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return s
}

func l2norm(v []float64) []float64 {
	s := 0.0
	for _, x := range v {
		s += x * x
	}
	out := make([]float64, len(v))
	if s == 0 {
		return out
	}
	inv := 1 / math.Sqrt(s)
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

func isZeroVec(v []float64) bool {
	for _, x := range v {
		if x != 0 && !math.IsNaN(x) {
			return false
		}
	}
	return true
}
