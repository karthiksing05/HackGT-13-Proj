package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/store"
	"context"
	"fmt"
	"math"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Taste vectors (People for you, Forum match %) come from the ML service
// the way a real account's do (pkg/profiles: /v1/user-profile over the
// saved preferences) when ML_SERVICE_URL answers with the catalog's encoder.
// Otherwise each person gets a blend of the catalog's own vectors for the
// categories they like, which lives in the same embedding space.

// profileTimeout bounds one person's profile refresh through the ML service.
const profileTimeout = 20 * time.Second

// tripCategories are the catalog categories that speak to each trip type:
// the inverse of the taste mapping in pkg/api/itineraries (taste.go).
// early_mornings is a time of day, not a category.
var tripCategories = map[string][]string{
	"outdoors":   {"park", "garden", "viewpoint", "zoo_aquarium", "hike"},
	"long_walks": {"hike", "tour"},
	"museums":    {"museum", "gallery", "landmark", "theater", "tour"},
	"food":       {"restaurant", "cafe", "market"},
	"shopping":   {"market", "shopping"},
	"live_music": {"live_music", "festival"},
	"nightlife":  {"bar", "nightclub", "comedy"},
	"sports":     {"sports_event", "rec_venue"},
	"big_crowds": {"nightclub", "sports_event", "festival"},
}

// catalogVector is one catalog document's stored vector.
type catalogVector struct {
	category string
	vec      []float64
}

// loadVectors reads the usable vectors of a catalog (places and events),
// in id order so a blend always adds them up the same way.
func loadVectors(ctx context.Context, st *store.Store, coll string) ([]catalogVector, error) {
	cursor, err := st.Collection(coll).Find(ctx, bson.M{"embedding.0": bson.M{"$exists": true}},
		options.Find().SetProjection(bson.M{"category": 1, "embedding": 1}).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("read %s vectors: %w", coll, err)
	}
	var rows []struct {
		Category  string    `bson:"category"`
		Embedding []float64 `bson:"embedding"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("read %s vectors: %w", coll, err)
	}
	out := make([]catalogVector, 0, len(rows))
	for _, r := range rows {
		if ml.Usable(r.Embedding, ml.Dim) {
			out = append(out, catalogVector{category: r.Category, vec: r.Embedding})
		}
	}
	return out, nil
}

// blend is a person's likes (like) or dislikes vector: for each trip type
// rated 4–5 (likes) or 1–2 (dislikes), the mean vector of the catalog items
// of its categories, weighted by how far the rating is from 3; then scaled
// to unit length. n is how many catalog items went in; the vector is nil
// when none did.
func blend(ratings map[string]int, vectors []catalogVector, like bool) ([]float64, int) {
	sum := make([]float64, ml.Dim)
	n := 0
	for _, trip := range contract.TripTypes {
		r, ok := ratings[trip]
		if !ok {
			continue
		}
		weight := float64(r - 3)
		if !like {
			weight = -weight
		}
		if weight <= 0 {
			continue
		}
		mean := make([]float64, ml.Dim)
		count := 0
		for _, cv := range vectors {
			if !hasAny([]string{cv.category}, tripCategories[trip]) {
				continue
			}
			for i, x := range cv.vec {
				mean[i] += x
			}
			count++
		}
		if count == 0 {
			continue
		}
		for i := range sum {
			sum[i] += weight * mean[i] / float64(count)
		}
		n += count
	}
	if n == 0 || !unit(sum) {
		return nil, 0
	}
	return sum, n
}

// unit scales v to length 1 in place; false for a zero vector.
func unit(v []float64) bool {
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return false
	}
	for i := range v {
		v[i] /= norm
	}
	return true
}

// tasteSource says where taste vectors come from on this run.
type tasteSource struct {
	client *ml.Client // nil or not usable: blends only
	usable bool
	line   string // what the report says
}

// checkTaste asks the ML service's /healthz whether it can build profiles
// with the catalog's encoder.
func checkTaste(ctx context.Context, client *ml.Client, url string) tasteSource {
	if client == nil {
		return tasteSource{line: "blended from each catalog's vectors (no ML service configured)"}
	}
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h, err := client.Health(hctx)
	switch {
	case err != nil:
		return tasteSource{client: client, line: fmt.Sprintf("blended from each catalog's vectors (the ML service at %s did not answer)", url)}
	case !h.CanEmbed() || h.EmbeddingModel != ml.Model || h.EmbeddingDim != ml.Dim:
		return tasteSource{client: client, line: fmt.Sprintf("blended from each catalog's vectors (the ML service at %s cannot embed with %s now: status %q, model %q)",
			url, ml.Model, h.Status, h.EmbeddingModel)}
	}
	return tasteSource{client: client, usable: true,
		line: fmt.Sprintf("built by the ML service at %s (/v1/user-profile, %s), like every account's", url, h.EmbeddingModel)}
}
