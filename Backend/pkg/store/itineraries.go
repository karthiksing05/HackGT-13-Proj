package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Itineraries is the itineraries collection (backend-B). Reads that serve a
// viewer require membership (memberIds) and skip deleted plans; host-only
// writes filter on hostId. Forum plan posts are derived from these
// documents by pkg/api/social (visibility != just_me), so nothing here
// writes forum_posts. Leave and delete also update the plan's group thread
// and its content in the shared social collections.
type Itineraries struct{ s *Store }

func (s *Store) Itineraries() Itineraries { return Itineraries{s} }

func (it Itineraries) coll() *mongo.Collection { return it.s.db.Collection(CollItineraries) }

// live matches itineraries that are not deleted, plus extra conditions.
func (Itineraries) live(extra bson.M) bson.M {
	filter := bson.M{"status": bson.M{"$ne": models.ItineraryDeleted}}
	for k, v := range extra {
		filter[k] = v
	}
	return filter
}

func (it Itineraries) findOne(ctx context.Context, filter bson.M) (*models.Itinerary, error) {
	var doc models.Itinerary
	if err := decodeOne(it.coll().FindOne(ctx, filter), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (it Itineraries) findMany(ctx context.Context, filter bson.M, opts ...options.Lister[options.FindOptions]) ([]*models.Itinerary, error) {
	cursor, err := it.coll().Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	docs := []*models.Itinerary{}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// Insert stores a new itinerary, assigning its id (when empty), status
// active, the host as the first member and the timestamps.
func (it Itineraries) Insert(ctx context.Context, doc *models.Itinerary) error {
	now := it.s.Now()
	if doc.ID == "" {
		doc.ID = NewID()
	}
	if doc.Status == "" {
		doc.Status = models.ItineraryActive
	}
	if len(doc.MemberIDs) == 0 {
		doc.MemberIDs = []string{doc.HostID}
	}
	if doc.Items == nil {
		doc.Items = []models.ItineraryItem{}
	}
	doc.CreatedAt, doc.UpdatedAt = now, now
	_, err := it.coll().InsertOne(ctx, doc)
	return err
}

// Get loads a live itinerary without a membership check (forum, joins).
func (it Itineraries) Get(ctx context.Context, id string) (*models.Itinerary, error) {
	return it.findOne(ctx, it.live(bson.M{"_id": id}))
}

// ForMember loads a live itinerary userID belongs to; anything else
// (unknown, deleted, someone else's) is ErrNotFound.
func (it Itineraries) ForMember(ctx context.Context, id, userID string) (*models.Itinerary, error) {
	return it.findOne(ctx, it.live(bson.M{"_id": id, "memberIds": userID}))
}

// ByIDs loads live itineraries keyed by id; unknown ids are simply absent.
func (it Itineraries) ByIDs(ctx context.Context, ids []string) (map[string]*models.Itinerary, error) {
	out := make(map[string]*models.Itinerary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	docs, err := it.findMany(ctx, it.live(bson.M{"_id": bson.M{"$in": ids}}))
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		out[doc.ID] = doc
	}
	return out, nil
}

// flipPast marks the member's active itineraries whose back-by time is
// before now as past. Lists call it first, so "active" never includes a
// plan that is over.
func (it Itineraries) flipPast(ctx context.Context, userID string, now time.Time) error {
	_, err := it.coll().UpdateMany(ctx,
		bson.M{"memberIds": userID, "status": models.ItineraryActive, "backBy": bson.M{"$lt": now}},
		bson.M{"$set": bson.M{"status": models.ItineraryPast, "updatedAt": it.s.Now()}})
	return err
}

// ListActive is the member's active itineraries, soonest first (at most
// limit), after lazily flipping the ones that are over to past.
func (it Itineraries) ListActive(ctx context.Context, userID string, now time.Time, limit int) ([]*models.Itinerary, error) {
	if err := it.flipPast(ctx, userID, now); err != nil {
		return nil, err
	}
	return it.findMany(ctx, bson.M{"memberIds": userID, "status": models.ItineraryActive},
		options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit)))
}

// ListPast is the member's past itineraries, most recent first.
func (it Itineraries) ListPast(ctx context.Context, userID string, now time.Time, limit int) ([]*models.Itinerary, error) {
	if err := it.flipPast(ctx, userID, now); err != nil {
		return nil, err
	}
	return it.findMany(ctx, bson.M{"memberIds": userID, "status": models.ItineraryPast},
		options.Find().SetSort(bson.D{{Key: "start", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(int64(limit)))
}

// SearchActive is the member's active itineraries whose title, or the
// title of a stop (never a transit leg), contains q, ignoring case.
func (it Itineraries) SearchActive(ctx context.Context, userID, q string, now time.Time, limit int) ([]*models.Itinerary, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*models.Itinerary{}, nil
	}
	if err := it.flipPast(ctx, userID, now); err != nil {
		return nil, err
	}
	rx := bson.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
	return it.findMany(ctx, bson.M{
		"memberIds": userID,
		"status":    models.ItineraryActive,
		"$or": []bson.M{
			{"title": rx},
			{"items": bson.M{"$elemMatch": bson.M{"kind": bson.M{"$ne": models.ItemTransit}, "title": rx}}},
		},
	}, options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit)))
}

// FindItem is the live itinerary of userID that holds itemID, with the
// item's index in it; ErrNotFound when the viewer has no such item.
func (it Itineraries) FindItem(ctx context.Context, itemID, userID string) (*models.Itinerary, int, error) {
	doc, err := it.findOne(ctx, it.live(bson.M{"items.id": itemID, "memberIds": userID}))
	if err != nil {
		return nil, -1, err
	}
	for i := range doc.Items {
		if doc.Items[i].ID == itemID {
			return doc, i, nil
		}
	}
	return nil, -1, ErrNotFound
}

// WithItemsBetween is the member's live itineraries with at least one item
// starting in [from, to) (calendar days).
func (it Itineraries) WithItemsBetween(ctx context.Context, userID string, from, to time.Time) ([]*models.Itinerary, error) {
	return it.findMany(ctx, it.live(bson.M{
		"memberIds": userID,
		"items":     bson.M{"$elemMatch": bson.M{"start": bson.M{"$gte": from, "$lt": to}}},
	}), options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(500))
}

// PastStop is one stop of a member's itinerary that has ended.
type PastStop struct {
	ItineraryID string               `bson:"itineraryId"`
	Members     int                  `bson:"members"`
	TZ          string               `bson:"tz"`
	Item        models.ItineraryItem `bson:"item"`
}

// PastStopCursor resumes PastStops after the stop that started at Start
// with id ItemID.
type PastStopCursor struct {
	Start  time.Time
	ItemID string
}

// PastStopsQuery selects a page of a member's ended stops.
type PastStopsQuery struct {
	UserID  string
	Now     time.Time
	Exclude []string        // item ids to leave out (rated ones for ?unrated=true)
	After   *PastStopCursor // nil = from the most recent
	Limit   int
}

// PastStops is the member's stops that ended before q.Now across their
// live itineraries, most recent start first (ties by item id, descending).
func (it Itineraries) PastStops(ctx context.Context, q PastStopsQuery) ([]PastStop, error) {
	stop := bson.M{"items.kind": models.ItemStop, "items.end": bson.M{"$lt": q.Now}}
	if len(q.Exclude) > 0 {
		stop["items.id"] = bson.M{"$nin": q.Exclude}
	}
	if q.After != nil {
		stop["$or"] = []bson.M{
			{"items.start": bson.M{"$lt": q.After.Start}},
			{"items.start": q.After.Start, "items.id": bson.M{"$lt": q.After.ItemID}},
		}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: it.live(bson.M{
			"memberIds": q.UserID,
			"items":     bson.M{"$elemMatch": bson.M{"kind": models.ItemStop, "end": bson.M{"$lt": q.Now}}},
		})}},
		{{Key: "$unwind", Value: "$items"}},
		{{Key: "$match", Value: stop}},
		{{Key: "$sort", Value: bson.D{{Key: "items.start", Value: -1}, {Key: "items.id", Value: -1}}}},
		{{Key: "$limit", Value: q.Limit}},
		{{Key: "$project", Value: bson.M{
			"_id": 0, "itineraryId": "$_id", "members": bson.M{"$size": "$memberIds"}, "tz": 1, "item": "$items",
		}}},
	}
	cursor, err := it.coll().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	rows := []PastStop{}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// UpdateHost applies set (bson field names, plus updatedAt) to a live
