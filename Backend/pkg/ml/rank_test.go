package ml_test

import (
	"Backend/pkg/ml"
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// rankedPayload answers /v1/events/rank with the given ids, best first.
func rankedPayload(reranked bool, ids ...string) map[string]any {
	events := make([]any, len(ids))
	for i, id := range ids {
		var rerank any
		if reranked {
			rerank = 4.0 - float64(i)
		}
		events[i] = map[string]any{"event_id": id, "score": 0.9 - 0.1*float64(i), "rerank_score": rerank}
	}
	return map[string]any{"events": events, "model_version": "classifier-v1", "reranked": reranked}
}

func eventIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	var ids []string
	for _, e := range body["events"].([]any) {
		ids = append(ids, e.(map[string]any)["id"].(string))
	}
	return ids
}

var jordan = ml.UserVectors{Positive: basis(ml.Dim, 0), PositiveText: "Interests:\n- local food"}

func TestRankEventsChecksBeforeSending(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false))
	c := f.client()
	ctx := context.Background()

	if _, err := c.RankEvents(ctx, &ml.RankEventsRequest{User: ml.UserInput{PositiveEmbedding: make([]float64, ml.Dim)}}); !errors.Is(err, ml.ErrNoUserEmbedding) {
		t.Fatalf("zero positive: err = %v", err)
	}
	bad := map[string]*ml.RankEventsRequest{
		"event dim":   {User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)}, Events: []ml.EventInput{{ID: "a", Embedding: basis(8, 0)}}},
		"zero event":  {User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)}, Events: []ml.EventInput{{ID: "a", Embedding: make([]float64, ml.Dim)}}},
		"repeated id": {User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)}, Events: []ml.EventInput{{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "a", Embedding: basis(ml.Dim, 2)}}},
		"negative":    {User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0), NegativeEmbedding: basis(8, 0)}},
		"search":      {User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)}, SearchEmbedding: basis(8, 0)},
	}
	for name, req := range bad {
		if _, err := c.RankEvents(ctx, req); !errors.Is(err, ml.ErrInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if f.count("/v1/events/rank") != 0 {
		t.Fatal("invalid requests must not be sent")
	}

	if _, err := c.RankEvents(ctx, &ml.RankEventsRequest{User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)}}); err != nil {
		t.Fatal(err)
	}
	body := f.body("/v1/events/rank", -1)
	user := body["user"].(map[string]any)
	if negative := user["negative_embedding"].([]any); len(negative) != ml.Dim || negative[0] != 0.0 {
		t.Fatal("a missing negative vector is sent as zeros")
	}
	if events := body["events"].([]any); len(events) != 0 {
		t.Fatal("nil events are sent as []")
	}
	if _, ok := user["excluded_categories"]; ok {
		t.Fatal("empty constraints are omitted")
	}
	if options := body["options"].(map[string]any); options["rerank"] != false {
		t.Fatalf("rerank must be explicit: %v", options)
	}
}

func TestRankMapsScoresAndDropsMissingIDs(t *testing.T) {
	f := newFakeML(t)
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(true, "b", "a"))
	out, err := f.client().Rank(context.Background(), ml.RankInput{
		User: ml.UserInput{PositiveEmbedding: basis(ml.Dim, 0)},
		Events: []ml.EventInput{
			{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "b", Embedding: basis(ml.Dim, 2)}, {ID: "c", Embedding: basis(ml.Dim, 3)},
		},
		SearchEmbedding: basis(ml.Dim, 5),
		SearchText:      "Interests:\n- jazz",
		Opts:            ml.RankingOptions{Rerank: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Order, []string{"b", "a"}) || out.Sent != 3 || !out.Reranked || out.ModelVersion != "classifier-v1" {
		t.Fatalf("output: %+v", out)
	}
	if _, ok := out.Scores["c"]; ok || out.Scores["b"] != 0.9 || out.Rerank["b"] != 4 || out.Rerank["a"] != 3 {
		t.Fatalf("scores %v rerank %v", out.Scores, out.Rerank)
	}
	body := f.body("/v1/events/rank", -1)
	if body["search_text"] != "Interests:\n- jazz" || len(body["search_embedding"].([]any)) != ml.Dim {
		t.Fatalf("search not sent: %v", body["search_text"])
	}
}

func TestRankActivitiesSendsStoredVectors(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false, "c", "a"))
	start := time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC)
	activities := []ml.Candidate{
		{ID: "a", Embedding: filled(ml.Dim, 0.0312), Description: "Interests:\n- jazz", Price: ptr(12.5), Category: "live_music", Start: &start, Lat: ptr(33.77), Lng: ptr(-84.39)},
		{ID: "b", Embedding: nil, Description: "Interests:\n- hiking"},
		{ID: "c", Embedding: basis(ml.Dim, 2), Category: "park"},
		{ID: "a", Embedding: basis(ml.Dim, 9)}, // repeated: the first one wins
		{ID: "", Embedding: basis(ml.Dim, 3)},  // no id: skipped
		{ID: "d", Embedding: basis(512, 0)},    // wrong size: unembedded
	}
	res := f.client().RankActivities(context.Background(), jordan, activities, ml.Search{}, ml.Constraints{}, 0)
	if res.Reason != "" || !reflect.DeepEqual(res.IDs(), []string{"c", "a", "b", "d"}) {
		t.Fatalf("result: %+v", res)
	}
	if res.Sent != 2 || res.Returned != 2 || res.Dropped != 0 || res.Unembedded != 2 || res.ModelVersion != "classifier-v1" {
		t.Fatalf("counts: %+v", res)
	}
	if res.Items[0].Score == nil || *res.Items[0].Score != 0.9 || res.Items[2].Score != nil || res.Items[3].Score != nil {
		t.Fatalf("scores: %+v", res.Items)
	}
	body := f.body("/v1/events/rank", -1)
	if ids := eventIDs(t, body); !reflect.DeepEqual(ids, []string{"a", "c"}) {
		t.Fatalf("sent %v", ids)
	}
	a := body["events"].([]any)[0].(map[string]any)
	if a["embedding"].([]any)[0] != 0.0312 || a["description"] != "Interests:\n- jazz" || a["price"] != 12.5 ||
		a["category"] != "live_music" || a["start_time"] != "2026-09-26T22:00:00Z" || a["latitude"] != 33.77 {
		t.Fatalf("event a: %v", a)
	}
}

