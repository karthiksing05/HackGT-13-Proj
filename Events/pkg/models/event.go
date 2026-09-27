package models

import "time"

// ReservedSlugs cannot be used as event slugs because they conflict with merchant system routes.
var ReservedSlugs = map[string]bool{
	"api":       true,
	"t":         true,
	"dashboard": true,
	"_demo":     true,
	"healthz":   true,
	"sandbox":   true,
	"events":    true,
	"static":    true,
	"favicon":   true,
}

// IsReservedSlug reports whether a slug is reserved.
func IsReservedSlug(slug string) bool {
	return ReservedSlugs[slug]
}

// Event represents a bookable event in Saltlight Harbor.
type Event struct {
	Slug         string    `json:"slug" bson:"slug"`
	Title        string    `json:"title" bson:"title"`
	Description  string    `json:"description" bson:"description"`
	Summary      string    `json:"summary" bson:"summary"`
	Category     string    `json:"category" bson:"category"`
	Tags         []string  `json:"tags" bson:"tags"`
	Venue        string    `json:"venue" bson:"venue"`
	Address      string    `json:"address" bson:"address"`
	Start        time.Time `json:"starts_at" bson:"start"`
	End          time.Time `json:"ends_at" bson:"end"`
	UnitCents    int       `json:"unit_cents" bson:"unitCents"`
	MaxUnitCents int       `json:"max_unit_cents" bson:"maxUnitCents"`
	Capacity     int       `json:"capacity" bson:"capacity"`
	Remaining    int       `json:"remaining" bson:"remaining"`
	ImageURL     string    `json:"image_url" bson:"imageUrl"`
}

// EventSummary is the minimal event description embedded in quotes and order confirmations.
type EventSummary struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	StartsAt string `json:"starts_at"` // RFC 3339 UTC
	Venue    string `json:"venue"`
}

// Summary returns the EventSummary for the event.
func (e *Event) SummaryView() EventSummary {
	return EventSummary{
		Slug:     e.Slug,
		Title:    e.Title,
		StartsAt: e.Start.UTC().Format(time.RFC3339),
		Venue:    e.Venue,
	}
}
