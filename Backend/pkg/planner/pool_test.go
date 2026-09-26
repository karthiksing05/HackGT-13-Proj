package planner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCursorsAndIDs(t *testing.T) {
	runID := "0192a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	cur := EncodeCursor(runID, 3)
	if cur != "dag_"+runID+"_3" {
		t.Errorf("cursor = %s", cur)
	}
	id, off, ok := DecodeCursor(cur)
	if !ok || id != runID || off != 3 {
		t.Errorf("decode = %s %d %v", id, off, ok)
	}
	for _, bad := range []string{"", "legacy", "dag_", "dag_x", "dag_x_-1", "dag_x_y"} {
		if _, _, ok := DecodeCursor(bad); ok {
			t.Errorf("%q should not decode", bad)
		}
	}
	opt := OptionID(runID, 2)
	if got, ok := RunIDFromOption(opt); !ok || got != runID {
		t.Errorf("option id round trip: %s %v", got, ok)
	}
	if _, ok := RunIDFromOption("opt-a"); ok {
		t.Error("mock option ids are not ours")
	}
	if a, ok := ActivityIDFromStop(StopID("6ab7449b25e9f6cc68d5827b", 1)); !ok || a != "6ab7449b25e9f6cc68d5827b" {
		t.Errorf("stop id: %s %v", a, ok)
	}
	if a, ok := ActivityIDFromStop(AltStopID("6ab7449b25e9f6cc68d5827b", 0)); !ok || a != "6ab7449b25e9f6cc68d5827b" {
		t.Errorf("alt id: %s %v", a, ok)
	}
	if _, ok := ActivityIDFromStop("opt-a-0"); ok {
		t.Error("foreign stop id")
	}
}

func TestMemPoolStoreTTL(t *testing.T) {
	clock := NewFakeClock(testNow)
	store := NewMemPoolStore(clock)
	ctx := context.Background()
	pool := &PlanPool{ID: "r1", ExpiresAt: testNow.Add(time.Hour), Alternatives: map[string]Stop{}}
	if err := store.SavePool(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAlternatives(ctx, "r1", []Stop{{ID: "alt_x_0"}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPool(ctx, "r1")
	if err != nil || len(got.Alternatives) != 1 {
		t.Fatalf("get: %v %+v", err, got)
	}
	clock.Advance(2 * time.Hour)
	if _, err := store.GetPool(ctx, "r1"); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("expired pool: %v", err)
	}
}
