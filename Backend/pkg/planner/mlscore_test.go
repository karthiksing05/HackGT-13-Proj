package planner

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeML is the ML service's HTTP surface with deterministic answers:
// score = 0.5 + 0.5·cos(user, event), Jev = 4·score, and the ids in drop
// left out as the service's filters would.
type fakeML struct {
	mu        sync.Mutex
	jev       bool
	rankFails bool
	drop      map[string]bool
	ranks     []ml.RankEventsRequest
	searches  []map[string]any
}

func (f *fakeML) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "jev": f.jev,
				"ranking":   map[string]any{"model_version": "fake-1"},
				"embedding": map[string]any{"model": ml.Model, "dim": testDim, "provider": "fake"}})
		case "/v1/search-profile":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.searches = append(f.searches, body)
			mood, _ := body["mood_text"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"search_text": "Interests:\n- " + mood,
				"search_embedding": vectorOf("outdoor", "food"), "model": ml.Model, "dim": testDim, "provider": "fake"})
		case "/v1/events/rank":
			var req ml.RankEventsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.ranks = append(f.ranks, req)
			if f.rankFails {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{"detail": "Embedding provider unavailable."})
				return
			}
			var events []map[string]any
			for _, e := range req.Events {
				if f.drop[e.ID] {
					continue
				}
				score := 0.5 + 0.5*dot(req.User.PositiveEmbedding, e.Embedding)
				ev := map[string]any{"event_id": e.ID, "score": score, "rerank_score": nil}
				if req.Options.Rerank {
					ev["rerank_score"] = 4 * score
				}
				events = append(events, ev)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"events": events, "model_version": "fake-1", "reranked": req.Options.Rerank})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// calls returns copies of what the fake received, under its lock.
func (f *fakeML) calls() ([]ml.RankEventsRequest, []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ml.RankEventsRequest(nil), f.ranks...), append([]map[string]any(nil), f.searches...)
}

func (f *fakeML) setRankFails(v bool) {
	f.mu.Lock()
	f.rankFails = v
	f.mu.Unlock()
}

func (f *fakeML) client(t *testing.T) *ml.Client {
	return ml.NewClientWithOptions(ml.Options{BaseURL: f.serve(t).URL, Dim: testDim, Rerank: true})
}

