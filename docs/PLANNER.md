# The planner

How `POST /plans/generate` turns a free window into three timed plans, what every plan guarantees, and
how to look inside a run. Code: `Backend/pkg/planner` (orchestration) over the pure DAG solver in
`Backend/pkg/itinerary` and the travel estimates in `Backend/pkg/travel`. Design:
[design/planner.md](design/planner.md).

## What every option guarantees

These are asserted in tests for every returned option, on every page:

| Guarantee | Detail |
|---|---|
| Inside the window | every stop arrives at or after the start time and departs by the back-by time; a fixed-start event fits its published length (or the p75 estimate), or the option carries `late_flag` |
| Feasible in time | stops never overlap; travel between stops is accounted for; buffer of 10 minutes before each start; the user is back at the end place by back-by |
| Within range | every leg between stops is at most the range's maximum (walkable 2 km, transit 10 km, anywhere 25 km; walking legs are capped at 4 km); the legs from the start and to the end may be twice that |
| Within budget | known costs add up to at most the budget's total; each stop's price tier is at most the budget's tier; "Free" means every stop is known to be free. Unknown prices are never treated as free: they are kept for tiers 1–3, counted as 0 and shown as `price_cents: null` |
| Age-appropriate | no `21_plus` tag, bar or nightclub for users under 21; under 18 also excludes `18+` names and the `drinks` tag |
| Respects the mood | excluded categories and tags from the mood text ("no bars", "nothing outdoors", "sober") and the user's avoid tags never appear |
| Varied | no venue or recurring series twice, at most one stop per category |
| Real | every `activity_id` belongs to the caller's catalog; every stop has coordinates |
| Well-formed | `legs` has one entry more than `stops` (from `start`, to `end`); leg `mode` is one of the app's `walk`, `marta`, `rideshare`, `drive`, `uber`, never `transit` |

Events keep their real start times. Places and drop-in events are scheduled on a 30-minute grid inside
their opening hours (`flexible: true`).

## Request mapping

The app sends the contract `PlanRequest`; the legacy shape from the first backend (`start_location:
"lat,lng"`, `date` + `start_time` + `back_by_time` strings, `range_km`, `budget_cents`) is still accepted
and detected by whether `start` decodes as an object.

| App field | Becomes |
|---|---|
| `start`, `end` (`Place`) | start and end points; a missing coordinate falls back to the user's last location, then the city's default point. Demo snap rule: a start more than 60 km from the user's `city` centre is replaced by that city's default point (`relaxed: ["snapped_start"]`) |
| `date`, `start_time`, `back_by` (instants) | the window `[From, BackBy]` in UTC; back-by at or before start means the next day; a window that already ended is `400 invalid_request` |
| `X-Time-Zone` | the local day for `plan_runs.tz` and rendering; the catalog city's zone wins when known |
| `range` | `walkable` → 2 km, `transit` → 10 km, `anywhere` → 25 km per leg; the retrieval radius is `clamp(maxLegKm × (1 + 0.5 × maxStops), 3, 30)` km |
| `ride`, `modes` | `drive` → driving legs labelled Drive; `cover` → driving legs labelled Rideshare; `none` → transit when `modes` includes `marta`, `transit`, `bus`, `train` or `subway`, else walking. Legs of 0.8 km or less are always walked |
| `budget` 0–3 | Free only; tier 1 with a $25 total; tier 2 with a $70 total; tier 3 unlimited. Legacy `budget_cents` → total with tier 3 |
| `pace` | `relaxed`/`chill` → 2 stops, 90 min max idle; `balanced` → 3, 60 min; `packed` → 5 (target 4), 30 min |
| `tags` (quick picks) | facets scored for coverage: Outdoors, Food, Art, Music, Chill, Active, Meet people, Nerdy, Nightlife; each is a set of categories and tags a stop can cover |
| `mood_text` | deterministic rules, no LLM: negations become hard exclusions ("no bars" → bar, nightclub, drinks), `free`/`broke` → free only, `kids`/`family` → no 21+ or bars, `sober` → no drinks; everything else is soft and goes into the search embedding |
| `who`, `open_seats` | passed to the search text; the plan itself does not change |
| user record | age bracket (default 21+), avoid tags from the taste profile, `flexible` (allows the budget relax step), catalog and city |