// itinerary hostID hosts and returns the new document; ErrNotFound when it
// is gone or not theirs.
func (it Itineraries) UpdateHost(ctx context.Context, id, hostID string, set bson.M) (*models.Itinerary, error) {
	fields := bson.M{"updatedAt": it.s.Now()}
	for k, v := range set {
		fields[k] = v
	}
	return it.findOneAndUpdate(ctx, it.live(bson.M{"_id": id, "hostId": hostID}), bson.M{"$set": fields})
}

func (it Itineraries) findOneAndUpdate(ctx context.Context, filter, update bson.M) (*models.Itinerary, error) {
	var doc models.Itinerary
	err := it.coll().FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// SetSharedNotes writes the notes every member of the plan sees on one item
// and who wrote them; nil notes clears them.
func (it Itineraries) SetSharedNotes(ctx context.Context, id, itemID string, notes *string, by string) (*models.Itinerary, error) {
	now := it.s.Now()
	var update bson.M
	if notes == nil {
		update = bson.M{
			"$unset": bson.M{"items.$.sharedNotes": "", "items.$.sharedNotesBy": ""},
			"$set":   bson.M{"updatedAt": now},
		}
	} else {
		update = bson.M{"$set": bson.M{"items.$.sharedNotes": *notes, "items.$.sharedNotesBy": by, "updatedAt": now}}
	}
	return it.findOneAndUpdate(ctx, it.live(bson.M{"_id": id, "items.id": itemID}), update)
}

// AddMember puts userID on a live itinerary (no duplicates) and returns it.
func (it Itineraries) AddMember(ctx context.Context, id, userID string) (*models.Itinerary, error) {
	return it.findOneAndUpdate(ctx, it.live(bson.M{"_id": id}),
		bson.M{"$addToSet": bson.M{"memberIds": userID}, "$set": bson.M{"updatedAt": it.s.Now()}})
}

// RemoveMember takes userID, never the host, off a live itinerary and
// returns it; ErrNotFound when they are not a (non-host) member.
func (it Itineraries) RemoveMember(ctx context.Context, id, userID string) (*models.Itinerary, error) {
	return it.findOneAndUpdate(ctx,
		it.live(bson.M{"_id": id, "memberIds": userID, "hostId": bson.M{"$ne": userID}}),
		bson.M{"$pull": bson.M{"memberIds": userID}, "$set": bson.M{"updatedAt": it.s.Now()}})
}

// SetThreadID records the plan's group thread (set by the social area when
// it creates the thread).
func (it Itineraries) SetThreadID(ctx context.Context, id, threadID string) error {
	res, err := it.coll().UpdateOne(ctx, it.live(bson.M{"_id": id}),
		bson.M{"$set": bson.M{"threadId": threadID, "updatedAt": it.s.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// groupThreadIDs are the ids of the plan's group thread: the thread whose
// itineraryId is the plan (and the one the plan records, if different).
func (it Itineraries) groupThreadIDs(ctx context.Context, doc *models.Itinerary) ([]string, error) {
	filter := bson.M{"itineraryId": doc.ID}
	if doc.ThreadID != "" {
		filter = bson.M{"$or": []bson.M{{"itineraryId": doc.ID}, {"_id": doc.ThreadID}}}
	}
	cursor, err := it.s.db.Collection(CollThreads).Find(ctx, filter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// Leave takes a member (never the host) off the plan and out of its group
// thread, and marks their join cancelled. It returns the itinerary as it
// is now and the group thread they left ("" when the plan has none).
func (it Itineraries) Leave(ctx context.Context, id, userID string) (*models.Itinerary, string, error) {
	doc, err := it.RemoveMember(ctx, id, userID)
	if err != nil {
		return nil, "", err
	}
	now := it.s.Now()
	threads, err := it.groupThreadIDs(ctx, doc)
	if err != nil {
		return nil, "", err
	}
	threadID := ""
	if len(threads) > 0 {
		threadID = threads[0]
		if _, err := it.s.db.Collection(CollThreads).UpdateMany(ctx, bson.M{"_id": bson.M{"$in": threads}},
			bson.M{"$pull": bson.M{"memberIds": userID}, "$set": bson.M{"updatedAt": now}}); err != nil {
			return nil, "", err
		}
	}
	if _, err := it.s.db.Collection(CollJoinRequests).UpdateMany(ctx,
		bson.M{"itineraryId": id, "userId": userID, "status": models.JoinAccepted},
		bson.M{"$set": bson.M{"status": models.JoinCancelled, "updatedAt": now}}); err != nil {
		return nil, "", err
	}
	return doc, threadID, nil
}

// DeleteHost deletes a plan hostID hosts. The group thread with its
// messages, expenses and photos, and the plan's join records, go first;
// then the itinerary is marked deleted, so a retry after a partial failure
// finishes the job. It returns the itinerary as it was.
func (it Itineraries) DeleteHost(ctx context.Context, id, hostID string) (*models.Itinerary, error) {
	doc, err := it.findOne(ctx, it.live(bson.M{"_id": id, "hostId": hostID}))
	if err != nil {
		return nil, err
	}
	threads, err := it.groupThreadIDs(ctx, doc)
	if err != nil {
		return nil, err
	}
	if len(threads) > 0 {
		in := bson.M{"$in": threads}
		if _, err := it.s.db.Collection(CollMessages).DeleteMany(ctx, bson.M{"threadId": in}); err != nil {
			return nil, err
		}
		if _, err := it.s.db.Collection(CollExpenses).DeleteMany(ctx, bson.M{"groupId": in}); err != nil {
			return nil, err
		}
		if _, err := it.s.db.Collection(CollPhotos).DeleteMany(ctx, bson.M{"groupId": in, "kind": models.PhotoKindGroup}); err != nil {
			return nil, err
		}
		if _, err := it.s.db.Collection(CollThreads).DeleteMany(ctx, bson.M{"_id": in}); err != nil {
			return nil, err
		}
	}
	if _, err := it.s.db.Collection(CollJoinRequests).DeleteMany(ctx, bson.M{"itineraryId": id}); err != nil {
		return nil, err
	}
	res, err := it.coll().UpdateOne(ctx, it.live(bson.M{"_id": id, "hostId": hostID}),
		bson.M{"$set": bson.M{"status": models.ItineraryDeleted, "updatedAt": it.s.Now()}})
	if err != nil {
		return nil, err
	}
	if res.MatchedCount == 0 {
		return nil, ErrNotFound
	}
	return doc, nil
}
