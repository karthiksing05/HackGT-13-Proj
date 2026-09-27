package main

import (
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/profiles"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// writeOrder is the order documents are written in: people before what
// links them, plans before their chats.
var writeOrder = []string{store.CollUsers, store.CollFriendships, store.CollFriendRequests,
	store.CollItineraries, store.CollThreads, store.CollMessages, store.CollForumPosts}

// userKept are the fields of a seeded account a rerun leaves as they are:
// its password hash (random; nobody signs in as a showcase person), when it
// was made, its learned taste tags and calendar connections. Its vectors are
// written separately.
var userKept = map[string]bool{"_id": true, "passwordHash": true, "createdAt": true, "taste": true, "integrations": true}

// taggedDoc is doc as the API stores it, plus the seed's tag and world.
func taggedDoc(doc any, world string) (bson.D, error) {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := bson.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return append(d, bson.E{Key: fieldSeed, Value: seedTag}, bson.E{Key: fieldWorld, Value: world}), nil
}

// tally counts written documents per collection.
type tally struct {
	created, updated map[string]int
	tasteML          int
	tasteBlend       int
	tasteKept        int
	tasteNone        int
}

// write stores every create and update of the world, then the taste
// vectors of the people it wrote and each chat's last message.
func (wp *worldPlan) write(ctx context.Context, st *store.Store, taste tasteSource) (*tally, error) {
	t := &tally{created: map[string]int{}, updated: map[string]int{}}
	for _, coll := range writeOrder {
		for _, s := range wp.sections {
			for _, c := range s.changes {
				if c.coll != coll || (c.act != actCreate && c.act != actUpdate) {
					continue
				}
				var err error
				if coll == store.CollUsers {
					err = writeUser(ctx, st, c, wp.spec.name)
				} else {
					err = writeDoc(ctx, st, c, wp.spec.name)
				}
				if err != nil {
					return nil, err
				}
				if c.act == actCreate {
					t.created[coll]++
				} else {
					t.updated[coll]++
				}
			}
		}
	}
	if err := wp.writeTaste(ctx, st, taste, t); err != nil {
		return nil, err
	}
	return t, wp.touchChats(ctx, st)
}

func writeUser(ctx context.Context, st *store.Store, c change, world string) error {
	u := c.doc.(*models.User)
	if c.act == actCreate {
		hash, err := util.HashPassword(util.RandomToken(24))
		if err != nil {
			return err
		}
		u.PasswordHash = hash
		d, err := taggedDoc(u, world)
		if err != nil {
			return err
		}
		if _, err := st.Collection(store.CollUsers).InsertOne(ctx, d); err != nil {
			return fmt.Errorf("create %s: %w", u.Name, err)
		}
		return nil
	}
	d, err := taggedDoc(u, world)
	if err != nil {
		return err
	}
	set := bson.D{}
	for _, e := range d {
		if !userKept[e.Key] {
			set = append(set, e)
		}
	}
	res, err := st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": u.ID, fieldSeed: seedTag}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("update %s: %w", u.Name, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("update %s: the account changed while the seed ran", u.Name)
	}
	return nil
}

// writeDoc replaces the seed's own document under the id, or inserts it;
// the filter on the tag means it can never overwrite anyone else's.
func writeDoc(ctx context.Context, st *store.Store, c change, world string) error {
	d, err := taggedDoc(c.doc, world)
	if err != nil {
		return err
	}
	_, err = st.Collection(c.coll).ReplaceOne(ctx, bson.M{"_id": c.id, fieldSeed: seedTag}, d, options.Replace().SetUpsert(true))
	if store.IsDuplicate(err) {
		return fmt.Errorf("write %s %v: a document that is not this seed's appeared meanwhile", c.coll, c.id)
	}
	if err != nil {
		return fmt.Errorf("write %s %v: %w", c.coll, c.id, err)
	}
	return nil
}

