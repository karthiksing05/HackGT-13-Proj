package itinerary

import (
	"sort"
	"time"
)

// Busy blocks (Window.Busy) are when the user is already taken: the events
// on their calendar. Nothing is scheduled on top of one, neither a visit
// nor a leg, and the time inside one is not idle time. Where a class is,
// the calendar does not say, so the user is taken to come from the
// previous place: a leg into a visit sets off when the previous visit ends
// or, with busy blocks in between, when the last of them ends (LegInto);
// the leg to the end point sets off at the first free moment long enough
// for it (LegAfter). The saved plan and its re-timing lay legs out the
// same way.

// MergeBusy sorts busy blocks and merges the ones that overlap or touch;
// empty ones go. Window.Busy is in this form.
func MergeBusy(blocks []Interval) []Interval {
	sorted := make([]Interval, 0, len(blocks))
	for _, b := range blocks {
		if b.End.After(b.Start) {
			sorted = append(sorted, Interval{Start: b.Start.UTC(), End: b.End.UTC()})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Start.Equal(sorted[j].Start) {
			return sorted[i].Start.Before(sorted[j].Start)
		}
		return sorted[i].End.Before(sorted[j].End)
	})
	var out []Interval
	for _, b := range sorted {
		if n := len(out); n > 0 && !b.Start.After(out[n-1].End) {
			out[n-1].End = maxTime(out[n-1].End, b.End)
			continue
		}
		out = append(out, b)
	}
	return out
}

// Clashes reports whether [start, end) overlaps a busy block.
func Clashes(busy []Interval, start, end time.Time) bool {
	return firstClash(busy, start, end) != nil
}

// firstClash is the earliest busy block overlapping [start, end), or nil.
func firstClash(busy []Interval, start, end time.Time) *Interval {
	for i := range busy {
		if busy[i].Start.Before(end) && busy[i].End.After(start) {
			return &busy[i]
		}
	}
	return nil
}

// clashesAny reports whether any of ivs overlaps a busy block.
func clashesAny(busy, ivs []Interval) bool {
	for _, iv := range ivs {
		if iv.End.After(iv.Start) && Clashes(busy, iv.Start, iv.End) {
			return true
		}
	}
	return false
}

// WithoutBusy is ivs (in order) without the busy blocks: their free parts.
func WithoutBusy(ivs, busy []Interval) []Interval {
	if len(busy) == 0 {
		return ivs
	}
	var out []Interval
	for _, iv := range ivs {
		cur := iv.Start
		for _, b := range busy {
			if !b.End.After(cur) {
				continue
			}
			if !b.Start.Before(iv.End) {
				break
			}
			if b.Start.After(cur) {
				out = append(out, Interval{Start: cur, End: b.Start})
			}
			cur = b.End
		}
		if iv.End.After(cur) {
			out = append(out, Interval{Start: cur, End: iv.End})
		}
	}
	return out
}

// LegInto is when the leg into a visit that starts at `by` sets off, no
// earlier than `after`: then, or when the last busy block ending by the
// visit's start ends.
func LegInto(busy []Interval, after, by time.Time) time.Time {
	t := after
	for _, b := range busy {
		if b.End.After(t) && !b.End.After(by) {
			t = b.End
		}
	}
	return t
}

// LegAfter is when a leg of the given length sets off, no earlier than
// `after`: then, or after each busy block it would run into.
func LegAfter(busy []Interval, after time.Time, leg time.Duration) time.Time {
	t := after
	for _, b := range busy {
		if !b.End.After(t) {
			continue
		}
		if !b.Start.Before(t.Add(leg)) {
			break
		}
		t = b.End
	}
	return t
}

// FreeBetween is the time in [from, to) outside the busy blocks (negative
// when to is before from).
func FreeBetween(busy []Interval, from, to time.Time) time.Duration {
	d := to.Sub(from)
	if d <= 0 {
		return d
	}
	for _, b := range busy {
		if s, e := maxTime(b.Start, from), minTime(b.End, to); e.After(s) {
			d -= e.Sub(s)
		}
	}
	return d
}

// LongestFree is the longest stretch of [from, to) outside the busy blocks.
func LongestFree(from, to time.Time, busy []Interval) time.Duration {
	var best time.Duration
	for _, iv := range WithoutBusy([]Interval{{Start: from, End: to}}, busy) {
		best = maxDur(best, iv.End.Sub(iv.Start))
	}
	return best
}
