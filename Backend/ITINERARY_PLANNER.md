# Itinerary planner

Moved to [`docs/PLANNER.md`](../docs/PLANNER.md). That page covers what every plan guarantees, how the
app's `PlanRequest` is mapped (the legacy `start_location: "lat,lng"` / `HH:MM` / `range_km` /
`budget_cents` shape described here before is still accepted), candidate retrieval with guaranteed
filters, the candidate quality rules from Bayan Mardon's solver work, drop-in stays, the iterative DAG
loop, the fields the app receives (`arrive_time`, `depart_time`, `kind`, `flexible`, `activity_id`,
`late_flag`, `total_cost_cents`, `broken_at`; the older `title`/`summary`/`legs` shape is gone),
`/plans/generate/more`, `/plans/route`, `/plans/alternatives`, saving, the `PLANNER_*` knobs, how to read
a `plan_runs` document, results measured on the live server, and the known limitations (straight-line
travel estimates, the opening-hours convention). The solver itself is still `pkg/itinerary` (nodes →
graph → K-best paths → evaluate); the orchestration around it is `pkg/planner`.
