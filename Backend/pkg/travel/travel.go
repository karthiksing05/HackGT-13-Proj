// Package travel estimates travel time between two points. Itinerary logic
// only depends on the Provider interface, so the routing backend (heuristic,
// cached, ORS, OSRM) can change without touching the optimizer.
package travel

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64
	Lng float64
}

// Mode is the main way the user gets between stops.
type Mode string

const (
	Walk    Mode = "walk"
	Transit Mode = "transit"
	Drive   Mode = "drive"
)

// Pair is a directed origin -> destination. (A, B) and (B, A) are different
// pairs: driving times are not symmetric.
type Pair struct {
	From Point
	To   Point
}

// Leg is one travel segment.
type Leg struct {
	Duration   time.Duration
	DistanceKm float64
	Mode       Mode   // mode actually used; short legs are walked
	Source     string // "heuristic", "cache", "ors", ...
}

// Provider returns a leg for every requested pair. Implementations should
// batch upstream calls; callers pass all the pairs they need at once.
type Provider interface {
	Legs(ctx context.Context, pairs []Pair, mode Mode) (map[Pair]Leg, error)
}

// EarthRadiusKm is the mean Earth radius. Anything that converts distances
// to angles (e.g. Mongo $centerSphere) must use the same value.
const EarthRadiusKm = 6371.0

// HaversineKm is the straight-line distance between two points.
func HaversineKm(a, b Point) float64 {
	rad := math.Pi / 180.0
	dLat := (b.Lat - a.Lat) * rad
	dLng := (b.Lng - a.Lng) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthRadiusKm * math.Asin(math.Sqrt(math.Min(1, h)))
}

// Top speeds used for lower bounds. They must never be slower than any real
// leg, or pruning would drop feasible pairs.
var maxSpeedKmh = map[Mode]float64{
	Walk:    6,
	Transit: 40,
	Drive:   60,
}

// LowerBound is the fastest a leg could possibly be. Used to prune pairs
// before asking a provider.
func LowerBound(a, b Point, mode Mode) time.Duration {
	speed, ok := maxSpeedKmh[mode]
	if !ok {
		speed = maxSpeedKmh[Walk]
	}
	hours := HaversineKm(a, b) / speed
	return time.Duration(hours * float64(time.Hour))
}

// LocKey is a stable cache key for a point, rounded to ~11 m.
func LocKey(p Point) string {
	return fmt.Sprintf("%.4f,%.4f", p.Lat, p.Lng)
}

// ParsePoint parses "lat,lng" (whitespace allowed). It rejects anything that
// isn't two in-range numbers, so free-text place names return ok=false.
func ParsePoint(s string) (Point, bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return Point{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return Point{}, false
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 || math.IsNaN(lat) || math.IsNaN(lng) {
		return Point{}, false
	}
	return Point{Lat: lat, Lng: lng}, true
}
