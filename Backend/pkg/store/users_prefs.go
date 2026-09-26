package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Users methods for the signed-in person's own settings (backend-A):
// preferences, calendar connections and the avatar photo.

// SavePrefs stores the preferences and marks setup complete (the first save
// finishes Profile setup). It returns the updated user.
func (u Users) SavePrefs(ctx context.Context, id string, prefs models.UserPrefs) (*models.User, error) {
	if prefs.Ratings == nil {
		prefs.Ratings = map[string]int{}
	}
	if prefs.Answers == nil {
		prefs.Answers = map[string]string{}
	}
	return u.Update(ctx, id, bson.M{"prefs": prefs, "setupComplete": true})
}

// calendarProviders are the keys under users.integrations.
var calendarProviders = map[string]bool{"google": true, "outlook": true}

// SetIntegration records a calendar connection (with the time it was made)
// or its removal.
func (u Users) SetIntegration(ctx context.Context, id, provider string, connected bool) error {
	if !calendarProviders[provider] {
		return fmt.Errorf("unknown calendar provider %q", provider)
	}
	oid, ok := objectID(id)
	if !ok {
		return ErrNotFound
	}
	now := u.s.Now()
	state := models.IntegrationState{Connected: connected}
	if connected {
		state.At = &now
	}
	res, err := u.coll().UpdateOne(ctx, bson.M{"_id": oid},
		bson.M{"$set": bson.M{"integrations." + provider: state, "updatedAt": now}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SwapPhoto points the avatar at photoID (nil clears it) in one step and
// returns the photo id it replaced ("" when there was none), so concurrent
// uploads never lose track of a photo.
func (u Users) SwapPhoto(ctx context.Context, id string, photoID *string) (string, error) {
	oid, ok := objectID(id)
	if !ok {
		return "", ErrNotFound
	}
	set := bson.M{"updatedAt": u.s.Now()}
	update := bson.M{"$set": set}
	if photoID != nil {
		set["photoId"] = *photoID
	} else {
		update["$unset"] = bson.M{"photoId": ""}
	}
	var before struct {
		PhotoID string `bson:"photoId"`
	}
	err := u.coll().FindOneAndUpdate(ctx, bson.M{"_id": oid}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.Before).SetProjection(bson.M{"photoId": 1})).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return before.PhotoID, nil
}

// DeleteAvatarPhoto removes one of the user's own avatar photos. A photo that
// is already gone, a group photo or someone else's photo is left alone.
func (u Users) DeleteAvatarPhoto(ctx context.Context, userID, photoID string) error {
	if photoID == "" {
		return nil
	}
	_, err := u.s.db.Collection(CollPhotos).DeleteOne(ctx,
		bson.M{"_id": photoID, "ownerId": userID, "kind": models.PhotoKindAvatar})
	return err
}
