# Planner design: guaranteed filters, iterative DAG loop, alternatives

Owner: agent **planner-F**. Paths are relative to `Backend/` unless noted. The iOS contract is
`frontend/API_CONTRACT.md` (Planning table + examples `PlanRequest`, `PlanBatch`, `RouteRequest`,
`RouteResult`, `PlanAlternatives`, `CreateItineraryRequest`, `Itinerary`); the Swift models in
`frontend/SideQuestz/Models/Planning.swift` and `Itinerary.swift` are the source of truth for field names.

## 0. What exists today and what is wrong with it

| Area | Current state | Consequence |
|---|---|---|
| Candidate load (`pkg/handlers/planning.go`) | `ListActivities("", …, 15)` from the hard-coded `activities` collection, `_id` order, no city/geo/time filter, tags as `$in`, price as `price.cents` (a field ingested docs do not have) | 15 arbitrary docs; per-user catalog ignored; most windows produce "no_candidates_fit_window" |
| ML rank (`pkg/ml/client.go`) | 10 s client timeout, `Rerank: true` (Jev up to 60 s → always times out → silent `_id` order); random "deterministic" user vectors; dropped events re-appended | Ranking is effectively random |
| DAG (`pkg/itinerary/*`) | Solid and tested (K-best on a DAG, brute-force oracle). One pass, no evaluation loop, 64-series bitmask cap, `restaurant`/`cafe`/`nightclub` places excluded (`nodes.go`) | "Food" can never be satisfied by a place; variety capped |
| Pools (`pkg/handlers/plan_dag.go`) | In-process maps, 2 h TTL | Lost on restart, not shared across workers |
| Request/response shapes | Go expects `start_location:"lat,lng"`, `date`+`start_time` strings, `range_km`, `budget_cents`; emits `title/summary/stops[].name/lat/lng/duration_min` | The app sends `start:{coordinate}`, ISO instants, `range:"transit"`, `budget:1`, and decodes `name/tag/meta/stops[].title/subtitle/place/duration_minutes` |
| `Leg.mode` | Go emits `transit` | App enum is `walk|marta|rideshare|drive|uber` |
| `/plans/alternatives` | Not implemented | Review › Swap is broken on live |
| `POST /itineraries` | Legacy `{option_id, items}` | App sends `{plan, option, stop_order, route}` |
| `ActivityPrice` (`pkg/models/models.go`) | `Tier int`, `IsFree bool` | BSON `null` decodes to `0/false` = "free" |
| Mongo indexes | Backend creates none | No TTL for new collections |

## 1. Architecture and package layout

Keep `pkg/itinerary` pure (nodes → graph → solve → evaluate) with small additive changes. Add one
orchestration package `pkg/planner` that owns request normalisation, retrieval, scoring, the iterative
loop, pool persistence and rendering to the app's shapes. Handlers become thin.

```
pkg/planner/
  config.go        Config (knobs + FromEnv), ScoreWeights, facet tables, city table
  spec.go          PlanSpec, ParsePlanRequest (app shape + legacy shape), budget/range/ride mapping
  mood.go          MoodConstraints: deterministic hard/soft extraction from mood_text + quick-pick tags
  source.go        CandidateSource interface, CandidateQuery, MongoSource (wraps store), FakeSource (tests)
  retrieve.go      Retrieve(): phase A/B, Go-side feasibility (defense in depth), shortlist
  vector.go        query vector blend, dot products, quotas
  scorer.go        Scorer interface (ML classifier ± Jev), PriorScore, fallbacks
  candidate.go     Candidate, Pool (mutex-guarded map), Facets
  loop.go          Run(): rounds, diagnose(), adapt(), expand(), stop rules
  metrics.go       PlanMetrics, Score(), signatures, mergeBest()
  render.go        AppPlanOption/AppPlanStop/AppLeg/… + name/tag/meta/subtitle generators
  pool.go          PlanPool/PlanRun documents, PoolStore interface, cursor encode/decode
  alternatives.go  Alternatives(): slot computation, query, ranking, reason strings
  route.go         Route(): stop resolution (option + alternatives) → itinerary.Evaluate → AppRouteResult
  save.go          BuildItinerary(): pool option + stop_order + route → models.Itinerary + AppItinerary
  *_test.go

pkg/store/planstore.go     Mongo: FindCandidates, FetchEmbeddings, plan_pools, plan_runs
pkg/datastore/indexes.go   EnsureIndexes() (TTL + lookup indexes), called from main after Connect
pkg/ml/client.go           Rank() (no re-append, ctx deadlines), Embed(), SearchProfile(), Vec rounding
pkg/api/planning/          GeneratePlans, GenerateMorePlans, RoutePlan, StopAlternatives (thin adapters)
pkg/itinerary/{config,nodes,hours,solve}.go  Utility hook, 128-bit series mask, place categories, deterministic sort
```

## 2. Data-model changes (`pkg/models/models.go`)

