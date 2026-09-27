package itinerary

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"slices"
	"sort"
	"time"
)

// How an event can be attended.
type StayKind int

const (
	// StayWhole: from its start to its end (a play, a class, a tour, a
	// reservation).
	StayWhole StayKind = iota
	// StayClipped: a fixed start, but joining up to LateArrival late or
	// leaving early is normal (a set, a match to watch, an open house).
	StayClipped
	// StayWindow: an open span to drop into (attendance "drop_in", or a
	// listing longer than MaxDuration such as a multi-day convention).
	StayWindow
)

// LateArrival is how long after its start a clipped stay may begin.
const LateArrival = 15 * time.Minute

// A drop-in stay lasts at least min(maxMinStay, minStayShare of the event),
// and never less than Config.MinDuration.
const (
	maxMinStay   = 60 * time.Minute
	minStayShare = 0.5
)

// Event categories whose fixed-start listings can be joined late or left
// early. Tours, classes, screenings, plays, comedy sets and reservations
// are attended whole.
var clippableCategories = map[string]bool{
	"live_music": true, "market": true, "festival": true, "community_event": true,
	"sports_event": true, "gallery": true, "museum": true, "nightclub": true,
	"bar": true, "rec_venue": true,
}

// Clippable reports whether a fixed-start event may be joined late or left
// early. Taking part in an active session (a dive, a round-robin) needs the
// whole of it, unless it is open play for anyone (tagged solo_friendly).
func Clippable(a *models.Activity) bool {
	if a.Kind == "place" || !clippableCategories[a.Category] {
		return false
	}
	return !hasTag(a, "active") || hasTag(a, "solo_friendly")
}

func hasTag(a *models.Activity, tag string) bool {
	return slices.Contains(a.Tags, tag)
}

// EventStay is when an event runs and how it can be attended.
type EventStay struct {
	Kind  StayKind
	Start time.Time // UTC
	// End is the published end. Without one: start + the p75 visit length
	// for a clipped stay, start + the median for the others.
	End time.Time
	// MinStay is the shortest acceptable visit of a clipped or window stay.
	MinStay time.Duration
}

// StayFor classifies an event. ok is false when it has no start time.
func StayFor(a *models.Activity, cfg Config) (EventStay, bool) {
	if a.Start == nil {
		return EventStay{}, false
	}
	st := EventStay{Kind: StayWhole, Start: a.Start.UTC()}
	visit, visit75 := durations(a, cfg)
	published := a.End != nil && a.End.After(st.Start)
	switch {
	case a.Attendance != nil && *a.Attendance == "drop_in":
		st.Kind = StayWindow
	case published && a.End.Sub(st.Start) > cfg.MaxDuration:
		// A long published span is a window the user can drop into, not a
		// visit length (e.g. a 31-hour convention listed as fixed_start).
		st.Kind = StayWindow
	case Clippable(a):
		st.Kind = StayClipped
	}
	switch {
	case published:
		st.End = a.End.UTC()
	case st.Kind == StayClipped:
		st.End = st.Start.Add(visit75)
	default:
		st.End = st.Start.Add(visit)
	}
	switch st.Kind {
	case StayClipped:
		st.MinStay = clampMinStay(time.Duration(minStayShare*float64(st.End.Sub(st.Start))), cfg)
	case StayWindow:
		st.MinStay = clampMinStay(visit, cfg)
	}
	return st, true
}

func clampMinStay(d time.Duration, cfg Config) time.Duration {
	if d > maxMinStay {
		d = maxMinStay
	}
	if d < cfg.MinDuration {
		d = cfg.MinDuration
	}
	return d
}

// ClippedFits reports whether a clipped stay of at least MinStay fits in
// [from, to): it must begin between the event's start and LateArrival
// after it, and end by the event's end.
func (st EventStay) ClippedFits(from, to time.Time) bool {
	begin := maxTime(st.Start, from)
	if begin.After(st.Start.Add(LateArrival)) {
		return false
	}
	return !begin.Add(st.MinStay).After(minTime(st.End, to))
}

// clippedNodes lays out the stays at a clipped event: starting at the
// event's start or LateArrival after it (inside the window), each ending at
// the earliest acceptable time, on the slot grid, at the last moment that
// still reaches the end point by back-by, and at the event's end. A stay
// keeps a share of the event's utility in proportion to how much of it the
// user sees: 80% for the shortest, all of it for the whole event.
func clippedNodes(w Window, n Node, st EventStay, cfg Config) []Node {
	span := st.End.Sub(st.Start)
	lastEnd := minTime(st.End, w.BackBy)
	var homeBy time.Time
	if w.End != nil {
		homeBy = w.BackBy.Add(-travel.Estimate(n.Loc, *w.End, w.Mode).Duration).Truncate(time.Minute)
	}
	perStart := max(cfg.MaxSlots, 2)
	var out []Node
	for _, begin := range []time.Time{st.Start, st.Start.Add(LateArrival)} {
		if begin.Before(w.From) {
			continue
		}
		earliest := begin.Add(st.MinStay)
		if earliest.After(lastEnd) {
			continue
		}
		ends := []time.Time{earliest, lastEnd}
		if !homeBy.IsZero() && homeBy.After(earliest) && homeBy.Before(lastEnd) {
			ends = append(ends, homeBy)
		}
		var grid []time.Time
		for t := earliest.Truncate(cfg.SlotStep).Add(cfg.SlotStep); t.Before(lastEnd); t = t.Add(cfg.SlotStep) {
			grid = append(grid, t)
		}
		// Grid ends up to the per-start cap (a long event), latest first.
		for i := len(grid) - 1; i >= 0 && len(ends) < perStart; i-- {
			ends = append(ends, grid[i])
		}
		ends = uniqueTimes(ends)
		for _, e := range ends {
			c := n
			c.Start, c.End, c.P75End = begin, e, e
			c.Flexible = false
			share := 1.0
			if span > 0 {
				share = float64(e.Sub(begin)) / float64(span)
				if share > 1 {
					share = 1
				}
			}
			c.Utility = n.Utility * (0.8 + 0.2*share)
			out = append(out, c)
		}
	}
	return out
}

// uniqueTimes sorts and dedupes.
func uniqueTimes(ts []time.Time) []time.Time {
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	out := ts[:0]
	for i, t := range ts {
		if i == 0 || !t.Equal(out[len(out)-1]) {
			out = append(out, t)
		}
	}
	return out
}
