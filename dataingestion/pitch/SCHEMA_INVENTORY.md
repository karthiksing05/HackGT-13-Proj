# `pitch_activities` schema inventory

The canonical schema is `freetime.demo_activities` (100 documents, inspected on 2026-09-27), read
together with the ingestion model (`dataingestion/ingest/models.py`, `Activity`), the Go model
(`Backend/pkg/models/activity.go`), the pipeline's writers (`ingest/pipeline.py`,
`ingest/agent/embed_text.py`, `ml/tools/embed_missing.py`) and `docs/DATA.md`. Neither catalog
collection has a MongoDB validator, so these three sources are the schema. Field names, nesting and
BSON types below match `demo_activities`, and `pitch.validate` checks them against a type inventory
of it (`pitch/demo_schema.json`).

Legend for *When*: **all** means every document; **event** and **place** mean that kind; **null otherwise**
means the field is present with a null value when it does not apply (the demo convention).

| Field path | BSON type | Meaning | When | Generation rule |
|---|---|---|---|---|
| `_id` | objectId | document id | all | `sha1(sourceKeys[0])[:24]`, the demo generator's rule; deterministic, so reruns cannot duplicate |
| `kind` | string | `event` or `place` | all | events for dated occurrences; places for flexible visits inside opening hours |
| `city` | string | catalog city slug | all | `atlanta` |
| `name` | string | title on the stop card | all | places: the real venue's name (two trails named with their park for context: White Trail and Cherokee Trail); events: invented, generic, no real performer named |
| `summary` | string | detail-screen line, at most 25 words | all | the description's first sentence (demo rule) |
| `description` | string | 1–3 factual sentences, at most 2000 characters | all | written for the pitch; says what the price covers |
| `category` | string | the fixed vocabulary of docs/DATA.md | all | vocabulary only; places only in categories the planner schedules |
| `sourceCategory` | string | the source's own classification | all | places: the real record's Google type or OSM tag; events: the category (as demo) |
| `tags` | array of string | the fixed tag vocabulary | all | hand-written per activity; `free`, `cheap`, `splurge` derived from the price by one rule |
| `location` | object | GeoJSON point | all | copied verbatim from the real venue record |
| `location.type` | string | `Point` | all | |
| `location.coordinates` | array of 2 doubles | `[lng, lat]`, longitude first | all | copied verbatim |
| `address` | object or null | postal address | all; null for OpenStreetMap trails | copied from the real record; `formatted` composed from its own parts where a listing left it empty |
| `address.formatted` | string | one-line address | when the address exists | real, or composed from the real parts |
| `address.street`, `.locality`, `.region`, `.postalCode`, `.countryCode` | string or null | parts | when the address exists | copied verbatim (Google leaves `street` null for some parks) |
| `venueName` | string or null | where it happens | all; null for the 6 trails without a known park | places: the real name; events: the real venue's name (3 listing names tidied: Tabernacle, The Eastern, The Masquerade - Altar); trails: the record's park |
| `start`, `end` | date or null | UTC instants | event; null otherwise | local America/New_York time converted to UTC; `end > start`; overnight events end on the next local date |
| `attendance` | string | `fixed_start` or `drop_in` | all | events: fixed for shows, classes, tours, meals; drop-in for markets, festivals, parties, open studios. Places: `drop_in` (demo convention) |
| `timezone` | string | IANA zone | all | `America/New_York` |
| `weeklyHours` | array or null | open/close minutes since Sunday 00:00 local | place; null otherwise | the real record's Google hours, verbatim (open all week is `{0, 10080}`); for the 4 venues whose record has none, the planner's category defaults written out with `hoursSource: default` |
| `weeklyHours[].open`, `.close` | int | minutes 0–10080 | place | a close below the open wraps past Saturday |
| `hoursSource` | string or null | `google`, `osm`, `nps`, `default` | place; null otherwise | as recorded, or `default` |
| `recurrence` | object or null | weekly series metadata | series occurrences; null otherwise | 11 two-week series; every occurrence is its own document (the planner never expands rules) |
| `recurrence.seriesKey` | string | shared by occurrences | series | `pitch:series:<slug>` |
| `recurrence.frequency` | string | | series | `weekly` |
| `recurrence.rule` | string | RFC 5545 RRULE | series | `FREQ=WEEKLY;BYDAY=..` |
| `recurrence.daysOfWeek` | array of int | 0 = Sunday | series | the occurrences' local weekday |
| `recurrence.localStartTime` | string | `HH:MM` local | series | |
| `recurrence.until` | date | the last occurrence | series | the second occurrence's start |
| `recurrence.text` | string | human wording | series | e.g. "Sundays at 11:30 AM, Sep 27 – Oct 4, 2026" |
| `recurrence.origin` | string | where the rule came from | series | `source_rule` (the synthetic source states it) |
| `duration` | object | lognormal visit length | all | |
| `duration.medianMin`, `.sigma`, `.p75Min` | double | minutes; p75 = median·e^(0.674σ) | all | fixed-start events: the span with σ 0.1 (`event_times`); drop-ins and places: the category prior; trails: the real trail model |
| `duration.source` | string | provenance of the estimate | all | `event_times`, `category_prior`, `trail_model` |
| `price` | object | minimum expected participation cost, per person | all | |
| `price.min`, `price.max` | double | whole US dollars | all | 0/0 when free; max ≥ min |
| `price.currency` | string | | all | `USD` |
| `price.tier` | int | 0–4 on Atlanta's bounds 0/15/40/80, from `min` | all | `price_from_range` |
| `price.isFree` | bool | | all | true exactly when min and max are 0 |
| `rating`, `ratingCount` | double / int or null | the Google rating | real Google places; null otherwise | copied verbatim; never invented |
| `popularity` | double | 0..1 prior | all | the demo generator's formula, from the real review count where there is one |
| `trail` | object or null | trail geometry and stats | trails; null otherwise | copied verbatim from the OpenStreetMap record |
| `trail.lengthKm`, `.ascentM`, `.descentM` | double | | trails | |
| `trail.loop` | bool | | trails | |
| `trail.geometry` | object | GeoJSON LineString | trails | |
| `url` | string or null | the "Website" link | places: the real record's link; null for events | events are synthetic and have no page, so no URL is claimed |
| `ticketUrl` | null | where tickets are sold | all | null: there is no real ticket page, and the sandbox merchant only sells Saltlight's events |
| `imageUrl` | null | photo | all | null, as in demo_activities; no approved images exist |
| `sourceKeys` | array of string | unique upsert key | all | `pitch:<slug>` for places, `pitch:<slug>@<local date>` for events (DATA_COLLECTION_SPEC §4.6) |
| `sources` | array of object | provenance | all | one entry naming the synthetic `pitch` source; real records are listed in the manifest instead |
| `sources[].name`, `.id`, `.url`, `.fetchedAt` | string / string / string / date | | all | `pitch`, the key, `https://pitch.sidequestz.example/...` (reserved domain, like demo's `saltlight.example`), the generation time |
| `googlePlaceId` | string or null | Google place id | real Google places; null otherwise | copied verbatim; identifies the venue the hours and rating come from |
| `expiresAt` | date or null | the pipeline's expiry | event; null otherwise | `(end or start + p75) + 6 h` (`ingest.pipeline.finalize`). The collection has no TTL index, so nothing expires |
| `createdAt`, `updatedAt` | date | | all | the generation time |
| `embeddingText` | string | eight-section text for the embedding model | all | the pipeline's prompt `event_embedding_text.md`, written by Claude over `event_data(doc)` with no web research |
| `embeddingTextHash` | string | sha1 of the text | all | |
| `embeddingTextMeta` | object | how the text was made | all | |
| `embeddingTextMeta.model` | string | writer model | all | the Claude model that wrote it |
| `embeddingTextMeta.prompt` | string | `event_embedding_text.md@c8e82932f5a1` | all | the pipeline's template hash |
| `embeddingTextMeta.grounded`, `.sources` | bool, array | web research | all | false and empty: nothing to ground for synthetic listings |
| `embeddingTextMeta.inputHash` | string | sha1(template hash + input) | all | the embed-text agent's rule; a changed document invalidates its text |
| `embeddingTextMeta.generatedAt` | date | | all | when the text was written |
| `embedding`, `embeddingModel`, `embeddingMeta` | (absent) | the 1024-d vector | not yet | deliberately absent, not null: `embed_missing` only selects documents whose `embedding` is missing |

## Where the sources disagree, and what the application supports

| Topic | demo_activities | activities / pipeline | Application | Pitch uses |
|---|---|---|---|---|
| `attendance` on places | `drop_in` | null | never read for places | `drop_in` (demo) |
| `expiresAt` for events | `end` | `(end or start + p75) + 6 h` | only a TTL index would read it; the demo collection has none | the pipeline rule, and no TTL index |
| `summary` | first sentence | null (not written) | shown on the detail screen | first sentence (demo) |
| `price` | always set | often null (unknown) | null means unknown, never free | always set, with `isFree` a boolean |
| `price.cents` | absent | absent | the Go model's legacy field, read only when `min` is missing | absent |
| `tags` on trails | vocabulary | adapter tags (`outdoors`, `hiking`, `loop`) | quick picks match vocabulary tags | vocabulary |
| `hoursSource` of invented hours | `google` on invented places | `default` for assumed hours | informational | real Google hours as `google`; assumed hours as `default` |
| Places open all week | `00:00–23:59` per day | `{open: 0, close: 10080}` | a close below the open wraps | as recorded, `{0, 10080}` (DATA_COLLECTION_SPEC §4.1) |
| `recurrence` | always null | always null | projected out; the planner groups repeats by venue + name | set on the 22 series occurrences, following the model and the spec |
| `blurb` | absent | 3 documents | optional | absent |
| Vectors | present | present | required for classifier ranking; without them the planner ranks by its prior | absent until vectors are computed later |
