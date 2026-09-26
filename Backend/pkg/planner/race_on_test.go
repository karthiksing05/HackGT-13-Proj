//go:build race

package planner

// raceEnabled: the race detector slows planning several-fold, so
// wall-clock budgets only hold in normal builds.
const raceEnabled = true
