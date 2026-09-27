package store

import (
	"Backend/pkg/models"
	"context"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// searchProjection leaves out what a search result never shows: vectors,
// long texts, trail geometry and provenance.
var searchProjection = bson.M{
	"embedding": 0, "embeddingText": 0, "embeddingTextHash": 0, "embeddingMeta": 0, "embeddingTextMeta": 0,
	"description": 0, "trail.geometry": 0, "sources": 0, "sourceKeys": 0, "recurrence": 0,
}

// eventLookback: an event without a published end that started this long
// before the day can't still be running (a stay is at most six hours).
const eventLookback = 24 * time.Hour

// SearchActivities is the catalog side of GET /activities/search (the
// must-see picks): the events that may run during [from, to) and the
// places whose name, venue name, category or one of the tags contains q,
// ignoring case (a space in q also matches the underscore of a category or
// tag, so "live music" finds live_music); every such event and place for an
// empty q. The caller applies the planner's pick rules (opening hours, age,
// over) and ranks. At most limit of each kind come back, events by start
// and places by rating (then _id).
func (c Catalog) SearchActivities(ctx context.Context, catalog, q string, from, to time.Time, limit int) ([]models.Activity, error) {
	var text bson.M
	if q = strings.TrimSpace(q); q != "" {
		rx := bson.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
		slug := bson.Regex{Pattern: regexp.QuoteMeta(strings.Join(strings.Fields(q), "_")), Options: "i"}
		text = bson.M{"$or": []bson.M{{"name": rx}, {"venueName": rx}, {"category": slug}, {"tags": slug}}}
	}
	events := bson.M{"kind": "event", "start": bson.M{"$lt": to}, "$or": []bson.M{
		{"end": bson.M{"$gt": from}},
		{"end": nil, "start": bson.M{"$gte": from.Add(-eventLookback)}},
	}}
	places := bson.M{"kind": "place"}
	var out []models.Activity
	for _, part := range []struct {
		filter bson.M
		sort   bson.D
	}{
		{events, bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}},
		{places, bson.D{{Key: "rating", Value: -1}, {Key: "_id", Value: 1}}},
	} {
		filter := part.filter
		if text != nil {
			filter = bson.M{"$and": []bson.M{part.filter, text}}
		}
		opts := options.Find().SetProjection(searchProjection).SetSort(part.sort)
		if limit > 0 {
			opts.SetLimit(int64(limit))
		}
		cursor, err := c.coll(catalog).Find(ctx, filter, opts)
		if err != nil {
			return nil, err
		}
		var docs []models.Activity
		if err := cursor.All(ctx, &docs); err != nil {
			return nil, err
		}
		out = append(out, docs...)
	}
	return out, nil
}