// writeTaste gives each person the seed wrote their taste vectors: rebuilt
// through the ML service like any account's (pkg/profiles), or, without
// it, the blend of their liked categories' catalog vectors, unless the ML
// service built them on an earlier run.
func (wp *worldPlan) writeTaste(ctx context.Context, st *store.Store, taste tasteSource, t *tally) error {
	var refresher *profiles.Service
	if taste.usable {
		refresher = profiles.New(st, taste.client, time.Now)
	}
	for _, m := range wp.members {
		if m.act != actCreate && m.act != actUpdate {
			continue
		}
		if refresher != nil {
			rctx, cancel := context.WithTimeout(ctx, profileTimeout)
			err := refresher.Refresh(rctx, m.uid.Hex())
			cancel()
			if err == nil {
				t.tasteML++
				continue
			}
			m.taste = fmt.Sprintf("the ML service failed (%v)", err)
			if m.current != nil && m.current.ProfileTextHash != "" {
				t.tasteKept++
				continue
			}
			if wp.vectors == nil {
				vectors, err := loadVectors(ctx, st, wp.spec.catalog)
				if err != nil {
					return err
				}
				wp.vectors = vectors
			}
			m.pos, _ = blend(m.prefs.Ratings, wp.vectors, true)
			m.neg, _ = blend(m.prefs.Ratings, wp.vectors, false)
		} else if m.current != nil && m.current.ProfileTextHash != "" {
			t.tasteKept++
			continue
		}
		if m.pos == nil {
			t.tasteNone++
			continue
		}
		set := bson.M{"positiveEmbedding": m.pos, "embeddingModel": ml.Model}
		update := bson.M{"$set": set}
		if m.neg != nil {
			set["negativeEmbedding"] = m.neg
		} else {
			update["$unset"] = bson.M{"negativeEmbedding": ""}
		}
		if _, err := st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": m.uid, fieldSeed: seedTag}, update); err != nil {
			return fmt.Errorf("taste vectors of %s: %w", m.name, err)
		}
		t.tasteBlend++
	}
	return nil
}

// touchChats points each seeded chat's last message at its newest one,
// which is a real person's when someone wrote in it after an earlier run.
func (wp *worldPlan) touchChats(ctx context.Context, st *store.Store) error {
	for _, id := range wp.threads {
		var last models.Message
		err := st.Collection(store.CollMessages).FindOne(ctx, bson.M{"threadId": id},
			options.FindOne().SetSort(bson.D{{Key: "sentAt", Value: -1}, {Key: "_id", Value: -1}})).Decode(&last)
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read the last message of %s: %w", id, err)
		}
		if _, err := st.Collection(store.CollThreads).UpdateOne(ctx, bson.M{"_id": id, fieldSeed: seedTag}, bson.M{"$set": bson.M{
			"lastMessageText": last.Text, "lastSenderId": last.SenderID, "lastMessageAt": last.SentAt,
		}}); err != nil {
			return fmt.Errorf("update %s: %w", id, err)
		}
	}
	return nil
}

// ---- remove ----------------------------------------------------------------------

// removeOrder deletes what hangs off the seeded plans and chats first.
var removeOrder = []string{store.CollMessages, store.CollForumPosts, store.CollThreads, store.CollItineraries,
	store.CollFriendRequests, store.CollFriendships, store.CollUsers}

// count is how many documents of a collection a removal takes.
type count struct {
	coll string
	n    int64
	what string
}

// removal is what --remove takes away in one world: every document tagged
// with the seed and the world, plus what only exists inside the seeded
// plans and chats, as when a host deletes a plan in the app (other people's
// messages in the chats, expenses and group photos, join records and "Plan
// together" marks). Anything else stays, including real people's friend
// requests to or DMs with showcase people (the app skips deleted accounts).
type removal struct {
	world   string
	tagged  []count
	inside  []count
	filters map[string][]bson.M
	people  []string
	plans   []string
}

func (r *removal) total() int64 {
	var n int64
	for _, c := range append(slices.Clone(r.tagged), r.inside...) {
		n += c.n
	}
	return n
}

