# Embeddings

One embedding model, one text format, several ways to run it. This page says what gets embedded, when,
by which provider, and where the vectors live. Code: `ml/embedding` (providers), `ml/profiles` (text
templates), `ml/api` (routes), `ml/tools` (probes and jobs), and on the Go side `Backend/pkg/ml` (the
client) and `Backend/pkg/profiles` (when a user's vectors are rebuilt). Design:
[design/embeddings.md](design/embeddings.md). Model card: [ml/models.md](../ml/models.md). The ML
service's endpoints are documented in [ml/README.md](../ml/README.md).

## The one rule

Every vector in the system comes from `Qwen/Qwen3-Embedding-0.6B`: 1024 dimensions, L2-normalized,
`max_seq_length` 512, **no instruction prefix**, over the eight-section text format (Interests,
Activities, Social, Environment, Pace, Cost, Timing, Experience) described in
[ml/description_generation.md](../ml/description_generation.md). The compatibility classifier
(`ml/checkpoints/compatibility_classifier.pt`, first layer 7×1024 → 256) was trained on exactly that
space and the catalog vectors were backfilled the same way, so:

- no `Instruct: … Query:` prefixes on user or search texts, whatever a remote provider's model card
  suggests (the first draft of `Backend/EMBEDDINGS.md` said otherwise; that advice is withdrawn);
- no Matryoshka truncation to fewer dimensions;
- no other encoder or text format without retraining ([ml/training.md](../ml/training.md)); the Go side
  refuses an on-demand vector from another model, and `embed_missing` checks the model on `/healthz`
  before it writes anything;
- the text is built by Python (`ml/profiles`), never assembled in Go, so the template, its validator
  and the encoder settings live in one place and carry one version (`profile-v1`);
- blank text → zero vector. A user without dislikes has a zero negative vector, which the classifier
  was trained with; the `/v1/events/rank` endpoint rejects an all-zero *positive* vector.

Negative signals are written as **topics, not negations** ("Environment: loud nightclub setting", not
"I don't like clubs"): embeddings place a negated sentence next to the thing itself, so the negative
vector is only ever used as a penalty, never as a match.

## Providers and the parity gate

| Provider | How it runs | Needs | Status on 2026-09-26 |
|---|---|---|---|
| `local` | sentence-transformers on the VPS CPU, fp32: loaded lazily behind a lock, `EMBED_LOCAL_THREADS` torch threads; a warmup thread embeds one canary at startup (20–60 s on the VPS), and startup never waits for it | nothing; weights cached under `HF_HOME=/opt/ml/.cache` | **serves all traffic** |
| `hf` | the Hugging Face router: routes `deepinfra` (OpenAI-style `/v1/openai/embeddings`) then `hf-inference` (`feature-extraction`), the routes the Hub reports live first; batches of 32, texts cut at 2000 characters; 429 and 5xx retry, a 401 or 403 skips the route for 600 s | a fine-grained `HF_TOKEN` with "Make calls to Inference Providers" | off: the token in use is rejected as invalid (401); needs a new fine-grained token with that permission, then parity |
| `vertex` | REST `POST https://{VERTEX_HOST}/v1/projects/{p}/locations/{l}/endpoints/{e}:predict` with google-auth, batches of 16, the instance key the probe found (`VERTEX_INPUT_KEY`: `inputs`, `prompt` or `text`); unwraps `[floats]`, `[[floats]]` or `{"embedding": …}` | `GOOGLE_APPLICATION_CREDENTIALS` naming a service-account key with Vertex AI User (`roles/aiplatform.user`) on the project | off: the key's service account is denied `aiplatform.endpoints.get` and `…predict` (403); needs the role, a new probe for the dedicated DNS and input key, then parity |

**How `EMBED_PROVIDER=auto` (the VPS setting) builds its chain:** Vertex joins when
`GOOGLE_APPLICATION_CREDENTIALS` names an existing key file, but serves only after its startup probe
returned a 1024-d vector; HF joins whenever `HF_TOKEN` is set; the local model is always last. A
provider that fails three times in a row is skipped for 60 s (600 s after an auth error), each fallback
is logged, and when all fail the endpoints answer 503 "Embedding provider unavailable.". `vertex`, `hf`
and `local` pin one provider (with the local model behind a remote one unless `EMBED_LOCAL_FALLBACK=0`);
a pinned provider that is not configured fails at startup. In front of the chain, a SQLite cache keyed by
`sha1(model + text)` serves both uvicorn workers (blanks are never cached).

`GET /healthz` reports `status` (`degraded` when no provider can serve; ranking still works),
`embedding.mode` (the `EMBED_PROVIDER` setting), `embedding.provider` (the last one that served, or
`none`), each provider's status and latency, the cache and the counters, plus
`profile_template_version` and `jev`. `GET /healthz?probe=1` embeds a canary past the cache and answers
503 when nothing works.

**Parity gate.** In `auto` a remote provider starts serving as soon as its credentials work, so check it
before the credentials go into `/opt/ml/.env`. On the VPS, with the new token or key loaded in that
shell only:

```sh
cd /opt/ml && .venv/bin/python -m tools.hf_probe            # token, provider mapping, one embed per route
.venv/bin/python -m tools.vertex_probe                      # credentials, endpoint, DNS, input key, dim, latency
.venv/bin/python -m tools.parity_check --provider hf        # or vertex: every cosine >= 0.995, else exit 1
```

`parity_check --provider` embeds five texts with that provider alone (no cache, no local fallback) and
compares them with `ml/tests/fixtures/qwen3_golden.json`, produced by the local model with the training
settings (the local fp32 vectors match the stored bf16 backfill at cosine 0.9997). A remote server that
applies a prompt, different pooling or truncation would pass a shape check and still move every vector.
`--ml-url http://127.0.0.1:8000` runs the same comparison through the running service.

**Vertex costs money while idle.** A Model Garden endpoint bills per node-hour while a model is
deployed. Undeploy it when it is not in use:
`gcloud ai endpoints undeploy-model <endpoint id> --region=<region> --deployed-model-id=<id>`.

## When we embed

| Trigger | What happens | What is stored |
|---|---|---|
| Sign-up | nothing: an account without preferences would embed to a zero vector | — |
| `PUT /me/preferences` | save, then rebuild the profile synchronously (15 s): `POST /v1/user-profile` with the preferences, answers, Facebook interests and the 20 newest rated stops. A failure never fails the save: the old vectors stay and the hashes are cleared, so the next refresh retries | `positiveText`, `negativeText`, `positiveEmbedding`, `negativeEmbedding`, `embeddingModel`, `profileTextHash`, `profileInputHash`, `profileUpdatedAt` |
| Facebook import | store `facebookInterests`, then rebuild the profile synchronously (15 s) | same |
| Facebook disconnect, deauthorize or data deletion | drop the interests, rebuild the profile in the background (20 s) | same |
| Every rebuild | skipped when `profileInputHash` (a SHA-1 of the request above, prefixed `in1:`) is unchanged and the stored model and template version match what `/healthz` reports | — |
| `POST /plans/generate` | `POST /v1/search-profile` for that request (5 s; skipped when mood text and quick picks are both empty) | nothing on the user; the query vector goes into `plan_pools` |
| `PUT /ratings/{itemId}` | in the background (10 s): 4–5 stars fold the rated activity's vector into the positive embedding, 1–2 into the negative one, 3 changes nothing (`POST /v1/compatibility/user-embedding/update`, `new = normalize(0.8·old + 0.2·activity)`). An activity with a text but no vector is embedded first (`POST /v1/embed`, kind `activity`) and stored | the user vector; the activity vector (guarded on `embeddingTextHash`, `embeddingMeta.source: go_on_demand`) |
| Activities without vectors | `tools/embed_missing.py` on a systemd timer, every 15 minutes | activity vectors on both catalogs |
| `sidequestz-admin seed-demo` | rebuilds Sandy Byte's profile through the same code (20 s); reports "not refreshed" (and still succeeds) when the ML service is down | Sandy's profile fields |

At most eight background profile jobs run at once; beyond that a job is skipped with a warning, and
the next rebuild catches up. `POST /v1/embed` takes `{texts: […], kind: user|search|activity}` (up to 64
texts of up to 8000 characters); the kind is for logs and metrics only and never changes the text.

## Text formats

All three texts share the format: sections in the fixed order, each `Name:` followed by `- bullet`
lines, a blank line between sections, empty sections omitted, bullets lowercase, at most 8 words,
deduplicated, no trailing newline, no placeholders, URLs or ids. `ml/profiles/validate.py` applies the
same checks as the data pipeline's `activity_text.check`.

**User profile** (`POST /v1/user-profile` → `positive_text`, `negative_text`, both vectors,
`profile_text_hash = "profile-v1:" + sha1(positive + "\n---\n" + negative)`). Built from the app's
`Preferences` plus what the server knows:

| Input | Effect |
|---|---|
| ratings 1–5 for `outdoors, food, museums, live_music, nightlife, sports, shopping, big_crowds, early_mornings, long_walks` | 4 or 5 → likes (two bullets per section for a 5, one for a 4); 1 or 2 → dislikes (two for a 1, one for a 2, from a mirrored lexicon); 3 → nothing |
| `company`, `pace` | Social and Pace bullets ("small-group setting", "moderate pace") |
| `spend`, `prefer_free`, `flexibility` | Cost bullets; `stick_to_budget` adds "expensive tickets" to the dislikes |
| `answers.perfect_afternoon`, `answers.plan_around` / `answers.never_do` | free text split into short phrases, routed to a section by keywords, positive / negative |
| Facebook `interests` | positive Interests |
| the 20 most recent rated stops | category phrases positive (4–5 stars) or negative (1–2), plus tag rules ("great people" → Social, "too crowded" → "large crowds", "too pricey" → "expensive tickets") |

Caps per section: Interests 10, Activities 8, Social 4, Environment 4, Pace 3, Cost 3, Timing 3,
Experience 3. Sandy Byte's seeded preferences (outdoors and long walks 5; live music, food and early
mornings 4; museums and sports 3; shopping and nightlife 2; big crowds 1; small group, balanced, under
$15, prefers free, plus her three answers) render to these texts, byte for byte the
`ml/tests/fixtures/profile_seed_user.json` fixture the tests check:

