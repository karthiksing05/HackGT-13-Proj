# Itinerary planner

Moved to [`docs/PLANNER.md`](../docs/PLANNER.md). That page covers what every plan guarantees, how the
app's `PlanRequest` is mapped (the legacy `start_location: "lat,lng"` / `HH:MM` / `range_km` /
`budget_cents` shape described here before is still accepted), candidate retrieval with guaranteed
filters, the iterative DAG loop, the response fields the app decodes (`arrive_time`, `kind`, `flexible`,
`late_flag`, `legs` with `start` and `end` ids, `broken_at`), `/plans/generate/more`, `/plans/route`,
`/plans/alternatives`, saving, the `PLANNER_*` knobs, how to read a `plan_runs` document, and the known
limitations (straight-line travel estimates, the opening-hours convention). The solver itself is still
`pkg/itinerary` (nodes → graph → K-best paths → evaluate); the orchestration around it is `pkg/planner`.
