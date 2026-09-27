package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"Backend/pkg/util"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestNoDiscardedWrites enforces backend-contract §2.3: no store file may
// throw away the error of a write.
func TestNoDiscardedWrites(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	discard := regexp.MustCompile(`^\s*(_\s*,\s*)?_\s*=\s*.*\.(InsertOne|InsertMany|UpdateOne|UpdateMany|UpdateByID|ReplaceOne|DeleteOne|DeleteMany|FindOneAndUpdate|FindOneAndReplace|FindOneAndDelete|BulkWrite|Drop|CreateOne|CreateMany|DropOne)\(`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if discard.MatchString(line) {
				t.Errorf("%s:%d discards a write error: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

var testNow = time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return testutil.Store(t, func() time.Time { return testNow })
}

func TestEnsureIndexesIdempotentAndLeavesCatalogTTL(t *testing.T) {
	db := testutil.DB(t)
	ctx := context.Background()
	// A catalog collection as the seed leaves it: a 2dsphere index under a
	// foreign name and a TTL index that must survive.
	catalog := db.Collection(store.CollDemoActivities)
	_, err := catalog.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "location", Value: "2dsphere"}}, Options: options.Index().SetName("seed_geo")},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("seed_ttl").SetExpireAfterSeconds(0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(db, func() time.Time { return testNow })
	for i := range 2 {
		if err := s.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes run %d: %v", i+1, err)
		}
	}
	names := func(coll string) map[string]bool {
		cursor, err := db.Collection(coll).Indexes().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var docs []struct {
			Name string `bson:"name"`
		}
		if err := cursor.All(ctx, &docs); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, d := range docs {
			out[d.Name] = true
		}
		return out
	}
	users := names(store.CollUsers)
	for _, want := range []string{"email_unique", "usernameLower_unique", "nameLower", "roles"} {
		if !users[want] {
			t.Errorf("users index %s missing: %v", want, users)
		}
	}
	if !names(store.CollMessages)["threadId_senderId_clientId_unique"] || !names(store.CollPlanRuns)["expiresAt_ttl"] {
		t.Error("partial / TTL indexes missing")
	}
	got := names(store.CollDemoActivities)
	if !got["seed_geo"] || !got["seed_ttl"] || !got["name_1"] || !got["city_1_kind_1_start_1"] {
		t.Errorf("catalog indexes wrong (TTL must survive, missing ones created under default names): %v", got)
	}
	if got["location_2dsphere"] {
		t.Error("a second 2dsphere index was created although one existed under another name")
	}

}

func newUser(name, email, username string) *models.User {
	return &models.User{Name: name, Email: email, Username: username, PasswordHash: "x", AvatarColor: "ink", Status: "open", City: "atlanta"}
}

