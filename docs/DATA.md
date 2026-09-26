# Data

The catalog the planner reads, where it comes from, and how to load and refresh it. Pipeline code:
`dataingestion/` ([README](../dataingestion/README.md)); the full design is
[dataingestion/DATA_COLLECTION_SPEC.md](../dataingestion/DATA_COLLECTION_SPEC.md).

## Collections

| Collection | Holds | Written by |
|---|---|---|
| `activities` | real events, places and trails; `city` slug per document (Atlanta today; the sample on a dev machine is 400 documents) | the ingestion pipeline (`python -m ingest …`); vectors by the Raven backfill and `embed_missing` |
| `demo_activities` | the 100 Saltlight Harbor activities, same schema, `city: saltlight` | imported from `dataingestion/demo/saltlight_harbor.json`; texts from the pipeline's `embed-text` prompt, vectors from the Raven backfill (below) |
| `runs`, `quota`, `research`, `crawl_state`, `travel_cache` | pipeline bookkeeping: one record per adapter run; API quota counters; cached web research per activity; the crawler's last-run times; travel-matrix cache | the pipeline |
| `users`, `itineraries`, `plan_pools`, `plan_runs`, … | the app's own collections | the Go API ([ARCHITECTURE.md](ARCHITECTURE.md), [design/backend-contract.md](design/backend-contract.md) §2) |

A user's `catalog` field picks `activities` or `demo_activities`; the app never mixes them.

## The `activities` schema (summary)

