package store

import (
	"Backend/pkg/models"
	"context"
	"maps"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Users is the users collection. Ids are ObjectID hex strings; an unparsable
// id reads as ErrNotFound. Area agents add methods in their own files
// (users_prefs.go, users_taste.go, …).
type Users struct{ s *Store }

func (s *Store) Users() Users { return Users{s} }

func (u Users) coll() *mongo.Collection { return u.s.db.Collection(CollUsers) }

func objectID(id string) (bson.ObjectID, bool) {
	oid, err := bson.ObjectIDFromHex(id)
	return oid, err == nil
}

// Create inserts a user, assigning the id, the lowercase search fields and
// timestamps. Duplicates are ErrEmailTaken / ErrUsernameTaken.
func (u Users) Create(ctx context.Context, user *models.User) error {
	now := u.s.Now()
	if user.ID.IsZero() {
		user.ID = bson.NewObjectID()
	}
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.NameLower = strings.ToLower(user.Name)
	user.UsernameLower = strings.ToLower(user.Username)
	user.CreatedAt, user.UpdatedAt, user.LastActiveAt = now, now, now
	_, err := u.coll().InsertOne(ctx, user)
	return userWriteError(err)
}

func userWriteError(err error) error {
	switch {
	case err == nil:
		return nil
	case duplicateOn(err, "email_unique"):
		return ErrEmailTaken
	case duplicateOn(err, "usernameLower_unique"):
		return ErrUsernameTaken
	case IsDuplicate(err):
		return ErrConflict
	}
	return err
}

func (u Users) ByID(ctx context.Context, id string) (*models.User, error) {
	oid, ok := objectID(id)
	if !ok {
		return nil, ErrNotFound
	}
	var user models.User
	if err := decodeOne(u.coll().FindOne(ctx, bson.M{"_id": oid}), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// ByIDs loads many users keyed by hex id; unknown ids are simply absent.
func (u Users) ByIDs(ctx context.Context, ids []string) (map[string]*models.User, error) {
	out := make(map[string]*models.User, len(ids))
	oids := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if oid, ok := objectID(id); ok {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return out, nil
	}
	cursor, err := u.coll().Find(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return nil, err
	}
	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	for _, user := range users {
		out[user.ID.Hex()] = user
	}
	return out, nil
}

func (u Users) ByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := decodeOne(u.coll().FindOne(ctx, bson.M{"email": strings.ToLower(strings.TrimSpace(email))}), &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (u Users) ByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	handle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if err := decodeOne(u.coll().FindOne(ctx, bson.M{"usernameLower": handle}), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// Search finds people whose handle starts with q or whose name has a word
// starting with q ("@" and case ignored), excluding excludeID, sorted by name.
func (u Users) Search(ctx context.Context, q, excludeID string, limit int) ([]*models.User, error) {
	q = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "@"))
	if q == "" {
		return []*models.User{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	quoted := regexp.QuoteMeta(q)
	filter := bson.M{"$or": []bson.M{
		{"usernameLower": bson.M{"$regex": "^" + quoted}},
		{"nameLower": bson.M{"$regex": "(^|\\s)" + quoted}},
	}}
	if oid, ok := objectID(excludeID); ok {
		filter["_id"] = bson.M{"$ne": oid}
	}
	cursor, err := u.coll().Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "nameLower", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	users := []*models.User{}
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// Update applies a $set (plus updatedAt) and returns the new document. Keys
// are bson field names; setting name or username also refreshes the
// lowercase copies. Duplicates are ErrEmailTaken / ErrUsernameTaken.
func (u Users) Update(ctx context.Context, id string, set bson.M) (*models.User, error) {
	oid, ok := objectID(id)
	if !ok {
		return nil, ErrNotFound
	}
	fields := bson.M{"updatedAt": u.s.Now()}
	maps.Copy(fields, set)
	if name, ok := fields["name"].(string); ok {
		fields["nameLower"] = strings.ToLower(name)
	}
	if username, ok := fields["username"].(string); ok {
		fields["usernameLower"] = strings.ToLower(username)
	}
	var user models.User
	err := u.coll().FindOneAndUpdate(ctx, bson.M{"_id": oid}, bson.M{"$set": fields},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, userWriteError(err)
	}
	return &user, nil
}

// Unset removes fields (bson names) from a user.
func (u Users) Unset(ctx context.Context, id string, fields ...string) error {
	oid, ok := objectID(id)
	if !ok {
		return ErrNotFound
	}
	unset := bson.M{}
	for _, f := range fields {
		unset[f] = ""
	}
	res, err := u.coll().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$unset": unset, "$set": bson.M{"updatedAt": u.s.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword stores a new argon2id hash.
func (u Users) SetPassword(ctx context.Context, id, hash string) error {
	_, err := u.Update(ctx, id, bson.M{"passwordHash": hash})
	return err
}

// SetPhoto points the avatar at a photo id; nil clears it.
func (u Users) SetPhoto(ctx context.Context, id string, photoID *string) error {
	if photoID == nil {
		return u.Unset(ctx, id, "photoId")
	}
	_, err := u.Update(ctx, id, bson.M{"photoId": *photoID})
	return err
}

// Touch records activity (lastActiveAt) without bumping updatedAt.
func (u Users) Touch(ctx context.Context, id string) error {
	oid, ok := objectID(id)
	if !ok {
		return ErrNotFound
	}
	_, err := u.coll().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"lastActiveAt": u.s.Now()}})
	return err
}
