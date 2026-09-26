//go:build race

package itinerary

// raceEnabled: the race detector slows the optimizer several-fold, so
// wall-clock budgets only hold in normal builds.
const raceEnabled = true
