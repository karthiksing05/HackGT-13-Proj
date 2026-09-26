package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeProfiles struct {
	mu      sync.Mutex
	refresh []string
	rated   []string
	err     error
}

func (f *fakeProfiles) Refresh(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = append(f.refresh, userID)
	return f.err
}

func (f *fakeProfiles) Rated(_ context.Context, userID, activityID string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rated = append(f.rated, userID+"/"+activityID)
	return f.err
}

func (f *fakeProfiles) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refresh), len(f.rated)
}

func TestProfileHelpersAreNoOpsWithoutProfiles(t *testing.T) {
	d := &Deps{}
	if err := d.RefreshProfile(context.Background(), "u1", time.Second); err != nil {
		t.Fatalf("RefreshProfile without Profiles: %v", err)
	}
	d.RefreshProfileAsync("u1", time.Second)
	d.RatedAsync("u1", "a1", 5, time.Second)
}

func TestProfileHelpersCallThrough(t *testing.T) {
	f := &fakeProfiles{err: errors.New("ml down")}
	d := &Deps{Profiles: f}
	if err := d.RefreshProfile(context.Background(), "u1", time.Second); err == nil {
		t.Fatal("RefreshProfile should return the refresher's error for the caller to log")
	}
	d.RefreshProfileAsync("u1", time.Second)
	d.RatedAsync("u1", "a1", 4, time.Second)
	d.RatedAsync("u1", "", 4, time.Second) // no activity: skipped
	deadline := time.Now().Add(2 * time.Second)
	for {
		refreshes, rated := f.counts()
		if refreshes == 2 && rated == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d refreshes and %d rated calls, want 2 and 1", refreshes, rated)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