func TestMLScorerSpeaksTheServiceUnits(t *testing.T) {
	f := &fakeML{drop: map[string]bool{}}
	scorer := NewMLScorer(f.client(t))
	near := offsetKm(seasideMkt, 0.4, 0)
	fixed := synthEvent("Gig", "live_music", near, localAt(19, 0), 90, []string{"music"}, priceOf(12.5))
	fixed.End = timep(localAt(20, 30))
	running := synthEvent("Street fair", "festival", near, localAt(16, 0), 60, nil, priceOf(0))
	running.Attendance, running.End = strp("drop_in"), timep(localAt(21, 0))
	openEnded := synthEvent("Open studio", "gallery", near, localAt(17, 0), 60, nil, nil)
	openEnded.Attendance = strp("drop_in")
	convention := synthEvent("Convention", "festival", near, localAt(9, 0).Add(-24*time.Hour), 60, nil, priceOf(40))
	convention.End = timep(localAt(20, 0).Add(24 * time.Hour))
	park := synthPlace("Park", "park", near, nil, nil, nil)
	var cands []*Candidate
	for _, a := range []models.Activity{fixed, running, openEnded, convention, park} {
		a.Embedding = vectorFor(&a)
		cands = append(cands, newCandidate(a))
	}
	cands[0].Text = "Interests:\n- live music"
	f.drop[park.ID.Hex()] = true
	budget := int64(2500)
	from, backBy := localAt(18, 0).UTC(), localAt(23, 0).UTC()
	res, err := scorer.Score(t.Context(), ScoreRequest{
		User:              &UserContext{PositiveText: "Interests:\n- live music", NegativeText: "Environment:\n- crowded venue"},
		PositiveEmbedding: sandyPositive, NegativeEmbedding: vectorOf("cat:nightclub"),
		Candidates: cands, SearchEmbedding: vectorOf("food"), SearchText: "food",
		Rerank: true, RerankTopK: 20, From: from, BackBy: backBy,
		Center: &seasideMkt, MaxDistanceKm: 8.04672, MaxPriceCents: &budget, ExcludedCategories: []string{"bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scores) != 4 || res.Scores[park.ID.Hex()] != 0 || len(res.Rerank) != 4 || res.ModelVersion != "fake-1" || !res.Reranked {
		t.Errorf("result %+v", res)
	}
	if _, ok := res.Scores[park.ID.Hex()]; ok {
		t.Error("a dropped id must stay missing")
	}
	ranks, _ := f.calls()
	req := ranks[0]
	u := req.User
	if u.MaxPrice == nil || *u.MaxPrice != 25 || u.MaxDistanceMiles == nil || math.Abs(*u.MaxDistanceMiles-5) > 1e-9 ||
		*u.Latitude != seasideMkt.Lat || *u.Longitude != seasideMkt.Lng || !u.AvailableStart.Equal(from) || !u.AvailableEnd.Equal(backBy) ||
		strings.Join(u.ExcludedCategories, ",") != "bar" || u.PositiveText == "" || u.NegativeText == "" || len(u.NegativeEmbedding) != testDim {
		t.Errorf("user side %+v", u)
	}
	if !req.Options.Rerank || req.Options.RerankTopK != 20 || req.SearchText != "food" || len(req.SearchEmbedding) != testDim {
		t.Errorf("options %+v", req.Options)
	}
	byID := map[string]ml.EventInput{}
	for _, e := range req.Events {
		byID[e.ID] = e
	}
	g := byID[fixed.ID.Hex()]
	if g.Price == nil || *g.Price != 12.5 || !g.StartTime.Equal(fixed.Start.UTC()) || !g.EndTime.Equal(fixed.End.UTC()) || g.Description == "" || g.Category != "live_music" {
		t.Errorf("fixed event %+v", g)
	}
	if e := byID[running.ID.Hex()]; !e.StartTime.Equal(from) || !e.EndTime.Equal(running.End.UTC()) {
		t.Errorf("a running drop-in is clipped to the window: %v–%v", e.StartTime, e.EndTime)
	}
	if e := byID[openEnded.ID.Hex()]; !e.StartTime.Equal(from) || !e.EndTime.Equal(backBy) || e.Price != nil {
		t.Errorf("an open-ended drop-in: %v–%v price %v", e.StartTime, e.EndTime, e.Price)
	}
	if e := byID[convention.ID.Hex()]; !e.StartTime.Equal(from) || !e.EndTime.Equal(backBy) {
		t.Errorf("a multi-day span is a drop-in: %v–%v", e.StartTime, e.EndTime)
	}
	if e := byID[park.ID.Hex()]; e.StartTime != nil || e.EndTime != nil || e.Price != nil || *e.Latitude != near.Lat {
		t.Errorf("a place has no times and an unknown price stays unknown: %+v", e)
	}
}

func TestMLScorerSearchVectorAndJev(t *testing.T) {
	f := &fakeML{jev: true}
	scorer := NewMLScorer(f.client(t))
	sv, err := scorer.SearchVector(t.Context(), SearchInput{MoodText: "tacos by the water", Tags: []string{"Food"}, Who: "friends",
		Pace: "chill", Budget: 0, StartTime: localAt(18, 0), BackBy: localAt(23, 0), Timezone: "America/New_York"})
	if err != nil || len(sv.Embedding) != testDim || !strings.Contains(sv.Text, "tacos") {
		t.Fatalf("search vector: %+v %v", sv, err)
	}
	_, searches := f.calls()
	body := searches[0]
	if b, ok := body["budget"]; !ok || b.(float64) != 0 {
		t.Errorf("a Free budget must still be sent: %v", body["budget"])
	}
	if body["pace"] != "relaxed" || body["timezone"] != "America/New_York" || body["start_time"] == nil || body["back_by"] == nil || body["who"] != "friends" {
		t.Errorf("search body %v", body)
	}
	// JevAvailable never blocks: the first call starts a health check.
	deadline := time.Now().Add(2 * time.Second)
	for !scorer.JevAvailable() {
		if time.Now().After(deadline) {
			t.Fatal("jev never became available")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPlannerOverTheMLService(t *testing.T) {
	f := &fakeML{jev: true, drop: map[string]bool{}}
	acts := saltlight(t)
	for id := range dropSet(acts) {
		f.drop[id] = true
	}
	cfg := testConfig()
	cfg.Jev = "async"
	tp := newTestPlanner(acts, cfg)
	scorer := NewMLScorer(f.client(t))
	tp.Planner.Scorer, tp.Planner.Search = scorer, scorer
	for deadline := time.Now().Add(2 * time.Second); !scorer.JevAvailable(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("jev never became available")
		}
	}
	user := sandy()
	user.PositiveText = "Interests:\n- live music"
	o := defaultReq()
	o.tags, o.mood = []string{"Outdoors", "Food"}, "something chill outside, then food"
	batch, spec := tp.generate(t, user, o)
	if len(batch.Options) == 0 {
		t.Fatalf("no options: %s", batch.Reason)
	}
	run := tp.run(t, batch.RunID)
	if run.ML.Mode != "classifier" || run.ML.Model != "fake-1" || run.Counts.MLDropped == 0 || run.ML.Jev == nil || !run.ML.Jev.Requested {
		t.Errorf("ml log %+v, dropped %d", run.ML, run.Counts.MLDropped)
	}
	catalog := catalogByID(acts)
	for _, opt := range tp.allOptions(t, user, batch) {
		assertGuarantees(t, spec, opt, catalog)
		for _, s := range opt.Stops {
			if f.drop[s.ActivityID] {
				t.Errorf("%s was dropped by the service but is in %s", s.ActivityID, opt.ID)
			}
		}
	}
	ranks, _ := f.calls()
	for _, req := range ranks {
		seen := map[string]bool{}
		for _, e := range req.Events {
			if seen[e.ID] || len(e.Embedding) != testDim || f.drop[e.ID] && req.Options.Rerank {
				t.Errorf("event %s sent badly (rerank %v)", e.ID, req.Options.Rerank)
			}
			seen[e.ID] = true
		}
		if req.Options.Rerank {
			t.Error("the classifier calls never ask for the rerank")
		}
	}
	// The async rerank runs after the answer: texts attached, scores cached.
	if len(tp.jobs) != 1 {
		t.Fatalf("%d background jobs", len(tp.jobs))
	}
	tp.jobs[0]()
	ranks, _ = f.calls()
	last := ranks[len(ranks)-1]
	if !last.Options.Rerank || last.Options.RerankTopK != cfg.JevTopK || last.Events[0].Description == "" {
		t.Errorf("rerank request %+v", last.Options)
	}
	run = tp.run(t, batch.RunID)
	pool := tp.pool(t, batch.RunID)
	if run.ML.Jev == nil || run.ML.Jev.CompletedAt == nil || len(run.ML.Jev.Scores) == 0 || run.ML.Jev.Err != "" {
		t.Errorf("jev log %+v", run.ML.Jev)
	}
	jev := 0
	for _, sc := range pool.Scores {
		if sc.Jev != nil {
			jev++
		}
	}
	if jev != len(run.ML.Jev.Scores) {
		t.Errorf("%d jev scores cached, %d logged", jev, len(run.ML.Jev.Scores))
	}

	// A failing service: planning goes on without scores, and says why.
	f.setRankFails(true)
	tp2 := newTestPlanner(acts, testConfig())
	tp2.Planner.Scorer, tp2.Planner.Search = scorer, scorer
	batch, _ = tp2.generate(t, user, o)
	if run := tp2.run(t, batch.RunID); len(batch.Options) == 0 || !strings.HasPrefix(run.ML.Mode, "fallback:") {
		t.Errorf("fallback: %d options, mode %q", len(batch.Options), run.ML.Mode)
	}
}