```go
// User (names shared with the embedding workstream)
Catalog string `bson:"catalog,omitempty" json:"-"`            // "activities" (default) | "demo_activities"
City    string `bson:"city,omitempty"    json:"city,omitempty"`
// PositiveEmbedding / NegativeEmbedding / PositiveText / NegativeText exist; all json:"-".

// ActivityPrice: nullable fields nullable (fixes the "null = free" bug)
type ActivityPrice struct {
    Tier     *int     `bson:"tier" json:"tier"`
    Cents    int64    `bson:"cents,omitempty" json:"cents,omitempty"` // legacy seed only
    Min      *float64 `bson:"min,omitempty" json:"min,omitempty"`
    Max      *float64 `bson:"max,omitempty" json:"max,omitempty"`
    Currency string   `bson:"currency" json:"currency"`
    IsFree   *bool    `bson:"isFree" json:"isFree"`
}
func (a *Activity) CostCents() (cents int64, known bool)   // known=false when Price==nil or nothing set
func (a *Activity) PriceTier() (tier int, known bool)       // Tier, else from Min via {0,15,40,80}, else IsFree

// Itinerary additions (persisted; legacy fields kept for readers)
StartAt, BackByAt time.Time; TZ string
StartPlace, EndPlace PlacePoint // {Name string; Lat, Lng float64; HasCoord bool}
PlanRunID, OptionID string
// ItineraryItem additions
Kind string `bson:"kind"`            // "sidequest" | "transit" | "group" | "busy"
ActivityID, Category string; Tags []string; Flexible bool
Description, WebsiteURL, ImageURL string; PriceKnown bool
Leg *LegInfo                          // transit items: {Mode string; Minutes int; DistanceKm float64}
```

Grep for `Price.Tier` / `Price.IsFree` / `Price.Cents` users and switch them to `CostCents()`.

## 3. Request normalisation (`spec.go`)

Accept both the app shape (contract `PlanRequest`) and the legacy shape (`Backend/ITINERARY_PLANNER.md`).
Detect by whether `start` decodes as an object.

```go
type PlanSpec struct {
    UserID, Catalog, City string        // Catalog validated ∈ {"activities","demo_activities"}
    TZ *time.Location                   // catalog city tz > nearest-candidate tz > X-Time-Zone > America/New_York
    Start, End *travel.Point            // nil → user.LastLocation → city default point
    StartName, EndName string
    From, BackBy time.Time              // UTC instants
    LocalDate string                    // "2026-09-26" in TZ
    Mode travel.Mode; DriveLabel string // ResolveMode(ride, modes): drive→Drive/"drive", cover→Drive/"rideshare", none→Transit if modes∩{marta,transit,bus,train,subway} else Walk
    Range string; MaxLegKm float64      // walkable 2 / transit 10 / anywhere 25; Walk mode caps at WalkLegCapKm (4)
    Budget Budget                       // {Tier 0..3; TotalCents int64 (0=unlimited); FreeOnly bool}
    Pace string                         // relaxed→chill, balanced, packed
    Who string; OpenSeats int
    MoodText string; QuickPicks []string
    Facets []Facet                      // from QuickPicks + mood (soft, scored for coverage)
    Hard HardConstraints                // from mood + user: ExcludeCategories, ExcludeTags, RequireTags, FreeOnly
    AgeBracket string                   // user.AgeBracket, default "21_plus"
    AvoidTags []string                  // user.Taste.AvoidTags
    Flexible bool                       // user.Prefs.Flexible: allows the budget relax step
    Raw json.RawMessage                 // logged verbatim in plan_runs
    SnappedStart bool
}
func ParsePlanRequest(raw []byte, tzHeader string, now time.Time, user *models.User, cfg Config) (PlanSpec, error)
```

Mapping tables (`config.go`):
- Budget `0..3` → `{FreeOnly}`, `{Tier 1, Total 2500}`, `{Tier 2, Total 7000}`, `{Tier 3, unlimited}`. Legacy `budget_cents>0` → Total=budget_cents, Tier=3. Per-stop tier enforced in retrieval; the total in `Solve` (`Window.BudgetCents`).
- Quick picks → facets: `Outdoors{tags outdoor,nature; cats park,garden,hike,viewpoint}`, `Food{food; restaurant,cafe,market}`, `Art{art; gallery,museum}`, `Music{music; live_music}`, `Chill{low_energy}`, `Active{active,high_energy; hike,rec_venue,sports_event}`, `Meet people{group; community_event,class_workshop,festival}`, `Nerdy{learning; class_workshop,museum,tour}`, `Nightlife{late_night,drinks; bar,nightclub,live_music,comedy}`. A stop covers a facet when its category ∈ cats or tags ∩ facet.tags ≠ ∅.
- Window: app sends instants; `From = start_time`, `BackBy = back_by` (+1 day if ≤ From); reject if `BackBy ≤ now` (400 `invalid_request: window already ended`).
- Demo snap rule: if `user.City` is set and `Start` is > `CitySnapKm` (60) from that city's centre, replace Start/End with the city's default point and set `SnappedStart` (returned in `PlanBatch.relaxed`). Sandy Byte's phone is in Atlanta while her catalog is Saltlight.

`mood.go` — `ExtractMoodConstraints(mood string, picks []string) (HardConstraints, []Facet)`: deterministic,
logged. Hard rules: negation patterns (`no|not|avoid|without|skip|nothing` + synonym) → exclude
category/tag ("no bars" → cats bar,nightclub + tag drinks; "nothing outdoors" → tag outdoor); `free|broke|no
money` → FreeOnly; `kids|family` → exclude tag 21_plus and cats bar,nightclub; `sober|no drinking` → tag
drinks. Everything else is soft (search embedding + facets). No LLM in v1.

City resolution: static table `cities = {atlanta, seattle, sf, nyc, berlin, saltlight}` with centre, tz,
default start point (from `dataingestion/cities/*.yaml`; saltlight default = Seaside Market Square
31.3680,-81.4250); `NearestCity(p) (city, ok)` within 60 km; `user.City` wins for demo users.

