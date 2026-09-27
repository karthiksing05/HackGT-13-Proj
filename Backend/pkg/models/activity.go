package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Catalog document schemas (collections activities / demo_activities). Moved
// unchanged from models.go: pkg/itinerary, pkg/ml and the planner depend on
// these exact fields.

// GeoJSONPoint represents a GeoJSON 2D Point with [lng, lat]
type GeoJSONPoint struct {
	Type        string    `bson:"type" json:"type"`               // "Point"
	Coordinates []float64 `bson:"coordinates" json:"coordinates"` // [lng, lat] - lng FIRST
}

// ActivityAddress represents the address component of an activity
type ActivityAddress struct {
	Formatted   *string `bson:"formatted" json:"formatted"`
	Street      *string `bson:"street" json:"street"`
	Locality    *string `bson:"locality" json:"locality"`
	Region      *string `bson:"region" json:"region"`
	PostalCode  *string `bson:"postalCode" json:"postalCode"`
	CountryCode *string `bson:"countryCode" json:"countryCode"`
}

// ActivityDuration represents duration metadata
type ActivityDuration struct {
	MedianMin float64 `bson:"medianMin" json:"medianMin"`
	Sigma     float64 `bson:"sigma" json:"sigma"`
	P75Min    float64 `bson:"p75Min" json:"p75Min"`
	Source    string  `bson:"source" json:"source"`
}

// ActivityPrice represents activity price and tier
type ActivityPrice struct {
	Tier     int      `bson:"tier" json:"tier"` // 0 free, 1 <$15, 2 $15–40, 3 $40+
	Cents    int64    `bson:"cents" json:"cents"`
	Min      *float64 `bson:"min,omitempty" json:"min,omitempty"` // whole currency units, as ingested
	Max      *float64 `bson:"max,omitempty" json:"max,omitempty"`
	Currency string   `bson:"currency" json:"currency"`
	IsFree   bool     `bson:"isFree" json:"isFree"`
}

// ActivitySource represents provenance of scraped or imported activity
type ActivitySource struct {
	Name      string    `bson:"name" json:"name"`
	ID        string    `bson:"id" json:"id"`
	URL       string    `bson:"url" json:"url"`
	FetchedAt time.Time `bson:"fetchedAt" json:"fetchedAt"`
}

// WeeklyHourRange represents open/close minutes of the week (e.g. 360 to 1080)
type WeeklyHourRange struct {
	Open  int `bson:"open" json:"open"`
	Close int `bson:"close" json:"close"`
}

// GeoJSONLineString represents a GeoJSON LineString geometry
type GeoJSONLineString struct {
	Type        string      `bson:"type" json:"type"`
	Coordinates [][]float64 `bson:"coordinates" json:"coordinates"`
}

// TrailInfo represents trail geometry and statistics
type TrailInfo struct {
	LengthKm float64           `bson:"lengthKm" json:"lengthKm"`
	AscentM  float64           `bson:"ascentM" json:"ascentM"`
	DescentM float64           `bson:"descentM" json:"descentM"`
	Loop     bool              `bson:"loop" json:"loop"`
	Geometry GeoJSONLineString `bson:"geometry" json:"geometry"`
}

// Activity represents the full activity document schema from the freetime database
type Activity struct {
	ID                bson.ObjectID     `bson:"_id,omitempty" json:"_id"`
	Kind              string            `bson:"kind" json:"kind"` // "place" | "event"
	City              string            `bson:"city" json:"city"` // "atlanta"
	Name              string            `bson:"name" json:"name"`
	Summary           *string           `bson:"summary" json:"summary"`
	Description       *string           `bson:"description" json:"description"`
	Category          string            `bson:"category" json:"category"`
	SourceCategory    *string           `bson:"sourceCategory" json:"sourceCategory"`
	Tags              []string          `bson:"tags" json:"tags"`
	Location          GeoJSONPoint      `bson:"location" json:"location"` // coordinates: [lng, lat]
	Address           *ActivityAddress  `bson:"address" json:"address"`
	VenueName         *string           `bson:"venueName" json:"venueName"`
	Start             *time.Time        `bson:"start" json:"start"`
	End               *time.Time        `bson:"end" json:"end"`
	Attendance        *string           `bson:"attendance" json:"attendance"`
	Timezone          string            `bson:"timezone" json:"timezone"` // "America/New_York"
	WeeklyHours       []WeeklyHourRange `bson:"weeklyHours" json:"weeklyHours"`
	HoursSource       *string           `bson:"hoursSource" json:"hoursSource"`
	Recurrence        any               `bson:"recurrence" json:"recurrence"`
	Duration          *ActivityDuration `bson:"duration" json:"duration"`
	Price             *ActivityPrice    `bson:"price" json:"price"`
	Rating            *float64          `bson:"rating" json:"rating"`
	RatingCount       *int              `bson:"ratingCount" json:"ratingCount"`
	Popularity        *float64          `bson:"popularity" json:"popularity"`
	Trail             *TrailInfo        `bson:"trail" json:"trail"`
	URL               *string           `bson:"url" json:"url"`
	TicketURL         *string           `bson:"ticketUrl" json:"ticketUrl"`
	ImageURL          *string           `bson:"imageUrl" json:"imageUrl"`
	SourceKeys        []string          `bson:"sourceKeys" json:"sourceKeys"`
	Sources           []ActivitySource  `bson:"sources" json:"sources"`
	GooglePlaceID     *string           `bson:"googlePlaceId" json:"googlePlaceId"`
	ExpiresAt         *time.Time        `bson:"expiresAt" json:"expiresAt"`
	CreatedAt         time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt         time.Time         `bson:"updatedAt" json:"updatedAt"`
	Embedding         []float64         `bson:"embedding,omitempty" json:"embedding,omitempty"`
	EmbeddingText     *string           `bson:"embeddingText,omitempty" json:"embeddingText,omitempty"`
	EmbeddingTextHash *string           `bson:"embeddingTextHash,omitempty" json:"embeddingTextHash,omitempty"`
	Score             *float64          `bson:"score,omitempty" json:"score,omitempty"`
	RerankScore       *float64          `bson:"rerankScore,omitempty" json:"rerank_score,omitempty"`
}
