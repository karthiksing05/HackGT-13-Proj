# The planner

How `POST /plans/generate` turns a free window into three timed plans, what every plan guarantees, and
how to look inside a run. Code: `Backend/pkg/planner` (orchestration) over the pure DAG solver in
`Backend/pkg/itinerary` and the travel estimates in `Backend/pkg/travel`; `Backend/pkg/api/planning` is
the thin HTTP layer. Design: [design/planner.md](design/planner.md).

## What every option guarantees

Every rendered option is checked against these rules before it is shown (an option that fails is left
out and counted in `plan_runs.final.rejected`, which stays 0 unless there is a bug), and the tests assert
them on every page. Must-see picks add one, "contains every pick", and are exempt from the rules they
bypass ([Must-see picks](#must-see-picks)):

| Guarantee | Detail |
|---|---|
| Inside the window | every stop arrives at or after the start time and departs by the back-by time |
| Events attended properly | an event attended whole fits its published length (or its p75 estimate) before back-by, or the option carries `late_flag`; an event that may be joined late (see [Drop-in stays](#drop-in-stays)) is joined at its start or up to 15 minutes after, left by its end, and visited for at least min(60 min, half the event) |
| Feasible in time | stops never overlap; travel between stops is accounted for, with a 10-minute buffer before each start; the user is back at the end place by back-by |
| Around the calendar | no stop, stay or leg is on top of a busy block, the user's `calendar_events` around the window (read by `mongosource`, kept in the pool for routes, alternatives and the save): a leg into a stop sets off when the previous stop ends or after the last block before the stop, the way back at the first free moment, and time in a block is not idle time. A window the blocks leave no 15-minute stretch in is an empty batch with `reason: calendar_full` |
| Within range | every leg, the first one from the start and the last one back to the end included, is at most the range's maximum: walkable 2 km, transit 10 km, anywhere 25 km, and walking legs at most 4 km |
| Within budget | known costs add up to at most the budget's total; each stop's price tier is at most the budget's tier; a free plan has only stops known to be free, or with no price at all in a normally free category (park, hike, landmark, viewpoint, garden, market, community event). An unknown price is never treated as free: it is kept for budgets $–$$$, counted as 0 and sent as no price |
| Age-appropriate | no `21_plus` tag, bar, brewery or nightclub for users under 21; under 18 also excludes `18+` names and the `drinks` tag |
| Respects the mood | categories and tags excluded by the mood text ("no bars", "nothing outdoors", "sober") and the user's avoided tags (which also match categories) never appear |
| Varied | no venue, exhibition or recurring series twice, and at most one stop per category (so at most one restaurant and one café; `other` is exempt) |
| Real | every `activity_id` belongs to the caller's catalog; every stop has coordinates |
| Well-formed | `legs` has one entry more than `stops` (from `start`, to `end`); a leg's `mode` is one of the app's `walk`, `marta`, `rideshare`, `drive`, never `transit` (the planner never produces `uber`) |

Places and drop-in events are scheduled inside their opening hours (`flexible: true`) at a few candidate
starts: the earliest arrival from the start, the latest start that still gets home by back-by, each
opening, and the :00 and :30 grid. A place may also be visited in a short form (half its usual length,
between 15 and 30 minutes); tours and restaurants need at least three quarters of their length.

## Request mapping

The app sends the contract `PlanRequest`; the legacy shape from the first backend (`start_location:
"lat,lng"`, `date` + `start_time` + `back_by_time` strings, `range_km`, `budget_cents`) is still accepted
and detected by whether `start` decodes as an object.

| App field | Becomes |
|---|---|
| `start`, `end` (`Place`) | the start is the request's coordinate, else the user's home base, else their last known location, else the city's default point; the end is the request's, else the start. Snap rule: when the user has a `city` and the start is more than 60 km from its centre, the plan starts and ends at the home base (or, without one in that city, the city's default point) and the run records `snappedStart` |
| `start_time`, `back_by` (instants) | the window in UTC; back-by at or before the start means the next day |
| `X-Time-Zone` | the plan's local day when the catalog city's zone is unknown (then `America/New_York`) |
| `range` | `walkable` → 2 km, `transit` → 10 km, `anywhere` → 25 km per leg (walking capped at 4 km). The retrieval radius is `maxLeg × ceil(maxStops / 2) + ½ × distance(start, end)`, clamped to 3–30 km |
| `ride`, `modes` | `drive` → driving legs labelled Drive; `cover` → driving legs labelled Rideshare; `none` → transit when `modes` includes `marta`, `transit`, `bus`, `train` or `subway`, else walking. Legs of 0.8 km or less are always walked |
| `budget` 0–3 | Free only; tier 1 with a $25 total; tier 2 with a $70 total; tier 3 unlimited. Legacy `budget_cents` → that total with tier 3 |
| `pace` | the request's, else the user's usual pace, else balanced. `relaxed` (or `chill`): up to 3 stops, aiming for 2, 90 min longest idle gap, stop bonus 0.02; `balanced`: 3 stops, 60 min, 0.05; `packed`: up to 5, aiming for 4, 30 min, 0.08 |
| `tags` (quick picks) | facets scored for coverage: Outdoors, Food, Art, Music, Chill, Active, Meet people, Nerdy, Nightlife; each is a set of categories and tags a stop can cover. Other words for them work too ("Live music" is Music, "outside" Outdoors, "bars" Nightlife) |
| `mood_text` | deterministic rules, no model: a negation with up to two words in between ("no loud bars", "nothing too outdoorsy") excludes that thing's categories and tags; `free`, `broke`, `no money` → free only; `kids`, `family` → no 21+, bars, breweries or nightclubs; `sober` → no drinks, bars, breweries or nightclubs. Positive words add soft facets ("by the water" → Outdoors); everything shapes the search embedding |
| `who`, `open_seats` | go into the search text; the plan itself does not change |
| `must_include` | the must-see picks, catalog ids every option visits ([Must-see picks](#must-see-picks)); repeats count once, more than 10 is `400` "Pick up to 10 must-see spots." |
| user record | age bracket (adult → 21+, under 21 → 18–20, else 13–17), avoided tags, `flexible` (allows the budget relax step), catalog, city and home base |

Requests the planner cannot serve are not errors: a window that already ended or missing times give
`200 {"options": [], "done": true, "reason": "invalid_request: window already ended"}` (or `…: start_time
and back_by are required`), which the app shows as "Check your start, end and times, then try again.".
An enum value outside the app's choices is `400` with that sentence. Without a planner (it failed to
start) every `/plans/*` route answers `503` "Planning is warming up. Try again in a moment.". The raw
body is logged verbatim in `plan_runs.request`.

## Retrieval

**Phase A, Mongo, guaranteed filters.** Two queries run in parallel on the caller's catalog. Events use
the `{city, kind, start}` index: fixed-start events that start between 15 minutes before the window and 15
minutes before its end (the early ones are kept only if they may be joined late), and drop-in events that
overlap the window. Places use the 2dsphere index within the radius, restricted to the categories the
solver schedules: park, garden, hike, viewpoint, landmark, museum, gallery, shopping, rec venue, zoo or
aquarium, market, tour, restaurant, café, bakery, dessert, food hall, bar, brewery and nightclub; and to a
rating of 4 or more (unrated hiking trails are kept). Both apply the tag and category exclusions, the
age rules and the price clause. Nothing heavy (vectors, trail geometry, descriptions) is projected. Caps:
400 events, 600 places.

**Go-side feasibility.** Before any scoring, everything Mongo cannot express is re-checked: an event's
stay fits the window; a place is open for at least 15 minutes inside it; cost and price tier against the
budget (a `null` price is unknown, not free); distance from the start; a valid location; duplicates;
add-on listings (parking, express entry, "not a concert ticket"). Places without `weeklyHours` get default
hours for their category (parks 6–22, hikes 6–20, landmarks and viewpoints all day, museums 10–17,
galleries 11–18, gardens 8–19, shopping 10–21, markets 8–14, rec venues 10–22, zoos 9–17, restaurants
11–22, cafés 7–18, bakeries 7–17, dessert 12–22, food halls 10–21); bars, nightclubs, breweries and tours
without real hours are never scheduled. Every drop reason is counted into `plan_runs.counts.drops`.

**Too few candidates.** When fewer than 5 survive, the range is relaxed once: twice the radius, legs
half as long again (walking still at most 4 km), and `range` goes into the run's `relaxed` list.

**Phase B, vectors.** `FetchEmbeddings` loads the 1024-d vectors of the survivors only (cap 800, the best
priors first).

**Query vector and shortlist.** `q = normalize((1 − w)·positiveEmbedding + w·searchEmbedding)` with
`w = 0.6`; without a profile the search vector alone is used; without either, the prior alone (no random
vectors, ever). Each candidate scores `dot(q, e) − 0.5·max(0, dot(negative, e)) + 0.05 per covered
facet`, and the shortlist takes the top 120 subject to at least 15 per requested facet, at least 30 %
events when available, and at most 25 per category. The prior is `0.5·clamp((rating − 3)/2) +
0.2·popularity + 0.3·facetMatch`. The timed-entry slots of one exhibition are one series: they take one
ranker slot and share its scores (`counts.seriesSiblings`).

**ML scoring.** One call to `POST /v1/events/rank` with the stored user vectors, the search embedding
and text, and the shortlist's vectors (classifier only, 3 s). Ids the service drops stay dropped. On
error or timeout the raw score is `0.6·cosineRank + 0.4·prior` and `plan_runs.ml.mode` says
`fallback:<err>`. With `PLANNER_JEV=async`, when the ML service reports `jev: true` and the user has
profile texts, one background Jev rerank of the final shortlist (`rerank_top_k: 20`) is written into
`plan_runs.ml.jev` and the pool's score cache after the response is sent; `sync` waits for it instead.

## Candidate quality rules (from the backend branch)

These rules come from Bayan Mardon's work on the DAG solver on the `backend` branch ("improve dag
solver"), ported into the planner:

- **The range caps every leg**, the first one from the start and the last one back to the end included
  (the old rule allowed those two twice the range).
- **Places must be rated 4 or more** (`PLANNER_MIN_PLACE_RATING`); unrated hiking trails are exempt,
  since trails rarely carry ratings.
- **Fewer than 5 candidates** (`PLANNER_MIN_CANDIDATES`) trigger one retry at twice the radius.
- **Timed-entry slots of one exhibition** (same venue and name) rank as one item and share its score.
- **At most 25 event series and 25 place series** go into one solve (`PLANNER_SOLVER_EVENTS`,
  `PLANNER_SOLVER_PLACES`), the best by utility.
- **Duplicate listings of one show** (the same start time within 100 m) count as one event.
- **One stop per category**, so a plan has at most one restaurant and one café.
- **Stops at or below the midpoint add nothing**: 0.5 on the classifier's scale, 2 of 4 on the LLM
  rerank's. Above it, utility is rescaled as `(score − 0.5) / 0.5`, so a higher bar cuts weak stops without
  shrinking good ones against travel and waiting.
- **Waiting at home before the first stop is free**; only gaps between stops count as idle time.
- **Food and drink places are schedulable**: restaurant, café, bakery, dessert, food hall, brewery, and
  tours (cafés default to 7–18). Meal times are not modelled: a restaurant can land at any open time.
- **The live Atlanta catalog has no restaurants or cafés yet** (the ingestion's Google Places type groups
  leave food out), so food stops appear only in Saltlight Harbor.

## Drop-in stays

A tuning pass made fixed-start events in these categories joinable late and leavable early: live music,
market, festival, community event, sports event, gallery, museum, nightclub, bar and rec venue. Such a
stay begins at the event's start or up to 15 minutes after, ends by the event's end (earlier when the
plan has to get home), and lasts at least min(60 minutes, half the event). A session that needs the whole
of it (tagged `active`, unless it is open play tagged `solo_friendly`) is attended whole, as are tours,
classes, screenings, plays, comedy and restaurants. Drop-in events (`attendance: drop_in`) and listings
longer than 6 hours are windows to drop into. The phase-A query reaches 15 minutes before the window so an
event that started just before it can still be joined.

## Must-see picks

On Create's vibe step the user can search their catalog (`GET /activities/search`, below) and pick
events and places; their ids come back as `PlanRequest.must_include`, and every option on every page
(`/plans/generate` and `/more`) visits each of them. Code: `pkg/planner/picks.go`, the solver's
`itinerary.Config.Required`.

- **The hard rules hold.** A pick is in the user's catalog, on the plan's day (or days) and not over at
  business time, a place of a category plans visit and open that day (hours as in retrieval), and allowed
  for the user's age. The first pick, in request order, that fails ends the run before any retrieval with
  `reason: "must_include_unavailable: <title>"` ("a pick" for a malformed id or one not in the catalog).
  Then the picks alone must fit the window with real travel times (a picks-only solve), else
  `must_include_no_fit`.
- **The soft rules don't.** A pick skips phase A (so the retrieval radius), the Go-side filters, the
  shortlist and its quotas, the classifier's drops, the quality bar, the per-stop price rules and the
  exclusions of the mood text and the user's avoided tags. It is still embedded and scored with the
  shortlist, and counts at least as an unscored stop (0.6), so it is never a weak stop and counts toward
  the pace. Its own legs are held to the clock, not to the range; two picks may wait any time for each
  other; the stop cap rises to the number of picks.
- **The rest is planned around them as usual.** Each pick is a required visit: finished paths must hold
  its series bit, a path that can no longer make one (too late, or no stop left under the cap) is dropped
  at once, and every node keeps K paths per set of picks made, which keeps the DP exact without the
  per-path rules (the brute-force oracle checks it, K = 1 included). Other activities of a pick's series
  are left out; a pick's category is taken from the start, so no other stop shares it (picks may share
  one). When the picks cost more than the budget's total, the total grows to their cost, so the rest adds
  nothing priced; `total_cost_cents` counts the picks. The weak-stop and shared-stop issues, the metrics'
  budget use and both diversity orderings leave the picks out, since every plan has them.
- `/plans/alternatives` and `/plans/route` are unchanged: a pick can still be swapped or removed on Review.
  `plan_runs.spec.mustInclude` records the picks, their shortlist entries say `source: must_include`.

**`GET /activities/search`** `?q=&near=lat,lng&date=YYYY-MM-DD&limit=` → `[ActivityHit]` (store
`Catalog.SearchActivities`, then `planner.ActivityHits`): the events of `date` (in `X-Time-Zone`; today in
business time without it) that are not over and the places open that day, under the same pick rules,
whose name, venue name, category or a tag contains `q` (a space also matches a category's or tag's
underscore). Names starting with `q` come first, then names with a word starting with it, then the other
matches; within each, events by start, then places by distance from `near` (by rating without it). An
empty `q` suggests the day's events by start, then the best-rated places within 5 km of `near`, then the
rest by distance. The subtitle is `"{Category} · {6:30 PM} · {0.7 mi}"` (the time for events, the
distance with `near`); `price_cents` and `distance_mi` are left out when unknown. `limit` is 20 by default
and at most 50; a malformed `near`, `date` or `limit` is `400` with a sentence.

## The loop

Round 0 solves on the retrieved pool. Each following round looks at the best three plans, names what is
wrong with them, adapts the solver, fetches targeted candidates, and solves again on the whole pool.

```
Generate:
  retrieve → feasible → embeddings → shortlist → score → pool (round 0)
  for r in 0..2:
      plans  = solve with the stops above the bar only; if that gives less than a page (3),
               solve again with weak stops allowed
               (each solve: BuildNodes(utility = raw + boosts, 25 event + 25 place series)
                → BuildGraph → Solve(K) → Diverse(Mu))
      scored = Evaluate(plans)              # metrics + Score
      best   = mergeBest(best, scored)      # union by signature, Score desc, ordered with ScoreMu
      stop when: last round · past the 5 s soft budget · no issues · nothing added and nothing adapted
                 · top-3 improved by < 0.02 with nothing added
      issues = Diagnose(best[:3]); Adapt(issues); Expand(issues[:2])
  drop plans with a weak stop when a page of plans has none;
  drop plans travelling more than half their time when another does not
  render → check guarantees → save pool + run → respond
```

| Issue | Trigger | Adapt (no I/O) | Expand (targeted Mongo query) |
|---|---|---|---|
| `no_plans` / `few_plans` | fewer than 3 plans | one step per round: K 16→32 and 12→18 starts per activity; then the range step (twice the radius, legs half as long again; once per run); then one budget tier up if the user is flexible and the budget is not Free; then drop the facets that came only from the mood text | round-0 retrieval with the relaxed filters, excluding the pool |
| `uncovered_facet` | a requested facet covered by no above-the-bar stop in the top 3 | boost that facet's utility by 0.10 | that facet's categories and tags, 40 candidates |
| `idle_gap` | a wait longer than half the pace's longest idle gap in the best plan | — | places open ≥ 30 min and events starting in the gap, centred between the neighbours |
| `head_gap` / `tail_gap` | the plan starts > 60 min late or ends > 75 min early with room for more stops | — | the same, for the head or tail slot |
| `weak_stop` | a stop with raw score < 0.45 | — | same category near that stop, ± 30 min, 30 candidates |
| `shared_stop` | all three plans share a series | the solver's Mu += 0.25; the option ordering's ScoreMu += 0.05 (at most twice its setting) | that slot, excluding the shared category |
| `under_pace` | on a window of 2.5 h or more, the best plan has fewer stops than the pace's target − 1 | K = 24; the per-stop bonus rises to `PLANNER_UNDER_PACE_BONUS` (0.05) | events and places in the half of the window the best plan leaves empty |

**Score.** Each plan's metrics are fit (mean raw score of its stops), fill (the value above the bar its
stops hold against the pace's target: each above-the-bar stop contributes its rescaled utility, the sum
is divided by the target stop count), coverage (requested facets covered by above-the-bar stops), variety
(distinct categories / stops), pace fit (above-the-bar stops against the target), travel share, idle
share, budget use, a late-risk flag and the weak-stop count:

```
Score = 0.30·fit + 0.20·fill + 0.15·coverage + 0.05·variety + 0.10·paceFit + 0.15·(1 − travelShare)
        + 0.05·(1 − idleShare) − 0.05·[lateRisk] − 0.10·weakStops − 0.05·max(0, budgetUse − 0.9)·10
        clamped to 0..1
```

Inside the solver, every above-the-bar stop also earns the pace's stop bonus (relaxed 0.02, balanced
0.05, packed 0.08), so a fuller plan wins when its extra stops are good ones. Options are ordered by Score
less `PLANNER_SCORE_MU` (0.15) times their largest overlap with the options before them, so pages stay
varied.

**Determinism.** The same inputs and scores give byte-identical output: candidates are sorted by
`(score desc, _id asc)` before quotas, the solver breaks ties by id, itineraries with the same stops in the
same order collapse to the best of them, expansions merge in a fixed order and the ordering picks the
first maximum. Tests inject a fixed clock.

**Time budget.** `PLANNER_SOFT_BUDGET_MS` (5000) stops new rounds, `PLANNER_HARD_TIMEOUT_MS` (9000) ends
the request. Every stage is timed into `plan_runs.timings` (`phase_a_ms`, `search_ms`, `phase_b_ms`,
`classifier_ms`, …). On the live server `/plans/generate` takes 0.2–0.6 s.

## Measured on the live server

Sandy Byte's plans on the deployed server, stops per option on the first page, before and after the
Candidate quality rules, Drop-in stays and the tuning pass:

| Request | Before | After |
|---|---|---|
| Tomorrow 12–4 PM, walkable, balanced | 2, 2, 1 | 3, 3, 2 |
| Tomorrow 1–6 PM, transit, packed | 3, 3, 4 | 4, 4, 4 |
| Tomorrow 5–9 PM, walkable, relaxed | 1, 1, 2 | 2, 2, 1 (now includes the Sea Shanty Singalong, joined at 5:15) |
| Tonight 5:30–9 PM, walkable (a run with the clock pinned) | 1, 1, 1 | 2, 2, 1, with Sunset Jazz on Pier Nine joined 15 minutes late |

Generation takes 0.2–0.6 s. Known limit: transit and anywhere days still cover 8–12 miles, because a
stop's value does not yet grow with the length of the visit.

## Outputs

The HTTP layer emits only what `pkg/contract` defines; everything else the planner computes stays in
`plan_pools` and `plan_runs`.

| Wire type | Keys |
|---|---|
| `PlanBatch` | `options`, `cursor` (while more remain), `done`, and `reason` only with empty `options`: `no_candidates_fit_window`, `no_feasible_itinerary`, `invalid_request: …`, `must_include_unavailable: <title>` or `must_include_no_fit` |
| `PlanOption` | `id` (`<runId>-<n>`), `name`, `tag`, `meta`, `stops`, `late_flag` (always), `total_cost_cents` (the known prices' sum, left out when no price is known) |
| `PlanStop` | `id`, `title`, `subtitle`, `place {name, coordinate}`, `duration_minutes`, plus `arrive_time`, `depart_time`, `kind` (`event` or `place`), `flexible`, `activity_id` |
| `RouteResult` | `legs [{mode, minutes}]`, `stop_times [{start, end}]`, `arrival`, `minutes_late`, `broken_at` (always; −1 when every stop works) |
| `PlanAlternative` | `stop`, `reason` |

`late_flag` means a stop that runs to its p75 length would miss the next start or back-by; the app shows
it as a "Tight timing" chip. After a reorder, `broken_at` becomes the "Some stops would be late" state and
`reason` the empty-state copy. The pool keeps more per option (summary, route summary, legs with
distances and stop ids, costs with `cost_known`, depart and arrival, metrics and score) for `/more`,
`/route` and `/alternatives`.

Generated text: `name` is the first two stops shortened ("Sunset Jazz + Seaside Market Hall", then "+ N
more"); `tag` is "Best match" for the first option and then, in order and without repeats, Free,
Outdoors, Nightlife, Meet people, Chill, Packed, Active, Culture, or "Different vibe"; `meta` is
`"{Free|~$|~$$|~$$$} · {miles} mi {walking|by transit|by car|by rideshare} · {n} {transit|drive|rideshare} legs"`,
with the price part left out when no price is known and "N stops" for walking-only plans; a stop's
`subtitle` is `"{Category} · {Free|$|$$|$$$}"` plus the start time for events.

## Follow-up calls

| Call | What happens |
|---|---|
| `POST /plans/generate/more {cursor}` | the cursor is `dag_<runId>_<offset>`; the pool (`plan_pools`, 6 h TTL, shared across workers and restarts) returns the next two options, and `done` when exhausted. An unknown, expired or another user's cursor returns `{options: [], done: true}` |
| `POST /plans/route` | resolves `stop_order` against the option's stops and the alternatives already suggested for it, applies the request's start, end, times, ride and modes, and re-times: legs, `stop_times`, `arrival`, `minutes_late` and `broken_at`, the first stop in the new order that no longer works (a fixed start reached late or a place that would be closed; minutes outside opening hours count as late). An expired or another user's pool is `404` "This plan expired. Generate again."; a stop id the plan does not know is `400` "That plan changed. Go back and try again." |
| `POST /plans/alternatives` | up to five stops for the same slot (± 30 min), of the same category or sharing at least two tags, reachable from the neighbours within the range, never already in the plan; scored `0.7·cosine + 0.3·ML` against the pool's stored query vector; `reason` like "Also live music · 0.4 mi away · Free". They are stored in the pool so `/plans/route` knows them. Same errors as `route` |
| `POST /itineraries` | built by `pkg/api/itineraries` from the request body: a `transit` item per route leg ("Walk to …", "MARTA to …", "Rideshare to …", "Drive to …") and a `sidequest` item per stop at its route times, with the activity id, price, website and bookability the planner's `ResolveStop` finds (the newest live pool that holds the stop id, else the catalog activity the id names). It works after the pool expired. The itinerary keeps `runId` and `optionId`. The host's `calendar_events` that overlap the window come along as `busy` items (only the host sees them), and each leg is laid around them as above |
| `GET /calendar/days` | the saved items on each local day, plus the viewer's busy blocks from `calendar_events` on every day they overlap (with their event id and no `itinerary_id`; `GET /events/{id}` opens one) |

## Knobs

All read from the environment by `pkg/planner` (`FromEnv`), with these defaults:

| Variable | Default | Meaning |
|---|---|---|
| `PLANNER` | `auto` | still validated (`auto`, `dag` or `legacy`), but the DAG planner is the only one now; the VPS sets `dag` |
| `PLANNER_SOFT_BUDGET_MS` / `PLANNER_HARD_TIMEOUT_MS` | 5000 / 9000 | stop starting rounds after the soft budget; give up at the hard timeout |
| `PLANNER_ROUNDS` / `PLANNER_EPSILON` | 3 / 0.02 | loop length and the minimum top-3 improvement to continue |
| `PLANNER_SHORTLIST_N` / `PLANNER_FACET_QUOTA` / `PLANNER_CATEGORY_CAP` | 120 / 15 / 25 | the cosine shortlist |
| `PLANNER_PHASE_A_EVENTS` / `PLANNER_PHASE_A_PLACES` / `PLANNER_EMBED_FETCH_CAP` | 400 / 600 / 800 | retrieval caps |
| `PLANNER_MIN_PLACE_RATING` | 4 | places rated below it are dropped (unrated hikes kept) |
| `PLANNER_MIN_CANDIDATES` | 5 | fewer feasible candidates trigger the one range retry |
| `PLANNER_SOLVER_EVENTS` / `PLANNER_SOLVER_PLACES` | 25 / 25 | series of each kind one solve sees |
| `PLANNER_EXPANSION_LIMIT` / `PLANNER_MAX_EXPANSIONS` | 40 / 2 | candidates per expansion query, expansions per round |
| `PLANNER_ML_TIMEOUT_MS` / `PLANNER_SEARCH_TIMEOUT_MS` | 3000 / 5000 | the classifier call and the search-profile call |
| `PLANNER_JEV` / `PLANNER_JEV_TIMEOUT_MS` / `PLANNER_JEV_TOP_K` | `async` / 45000 / 20 | the optional LLM rerank (`off`, `async`, `sync`) |
| `PLANNER_SEARCH_WEIGHT` / `PLANNER_DISLIKE_LAMBDA` | 0.6 / 0.5 | query-vector blend and the dislike penalty |
| `PLANNER_RANGE_KM` | `walkable=2,transit=10,anywhere=25` | per-leg maxima (`2,10,25` works too) |
| `PLANNER_WALK_LEG_CAP_KM` / `PLANNER_CITY_SNAP_KM` | 4 / 60 | walking cap; the snap rule |
| `PLANNER_MAX_TRAVEL_SHARE` | 0.5 | options travelling more than this share of their time are dropped when another is not |
| `PLANNER_UNDER_PACE_BONUS` | 0.05 | the per-stop bonus after an under-pace round |
| `PLANNER_SCORE_MU` | 0.15 | how much an option's overlap with earlier options lowers its place in the order |
| `PLANNER_POOL_TTL_H` / `PLANNER_RUN_TTL_H` | 6 / 72 | Mongo TTLs of `plan_pools` and `plan_runs` |
| `PLANNER_SERIES_CAP` | 96 | distinct series the solver's label mask tracks |
| `PLANNER_DEBUG` | 0 | `1` attaches the run log to the planner's internal result; the HTTP layer never sends it, so read `plan_runs` |

The solver's own settings (`pkg/itinerary.DefaultConfig`): the quality bar (Tau) 0.5, travel penalty
0.005 per minute, 10-minute buffer, no cost for waiting before the first stop, K = 16 partial paths per
node, 64 finished itineraries, diversity penalty 0.5, a 30-minute grid with up to 12 starts per flexible
activity, stays between 15 minutes and 6 hours, 0.6 for an unscored activity, places weighted 0.8
against events.

## Inspecting a run

Every generation writes a `plan_runs` document (72 h TTL) with the raw request, the parsed spec, the
filters, the counts (phase A, feasible, drops by reason, shortlist, ranked, `seriesSiblings`), the
shortlist with every score component, one entry per round (K, Mu, `scoreMu`, `stopBonus`, boosts,
`withWeak`, node and edge counts, timings, the top-3 signatures, scores and metrics with `fill`,
`goodStops` and `weakStops`, issues, expansions, what was adapted and why the loop stopped), the ML mode,
and the final option ids, `relaxed`, `rejected`, `travelHeavy` and `weakDropped`. The saved itinerary
links back through its `runId`; the app's save path does not write the run's `outcome` field. Locally
the database is in the `sq-mongo` container, so prefix the queries with `docker exec sq-mongo mongosh
--quiet freetime --eval '…'`; on the VPS run `mongosh freetime` directly.

```js
// 1. The latest run for a user: what was asked, what came back, how long it took
db.plan_runs.find({userId: "<user id>"}, {createdAt: 1, tz: 1, snappedStart: 1, "filters.window": 1, "final": 1, "ml.mode": 1})
  .sort({createdAt: -1}).limit(1)

// 2. Why candidates disappeared: phase A counts, feasibility drops by reason, shortlist size
db.plan_runs.findOne({_id: "<run id>"}, {counts: 1, "filters.radiusKm": 1, "filters.budget": 1, "filters.moodHard": 1})

// 3. What the loop did: per round, the issues, expansions, weak-stop solve and the top-3 plans
db.plan_runs.findOne({_id: "<run id>"}, {"rounds.round": 1, "rounds.issues": 1, "rounds.expansions": 1, "rounds.adapted": 1,
  "rounds.withWeak": 1, "rounds.stop": 1, "rounds.top3.score": 1, "rounds.top3.metrics": 1, "rounds.solveMs": 1})
```

`db.plan_pools.findOne({_id: "<run id>"})` shows the rendered options, their stop records and any
alternatives handed out.

## Limitations

- **Travel times are estimates**, not routes: straight-line distance × 1.3, at 4.5 km/h walking, 20 km/h
  plus 5 minutes for transit, 30 km/h plus 5 minutes for driving; legs of 0.8 km or less are always
  walked. A routing provider plugs in behind `travel.Provider` ([roadmap](ROADMAP.md)).
- **A stop's value does not grow with the visit**, so transit and anywhere days still spread over 8–12
  miles.
- **Meal times are not modelled**, and the live Atlanta catalog has no restaurants or cafés yet.
- **Mood parsing is rule-based.** Negations, "free", "family" and "sober" become hard constraints;
  everything else only shapes the search embedding and the soft facets.
- **Opening hours** assume day 0 of `weeklyHours` is Sunday (Google's convention), unverified against
  live Google data; the demo generator follows the same convention. Places without hours get category
  defaults; bars, nightclubs, breweries and tours without hours are never scheduled.
- **Scoring degrades quietly**: a user without a profile gets mood-only or prior-only ranking
  (`plan_runs.ml.mode` says which); an ML timeout means the cosine and prior fallback.
- **One process per solve.** The pool is shared through Mongo, but each run's loop lives in one request;
  the 9 s hard timeout sits well under Cloudflare's 100 s request cap.
- **Variety is enforced, not chosen**: one stop per category and at most 25 event and 25 place series per
  solve, so a bar crawl or a food crawl is not something the planner will produce.