## 4. WP-A: Candidate retrieval with guaranteed filters

### 4.1 Store queries (`pkg/store/planstore.go`)

```go
type CandidateQuery struct {
    Catalog, City string
    Kinds []string                 // "event","place"
    Center travel.Point; RadiusKm float64
    From, To time.Time             // window (or an expansion slot)
    MaxTier int; FreeOnly bool; AllowUnknownPrice bool
    AgeBracket string
    ExcludeCategories, ExcludeTags []string
    IncludeCategories []string     // place categories or a facet's categories
    AnyTags []string               // facet tags
    ExcludeIDs []bson.ObjectID
    LimitEvents, LimitPlaces int
}
func (s *Store) FindCandidates(ctx context.Context, q CandidateQuery) (events, places []models.Activity, err error)
func (s *Store) FetchEmbeddings(ctx context.Context, catalog string, ids []bson.ObjectID) (map[string][]float64, error)
```

Two `Find`s run in parallel, so `{city, kind, start}` serves events and the 2dsphere index serves places.
Radius `radiusKm = clamp(MaxLegKm * (1 + 0.5*pace.MaxStops), 3, 30)`.

Events (fixed_start: `start ∈ [From, To−15m]`; drop_in: overlaps the window):
```js
db.<catalog>.find({
  city: "<city>",                       // omitted when unknown
  kind: "event",
  location: { $geoWithin: { $centerSphere: [[lng, lat], radiusKm / 6378.1] } },
  $or: [
    { attendance: { $ne: "drop_in" }, start: { $gte: From, $lte: To - 15min } },
    { attendance: "drop_in", start: { $lt: To }, $or: [ { end: null }, { end: { $gt: From } } ] }
  ],
  tags: { $nin: ["21_plus", ...avoidTags, ...excludeTags] }, category: { $nin: ["bar","nightclub", ...excludeCats] },
  name: { $not: { $regex: "21\\+", $options: "i" } },          // 13_17 also: "(18\\+|21\\+)"
  <priceClause>
}, { projection: { embedding: 0, "trail.geometry": 0, description: 0, embeddingText: 0, sources: 0, sourceKeys: 0, recurrence: 0 } })
 .sort({ start: 1 }).limit(400)
```
Places:
```js
db.<catalog>.find({
  city, kind: "place",
  location: { $geoWithin: { $centerSphere: [[lng, lat], radiusKm / 6378.1] } },
  category: { $in: <placeCategories ∖ excluded> },
  tags: { $nin: [...] }, <priceClause>
}, sameProjection).sort({ rating: -1, popularity: -1 }).limit(600)
```
Price clause (**null price = unknown, never "free"**):
- `FreeOnly` (budget 0): `{$or: [{"price.isFree": true}, {"price.min": 0}, {"price.tier": 0}, {price: null, category: {$in: ["park","hike","landmark","viewpoint","garden","market","community_event"]}}]}`.
- Tier 1–2: `{$or: [{"price.tier": {$lte: t}}, {"price.min": {$lte: {1:15, 2:40}[t]}}, {"price.tier": null, "price.min": null}, {price: null}]}` — unknown kept, cost counted as 0 with `price_known:false`; the app shows `price_cents: null`.
- Tier 3: none.

Age: `21_plus` → no filter; `18_20` → tag `21_plus`, cats bar/nightclub, name `21+`; `13_17` → additionally
name `18+` and tag `drinks`.

Phase B (`FetchEmbeddings`): `find({_id: {$in: ids}}, {projection: {embedding: 1}})` for the Go-feasible
survivors only (cap `EmbedFetchCap` 800, ordered by prior score if over cap). The catalog name is validated
against an allow-list before it is used as a collection name.

### 4.2 Go-side feasibility (`retrieve.go`)

Re-check everything Mongo cannot express, before any scoring:
- events: fixed_start `start ≥ From && start + max(end−start, p75) ≤ BackBy`; drop_in overlap `≥ p75` (fallback `medianMin`, then 60 min); `end == nil → start + p75Min`.
- places: `itinerary.OpenIntervals(weeklyHours, category, tz, From, BackBy)` yields ≥ `MinDuration` (15 min) inside the window.
- `CostCents()` ≤ `Budget.TotalCents` (when set), `PriceTier()` ≤ `Budget.Tier`, FreeOnly rules.
- age/exclusions exactly as the query.
- distance from Start ≤ `radiusKm`.
- `location` present and not (0,0); duplicates by `_id`; drop add-on listings (name matches `parking|express entry|not a concert ticket`).

`pkg/itinerary/nodes.go`: widen `placeCategories` with `restaurant`, `cafe`, and `bar`/`nightclub` (drop-in
venues with hours); `hours.go` `defaultDailyHours` += `restaurant {11:00–22:00}`, `cafe {07:00–19:00}`;
bars/nightclubs still require real hours. Every drop reason is counted into `plan_runs.counts.drops`.

### 4.3 Query vector and cosine shortlist (`vector.go`)