<table>
<tr><th>Positive</th><th>Negative</th></tr>
<tr><td>

```text
Interests:
- outdoor recreation
- local food
- live music
- live music somewhere small while the sun goes

Activities:
- park visit
- guided hike
- long walks
- food tasting
- live music performance

Social:
- small-group setting
- close friends
- whoever's free to wander

Environment:
- outdoor setting
- long walk along the water
- snack from the market

Pace:
- extended walking
- early start
- moderate pace

Cost:
- free admission
- under $15 admission

Timing:
- early morning
- sunrise swims
- saturday market
```

</td><td valign="top">

```text
Interests:
- nightlife
- shopping

Activities:
- market browsing

Social:
- large crowds
- huge crowds

Environment:
- crowded venue
- loud nightclub setting
- packed clubs

Timing:
- late night
- only gets going after midnight
```

</td></tr>
</table>

**Search text** (`POST /v1/search-profile` → `search_text`, `search_embedding`; empty inputs give an
empty text and no vector). Quick picks map to Interests, Activities, Environment or Pace bullets
(unknown picks become an Interests bullet), `who` to Social, `pace` to Pace, `budget` to Cost, the window
to Timing ("friday afternoon", "weekday afternoon", "multi-hour outing"), then the mood phrases.

**Activity text** (`activities.embeddingText`). Written once per activity by the data pipeline's
`embed-text` step from the listing plus web research (prompt:
`dataingestion/ingest/agent/prompts/event_embedding_text.md`), or by the Raven backfill
(`ml/datagen/mongo_backfill.py`) for listings that had none. `embeddingTextHash` is the SHA-1 of that
text; a vector is valid only while `embeddingMeta.textHash` equals it.

