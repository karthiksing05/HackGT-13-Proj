package main

import (
	"Backend/pkg/store"
	"fmt"
	"io"
	"strings"
)

// printer writes the report: what the seed would do (a dry run), did, or
// would take away.
type printer struct{ w io.Writer }

func (p printer) f(format string, args ...any) { fmt.Fprintf(p.w, format+"\n", args...) }

var symbols = map[action]string{actCreate: "+", actUpdate: "~", actKeep: "=", actConflict: "!"}

// counts adds up actions.
type counts struct{ create, update, keep, conflict int }

func (c *counts) add(a action) {
	switch a {
	case actCreate:
		c.create++
	case actUpdate:
		c.update++
	case actKeep:
		c.keep++
	case actConflict:
		c.conflict++
	}
}

func (c counts) String() string {
	var parts []string
	for _, part := range []struct {
		n    int
		what string
	}{{c.create, "new"}, {c.update, "rewritten"}, {c.keep, "kept as they are"}, {c.conflict, "in conflict"}} {
		if part.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.n, part.what))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func (p printer) header(o Options) {
	if len(o.History) > 0 {
		var who []string
		for _, sel := range o.History {
			who = append(who, "@"+sel.handle)
		}
		p.f("SideQuests history seed for %s (everything it creates is tagged %s: %q)", strings.Join(who, ", "), fieldSeed, historyTag)
	} else {
		p.f("SideQuests showcase seed (everything it writes is tagged %s: %q)", fieldSeed, seedTag)
	}
	p.f("Target: %s", o.Target)
	switch {
	case o.Remove && o.Apply:
		p.f("Mode: remove (deletes what this seed wrote)")
	case o.Remove:
		p.f("Mode: remove, dry run (nothing is deleted; add --apply to delete)")
	case o.Apply:
		p.f("Mode: write")
	default:
		p.f("Mode: dry run (nothing is written; add --apply to write)")
	}
}

// primary is the collection a section is about (a plan's chat and messages
// are counted with it).
func primary(s *section) string {
	switch s.title {
	case "People":
		return store.CollUsers
	case "Friendships":
		return store.CollFriendships
	case "Friend requests":
		return store.CollFriendRequests
	case "Plans":
		return store.CollItineraries
	}
	return store.CollForumPosts
}

func (p printer) world(wp *worldPlan) {
	w := wp.spec
	p.f("")
	title := strings.ToUpper(w.name[:1]) + w.name[1:]
	if wp.skipped != "" {
		p.f("%s (%s): skipped, %s", title, w.about, wp.skipped)
		return
	}
	p.f("%s (%s) · catalog %s: %d documents, %d places", title, w.about, w.catalog, wp.catalog.docs, len(wp.catalog.places))
	now := wp.clock.now.In(wp.clock.loc).Format("Mon Jan 2, 3:04 PM MST")
	if w.name == worldSaltlight {
		p.f("  Times from the demo date: it is %s there now (%s)", now, wp.clock.loc)
		p.f("  Demo account: %s <%s>, id %s (linked to, never changed)", wp.sandy.Name, wp.sandy.Email, wp.sandy.ID.Hex())
	} else {
		p.f("  Times from now: %s (%s)", now, wp.clock.loc)
	}
	for _, s := range wp.sections {
		var c counts
		for _, ch := range s.changes {
			if ch.coll == primary(s) {
				c.add(ch.act)
			}
		}
		head := fmt.Sprintf("  %s: %s", s.title, c)
		if s.extra != "" {
			head += "; " + s.extra
		}
		p.f("%s", head)
		for _, ch := range s.changes {
			if ch.coll != primary(s) && ch.act != actConflict {
				continue
			}
			line := "    " + symbols[ch.act] + " "
			if ch.label != "" {
				line += ch.label
			} else {
				line += fmt.Sprintf("%s %v", ch.coll, ch.id)
			}
			if ch.note != "" {
				line += " (" + ch.note + ")"
			}
			p.f("%s", line)
			if ch.detail != "" {
				p.f("        %s", ch.detail)
			}
		}
	}
	for _, n := range wp.notes {
		p.f("  Note: %s", n)
	}
}

// totals adds up every change of the worlds that run.
func totals(plans []*worldPlan) counts {
	var c counts
	for _, wp := range plans {
		for _, s := range wp.sections {
			for _, ch := range s.changes {
				c.add(ch.act)
			}
		}
	}
	return c
}

func (p printer) written(wp *worldPlan, t *tally) {
	var parts []string
	for _, coll := range writeOrder {
		if n := t.created[coll] + t.updated[coll]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", coll, n))
		}
	}
	p.f("  %s: wrote %s", wp.spec.name, strings.Join(parts, ", "))
	var taste []string
	for _, part := range []struct {
		n    int
		what string
	}{{t.tasteML, "built by the ML service"}, {t.tasteBlend, "blended from catalog vectors"},
		{t.tasteKept, "kept from an earlier ML build"}, {t.tasteNone, "without vectors (nothing in the catalog to blend)"}} {
		if part.n > 0 {
			taste = append(taste, fmt.Sprintf("%d %s", part.n, part.what))
		}
	}
	if len(taste) > 0 {
		p.f("    taste vectors: %s", strings.Join(taste, ", "))
	}
	for _, m := range wp.members {
		if strings.HasPrefix(m.taste, "the ML service failed") {
			p.f("    %s: %s", m.name, m.taste)
		}
	}
}

func (p printer) removal(r *removal) {
	p.f("")
	title := strings.ToUpper(r.world[:1]) + r.world[1:]
	if r.total() == 0 {
		p.f("%s: nothing of this seed's", title)
		return
	}
	p.f("%s:", title)
	for _, c := range r.tagged {
		line := fmt.Sprintf("  %s: %d", c.coll, c.n)
		switch c.coll {
		case store.CollUsers:
			line += " (" + strings.Join(r.people, ", ") + ")"
		case store.CollItineraries:
			line += " (" + strings.Join(r.plans, ", ") + ")"
		}
		p.f("%s", line)
	}
	for _, c := range r.inside {
		p.f("  %s: %d more (%s)", c.coll, c.n, c.what)
	}
}