```go
type QueryVector struct{ Q, Neg []float64; HasNeg bool; Source string } // "user+search" | "user" | "search" | "none"
func BuildQueryVector(user *models.User, searchEmb []float64, cfg Config) QueryVector
//   q = l2norm((1-w)·pos + w·search)  w = SearchWeight 0.6; pos absent → q = search; both absent → Source "none"
func Shortlist(cands []Candidate, qv QueryVector, spec PlanSpec, cfg Config) []Candidate
//   s_i = dot(q, e_i) − λ·max(0, dot(neg, e_i)) + Σ_f 0.05·covers(i,f) ; λ = DislikeLambda 0.5
//   sort by (s desc, id asc); take N = ShortlistN (120) subject to:
//     - quota: ≥ FacetQuota (15) per requested facet if available, ≥ 30 % events if available
//     - cap:   ≤ CategoryCap (25) per category
//   Source "none": s_i = Prior(i) (rating/popularity/facet heuristic) — no random vectors, ever
```
`Prior(i) = 0.5·clamp((rating−3)/2) + 0.2·popularity + 0.3·facetMatch` with nil → 0.4/0.3/0.5.

The search embedding comes from `ml.SearchProfile({mood_text, tags, who, pace, budget, start_time,
back_by, timezone})` (runs in parallel with phase A; skipped when mood text and quick picks are empty).

### 4.4 ML scoring (`scorer.go`, `pkg/ml/client.go`)

```go
type RankInput struct { User UserInput; Events []EventInput; SearchEmbedding []float64; SearchText string; Opts RankingOptions }
type RankOutput struct { Scores map[string]float64; Rerank map[string]float64; ModelVersion string; Reranked bool }
func (c *Client) Rank(ctx context.Context, in RankInput) (RankOutput, error)   // honours ctx deadline; no re-append; ids missing from the response stay dropped
type Vec []float64 // MarshalJSON rounds to 5 decimals
```
`http.Client.Timeout` → 90 s; per-call deadlines come from the context (`MLTimeout` 3 s for the classifier,
`JevTimeout` 45 s).

```go
type Scorer interface { Score(ctx context.Context, user *models.User, cands []Candidate, qv QueryVector, searchText string, jev bool) (ScoreResult, error) }
```
Policy: round 0 and every expansion call the classifier only (`rerank:false`). Positive embedding sent =
`user.PositiveEmbedding`, else the search embedding, else skip ML (the service rejects all-zero positives).
On error/timeout: `raw = 0.6·cosRank + 0.4·prior` and `plan_runs.ml = "fallback:<err>"`. Jev
(`PLANNER_JEV=off|async|sync`, default `async` when the ML service reports `jev: true`): `async` fires one
background `Rank(rerank:true, rerank_top_k:20)` on the final shortlist after the response is sent and writes
`rerank_score`s into `plan_runs.jev` and the pool's candidate cache (used by `/plans/alternatives`).

## 5. WP-B: The iterative DAG loop

### 5.1 Structures (`candidate.go`, `loop.go`)

```go
type Candidate struct {
    Act    models.Activity   // light projection; Embedding filled after phase B
    Facets []string          // "kind:event", "cat:live_music", "tag:outdoor", …
    Cos, Dislike, Prior float64
    ML, Jev *float64
    Source string            // "retrieval" | "expand:uncovered_facet:Food" | "expand:idle_gap" | …
    Round  int
}
func (c *Candidate) Raw() float64   // Jev/4 ▸ ML ▸ 0.6·cosRank+0.4·prior, clamped 0..1

type Pool struct { mu sync.Mutex; byID map[string]*Candidate; order []string }

type Run struct {
    ID string; Spec PlanSpec; Window itinerary.Window; Cfg Config; ItCfg itinerary.Config
    QV QueryVector; Pool *Pool
    Best []ScoredPlan          // deduped, Score-sorted, Diverse-ordered
    Boosts map[string]float64  // facet → utility boost applied in the next solve
    Relaxed []string           // "range", "budget", "mood_soft", "snapped_start"
    Log PlanRun                // persisted
    Deadline time.Time
}
type ScoredPlan struct { It itinerary.Itinerary; Metrics PlanMetrics; Score float64; Signature string /* sorted series keys */ }
```

`itinerary.Config` gets `Utility func(a *models.Activity) float64` (nil → current behaviour) so the planner
supplies `Raw() + boost(facets)` without mutating activities; `sortNodes` gets an `Act.ID`+`Flexible`
tiebreak; `label.series/cats` become a `[2]uint64` mask (`SeriesCap` 96). The brute-force oracle test
(`solve_test.go`) must still pass.

### 5.2 Plan metrics and score (`metrics.go`)

```go
type PlanMetrics struct {
    Fit, Coverage, Variety, PaceFit, TravelShare, IdleShare, BudgetUse float64
    LateRisk bool; Stops int; CoveredFacets, MissingFacets []string
}
// fit        = mean Raw() of stops (raw ML/Jev, never the boosted utility)
// coverage   = |covered ∩ requested| / |requested|  (1 when nothing requested)
// variety    = distinct categories / stops
// paceFit    = 1 − |stops − target| / target   (target: chill 2, balanced 3, packed 4)
// travelShare= TravelMin / (Arrival−Depart) ; idleShare = WaitMin / windowMin ; budgetUse = cost/TotalCents (0 if unlimited)
Score = 0.45·fit + 0.15·coverage + 0.10·variety + 0.10·paceFit + 0.05·(1−travelShare) + 0.05·(1−idleShare)
        − 0.05·[lateRisk] − 0.05·max(0, budgetUse−0.9)·10            (weights = Config.ScoreWeights, clamp 0..1)
```
`mergeBest(best, new, PoolSize)`: union by `Signature` (keep the higher Score), sort by (Score desc,
Signature asc), then `itinerary.Diverse`-style ordering with `Mu` on the Score so pages stay varied.