| Field | Type | Notes |
|---|---|---|
| `kind` | `event` \| `place` | trails are places with `category: hike` and a `trail` subdocument (length, ascent, loop, GeoJSON line) |
| `city` | slug | `atlanta`, `saltlight`, … from `dataingestion/cities/<slug>.yaml` |
| `name`, `summary`, `description`, `venueName`, `address {formatted, street, locality, region, postalCode, countryCode}` | strings | `summary` ≤ 25 words, factual; `description` raw source text ≤ 2000 chars |
| `category`, `tags` | fixed vocabularies | below |
| `location` | GeoJSON Point `[lng, lat]` | longitude first |
| `start`, `end`, `attendance` | UTC instants; `fixed_start` \| `drop_in` | events only. `fixed_start`: arrive at `start`; `drop_in`: any time in `[start, end]`. The planner lets some fixed-start categories be joined up to 15 minutes late ([PLANNER.md](PLANNER.md#drop-in-stays)) |
| `timezone`, `weeklyHours [{open, close}]`, `hoursSource` | IANA zone; minutes since Sunday 00:00 local (day 0 = Sunday, wraps past Saturday) | places only; `hoursSource` `google` \| `osm` \| `nps` \| `default` \| null |
| `recurrence {seriesKey, frequency, rule, daysOfWeek, localStartTime, until, text, origin}` | one document per occurrence | `seriesKey` is what the planner uses to avoid the same weekly night twice |
| `duration {medianMin, sigma, p75Min, source}` | lognormal stay length | `p75Min` is the planner's default stay; `source` `event_times` \| `source_field` \| `google_typical` \| `trail_model` \| `llm` \| `category_prior` |
| `price {min, max, currency, tier, isFree}` | tier 0–4 or null | **null means unknown, never free**; tiers are the city's `price_tiers` bounds (Atlanta `[0, 15, 40, 80]`) |
| `rating`, `ratingCount`, `popularity` | numbers or null | Google rating; RA / PredictHQ popularity 0..1. The planner only schedules places rated 4 or more (unrated hikes excepted) |
| `url`, `ticketUrl`, `imageUrl` | strings or null | `ticketUrl` or a known price makes a stop "bookable" |
| `blurb {text, sources, grounded, model, inputHash, generatedAt}` | 50–90 words | the detail-screen paragraph; optional |
| `embeddingText`, `embeddingTextHash`, `embeddingTextMeta`; `embedding`, `embeddingModel`, `embeddingMeta` | see [EMBEDDINGS.md](EMBEDDINGS.md) | the vector is valid only while `embeddingMeta.textHash == embeddingTextHash` |
| `sourceKeys`, `sources [{name, id, url, fetchedAt}]`, `googlePlaceId` | provenance | `sourceKeys` is the unique upsert key; a merged activity carries several |
| `createdAt`, `updatedAt`, `expiresAt` | | events: `expiresAt = (end ?? start + p75) + 6 h` for the TTL index; places: null |

### Vocabularies

**category**: `museum, gallery, park, garden, zoo_aquarium, landmark, viewpoint, hike, cafe, restaurant,
bar, nightclub, live_music, comedy, theater, cinema, sports_event, rec_venue, market, shopping, festival,
class_workshop, tour, community_event, other`. Source categories map through
`dataingestion/seeds/category_maps.yaml` (Ticketmaster segment/genre, Google `primaryType`, RA genre);
unmapped values go to the LLM classifier. Google food places (restaurants, cafés, bakeries) are left
unmapped on purpose, so the live Atlanta catalog has none. The planner also schedules `bakery`,
`dessert`, `food_hall` and `brewery` places when a catalog carries them; the pipeline does not produce
those categories yet.

**tags**: `indoor, outdoor, free, cheap, splurge, low_energy, medium_energy, high_energy, solo_friendly,
date, group, late_night, daytime, family, 21_plus, food, drinks, music, art, nature, active, learning,
touristy, local_favorite`. The planner's quick-pick facets and age rules are built from these
([PLANNER.md](PLANNER.md)).

## Indexes and the demo TTL exception

| Index | Created by | Purpose |
|---|---|---|
| `{location: "2dsphere"}` | `ingest init-db`; the API's `ensure-indexes` adds it if missing | radius queries for places, alternatives, `/places/reverse` |
| `{city: 1, kind: 1, start: 1}` | both | the planner's event query |
| `{name: 1}` | the API | `/places/search` |
| `{sourceKeys: 1}` unique, `{category: 1}`, `{"recurrence.seriesKey": 1}` sparse | `ingest init-db` | idempotent upserts, category filters, series |
| `{expiresAt: 1}` TTL (`expireAfterSeconds: 0`) | `ingest init-db` on `activities` | past events delete themselves 6 h after they end |

The API's `EnsureIndexes` never creates or drops TTL indexes on the catalogs. The demo catalog is the
exception to expiry: its events are dated 2026-09-26 to 2026-10-03 and must outlive that week, so the
runbook runs `sidequestz-admin drop-ttl demo_activities`, which removes any index on that collection
with `expireAfterSeconds`. `drop-ttl activities` refuses without `--force`.

The ingestion spec also describes an Atlas Vector Search index; the deployment uses a self-hosted
`mongod`, so similarity is computed in the API over the vectors of a filtered candidate set instead.

## Sources

| Source | Gives | Cost and limits |
|---|---|---|
| Ticketmaster Discovery API v2 | ticketed events (concerts, comedy, sports, theatre), 7-day horizon, 28 days for series detection | 5,000 calls/day, 2 req/s; free |
| Google Places API (New) | places with `weeklyHours`, price level, rating, `primaryType`; food places deliberately excluded | needs billing; Nearby and Text Search go through the quota counter (900/month each) |
| OpenStreetMap via Overpass + OpenTopoData | hikes: named routes and joined paths within `hikes_radius_km`, elevation profiles, Tobler duration | free; rate-limited; 1000 elevation calls/day |
| Resident Advisor | club nights and electronic music (`area_id` per city) | free, cached 1 h |
| SerpApi Google Events, web calendars (iCal / JSON-LD), Eventbrite | broader event coverage | SerpApi 200/month cap; the others are scrapers with a 1 req/s per-domain limit and disk caches |
| Nominatim → Google Geocoding | coordinates and address parts | Nominatim 1 req/s and ≤ 300 lookups per run; Google 9,000/month once billing is on |
| Muse (Meta Model API, `web_search`) then Gemini | web research notes per activity, then the `blurb` and the eight-section `embeddingText` | Muse pay-as-you-go with a daily cap; Gemini free tier with per-model daily limits; research is cached in `research` |

Dedupe: candidates within 200 m, then event matching on local date, start within 30 min, venue and
title similarity (or embedding cosine ≥ 0.90); place matching on `googlePlaceId` or distance ≤ 75 m plus
name similarity; merges union the source keys and apply per-field precedence (hours google > osm > nps;
price ticketmaster > eventbrite > … > llm).

## Commands

```sh
cd dataingestion
python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt     # keys in the root .env, see .env.example
.venv/bin/python -m ingest init-db                          # indexes, idempotent
.venv/bin/python -m ingest run ticketmaster [--dry-run]     # one adapter; dry runs normalize to out/ without writing
.venv/bin/python -m ingest run-all                          # every adapter enabled in cities/<slug>.yaml
.venv/bin/python -m ingest pipeline                         # fetch → backfill → blurbs/embedding text → coverage
.venv/bin/python -m ingest crawl [--once --now events]      # hourly events, weekly places; new activities only
.venv/bin/python -m ingest embed-text --kind event          # eight-section text (web research + Gemini)
.venv/bin/python -m ingest blurb --kind event               # the detail-screen paragraph
.venv/bin/python -m ingest stats | quota                    # coverage by kind/category/source; API budgets
.venv/bin/python -m ingest snapshot export out/sample.json --limit 50
.venv/bin/python -m ingest snapshot import <file>           # upserts by _id into `activities`
.venv/bin/python -m pytest                                  # fixture tests; Mongo tests skip when Mongo is down
```

All commands take `--city <slug>` (default `atlanta` from `config.yaml`). Vectors are not part of the
pipeline: after `embed-text`, the ML service's timer (`tools/embed_missing.py`) or the Raven backfill
computes them ([EMBEDDINGS.md](EMBEDDINGS.md)).

## The demo snapshot

`dataingestion/demo/saltlight_harbor.json` is generated by `python -m demo.generate` from
`dataingestion/demo/generate.py`: a fictional seaside city with eight neighbourhoods, hashed but
deterministic coordinates and addresses, 45 events on fixed dates (`DAY0` = Saturday 2026-09-26, through
2026-10-03) and 55 places with weekly hours. Its `cities/saltlight.yaml` has no live sources and a demo
start at Seaside Market Square.

Load it into `demo_activities` (the pipeline's `snapshot import` only targets `activities`):

```sh
# a local Docker Mongo
docker cp dataingestion/demo/saltlight_harbor.json sq-mongo:/tmp/ && docker exec sq-mongo \
  mongoimport --db freetime --collection demo_activities --jsonArray --file /tmp/saltlight_harbor.json
# the VPS: mongoimport --db freetime --collection demo_activities --jsonArray --file saltlight_harbor.json
cd Backend && go run ./cmd/sidequestz-admin ensure-indexes && go run ./cmd/sidequestz-admin drop-ttl demo_activities
```

`mongoimport` reads the file's Extended JSON (`$date`, `$oid`) as real dates and ObjectIds; a reimport
with `--upsert` replaces documents by `_id`. That is enough for `seed-demo` (it needs at least three
Saltlight places) and for planning, but the JSON carries no `embeddingText` and no vectors, and
`embed_missing` only embeds documents that already have a text: until texts exist, the demo city ranks
by priors (ratings, popularity, facet matches) instead of taste. The deployed collection got its texts
from the pipeline's `embed-text` prompt (Gemini, without web research) and its vectors from the Raven
backfill (`ml/datagen/mongo_backfill.py`). To move the demo catalog to another database, copy the
collection with its texts and vectors instead of reimporting the JSON:

```sh
mongodump --db freetime --collection demo_activities --out /tmp/demo-dump
mongorestore --db freetime --collection demo_activities /tmp/demo-dump/freetime/demo_activities.bson
```

The seed's own tests load the JSON into a throwaway database; the planner's integration test copies
`freetime.demo_activities`.

## Caches and quotas

HTTP responses are cached under `dataingestion/cache/<adapter>/` (Ticketmaster and RA 1 h, Google and
Overpass 7 days, geocoding and elevation 90 days), so reruns while developing spend nothing. Paid or
capped APIs go through `quota.reserve(api, n)`, an atomic counter in the `quota` collection with caps
below the free allowance (Google Nearby/Text Search 900/month, Geocoding and Route Matrix 9,000/month,
SerpApi 200/month, Gemini grounded calls per day from `config.yaml`, Muse `daily_cap`); several keys per
API rotate through `KeyRing`. `--dry-run` estimates usage without reserving anything. `python -m ingest
quota` prints the counters.

## Attribution

- Trails and any OSM-derived place data: © OpenStreetMap contributors, under the ODbL. The app must
  show the notice wherever OSM data appears.
- Elevation: OpenTopoData (NED 10 m in the US, EU-DEM 25 m in Germany).
- Ticketmaster, Google Places, Resident Advisor, SerpApi and Eventbrite data are used under their terms
  and never redistributed; the catalog is not a public dataset.
- Research notes and blurbs cite their source URLs (`blurb.sources`, the `research` collection).
- Saltlight Harbor and everything in `demo/` is invented; any resemblance to real venues is coincidental.
