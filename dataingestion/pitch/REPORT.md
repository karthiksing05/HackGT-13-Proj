# Atlanta pitch catalog (`freetime.pitch_activities`)

> **Update 2026-09-27, ~03:30 EDT.** The catalog is complete in production:
> - All 250 items have embedding texts and vectors.
> - The 150 paid items have `ticketUrl`s. Their listings still have to be added to the ticket site: [TICKETS_HANDOFF.md](TICKETS_HANDOFF.md).
> - An audit fixed 8 descriptions: seven clubs and bars now state their price range, and "Oktoberfest Party" states 21+.
> - Sections below that describe texts or vectors as pending, or ticket links as absent, predate this update.

**Synthetic demonstration data.** 250 activities for the SideQuests pitch, built on real Atlanta
venues from `freetime.activities`. Every venue's location, address, opening hours, Google rating and
link are copied verbatim from its real record. The event names, dates, times, prices, descriptions and
tags were written for the demo: they are **not verified public listings**, and nothing in the
documents claims otherwise. Provenance names only the synthetic `pitch` source, events carry no URL,
and no image links are set. The 150 paid items link to ticket pages on the sandbox ticket site ([TICKETS_HANDOFF.md](TICKETS_HANDOFF.md)). `manifest.json` maps every document to the real records it was
built on and lists which fields were copied, adjusted or invented.

## At a glance

