package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"testing"
	"time"
)

func plainItinerary(host string) *models.Itinerary {
	return &models.Itinerary{
		HostID: host, Title: "Plan", Visibility: models.VisibilityOpen, Start: testNow, BackBy: testNow.Add(3 * time.Hour),
		StartPlace: models.PlaceDoc{Name: "Home"}, EndPlace: models.PlaceDoc{Name: "Home"},
		Items: []models.ItineraryItem{{ID: store.NewID(), Kind: models.ItemStop, Title: "Stop", Start: testNow.Add(time.Hour), End: testNow.Add(2 * time.Hour)}},
	}
}

// TestItinerariesMembership pins what the social area relies on: members,
// the host who cannot be removed, the group thread id, and deletion.
func TestItinerariesMembership(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	its := s.Itineraries()
	doc := plainItinerary("host")
	if err := its.Insert(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if doc.ID == "" || doc.Status != models.ItineraryActive || len(doc.MemberIDs) != 1 || doc.MemberIDs[0] != "host" || !doc.CreatedAt.Equal(testNow) {
		t.Fatalf("Insert: %+v", doc)
	}
	for i := range 2 {
		got, err := its.AddMember(ctx, doc.ID, "guest")
		if err != nil || len(got.MemberIDs) != 2 || got.MemberIDs[1] != "guest" {
			t.Fatalf("AddMember #%d: %v %v", i+1, err, got)
		}
	}
	if _, err := its.ForMember(ctx, doc.ID, "guest"); err != nil {
		t.Fatalf("ForMember guest: %v", err)
	}
	if _, err := its.ForMember(ctx, doc.ID, "stranger"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ForMember stranger: %v", err)
	}
	item := doc.Items[0].ID
	if got, i, err := its.FindItem(ctx, item, "guest"); err != nil || i != 0 || got.ID != doc.ID {
		t.Fatalf("FindItem: %v %d", err, i)
	}
	if _, _, err := its.FindItem(ctx, item, "stranger"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("FindItem stranger: %v", err)
	}
	if _, err := its.RemoveMember(ctx, doc.ID, "host"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the host cannot be removed: %v", err)
	}
	if err := its.SetThreadID(ctx, doc.ID, "thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := its.SetThreadID(ctx, "nope", "thread-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetThreadID unknown: %v", err)
	}
	if got, err := its.ByIDs(ctx, []string{doc.ID, "nope"}); err != nil || len(got) != 1 || got[doc.ID].ThreadID != "thread-1" {
		t.Fatalf("ByIDs: %v %v", err, got)
	}
	if got, err := its.RemoveMember(ctx, doc.ID, "guest"); err != nil || len(got.MemberIDs) != 1 {
		t.Fatalf("RemoveMember: %v %v", err, got)
	}
	if _, err := its.DeleteHost(ctx, doc.ID, "guest"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("only the host deletes: %v", err)
	}
	if _, err := its.DeleteHost(ctx, doc.ID, "host"); err != nil {
		t.Fatal(err)
	}
	if _, err := its.Get(ctx, doc.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a deleted plan is gone: %v", err)
	}
	if _, err := its.AddMember(ctx, doc.ID, "late"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("joining a deleted plan: %v", err)
	}
}

func TestRatingsAndItemStates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	note := "loved it"
	prev, err := s.Ratings().Upsert(ctx, models.Rating{UserID: "u1", ItemID: "i1", ItineraryID: "it", ActivityID: "act", Stars: 4, Tags: []string{"Good value"}, Note: &note})
	if err != nil || prev != nil {
		t.Fatalf("first rating: %v %v", prev, err)
	}
	prev, err = s.Ratings().Upsert(ctx, models.Rating{UserID: "u1", ItemID: "i1", ItineraryID: "it", Stars: 2})
	if err != nil || prev == nil || prev.Stars != 4 || prev.ActivityID != "act" || prev.Note == nil {
		t.Fatalf("re-rating returns the old one: %+v %v", prev, err)
	}
	if _, err := s.Ratings().Upsert(ctx, models.Rating{UserID: "u1", ItemID: "i2", ItineraryID: "it", Stars: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Ratings().ForUser(ctx, "u1", []string{"i1", "i2", "i3"})
	if err != nil || len(got) != 2 || got["i1"].Stars != 2 || got["i1"].Note != nil || got["i1"].ActivityID != "" || got["i1"].Tags == nil {
		t.Fatalf("ForUser: %v %+v", err, got["i1"])
	}
	if ids, err := s.Ratings().RatedItemIDs(ctx, "u1"); err != nil || len(ids) != 2 {
		t.Fatalf("RatedItemIDs: %v %v", ids, err)
	}
	if list, err := s.Ratings().ListByUser(ctx, "u1", 0); err != nil || len(list) != 2 {
		t.Fatalf("ListByUser: %v %d", err, len(list))
	}

	states := s.ItemStates()
	if _, err := states.Get(ctx, "u1", "i1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no state yet: %v", err)
	}
	text, shared := "meet at the gate", "shared"
	if err := states.SetNotes(ctx, "u1", "i1", "it", &text, &shared); err != nil {
		t.Fatal(err)
	}
	if err := states.SetTransitMode(ctx, "u1", "i1", "it", "marta"); err != nil {
		t.Fatal(err)
	}
	if err := states.SetNotes(ctx, "u1", "i1", "it", nil, nil); err != nil {
		t.Fatal(err)
	}
	state, err := states.Get(ctx, "u1", "i1")
	if err != nil || state.Notes != nil || state.NotesScope != "shared" || state.TransitMode != "marta" || state.ItineraryID != "it" || state.UserID != "u1" {
		t.Fatalf("state: %+v %v", state, err)
	}
	if all, err := states.ForUser(ctx, "u1", []string{"i1", "i2"}); err != nil || len(all) != 1 {
		t.Fatalf("ForUser: %v %v", all, err)
	}
}

func TestBumpTasteKeys(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	u := newUser("Taste Person", "taste@example.com", "tasteperson")
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.Users().BumpTaste(ctx, u.ID.Hex(), map[string]float64{"taste.$bad": 1}, true); err == nil {
		t.Fatal("a key that is not a plain field name must be refused")
	}
	if err := s.Users().BumpTaste(ctx, "0123456789abcdef01234567", map[string]float64{"outdoors": 1}, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if err := s.Users().BumpTaste(ctx, u.ID.Hex(), map[string]float64{"outdoors": 1, "social": 0}, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.Users().ByID(ctx, u.ID.Hex())
	if err != nil || got.Taste.Tags["outdoors"] != 0.65 || got.Taste.Tags["social"] != 0.35 || got.Taste.RatingCount != 1 {
		t.Fatalf("taste: %+v %v", got.Taste, err)
	}
}
