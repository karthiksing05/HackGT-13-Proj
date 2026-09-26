package testutil

import (
	"context"
	"sync"
	"testing"
	"time"
)

// RatedCall is one Profiles.Rated call seen by ProfilesRecorder.
type RatedCall struct {
	UserID     string
	ActivityID string
	Stars      int
}

// ProfilesRecorder is a fake api.Profiles that records every call. Set Err
// to make every call fail (the handlers must still succeed).
type ProfilesRecorder struct {
	mu        sync.Mutex
	refreshed []string
	rated     []RatedCall
	Err       error
}

func (p *ProfilesRecorder) Refresh(_ context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshed = append(p.refreshed, userID)
	return p.Err
}

func (p *ProfilesRecorder) Rated(_ context.Context, userID, activityID string, stars int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rated = append(p.rated, RatedCall{UserID: userID, ActivityID: activityID, Stars: stars})
	return p.Err
}

// Refreshes is how many times Refresh ran for userID.
func (p *ProfilesRecorder) Refreshes(userID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, id := range p.refreshed {
		if id == userID {
			n++
		}
	}
	return n
}

// Rated returns a copy of every Rated call so far.
func (p *ProfilesRecorder) RatedCalls() []RatedCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]RatedCall(nil), p.rated...)
}

// WaitRefreshes waits until Refresh ran at least n times for userID (the
// async helpers run in the background).
func (p *ProfilesRecorder) WaitRefreshes(t testing.TB, userID string, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for p.Refreshes(userID) < n {
		if time.Now().After(deadline) {
			t.Fatalf("profile refreshes for %s: got %d, want %d", userID, p.Refreshes(userID), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitRated waits until at least n Rated calls were recorded.
func (p *ProfilesRecorder) WaitRated(t testing.TB, n int, timeout time.Duration) []RatedCall {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for len(p.RatedCalls()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("rated calls: got %d, want %d", len(p.RatedCalls()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return p.RatedCalls()
}