## Where the vectors are used

| Place | Formula |
|---|---|
| Planner shortlist (`Backend/pkg/planner/vector.go`) | `q = normalize((1 − 0.6)·positive + 0.6·search)`; `s = dot(q, e) − 0.5·max(0, dot(negative, e)) + 0.05 per covered facet` |
| `POST /v1/events/rank` | hard filters, then the classifier over `(positive', negative, event)` with `positive' = normalize(0.4·positive + 0.6·search)` (`SEARCH_WEIGHT`), an optional Jev rerank of the top k (15 s at most, then the model order) |
| Alternatives | `0.7·cosine(pool query vector, e) + 0.3·cached ML score` |
| Rating feedback | `normalize(0.8·old + 0.2·activity)` (`USER_EMBEDDING_ALPHA`); an empty vector takes the activity's direction |

Scores from the classifier approximate an LLM judge's 0–3 rating ÷ 3; compare them within one user's
candidates, not across users ([ml/models.md](../ml/models.md)). The planner treats 0.5 as the bar a stop
has to clear ([PLANNER.md](PLANNER.md)).

## Storage

| Collection | Fields | Notes |
|---|---|---|
| `users` | `positiveEmbedding`, `negativeEmbedding` (1024 floats), `positiveText`, `negativeText`, `embeddingModel`, `profileTextHash`, `profileInputHash`, `profileUpdatedAt`, `facebookInterests` | never on the wire (`json:"-"`); `GET /me` carries no vector |
| `activities`, `demo_activities` | `embedding` (float64, unit norm), `embeddingModel`, `embeddingMeta {model, dimension, normalized, prompt: null, maxSeqLength, generatedAt, textHash, source, provider, backfill}`, `embeddingText`, `embeddingTextHash`, `embeddingTextMeta` | `source` is `mongo_backfill`, `embed_missing` or `go_on_demand`; the planner loads vectors only in phase B |
| `plan_pools` | `queryVector`, `negVector`, `hasNeg`, `scores {activityId: {ml, jev}}` | 6 h TTL; used by `/more`, `/route` and `/alternatives` |

