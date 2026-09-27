package planner_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/testutil"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Must-see picks end to end: Sandy searches her catalog (a copy of
// freetime.demo_activities) on Create's vibe step, picks what she finds,
// and every option of every page visits it; the real router, planner,
// Mongo store and ML client (over a fake ML service).
func TestMustSeeOverHTTP(t *testing.T) {
	srv := testutil.New(t, testutil.WithNow(testutil.Fixed)) // Saturday 26 Sep 2026, noon in New York
	ctx := t.Context()
	db := srv.Store.DB()
	acts := seedDemoCatalog(t, db)
	catalog := map[string]models.Activity{}
	for _, a := range acts {
		catalog[a.ID.Hex()] = a
	}
	profile := profileVector(t, acts)
	mlSrv := httptest.NewServer(http.HandlerFunc((&fakeML{search: profile}).handler))
	t.Cleanup(mlSrv.Close)
	cfg := planner.DefaultConfig()
	cfg.PoolTTL, cfg.RunTTL = 24*365*time.Hour, 24*365*time.Hour // the TTL monitor uses the wall clock, the test clock is frozen
	st := mongosource.New(db, srv.Clock)
	if err := st.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	scorer := planner.NewMLScorer(ml.NewClient(mlSrv.URL))
	p, err := planner.New(cfg, planner.Deps{Source: st, Embeddings: st, Lookup: st, Pools: st, Scorer: scorer, Search: scorer, Clock: srv.Clock})
	if err != nil {
		t.Fatal(err)
	}
	srv.Deps.Planner = planner.NewService(p)

	sandy := srv.Signup(t, "Sandy Byte")
	if _, err := srv.Store.Users().Update(ctx, sandy.UserID, bson.M{
		"catalog": "demo_activities", "city": "saltlight",
		"homeBase":          models.HomeBase{Name: seaside.Name, Lat: seaside.Coordinate.Lat, Lng: seaside.Coordinate.Lng},
		"positiveEmbedding": profile, "positiveText": "Interests:\n- outdoor recreation\n- live music",
	}); err != nil {
		t.Fatal(err)
	}

	// The search: "jazz" on the plan's day finds the Sunset Jazz set.
	var hits []contract.ActivityHit
	decodeStrict(t, srv.Do(t, "GET", fmt.Sprintf("/activities/search?q=jazz&date=2026-09-26&near=%v,%v", seaside.Coordinate.Lat, seaside.Coordinate.Lng), nil, sandy).
		Expect(t, http.StatusOK), &hits)
	if len(hits) == 0 || hits[0].Title != "Sunset Jazz on Pier Nine" || hits[0].Kind != contract.StopKindEvent || hits[0].DistanceMi == nil {
		t.Fatalf("jazz on Saturday: %+v", hits)
	}
	jazz := hits[0].ID
	t.Logf("picked %s: %s", hits[0].Title, hits[0].Subtitle)

	// Generate with it picked (walking, 17:00–22:00): every option of every
	// page has it, within the set's times.
	from := time.Date(2026, 9, 26, 21, 0, 0, 0, time.UTC)
	req := planRequest(seaside, from, from.Add(5*time.Hour))
	req.MustInclude = []string{jazz}
	var batch contract.PlanBatch
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", req, sandy).Expect(t, http.StatusOK), &batch)
	if len(batch.Options) == 0 || batch.Reason != nil {
		t.Fatalf("generate with the jazz: %d options, reason %v", len(batch.Options), batch.Reason)
	}
	all := append([]contract.PlanOption(nil), batch.Options...)
	for cursor := batch.Cursor; cursor != nil; {
		var page contract.PlanBatch
		decodeStrict(t, srv.Do(t, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: *cursor}, sandy).Expect(t, http.StatusOK), &page)
		all = append(all, page.Options...)
		cursor = page.Cursor
	}
	set := catalog[jazz]
	for _, opt := range all {
		found := false
		for _, s := range opt.Stops {
			if s.ActivityID == nil || *s.ActivityID != jazz {
				continue
			}
			found = true
			if s.ArriveTime.Before(*set.Start) || s.DepartTime.After(*set.End) {
				t.Errorf("%s: the jazz at %v–%v, the set runs %v–%v", opt.Name, s.ArriveTime, s.DepartTime, set.Start, set.End)
			}
		}
		if !found {
			t.Errorf("option %s (%s) has no jazz", opt.ID, opt.Name)
		}
	}
	t.Logf("%d options, all with the jazz", len(all))

	// A pick on another day, and too many picks.
	sunday := ""
	for id, a := range catalog {
		if a.Name == "Harbor Dumpling Brunch Crawl" { // Sunday morning
			sunday = id
		}
	}
	req.MustInclude = []string{jazz, sunday}
	decodeStrict(t, srv.Do(t, "POST", "/plans/generate", req, sandy).Expect(t, http.StatusOK), &batch)
	if want := "must_include_unavailable: " + catalog[sunday].Name; len(batch.Options) != 0 || batch.Reason == nil || *batch.Reason != want {
		t.Errorf("a Sunday pick on Saturday: %+v, want %q", batch, want)
	}
	req.MustInclude = nil
	for i := 0; i < 11; i++ {
		req.MustInclude = append(req.MustInclude, fmt.Sprintf("%024x", i+1))
	}
	if msg := srv.Do(t, "POST", "/plans/generate", req, sandy).Expect(t, http.StatusBadRequest).Message(); msg != planner.MsgTooManyPicks {
		t.Errorf("eleven picks: %q", msg)
	}
}
