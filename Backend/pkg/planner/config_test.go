package planner

import (
	"testing"
	"time"
)

func TestDefaultsMatchTheDesign(t *testing.T) {
	c := DefaultConfig()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"PLANNER", c.Mode, "auto"},
		{"SOFT_BUDGET", c.SoftBudget, 5 * time.Second}, {"HARD_TIMEOUT", c.HardTimeout, 9 * time.Second},
		{"ROUNDS", c.Rounds, 3}, {"EPSILON", c.Epsilon, 0.02}, {"SHORTLIST_N", c.ShortlistN, 120},
		{"FACET_QUOTA", c.FacetQuota, 15}, {"CATEGORY_CAP", c.CategoryCap, 25},
		{"PHASE_A_EVENTS", c.PhaseAEvents, 400}, {"PHASE_A_PLACES", c.PhaseAPlaces, 600},
		{"EMBED_FETCH_CAP", c.EmbedFetchCap, 800}, {"EXPANSION_LIMIT", c.ExpansionLimit, 40},
		{"MAX_EXPANSIONS", c.MaxExpansions, 2}, {"ML_TIMEOUT", c.MLTimeout, 3 * time.Second},
		{"JEV", c.Jev, "async"}, {"JEV_TIMEOUT", c.JevTimeout, 45 * time.Second}, {"JEV_TOP_K", c.JevTopK, 20},
		{"SEARCH_WEIGHT", c.SearchWeight, 0.6}, {"DISLIKE_LAMBDA", c.DislikeLambda, 0.5},
		{"RANGE walkable", c.RangeKm["walkable"], 2.0}, {"RANGE transit", c.RangeKm["transit"], 10.0}, {"RANGE anywhere", c.RangeKm["anywhere"], 25.0},
		{"WALK_LEG_CAP", c.WalkLegCapKm, 4.0}, {"CITY_SNAP", c.CitySnapKm, 60.0},
		{"POOL_TTL", c.PoolTTL, 6 * time.Hour}, {"RUN_TTL", c.RunTTL, 72 * time.Hour},
		{"SERIES_CAP", c.SeriesCap, 96}, {"DEBUG", c.Debug, false},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}
}

func TestFromEnvOverrides(t *testing.T) {
	for k, v := range map[string]string{
		"PLANNER": "dag", "PLANNER_SOFT_BUDGET_MS": "2500", "PLANNER_ROUNDS": "2", "PLANNER_EPSILON": "0.05",
		"PLANNER_SHORTLIST_N": "80", "PLANNER_JEV": "sync", "PLANNER_RANGE_KM": "walkable=1.5,anywhere=30",
		"PLANNER_POOL_TTL_H": "1.5", "PLANNER_SERIES_CAP": "120", "PLANNER_DEBUG": "1", "PLANNER_ML_TIMEOUT_MS": "1200",
	} {
		t.Setenv(k, v)
	}
	c := FromEnv()
	if c.Mode != "dag" || c.SoftBudget != 2500*time.Millisecond || c.Rounds != 2 || c.Epsilon != 0.05 || c.ShortlistN != 80 {
		t.Errorf("overrides: %+v", c)
	}
	if c.Jev != "sync" || c.RangeKm["walkable"] != 1.5 || c.RangeKm["transit"] != 10 || c.RangeKm["anywhere"] != 30 {
		t.Errorf("jev %s ranges %v", c.Jev, c.RangeKm)
	}
	if c.PoolTTL != 90*time.Minute || c.SeriesCap != 120 || c.Itinerary.SeriesCap != 120 || !c.Debug || c.MLTimeout != 1200*time.Millisecond {
		t.Errorf("ttl %v series %d/%d debug %v ml %v", c.PoolTTL, c.SeriesCap, c.Itinerary.SeriesCap, c.Debug, c.MLTimeout)
	}
	// Garbage keeps the default.
	t.Setenv("PLANNER_ROUNDS", "many")
	t.Setenv("PLANNER_JEV", "maybe")
	t.Setenv("PLANNER", "yolo")
	c = FromEnv()
	if c.Rounds != 3 || c.Jev != "async" || c.Mode != "auto" {
		t.Errorf("bad values should keep defaults: %d %s %s", c.Rounds, c.Jev, c.Mode)
	}
	if got := parseRangeKm("3,12", DefaultConfig().RangeKm); got["walkable"] != 3 || got["transit"] != 12 || got["anywhere"] != 25 {
		t.Errorf("positional ranges %v", got)
	}
}

func TestNewRequiresItsStores(t *testing.T) {
	src := &FakeSource{}
	pools := NewMemPoolStore(nil)
	if _, err := New(DefaultConfig(), Deps{Embeddings: src, Pools: pools}); err == nil {
		t.Error("no source")
	}
	if _, err := New(DefaultConfig(), Deps{Source: src, Pools: pools}); err == nil {
		t.Error("no embeddings")
	}
	if _, err := New(DefaultConfig(), Deps{Source: src, Embeddings: src}); err == nil {
		t.Error("no pool store: pools would not survive a restart")
	}
	p, err := New(Config{}, Deps{Source: src, Embeddings: src, Pools: pools})
	if err != nil || p.Travel == nil || p.Clock == nil || p.NewID == nil || p.Cfg.Itinerary.Paces == nil {
		t.Errorf("defaults: %+v %v", p, err)
	}
}