The raw body is logged verbatim in `plan_runs.request`.

## Retrieval

**Phase A, Mongo, guaranteed filters.** Two queries run in parallel on the caller's catalog. Events use
the `{city, kind, start}` index: `fixed_start` events start inside `[From, To − 15 min]`, `drop_in` events
overlap the window. Places use the 2dsphere index within the radius, restricted to the place categories
(parks, gardens, hikes, viewpoints, museums, galleries, landmarks, markets, rec venues, restaurants,
cafés, and bars or nightclubs with real hours). Both apply the tag and category exclusions, the age
rules and the price clause. Nothing heavy (vectors, trail geometry, descriptions) is projected. Caps: 400
events, 600 places.

**Go-side feasibility.** Before any scoring, everything Mongo cannot express is re-checked: an event's
length fits before back-by; a place's `weeklyHours` give at least 15 minutes inside the window (default
hours for restaurants 11–22 and cafés 7–19; bars and nightclubs must have real hours); `CostCents()` and
`PriceTier()` against the budget (a `null` price is unknown, not free); distance from the start; a valid
location; duplicates; add-on listings (parking, express entry, "not a concert ticket"). Every drop reason
is counted into `plan_runs.counts.drops`.

**Phase B, vectors.** `FetchEmbeddings` loads the 1024-d vectors of the survivors only (cap 800, ordered
by prior score).

**Query vector and shortlist.** `q = normalize((1 − w)·positiveEmbedding + w·searchEmbedding)` with
`w = 0.6`; without a profile the search vector alone is used; without either, the prior alone (no random
vectors, ever). Each candidate scores `dot(q, e) − 0.5·max(0, dot(negative, e)) + 0.05 per covered
facet`, and the shortlist takes the top 120 subject to at least 15 per requested facet, at least 30 %
events when available, and at most 25 per category. The prior is `0.5·clamp((rating − 3)/2) +
0.2·popularity + 0.3·facetMatch`.

**ML scoring.** One call to `POST /v1/events/rank` with the stored user vectors, the search embedding
and text, and the shortlist's vectors (classifier only, 3 s). Ids the service drops stay dropped. On
error or timeout the raw score is `0.6·cosineRank + 0.4·prior` and `plan_runs.ml.mode` says
`fallback:<err>`. When the ML service reports `jev: true`, one background Jev rerank of the final
shortlist (`rerank_top_k: 20`) is written into `plan_runs.ml.jev` and the pool's score cache after the
response is sent (`PLANNER_JEV=off|async|sync`).

## The loop

Round 0 solves on the retrieved pool. Each following round looks at the best three plans, names what is
wrong with them, adapts the solver, fetches targeted candidates, and solves again on the whole pool.

```
Generate:
  retrieve → feasible → embeddings → shortlist → score → pool (round 0)
  for r in 0..2:
      plans  = BuildNodes(utility = raw + boosts) → BuildGraph → Solve(K) → Diverse(Mu)
      scored = Evaluate(plans)              # metrics + Score
      best   = mergeBest(best, scored)      # union by signature, Score desc, diverse order
      stop when: last round · past the 5 s soft budget · no issues · nothing added and nothing adapted
                 · top-3 improved by < 0.02 with nothing added
      issues = Diagnose(best[:3]); Adapt(issues); Expand(issues[:2])
  render → save pool + run → respond
```