func TestRankActivitiesDoesNotReappendDropped(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false, "a", "ghost"))
	activities := []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "b"}, {ID: "c", Embedding: basis(ml.Dim, 2)}}
	res := f.client().RankActivities(context.Background(), jordan, activities, ml.Search{}, ml.Constraints{}, 0)
	if !reflect.DeepEqual(res.IDs(), []string{"a", "b"}) || res.Dropped != 1 || res.Returned != 1 {
		t.Fatalf("c was dropped by the service and must stay dropped: %+v", res)
	}
}

func TestRankActivitiesNoUserEmbedding(t *testing.T) {
	f := newFakeML(t)
	activities := []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "b"}, {ID: "c", Embedding: basis(ml.Dim, 2)}}
	for _, user := range []ml.UserVectors{{}, {Positive: make([]float64, ml.Dim)}, {Positive: basis(8, 0)}} {
		res := f.client().RankActivities(context.Background(), user, activities, ml.Search{Embedding: basis(ml.Dim, 3)}, ml.Constraints{}, 2)
		if res.Reason != ml.ReasonNoUserEmbedding || !reflect.DeepEqual(res.IDs(), []string{"a", "b"}) || res.Items[0].Score != nil {
			t.Fatalf("result: %+v", res)
		}
	}
	if f.count("/v1/events/rank")+f.count("/healthz") != 0 {
		t.Fatal("no call without a user vector")
	}
}

func TestRankActivitiesWithoutEmbeddedCandidates(t *testing.T) {
	f := newFakeML(t)
	res := f.client().RankActivities(context.Background(), jordan, []ml.Candidate{{ID: "a"}, {ID: "b"}}, ml.Search{}, ml.Constraints{}, 0)
	if res.Reason != ml.ReasonNoEmbeddedCandidates || !reflect.DeepEqual(res.IDs(), []string{"a", "b"}) || res.Unembedded != 2 {
		t.Fatalf("result: %+v", res)
	}
	if f.count("/v1/events/rank") != 0 {
		t.Fatal("nothing to send")
	}
}

func TestRankActivitiesKeepsInputOrderWhenTheServiceFails(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusInternalServerError, map[string]any{"detail": "Compatibility inference failed."})
	activities := []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "b"}, {ID: "c", Embedding: basis(ml.Dim, 2)}}
	res := f.client().RankActivities(context.Background(), jordan, activities, ml.Search{}, ml.Constraints{}, 0)
	if res.Reason != ml.ReasonMLUnavailable || !reflect.DeepEqual(res.IDs(), []string{"a", "b", "c"}) || res.Items[0].Score != nil || res.Sent != 2 {
		t.Fatalf("result: %+v", res)
	}
}

func TestRankActivitiesRerankFlagFromHealth(t *testing.T) {
	activities := []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}}
	cases := []struct {
		name       string
		jev        bool
		allow      bool
		user       ml.UserVectors
		rerank     bool
		healthHits int
	}{
		{"jev on", true, true, jordan, true, 1},
		{"jev off", false, true, jordan, false, 1},
		{"ML_RERANK=false", true, false, jordan, false, 0},
		{"no profile text", true, true, ml.UserVectors{Positive: basis(ml.Dim, 0)}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeML(t)
			f.reply("/healthz", http.StatusOK, healthPayload(tc.jev))
			f.reply("/v1/events/rank", http.StatusOK, rankedPayload(tc.rerank, "a"))
			c := f.client(func(o *ml.Options) { o.Rerank = tc.allow })
			res := c.RankActivities(context.Background(), tc.user, activities, ml.Search{}, ml.Constraints{}, 0)
			options := f.body("/v1/events/rank", -1)["options"].(map[string]any)
			if options["rerank"] != tc.rerank || res.Reranked != tc.rerank {
				t.Fatalf("options %v, reranked %v", options, res.Reranked)
			}
			if tc.rerank && options["rerank_top_k"] != float64(ml.DefaultRerankTopK) {
				t.Fatalf("top k: %v", options["rerank_top_k"])
			}
			if got := f.count("/healthz"); got != tc.healthHits {
				t.Fatalf("/healthz calls: %d", got)
			}
		})
	}
}

