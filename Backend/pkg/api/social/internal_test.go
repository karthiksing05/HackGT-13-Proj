package social

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"fmt"
	"testing"
	"time"
)

func TestEqualShares(t *testing.T) {
	for _, tc := range []struct {
		total, n int
		want     string
	}{
		{4000, 3, "[1334 1333 1333]"},
		{100, 3, "[34 33 33]"},
		{1001, 2, "[501 500]"},
		{0, 2, "[0 0]"},
		{5, 0, "[]"},
		{-1, 2, "[]"},
	} {
		if got := fmt.Sprint(equalShares(tc.total, tc.n)); got != tc.want {
			t.Errorf("equalShares(%d, %d) = %s, want %s", tc.total, tc.n, got, tc.want)
		}
	}
}

// TestBalancesPort mirrors MockLedger.balances: + they owe me, − I owe them,
// only current members, shares recomputed when they do not match the split.
func TestBalancesPort(t *testing.T) {
	members := []string{"me", "maya", "dev"}
	expenses := []*models.Expense{
		{AmountCents: 4200, PayerID: "dev", SplitAmong: members, Shares: []int{1400, 1400, 1400}},
		{AmountCents: 750, PayerID: "me", SplitAmong: members, Shares: []int{250, 250, 250}},
		{AmountCents: 1000, PayerID: "maya", SplitAmong: []string{"me", "maya"}}, // no shares stored
		{AmountCents: 600, PayerID: "gone", SplitAmong: []string{"me", "gone"}, Shares: []int{300, 300}},
	}
	got := fmt.Sprint(balances(expenses, "me", members))
	if want := "[{maya -250} {dev -1150}]"; got != want {
		t.Fatalf("balances = %s, want %s", got, want)
	}
	if got := fmt.Sprint(balances(nil, "me", []string{"me"})); got != "[]" {
		t.Fatalf("alone: %s", got)
	}
}

func TestLabels(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ny) }
	now := at(2026, 9, 26, 12, 0) // Saturday
	for _, tc := range []struct{ got, want string }{
		{whenLabel(at(2026, 9, 26, 17, 30), at(2026, 9, 26, 20, 0), now), "Today · 5:30–8 PM"},
		{whenLabel(at(2026, 9, 27, 17, 30), at(2026, 9, 27, 20, 0), now), "Tomorrow · 5:30–8 PM"},
		{whenLabel(at(2026, 10, 3, 6, 0), at(2026, 10, 3, 10, 0), now), "Oct 3 · 6–10 AM"},
		{whenLabel(at(2026, 9, 29, 11, 0), at(2026, 9, 29, 13, 0), now), "Tue · 11 AM–1 PM"},
		{lockLabel(at(2026, 9, 26, 17, 0), now), "Locks 5:00 PM"},
		{lockLabel(at(2026, 9, 27, 21, 0), now), "Locks Sun 9 PM"},
		{lockLabel(at(2026, 10, 5, 9, 30), now), "Locks Oct 5 9:30 AM"},
		{lockLabel(at(2026, 9, 26, 9, 0), now), "Locked 9:00 AM"},
		{lastTimeLabel(at(2026, 9, 26, 17, 12).Add(-12*time.Hour), now), "5:12 AM"},
		{lastTimeLabel(at(2026, 9, 24, 8, 0), now), "Thu"},
		{lastTimeLabel(at(2026, 9, 19, 8, 0), now), "Sep 19"},
		{ago(now.Add(-30*time.Second), now), "just now"},
		{ago(now.Add(-5*time.Minute), now), "5 min ago"},
		{ago(now.Add(-3*time.Hour), now), "3 h ago"},
		{ago(now.Add(-72*time.Hour), now), "on Sep 23"},
		{balanceChip(-900), "You owe $9"},
		{balanceChip(450), "You're owed $4.50"},
		{balanceChip(0), "Settled up"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
	// Across the spring-forward night the next day is still "Tomorrow".
	march := at(2027, 3, 13, 22, 0)
	if got := dayLabel(at(2027, 3, 14, 18, 0), march); got != "Tomorrow" {
		t.Errorf("DST: %q", got)
	}
	if got := dayDiff(at(2026, 11, 2, 0, 30), at(2026, 10, 31, 23, 30)); got != 2 {
		t.Errorf("fall back: %d", got)
	}
}

func TestStatusLine(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 9, 26, 14, 0, 0, 0, ny)
	open := &models.User{Status: "open"}
	busy := &models.User{Status: "busy"}
	until := time.Date(2026, 9, 26, 18, 30, 0, 0, ny)
	shared := &models.Itinerary{Title: "Thrift crawl", Visibility: models.VisibilityFriends}
	private := &models.Itinerary{Title: "Dentist", Visibility: models.VisibilityJustMe}
	old := now.Add(-72 * time.Hour)
	for _, tc := range []struct {
		user     *models.User
		facts    store.PresenceFacts
		since    time.Time
		line     string
		activity contract.FriendActivity
	}{
		{open, store.PresenceFacts{FreeUntil: &until}, old, "Free until 6:30 PM", contract.ActivityFree},
		{open, store.PresenceFacts{Sidequest: shared}, old, "On a sidequest · Thrift crawl", contract.ActivityOnSidequest},
		{open, store.PresenceFacts{Sidequest: private}, old, "On a sidequest", contract.ActivityOnSidequest},
		{busy, store.PresenceFacts{FreeUntil: &until}, old, "Busy", contract.ActivityBusy},
		{open, store.PresenceFacts{}, now.Add(-time.Hour), "Just added", contract.ActivityNew},
		{open, store.PresenceFacts{}, old, "Open to plans", contract.ActivityFree},
		{open, store.PresenceFacts{}, time.Time{}, "Open to plans", contract.ActivityFree},
	} {
		line, activity := statusLine(tc.user, tc.facts, tc.since, now)
		if line != tc.line || activity != tc.activity {
			t.Errorf("statusLine(%s, %+v) = %q %s, want %q %s", tc.user.Status, tc.facts, line, activity, tc.line, tc.activity)
		}
	}
}