| Issue | Trigger | Adapt (no I/O) | Expand (targeted Mongo query) |
|---|---|---|---|
| `no_plans` / `few_plans` | fewer than 3 plans | K 16→32 and 12→18 slots; then range ×1.5; then budget tier +1 if the user is flexible; then drop soft mood facets | round-0 retrieval with the relaxed filters, excluding the pool |
| `uncovered_facet` | a requested facet in none of the top-3 stops | boost that facet's utility by 0.10 (≤ 0.15 per node) | that facet's categories and tags, 40 candidates |
| `idle_gap` | a wait longer than half the pace's max idle in the best plan | — | places open ≥ 30 min and events starting in the gap, centred between the neighbours |
| `head_gap` / `tail_gap` | the plan starts > 60 min late or ends > 75 min early with room for more stops | — | the same, for the head or tail slot |
| `weak_stop` | a stop with raw score < 0.45 | — | same category near that stop, ± 30 min, 30 candidates |
| `shared_stop` | all three plans share a series | Mu += 0.25 | that slot, excluding the shared category |
| `under_pace` | the best plan has fewer stops than target − 1 | K = 24 | places open in the uncovered half of the window near the start |

**Score.** Each plan's metrics are fit (mean raw score of the stops), coverage (requested facets
covered), variety (distinct categories / stops), pace fit, travel share, idle share, budget use and a
late-risk flag:

```
Score = 0.45·fit + 0.15·coverage + 0.10·variety + 0.10·paceFit + 0.05·(1 − travelShare) + 0.05·(1 − idleShare)
        − 0.05·[lateRisk] − 0.05·max(0, budgetUse − 0.9)·10        clamped to 0..1
```

**Determinism.** The same inputs and scores give byte-identical output: candidates are sorted by
`(score desc, _id asc)` before quotas, the solver breaks ties by id, expansions merge in a fixed order and
the diverse pass picks the first maximum. Tests inject a fixed clock.

**Time budget.** Target p50 ≤ 6 s without Jev: phase A in parallel with the search embedding 100–400 ms,
phase B 100–250 ms, classifier 300–800 ms, solve ≤ 250 ms, each extra round 0.6–1.2 s
(`PLANNER_SOFT_BUDGET_MS` 5000, `PLANNER_HARD_TIMEOUT_MS` 9000). Every stage is timed into
`plan_runs.timings`. Measured on the VPS: p50 and p95 to be filled in after deploy. <!-- verify-after-deploy -->

## Outputs

The response is the app's `PlanBatch`; everything beyond the required keys is additive and optional.

| App type (required keys) | Additive fields |
|---|---|
| `PlanBatch {options, cursor?, done}` | `planner: "dag"`, `run_id`, `reason` (only with empty `options`: `no_candidates_fit_window`, `no_feasible_itinerary`, `invalid_request: …`), `relaxed` (`range`, `budget`, `mood_soft`, `snapped_start`), `debug` (with `?debug=1` and `PLANNER_DEBUG=1`) |
| `PlanOption {id, name, tag, meta, stops}` | `summary`, `route_summary`, `legs [{from_stop_id, to_stop_id, mode, minutes, distance_km}]`, `total_cost_cents`, `cost_known`, `total_duration_min`, `depart_time`, `arrival_time`, `late_flag`, `score`, `metrics` |
| `PlanStop {id, title, subtitle, place {name, coordinate}, duration_minutes}` | `activity_id`, `kind` (`event` or `place`), `category`, `tags`, `arrive_time`, `depart_time`, `flexible`, `price_cents` (null when unknown), `price_known`, `address`, `website_url`, `image_url`, `utility`, `order` |
| `Leg {mode, minutes}` | `distance_km`, `from_stop_id`, `to_stop_id` |
| `RouteResult {legs, stop_times [{start, end}], arrival, minutes_late}` | `option_id`, `broken_at` (index in the new order of the first fixed start reached late, or −1), `late_flag`, `depart`, `total_duration_min`, `stop_times[i].stop_id`, `stop_times[i].flexible` |
| `PlanAlternative {stop, reason}` | `fit_score`, `distance_km`, `slot_shift_min` |
| `Itinerary` / `ItineraryItem` | `host`, `run_id`, `option_id`, `tz`; items add `activity_id`, `category`, `flexible`, `leg {mode, minutes, distance_km}` on transit items, `image_url` |

