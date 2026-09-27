package models

import "time"

// CalendarEvent is one busy block on a person's calendar (calendar_events): a class, a lab, a shift,
// time set aside for homework. Connected calendars and the showcase seed write them; the app shows
// them as busy blocks on Home and in the calendar, and the planner keeps sidequests clear of them.
// A recurring event is stored as one document per occurrence, sharing a SeriesID.
type CalendarEvent struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"userId"`
	Title     string    `bson:"title"`
	Start     time.Time `bson:"start"`
	End       time.Time `bson:"end"`
	Location  string    `bson:"location,omitempty"`
	Source    string    `bson:"source"`             // google | outlook | seed
	SeriesID  string    `bson:"seriesId,omitempty"` // shared by the occurrences of a recurring event
	Seed      string    `bson:"seed,omitempty"`     // "calendar-v1" on the showcase seed's events
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}
