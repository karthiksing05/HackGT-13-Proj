# Data ingestion

Fills MongoDB with activities (events + places) for the planner. The full design is in
[DATA_COLLECTION_SPEC.md](DATA_COLLECTION_SPEC.md).

## How this feeds the app

- **`activities` is the planner's catalog.** Every document the adapters write (events, places, trails)
  is a candidate for `POST /plans/generate`: the Go API filters by `city`, `kind`, `location`, `start`,
  `weeklyHours`, `price`, `tags` and, for places, a `rating` of 4 or more (unrated trails excepted), then
  scores the survivors. The planner can schedule restaurants and cafés, but the Google Places type
  groups in `config.yaml` leave food out, so the Atlanta catalog has none yet. The app never reads the collection
  directly. The schema and indexes are summarized in [../docs/DATA.md](../docs/DATA.md).
- **`embeddingText` is what gets embedded.** `embed-text` writes the eight-section text and
  `embeddingTextHash`; the vector itself is computed later by the ML service's `tools/embed_missing.py`
  timer or the Raven backfill, with no instruction prefix, and stored in `embedding` / `embeddingMeta`.
  Editing the prompt changes the hash, which re-embeds ([../docs/EMBEDDINGS.md](../docs/EMBEDDINGS.md)).
- **The demo city is separate.** `demo/saltlight_harbor.json` (from `python -m demo.generate`) is a
  fictional city in the same schema; it lives in `demo_activities`, which only the demo account reads.
  `snapshot import` targets `activities`, so load it with `mongoimport --collection demo_activities`
  ([../docs/DATA.md](../docs/DATA.md#the-demo-snapshot)). Its events run from Sep 26 to Oct 2, 2026 (New York time).
  The JSON has no `embeddingText` or vectors; the deployed copy got its texts from this pipeline's
  prompt and its vectors from the Raven backfill.
- **Not deployed.** The pipeline and the crawler run from a laptop against a local Mongo or a tunnel to
  the VPS; the VPS holds a snapshot of the Atlanta catalog (vectors backfilled on 2026-09-26) and the
  demo city. Deploying the crawler is on the [roadmap](../docs/ROADMAP.md).

## Setup

```sh
cd dataingestion
python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt
```

Keys live in the repo-root `.env` (see `.env.example`). Mongo defaults to
`mongodb://localhost:27017`, db `freetime`; set `MONGODB_URI` for Atlas.

## Commands

```sh
.venv/bin/python -m ingest init-db                          # indexes (idempotent)
.venv/bin/python -m ingest run ticketmaster                 # next 7 days of events
.venv/bin/python -m ingest run ticketmaster --dry-run       # normalize to out/ without writing
.venv/bin/python -m ingest run google_places --dry-run      # estimate paid calls vs quota, no requests
.venv/bin/python -m ingest run google_places                # needs GOOGLE_MAPS_API_KEY with billing on
.venv/bin/python -m ingest run osm_trails --dry-run         # OSM hikes to out/ (elevation from cache only)
.venv/bin/python -m ingest run-all                          # every adapter enabled in cities/<slug>.yaml
.venv/bin/python -m ingest pipeline                         # fetch all sources -> backfill -> blurbs/embedding text -> coverage
.venv/bin/python -m ingest stats | quota
.venv/bin/python -m ingest crawl                            # scheduler: new events hourly, places weekly (runs until stopped)
.venv/bin/python -m ingest crawl --once --now events         # one check right now (cron/launchd friendly)
.venv/bin/python -m ingest blurb --kind event             # web research + paragraph for every event (Gemini)
.venv/bin/python -m ingest blurb --limit 3 --dry-run       # try a few; blurbs go to out/, not the DB
.venv/bin/python -m ingest embed-text --kind event        # structured text for the ML embeddings (same web research)
.venv/bin/python -m ingest snapshot export out/sample.json --limit 50
.venv/bin/python -m pytest                                  # fixture tests; Mongo tests skip if Mongo is down
```

Responses are cached under `cache/<adapter>/` (Ticketmaster 1h, Resident Advisor 1h, Google 7 days,
Overpass 7 days, geocoding and elevation 90 days), so reruns while developing don't spend quota. Geocoding (Google Geocoding API,
counted against the `google_geocode` quota) only calls the network when Mongo is reachable.

## Crawler (new activities only)

`ingest crawl` (`ingest/crawler.py`, config in `config.yaml` -> `crawler`) checks sources on a
schedule: Ticketmaster and Resident Advisor every hour, Google Places and OSM trails weekly. Each
fetched activity is checked against the DB first. A matching source key means it's known and
left untouched. The same city, normalized name and start within 30 minutes (from any source)
means it's another listing of a known event, so only its source key is attached. Only
genuinely new activities are inserted, researched and written up (blurb + embedding text). A new
showing of a show that's already researched reuses that research instead of a new web search.
New activities whose write-up failed (e.g. Gemini quota) are retried for `retry_hours`.
Last-run times per city/tier are kept in the `crawl_state` collection, so restarting the
crawler doesn't redo the weekly places check early. Per-source counts land in `runs` as usual.

## Web research, blurbs and embedding text

Research and writing are separate steps:

1. **Research (Muse):** `ingest/agent/muse.py` calls Muse Spark on the Meta Model API
   (`POST /v1/responses` with the `web_search` tool) to look up each event, its performers and
   venue. It returns factual notes plus cited source URLs, cached in the `research` collection
   (one per activity per provider; quota: `muse_research`, see `ingest quota`).
2. **Writing (Gemini):** the Muse notes and the event's own fields fill `{{EVENT_DATA}}` in
   `ingest/agent/prompts/event_embedding_text.md`, and Gemini writes the ML text.
   - `embed-text` -> `activities.embeddingText` (+ `embeddingTextHash`, `embeddingTextMeta`).
     Output is checked in code (allowed sections only, no placeholders/URLs/sentences, no empty
     sections) and regenerated once on failure. Editing the prompt file regenerates everything.
   - `blurb` -> `activities.blurb` (the 50-90 word paragraph for the detail screen).

`config.yaml` -> `blurb.research_provider` switches research between `muse` (default),
`gemini` (Google Search grounding, 20 requests/day per model on the free tier) and `claude`.

**Research by Claude Code (no Muse key needed).** The `claude` provider never calls an API: an
outside agent (e.g. Claude Code with web search) writes the notes into a file and they are imported.

```sh
.venv/bin/python -m ingest research todo --limit 10     # out/research_todo_<city>.json: facts, empty notes/sources
# ...have Claude Code fill in "notes" (bullet facts) and "sources" (URLs) for each row...
.venv/bin/python -m ingest research import out/research_todo_atlanta.json
.venv/bin/python -m ingest blurb --research-provider claude          # only activities with imported notes
.venv/bin/python -m ingest embed-text --research-provider claude
```

`todo` lists upcoming activities without `claude` notes, one row per show (repeat showings are
folded together), soonest first. `import` stores each row's notes for every showing of that show,
skips rows with empty notes, and warns if an activity's facts changed since the export. Writes use
`gemini-3.5-flash-lite` (fallback `gemini-flash-latest`).

The Muse key needs billing on the Meta developer account: without it every call returns
`402 billing_not_configured`, research turns off for the run, and Gemini writes from our own
fields only. Those outputs regenerate automatically once research exists.

## Trails (OpenStreetMap)

`osm_trails` (`ingest/adapters/osm_trails.py`) makes hikes from one Overpass query within
`hikes_radius_km` of the city centre: named hiking routes, plus named paths/tracks/footways/cycleways
joined into whole trails (pieces split by road crossings are joined; junk names like "unmarked trail"
or "Bronner Road" are dropped). Each trail is a `place` with `category: hike`, a `trail` subdocument
(length, climb, loop, GeoJSON line), `venueName` = the park it's in, and assumed daily hours
(`osm_trails.hours`, default 06:00-18:00, `hoursSource: default`). Duration is Tobler's hiking
function over an OpenTopoData elevation profile (`ingest/enrich/trails.py`, quota `opentopodata`,
1000/day); if elevation is unavailable the trail is timed as flat ground. Settings are in
`config.yaml` -> `osm_trails`. OSM data needs "© OpenStreetMap contributors" shown in the app.
