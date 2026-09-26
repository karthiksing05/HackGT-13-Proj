package itinerary

// Diverse reorders the pool so that each itinerary is the best remaining one
// after a penalty for overlapping with those already chosen:
//
//	score = utility - mu * max Jaccard(series, chosen series)
//
// The first element is always the best itinerary. The whole pool is
// ordered, so later pages ("more options") stay varied too. Itineraries
// with the same stops in the same order (e.g. one drop-in visited at 16:00
// or 16:30) collapse to the best of them.
func Diverse(pool []Itinerary, mu float64) []Itinerary {
	pool = dedupe(pool)
	sets := make([]map[string]bool, len(pool))
	for i, it := range pool {
		sets[i] = seriesSet(it)
	}
	used := make([]bool, len(pool))
	var order []int
	for len(order) < len(pool) {
		best, bestScore := -1, 0.0
		for i := range pool {
			if used[i] {
				continue
			}
			overlap := 0.0
			for _, k := range order {
				if o := jaccard(sets[i], sets[k]); o > overlap {
					overlap = o
				}
			}
			score := pool[i].Utility - mu*overlap
			if best < 0 || score > bestScore {
				best, bestScore = i, score
			}
		}
		used[best] = true
		order = append(order, best)
	}
	out := make([]Itinerary, len(order))
	for i, k := range order {
		out[i] = pool[k]
	}
	return out
}

func seriesSet(it Itinerary) map[string]bool {
	s := make(map[string]bool, len(it.Stops))
	for _, st := range it.Stops {
		s[st.Node.SeriesKey] = true
	}
	return s
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

// dedupe keeps the highest-utility itinerary for each stop sequence.
func dedupe(pool []Itinerary) []Itinerary {
	best := map[string]int{}
	var out []Itinerary
	for _, it := range pool {
		sig := ""
		for _, st := range it.Stops {
			sig += st.Node.SeriesKey + "\x00"
		}
		if i, ok := best[sig]; ok {
			if it.Utility > out[i].Utility {
				out[i] = it
			}
			continue
		}
		best[sig] = len(out)
		out = append(out, it)
	}
	return out
}
