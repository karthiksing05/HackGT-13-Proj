# Data Collection Spec — HackGT 13 Free-Time Planner

> **Audience:** §0 is for the team. §1 onward is for the coding agent building the ingestion layer; read all of it before writing code.
> **Scope:** this is a 36-hour hackathon build (hacking started Friday, Sep 25 at 8pm; judging is at Sunday's expo). Working data in Mongo beats completeness. Anything marked **VERIFY** is an expected shape or limit that must be checked against current docs or a live response before you rely on it.

---

## 0. Kickoff checklist (team, tonight)

### Keys and accounts (split these up, ~30 min)

- [ ] **Ticketmaster** developer key. It's instant, and it's the first adapter, so get it first.
- [ ] **Gemini** API key from AI Studio (enrichment, embeddings, blurb agent). Note the key's free-tier rate limits and its Google Search grounding allowance.
- [ ] **MongoDB Atlas** free cluster (Vector Search works on free clusters for prototyping). Create a DB user, open network access for the weekend, and copy the connection URI. Ask at the MLH table about Atlas credits.
- [ ] **SerpApi** free account (250 searches/month).
- [ ] **Google Cloud:** create a project and enable billing (new accounts usually get free-trial credit, **VERIFY**). Enable Places API (New), Geocoding API and Routes API. Create one API key restricted to those three. Set a small budget alert and per-API daily quotas. If this stalls, move on: nothing tonight depends on Google (§10).
- [ ] Optional: PredictHQ trial (start it now if we want it), NPS key, Hugging Face token for the Foursquare dataset.
- [ ] Put every key in one `.env`, shared by DM and never committed. Commit `.env.example`.

### Team decisions (whole team, ~20 min; these unblock everyone)

- [ ] Walk the planner, ML and frontend owners through the activity schema (§4). It's the contract between us. Confirm that `location` is `[lng, lat]`, and agree on the `weeklyHours` encoding, the `duration` fields, and the `address`, `recurrence` and `blurb` shapes.
- [ ] Pick the embedding model and dimension with the ML owner, since user vectors and activity vectors must live in the same space. Default: Gemini embeddings at 768 dims.
- [ ] Agree with the ML owner that ratings on recurring events are stored against `recurrence.seriesKey`, so liking this week's trivia night carries over to next week's.
- [ ] Lock the demo scenario: Atlanta, starting near Klaus/Midtown, with a free window during Sunday's expo (e.g. 1–5pm). Record it under `demo` in `cities/atlanta.yaml`. The data only has to be great for that area and window.
- [ ] Decide which laptop runs ingestion and who holds the demo snapshot.

### Seeds (data lead, before the agent needs them)

- [ ] Write `cities/atlanta.yaml` (§3).
- [ ] Fill `seeds/atlanta/sources.yaml` with ~10 real calendar URLs copied from a browser: BeltLine events, GT campus calendar, Discover Atlanta, High Museum, Atlanta-Fulton library, Piedmont Park, Creative Loafing, and a few venue calendars.
- [ ] **Resident Advisor:** open RA's Atlanta events page, find the `graphql` request under DevTools → Network, save its query to `seeds/ra_event_listings.graphql` and put its area ID in `cities/atlanta.yaml` (§5.5).
- [ ] **Eventbrite:** open the Atlanta browse page, view source, confirm `window.__SERVER_DATA__` is there, and note the city slug (e.g. `ga--atlanta`) and the category slugs we want (§5.6).

### First build steps

- [ ] Hand this spec to the coding agent and start M0 (§10).
- [ ] Get Ticketmaster events into Mongo tonight, then export ~50 docs as JSON so frontend and planner can build against real data while ingestion continues.
- [ ] Before calling it a night, start a full `run-all` so Saturday begins with a populated DB.

---

## 1. Goal

Fill MongoDB with **activities** for a target city, normalized into one schema:

- **Events**: time-bound (concerts, comedy, club nights, markets, workshops).
- **Places**: available within opening hours (museums, parks, cafés, arcades).
- **Trails**: hikes, stored as places with a `trail` subdocument.

Each activity carries what the itinerary planner needs: location, time window (event times or weekly opening hours), expected duration, price, category/tags, an embedding, and (for the top activities) an AI-written blurb.

The planner (owned by teammates) solves a knapsack/orienteering-style problem: fit the highest-value set of activities into a user's free window, including travel time. **After location, the most important fields are the time window and the duration**, and sources omit these most often.

Product context: the user says where they are and how much free time they have; the app builds a timed plan (events + places + transit) and learns from post-outing ratings. Atlanta is the demo city. City values live in config (§3), so adding Berlin for the pitch is a config file, not a code change.

---

## 2. Ground rules

1. **Secrets from env only.** `.env` is gitignored; `.env.example` is committed.
2. **Idempotent writes.** Upsert by `sourceKeys` (§4). Re-running any adapter creates zero duplicates.
3. **Fixture-first adapters.** Save one live response to `fixtures/<adapter>/`, write the normalizer against it, and add one smoke test. This keeps us from debugging against the network at 3am.
4. **Never invent source URLs, IDs or GraphQL queries.** They come from `seeds/`, `cities/`, or a verified live response.
5. **Guard the limited APIs.** Google SKUs, SerpApi and Gemini grounded calls go through the quota counter (§7). Never exceed a cap.
6. **Keep scrapers from getting us blocked mid-hackathon:** ~1 request/second per domain, and cache responses to `cache/<adapter>/` so development re-uses them instead of re-fetching. Don't burn time fighting CAPTCHAs or bot walls. If a site blocks us, drop it and move on.
7. **City values live in `cities/<slug>.yaml`,** not in code (coordinates, bbox, query text, source IDs).
8. **Fail soft.** A broken source logs an error to `runs`, and the rest of `run-all` keeps going. The demo runs from a snapshot and never depends on live fetching.

---

## 3. City configuration

Every command takes `--city <slug>`; if it's omitted, `default_city` from `config.yaml` is used.

**`config.yaml` (global):**

```yaml
default_city: atlanta
events_horizon_days: 7
series_detection_days: 28     # fetch this far ahead to spot recurring series; only the horizon is inserted (§6.7)
refresh:                      # only used by the stretch scheduler (§8)
  events_every_hours: 6
  places_every_days: 7
```

**`cities/<slug>.yaml` (example):**

```yaml
slug: atlanta
name: Atlanta
country_code: US              # ISO 3166-1 alpha-2
default_region: GA            # fallback for address.region; null if the bbox spans several states
language: en                  # SerpApi hl, Places languageCode
timezone: America/New_York
currency: USD                 # prices stored in local currency; no FX conversion
price_tiers: [0, 15, 40, 80]  # upper bounds for tiers 0-3; above the last = tier 4
center: { lat: 33.7490, lng: -84.3880 }
bbox: { south: 33.62, west: -84.55, north: 33.92, east: -84.25 }
hikes_radius_km: 60
elevation_dataset: ned10m     # OpenTopoData; ned10m is US-only
neighborhoods: [Midtown, Downtown, Old Fourth Ward, Inman Park, West Midtown, Decatur, Buckhead]
demo:                         # used by `blurb --top` and the acceptance checks
  start: { lat: null, lng: null }   # Klaus building; fill in from a map
  window_local: ["Sun 13:00", "Sun 17:00"]
query_terms:
  places: [rooftop bars, live music venues, board game cafe, escape room, things to do]
  events: [concerts, comedy, free events, art exhibitions, markets, nightlife, trivia night, workshops]
ticketmaster: { radius_km: 40 }
resident_advisor: { area_id: null }                  # from DevTools (§5.5)
eventbrite:
  slug: ga--atlanta                                  # VERIFY
  categories: [all-events, music--events, food-and-drink--events, arts--events, nightlife--events]   # VERIFY slugs
nps_park_codes: []
adapters:
  ticketmaster: true
  google_places: true         # needs billing (§5.2)
  osm_places: false           # fallback when Google billing is off (§5.2)
  serpapi_events: true
  web_calendar: true
  resident_advisor: true
  eventbrite: true
  osm_trails: false
  google_typical_time: false
  predicthq: false
  nps: false
  fsq_os: false
```

**Adding a city:** copy `atlanta.yaml` and edit it by hand. Take `center` and `bbox` from a Geocoding or Nominatim lookup (clamp the bbox to ~40 × 40 km), `timezone` from `timezonefinder`, and switch off adapters with thin coverage there (Ticketmaster outside North America, NPS outside the US). RA coverage is much stronger in Berlin than Atlanta.

**City scoping:** `activities` and `runs` carry a `city` slug. The quota counter (§7) is shared across cities.

---

## 4. Data model

### 4.1 `activities`

```jsonc
{
  "_id": "ObjectId",
  "kind": "event" | "place",
  "city": "atlanta",
  "name": "string",
  "summary": "string",               // ≤25 words, LLM-written, factual (list rows)
  "description": "string|null",      // raw source text, truncated to 2,000 chars
  "category": "string",              // fixed vocabulary, §4.3
  "tags": ["string"],                // fixed vocabulary, §4.3
  "location": { "type": "Point", "coordinates": [lng, lat] },   // GeoJSON: lng FIRST
  "address": {                       // null if unknown
    "formatted": "string|null",      // one line, as the source gives it; geocode cache key
    "street": "string|null",
    "locality": "string|null",       // city or town, e.g. "Decatur"
    "region": "string|null",         // state/province short code, e.g. "GA" (ISO 3166-2 without the country prefix)
    "postalCode": "string|null",
    "countryCode": "US"              // ISO 3166-1 alpha-2
  } | null,
  "venueName": "string|null",

  // --- Time ---
  "start": "ISODate|null",           // events: UTC
  "end": "ISODate|null",             // events: UTC, null if unknown
  "attendance": "fixed_start" | "drop_in",
      // fixed_start: must arrive at start (concert, show, class)
      // drop_in: can come any time in [start, end] (market, exhibition, festival, club night)
  "timezone": "America/New_York",
  "weeklyHours": [ { "open": 540, "close": 1260 } ] | null,
      // places only. Minutes since Sunday 00:00 local (0..10079), day 0 = Sunday.
      // If close < open the interval wraps past Saturday 23:59. Open 24/7 = [{open:0, close:10080}].
  "hoursSource": "google" | "osm" | "nps" | null,

  // --- Recurring events (§4.6); null for one-off events and places ---
  "recurrence": {
    "seriesKey": "string",           // shared by every occurrence; ratings attach here
    "frequency": "daily" | "weekly" | "biweekly" | "monthly" | "irregular",
    "rule": "string|null",           // RFC 5545 RRULE when the source gives one, e.g. "FREQ=WEEKLY;BYDAY=TU"
    "daysOfWeek": [2],               // 0 = Sunday, same as weeklyHours
    "localStartTime": "19:00",       // "HH:MM" local; null if it varies
    "until": "ISODate|null",         // last occurrence; null if open-ended or unknown
    "text": "string|null",           // the source's wording, e.g. "Every Tuesday at 7pm"
    "origin": "source_rule" | "source_series" | "llm" | "detected"
  } | null,

  // --- Duration: how long a person typically stays, lognormal (median = e^mu) ---
  "duration": {
    "medianMin": 90,
    "sigma": 0.3,
    "p75Min": 110,                   // = medianMin * exp(0.674 * sigma); planner default
    "source": "event_times" | "source_field" | "google_typical" | "trail_model" | "llm" | "category_prior"
  },

  // --- Price (local currency) ---
  "price": { "min": null, "max": null, "currency": "USD", "tier": 0, "isFree": null },   // tier 0..4 or null

  // --- Quality ---
  "rating": null, "ratingCount": null,
  "popularity": null,                // 0..1 when a source gives rank/attendance

  // --- Trails only (category = "hike") ---
  "trail": {
    "lengthKm": 5.2, "ascentM": 180, "descentM": 180,
    "loop": true,                    // false = out-and-back; duration covers the round trip
    "geometry": { "type": "LineString", "coordinates": [[lng, lat], "..."] }
  } | null,

  // --- Links ---
  "url": "string|null", "ticketUrl": "string|null", "imageUrl": "string|null",

  // --- AI blurb (§6.6) ---
  "blurb": {
    "text": "string",                // 50–90 words, shown on the detail screen
    "sources": ["url"],              // pages the agent used (grounding metadata)
    "grounded": true,                // false = written from our own fields only
    "model": "string",
    "inputHash": "string",           // regenerate only when the facts packet changes
    "generatedAt": "ISODate"
  } | null,

  // --- Provenance / dedupe ---
  "sourceKeys": ["ticketmaster:G5vYZ9...", "google:ChIJ..."],   // unique multikey index
  "sources": [ { "name": "ticketmaster", "id": "string", "url": "string", "fetchedAt": "ISODate" } ],
  "googlePlaceId": "string|null",

  // --- ML ---
  "embedding": [0.0],
  "embeddingModel": "string",
  "embeddingTextHash": "string",     // recompute embedding only when this changes

  "createdAt": "ISODate",
  "updatedAt": "ISODate",
  "expiresAt": "ISODate|null"        // TTL. events: (end ?? start + p75) + 6h. places: null.
}
```

### 4.2 Indexes (`ensure_indexes()` must be idempotent)

| Index | Spec |
|---|---|
| Geo | `{ location: "2dsphere" }` |
| Dedupe/upsert | `{ sourceKeys: 1 }`, **unique** |
| City + time queries | `{ city: 1, kind: 1, start: 1 }` |
| Category | `{ category: 1 }` |
| Series | `{ "recurrence.seriesKey": 1 }`, sparse |
| TTL | `{ expiresAt: 1 }`, `expireAfterSeconds: 0` |
| Vector (Atlas Search) | name `activities_embedding`, `vector` on `embedding` (cosine, `numDimensions` = `EMBEDDING_DIM`), `filter` on `city`, `kind`, `category`, `price.tier` |

### 4.3 Fixed vocabularies

**category:** `museum, gallery, park, garden, zoo_aquarium, landmark, viewpoint, hike, cafe, restaurant, bar, nightclub, live_music, comedy, theater, cinema, sports_event, rec_venue (bowling/climbing/arcade/escape room), market, shopping, festival, class_workshop, tour, community_event, other`

**tags:** `indoor, outdoor, free, cheap, splurge, low_energy, medium_energy, high_energy, solo_friendly, date, group, late_night, daytime, family, 21_plus, food, drinks, music, art, nature, active, learning, touristy, local_favorite`

Source-category → our category maps live in `seeds/category_maps.yaml` (Google `primaryType`, Ticketmaster segment/genre, Eventbrite category, RA genre, OSM tags, PredictHQ category, schema.org Event subtype). Unmapped values go to the LLM classifier (§6.3).

### 4.4 Source list (`seeds/<city>/sources.yaml`)

API adapters (Ticketmaster, Google, SerpApi, RA, Eventbrite) need no entry; their parameters come from the city config. Web calendars are listed here:

```yaml
- id: beltline                # sourceKeys use web:<id>:...
  url: https://...            # copied from a browser, never guessed
  enabled: true
  notes: ""
```

The adapter records which extraction step worked (§5.4) in `runs`.

### 4.5 Other collections

- `runs`: one per adapter run `{ source, city, startedAt, finishedAt, fetched, inserted, updated, merged, errors[] }`.
- `quota`: `{ api, period: "2026-09" | "2026-09-26", used, cap }`. Shared across cities.
- `travel_cache`: `{ originCell, destCell, mode, hourOfWeek, durationSec, distanceM, fetchedAt }`, TTL 7 days.

### 4.6 Recurring events

**One document per occurrence.** A weekly trivia night becomes one `event` doc per date inside the horizon. Each has its own `start`, `end` and `expiresAt`, and all of them carry the same `recurrence` subdocument. The planner keeps querying by `start` and never expands rules itself.

- **`sourceKeys`:** if the source gives each occurrence its own ID (Ticketmaster, Eventbrite), use it. If we expand a rule ourselves, use `<series source key>@<local date>`, e.g. `web:beltline:https://…/run-club@2026-09-29`. Reruns regenerate the same keys, so upserts stay idempotent.
- **`seriesKey`:** the source's series ID when it has one (`eventbrite:series:<id>`); for iCal/JSON-LD, the event's UID or URL (`web:<source_id>:<uid|url>`); for detected series, `series:` + sha1(normalized title | venue key) (§6.7).
- **Where recurrence comes from** (stored as `origin`):
  - `source_rule`: an iCal `RRULE` (§5.4 step 2) or a schema.org `eventSchedule` in JSON-LD (§5.4).
  - `source_series`: a source that links occurrences itself, e.g. Eventbrite series (§5.6).
  - `llm`: free text such as SerpApi's `when` ("Every Tuesday, 7–9 PM") or pages read by the LLM fallback. Add the `recurrence` fields (minus `seriesKey` and `origin`) to the LLM extraction schema.
  - `detected`: series detection (§6.7), for sources that list each date separately with no link (Ticketmaster, RA, SerpApi).
- **Expansion:** generate occurrences from `rule` for `[today, today + events_horizon_days]`, stop at `until`, and skip exception dates (`EXDATE`, `exceptDate`).
- **Shared work:** occurrences share `embeddingTextHash`, so copy the embedding from a sibling instead of re-embedding. Generate the blurb once per `seriesKey` and copy it to every occurrence (blurbs never mention dates).
- **Events vs. place hours:** a bar's weekly trivia night is a recurring *event* at that venue. The bar's opening hours stay in its *place* doc's `weeklyHours`.

---

## 5. Sources

Priority: **P0** required for the demo · **P1** should have · **P2** only if we're ahead.

### 5.1 Ticketmaster Discovery API v2: P0, ticketed events (build first)

- **Auth:** query param `apikey=$TICKETMASTER_API_KEY`.
- **Request:** `GET https://app.ticketmaster.com/discovery/v2/events.json?apikey=…&geoPoint=<geohash of city.center>&radius=<city.ticketmaster.radius_km>&unit=km&startDateTime=<ISO>Z&endDateTime=<ISO>Z&size=200&page=N&sort=date,asc` (**VERIFY** `geoPoint` vs deprecated `latlong`).
- **Limits:** 5,000 calls/day, 2 requests/second. Deep paging is capped at 1,000 items (`size × page`), so split the horizon into day-sized windows.
- **Coverage:** includes TicketWeb inventory, which many smaller clubs and music venues use.
- **Normalize from `_embedded.events[]` (VERIFY paths against a fixture):**
  - `id`, `name`, `url`.
  - `dates.start.dateTime`. Skip events with `dates.start.timeTBA` or status `cancelled`/`postponed`.
  - `dates.end.dateTime` if present.
  - `priceRanges[].min/max`.
  - `classifications[0].segment.name / genre.name / subGenre.name` → category.
  - `_embedded.venues[0]`: `name`; `location.latitude/longitude` (strings; cast to float); address from `address.line1` → `street`, `city.name` → `locality`, `state.stateCode` → `region`, `postalCode`, `country.countryCode` (**VERIFY** paths).
  - `images[]`: pick the widest 16:9.
  - `attendance: fixed_start`.
- `sourceKeys: ["ticketmaster:<id>"]`.

### 5.2 Google Places API (New): P0 once billing is on, places + hours + price + rating

- **Auth:** headers `X-Goog-Api-Key: $GOOGLE_MAPS_API_KEY`, `X-Goog-FieldMask: <mask>`. Needs a GCP project with billing enabled, even within free caps. **Billing isn't on yet.** Build this adapter against a fixture made from the documented response example, fail fast with a clear message on a missing key or a billing error, and make sure every other adapter runs without it.
- **Localization:** send `languageCode = city.language` and `regionCode = city.country_code`.
- **Endpoints:**
  - `POST https://places.googleapis.com/v1/places:searchNearby` with body `includedTypes`, `maxResultCount` (≤20), `rankPreference: POPULARITY`, `locationRestriction.circle {center, radius}`.
  - `POST https://places.googleapis.com/v1/places:searchText` with body `textQuery`, `locationRestriction.rectangle`, `pageSize`, `pageToken` (**VERIFY** pagination fields).
- **Field mask (bills at the Enterprise SKU; don't add fields):**
  `places.id,places.displayName,places.primaryType,places.types,places.location,places.formattedAddress,places.addressComponents,places.regularOpeningHours,places.priceLevel,places.priceRange,places.rating,places.userRatingCount,places.websiteUri,places.googleMapsUri,places.businessStatus`
- **Never request Atmosphere fields** (`reviews`, `editorialSummary`, `generativeSummary`, `goodForGroups`, `liveMusic`, `outdoorSeating`, `serves*`, …). They bill at a pricier SKU.
- **Billing:** per request, not per place. Nearby Search Enterprise and Text Search Enterprise are separate SKUs, each with 1,000 free requests/month. Quota caps: 900 each.
- **Crawl strategy (Nearby):** a grid of ~3 km cells over the city bbox (compute the count from the bbox; ≈110 for Atlanta). For each cell, query each type group below.
  - Culture, outdoors and fun: if a call returns 20 results (saturated), split the cell into 4 and recurse, down to a 750 m minimum.
  - Food & drink: **don't recurse.** Take the top 20 by popularity per cell; we want notable spots, not every restaurant.
- **Type groups (VERIFY each against Places API (New) "Table A"):**
  - culture: `museum, art_gallery, performing_arts_theater, tourist_attraction, historical_landmark`
  - outdoors: `park, hiking_area, botanical_garden, zoo, aquarium`
  - fun: `bowling_alley, amusement_center, movie_theater, video_arcade, karaoke`
  - food_drink: `cafe, bar, night_club, restaurant`
- **Text Search** budget goes to queries built from `"{term} in {area}"`, with `term` from `city.query_terms.places` and `area` from `city.neighborhoods` (or `city.name`).
- **Budget check:** `--dry-run` prints the estimated call count per SKU and aborts if it exceeds the remaining cap.
- **Normalize:**
  - Keep only `businessStatus == OPERATIONAL`.
  - `priceLevel` → tier: `PRICE_LEVEL_FREE`=0, `INEXPENSIVE`=1, `MODERATE`=2, `EXPENSIVE`=3, `VERY_EXPENSIVE`=4.
  - `priceRange.startPrice/endPrice.units` → `price.min/max`.
  - `regularOpeningHours.periods[]` → `weeklyHours`. Day 0 = Sunday. A period with no `close` means open 24h (**VERIFY**).
  - `formattedAddress` → `address.formatted`. From `addressComponents[]` by type: `street_number` + `route` → `street`, `locality` → `locality`, `administrative_area_level_1` (`shortText`) → `region`, `postal_code` → `postalCode`, `country` (`shortText`) → `countryCode`. `addressComponents` sits below the Enterprise fields already in the mask, so it shouldn't change the SKU (**VERIFY**).
  - `sourceKeys: ["google:<id>"]`; set `googlePlaceId`; `hoursSource: "google"`.

**No billing by Saturday morning? Use OSM places instead** (`osm_places` adapter, free). One Overpass query over the city bbox:

```
[out:json][timeout:120];
(
  nwr["tourism"~"^(museum|gallery|attraction|viewpoint|zoo|aquarium)$"]["name"]({south},{west},{north},{east});
  nwr["leisure"~"^(park|garden|bowling_alley|escape_game|amusement_arcade)$"]["name"]({south},{west},{north},{east});
  nwr["amenity"~"^(bar|pub|cafe|nightclub|cinema|theatre|arts_centre)$"]["name"]({south},{west},{north},{east});
);
out center tags;
```

Parse the `opening_hours` tag into `weeklyHours` with an opening-hours parser library (**VERIFY** which Python package works); leave it `null` if parsing fails. Address comes from the `addr:*` tags (`addr:housenumber` + `addr:street`, `addr:city`, `addr:state`, `addr:postcode`). No ratings or prices, so rank these lower. `hoursSource: "osm"`, `sourceKeys: ["osm:<type>/<id>"]`. When Google comes online later, dedupe (§6.4) merges the two.

### 5.3 SerpApi Google Events: P1, broad event coverage

- **Auth:** `api_key=$SERPAPI_API_KEY`.
- **Request:** `GET https://serpapi.com/search.json?engine=google_events&q=<query>&hl=<city.language>&gl=<city.country_code, lowercase>&htichips=date:week&start=0`.
- **Budget:** free plan is 250 searches/month; quota cap 200. Cache every response on disk and never repeat an identical query within 24h.
- **Query plan:** ≤40 searches per full refresh. Base query `events in {city.name}`, plus each term in `city.query_terms.events` combined with `htichips` of `date:today`, `date:weekend` or `date:week`. Max 3 pages per query.
- **Normalize from `events_results[]`:**
  - `title`.
  - `date.start_date` + `date.when`. `when` is free text; parse with `dateparser`, and on failure route to the LLM normalizer. Recurring phrasing ("Every Tuesday", "Weekly") also goes to the LLM normalizer, which returns the `recurrence` fields (§4.6).
  - `address[]` (joined into `address.formatted`; geocoding fills the parts), `venue.name`, `link`, `ticket_info[]` (first `link_type == "tickets"`), `description`, `thumbnail`.
  - **No coordinates**, so geocode (§6.1).
- `sourceKeys: ["serpapi:" + sha1(title|start_date|venue)]`. There's no stable ID. Expect heavy overlap with Ticketmaster and Eventbrite; dedupe handles it.

### 5.4 Web calendars: P1, local and non-ticketed events

- **Seeds:** `seeds/<city>/sources.yaml` (§4.4): tourism board calendar, museum calendars, parks, library, university calendar, venue sites, alt-weekly listings.
- **Extraction ladder.** Try each step in order and log the step that worked:
  1. **JSON-LD:** parse `<script type="application/ld+json">` (handle arrays and `@graph`). Accept `Event` and subtypes: `MusicEvent, TheaterEvent, ComedyEvent, Festival, ExhibitionEvent, SocialEvent, EducationEvent, DanceEvent, FoodEvent, SportsEvent`. If a listing page lacks JSON-LD but its detail pages have it, follow same-domain detail links (≤50 per run).
  2. **iCal:** find `<link type="text/calendar">`, `.ics` hrefs or `webcal://` links. Parse with `icalendar` and expand RRULEs within the horizon using `recurring_ical_events`. Keep the RRULE text in `recurrence.rule` (§4.6).
  3. **WordPress "The Events Calendar":** probe `{origin}/wp-json/tribe/events/v1/events?start_date=YYYY-MM-DD&per_page=50` (**VERIFY**).
  4. **Localist:** probe `{origin}/api/2/events?days=14&pp=100` (**VERIFY**).
  5. **LLM fallback:** convert HTML to clean text (`trafilatura`), chunk it, and extract with Gemini structured output using the §6.3 event schema. Cap tokens per run.
  6. **JS-rendered pages:** if the HTML is an empty shell, render once with Playwright and rerun steps 1 and 5 on the result.
- **Normalize schema.org Event:**
  - `name`, `startDate`, `endDate`, `description`, `image`, `url`.
  - `location` (`Place.name`, `geo`, and `address`). A `PostalAddress` maps `streetAddress`, `addressLocality`, `addressRegion`, `postalCode`, `addressCountry` to the §4.1 parts; a plain string goes to `address.formatted`.
  - `eventSchedule` (`repeatFrequency`, `byDay`, `startTime`, `endDate`, `exceptDate`) → `recurrence` with `origin: "source_rule"`.
  - `offers` (`price`, `priceCurrency`, `url`).
  - Skip `eventStatus = EventCancelled` and online-only `eventAttendanceMode`.
- `sourceKeys: ["web:<source_id>:" + (event url || sha1(name|startDate))]`.

### 5.5 Resident Advisor: P1, club nights and electronic music

RA's own site loads listings from a GraphQL endpoint, and that's what we call.

- **Request:** `POST https://ra.co/graphql` with a JSON body `{ operationName: "GET_EVENT_LISTINGS", variables, query }`.
  - `query`: read from `seeds/ra_event_listings.graphql`, copied from the browser's Network tab. Don't write it from memory.
  - `variables`: `filters.areas.eq = city.resident_advisor.area_id`, `filters.listingDate.gte/lte` = the horizon (local dates), `pageSize: 20`, `page: N`. Page until a page comes back short.
  - Headers: `Content-Type: application/json`, a normal browser `User-Agent`, and `Referer` set to the city's RA events page.
- **Normalize (VERIFY every path against a fixture):**
  - event `id`, `title`, `date`/`startTime`/`endTime` (check whether they're local or UTC), `contentUrl` → `url` (prefix `https://ra.co`).
  - `venue.name`, `venue.address` → `address.formatted` (one string; geocoding fills the parts); coordinates if the venue object has them, otherwise geocode (§6.1).
  - `artists[].name` → joined into `description` ("Lineup: …").
  - `attending` count → `popularity` (min-max scale within the city).
  - `attendance: drop_in`; category `nightclub` or `live_music` via `category_maps.yaml`; tags include `late_night`, `music`, usually `21_plus`.
- `sourceKeys: ["ra:<id>"]`.
- Set `adapters.resident_advisor: false` for cities where RA returns little.

### 5.6 Eventbrite browse pages: P1, community events, classes, markets

The official API can't search by city, so scrape the public browse pages.

- **Request:** `GET https://www.eventbrite.com/d/{city.eventbrite.slug}/{category}/?page=N` for each category in the city config, pages 1–5.
- **Extract:** the page embeds its results as `window.__SERVER_DATA__ = {…};`. Pull it with a regex (`window\.__SERVER_DATA__\s*=\s*(\{.*?\});`, DOTALL) and read `search_data.events.results[]` (**VERIFY** path).
- **Normalize (VERIFY against a fixture):**
  - `id`, `name`, `url`, `summary` → `description`, `image.url`.
  - `start_date` + `start_time`, `end_date` + `end_time` (local; convert with `city.timezone`).
  - `primary_venue.name`. From `primary_venue.address`: `address_1` → `street`, `city` → `locality`, `region` → `region`, `postal_code` → `postalCode`, `country` → `countryCode`, `localized_address_display` → `formatted`, and `latitude`/`longitude` (strings; cast to float) (**VERIFY** paths).
  - If the result belongs to an Eventbrite series (**VERIFY** the field, e.g. a series ID), set `recurrence` with `seriesKey: "eventbrite:series:<id>"` and `origin: "source_series"`.
  - `tickets_url` → `ticketUrl`. Price if present in the result (**VERIFY** field); otherwise leave `null` and let enrichment decide `isFree`.
  - Skip `is_online_event: true`.
  - Events spanning >4h (markets, fairs) → `drop_in`; otherwise `fixed_start`.
- **Fallback:** if `__SERVER_DATA__` moves, collect event URLs from the page and parse each event page's JSON-LD with the §5.4 step-1 normalizer (≤100 detail pages per run).
- `sourceKeys: ["eventbrite:<id>"]`.

### 5.7 Google Routes API: P1, travel times (a service for the planner)

- **Request:** `POST https://routes.googleapis.com/distanceMatrix/v2:computeRouteMatrix` with header `X-Goog-FieldMask: originIndex,destinationIndex,duration,distanceMeters,status,condition`. `travelMode`: `TRANSIT | WALK | DRIVE`.
- **Billing:** per element (origins × destinations). The Essentials SKU has 10,000 free elements/month; quota cap 9,000. **VERIFY** which modes bill as Essentials vs Pro, and the per-request element limits (lower for transit).
- Only call it for planner shortlists (≤15 candidates).
- **Cache** in `travel_cache`, keyed by H3 res-8 cell pair + mode + hour-of-week.
- **Fallback** when quota runs out or billing is off: haversine × 1.3 ÷ speed (walk 4.8 km/h, transit 18 km/h, drive 30 km/h).

### 5.8 Gemini: P1 enrichment and blurbs, P2 source discovery

- **Setup:** `google-genai` SDK. Env: `GEMINI_API_KEY`, `GEMINI_MODEL` (flash tier), `EMBEDDING_MODEL`, `EMBEDDING_DIM`. Don't hardcode model versions.
- **Uses:**
  - (a) Enrichment (§6.3), batched 20 activities per request with structured output.
  - (b) LLM extraction fallback (§5.4 step 5).
  - (c) Embeddings (§6.5).
  - (d) **Blurb agent** (§6.6).
  - (e) **Discovery agent** (P2): a grounded call with Google Search: "Find venues and organizations in {city.name} with public event calendars." Probe each candidate URL with the §5.4 ladder and append the ones where steps 1–4 succeed to `seeds/<city>/discovered.yaml` for a human to copy into `sources.yaml`.
- **Caution:** grounding tools and JSON-schema output may not combine in one call (**VERIFY**). When you need both, use two calls: grounded research, then schema extraction.

### 5.9 Google Maps "typical time spent": P2, better durations

Google Maps place pages show "People typically spend X to Y here" for many places. That's the best real-world duration signal we can get, and duration is the field the planner cares about most after location.

- Run only for the top ~100 places in the demo area by `ratingCount`.
- Try an existing open-source popular-times scraper first (**VERIFY** one still works; Google changes this markup often). If none does, load the place's `googleMapsUri` with Playwright and read the "typically spend" text.
- **Convert:** treat X and Y as the 25th and 75th percentiles of a lognormal: `medianMin = sqrt(X·Y)`, `sigma = ln(Y/X) / 1.349`. A single value ("up to 45 min") → `medianMin` = that value, `sigma` = the category prior's. `duration.source = "google_typical"`.
- This only ever improves on priors. If it breaks, the pipeline carries on without it.

### 5.10 OSM trails + elevation: P2, hikes with computed duration

Good pitch material (a real hiking-time model), but hikes aren't central to a Midtown Sunday-afternoon demo.

- **Overpass:** `POST https://overpass-api.de/api/interpreter`. Run one query at a time and cache results. Fill `{radius_m}`, `{lat}`, `{lng}` from `city.hikes_radius_km` and `city.center`:

  ```
  [out:json][timeout:180];
  (
    relation["route"~"^(hiking|foot)$"](around:{radius_m},{lat},{lng});
    way["highway"~"^(path|track)$"]["name"]["access"!~"^(private|no)$"](around:{radius_m},{lat},{lng});
    way["highway"="footway"]["name"]["footway"!="sidewalk"](around:{radius_m},{lat},{lng});
    way["leisure"~"^(park|nature_reserve)$"](around:{radius_m},{lat},{lng});
    way["boundary"="protected_area"](around:{radius_m},{lat},{lng});
  );
  out geom;
  ```

- **Build trails:** prefer route relations; otherwise group named ways by name inside the same park/reserve polygon and merge segments with `shapely.ops.linemerge`. Drop anything under 1 km. `loop = true` if the endpoints are within 100 m. Location = trail start, or the nearest `amenity=parking` within 300 m.
- **Elevation:** OpenTopoData `https://api.opentopodata.org/v1/{city.elevation_dataset}?locations=lat,lng|…` (≤100 points/request, ~1 request/s, **VERIFY** daily limit). Resample each line every 30 m and apply a moving median (window 3) before summing ascent.
- **Duration: Tobler's hiking function** per segment: `v = 6 · exp(−3.5 · |s + 0.05|)` km/h with `s = dh/dx`; `t = Σ dx / v`. Out-and-back = outbound + return (reversed slopes). `medianMin = t`, `sigma = 0.25`, `source = "trail_model"`.
- **Merge** with Google `hiking_area`/`park` places within 500 m: rating/hours from Google, geometry/length/duration from OSM.
- Show "© OpenStreetMap contributors" somewhere in the app (one line in the About screen).
- `sourceKeys: ["osm:relation/<id>"]` or `["osm:ways/" + sha1(sorted way ids)]`.

### 5.11 Optional extras: P2

- **PredictHQ** (14-day trial): `GET https://api.predicthq.com/v1/events/?within=15km@{lat},{lng}&active.gte=<date>&active.lte=<date>&category=concerts,festivals,performing-arts,sports,community,expos&limit=500`, header `Authorization: Bearer $PREDICTHQ_TOKEN` (**VERIFY** params). Useful fields: `duration` (seconds) → `source_field`; `rank`/`phq_attendance` → `popularity`; `location` as `[lng, lat]` (**VERIFY** order). Pull once.
- **NPS API** (US only): `GET https://developer.nps.gov/api/v1/thingstodo?parkCode=<codes>`, `/events`, `/parks`. For Atlanta, likely Chattahoochee River NRA and Kennesaw Mountain NBP (**VERIFY** codes).
- **Foursquare OS Places** (gated Hugging Face dataset `foursquare/fsq-os-places`, read with DuckDB): long-tail places with website/Instagram, but no hours, prices or ratings. Filter to the bbox, `date_closed IS NULL`, and arts/entertainment/outdoors/nightlife categories; match to existing places (≤75 m and name `token_set_ratio` ≥85) to add links.

### 5.12 Skipped (not worth hackathon time)

| Source | Why |
|---|---|
| AllTrails | Aggressive bot blocking; OSM + Tobler already gives us trails and durations. |
| Foursquare paid Places API | Hours are a paid Premium field. |
| Eventbrite official API | Can't search by city; the browse-page scrape (§5.6) covers more. |
| BestTime | Limited free credits; §5.9 covers dwell time. |

---

## 6. Pipeline

**Stages:** `fetch → normalize → expand recurrences → geocode (if no coords or no region) → classify/enrich → duration → dedupe/merge → detect series → embed → upsert`, then the **blurb agent** (§6.6) runs separately on a selected subset.

### 6.1 Geocoding

- Google Geocoding API when billing is on (Essentials SKU: 10,000 free/month; quota cap 9,000).
- **Until then:** Nominatim (OpenStreetMap). Max 1 request/second (they block faster clients), a descriptive User-Agent, ≤300 lookups per run.
- Cache every result by normalized `address.formatted`. Bias to the city bbox and drop results outside it.
- **Address parts:** fill `street`, `locality`, `region`, `postalCode` and `countryCode` from the geocoder's components (Google `address_components`; Nominatim with `addressdetails=1`, whose `ISO3166-2-lvl4` value such as `US-GA` gives the region code, **VERIFY**). Never overwrite parts the source already gave.
- **Region:** always store the short code. Convert full names with `pycountry`, scoped to the city's country ("Georgia" → `GA`). If a doc has coordinates but still no region, use `city.default_region`; if that's null because the metro spans states, reverse-geocode with Nominatim, cached by H3 res-7 cell.
- Events that still lack coordinates are held back and retried on the next run.

### 6.2 Duration resolution (first match wins)

1. **Event times:** if an event has both `start` and `end`:
   - Span ≤4h: `medianMin = end − start`, `attendance = fixed_start`, `source = event_times`.
   - Span >4h: treat `[start, end]` as a drop-in window and use the category prior for stay length.
2. **Source field:** PredictHQ `duration`, NPS duration → `source_field`.
3. **Google typical time** (§5.9) → `google_typical`.
4. **Trail model:** Tobler (§5.10).
5. **LLM estimate:** anchored on the category prior (§6.3).
6. **Category prior** (initial guesses; tune later):

| category | median min | sigma | | category | median min | sigma |
|---|---|---|---|---|---|---|
| museum | 120 | 0.35 | | live_music | 150 | 0.30 |
| gallery | 50 | 0.40 | | comedy | 100 | 0.20 |
| park | 60 | 0.50 | | theater | 150 | 0.20 |
| garden | 90 | 0.35 | | cinema | 140 | 0.15 |
| zoo_aquarium | 180 | 0.30 | | sports_event | 180 | 0.20 |
| landmark | 30 | 0.50 | | rec_venue | 90 | 0.30 |
| viewpoint | 20 | 0.50 | | market | 60 | 0.45 |
| cafe | 45 | 0.40 | | shopping | 60 | 0.50 |
| restaurant | 75 | 0.30 | | festival | 150 | 0.45 |
| bar | 90 | 0.40 | | class_workshop | 120 | 0.25 |
| nightclub | 180 | 0.35 | | tour | 90 | 0.30 |
| community_event | 90 | 0.40 | | other | 60 | 0.50 |

Always compute `p75Min = medianMin · exp(0.674 · sigma)`. Store the parameters, not just a number: the ML side will later update them from user feedback ("how long did you stay?").

### 6.3 LLM enrichment contract

Input: `name, kind, source category, venueName, description (≤1,500 chars), category prior`. Output, validated with pydantic:

```json
{
  "id": "string",
  "category": "<§4.3 enum>",
  "tags": ["<§4.3 enum>"],
  "attendance": "fixed_start | drop_in",
  "durationMedianMin": 90,
  "priceTier": null,
  "isFree": null,
  "summary": "≤25 words, factual, no hype"
}
```

Prompt rules: never invent facts, and use `null` when price is unknown. On a validation failure, retry once; if it fails again, fall back to priors and the mapped category. Only fill fields the source didn't provide; source data always wins over the LLM.

### 6.4 Dedupe and merge

- **Candidates:** `$geoNear` within 200 m (avoids O(n²)).
- **Event match:** all of the following:
  - same local date
  - |Δstart| ≤30 min
  - venue distance ≤150 m, or venue name `token_set_ratio` ≥85
  - title `token_set_ratio` ≥80, or embedding cosine ≥0.90
- **Place match:** same `googlePlaceId`, or distance ≤75 m and name `token_set_ratio` ≥85.
- **Merge:** union `sourceKeys` and `sources`, then apply field precedence:
  - hours: google > osm > nps
  - price: ticketmaster > eventbrite > web_jsonld > predicthq > serpapi > google > llm
  - coordinates: google > ticketmaster > eventbrite > resident_advisor > web_jsonld > predicthq > geocoded
  - address: google > ticketmaster > eventbrite > web_jsonld > osm > geocoded, part by part (a lower source can fill a part the higher one lacks)
  - recurrence: source_rule > source_series > llm > detected
  - rating: google
  - popularity: resident_advisor / predicthq, whichever is present
  - description: longest non-empty
- **Upsert flow:** find an existing doc by any incoming `sourceKeys` → merge; else run the fuzzy match → merge; else insert.
- Log every merge to `runs.merged` so a few can be spot-checked.

### 6.5 Embeddings

Embedding text: `"{name}. {summary} Category: {category}. Tags: {tags}. Venue: {venueName}."`
Hash this text into `embeddingTextHash` and re-embed only when the hash changes. The blurb is deliberately left out so regenerating blurbs never moves vectors.

### 6.6 Blurb agent (the paragraph on the detail screen)

`summary` (≤25 words) is fine for list rows, but the detail screen and the live demo need a short paragraph that tells someone what they're walking into. Source descriptions are often empty (SerpApi, RA) or pure marketing copy (Ticketmaster), so an agent researches the activity and writes one.

**Which activities get a blurb**

- **Batch:** `python -m ingest blurb --top 300` covers activities within 5 km of `city.demo.start` that are feasible in `city.demo.window_local`, ranked by `popularity`, or `rating × log(ratingCount)` for places.
- **Lazy:** `api.blurb(activity_id)` (§9) generates on first open and caches the result. The app shows `summary` while it loads.
- Skip any activity whose `blurb.inputHash` still matches.

**Agent loop (one activity)**

1. **Facts packet** from our own doc: name, kind, category, tags, venueName, address, local start/end or today's hours, price, rating and ratingCount, description (≤1,500 chars), url.
2. **Decide whether to research.** If the description already has ≥300 characters of real content, skip to step 4 (ungrounded and cheaper).
3. **Research call:** Gemini with the Google Search grounding tool, plus the URL-context tool pointed at `url` when there is one (**VERIFY** tool names in the current SDK). Prompt: "Find what this is and what someone attending can expect. Return short factual bullet points only." Save the grounding URIs as `blurb.sources`.
4. **Write call** (plain text, no tools). Prompt rules:
   - 50–90 words, second person, present tense.
   - Say what it is, what you'd do there, and one practical detail (typical stay, price, when to arrive, what to bring) only if it's in the facts.
   - Use only facts from the packet or the research notes. If unsure, leave it out.
   - Don't restate the date or time; the UI already shows them.
   - No hype words ("amazing", "must-see", "unforgettable", "vibrant", "hidden gem").
5. **Check in code** (no LLM): word count is 50–90; no banned words; every number in the blurb (prices, times, years, counts) appears somewhere in the facts packet or research notes. On failure, regenerate once and pass the failure reason back; if it fails again, fall back to `summary`.
6. **Save** the `blurb` subdocument (§4.1) with `inputHash = sha1(facts packet)` and `grounded` set to whether step 3 ran.

**Budget:** grounded calls have their own, smaller free allowance (**VERIFY** in AI Studio), so they go through the quota counter as `gemini_grounded`. Match concurrency to the key's rate limit and back off on 429s. At ~10 requests/minute, 300 blurbs at up to 2 calls each takes about an hour, so start the batch early Saturday evening.

**Stretch ideas**

- **"Why you'll like it" line** (planner side): one sentence per stop, generated at plan time from the user profile plus the blurb. It's personal, so don't store it on the activity.
- **Audio preview** (a fit for MLH's "Best Use of ElevenLabs" prize): read the itinerary's blurbs aloud as a 60-second walkthrough.
- **Live demo moment:** open an event with no description and let judges watch the agent research it and write the blurb.

### 6.7 Series detection

Ticketmaster, RA and SerpApi list each date of a recurring event as a separate event with nothing linking them. A weekly event shows up only once in a 7-day horizon, so Ticketmaster and RA (both cheap to query) fetch `series_detection_days` (28) ahead; the extra dates are used for detection only and aren't inserted. SerpApi's budget is too tight for that, so its events are only matched against what's already in the DB. After dedupe, group events that don't have a `recurrence` yet:

- **Same series when:** normalized titles have `token_set_ratio` ≥90, the venue matches (≤150 m, or venue name `token_set_ratio` ≥85), local start times are within 15 min, and there are ≥2 different local dates. Strip dates and edition markers ("Sept 30", "#12", "Vol. 3") from titles before comparing.
- **Frequency** from the median gap between dates: 1 day → `daily`, 7 → `weekly`, 14 → `biweekly`, 28–31 → `monthly`, anything else → `irregular`.
- Fill `daysOfWeek` and `localStartTime` from the occurrences; set `rule: null`, `until: null`, `origin: "detected"`. The venue key in `seriesKey` is `googlePlaceId` if present, otherwise the venue's H3 res-9 cell.
- Lean toward precision: a false positive shares ratings between unrelated events. Log detected series to `runs` and spot-check a few.

---

## 7. Quota counter

Before every limited call, run `quota.reserve(api, n)`: an atomic `findOneAndUpdate` with filter `used + n <= cap` and `$inc used`. It raises `QuotaExceeded` when the cap would be exceeded. `--dry-run` estimates usage without reserving anything.

| API | Free allowance | Cap | Period |
|---|---|---|---|
| Google Nearby Search Enterprise | 1,000 | 900 | month |
| Google Text Search Enterprise | 1,000 | 900 | month |
| Google Geocoding | 10,000 | 9,000 | month |
| Google Route Matrix (elements) | 10,000 | 9,000 | month |
| SerpApi | 250 | 200 | month |
| Gemini grounded calls | per key (**VERIFY**) | config | day |

Ticketmaster (5,000/day, 2 req/s), OpenTopoData, Nominatim and the scraped sites only need rate limiting in the shared HTTP client, not a counter.

---

## 8. Code layout (Python 3.11+)

```
ingest/
  __main__.py              # CLI (typer)
  config.py                # config.yaml + cities/<slug>.yaml + env
  models.py                # pydantic v2: Activity, Address, Recurrence, Blurb, Run, EnrichmentOut
  db.py                    # client, ensure_indexes(), upsert_activity()
  quota.py
  http.py                  # shared httpx client: retries (tenacity), per-domain rate limit, disk cache
  adapters/
    base.py                # Adapter: fetch() -> Iterable[raw]; normalize(raw) -> Iterable[ActivityDraft]
    ticketmaster.py  google_places.py  osm_places.py  serpapi_events.py  web_calendar.py
    resident_advisor.py  eventbrite.py
    osm_trails.py  google_typical.py  predicthq.py  nps.py  fsq_os.py      # P2
  enrich/
    geocode.py  classify.py  llm.py  duration.py  trails.py  embed.py
    recurrence.py          # expand rules (§4.6), detect series (§6.7)
  agent/
    blurb.py               # §6.6
    discover.py            # P2, §5.8(e)
  dedupe.py
  pipeline.py
  travel.py                # route matrix + cache + fallback
  api.py                   # planner-facing queries (§9)
  scheduler.py             # stretch: loop run-all on config.refresh cadences
config.yaml
cities/<slug>.yaml
seeds/<city>/sources.yaml
seeds/ra_event_listings.graphql
seeds/category_maps.yaml
fixtures/<adapter>/*.json
cache/                     # gitignored
tests/
.env.example
```

**Libraries:** `httpx, tenacity, pydantic, pymongo, typer, rapidfuzz, pycountry, extruct, selectolax, trafilatura, icalendar, recurring-ical-events, dateparser, shapely, h3, google-genai, python-dotenv, timezonefinder, pytest`. Add `playwright` only if a source needs rendering (§5.4 step 6, §5.9), and `duckdb` only for FSQ.

**Env (`.env.example`):** `MONGODB_URI, GOOGLE_MAPS_API_KEY, GEMINI_API_KEY, GEMINI_MODEL, EMBEDDING_MODEL, EMBEDDING_DIM, TICKETMASTER_API_KEY, SERPAPI_API_KEY, PREDICTHQ_TOKEN, NPS_API_KEY, HF_TOKEN, CONTACT_EMAIL`

**CLI:**

```
python -m ingest init-db
python -m ingest run <adapter|source_id> [--city <slug>] [--dry-run] [--limit N]
python -m ingest run-all [--city <slug>] [--dry-run]
python -m ingest enrich [--only-missing]
python -m ingest dedupe
python -m ingest blurb [--top N] [--city <slug>]
python -m ingest stats [--city <slug>]      # counts by kind/category/source; % with hours, duration, price, embedding, blurb
python -m ingest quota
python -m ingest snapshot export|import <path>
python -m ingest scheduler                  # stretch
python -m ingest discover [--city <slug>]   # stretch
```

---

## 9. Interface for the planner (`ingest/api.py`)

- **`candidates(lat, lng, window_start, window_end, radius_m=5000, max_price_tier=None, categories=None, limit=200)`**
  - Uses `$geoNear` plus feasibility filters:
    - `fixed_start` events: `start ∈ [window_start, window_end − p75]`
    - `drop_in` events: overlap of `[start, end]` and the window ≥ `p75`
    - places: some `weeklyHours` interval overlaps the window by ≥ `p75`
    - places with unknown hours: allowed only 10:00–18:00 local, returned with `hoursUnknown: true`
- **`similar(activity_id, k=5, **same filters)`**: Atlas Vector Search, same `kind`, feasibility-filtered, and collapsed by `recurrence.seriesKey` so one weekly event can't fill every slot. Powers "quick replace".
- **`series(series_key)`**: upcoming occurrences of a recurring event, for an "also every Tuesday" line in the UI.
- **`travel_matrix(points, mode, depart_at)`**: §5.7 with cache and fallback.
- **`blurb(activity_id)`**: returns the cached blurb, or runs the §6.6 agent, saves and returns it.

---

## 10. Hackathon timeline and acceptance criteria

| When | Scope | Done when |
|---|---|---|
| Fri night | **M0:** skeleton (config, models, db + indexes, http client, quota, CLI stubs, `.env.example`) + Ticketmaster | `init-db` is idempotent; ≥150 Ticketmaster events in the next 7 days; a rerun inserts 0 docs; ~50-doc JSON sample shared with the team |
| Sat morning | **M1:** Google Places (or OSM places if no billing), Nominatim geocoding, SerpApi | ≥1,000 operational places with `weeklyHours` in the bbox (Google), or ≥500 from OSM; SerpApi events geocoded |
| Sat midday | **M2:** web calendars (≥5 seeded), Resident Advisor, Eventbrite | ≥3 web sources extract via steps 1–4; RA and Eventbrite events in the DB; overlapping Ticketmaster/Eventbrite/SerpApi events merge |
| Sat afternoon | **M3:** enrich, duration, dedupe, embeddings, `candidates()` + `similar()` | 100% of docs have `category`, `duration.p75Min` and `embedding`; ≥90% have `price.tier` or `isFree`; `candidates()` returns ≥20 feasible results near Klaus for Sunday 1–5pm; ≥95% of docs have `address.region`; recurring events expand to one doc per date and a rerun adds none; 5 detected series spot-checked |
| Sat evening | **M4:** blurb agent + Routes travel matrix | the top 300 have blurbs; 10 spot-checked with no invented facts; `travel_matrix()` works with cache and fallback |
| Sat night (if ahead) | **M5 stretch:** Google typical time, OSM trails, PredictHQ, scheduler, discovery agent, ElevenLabs audio | each passes its fixture test and stays under its cap |
| Sun early morning | **Freeze** | fresh `run-all` → `enrich` → `blurb --top 300` → `snapshot export`; the app demos from the snapshot |

**Google billing note:** if billing still isn't on by Saturday morning, switch `osm_places` on, keep Nominatim for geocoding and the haversine fallback for travel, and carry on. Turn Google on whenever it's ready; dedupe merges it into the OSM places.

Check live.hexlabs.org for the exact Devpost submission time and work back from it.

---

## 11. Decisions

**Decided**

- **Language:** Python 3.11+ for ingestion. The API server may be Go (the brainstorm references the Go Mongo driver); the two share only the Mongo schema.
- **Cities:** Atlanta for the demo; others via `cities/<slug>.yaml`.
- **Database:** MongoDB Atlas (needed for Vector Search).
- **Runtime:** one laptop runs ingestion; the app demos from a snapshot.
- **Events horizon:** 7 days.

**Pending**

- **Google Cloud billing:** the team will enable it, but it isn't on yet (§0, §5.2, §10).
- **Embedding model and dimension:** confirm with the ML owner (§0).
