package store_test

import (
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"testing"
	"time"
)

func TestCalendarEventsOverlappingIsPerUser(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	day := func(hour, minute int) time.Time { return time.Date(2026, 9, 28, hour, minute, 0, 0, time.UTC) }
	ev := func(user, title string, start, end time.Time) *models.CalendarEvent {
		return &models.CalendarEvent{UserID: user, Title: title, Start: start, End: end, Source: "seed", Seed: "calendar-v1"}
	}
	lecture := ev("u1", "CS 3510 lecture", day(17, 0), day(17, 50))
	if err := s.CalendarEvents().Insert(ctx,
		ev("u1", "Lab", day(19, 0), day(21, 0)),
		lecture,
		ev("u1", "Office hours", day(14, 0), day(15, 0)),
		ev("u2", "Someone else's class", day(17, 0), day(18, 0)),
		ev("u1", "Ends before it starts", day(16, 0), day(15, 30)),
	); err != nil {
		t.Fatal(err)
	}
	if lecture.ID == "" || !lecture.CreatedAt.Equal(testNow) || !lecture.UpdatedAt.Equal(testNow) {
		t.Fatalf("insert fills id and timestamps: %+v", lecture)
	}

	titles := func(from, to time.Time) []string {
		t.Helper()
		evs, err := s.CalendarEvents().Overlapping(ctx, "u1", from, to)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range evs {
			out = append(out, e.Title)
		}
		return out
	}
	// In start order; an event that started before the range but runs into
	// it counts, one that ends as the range begins does not.
	if got := titles(day(15, 0), day(20, 0)); len(got) != 2 || got[0] != "CS 3510 lecture" || got[1] != "Lab" {
		t.Errorf("15:00–20:00: %v", got)
	}
	if got := titles(day(14, 30), day(17, 0)); len(got) != 1 || got[0] != "Office hours" {
		t.Errorf("14:30–17:00 (the lecture starts as it ends): %v", got)
	}
	if got := titles(day(0, 0), day(24, 0)); len(got) != 3 {
		t.Errorf("the whole day: %v", got)
	}
	if got := titles(day(17, 0), day(17, 0)); len(got) != 0 {
		t.Errorf("an empty range: %v", got)
	}

	got, err := s.CalendarEvents().Get(ctx, "u1", lecture.ID)
	if err != nil || got.Title != lecture.Title || !got.Start.Equal(lecture.Start) {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.CalendarEvents().Get(ctx, "u2", lecture.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another user's event: %v", err)
	}

	// EnsureIndexes created the read's index.
	specs, err := s.Collection(store.CollCalendarEvents).Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, spec := range specs {
		found = found || spec.Name == "userId_start"
	}
	if !found {
		t.Error("no userId_start index on calendar_events")
	}
}