func TestUsers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	users := s.Users()

	jordan := newUser("Jordan Lee", "Jordan@GaTech.edu", "jordanlee")
	if err := users.Create(ctx, jordan); err != nil {
		t.Fatal(err)
	}
	if jordan.ID.IsZero() || jordan.Email != "jordan@gatech.edu" || jordan.NameLower != "jordan lee" || !jordan.CreatedAt.Equal(testNow) {
		t.Fatalf("Create did not normalize: %+v", jordan)
	}
	if err := users.Create(ctx, newUser("Other", "jordan@gatech.edu", "someoneelse")); !errors.Is(err, store.ErrEmailTaken) || !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	if err := users.Create(ctx, newUser("Other", "other@example.com", "JordanLee")); !errors.Is(err, store.ErrUsernameTaken) {
		t.Fatalf("duplicate username (case-insensitive): %v", err)
	}
	maya := newUser("Maya Ramirez", "maya@example.com", "mayar")
	if err := users.Create(ctx, maya); err != nil {
		t.Fatal(err)
	}

	id := jordan.ID.Hex()
	if got, err := users.ByID(ctx, id); err != nil || got.Email != "jordan@gatech.edu" {
		t.Fatalf("ByID: %v %+v", err, got)
	}
	if _, err := users.ByID(ctx, "not-an-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bad id must be ErrNotFound: %v", err)
	}
	if _, err := users.ByID(ctx, bson.NewObjectID().Hex()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id must be ErrNotFound: %v", err)
	}
	if got, err := users.ByEmail(ctx, " JORDAN@gatech.edu "); err != nil || got.ID != jordan.ID {
		t.Fatalf("ByEmail: %v", err)
	}
	if got, err := users.ByUsername(ctx, "@JordanLee"); err != nil || got.ID != jordan.ID {
		t.Fatalf("ByUsername: %v", err)
	}
	many, err := users.ByIDs(ctx, []string{id, maya.ID.Hex(), "garbage", bson.NewObjectID().Hex()})
	if err != nil || len(many) != 2 || many[id].Name != "Jordan Lee" {
		t.Fatalf("ByIDs: %v %v", err, many)
	}

	found, err := users.Search(ctx, "lee", maya.ID.Hex(), 20)
	if err != nil || len(found) != 1 || found[0].ID != jordan.ID {
		t.Fatalf("Search by name word: %v %v", err, found)
	}
	found, err = users.Search(ctx, "@may", "", 20)
	if err != nil || len(found) != 1 || found[0].ID != maya.ID {
		t.Fatalf("Search by handle: %v %v", err, found)
	}
	found, err = users.Search(ctx, "jordan", id, 20)
	if err != nil || len(found) != 0 {
		t.Fatalf("Search must exclude self: %v %v", err, found)
	}

	if _, err := users.Update(ctx, id, bson.M{"username": "MayaR"}); !errors.Is(err, store.ErrUsernameTaken) {
		t.Fatalf("Update duplicate username: %v", err)
	}
	updated, err := users.Update(ctx, id, bson.M{"name": "Jordan L.", "status": "busy"})
	if err != nil || updated.Name != "Jordan L." || updated.NameLower != "jordan l." || updated.Status != "busy" {
		t.Fatalf("Update: %v %+v", err, updated)
	}
	photo := "ph-1"
	if err := users.SetPhoto(ctx, id, &photo); err != nil {
		t.Fatal(err)
	}
	if got, _ := users.ByID(ctx, id); got.PhotoID == nil || *got.PhotoID != "ph-1" {
		t.Fatal("SetPhoto did not store")
	}
	if err := users.SetPhoto(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := users.ByID(ctx, id); got.PhotoID != nil {
		t.Fatal("SetPhoto(nil) did not clear")
	}
	if err := users.SetPassword(ctx, id, "newhash"); err != nil {
		t.Fatal(err)
	}
	if err := users.Touch(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, _ := users.ByID(ctx, id); got.PasswordHash != "newhash" || !got.LastActiveAt.Equal(testNow) {
		t.Fatalf("SetPassword/Touch: %+v", got)
	}
	if _, err := users.Update(ctx, bson.NewObjectID().Hex(), bson.M{"name": "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update unknown: %v", err)
	}
}

func TestTokensRotationAndReuse(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tokens := s.Tokens()
	ttl := 720 * time.Hour

	first, err := tokens.Issue(ctx, "user-1", ttl)
	if err != nil || first == "" {
		t.Fatal(err)
	}
	second, userID, err := tokens.Rotate(ctx, first, ttl)
	if err != nil || userID != "user-1" || second == first {
		t.Fatalf("Rotate: %v %s", err, userID)
	}
	// Replaying the first token revokes the family, including the live second one.
	if _, _, err := tokens.Rotate(ctx, first, ttl); !errors.Is(err, store.ErrTokenReused) || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reuse must be ErrTokenReused (an ErrNotFound): %v", err)
	}
	if _, _, err := tokens.Rotate(ctx, second, ttl); err == nil {
		t.Fatal("family not revoked after reuse")
	}
	if _, _, err := tokens.Rotate(ctx, "unknown", ttl); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}

	third, _ := tokens.Issue(ctx, "user-1", ttl)
	fourth, _ := tokens.Issue(ctx, "user-1", ttl)
	if err := tokens.Revoke(ctx, third); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Revoke(ctx, "unknown"); err != nil {
		t.Fatalf("Revoke unknown must not fail: %v", err)
	}
	if _, _, err := tokens.Rotate(ctx, third, ttl); err == nil {
		t.Fatal("revoked token rotated")
	}
	if err := tokens.RevokeAll(ctx, "user-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tokens.Rotate(ctx, fourth, ttl); err == nil {
		t.Fatal("RevokeAll left a live token")
	}

	expired, _ := tokens.Issue(ctx, "user-2", -time.Minute)
	if _, _, err := tokens.Rotate(ctx, expired, ttl); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestResets(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	resets := s.Resets()
	email := "jordan@gatech.edu"
	if err := resets.Create(ctx, email, util.SHA256Hex("123456"), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := resets.Create(ctx, email, util.SHA256Hex("654321"), 10*time.Minute); err != nil {
		t.Fatalf("Create must replace: %v", err)
	}
	code, err := resets.Get(ctx, email)
	if err != nil || code.CodeHash != util.SHA256Hex("654321") || code.Attempts != 0 || !code.ExpiresAt.Equal(testNow.Add(10*time.Minute)) {
		t.Fatalf("Get: %v %+v", err, code)
	}
	if err := resets.RecordAttempt(ctx, email); err != nil {
		t.Fatal(err)
	}
	if code, _ = resets.Get(ctx, email); code.Attempts != 1 {
		t.Fatal("attempt not counted")
	}
	if err := resets.Consume(ctx, email, "jti-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Consume before verify: %v", err)
	}
	if err := resets.MarkVerified(ctx, email, "jti-1"); err != nil {
		t.Fatal(err)
	}
	if err := resets.Consume(ctx, email, "jti-other"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Consume with another jti: %v", err)
	}
	if err := resets.Consume(ctx, email, "jti-1"); err != nil {
		t.Fatal(err)
	}
	if err := resets.Consume(ctx, email, "jti-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Consume twice: %v", err)
	}
	if _, err := resets.Get(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get unknown: %v", err)
	}
	if err := resets.MarkVerified(ctx, "nobody@example.com", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MarkVerified unknown: %v", err)
	}
}

