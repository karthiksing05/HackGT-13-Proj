package planner_test

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Planning on the demo clock (DEMO_DATE): a demo account plans the
// catalog's days as if they were still ahead, whatever the real day, while
// its plan pools expire by the real clock. Same HTTP surface, real planner
// and Mongo store as TestPlanningOverHTTP.
func TestPlanningOnTheDemoDate(t *testing.T) {
	if store.ActivityCollection != store.CollDemoActivities {
		t.Skip("demo clock requires ActivityCollection = CollDemoActivities")
	}
	ny, _ := time.LoadLocation("America/New_York")
	for _, tc := range []struct {
		name         string
		demoDate     string
		real         time.Time // the pinned real clock
		from, backBy time.Time // the window asked for
		mood         string
		tags         []string
	}{
		// Thursday Sep 24 while it is really Sunday Sep 27: Saturday evening is still ahead.
		{"saturday evening seen from Sep 24", "2026-09-24", time.Date(2026, 9, 27, 12, 0, 0, 0, ny),
			time.Date(2026, 9, 26, 18, 0, 0, 0, ny), time.Date(2026, 9, 26, 23, 0, 0, 0, ny),
			"live music or comedy tonight", []string{"Music"}},
		// Sunday Sep 27, the catalog's busiest day, on a real day a week later: "today" 1–6 PM.
		{"today on Sep 27", "2026-09-27", time.Date(2026, 10, 5, 12, 0, 0, 0, ny),
			time.Date(2026, 9, 27, 13, 0, 0, 0, ny), time.Date(2026, 9, 27, 18, 0, 0, 0, ny),
			"something active outside, meet people", []string{"Active", "Meet people"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.New(t, testutil.WithNow(tc.real.UTC()), testutil.WithConfig(func(c *config.Config) { c.DemoDate = tc.demoDate }))
			ctx := t.Context()
			db := srv.Store.DB()
			acts := seedDemoCatalog(t, db)
			profile := profileVector(t, acts)
			mlSrv := httptest.NewServer(http.HandlerFunc((&fakeML{search: profile}).handler))
			t.Cleanup(mlSrv.Close)
			cfg := planner.DefaultConfig()
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
				"email": testutil.UniqueEmail("demo"), "city": "saltlight",
				"homeBase":          models.HomeBase{Name: seaside.Name, Lat: seaside.Coordinate.Lat, Lng: seaside.Coordinate.Lng},
				"positiveEmbedding": profile, "positiveText": "Interests:\n- outdoor recreation\n- live music",
			}); err != nil {
				t.Fatal(err)
			}
			req := planRequest(seaside, tc.from, tc.backBy)
			req.MoodText, req.Tags = tc.mood, tc.tags
			day := tc.from.Format("2006-01-02")

			var batch contract.PlanBatch
			decodeStrict(t, srv.Do(t, "POST", "/plans/generate", req, sandy).Expect(t, http.StatusOK), &batch)
			if len(batch.Options) == 0 || batch.Reason != nil {
				t.Fatalf("generate on %s: %d options, reason %q", day, len(batch.Options), deref(batch.Reason))
			}
			all := append([]contract.PlanOption(nil), batch.Options...)
			for cursor := batch.Cursor; cursor != nil; {
				var page contract.PlanBatch
				decodeStrict(t, srv.Do(t, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: *cursor}, sandy).Expect(t, http.StatusOK), &page)
				all = append(all, page.Options...)
				cursor = page.Cursor
			}
			events := map[string]bool{}
			for _, opt := range all {
				for _, s := range opt.Stops {
					if s.ArriveTime.Before(tc.from) || s.DepartTime.After(tc.backBy) {
						t.Errorf("stop %s runs %v–%v outside the window", s.Title, s.ArriveTime, s.DepartTime)
					}
					if s.Kind == contract.StopKindEvent && s.ArriveTime.In(ny).Format("2006-01-02") == day {
						events[s.Title] = true
					}
				}
			}
			if len(events) == 0 {
				t.Fatalf("no %s event among %d options", day, len(all))
			}
			t.Logf("%s events planned: %v", day, events)

			// The pool expires by the real clock (the TTL monitor's), not the demo date's.
			var pool struct {
				ExpiresAt time.Time `bson:"expiresAt"`
			}
			if err := db.Collection(mongosource.PoolsCollection).FindOne(ctx, bson.M{"userId": sandy.UserID}).Decode(&pool); err != nil {
				t.Fatal(err)
			}
			if !pool.ExpiresAt.Equal(tc.real.Add(cfg.PoolTTL)) {
				t.Errorf("pool expires %s, want %s (real time + %s)", pool.ExpiresAt, tc.real.Add(cfg.PoolTTL), cfg.PoolTTL)
			}

			// A window before the demo date is over for Sandy.
			var empty contract.PlanBatch
			before := planRequest(seaside, tc.from.AddDate(0, 0, -4), tc.backBy.AddDate(0, 0, -4))
			decodeStrict(t, srv.Do(t, "POST", "/plans/generate", before, sandy).Expect(t, http.StatusOK), &empty)
			if empty.Reason == nil || *empty.Reason != "invalid_request: window already ended" {
				t.Errorf("a window before the demo date: reason %q", deref(empty.Reason))
			}
			// Everyone else lives in real time: the same window is over for them.
			other := srv.Signup(t, "Real Person")
			decodeStrict(t, srv.Do(t, "POST", "/plans/generate", req, other).Expect(t, http.StatusOK), &empty)
			if empty.Reason == nil || *empty.Reason != "invalid_request: window already ended" || len(empty.Options) != 0 {
				t.Errorf("a normal account on real time: %d options, reason %q", len(empty.Options), deref(empty.Reason))
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
