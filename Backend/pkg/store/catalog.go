package store

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Catalog reads the activity catalogs (activities, demo_activities); a
// user's catalog field picks the collection. Nothing here writes, and the
// 1024-d embeddings are loaded only by Embeddings (catalog_vectors.go).
type Catalog struct{ s *Store }

func (s *Store) Catalog() Catalog { return Catalog{s} }

// CatalogCollection is the collection for a user's catalog: demo_activities
// for the demo catalog, activities for anything else.
func CatalogCollection(catalog string) string {
	if catalog == CollDemoActivities {
		return CollDemoActivities
	}
	return CollActivities
}

func (c Catalog) coll(catalog string) *mongo.Collection {
	return c.s.db.Collection(CatalogCollection(catalog))
}

// catalogNoVectors keeps the embedding fields out of every read.
var catalogNoVectors = bson.M{"embedding": 0, "embeddingText": 0, "embeddingTextHash": 0}

// Activity loads one catalog document by its hex id (ErrNotFound when the
// id is malformed or absent).
func (c Catalog) Activity(ctx context.Context, catalog, id string) (*models.Activity, error) {
	oid, ok := objectID(id)
	if !ok {
		return nil, ErrNotFound
	}
	var doc models.Activity
	err := decodeOne(c.coll(catalog).FindOne(ctx, bson.M{"_id": oid}, options.FindOne().SetProjection(catalogNoVectors)), &doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// CatalogPlace is a named point from a catalog: a place under its own name,
// an event under its venue's name.
type CatalogPlace struct {
	Name      string
	Lat, Lng  float64
	DistanceM float64 // from the reference point; 0 without one
}

type catalogRow struct {
	Kind      string              `bson:"kind"`
	Name      string              `bson:"name"`
	VenueName *string             `bson:"venueName"`
	Location  models.GeoJSONPoint `bson:"location"`
	Dist      float64             `bson:"dist"`
}

func (row catalogRow) place() (CatalogPlace, bool) {
	name := row.Name
	if row.Kind == "event" {
		if row.VenueName == nil {
			return CatalogPlace{}, false
		}
		name = *row.VenueName
	}
	name = strings.TrimSpace(name)
	if name == "" || len(row.Location.Coordinates) != 2 {
		return CatalogPlace{}, false
	}
	return CatalogPlace{Name: name, Lat: row.Location.Coordinates[1], Lng: row.Location.Coordinates[0], DistanceM: row.Dist}, true
}

// SearchPlaces finds catalog places whose name (or, for events, whose
// venue's name) contains q, ignoring case: nearest first from near, by name
// without it. An empty q is the places nearest to near (none without it).
// Duplicate names are dropped; at most limit come back.
func (c Catalog) SearchPlaces(ctx context.Context, catalog, q string, near *travel.Point, limit int) ([]CatalogPlace, error) {
	if limit <= 0 {
		limit = 10
	}
	q = strings.TrimSpace(q)
	var filter bson.M
	if q != "" {
		rx := bson.Regex{Pattern: regexp.QuoteMeta(q), Options: "i"}
		filter = bson.M{"$or": []bson.M{
			{"kind": bson.M{"$ne": "event"}, "name": rx},
			{"kind": "event", "venueName": rx},
		}}
	} else if near == nil {
		return []CatalogPlace{}, nil
	}
	// Several events can share a venue: over-fetch, then keep the first of each name.
	fetch := int64(limit * 4)
	var rows []catalogRow
	if near != nil {
		stage := bson.M{
			"near":          bson.M{"type": "Point", "coordinates": []float64{near.Lng, near.Lat}},
			"key":           "location",
			"distanceField": "dist",
			"spherical":     true,
		}
		if filter != nil {
			stage["query"] = filter
		}
		cursor, err := c.coll(catalog).Aggregate(ctx, mongo.Pipeline{
			{{Key: "$geoNear", Value: stage}},
			{{Key: "$limit", Value: fetch}},
			{{Key: "$project", Value: bson.M{"kind": 1, "name": 1, "venueName": 1, "location": 1, "dist": 1}}},
		})
		if err != nil {
			return nil, err
		}
		if err := cursor.All(ctx, &rows); err != nil {
			return nil, err
		}
	} else {
		cursor, err := c.coll(catalog).Find(ctx, filter, options.Find().
			SetProjection(bson.M{"kind": 1, "name": 1, "venueName": 1, "location": 1}).
			SetSort(bson.D{{Key: "name", Value: 1}}).SetLimit(fetch))
		if err != nil {
			return nil, err
		}
		if err := cursor.All(ctx, &rows); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	out := []CatalogPlace{}
	for _, row := range rows {
		place, ok := row.place()
		if !ok || seen[strings.ToLower(place.Name)] {
			continue
		}
		seen[strings.ToLower(place.Name)] = true
		out = append(out, place)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// Nearest is the catalog place closest to pt within maxMeters; ErrNotFound
// when there is none that close.
func (c Catalog) Nearest(ctx context.Context, catalog string, pt travel.Point, maxMeters float64) (*CatalogPlace, error) {
	cursor, err := c.coll(catalog).Aggregate(ctx, mongo.Pipeline{
		{{Key: "$geoNear", Value: bson.M{
			"near":          bson.M{"type": "Point", "coordinates": []float64{pt.Lng, pt.Lat}},
			"key":           "location",
			"distanceField": "dist",
			"maxDistance":   maxMeters,
			"spherical":     true,
		}}},
		{{Key: "$limit", Value: 8}},
		{{Key: "$project", Value: bson.M{"kind": 1, "name": 1, "venueName": 1, "location": 1, "dist": 1}}},
	})
	if err != nil {
		return nil, err
	}
	var rows []catalogRow
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if place, ok := row.place(); ok {
			return &place, nil
		}
	}
	return nil, ErrNotFound
}
