# Handoff: the Atlanta pitch catalog (`freetime.pitch_activities`)

> **Update 2026-09-27, ~03:30 EDT: steps 3–5 are done.**
> - **Texts and vectors:** all 250 documents have embedding texts (the rest were written by Claude) and
>   Qwen3-Embedding-0.6B vectors. The vectors were computed on MPCDF Raven with `ml/datagen/mongo_backfill.py`
>   and `backfill.sbatch`, the same fields `embed_missing` writes.
> - **Tickets:** the 150 paid items carry `ticketUrl`s. The ticket site still needs their listings; see
>   [TICKETS_HANDOFF.md](TICKETS_HANDOFF.md) and `pitch/out/ticket_listings.json` (`python -m pitch.tickets`).
> - **Audit fixes:** 8 descriptions were corrected, and `pitch.load --update-fields` applied them.
> - **Validation:** `pitch.validate --mongo` now also checks stored vectors, and it reports 0 errors.
> - **Still open:** steps 6 and 7 (wiring the app, which a teammate is handling; the planner test). Also add
>   `pitch_activities` to `ml/ml-embed-missing.service`, so changed texts get re-embedded.
>
> **Note:** after `pitch.texts import`, run `pitch.texts pull` (with the tunnel up) before `pitch.validate
> --mongo`. `import` rounds the stored texts' `generatedAt` to whole seconds.

State as of **2026-09-27, about 03:05 EDT**, and how to finish it on any machine. What the catalog is and
how it was built: [REPORT.md](REPORT.md). Every field: [SCHEMA_INVENTORY.md](SCHEMA_INVENTORY.md).
Synthetic demonstration data: real Atlanta venues, invented listings.

## Where things stand

| | State |
|---|---|
| Collection | `freetime.pitch_activities` on the **production** MongoDB, reached through the SSH tunnel on `127.0.0.1:27017`. **250 documents** (99 places, 151 events), verified byte-identical to the validated export |
| Paid / free | **150 / 100**, recounted in MongoDB with the planner's own price rule |
| Indexes | the same 8 as `demo_activities`; `expiresAt_1` is a plain index, **no TTL**; no validator (like the other catalogs) |
| Embedding texts | **100 of 250 documents stored.** 25 more (24 distinct texts) are **written and checked but not uploaded**: `pitch/out/text_batches/written_01.out.json`. **125 documents (120 distinct inputs) are still unwritten.** Work stopped at a Claude session limit |
| Vectors | none, on purpose; they come after the texts (step 5) |
| App | **not wired in.** The catalog allow-lists reject `pitch_activities` (step 6) |
| Code | uncommitted: `dataingestion/pitch/` and `Backend/pkg/planner/mongosource/pitchcatalog/` |

## Quick resume

```sh
# 0. files: unzip the bundle at the repo root (see below), or check out a commit that has them
# 1. environment (once)
cd <repo>/dataingestion && python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt
ssh -N -L 27017:localhost:27017 <user>@<vps>                     # separate terminal; the team's tunnel
# 2. confirm the state (read-only)
.venv/bin/python -m pitch.validate --partial-texts --mongo      # expect "0 errors"
# 3. upload the 25 texts already written
.venv/bin/python -m pitch.texts import && .venv/bin/python -m pitch.generate \
  && .venv/bin/python -m pitch.load --partial-texts --update-texts
# 4. write the rest (step 4 below), then the final upload and check
.venv/bin/python -m pitch.load --update-texts && .venv/bin/python -m pitch.validate --mongo
```

## 1. Get the files onto the other machine

Nothing is committed. Use either of these:

- **The bundle** `dataingestion/out/pitch_handoff.zip` (built from this machine). Unzip it at the repo
  root. It holds the code, these docs and `pitch/out/`: the export, the manifest's inputs, the venue-record
  cache, every text written so far and the batch files. Keep it private: `pitch/out/` carries Google Places
  data that `docs/DATA.md` says is never redistributed.
