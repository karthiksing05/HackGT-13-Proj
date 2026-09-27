package planner

import (
	"Backend/pkg/models"
	"slices"
	"testing"
	"time"
)

// An event that can be joined late or left early is feasible whenever a
// stay of MinStay fits: starting at most 15 minutes late, ending by back-by.
func TestFeasibleClippedStays(t *testing.T) {
	tp := newTestPlanner(nil, testConfig())
	o := defaultReq() // 18:00–23:00
	spec := tp.spec(t, sandy(), o)
	q := baseQuery(&spec, tp.Cfg, radiusFor(&spec, tp.Cfg, spec.MaxLegKm))
	near := offsetKm(seasideMkt, 0.5, 0)
	set := func(name string, start, end time.Time) models.Activity {
		a := synthEvent(name, "live_music", near, start, int(end.Sub(start).Minutes()), []string{"music"}, priceOf(5))
		a.End = timep(end)
		return a
	}
	play := synthEvent("Late play", "theater", near, localAt(22, 0), 120, nil, priceOf(5))
	cases := []struct {
		act  models.Activity
		want string
	}{
		{set("Runs past back-by", localAt(21, 30), localAt(23, 45)), ""},
		{set("Began ten minutes ago", localAt(17, 50), localAt(19, 30)), ""},
		{set("Began twenty minutes ago", localAt(17, 40), localAt(19, 30)), "outside_window"},
		{set("Too little left", localAt(22, 40), localAt(23, 30)), "outside_window"}, // 25 of 50 minutes needed, 20 left
		{play, "outside_window"}, // attended whole: 22:00 + 2 h is past 23:00
	}
	for _, c := range cases {
		if got := eventFeasible(&c.act, &spec, &q, tp.Cfg.Itinerary); got != c.want {
			t.Errorf("%s: %q, want %q", c.act.Name, got, c.want)
		}
		if c.want == "" && !MatchesQuery(&c.act, &q) {
			t.Errorf("%s: the query must keep what the check keeps", c.act.Name)
		}
	}
	// An expansion slot applies the same 15 minutes.
	slot := q
	slot.From, slot.To = localAt(20, 0), localAt(22, 0)
	a := set("In the slot, a little late", localAt(19, 50), localAt(21, 30))
	if got := eventFeasible(&a, &spec, &slot, tp.Cfg.Itinerary); got != "" {
		t.Errorf("slot: %q", got)
	}
	a = set("Before the slot", localAt(19, 30), localAt(21, 30))
	if got := eventFeasible(&a, &spec, &slot, tp.Cfg.Itinerary); got != "outside_slot" {
		t.Errorf("slot: %q", got)
	}
}

func TestFacetAliases(t *testing.T) {
	for in, want := range map[string]string{
		"Live music": "Music", "live-music": "Music", "Outdoor": "Outdoors", "outdoors": "Outdoors",
		"Meet people": "Meet people", "meet_people": "Meet people", "Night out": "Nightlife",
	} {
		if f, ok := FacetByName(in); !ok || f.Name != want {
			t.Errorf("%q → %q %v, want %q", in, f.Name, ok, want)
		}
	}
	if _, ok := FacetByName("Underwater basket weaving"); ok {
		t.Error("unknown picks have no facet")
	}
}

func TestPreferStrongAndTravelShare(t *testing.T) {
	p := func(sig string, weak int, travel float64) ScoredPlan {
		sp := plan(0.5, sig)
		sp.Metrics.WeakStops, sp.Metrics.TravelShare = weak, travel
		return sp
	}
	plans := []ScoredPlan{p("a", 0, 0.2), p("b", 1, 0.2), p("c", 0, 0.6), p("d", 0, 0.3)}
	strong, n := preferStrong(plans, 3)
	if n != 1 || len(strong) != 3 || strong[1].Signature != "c" {
		t.Errorf("prefer strong: %d left out, %v", n, strong)
	}
	if kept, n := preferStrong(plans, 4); n != 0 || len(kept) != 4 {
		t.Error("with fewer than a page of strong plans, weak ones stay")
	}
	near, n := withinTravelShare(strong, 0.5)
	if n != 1 || len(near) != 2 || near[0].Signature != "a" || near[1].Signature != "d" {
		t.Errorf("travel share: %d left out, %v", n, near)
	}
	if all, n := withinTravelShare([]ScoredPlan{p("x", 0, 0.7), p("y", 0, 0.8)}, 0.5); n != 0 || len(all) != 2 {
		t.Error("when every plan travels a lot, they all stay")
	}
}