### 5.3 The loop

```
Generate(ctx, user, spec):
  run := newRun(spec); run.Deadline = now + SoftBudget (5.0 s)
  ── round 0 retrieval (parallel: SearchProfile | FindCandidates events | FindCandidates places)
  feasible := Feasible(spec.Window, events ∪ places)            // §4.2, drop counts logged
  if len(feasible)==0 → try relax ladder step "range" once (radius ×1.5, MaxLegKm ×1.5) → still 0 → return {options:[], reason:"no_candidates_fit_window"}
  embs := FetchEmbeddings(feasible)
  qv := BuildQueryVector(user, searchEmb)
  short := Shortlist(feasible, qv, spec)                          // N=120 with quotas
  scores := Scorer.Score(short, classifier only, 3 s)             // fallback on error
  pool.Add(short with scores, Source="retrieval", Round=0)

  for r := 0; r < Rounds (3); r++ {
      plans := Solve(run, r)              // BuildNodes(cfg.Utility = Raw+Boost) → BuildGraph → Solve(K_r) → Diverse(Mu_r)
      scored := Evaluate(plans, spec)     // PlanMetrics + Score
      prev := top3Sum(run.Best); run.Best = mergeBest(run.Best, scored)
      run.Log.Round(r, counts, timings, top3 signatures + metrics, issues)
      if r == Rounds-1 || now > run.Deadline { break }
      issues := Diagnose(run.Best[:3], spec, run.Window)         // ordered, deduped
      if len(issues)==0 { break }                                 // converged
      adapted := Adapt(run, issues)                               // boosts / K / Mu / relaxations (no I/O)
      added := Expand(ctx, run, issues[:MaxExpansionsPerRound(2)])// parallel Mongo → feasible → embeddings → cosine top-20 each → ONE classifier call → pool
      if added == 0 && !adapted { break }
      if r > 0 && top3Sum(run.Best) − prev < Epsilon (0.02) && added == 0 { break }
  }
  options := Render(run.Best)             // app shapes, "Best match" first
  SavePool(run) ; SaveRun(run) ; if Jev async → go jevRerank(run)
  return PlanBatch{options[:3], cursor "dag_<runID>_3", done: len ≤ 3, planner:"dag", run_id, relaxed}
```

`Diagnose(top3)` → `[]Issue{Kind, Facet, StopRef, Slot{From,To}, Anchor travel.Point, Category}`:

| Kind | Trigger | Adapt (no I/O) | Expand (Mongo query = base filters +) |
|---|---|---|---|
| `no_plans` / `few_plans` | `len(Best) < 3` | ladder: K 16→32 & MaxSlots 12→18; then `range` ×1.5; then budget tier +1 only if `user.Prefs.Flexible`; then drop soft mood facets | re-run round-0 retrieval with the relaxed filters, `ExcludeIDs = pool` |
| `uncovered_facet` | requested facet in no top-3 stop | `Boosts[facet] = 0.10` (cap 0.15 per node) | `IncludeCategories = facet.cats, AnyTags = facet.tags`, both kinds, `Limit 40` |
| `idle_gap` | a wait `> pace.MaxWait/2` in the best plan | — | `Center = midpoint(prev, next)`, `RadiusKm = MaxLegKm`, `From/To = gap`, places open ≥ 30 min in gap, events starting in gap, `Limit 40` |
| `head_gap` / `tail_gap` | first stop starts `> 60 min` after From / plan ends `> 75 min` before BackBy with stops `< MaxStops` | — | same as idle_gap with the head/tail slot |
| `weak_stop` | a stop with `Raw() < 0.45` in the best plan | — | `IncludeCategories = [stop.category]`, `Center = stop`, slot = stop ± 30 min, `Limit 30` |
| `shared_stop` | all top-3 share a series | `Mu += 0.25` | `Center = stop`, slot = stop's slot, `ExcludeCategories += stop.category`, `Limit 40` |
| `under_pace` | best plan stops `< target − 1` | `K = 24` | places open in the uncovered half of the window near Start, `Limit 40` |

Expansion results are merged in a deterministic order (issue index, then `_id`), scored with one classifier
call (2 s), and tagged `Source="expand:<kind>[:facet]"`. Round r+1 re-solves on the whole pool.

### 5.4 Configuration knobs (`config.go`, `FromEnv()`; defaults)

`PLANNER` (auto|dag|legacy; app-shape requests never fall back to the legacy builder), `PLANNER_SOFT_BUDGET_MS`
(5000), `PLANNER_HARD_TIMEOUT_MS` (9000), `PLANNER_ROUNDS` (3), `PLANNER_EPSILON` (0.02),
`PLANNER_SHORTLIST_N` (120), `PLANNER_FACET_QUOTA` (15), `PLANNER_CATEGORY_CAP` (25), `PLANNER_PHASE_A_EVENTS`
(400), `PLANNER_PHASE_A_PLACES` (600), `PLANNER_EMBED_FETCH_CAP` (800), `PLANNER_EXPANSION_LIMIT` (40),
`PLANNER_MAX_EXPANSIONS` (2), `PLANNER_ML_TIMEOUT_MS` (3000), `PLANNER_JEV` (async), `PLANNER_JEV_TIMEOUT_MS`
(45000), `PLANNER_JEV_TOP_K` (20), `PLANNER_SEARCH_WEIGHT` (0.6), `PLANNER_DISLIKE_LAMBDA` (0.5),
`PLANNER_RANGE_KM` (walkable 2, transit 10, anywhere 25), `PLANNER_WALK_LEG_CAP_KM` (4), `PLANNER_CITY_SNAP_KM`
(60), `PLANNER_POOL_TTL_H` (6), `PLANNER_RUN_TTL_H` (72), `PLANNER_SERIES_CAP` (96), `PLANNER_DEBUG` (0;
`?debug=1` echoes round logs).