| | |
|---|---|
| Database / collection | `freetime.pitch_activities` on the MongoDB behind `127.0.0.1:27017` (the SSH tunnel to the production VPS) |
| Documents | **250**: 99 places, 151 events |
| Paid / free | **150 paid (60%) / 100 free (40%)**, by the planner's own price reading (`price.go` `activityCost`, `itinerary.CostCents`) |
| City, zone | `atlanta`, `America/New_York` |
| Dates | **Sun Sep 27 – Sat Oct 10, 2026**, local. The pitch day is Sun Sep 27 (28 events) |
| Embedding texts | **250 of 250** (written by Claude with the pipeline's prompt; weekly repeats share one) |
| Vectors | **250 of 250**: Qwen3-Embedding-0.6B, 1024-d, unit norm, no prompt (Raven, 2026-09-27) |
| Ticket links | **150**, one per paid item: `https://events.sidequestz.tech/{slug}/tickets` |
| Indexes | the same seven as `demo_activities`; `expiresAt_1` is **not** a TTL |

## Why Atlanta, and why these dates

- **City.** The brief asks for real venues and neighborhoods from the real catalog, which exists only
  for real cities. `DATA_COLLECTION_SPEC.md` names Atlanta the demo city, and `cities/atlanta.yaml`
  sets its demo window (Sunday 1–5 PM) and start (the Klaus building at Georgia Tech, HackGT's venue).
  Atlanta is also the planner's default real catalog. The judges' account, Sandy Byte, plans in the
  fictional Saltlight Harbor, which has no real records to draw on.
- **Dates.** A fixed demo date is configured: production's `DEMO_DATE=2026-09-27` in `DEMO_TZ`
  (America/New_York), which is also today. The range starts on that Sunday and runs 14 days, so plans
  work on the pitch day, on "tomorrow" in the app, and for real-time accounts through the following
  weekend. Nothing precedes the pitch day, because past events cannot be planned. The range has no DST
  change (DST ends Nov 1).
- **Shape.** The pitch day is the busiest day (28 events, 9 morning, 10 afternoon, 8 evening and 1 late
  night; 12 of them free). Other weekends carry 17–19 events per day and weekdays 3–10, mostly evenings.
  Places cover every hour of the day through their real opening hours: 45 are open at 9:00 on the pitch
  day, 85 at 13:00, 81 at 16:00, 53 at 19:00 and 38 at 22:00.

## Distribution

| Group | Categories used | Total | Paid | Free |
|---|---|---:|---:|---:|
| Nightclubs, DJ nights, dancing | nightclub (places and events), community_event | 22 | 18 | 4 |
| Concerts and live music | live_music, bar (blues bars) | 30 | 22 | 8 |
| Restaurants and dinner experiences | restaurant (places and ticketed dinners) | 28 | 28 | 0 |
| Breakfast, brunch, cafés | restaurant, cafe, community_event, tour | 16 | 16 | 0 |
| Hikes, parks, outdoor recreation | park, hike, garden, tour | 40 | 6 | 34 |
| Museums, galleries, cultural visits | museum, gallery, landmark, zoo_aquarium, tour | 28 | 13 | 15 |
| Workshops and creative classes | class_workshop | 24 | 18 | 6 |
| Sports, fitness, active recreation | rec_venue, sports_event, class_workshop, community_event, tour | 26 | 13 | 13 |
| Comedy, theater, performances | comedy, theater, cinema | 18 | 15 | 3 |
| Markets, festivals, community | market, shopping, festival, community_event | 18 | 1 | 17 |
| **Total** | 23 of the vocabulary's 25 categories | **250** | **150** | **100** |

There were no adjustments between groups. Mix: 99 flexible place visits and 151 dated events (111
`fixed_start`, 40 `drop_in`); both indoor and outdoor; typical place visits of 30 minutes to 3 hours, and
events lasting 1 to 10 hours (a festival's open window); price tiers 0/1/2/3/4 = 100/51/76/19/4; 11 weekly
series of two occurrences each.

## What is real and what is synthetic

| Real, copied verbatim from `freetime.activities` | Adjusted, and documented per document in the manifest | Synthetic |
|---|---|---|
| venue names, coordinates, addresses; Google opening hours, ratings and review counts, place ids and links; OpenStreetMap trail geometry, length, climb, duration and link; Google price ranges for 25 bars, pubs and restaurants | 25 venues recategorized (21 pubs, taverns and brewpubs with full kitchens as `restaurant`, 2 cafés, a pickleball club as `rec_venue`, Ponce City Market as `market`), with `sourceCategory` keeping Google's type; 2 trails named with their park and 3 event venues' listing names tidied; `address.formatted` composed from the real parts for 15 event listings that left it empty; the planner's default hours written out for 4 venues whose record has none | every event (name, date, time, length, price, description); every description, summary and tag; estimated admissions and fees for 28 paid places; popularity (the demo formula); recurrence; embedding texts |

Real records behind the catalog: 159 in all. 129 Google Places records (bars and pubs, museums,
galleries, parks, rec venues, theaters, markets), 16 OpenStreetMap trails, and 11 Ticketmaster and 3
Resident Advisor listings used only for their venues' locations. The real listings' timing, such as concerts at
7–8 PM, club nights 10 PM–3 AM, Sunday day parties and theater matinees, shaped the schedule.
Synthetic events avoid real listings at the same venue and time: three were moved after a check
against the live catalog.

## Validation

`python -m pitch.validate` checks the export before insertion, and `--mongo` re-checks the
collection after it. Result on the stored collection (`--partial-texts --mongo`): **0 errors**, 250
documents identical to the export. The 8 warnings: 150 documents still waiting for an embedding text,
and 7 clubs and blues bars whose descriptions say "cover varies" instead of a dollar range (their price
objects carry the Google range).

Checked: the 250 count and the 150/100 split by the app's price logic; the group table; every document
against the pipeline's pydantic `Activity` model and against a BSON-type inventory of `demo_activities`;
vocabularies; unique ids and source keys (`_id = sha1(sourceKey)`); coordinates are `[lng, lat]` in
metro Atlanta and identical to the real records; hours, ratings, links, trails and durations identical
to the real records; UTC storage, local plausibility windows per activity type, overnight events ending
on the next morning, durations (`event_times` for fixed starts, category priors for drop-ins and
places), `expiresAt`; recurrence rules against every occurrence; `isFree`/min/max/tier and
`free`/`cheap`/`splurge` against the price; embedding texts (hash, prompt, input hash, format); and that
no vector field exists.

**Fields left empty where they do not apply** (the schema's own null convention):

| Field | Null on | Why |
|---|---|---|
| `url` | the 151 events | synthetic events have no web page; a real venue link would read as the event's source |
| `ticketUrl`, `imageUrl` | all 250 | no real ticket pages or approved images exist; the sandbox merchant sells only Saltlight's events |
| `rating`, `ratingCount` | events and the 16 trails | ratings are Google's; none is invented |
| `googlePlaceId` | events and trails | not Google places |
| `address` | the 16 trails | OpenStreetMap trails have no street address (as in `activities`) |
| `venueName` | 6 trails | no park is recorded for them |
| `start`, `end`, `expiresAt` | places | places use `weeklyHours` |
| `weeklyHours`, `hoursSource` | events | events use `start`/`end` |
| `recurrence` | 228 one-off documents | only the 22 series occurrences repeat |
| `trail` | non-trails | |
| `embedding`, `embeddingModel`, `embeddingMeta` | all (absent, not null) | vectors come later, as requested; absent is what `embed_missing` selects |

## Planner compatibility

Checked with the real Go planner in `Backend/pkg/planner/mongosource/pitchcatalog` (opt-in build tag
`pitchcatalog`). It loads the export into a throwaway `mongod` and refuses any server that holds a
`freetime` database.

- All 250 documents decode into `models.Activity`, and the planner counts 150 paid / 100 free. The
  free-only query keeps exactly the 100 free documents.
- The Mongo pre-filters and their Go re-check agree on all 576 queries: 8 windows (morning, afternoon,
  evening and overnight, weekday and weekend) × 3 start points × 3 ranges × 4 budgets × 2 age brackets.
- Fixed-start and drop-in eligibility: a 7 PM concert can still be joined at 7:10 (the 15-minute late
  rule for live music), a drop-in salsa social stays open until 8 PM, and a 6 PM dinner is not
  offered after it began.
- Nine plan requests ran end to end (pitch demo Sunday 1–5 PM walkable from Tech Square; free-only;
  brunch; Sunday evening; late night; Friday past midnight; Wednesday after work; Saturday outdoors;
  Saturday classes). Every option, from 10 to 41 per request, passes `planner.CheckOption`: inside the
  window, open or on time, back by the deadline, within range and budget, age rules, one stop per
  category. Free-only plans contain only free stops, and paid-inclusive plans include paid ones.
- **Ranking without vectors.** Until the vectors exist, the planner has no query vector and ranks by
  its prior. Places carry Google ratings of 4.0–4.9 and events carry none, so today's plans are made of
  places only. With vectors present (simulated in the test with a stand-in classifier), plans mix events
  and places: for example, the evening request yields 27 event stops across 31 options, and Friday night
  goes Hip-Hop Showcase (joined at 8:15) → 42 Bar and Grill → House & Disco Friday (12:10 AM).

## Integration: what is still needed

The collection exists and is valid, but **the app cannot select it yet**. No application code or
configuration was changed. To use it:

1. **Allow the catalog name** in the three hard-coded lists:
   - `Backend/pkg/planner/config.go:408`: add `"pitch_activities": "atlanta"` to `catalogs`;
   - `Backend/pkg/store/catalog.go` `CatalogCollection`: return `pitch_activities` for it (today
     anything but `demo_activities` becomes `activities`);
   - `Backend/pkg/store/store.go:70` `CatalogCollections`: add it so `ensure-indexes` covers it and
     `reset-app-data` never touches it;
   - and `Backend/pkg/planner/route.go:253`, the stop-detail fallback list.
2. **Point an account at it**: `users.catalog = "pitch_activities"`, `city = "atlanta"`, and a home
   base in Midtown (e.g. Tech Square). Otherwise the planner snaps starts more than 60 km away back to
   the account's city.
3. **Demo date, if wanted**: the demo clock applies only to `catalog == "demo_activities"`
   (`Backend/pkg/api/democlock.go` `ClockFor`). Without a change there, a pitch account lives in real
   time. That is fine on Sep 27 and for the two weeks after.
4. **Vectors** (deferred, as requested). Run `embed_missing` over the collection:
   `cd /opt/ml && .venv/bin/python -m tools.embed_missing --uri 'mongodb://127.0.0.1:27017/?directConnection=true' --db freetime --collections pitch_activities`,
   or add it to `ml-embed-missing.service`'s `--collections`. The texts are ready and the vector fields
   are absent, which is exactly what it selects. Until then, ranking is prior-only and plans show no events.

## Files

| File | What |
|---|---|
| `pitch/specs.py` | the 250 activities as data (venue record ids, times, prices, descriptions) |
| `pitch/generate.py` | builds the documents with the pipeline's model and helpers; writes the export and manifest |
| `pitch/texts.py` | exports each document's `{{EVENT_DATA}}`, checks and imports the embedding texts |
| `pitch/validate.py` | offline and `--mongo` validation (report: `pitch/out/validation_report.json`) |
| `pitch/load.py` | creates the collection and indexes, inserts only what is missing, logs every inserted id (`pitch/out/insert_log.json`) |
| `pitch/queries.js` | the same checks for mongosh |
| `pitch/SCHEMA_INVENTORY.md` | every field: type, meaning, when it applies, how it was generated |
| `pitch/manifest.json` | per document: group, price class, real records used, copied, adjusted and invented fields |
| `pitch/out/pitch_activities.json` | canonical Extended JSON export (BSON types preserved). Gitignored, because it carries Google Places data, which `docs/DATA.md` says is never redistributed |
| `Backend/pkg/planner/mongosource/pitchcatalog/` | the planner compatibility test |

Rebuild from scratch: `cd dataingestion && .venv/bin/python -m pitch.generate && .venv/bin/python -m pitch.texts import && .venv/bin/python -m pitch.generate && .venv/bin/python -m pitch.validate && .venv/bin/python -m pitch.load`.