- **Git**: commit `dataingestion/pitch/` and `Backend/pkg/planner/mongosource/pitchcatalog/`, but never
  `pitch/out/`, which is already gitignored. Then rebuild the local state on the other machine
  (step 3).

## 2. Set up (once per machine)

- Python 3.12, then `cd dataingestion && python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt`.
- The SSH tunnel to the VPS MongoDB on `127.0.0.1:27017` (`ssh -N -L 27017:localhost:27017 <user>@<vps>`).
  Credentials are not in the repo.
- Only for the planner test (step 7): Go 1.26 and a local `mongod`.

Everything runs from `dataingestion/`. Every read of production is read-only. The only writes are
`pitch.load`'s, and it only inserts missing documents or sets embedding-text fields. It never drops,
replaces or deletes anything, and it refuses to touch a stored document that differs from the export.

## 3. Rebuild the local state (only without the bundle)

```sh
.venv/bin/python -m pitch.generate                  # reads the 159 venue records (read-only) into pitch/out/
.venv/bin/python -m pitch.texts pull                # copies the texts already stored in the collection
.venv/bin/python -m pitch.generate                  # attaches them to the export
.venv/bin/python -m pitch.validate --partial-texts --mongo   # expect 0 errors
```

Without the bundle, the 25 written-but-unuploaded texts are lost; they simply get rewritten in step 4.
Don't edit `pitch/specs.py` unless you mean to change the catalog. Any change to a document changes its
text input, so its text becomes stale and must be rewritten, and `pitch.load` refuses to overwrite a
stored document that differs from the export.

## 4. Finish the embedding texts

The texts follow the pipeline's own prompt, `ingest/agent/prompts/event_embedding_text.md`, over each
document's `event_data()` with no web research, since the listings are synthetic and there is nothing to
ground. Claude writes them in place of the pipeline's Gemini call, and `embeddingTextMeta.model` records
the writer (`claude-opus-5-5` so far).

**First import any `*.out.json` already in `pitch/out/text_batches/`** (step 3 of Quick resume), then:

```sh
.venv/bin/python -m pitch.texts pending --batches 4    # writes pending_01.json .. pending_04.json (about 30 inputs each)
```

`pending` rewrites the `pending_*.json` files, so never run it while a `pending_NN.out.json` is still
waiting to be imported. For each `pending_NN.json`, give Claude Code this brief (one agent per file works),
with NN filled in:

> Write "embedding texts" by hand, following `dataingestion/ingest/agent/prompts/event_embedding_text.md`
> exactly: substitute each item's `eventData` for `{{EVENT_DATA}}` and produce only the structured text.
> Do not call any API, touch MongoDB or edit any other file. Input:
> `dataingestion/pitch/out/text_batches/pending_NN.json` (`{"items": [{"inputHash", "sourceKeys", "eventData"}]}`).
> Rules: only the sections Interests, Activities, Social, Environment, Pace, Cost, Timing, Experience, in
> that order, omitting unsupported ones; `Name:` headers; `- ` bullets that are lowercase, at most 8 words,
> not sentences and not repeated anywhere in the text; a blank line between sections. No placeholders,
> URLs, ids, dates, venue names, brands or people. Only state what the eventData supports.
> Conventions used so far: Timing from the "Starts" line as "<weekday> <morning|afternoon|evening>" plus
> "weekend …" or "weekday …" (Friday counts as a weekday), and "late night" when it runs past midnight;
> places get Timing only when the description says so. Cost: Free → `free` and `free admission`; a top
> price of $20 or less → `low cost`; a lowest price under $50 → `moderate cost`; $50 or more →
> `expensive`; extras such as `cover charge`, `drinks extra` or `parking fee` when stated. 21+ items get
> `21 and over` under Social. Write `dataingestion/pitch/out/text_batches/pending_NN.out.json` as
> `{"model": "<your exact model id>", "texts": {"<inputHash>": "<text with \n line breaks>"}}`, then run
> `cd dataingestion && .venv/bin/python -m pitch.texts check pitch/out/text_batches/pending_NN.out.json`
> until it prints `ok`.