## Environment

ML service (`/opt/ml/.env` and `ml.service`):

| Variable | Default | Purpose |
|---|---|---|
| `EMBED_PROVIDER` | `auto` | `auto`, `vertex`, `hf` or `local`; reported as `embedding.mode` |
| `EMBED_MODEL` / `EMBED_DIM` | `Qwen/Qwen3-Embedding-0.6B` / `1024` | must equal the catalog's `embeddingModel` |
| `HF_TOKEN` | – | read access to private `hf://` checkpoints, and the HF provider when it may call Inference Providers |
| `HF_EMBED_ROUTES` / `HF_ROUTER_BASE` | `deepinfra,hf-inference` / `https://router.huggingface.co` | |
| `HF_EMBED_TIMEOUT` / `HF_EMBED_MAX_BATCH` / `HF_EMBED_MAX_RETRIES` / `HF_AUTH_BACKOFF` | `20` / `32` / `3` / `600` | seconds, texts, retries, seconds |
| `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT`, `VERTEX_LOCATION`, `VERTEX_ENDPOINT_ID`, `VERTEX_HOST`, `VERTEX_INPUT_KEY`, `VERTEX_TIMEOUT`, `VERTEX_MAX_BATCH` | key path unset; the endpoint defaults point at the team's Model Garden deployment; `20` s, `16` | the Vertex provider |
| `EMBED_LOCAL_FALLBACK` / `EMBED_LOCAL_THREADS` / `EMBED_LOCAL_DEVICE` | `1` / `8` / `cpu` | |
| `EMBED_WARMUP` | `1` | background canary at startup |
| `EMBED_CACHE_PATH` / `EMBED_CACHE_MAX_ENTRIES` | `.cache/embeddings.sqlite` / `100000` | an empty path disables the cache |
| `EMBED_MAX_CHARS` | `2000` | client-side truncation for remote providers |
| `HF_HOME` | `/opt/ml/.cache` in `ml.service` | the unit runs with `ProtectHome=true` |
| `RANKING_MODEL` / `RANKING_CHECKPOINT` / `RANKING_DEVICE` / `RANKING_MODEL_VERSION` | `classifier` / bundled / `cpu` / derived | the compatibility model ([ml/README.md](../ml/README.md)) |
| `SEARCH_WEIGHT` / `USER_EMBEDDING_ALPHA` | `0.6` / `0.8` | |
| `TYPESAFE_API_KEY` | – | enables Jev; unset means model order only |
| `RERANK_TOP_K` / `RERANK_TIMEOUT_SECONDS` | `20` (12 in `ml.service`) / `15` | how many events Jev scores, and how long it may take before the model order is returned |
| `LOG_LEVEL` | `INFO` | |

