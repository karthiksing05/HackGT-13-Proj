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

// ErrFacebookTaken means the Facebook user is already linked to another
// SideQuests account (facebook_accounts.fbUserId is unique).
var ErrFacebookTaken = fmt.Errorf("facebook account linked to another user: %w", ErrConflict)

// Facebook is facebook_accounts (the connection) and facebook_imports (the
// latest import), both keyed by the SideQuests user id. A connection with an
// empty accessTokenEnc is disconnected: Facebook's deauthorize callback
// removed the token, while the link (fbUserId) and the import stay until the
// person disconnects here or asks Facebook for their data to be deleted.
type Facebook struct{ s *Store }

func (s *Store) Facebook() Facebook { return Facebook{s} }

func (f Facebook) accounts() *mongo.Collection { return f.s.db.Collection(CollFacebookAccounts) }
func (f Facebook) imports() *mongo.Collection  { return f.s.db.Collection(CollFacebookImports) }

// Account is the user's connection; ErrNotFound when there is none.
func (f Facebook) Account(ctx context.Context, userID string) (*models.FacebookAccount, error) {
	var acct models.FacebookAccount
	if err := decodeOne(f.accounts().FindOne(ctx, bson.M{"_id": userID}), &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// AccountByFBUserID is the connection of a Facebook user id; ErrNotFound
// when nobody linked it.
func (f Facebook) AccountByFBUserID(ctx context.Context, fbUserID string) (*models.FacebookAccount, error) {
	if fbUserID == "" {
		return nil, ErrNotFound
	}
	var acct models.FacebookAccount
	if err := decodeOne(f.accounts().FindOne(ctx, bson.M{"fbUserId": fbUserID}), &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

// SaveAccount inserts or replaces the user's connection, stamping updatedAt
// (and connectedAt when unset). A Facebook user linked to another account is
// ErrFacebookTaken.
func (f Facebook) SaveAccount(ctx context.Context, acct *models.FacebookAccount) error {
	acct.UpdatedAt = f.s.Now()
	if acct.ConnectedAt.IsZero() {
		acct.ConnectedAt = acct.UpdatedAt
	}
	if acct.GrantedScopes == nil {
		acct.GrantedScopes = []string{}
	}
	if acct.DeclinedScopes == nil {
		acct.DeclinedScopes = []string{}
	}
	_, err := f.accounts().ReplaceOne(ctx, bson.M{"_id": acct.UserID}, acct, options.Replace().SetUpsert(true))
	switch {
	case err == nil:
		return nil
	case duplicateOn(err, "fbUserId_unique"):
		return ErrFacebookTaken
	case IsDuplicate(err):
		return fmt.Errorf("save facebook account: %w", ErrConflict)
	}
	return err
}

// MarkNeedsReconnect records that Facebook no longer accepts the stored
// token; the next import asks the person to sign in again.
func (f Facebook) MarkNeedsReconnect(ctx context.Context, userID string) error {
	res, err := f.accounts().UpdateOne(ctx, bson.M{"_id": userID},
		bson.M{"$set": bson.M{"needsReconnect": true, "updatedAt": f.s.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateProfile keeps the name and the granted / declined scopes an import
// read from Facebook. It matches only a connection that still holds a token:
// ErrNotFound means it was disconnected or deleted meanwhile.
func (f Facebook) UpdateProfile(ctx context.Context, userID, name string, granted, declined []string) error {
	if granted == nil {
		granted = []string{}
	}
	if declined == nil {
		declined = []string{}
	}
	set := bson.M{"grantedScopes": granted, "declinedScopes": declined, "updatedAt": f.s.Now()}
	if name != "" {
		set["name"] = name
	}
	res, err := f.accounts().UpdateOne(ctx, bson.M{"_id": userID, "accessTokenEnc": bson.M{"$ne": ""}}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Deauthorize forgets the token of a Facebook user who removed the app on
// Facebook and returns their SideQuests user id; ErrNotFound when nobody
// linked that Facebook user.
func (f Facebook) Deauthorize(ctx context.Context, fbUserID string) (string, error) {
	if fbUserID == "" {
		return "", ErrNotFound
	}
	var acct models.FacebookAccount
	err := f.accounts().FindOneAndUpdate(ctx, bson.M{"fbUserId": fbUserID},
		bson.M{"$set": bson.M{"accessTokenEnc": "", "needsReconnect": false, "updatedAt": f.s.Now()}}).Decode(&acct)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", ErrNotFound
		}
		return "", err
	}
	return acct.UserID, nil
}

// Forget deletes everything imported from Facebook for the user: the
// connection, the import and users.facebookInterests. Saved preferences stay.
func (f Facebook) Forget(ctx context.Context, userID string) error {
	if _, err := f.imports().DeleteOne(ctx, bson.M{"_id": userID}); err != nil {
		return fmt.Errorf("delete facebook import: %w", err)
	}
	if _, err := f.accounts().DeleteOne(ctx, bson.M{"_id": userID}); err != nil {
		return fmt.Errorf("delete facebook account: %w", err)
	}
	if err := f.s.Users().SetFacebookInterests(ctx, userID, nil); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// DeleteImport removes the user's import only (the account changed hands).
func (f Facebook) DeleteImport(ctx context.Context, userID string) error {
	_, err := f.imports().DeleteOne(ctx, bson.M{"_id": userID})
	return err
}

// SaveImport replaces the user's import.
func (f Facebook) SaveImport(ctx context.Context, imp *models.FacebookImport) error {
	if imp.Pages == nil {
		imp.Pages = []models.FacebookPage{}
	}
	if imp.FriendFBIDs == nil {
		imp.FriendFBIDs = []string{}
	}
	if imp.SuggestedRatings == nil {
		imp.SuggestedRatings = map[string]int{}
	}
	if imp.Interests == nil {
		imp.Interests = []string{}
	}
	_, err := f.imports().ReplaceOne(ctx, bson.M{"_id": imp.UserID}, imp, options.Replace().SetUpsert(true))
	return err
}

// Import is the user's latest import; ErrNotFound before the first one.
func (f Facebook) Import(ctx context.Context, userID string) (*models.FacebookImport, error) {
	var imp models.FacebookImport
	if err := decodeOne(f.imports().FindOne(ctx, bson.M{"_id": userID}), &imp); err != nil {
		return nil, err
	}
	return &imp, nil
}

// UserIDsByFBUserID maps linked Facebook user ids to SideQuests user ids;
// ids nobody linked are absent.
func (f Facebook) UserIDsByFBUserID(ctx context.Context, fbUserIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(fbUserIDs))
	if len(fbUserIDs) == 0 {
		return out, nil
	}
	cursor, err := f.accounts().Find(ctx, bson.M{"fbUserId": bson.M{"$in": fbUserIDs}},
		options.Find().SetProjection(bson.M{"fbUserId": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UserID   string `bson:"_id"`
		FBUserID string `bson:"fbUserId"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.FBUserID] = row.UserID
	}
	return out, nil
}

// FriendLinks reads what the viewer has with each of others, as
// pkg/models/social.go stores it: friendships (friends[id]) and pending
// requests in either direction (pending[id]; an incoming one wins over an
// outgoing one). It is read-only and feeds friends_on_app.
func (f Facebook) FriendLinks(ctx context.Context, viewerID string, others []string) (friends map[string]bool, pending map[string]models.FriendRequest, err error) {
	friends = map[string]bool{}
	pending = map[string]models.FriendRequest{}
	if viewerID == "" || len(others) == 0 {
		return friends, pending, nil
	}
	pairIDs := make([]string, 0, len(others))
	for _, id := range others {
		pairIDs = append(pairIDs, models.FriendshipID(viewerID, id))
	}
	cursor, err := f.s.db.Collection(CollFriendships).Find(ctx, bson.M{"_id": bson.M{"$in": pairIDs}})
	if err != nil {
		return nil, nil, err
	}
	var links []models.Friendship
	if err := cursor.All(ctx, &links); err != nil {
		return nil, nil, err
	}
	for _, link := range links {
		for _, id := range link.UserIDs {
			if id != viewerID {
				friends[id] = true
			}
		}
	}

	cursor, err = f.s.db.Collection(CollFriendRequests).Find(ctx, bson.M{
		"status": models.RequestPending,
		"$or": []bson.M{
			{"fromId": viewerID, "toId": bson.M{"$in": others}},
			{"toId": viewerID, "fromId": bson.M{"$in": others}},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	var requests []models.FriendRequest
	if err := cursor.All(ctx, &requests); err != nil {
		return nil, nil, err
	}
	for _, req := range requests {
		other := req.ToID
		if req.ToID == viewerID {
			other = req.FromID
		}
		if existing, ok := pending[other]; ok && existing.ToID == viewerID {
			continue // an incoming request wins over an outgoing one
		}
		pending[other] = req
	}
	return friends, pending, nil
}