The app shows `late_flag` as a "Tight timing" chip, `broken_at` as "Some stops would be late" after a
reorder, and `reason` as the empty-state copy.

Generated text: `name` is the first two stops shortened ("Pier Nine + Kelp Hollow", "+ N more");
`tag` is "Best match" for the first option and then, in order and without repeats, Free, Outdoors,
Nightlife, Meet people, Chill, Packed, Active, Culture, or "Different vibe"; `meta` is
`"{Free|~$|~$$|~$$$} · {miles} mi {walking|by transit|by car|by rideshare} · {n} {marta|rideshare|drive} legs"`;
a stop's `subtitle` is `"{Category} · {Free|$|$$|$$$}"` plus the start time for events.

## Follow-up calls

| Call | What happens |
|---|---|
| `POST /plans/generate/more {cursor}` | the cursor is `dag_<runId>_<offset>`; the pool (`plan_pools`, 6 h TTL, shared across workers and restarts) returns the next two options and `done` when exhausted. An unknown or expired cursor returns `{options: [], done: true}` |
| `POST /plans/route` | resolves `stop_order` against the option's stops and any alternatives already returned for it (unknown id → 400 "Unknown stop …"; expired pool → 404 "This plan expired. Generate again."), overrides the window with the request's start, end, times, ride and modes, and re-evaluates: legs, `stop_times`, `arrival`, `minutes_late`, `broken_at` |
| `POST /plans/alternatives` | 3–5 stops for the same slot (± 30 min), same category or ≥ 2 shared tags, reachable from the neighbours, never already in the plan; scored `0.7·cosine + 0.3·ML` against the pool's stored query vector; `reason` like "Also live music · 0.4 mi away · Free". They are stored in the pool so `route` and `POST /itineraries` know them |
| `POST /itineraries` | builds the itinerary from the pool (or from the body's `option.stops` if the pool expired): for each stop a `transit` item ("Walk to …", "MARTA to …", "Rideshare to …", "Drive to …") then the `sidequest` item with the activity's id, price, website and bookability; the run's `outcome` records what was saved |
| `GET /calendar/days` | the saved items on each local day; no fabricated busy blocks |

## Knobs

All read from the environment by `pkg/planner` (`FromEnv`), with these defaults:

| Variable | Default | Meaning |
|---|---|---|
| `PLANNER` | `auto` | `auto`, `dag` or `legacy`; app-shape requests never fall back to the legacy builder (the VPS runs `dag`) |
| `PLANNER_SOFT_BUDGET_MS` / `PLANNER_HARD_TIMEOUT_MS` | 5000 / 9000 | stop expanding after the soft budget; give up at the hard timeout |
| `PLANNER_ROUNDS` / `PLANNER_EPSILON` | 3 / 0.02 | loop length and the minimum top-3 improvement to continue |
| `PLANNER_SHORTLIST_N` / `PLANNER_FACET_QUOTA` / `PLANNER_CATEGORY_CAP` | 120 / 15 / 25 | the cosine shortlist |
| `PLANNER_PHASE_A_EVENTS` / `PLANNER_PHASE_A_PLACES` / `PLANNER_EMBED_FETCH_CAP` | 400 / 600 / 800 | retrieval caps |
| `PLANNER_EXPANSION_LIMIT` / `PLANNER_MAX_EXPANSIONS` | 40 / 2 | candidates per expansion query, expansions per round |
| `PLANNER_ML_TIMEOUT_MS` | 3000 | classifier deadline per call |
| `PLANNER_JEV` / `PLANNER_JEV_TIMEOUT_MS` / `PLANNER_JEV_TOP_K` | `async` / 45000 / 20 | the optional LLM rerank |
| `PLANNER_SEARCH_WEIGHT` / `PLANNER_DISLIKE_LAMBDA` | 0.6 / 0.5 | query-vector blend and the dislike penalty |
| `PLANNER_RANGE_KM` | `walkable 2, transit 10, anywhere 25` | per-leg maxima |
| `PLANNER_WALK_LEG_CAP_KM` / `PLANNER_CITY_SNAP_KM` | 4 / 60 | walking cap; the demo snap rule |
| `PLANNER_POOL_TTL_H` / `PLANNER_RUN_TTL_H` | 6 / 72 | Mongo TTLs of `plan_pools` and `plan_runs` |
| `PLANNER_SERIES_CAP` | 96 | distinct series the solver's label mask can track |
| `PLANNER_DEBUG` | 0 | `?debug=1` echoes the round logs in `PlanBatch.debug` |

The solver's own tunables (`pkg/itinerary.DefaultConfig`): score baseline 0.3, travel penalty 0.005 per
minute, 10-minute buffer, K = 16 partial paths per node, pool of 64 itineraries, diversity penalty 0.5,
30-minute slot grid with up to 12 slots per flexible activity, stays between 15 minutes and 6 hours.

## Inspecting a run

Every generation writes a `plan_runs` document (72 h TTL) with the raw request, the parsed spec, the
filters, drop counts, the shortlist with every score component, one entry per round (K, Mu, boosts, node
and edge counts, timings, the top-3 signatures and metrics, issues, expansions), the ML mode, the final
option ids and `relaxed`, and later the saved outcome. Locally the database is in the `sq-mongo`
container, so prefix the queries with `docker exec sq-mongo mongosh --quiet freetime --eval '…'`; on the
VPS run `mongosh freetime` directly.

```js
// 1. The latest run for a user: what was asked, what came back, how long it took
db.plan_runs.find({userId: "<user id>"}, {createdAt: 1, tz: 1, "filters.window": 1, "final": 1, "ml.mode": 1})
  .sort({createdAt: -1}).limit(1)

// 2. Why candidates disappeared: phase A counts, feasibility drops by reason, shortlist size
db.plan_runs.findOne({_id: "<run id>"}, {counts: 1, "filters.radiusKm": 1, "filters.budget": 1, "filters.moodHard": 1})

// 3. What the loop did: per-round issues, expansions and the top-3 scores
db.plan_runs.findOne({_id: "<run id>"}, {"rounds.round": 1, "rounds.issues": 1, "rounds.expansions": 1,
  "rounds.top3.score": 1, "rounds.solveMs": 1, "rounds.mlMs": 1, "rounds.mongoMs": 1})
```

`db.plan_pools.findOne({_id: "<run id>"})` shows the rendered options, their stop records and any
alternatives handed out; `?debug=1` on the request returns the same round log inline when
`PLANNER_DEBUG=1`.

## Limitations

- **Travel times are estimates**, not routes: straight-line distance × 1.3, at 4.5 km/h walking, 20 km/h
  plus 5 minutes for transit, 30 km/h plus 5 minutes for driving; legs of 0.8 km or less are always
  walked. A routing provider plugs in behind `travel.Provider` ([roadmap](ROADMAP.md)).
- **Mood parsing is rule-based.** Negations, "free", "family" and "sober" become hard constraints;
  everything else only shapes the search embedding.
- **Opening hours** assume day 0 of `weeklyHours` is Sunday (Google's convention), unverified against
  live Google data; the demo generator follows the same convention. Restaurants and cafés without hours
  get defaults; bars and nightclubs without hours are never scheduled.
- **Scoring degrades gracefully but silently for the user**: a missing user profile means mood-only or
  prior-only ranking (`plan_runs.ml.mode` says which); an ML timeout means the cosine/prior fallback.
- **One process per solve.** The pool is shared through Mongo, but each run's loop lives in one request;
  a Cloudflare request cap of 100 s bounds the hard timeout.
- **Series and categories** are limited to 96 distinct series per run and one stop per category, so a
  "bar crawl" is not something the planner will produce.
