package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUsersPrefsIntegrationsAndPhoto(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	users := s.Users()
	u := newUser("Pat Prefs", "pat@example.com", "patprefs")
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	id := u.ID.Hex()

	saved, err := users.SavePrefs(ctx, id, models.UserPrefs{Company: "solo", Pace: "packed", Ratings: map[string]int{"food": 5}})
	if err != nil || !saved.SetupComplete || saved.Prefs.Company != "solo" || saved.Prefs.Ratings["food"] != 5 || saved.Prefs.Answers == nil {
		t.Fatalf("SavePrefs: %v %+v", err, saved)
	}
	if _, err := users.SavePrefs(ctx, bson.NewObjectID().Hex(), models.UserPrefs{Company: "solo"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SavePrefs unknown user: %v", err)
	}

	if err := users.SetIntegration(ctx, id, "google", true); err != nil {
		t.Fatal(err)
	}
	got, _ := users.ByID(ctx, id)
	if !got.Integrations.Google.Connected || got.Integrations.Google.At == nil || !got.Integrations.Google.At.Equal(testNow) || got.Integrations.Outlook.Connected {
		t.Fatalf("SetIntegration connect: %+v", got.Integrations)
	}
	if err := users.SetIntegration(ctx, id, "google", false); err != nil {
		t.Fatal(err)
	}
	if got, _ = users.ByID(ctx, id); got.Integrations.Google.Connected || got.Integrations.Google.At != nil {
		t.Fatalf("SetIntegration disconnect: %+v", got.Integrations)
	}
	if err := users.SetIntegration(ctx, id, "facebook", true); err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown provider must be refused, not written: %v", err)
	}
	if err := users.SetIntegration(ctx, bson.NewObjectID().Hex(), "outlook", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetIntegration unknown user: %v", err)
	}

	// SwapPhoto returns what it replaced; nil clears.
	first, second := "photo-1", "photo-2"
	if prev, err := users.SwapPhoto(ctx, id, &first); err != nil || prev != "" {
		t.Fatalf("first swap: %q %v", prev, err)
	}
	if prev, err := users.SwapPhoto(ctx, id, &second); err != nil || prev != first {
		t.Fatalf("second swap: %q %v", prev, err)
	}
	if got, _ = users.ByID(ctx, id); got.PhotoID == nil || *got.PhotoID != second {
		t.Fatalf("photo not swapped: %+v", got.PhotoID)
	}
	if prev, err := users.SwapPhoto(ctx, id, nil); err != nil || prev != second {
		t.Fatalf("clear: %q %v", prev, err)
	}
	if got, _ = users.ByID(ctx, id); got.PhotoID != nil {
		t.Fatal("photo not cleared")
	}
	if _, err := users.SwapPhoto(ctx, "nope", &first); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SwapPhoto bad id: %v", err)
	}

	// DeleteAvatarPhoto only ever removes the user's own avatar photos.
	photos := s.Photos()
	own := &models.Photo{OwnerID: id, Kind: models.PhotoKindAvatar, ContentType: "image/jpeg", Bytes: []byte("a")}
	group := &models.Photo{OwnerID: id, Kind: models.PhotoKindGroup, GroupID: "g1", ContentType: "image/jpeg", Bytes: []byte("g")}
	theirs := &models.Photo{OwnerID: "someone-else", Kind: models.PhotoKindAvatar, ContentType: "image/jpeg", Bytes: []byte("t")}
	for _, p := range []*models.Photo{own, group, theirs} {
		if err := photos.Put(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []*models.Photo{group, theirs, own, {ID: "missing"}} {
		if err := users.DeleteAvatarPhoto(ctx, id, p.ID); err != nil {
			t.Fatalf("DeleteAvatarPhoto %s: %v", p.ID, err)
		}
	}
	if err := users.DeleteAvatarPhoto(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := photos.Get(ctx, own.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("own avatar not deleted: %v", err)
	}
	for _, p := range []*models.Photo{group, theirs} {
		if _, err := photos.Get(ctx, p.ID); err != nil {
			t.Fatalf("%s must survive: %v", p.Kind, err)
		}
	}
}