Time budget (target p50 ≤ 6 s without Jev): parse 20 ms · phase A ∥ search embedding 100–400 ms · phase B
100–250 ms · cosine <5 ms · classifier (120) 300–800 ms · solve ≤ 250 ms · each extra round ≈ 0.6–1.2 s.
Every stage is timed into `plan_runs.timings` and a zerolog line.

### 5.5 Determinism

Same inputs + same scores ⇒ identical output: candidates sorted by `(score desc, _id asc)` before quotas;
`sortNodes` tiebreak by `_id`; expansions merged in fixed order; `mergeBest` tie by signature; Diverse picks
the first maximum; tests inject a fixed clock and an unlimited budget (`Config.Now func() time.Time`).

### 5.6 `plan_runs` document (TTL 72 h)

```js
{ _id: runId, userId, catalog, city, createdAt, expiresAt,
  request: <raw body>, spec: <PlanSpec minus Raw>, tz, snappedStart,
  filters: { radiusKm, window:{from,to}, budget, ageBracket, excludeCategories, excludeTags, moodHard, facets },
  counts: { eventsA, placesA, feasible, drops:{reason:n}, shortlist, ranked, mlDropped },
  shortlist: [{ id, kind, category, cos, dislike, prior, ml, source, round }],
  rounds: [{ round, k, mu, boosts, nodes, edges, itineraries, solveMs, mlMs, mongoMs, top3:[{signature, score, metrics}], issues:[…], expansions:[{kind, facet, fetched, feasible, added}] }],
  ml: { model, mode:"classifier"|"fallback:<err>", jev:{ requested, completedAt, scores:{id:score} } },
  final: { optionIds:[…], relaxed:[…], totalMs },
  outcome: { savedOptionId, savedAt, stopOrder, alternativesUsed:[…] }   // patched by POST /itineraries
}
```

## 6. WP-C: API surface, pools, alternatives, route, save, calendar

### 6.1 Pool persistence (`pool.go`, `planstore.go`, `indexes.go`)

```js
// plan_pools (TTL 6 h)
{ _id: runId, userId, catalog, createdAt, expiresAt,
  window: { from, backBy, tz, start:{lat,lng,name}, end:{…}, mode, driveLabel, maxLegKm, budgetCents, pace },
  spec: <PlanSpec subset needed by alternatives>,
  queryVector: [1024 floats], hasNeg: bool,
  options: [ { id:"<runId>-<n>", name, tag, meta, score, metrics, stops:[StopRecord], legs:[LegRecord], totalCostCents, arrival, depart, lateFlag } ],
  alternatives: { "<stopId>": StopRecord },
  scores: { "<activityId>": { ml, jev } }
}
StopRecord = { id:"stop_<activityId>_<i>" | "alt_<activityId>_<slot>", activityId, name, kind, category, tags, lat, lng, address, arrive, depart, durationMin, flexible, costCents, priceKnown, utility, seriesKey, url, imageUrl, summary }
```
```go
type PoolStore interface {
    SaveRun(ctx, *PlanRun) error; PatchRun(ctx, id string, patch bson.M) error
    SavePool(ctx, *PlanPool) error; GetPool(ctx, runID string) (*PlanPool, error)
    AddAlternatives(ctx, runID string, alts []StopRecord) error
}
```
Cursor: `dag_<runId>_<offset>` (stateless; `/more` returns `options[offset:offset+2]`, `done` when
exhausted; unknown/expired cursor → `{options:[], done:true}`). Option id `<runId>-<n>`.
`datastore.EnsureIndexes()`: `plan_pools{expiresAt} TTL`, `plan_runs{expiresAt} TTL`, `plan_runs{userId,
createdAt}`.

### 6.2 Response field mapping (app shape = required keys; everything else additive)

| App type (required keys) | Additive optional fields |
|---|---|
| `PlanBatch {options, cursor?, done}` | `planner:"dag"`, `run_id`, `reason` (`no_candidates_fit_window` / `no_feasible_itinerary` / `invalid_request: …`, only with empty options), `relaxed:[…]`, `debug` |
| `PlanOption {id, name, tag, meta, stops}` | `summary`, `route_summary`, `legs:[{from_stop_id,to_stop_id,mode,minutes,distance_km}]`, `total_cost_cents`, `cost_known`, `total_duration_min`, `depart_time`, `arrival_time`, `late_flag`, `score`, `metrics{…}` |
| `PlanStop {id, title, subtitle, place{name, coordinate{lat,lng}}, duration_minutes}` | `activity_id`, `kind:"event"|"place"`, `category`, `tags`, `arrive_time`, `depart_time`, `flexible`, `price_cents` (null when unknown), `price_known`, `address`, `website_url`, `image_url`, `utility`, `order` |
| `Leg {mode, minutes}` | `distance_km`, `from_stop_id`, `to_stop_id`. **`mode` ∈ app enum**: Walk→`walk`, Transit→`marta`, Drive→`drive`/`rideshare` — never `transit` |
| `RouteResult {legs, stop_times:[{start,end}], arrival, minutes_late}` | `option_id`, `broken_at` (index in the new order or −1), `late_flag`, `depart`, `total_duration_min`, `stop_times[i].stop_id`, `stop_times[i].flexible` |
| `PlanAlternative {stop: PlanStop, reason}` | `fit_score`, `distance_km`, `slot_shift_min` |
| `Itinerary` (contract) | `host`, `run_id`, `option_id`, `tz` |
| `ItineraryItem` (contract) | `activity_id`, `category`, `flexible`, `leg:{mode,minutes,distance_km}` (transit items), `image_url` |