Upload after each batch, so progress survives interruptions, and finish with a full check:

```sh
.venv/bin/python -m pitch.texts import && .venv/bin/python -m pitch.generate \
  && .venv/bin/python -m pitch.load --partial-texts --update-texts      # after each batch
.venv/bin/python -m pitch.validate --mongo    # once every document has a text: expect 0 errors, 250 with texts
```

## 5. Vectors (after every document has a text)

On the VPS, through the running ML service, the same tool its timer runs for the other catalogs:

```sh
cd /opt/ml && .venv/bin/python -m tools.embed_missing --uri 'mongodb://127.0.0.1:27017/?directConnection=true' \
  --db freetime --collections pitch_activities [--dry-run]
```

It selects the documents whose `embedding` is missing, which is all of them, because vector fields are
absent rather than null. To keep changed texts embedded automatically, add `pitch_activities` to the
`--collections` in `ml/ml-embed-missing.service`. **Until vectors exist, plans show places only.** Without
a query vector the planner ranks by its prior, and places carry Google ratings while events carry none.
The planner test shows plans mixing events and places once vectors are present.

## 6. Make the app use it (code changes, not made)

1. `Backend/pkg/planner/config.go` `var catalogs` (line ~408): add `"pitch_activities": "atlanta"`.
2. `Backend/pkg/store/catalog.go` `CatalogCollection`: return `pitch_activities` for that name. Today every
   name except `demo_activities` maps to `activities`.
3. `Backend/pkg/store/store.go` `CatalogCollections` (line ~70): add it, so `ensure-indexes` covers it and
   `reset-app-data` never drops it.
4. `Backend/pkg/planner/route.go` (line ~253): add it to the stop-detail fallback list.
5. Point an account at it: `users.catalog = "pitch_activities"`, `users.city = "atlanta"`, with a home base
   near Tech Square. Starts more than 60 km from the city snap back to the home base.
6. Optional: to pin that account to the demo date like Sandy, extend `ClockFor` in
   `Backend/pkg/api/democlock.go`, which today applies only to `demo_activities` accounts. Otherwise the
   account runs in real time, which suits the Sep 27 – Oct 10 range.

## 7. Re-run the planner compatibility test

It runs on a throwaway `mongod`, never production. The repo's own `integration` tests default to
`127.0.0.1:27017`, which is **production** while the tunnel is up, so don't run those with the tunnel
open.

```sh
mkdir -p /tmp/pitch-mongo && mongod --dbpath /tmp/pitch-mongo --port 27019 --bind_ip 127.0.0.1 --fork --logpath /tmp/pitch-mongo.log
cd <repo>/Backend && PITCH_TEST_URI='mongodb://127.0.0.1:27019/?directConnection=true' \
  go test -tags pitchcatalog -count=1 -v ./pkg/planner/mongosource/pitchcatalog/
mongod --dbpath /tmp/pitch-mongo --shutdown
```

It needs Go 1.26 and `dataingestion/pitch/out/pitch_activities.json`, and it refuses any server that has a
`freetime` database. Last run: all 4 tests passed. That covered 250 documents decoded and a 150/100 price
split, 576 queries where the Mongo and Go filters agreed, and 18 end-to-end plan runs in which every option
met the planner's guarantees.

## Troubleshooting

| Symptom | Meaning and fix |
|---|---|
| `pitch.load`: "holds a different version of …" | the export no longer matches the stored document, usually because a spec changed. Nothing was written. Restore the spec, or replace that document deliberately after review |
| `pitch.texts import`: "N problem(s)" | an `*.out.json` fails the checker or no longer matches its input file (e.g. `pending` was rerun before import). Fix it or rewrite it |
| `pitch.validate`: "the embedding text was written for another prompt or input (stale)" | the document changed after its text was written; rerun `pitch.texts pending` and rewrite that text |
| "these venue records are not in freetime.activities" | the tunnel is down, or the real catalog lost a record; `pitch.generate --refresh` re-reads them |
| Plans contain no events | vectors are still missing (step 5) |
