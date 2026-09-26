# Itinerary planner

How `POST /plans/generate` builds plans, and what the iOS app needs to send and handle.

## What it does

1. The backend loads candidate activities (events and places) and ranks them with the ML service.
2. The itinerary optimizer (`pkg/itinerary`) turns the ranked list into plans that actually work:
   - stops never overlap;
   - travel time between stops is accounted for;
   - each leg stays within `range_km`;
   - the user is back at the end location by `back_by_time`;
   - the same venue or exhibition is never visited twice, and there's at most one stop per category (e.g. one comedy show);
   - the total cost stays under `budget_cents` when prices are known.
3. It returns the 3 best plans, chosen so they don't all repeat the same stops. More plans from the same run are available through `/plans/generate/more`.

Events keep their real start times. Places (parks, bars, museums…) and drop-in events are scheduled at a time when they're open, on the hour or half hour.

Travel times are currently estimated from straight-line distance, not a routing service. Expect them to be roughly right, not exact.

## What iOS must send

`POST /plans/generate`

```json
{
  "start_location": "33.7766,-84.3890",
  "end_location": "33.7766,-84.3890",
  "date": "2026-09-26",
  "start_time": "18:00",
  "back_by_time": "23:00",
  "range_km": 3,
  "ride_choice": "none",
  "travel_modes": ["walk", "marta"],
  "budget_cents": 4000,
  "pace": "balanced",
  "mood_text": "Something chill, then live music",
  "tags": ["music"]
}
```

| Field | Format | Notes |
|---|---|---|
| `start_location` | `"lat,lng"` | **Required.** Decimal degrees, latitude first. Free text like `"Midtown"` isn't understood; the backend then falls back to the user's last known location. |
| `end_location` | `"lat,lng"` | Send the start again for a round trip, or the point the user picked. |
| `date` | `YYYY-MM-DD` | Local date in the city being planned. |
| `start_time`, `back_by_time` | `HH:MM`, 24-hour | Local time. A back-by at or before the start means the next day (`21:00` → `02:00` is fine). |
| `range_km` | number | Longest leg between stops, straight line. `0` uses a default: walk 2, transit 10, drive 25. Suggested mapping: Walkable 2, Transit 10, Anywhere 25. |
| `ride_choice` | `drive` \| `rideshare` \| `none` | `drive` or `rideshare` plans driving legs; anything else uses `travel_modes`. |
| `travel_modes` | list | `marta`/`transit` → transit legs; otherwise walking. Legs of 0.8 km or less are always walked. |
| `budget_cents` | integer | Total for the plan. `0` = no limit. Stops with no known price count as free. |
| `pace` | `chill` \| `balanced` \| `packed` | Max stops 2 / 3 / 5, and how much idle time between stops is acceptable. `relaxed` is accepted as `chill`. |
| `mood_text`, `tags` | | Used by the ML ranking, not by the planner. |

## What iOS gets back

```json
{
  "planner": "dag",
  "options": [
    {
      "id": "01a0…",
      "title": "Tech Tower, Bobby Lee: The Finally Tour 2026 & 1 more",
      "summary": "3 stops · 5:30 PM–10:00 PM · 4.1 km walking",
      "stops": [
        {
          "id": "stop_6ab7…_0",
          "place_id": "6ab7…",
          "name": "Tech Tower",
          "address": "225 North Ave NW, Atlanta, GA",
          "lat": 33.7724, "lng": -84.3947,
          "order": 0,
          "duration_min": 30,
          "estimated_cost_cents": 0,
          "arrive_time": "2026-09-26T21:30:00Z",
          "depart_time": "2026-09-26T22:00:00Z",
          "kind": "place",
          "flexible": true
        }
      ],
      "legs": [
        { "from_stop_id": "start", "to_stop_id": "stop_6ab7…_0", "mode": "walk", "duration_min": 11, "distance_km": 0.83 },
        { "from_stop_id": "stop_6ab7…_0", "to_stop_id": "stop_…_1", "mode": "transit", "duration_min": 14, "distance_km": 3.1 },
        { "from_stop_id": "stop_…_2", "to_stop_id": "end", "mode": "walk", "duration_min": 9, "distance_km": 0.7 }
      ],
      "route_summary": "Walk 11 min → Transit 14 min → … → Walk 9 min",
      "total_cost_cents": 3500,
      "total_duration_min": 290,
      "late_flag": false,
      "arrival_time": "2026-09-27T02:09:00Z"
    }
  ],
  "cursor": "dag_01a0…",
  "done": false
}
```