func planRemoval(ctx context.Context, st *store.Store, world string) (*removal, error) {
	r := &removal{world: world, filters: map[string][]bson.M{}}
	tagged := bson.M{fieldSeed: seedTag, fieldWorld: world}
	distinctIDs := func(coll string) ([]any, error) {
		var ids []any
		if err := st.Collection(coll).Distinct(ctx, "_id", tagged).Decode(&ids); err != nil {
			return nil, fmt.Errorf("read %s: %w", coll, err)
		}
		if ids == nil {
			ids = []any{} // $in needs an array, never null
		}
		return ids, nil
	}
	threads, err := distinctIDs(store.CollThreads)
	if err != nil {
		return nil, err
	}
	plans, err := distinctIDs(store.CollItineraries)
	if err != nil {
		return nil, err
	}
	posts, err := distinctIDs(store.CollForumPosts)
	if err != nil {
		return nil, err
	}
	postIDs := make([]any, 0, len(plans)+len(posts)) // a plan post's id is its itinerary's
	postIDs = append(append(postIDs, plans...), posts...)
	inside := []struct {
		coll, what string
		filter     bson.M
	}{
		{store.CollMessages, "written by others in the seeded chats", bson.M{"threadId": bson.M{"$in": threads}, fieldSeed: bson.M{"$ne": seedTag}}},
		{store.CollExpenses, "shared costs in the seeded chats", bson.M{"groupId": bson.M{"$in": threads}}},
		{store.CollPhotos, "photos in the seeded chats", bson.M{"groupId": bson.M{"$in": threads}, "kind": models.PhotoKindGroup}},
		{store.CollJoinRequests, "records of people joining the seeded plans", bson.M{"itineraryId": bson.M{"$in": plans}}},
		{store.CollPlanTogether, "“Plan together” marks on the seeded posts", bson.M{"postId": bson.M{"$in": postIDs}}},
	}
	for _, in := range inside {
		n, err := st.Collection(in.coll).CountDocuments(ctx, in.filter)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", in.coll, err)
		}
		if n > 0 {
			r.inside = append(r.inside, count{coll: in.coll, n: n, what: in.what})
			r.filters[in.coll] = append(r.filters[in.coll], in.filter)
		}
	}
	for _, coll := range removeOrder {
		n, err := st.Collection(coll).CountDocuments(ctx, tagged)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", coll, err)
		}
		if n > 0 {
			r.tagged = append(r.tagged, count{coll: coll, n: n})
			r.filters[coll] = append(r.filters[coll], tagged)
		}
	}
	var users []models.User
	if err := findAll(ctx, st, store.CollUsers, tagged, bson.M{"name": 1}, &users); err != nil {
		return nil, err
	}
	for _, u := range users {
		r.people = append(r.people, u.Name)
	}
	var its []models.Itinerary
	if err := findAll(ctx, st, store.CollItineraries, tagged, bson.M{"title": 1}, &its); err != nil {
		return nil, err
	}
	for _, it := range its {
		r.plans = append(r.plans, it.Title)
	}
	return r, nil
}

func findAll(ctx context.Context, st *store.Store, coll string, filter, projection bson.M, out any) error {
	cursor, err := st.Collection(coll).Find(ctx, filter, options.Find().SetProjection(projection).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return fmt.Errorf("read %s: %w", coll, err)
	}
	if err := cursor.All(ctx, out); err != nil {
		return fmt.Errorf("read %s: %w", coll, err)
	}
	return nil
}

// apply deletes what the removal counted: what lives inside the seeded
// plans and chats first, then the seeded documents.
func (r *removal) apply(ctx context.Context, st *store.Store) error {
	colls := append([]string{store.CollMessages, store.CollExpenses, store.CollPhotos, store.CollJoinRequests, store.CollPlanTogether}, removeOrder...)
	done := map[string]bool{}
	for _, coll := range colls {
		if done[coll] {
			continue
		}
		done[coll] = true
		for _, filter := range r.filters[coll] {
			if _, err := st.Collection(coll).DeleteMany(ctx, filter); err != nil {
				return fmt.Errorf("delete from %s: %w", coll, err)
			}
		}
	}
	return nil
}
