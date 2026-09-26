package store_test

import (
	"Backend/pkg/store"
	"context"
	"errors"
	"sync"
	"testing"
)

func TestDevices(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	devices := s.Devices()

	if err := devices.Upsert(ctx, "user-a", "tok-1", "ios"); err != nil {
		t.Fatal(err)
	}
	if err := devices.Upsert(ctx, "user-a", "tok-1", "ios"); err != nil {
		t.Fatalf("re-registering must be idempotent: %v", err)
	}
	if err := devices.Upsert(ctx, "user-a", "tok-2", "android"); err != nil {
		t.Fatal(err)
	}
	list, err := devices.ForUser(ctx, "user-a")
	if err != nil || len(list) != 2 || list[0].Token != "tok-1" || list[0].Platform != "ios" || list[0].ID == "" || !list[0].CreatedAt.Equal(testNow) {
		t.Fatalf("ForUser: %v %+v", err, list)
	}

	// The same install signed in to another account: the token moves.
	if err := devices.Upsert(ctx, "user-b", "tok-1", "ios"); err != nil {
		t.Fatal(err)
	}
	if list, _ := devices.ForUser(ctx, "user-a"); len(list) != 1 || list[0].Token != "tok-2" {
		t.Fatalf("token did not leave user-a: %+v", list)
	}
	if list, _ := devices.ForUser(ctx, "user-b"); len(list) != 1 || list[0].Token != "tok-1" {
		t.Fatalf("token did not move to user-b: %+v", list)
	}

	if err := devices.Delete(ctx, "user-a", "tok-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleting another account's token: %v", err)
	}
	if err := devices.Delete(ctx, "user-b", "tok-1"); err != nil {
		t.Fatal(err)
	}
	if err := devices.Delete(ctx, "user-b", "tok-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}

	// Concurrent first registrations of one token end as one document.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- devices.Upsert(ctx, "user-c", "tok-race", "ios")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert: %v", err)
		}
	}
	if list, _ := devices.ForUser(ctx, "user-c"); len(list) != 1 {
		t.Fatalf("concurrent upserts left %d documents", len(list))
	}
}