func TestWebSessions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ws := s.WebSessions()
	token, err := ws.Create(ctx, "user-1", store.PurposePaymentSetup, "", 10*time.Minute)
	if err != nil || token == "" {
		t.Fatal(err)
	}
	if _, err := ws.Peek(ctx, token, store.PurposeCalendarConnect); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong purpose: %v", err)
	}
	if sess, err := ws.Peek(ctx, token, store.PurposePaymentSetup); err != nil || sess.UserID != "user-1" {
		t.Fatalf("Peek: %v", err)
	}
	sess, err := ws.Consume(ctx, token, store.PurposePaymentSetup)
	if err != nil || sess.UserID != "user-1" || sess.UsedAt == nil {
		t.Fatalf("Consume: %v %+v", err, sess)
	}
	if _, err := ws.Consume(ctx, token, store.PurposePaymentSetup); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Consume twice: %v", err)
	}
	if _, err := ws.Peek(ctx, token, store.PurposePaymentSetup); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Peek after use: %v", err)
	}
	fb, err := ws.Insert(ctx, models.WebSession{UserID: "user-2", Purpose: store.PurposeFacebookState, Rerequest: true}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := ws.Consume(ctx, fb, store.PurposeFacebookState); err != nil || !sess.Rerequest {
		t.Fatalf("Insert kept extra fields: %v %+v", err, sess)
	}
	expired, _ := ws.Create(ctx, "user-3", store.PurposePaymentSetup, "", -time.Second)
	if _, err := ws.Consume(ctx, expired, store.PurposePaymentSetup); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
}

func TestPhotos(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	photos := s.Photos()
	p := &models.Photo{OwnerID: "user-1", Kind: models.PhotoKindGroup, GroupID: "g1", ContentType: "image/jpeg", Bytes: []byte("jpegbytes")}
	if err := photos.Put(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Size != 9 || !p.CreatedAt.Equal(testNow) {
		t.Fatalf("Put did not fill fields: %+v", p)
	}
	got, err := photos.Get(ctx, p.ID)
	if err != nil || string(got.Bytes) != "jpegbytes" || got.ContentType != "image/jpeg" {
		t.Fatalf("Get: %v %+v", err, got)
	}
	avatar := &models.Photo{OwnerID: "user-1", Kind: models.PhotoKindAvatar, ContentType: "image/png", Bytes: []byte("png")}
	if err := photos.Put(ctx, avatar); err != nil {
		t.Fatal(err)
	}
	list, err := photos.ListForGroup(ctx, "g1", 10)
	if err != nil || len(list) != 1 || list[0].ID != p.ID || list[0].Bytes != nil || list[0].Size != 9 {
		t.Fatalf("ListForGroup: %v %+v", err, list)
	}
	if n, err := photos.CountForGroup(ctx, "g1"); err != nil || n != 1 {
		t.Fatalf("CountForGroup: %v %d", err, n)
	}
	if err := photos.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := photos.Delete(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete twice: %v", err)
	}
	if _, err := photos.Get(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}