Go API (`/opt/backend/.env`): `ML_SERVICE_URL` (`http://127.0.0.1:8000`), `ML_RERANK` (`true`; the kill
switch is `false`), `ML_RERANK_TOP_K` (12), `ML_RANK_TIMEOUT_MS` (2000), `ML_RANK_RERANK_TIMEOUT_MS`
(20000), `ML_EMBED_TIMEOUT_MS` (15000, also `/v1/user-profile`), `ML_SEARCH_TIMEOUT_MS` (5000); the
planner's own `PLANNER_ML_TIMEOUT_MS` (3000) and `PLANNER_SEARCH_TIMEOUT_MS` (5000) bound its calls. The Go
client caches `/healthz` for 60 s (a failure for 10 s) and asks for a rerank only when it reports
`jev: true`.

## Jobs and checks

```sh
# fill missing or stale activity vectors through the running service (what the timer runs)
cd /opt/ml && .venv/bin/python -m tools.embed_missing --uri 'mongodb://127.0.0.1:27017/?directConnection=true' \
  --db freetime --collections activities demo_activities [--dry-run] [--limit N] [--no-stale]
# rank stored catalog vectors for a fixture profile, to judge the order by eye
.venv/bin/python -m tools.rank_smoke --catalog demo_activities --mood "something outdoorsy"

# the service and its provider
curl -s 127.0.0.1:8000/healthz | jq '{status, jev, ranking, embedding: {model, dim, mode, provider, providers}, profile_template_version}'
curl -s '127.0.0.1:8000/healthz?probe=1' | jq '.embedding.providers'
# a unit vector for a text, a zero vector for a blank: expect "Qwen/Qwen3-Embedding-0.6B 1024 <provider> [1.0, 0.0]"
curl -s 127.0.0.1:8000/v1/embed -H 'content-type: application/json' \
  -d '{"texts":["Interests:\n- live jazz\n\nCost:\n- free admission",""],"kind":"user"}' \
  | python3 -c 'import json,sys,math; r=json.load(sys.stdin); print(r["model"], r["dim"], r["provider"], [round(math.sqrt(sum(x*x for x in v)),6) for v in r["embeddings"]])'
# a user's stored profile (on the VPS; locally prefix with docker exec sq-mongo)
mongosh freetime --eval 'db.users.findOne({email:"demo@sidequestz.tech"},{profileTextHash:1,profileInputHash:1,embeddingModel:1,positiveEmbedding:{$slice:3}})'
# nothing leaks to the app
curl -s https://api.sidequestz.tech/me -H "Authorization: Bearer $T" | grep -c positive_embedding   # expect 0
```

`embed_missing` selects documents with an `embeddingText` and either no vector or one written for an
older text, embeds each distinct text once across both catalogs, writes with guarded updates (a text that
changed meanwhile keeps its state) and exits 1 when the service is unreachable or embeds with another
model. The bulk backfill for many activities at once (texts and vectors on a GPU) is
`ml/datagen/mongo_backfill.py` with `ml/datagen/backfill.sbatch` on Raven, which is how the Atlanta
catalog got its vectors on 2026-09-26. The Go tests against a running service need `ML_LIVE_URL`; the
slow Python tests need `ML_TEST_LOCAL_EMBEDDER=1` (local model) or `ML_TEST_HF=1` (HF parity).

## Retraining

The classifier is bound to this encoder and text format. To retrain, or to swap the encoder, follow
[ml/training.md](../ml/training.md) (`mpcdf.py submit raven ml/compatibility/classifier/train.sbatch`),
publish with `python -m compatibility.classifier.publish`, then re-embed every user and activity with
the new model and set `EMBED_MODEL` accordingly. Real labels for that retraining are on the
[roadmap](ROADMAP.md): what was shown (`plan_runs` keeps every shortlist with its scores), what was
saved (itineraries keep their `runId` and `optionId`) and how it was rated.
