package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// liveStore runs on a clock near the wall clock: forum posts carry a TTL
// index, which deletes by the real time.
func liveStore(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	return testutil.Store(t, func() time.Time { return now }), now
}

func TestEnsureDMIsOnePerPairUnderRace(t *testing.T) {
	s, _ := liveStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	created := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, b := "user-a", "user-b"
			if i%2 == 1 {
				a, b = b, a
			}
			th, isNew, err := s.Threads().EnsureDM(ctx, a, b)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("EnsureDM: %v", err)
				return
			}
			ids[th.ID] = true
			if isNew {
				created++
			}
		}(i)
	}
	wg.Wait()
	if len(ids) != 1 || created != 1 {
		t.Fatalf("threads %v, created %d", ids, created)
	}
	th, err := s.Threads().DM(ctx, "user-b", "user-a")
	if err != nil || th.IsGroup || len(th.MemberIDs) != 2 || th.Unread == nil || th.ReadAt == nil {
		t.Fatalf("DM: %v %+v", err, th)
	}
	if _, err := s.Threads().DM(ctx, "user-a", "user-c"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing DM: %v", err)
	}
}

func TestOneFreePostPerAuthor(t *testing.T) {
	s, now := liveStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			post := &models.ForumPost{AuthorID: "author", Visibility: models.PostVisibilityEveryone,
				Location: models.GeoJSONPoint{Type: "Point", Coordinates: []float64{-84.38, 33.78}}, Text: "Free", Until: now.Add(time.Hour)}
			if err := s.Forum().ReplaceFreePost(ctx, post); err != nil {
				t.Errorf("ReplaceFreePost: %v", err)
			}
		}()
	}
	wg.Wait()
	n, err := s.Collection(store.CollForumPosts).CountDocuments(ctx, bson.M{"authorId": "author"})
	if err != nil || n != 1 {
		t.Fatalf("posts left: %d %v", n, err)
	}
	live, err := s.Forum().LiveFreePost(ctx, "author", now)
	if err != nil || live.Type != models.PostFreeNow || !live.ExpiresAt.Equal(live.Until) {
		t.Fatalf("live post: %v %+v", err, live)
	}
	if _, err := s.Forum().LiveFreePost(ctx, "author", now.Add(2*time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a post past its time is not live: %v", err)
	}
	if err := s.Forum().DeleteFreePost(ctx, "someone-else", live.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete by another user: %v", err)
	}
}

func TestAddMemberGuards(t *testing.T) {
	s, now := liveStore(t)
	ctx := context.Background()
	insert := func(mod func(*models.Itinerary)) string {
		it := &models.Itinerary{ID: store.NewID(), HostID: "host", MemberIDs: []string{"host"}, Title: "Plan",
			Start: now.Add(2 * time.Hour), BackBy: now.Add(4 * time.Hour), Visibility: models.VisibilityOpen,
			Items: []models.ItineraryItem{}, Status: models.ItineraryActive}
		mod(it)
		if _, err := s.Collection(store.CollItineraries).InsertOne(ctx, it); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	open := insert(func(*models.Itinerary) {})
	it, err := s.Joins().AddMember(ctx, open, "joiner", now)
	if err != nil || len(it.MemberIDs) != 2 {
		t.Fatalf("join: %v %+v", err, it)
	}
	if again, err := s.Joins().AddMember(ctx, open, "joiner", now); err != nil || len(again.MemberIDs) != 2 {
		t.Fatalf("joining twice: %v %+v", err, again)
	}
	full := insert(func(it *models.Itinerary) { it.MaxGroupSize = testutil.Ptr(1) })
	locked := insert(func(it *models.Itinerary) { it.LockAt = testutil.Ptr(now) })
	started := insert(func(it *models.Itinerary) { it.Start = now })
	private := insert(func(it *models.Itinerary) { it.Visibility = models.VisibilityJustMe })
	past := insert(func(it *models.Itinerary) { it.Status = models.ItineraryPast })
	for id, want := range map[string]error{full: store.ErrPlanFull, locked: store.ErrPlanClosed, started: store.ErrPlanClosed,
		private: store.ErrNotFound, past: store.ErrNotFound, "missing": store.ErrNotFound} {
		if _, err := s.Joins().AddMember(ctx, id, "joiner", now); !errors.Is(err, want) {
			t.Errorf("AddMember(%s) = %v, want %v", id, err, want)
		}
	}
	if !errors.Is(store.ErrPlanFull, store.ErrConflict) || !errors.Is(store.ErrPlanClosed, store.ErrConflict) {
		t.Error("join refusals map to 409 when they reach api.Fail")
	}
	if _, removed, err := s.Joins().RemoveMember(ctx, open, "host"); err != nil || removed {
		t.Fatalf("the host is never removed: %v %v", removed, err)
	}
	if it, removed, err := s.Joins().RemoveMember(ctx, open, "joiner"); err != nil || !removed || len(it.MemberIDs) != 1 {
		t.Fatalf("remove: %v %v %+v", removed, err, it)
	}
}

func TestInviteCodes(t *testing.T) {
	code := regexp.MustCompile(`^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{8}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := store.NewInviteCode()
		if !code.MatchString(c) {
			t.Fatalf("code %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 199 {
		t.Fatalf("codes repeat: %d distinct of 200", len(seen))
	}
	for in, want := range map[string]string{"abcd-efgh": "ABCDEFGH", " o1iL 2345 ": "01112345", "ZZ": "ZZ"} {
		if got := store.NormalizeInviteCode(in); got != want {
			t.Errorf("NormalizeInviteCode(%q) = %q, want %q", in, got, want)
		}
	}
}