// The §7 check knows clipped stays: joined at most 15 minutes late, left
// by the end, at least MinStay long; and every leg, home legs included,
// within the range.
func TestCheckOptionClippedStays(t *testing.T) {
	gig := synthEvent("Set", "live_music", offsetKm(seasideMkt, 0.5, 0), localAt(18, 30), 150, []string{"music"}, priceOf(5))
	gig.End = timep(localAt(21, 0))
	tp := newTestPlanner([]models.Activity{gig}, testConfig())
	o := defaultReq()
	o.from, o.backBy = localAt(17, 30), localAt(21, 0)
	batch, spec := tp.generate(t, sandy(), o)
	if len(batch.Options) == 0 {
		t.Fatalf("no option: %s", batch.Reason)
	}
	opt := batch.Options[0]
	s := opt.Stops[0]
	if s.Flexible || s.Arrive.Before(*gig.Start) || !s.Depart.Before(*gig.End) || opt.LateFlag {
		t.Fatalf("stay %v–%v flexible %v late %v", s.Arrive.In(ny), s.Depart.In(ny), s.Flexible, opt.LateFlag)
	}
	catalog := catalogByID([]models.Activity{gig})
	if v := guaranteeViolations(spec, opt, catalog); len(v) != 0 {
		t.Fatalf("sound option flagged: %v", v)
	}
	bad := func(edit func(o *Option)) []string {
		cp := opt
		cp.Stops = append([]Stop(nil), opt.Stops...)
		edit(&cp)
		return guaranteeViolations(spec, cp, catalog)
	}
	if v := bad(func(o *Option) { o.Stops[0].Arrive = gig.Start.Add(20 * time.Minute) }); len(v) == 0 {
		t.Error("joining 20 minutes late must be flagged")
	}
	if v := bad(func(o *Option) { o.Stops[0].Depart = gig.End.Add(time.Minute) }); len(v) == 0 {
		t.Error("leaving after the end must be flagged")
	}
	if v := bad(func(o *Option) { o.Stops[0].Depart = o.Stops[0].Arrive.Add(30 * time.Minute) }); len(v) == 0 {
		t.Error("a stay under MinStay must be flagged")
	}
	far := offsetKm(seasideMkt, 0, spec.MaxLegKm+0.3)
	if v := bad(func(o *Option) { o.Stops[0].Place.Lat, o.Stops[0].Place.Lng = far.Lat, far.Lng }); len(v) == 0 {
		t.Error("a home leg over the range must be flagged")
	}
}

// An event that can be left early reaches the ML service clipped to the
// window, like a drop-in; one attended whole keeps its own times.
func TestEventInputClipsStays(t *testing.T) {
	near := offsetKm(seasideMkt, 0.4, 0)
	from, backBy := localAt(18, 0).UTC(), localAt(21, 0).UTC()
	set := synthEvent("Set", "live_music", near, localAt(17, 50), 150, nil, nil)
	set.End = timep(localAt(21, 30))
	play := synthEvent("Play", "theater", near, localAt(19, 0), 90, nil, nil)
	play.End = timep(localAt(20, 30))
	e := eventInput(newCandidate(set), from, backBy)
	if !e.StartTime.Equal(from) || !e.EndTime.Equal(backBy) {
		t.Errorf("set sent as %v–%v", e.StartTime, e.EndTime)
	}
	e = eventInput(newCandidate(play), from, backBy)
	if !e.StartTime.Equal(play.Start.UTC()) || !e.EndTime.Equal(play.End.UTC()) {
		t.Errorf("play sent as %v–%v", e.StartTime, e.EndTime)
	}
}

// A balanced 3-hour window whose best plan has one stop is under pace: the
// next round raises the per-stop bonus (and the issue can expand the pool).
func TestUnderPaceRaisesTheStopBonus(t *testing.T) {
	park := synthPlace("Only park", "park", offsetKm(seasideMkt, 0, 0.5), dailyHours(0, 24), []string{"outdoor"}, priceOf(0))
	cfg := testConfig()
	cfg.MinCandidates = 0
	tp := newTestPlanner([]models.Activity{park}, cfg)
	o := defaultReq()
	o.backBy = localAt(21, 0)
	batch, _ := tp.generate(t, sandy(), o)
	run := tp.run(t, batch.RunID)
	if len(run.Rounds) < 2 || !hasIssue(run.Rounds[0].Issues, "under_pace", "") {
		t.Fatalf("rounds %d, issues %+v", len(run.Rounds), run.Rounds[0].Issues)
	}
	if !slices.Contains(run.Rounds[0].Adapted, "stop_bonus:0.05") || run.Rounds[1].StopBonus != cfg.UnderPaceBonus {
		t.Errorf("adapted %v, round 1 bonus %v", run.Rounds[0].Adapted, run.Rounds[1].StopBonus)
	}
}
