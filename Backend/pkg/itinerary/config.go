// Package itinerary turns ranked candidate activities into feasible,
// time-ordered itineraries. It models the day as a DAG: every visit is a
// node with a fixed start and end, an edge i -> j exists when j can follow
// i after travel, and a K-best label pass finds the highest-utility paths
// from the user's start point to their end point.
//
// The package is pure: no database or HTTP. Travel times come from a
// travel.Provider passed in by the caller.
package itinerary

import (
	"strings"
	"time"
)

// PaceProfile is how full the user wants the day.
type PaceProfile struct {
	MaxStops   int
	MaxWait    time.Duration // longest idle gap between two stops
	LambdaWait float64       // utility lost per idle minute
}

// Config holds the optimizer's tunables. Utilities are on a 0..1 scale per
// stop, so a penalty of 0.15 costs roughly a sixth of a good stop.
type Config struct {
	// Score baseline: a stop scoring at or below it adds nothing to a plan;
	// above it, utility is (score - Tau) / (1 - Tau). 0.5 matches the
	// model's "good match" line (ml README) and the midpoint of the 0-4 LLM
	// rerank scale (2/4).
	Tau          float64
	LambdaTravel float64       // utility lost per travel minute
	Buffer       time.Duration // slack between arriving and the next start
	// Weight of idle time before the first stop, relative to idle time
	// between stops. 0 by default: waiting at home to go out isn't a cost.
	FirstWaitWeight float64
	K               int     // partial paths kept per node
	PoolSize        int     // finished itineraries kept
	Mu              float64 // overlap penalty when choosing varied itineraries

	SlotStep        time.Duration // spacing of start times for flexible visits
	MaxSlots        int           // start times per flexible activity
	MinDuration     time.Duration
	MaxDuration     time.Duration
	DefaultDuration time.Duration

	DefaultUtility float64 // used when the ranker gave no score; above Tau so plans still form
	PlaceWeight    float64 // places are always there; events are one-offs

	DefaultMaxLegKm map[string]float64 // by travel mode, when range_km is 0
	Paces           map[string]PaceProfile
}

func DefaultConfig() Config {
	return Config{
		Tau:             0.5,
		LambdaTravel:    0.005,
		Buffer:          10 * time.Minute,
		FirstWaitWeight: 0,
		K:               16,
		PoolSize:        64,
		Mu:              0.5,

		SlotStep:        30 * time.Minute,
		MaxSlots:        12,
		MinDuration:     15 * time.Minute,
		MaxDuration:     6 * time.Hour,
		DefaultDuration: 60 * time.Minute,

		DefaultUtility: 0.6,
		PlaceWeight:    0.8,

		DefaultMaxLegKm: map[string]float64{"walk": 2, "transit": 10, "drive": 25},
		Paces: map[string]PaceProfile{
			"chill":    {MaxStops: 2, MaxWait: 90 * time.Minute, LambdaWait: 0.002},
			"balanced": {MaxStops: 3, MaxWait: 60 * time.Minute, LambdaWait: 0.004},
			"packed":   {MaxStops: 5, MaxWait: 30 * time.Minute, LambdaWait: 0.008},
		},
	}
}

// Pace returns the profile for a pace name. Unknown or empty means balanced;
// "relaxed" is accepted as an alias for chill.
func (c Config) Pace(name string) PaceProfile {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "relaxed" {
		name = "chill"
	}
	if p, ok := c.Paces[name]; ok {
		return p
	}
	return c.Paces["balanced"]
}