func TestRankActivitiesSendsConstraintsAndSearch(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false, "a"))
	from := time.Date(2026, 9, 26, 18, 0, 0, 0, time.UTC)
	cons := ml.Constraints{
		MaxPrice: ptr(15.0), Lat: ptr(33.7766), Lng: ptr(-84.389), MaxDistanceMiles: ptr(3.1),
		AvailableStart: &from, AvailableEnd: ptr(from.Add(4 * time.Hour)), ExcludedCategories: []string{"nightclub"},
	}
	user := ml.UserVectors{Positive: basis(ml.Dim, 0), Negative: basis(ml.Dim, 6), PositiveText: "p", NegativeText: "n"}
	search := ml.Search{Embedding: basis(ml.Dim, 4), Text: "Timing:\n- friday afternoon"}
	f.client().RankActivities(context.Background(), user, []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}}, search, cons, 5)
	body := f.body("/v1/events/rank", -1)
	u := body["user"].(map[string]any)
	if u["max_price"] != 15.0 || u["latitude"] != 33.7766 || u["longitude"] != -84.389 || u["max_distance_miles"] != 3.1 ||
		u["available_start"] != "2026-09-26T18:00:00Z" || u["available_end"] != "2026-09-26T22:00:00Z" ||
		!reflect.DeepEqual(u["excluded_categories"], []any{"nightclub"}) || u["positive_text"] != "p" || u["negative_text"] != "n" {
		t.Fatalf("user: %v", u)
	}
	if u["negative_embedding"].([]any)[6] != 1.0 {
		t.Fatal("the stored negative vector is sent")
	}
	if body["search_text"] != "Timing:\n- friday afternoon" || body["search_embedding"].([]any)[4] != 1.0 {
		t.Fatal("search not sent")
	}
	if options := body["options"].(map[string]any); options["limit"] != 5.0 {
		t.Fatalf("limit: %v", options)
	}
}

func TestRankActivitiesClipsDropInsToTheWindow(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false))
	at := func(h int) *time.Time { return ptr(time.Date(2026, 9, 26, h, 0, 0, 0, time.UTC)) }
	activities := []ml.Candidate{
		{ID: "festival", Embedding: basis(ml.Dim, 1), DropIn: true, Start: at(10), End: at(20)},
		{ID: "concert", Embedding: basis(ml.Dim, 2), Start: at(10), End: at(20)},
		{ID: "breakfast", Embedding: basis(ml.Dim, 3), DropIn: true, Start: at(8), End: at(9)},
		{ID: "open-ended", Embedding: basis(ml.Dim, 4), DropIn: true, Start: at(10)},
	}
	cons := ml.Constraints{AvailableStart: at(14), AvailableEnd: at(18)}
	f.client().RankActivities(context.Background(), jordan, activities, ml.Search{}, cons, 0)
	times := map[string][2]any{}
	for _, e := range f.body("/v1/events/rank", -1)["events"].([]any) {
		m := e.(map[string]any)
		times[m["id"].(string)] = [2]any{m["start_time"], m["end_time"]}
	}
	want := map[string][2]any{
		"festival":   {"2026-09-26T14:00:00Z", "2026-09-26T18:00:00Z"},
		"concert":    {"2026-09-26T10:00:00Z", "2026-09-26T20:00:00Z"},
		"breakfast":  {"2026-09-26T08:00:00Z", "2026-09-26T09:00:00Z"},
		"open-ended": {"2026-09-26T10:00:00Z", nil},
	}
	if !reflect.DeepEqual(times, want) {
		t.Fatalf("times sent: %v", times)
	}
}

func TestRankActivitiesLimitAppliesToTheFinalList(t *testing.T) {
	f := newFakeML(t)
	f.reply("/healthz", http.StatusOK, healthPayload(false))
	f.reply("/v1/events/rank", http.StatusOK, rankedPayload(false, "a"))
	activities := []ml.Candidate{{ID: "a", Embedding: basis(ml.Dim, 1)}, {ID: "b"}, {ID: "c"}}
	res := f.client().RankActivities(context.Background(), jordan, activities, ml.Search{}, ml.Constraints{}, 2)
	if !reflect.DeepEqual(res.IDs(), []string{"a", "b"}) {
		t.Fatalf("ids: %v", res.IDs())
	}
}
