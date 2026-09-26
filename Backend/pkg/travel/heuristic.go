package travel

import (
	"context"
	"time"
)

// Detour factor: street distance is usually ~1.3x the straight line.
const detour = 1.3

// Legs at or under this straight-line distance are walked whatever the mode.
const walkAlwaysKm = 0.8

// Heuristic estimates legs from straight-line distance. It needs no network,
// so it is the default provider and the fallback for real routing.
type Heuristic struct{}

func (Heuristic) Legs(_ context.Context, pairs []Pair, mode Mode) (map[Pair]Leg, error) {
	out := make(map[Pair]Leg, len(pairs))
	for _, p := range pairs {
		out[p] = Estimate(p.From, p.To, mode)
	}
	return out, nil
}

// Estimate is the heuristic leg for one pair. Speeds follow the planner's
// previous calculateLegDuration: walk 4.5 km/h, transit 20 km/h + 5 min
// waiting, drive 30 km/h + 5 min parking or pickup.
func Estimate(a, b Point, mode Mode) Leg {
	km := HaversineKm(a, b)
	street := km * detour
	walk := minutes(street / 4.5 * 60)

	if km <= walkAlwaysKm || mode == Walk || mode == "" {
		return Leg{Duration: walk, DistanceKm: street, Mode: Walk, Source: "heuristic"}
	}

	var d time.Duration
	switch mode {
	case Transit:
		d = minutes(street/20*60 + 5)
	case Drive:
		d = minutes(street/30*60 + 5)
	default:
		d = walk
	}
	if walk <= d {
		return Leg{Duration: walk, DistanceKm: street, Mode: Walk, Source: "heuristic"}
	}
	return Leg{Duration: d, DistanceKm: street, Mode: mode, Source: "heuristic"}
}

func minutes(m float64) time.Duration {
	return time.Duration(m * float64(time.Minute)).Round(time.Minute)
}