### Things iOS must handle

- **Stop times.** `arrive_time` / `depart_time` are the scheduled visit, as UTC ISO 8601. Show them in the device's or city's local time. `arrive_time` is when the visit starts; the user may get there a few minutes earlier.
- **Legs.** There is one more leg than stops. The first leg starts at `"start"` (the user's start location), the last ends at `"end"`. `legs[i]` is the leg *arriving at* `stops[i]`.
- **Leg modes.** Possible values: `walk`, `transit`, `drive`, `rideshare`. **`transit` is new**: the Swift `TravelMode` enum needs a case for it (or map it to `marta`).
- **`kind` and `flexible`.** `kind` is `event` or `place`. `flexible: true` means the time was chosen by the planner and could move (places, drop-in events). `false` means a fixed event start.
- **`late_flag`.** True when an event whose length is estimated could run long enough to break the plan. Worth a small "tight timing" hint, not an error.
- **Empty or fallback results.**
  - If the planner can't build anything, the backend currently falls back to the old builder. Those options have **no `arrive_time`/`depart_time`**, no `"planner": "dag"`, and legs only *between* stops (no start or end leg). iOS should tolerate missing times until the fallback is removed.
  - With `PLANNER=dag` set on the server, there is no fallback: `options` is empty and `reason` explains why (`no_candidates_fit_window`, `no_feasible_itinerary`, or `invalid_request: …`).

## Follow-up calls

**Load more:** `POST /plans/generate/more` with `{"cursor": "<cursor>"}`. Returns 2 more options from the same run, a new `cursor`, and `done: true` on the last page. Cursors start with `dag_`. They're kept in server memory for 2 hours and are lost if the server restarts; an expired cursor returns an empty last page.

**Reorder stops:** `POST /plans/route`

```json
{ "option_id": "01a0…", "stop_order": ["stop_…_1", "stop_…_0"], "ride": "", "modes": ["walk"] }
```

Response:

```json
{
  "option_id": "01a0…",
  "recalculated_legs": [ … ],
  "stop_times": [ { "stop_id": "stop_…_1", "arrive_time": "…", "depart_time": "…" } ],
  "total_duration_min": 290,
  "arrival": "…",
  "late_flag": true,
  "minutes_late": 25,
  "broken_at": 1
}
```

`broken_at` is the index (in the new order) of the first fixed-time event the user would reach late, or `-1`. Events can't move, so most reorders of events will be late. Use this to warn, or to snap back to the original order.

**Save:** `POST /itineraries` with `{"option_id": "…", "visibility": "just_me"}`. The saved items keep each stop's scheduled times, so iOS doesn't need to send `items`.

## iOS checklist

- [ ] Send `start_location` / `end_location` as `"lat,lng"` strings.
- [ ] Send `date` as `YYYY-MM-DD` and times as `HH:MM`, all local.
- [ ] Map the range picker to `range_km` (e.g. 2 / 10 / 25) and pace to `chill` / `balanced` / `packed`.
- [ ] Decode the Go `PlanOption` shape: `title`, `summary`, `stops[].name`, `lat`/`lng`, `duration_min`, plus the new `arrive_time`, `depart_time`, `kind`, `flexible`. Make the new fields optional.
- [ ] Handle legs with `"start"` / `"end"` ids and the `transit` mode.
- [ ] Show `late_flag` as a hint, and handle an empty `options` list with `reason`.
- [ ] Use `broken_at` / `minutes_late` from `/plans/route` when the user reorders stops.

## Known limitations

- Travel times are straight-line estimates; a routing provider plugs in later behind the same interface (`pkg/travel`).
- Candidate filtering by city, day and area isn't done yet: the planner only sees the 15 activities the handler loads. Until that lands, many real requests fall back to the old builder.
- Some Ticketmaster add-on listings (parking, "Express Entry … Not a Concert Ticket") can appear as stops until filtering removes them.
- Opening hours assume day 0 of `weeklyHours` is Sunday (Google's convention). This hasn't been checked against real hours yet.