Generators in `render.go`:
- `name`: `short(s0) + " + " + short(s1)` (+ `" + N more"`); `short` = first 3 words, leading "The" dropped, cut at `:`/` at `/` - `.
- `tag`: first option `"Best match"`; others by rule order, skipping tags already used: `Free` · `Outdoors` · `Nightlife` · `Meet people` · `Chill` · `Packed` · `Active` · `Culture` · fallback `Different vibe`.
- `meta`: `"{Free|~$|~$$|~$$$} · {miles:.1f} mi {walking|by transit|by car|by rideshare} · {n} {marta|rideshare|drive} legs"`.
- `subtitle`: `"{CategoryLabel} · {Free|$|$$|$$$}"` (price omitted when unknown; events add `" · {h:mm PM}"`).

### 6.3 Handlers (`pkg/api/planning`)

- `GeneratePlans`: `ParsePlanRequest(body, X-Time-Zone, now, user)` → `planner.Generate` → `PlanBatch`. 400 with `{"message": …}` for invalid requests.
- `GenerateMorePlans`: decode cursor → `GetPool` → page.
- `RoutePlan`: body = contract `RouteRequest` (legacy accepted); `GetPool` (404 `"This plan expired. Generate again."`) → `resolveStops(pool, option, stop_order)` (option stops ∪ `pool.alternatives`; unknown id → 400 `"Unknown stop <id>"`) → override `Window` with request `start/end/start_time/back_by/ride/modes` when present → `itinerary.Evaluate` → `AppRouteResult`.
- `StopAlternatives`: §6.4.
- `CreateItinerary`: §6.5 (lives in `pkg/api/itineraries`; uses `planner.Service.ResolveStop`).

### 6.4 Alternatives (`alternatives.go`)

1. Resolve `stopOrder` (option ∪ alternatives); locate target `S` and neighbours `prev`/`next` (or Start/End). In-plan activity ids and series keys are excluded.
2. Slot: `[S.arrive − 30m, S.depart + 30m]` clipped to the window; anchor = `S` location (fallback midpoint(prev,next)); reachability: `prev.End + travel(prev→alt) + Buffer ≤ altStart` and `altEnd + travel(alt→next) + Buffer ≤ next.Start` when `next` is fixed (a flexible next may shift ≤ 30 min → `slot_shift_min`).
3. Query: pool's stored spec filters + `Center = anchor, RadiusKm = MaxLegKm` + facet match (`category == S.category` OR `AnyTags = S.tags` with ≥ 2 overlap checked in Go) + `From/To = slot` + `ExcludeIDs = plan` + `Limit 60`; Go feasibility inside the slot; `FetchEmbeddings`; score `dot(pool.queryVector, e) − λ·dislike` plus cached `scores[id]` (`0.7·cos + 0.3·ml`); 3–5 results best first; each gets the earliest feasible grid start and an id `alt_<activityId>_<slotIdx>`.
4. `reason` = `"Also {phrase} · {miles:.1f} mi away"` (+ `" · Free"`/`" · $$"` when the tier differs, `" · starts 8:30 PM"` for events). Phrase table: gallery/museum → "art to see"; park/garden/hike/viewpoint → "time outside"; restaurant/cafe/market → "good food"; bar → "drinks"; nightclub → "late-night music"; live_music → "live music"; comedy → "laughs"; rec_venue → "games"; class_workshop → "something hands-on"; tour/landmark → "sights"; community_event/festival → "people to meet"; default → "a similar vibe".
5. `AddAlternatives(pool, records)`.

### 6.5 Save (`save.go`)

Body = contract `CreateItineraryRequest {plan, option, stop_order, route, visibility, lock_at?, max_group_size?}`.
1. `GetPool(option.id)`; resolve `stop_order` → StopRecords (option ∪ alternatives). If the pool expired, fall back to `option.stops` from the body so saving never fails; log `pool_missing`.
2. Times: `route.stop_times[i]` when present and consistent, else re-`Evaluate`.
3. Items (uuid v7 ids): for each stop `i`: a `transit` item for `route.legs[i]` (`title:"{Walk|MARTA|Rideshare|Drive} to {stop.title}"`, `place.name:"{from} → {to}"`, `start` = previous end, `end = start + minutes`, `description:"Route options below. Times update live if you run late."`, `leg{mode,minutes,distance_km}`), then the `sidequest` item (`title`, `place{name, coordinate}`, `start/end`, `description` = summary ▸ subtitle, `website_url`, `bookable` = has ticket URL/price, `price_cents` = cost or null, `activity_id`, `category`, `flexible`); final transit item for the last leg (`end` = arrival).
4. Persist `models.Itinerary` (+ `StartAt/BackByAt`, places, `PlanRunID/OptionID`, `Kind` on items). `PatchRun(outcome)`.
5. Respond the contract `Itinerary` (`date` = local day, `going_count`, `is_host`).

### 6.6 Calendar days

