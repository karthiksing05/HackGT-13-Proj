package wire_test

import (
	"Backend/pkg/ml"
	"Backend/pkg/planner/mongosource"
	"Backend/pkg/planner/wire"
	"Backend/pkg/testutil"
	"testing"
)

func TestPlannerWiresTheStoreAndIndexes(t *testing.T) {
	db := testutil.DB(t)
	t.Setenv("PLANNER_ROUNDS", "2")
	svc, err := wire.Planner(t.Context(), db, ml.NewClient("http://127.0.0.1:1"))
	if err != nil || svc == nil {
		t.Fatalf("wire: %v", err)
	}
	if svc.P.Cfg.Rounds != 2 || svc.P.Scorer == nil || svc.P.Search == nil {
		t.Errorf("config and ML not wired: rounds %d", svc.P.Cfg.Rounds)
	}
	specs, err := db.Collection(mongosource.PoolsCollection).Indexes().ListSpecifications(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	if !names[mongosource.ExpiresIndex] || !names[mongosource.StopIDIndex] {
		t.Errorf("pool indexes %v", names)
	}
	// Without ML the planner still answers: an unknown cursor is the last page.
	svc, err = wire.Planner(t.Context(), db, nil)
	if err != nil || svc.P.Scorer != nil {
		t.Fatalf("no ML: %v", err)
	}
	batch, err := svc.More(t.Context(), nil, "dag_nope_3")
	if err != nil || !batch.Done || len(batch.Options) != 0 {
		t.Errorf("more: %+v %v", batch, err)
	}
	if _, err := wire.Planner(t.Context(), nil, nil); err == nil {
		t.Error("no database must be an error")
	}
}
