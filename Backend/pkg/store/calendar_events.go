package store

import (
	"Backend/pkg/models"
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func init() {
	appIndexes = append(appIndexes, indexSpec{coll: CollCalendarEvents,
		keys: bson.D{{Key: "userId", Value: 1}, {Key: "start", Value: 1}}, name: "userId_start"})
}

// CalendarEvents is calendar_events: the busy blocks on people's calendars
// (one document per occurrence), written by connected calendars and the
// showcase seed. Every read is one user's own.
type CalendarEvents struct{ s *Store }

func (s *Store) CalendarEvents() CalendarEvents { return CalendarEvents{s} }

func (c CalendarEvents) coll() *mongo.Collection { return c.s.db.Collection(CollCalendarEvents) }

// Overlapping is userID's events that overlap [from, to), by start (ties by
// id). An event that ends before it starts is no busy time and is left out.
func (c CalendarEvents) Overlapping(ctx context.Context, userID string, from, to time.Time) ([]models.CalendarEvent, error) {
	out := []models.CalendarEvent{}
	if userID == "" || !to.After(from) {
		return out, nil
	}
	cursor, err := c.coll().Find(ctx, bson.M{"userId": userID, "start": bson.M{"$lt": to}, "end": bson.M{"$gt": from}},
		options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []models.CalendarEvent
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for _, ev := range docs {
		if ev.End.After(ev.Start) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Get is one of userID's events; anyone else's is ErrNotFound.
func (c CalendarEvents) Get(ctx context.Context, userID, id string) (*models.CalendarEvent, error) {
	var doc models.CalendarEvent
	if err := decodeOne(c.coll().FindOne(ctx, bson.M{"_id": id, "userId": userID}), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Insert stores events as they are, filling in a missing id and the
// timestamps (connected calendars and tests; the seed writes its own).
func (c CalendarEvents) Insert(ctx context.Context, events ...*models.CalendarEvent) error {
	if len(events) == 0 {
		return nil
	}
	now := c.s.Now()
	docs := make([]any, 0, len(events))
	for _, ev := range events {
		if ev.ID == "" {
			ev.ID = NewID()
		}
		if ev.CreatedAt.IsZero() {
			ev.CreatedAt = now
		}
		ev.UpdatedAt = now
		docs = append(docs, ev)
	}
	_, err := c.coll().InsertMany(ctx, docs)
	return err
}