Return the contract's bare array: `[{id:"YYYY-MM-DD", date, items:[CalendarItem{id, kind, title, start, end, itinerary_id}]}]` where items are the viewer's active itineraries' `sidequest` (and `group`) items on that local day (X-Time-Zone). No fabricated busy blocks.

## 7. Guarantees checklist (asserted for every returned option)

Every stop: `arrive ≥ From`, `depart ≤ BackBy` (fixed events: `start + max(published, p75) ≤ BackBy` or `late_flag`); Σ known costs ≤ `Budget.TotalCents`; per-stop tier ≤ `Budget.Tier`; FreeOnly ⇒ all stops known free; no `21_plus` tag / bar / nightclub for non-`21_plus` users; every inter-stop leg ≤ `MaxLegKm` and depot legs ≤ 2×; no excluded category/tag/avoid tag; every `activity_id` belongs to the user's catalog; no series repeated; one stop per category; legs = stops + 1 with `start`/`end` ids; `mode` ∈ app enum.

## 8. Tests

Unit (`go test ./pkg/...`, fixtures `pkg/itinerary/testdata/atlanta_2026-09-26.json`, `nyc_2026-09-26.json`):
- `spec_test.go`: app-shape parsing (ISO instants + `X-Time-Zone`, range/ride/budget/pace/modes mapping, missing coordinate fallbacks, back_by ≤ start → next day, expired window → error), legacy parity, snap rule.
- `mood_test.go`: negations → hard excludes; "free" → FreeOnly; "family" → no 21_plus; unrelated text → nothing.
- `retrieve_test.go`: `FakeSource` on the fixtures plus injected violators → `Feasible` drops each with the right reason; shortlist quotas/caps; `Source:"none"` uses priors.
- `guarantees_test.go`: full `Generate` over both fixtures × {walk/transit/drive} × {budget 0..3} × {13_17, 18_20, 21_plus} × quick picks; assert §7 on every option (all pages), with a fake scorer that "drops" ids to prove nothing is re-appended.
- `loop_test.go`: a `FakeSource` whose expansion responses contain a "Food" candidate absent from round 0 → `uncovered_facet:Food`, round 1 covers it, `plan_runs.rounds` has 2 entries; stop rules; relax ladder logs `relaxed:["range"]`; Σ top-3 Score non-decreasing.
- `determinism_test.go`: same inputs twice → identical signatures, scores, ids, JSON.
- `render_test.go` + `route_test.go` + `save_test.go`: golden JSON for `PlanBatch`, `RouteResult`, `PlanAlternative`, `Itinerary`.
- `metrics_test.go`; `pkg/itinerary`: existing suite + `TestSeriesCapAbove64` + `TestRestaurantPlaces` + sort tiebreak; `pkg/ml/client_test.go`: deadlines, no re-append, `Vec` rounding, `Embed`/`SearchProfile` contract.

Integration (`//go:build integration`, `MONGO_URI=mongodb://127.0.0.1:27017 MONGO_DB=freetime_test`; the local
Docker Mongo already holds `demo_activities` (100, with vectors) and a 400-doc Atlanta sample in `freetime`):
seeds `demo_activities` from `dataingestion/demo/saltlight_harbor.json` when needed, creates indexes, inserts
Sandy Byte (`catalog:"demo_activities"`, `city:"saltlight"`, `ageBracket` variants). Asserts: `FindCandidates`
for Sat 2026-09-26 18:00–23:00 walkable from Seaside Market returns only in-window/in-radius docs; the age
variant excludes 21+ events; budget 0 excludes paid events; `Generate` end to end meets §7; `plan_pools`
round-trips through a new `Store` instance for `/more`, `/route`, `/alternatives`, `/itineraries`;
alternatives never repeat in-plan ids and fit the slot; TTL indexes exist.

## 9. Risks

1. User embeddings absent at launch → mood-only or prior-only scoring (logged in `plan_runs.ml.mode`).
2. Demo geography → snap rule (§3) plus the app's home base.
3. ML latency/payload: 120 × 1024-d vectors per call (~1 MB with rounding); `ShortlistN` 80 if needed.
4. Phase B fetch (≤ 800 docs ≈ 10 MB) is the largest I/O; cap and measure.
5. Widened place categories change behaviour; bars/nightclubs still need real hours.
6. Series mask widening touches the solver's hot loop; keep the oracle green.
7. Weekly-hours day-0 = Sunday unverified for Google data; the demo generator is consistent.

## 10. Work packages

Day 0 (shared): the interfaces + fakes (`PlanSpec`, `Candidate`, `CandidateSource`/`CandidateQuery`,
`Scorer`, `PoolStore`, `PlanRun/PlanPool`, `models.go` changes, `Config`; `FakeSource`, `FakeScorer`,
`MemPoolStore`).
- **WP-A Retrieval & scoring**: `planstore.go`, `retrieve.go`, `vector.go`, `mood.go`, `scorer.go`, `ml/client.go`, `itinerary/nodes.go` + `hours.go`, `datastore/indexes.go`.
- **WP-B Loop & metrics**: `loop.go`, `metrics.go`, `candidate.go`, `itinerary/config.go` Utility hook, `solve.go` mask, `plan_runs` logging.
- **WP-C API, pools, alternatives, route, save, calendar**: `spec.go`, `render.go`, `pool.go`, `alternatives.go`, `route.go`, `save.go`, handlers, calendar days.
Order: A → B → C → integration test → docs (`docs/PLANNER.md`) → iOS optional decoding (done by ios-3).
