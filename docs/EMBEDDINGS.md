# Embeddings

One embedding model, one text format, several ways to run it. This page says what gets embedded, when,
by which provider, and where the vectors live. Code: `ml/embedding` (providers), `ml/profiles` (text
templates), `ml/api` (routes), `ml/tools` (probes and jobs) and the Go client `Backend/pkg/ml`. Design:
[design/embeddings.md](design/embeddings.md). Model card: [ml/models.md](../ml/models.md).

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
- no other encoder or text format without retraining ([ml/training.md](../ml/training.md));
- the text is built by Python (`ml/profiles`), never assembled in Go, so the template, its validator
  and the encoder settings live in one place and carry one version (`profile-v1`);
- blank text → zero vector. A user without dislikes has a zero negative vector, which the classifier
  was trained with; the `/v1/events/rank` endpoint rejects an all-zero *positive* vector.

Negative signals are written as **topics, not negations** ("Environment: loud nightclub setting", not
"I don't like clubs"): embeddings place a negated sentence next to the thing itself, so the negative
vector is only ever used as a penalty, never as a match.

## Providers and the parity gate

| Provider | How it runs | Needs | Status |
|---|---|---|---|
| `local` | sentence-transformers on the VPS CPU: lazy, lock-guarded load, `torch` threads = `EMBED_LOCAL_THREADS`, a warmup thread embeds one canary at startup. About 2.5 GB per uvicorn worker, first load 20–60 s | nothing; weights cached under `HF_HOME=/opt/ml/.cache` | the safety net; live |
| `hf` | the Hugging Face router, routes `deepinfra` (OpenAI-style `/v1/openai/embeddings`) then `hf-inference` (`feature-extraction`); the provider mapping is looked up once per process; batches of 32, texts cut at 2000 characters; 429 and 5xx retry, 401/403 skip the route for 600 s | a fine-grained `HF_TOKEN` with "Make calls to Inference Providers" | not enabled: on 2026-09-26 the token lacked that permission <!-- verify-after-deploy --> |
| `vertex` | REST `POST https://{VERTEX_HOST}/v1/projects/{p}/locations/{l}/endpoints/{e}:predict` with google-auth (`GOOGLE_APPLICATION_CREDENTIALS`), instance key from the probe (`VERTEX_INPUT_KEY`: `inputs`, `prompt` or `text`); unwraps `[floats]`, `[[floats]]` or `{"embedding": …}` | a service-account key with `roles/aiplatform.user` on the project | not enabled: on 2026-09-26 the service account lacked the role <!-- verify-after-deploy --> |

`EMBED_PROVIDER=auto` (the VPS setting) builds the chain vertex (if configured) → hf (if a token is set)
→ local, behind a circuit breaker per provider (opens after 3 failures, retries after 60 s, 600 s for
auth failures) and a SQLite cache keyed by `sha1(model + text)` that both uvicorn workers share (blanks
are never cached). `GET /healthz` reports the active provider, each provider's status and latency, the
cache and the counters; `GET /healthz?probe=1` embeds a canary past the cache and answers 503 when
nothing works.

**Parity gate.** A remote provider is switched on only after, on the VPS:

```sh
cd /opt/ml && .venv/bin/python -m tools.hf_probe          # or tools.vertex_probe: DNS, input key, dim, latency
.venv/bin/python -m tools.parity_check --ml-url http://127.0.0.1:8000   # min cosine >= 0.995 vs the local goldens
```

The goldens (`ml/tests/fixtures/qwen3_golden.json`) were produced locally with the training settings. A
remote server that applies a prompt, different pooling or truncation would pass a shape check and still
move every vector; below the gate it stays off and `local` keeps serving.

**Vertex costs money while idle.** A Model Garden endpoint bills per node-hour while a model is
deployed. Undeploy it when it is not in use:
`gcloud ai endpoints undeploy-model <endpoint id> --region=<region> --deployed-model-id=<id>`.

## When we embed

| Trigger | Call from Go | What is stored |
|---|---|---|
| Sign-up | `POST /v1/user-profile` in the background (15 s) | the user's profile fields (empty profile → zero vector, "unranked") |
| `PUT /me/preferences` | save the preferences, then `POST /v1/user-profile` synchronously (15 s). On error keep the old vectors and clear `profileTextHash` so the next rank call retries | `positiveText`, `negativeText`, `positiveEmbedding`, `negativeEmbedding`, `embeddingModel`, `profileTextHash`, `profileUpdatedAt` |
| Facebook import | set `facebookInterests`, then refresh the profile | same |
| Any rank call | `ensureUserProfile` when the vector is missing, `embeddingModel` differs from the service's model, or the hash prefix is not the current template version | same |
| `POST /plans/generate` | `POST /v1/search-profile` for that request (5 s; skipped when mood text and quick picks are empty) | nothing on the user; the query vector goes into `plan_pools` |
| `PUT /ratings/{itemId}` | resolve the stop's activity; if it has `embeddingText` but no vector, `POST /v1/embed` (kind `activity`) and store it; then `POST /v1/compatibility/user-embedding/update` with the activity's vector: stars ≥ 3 fold into the positive embedding, below into the negative (`new = normalize(0.8·old + 0.2·activity)`) | the activity vector (guarded on `embeddingTextHash`), the user vector |
| Activities without vectors | `tools/embed_missing.py` on a systemd timer (15 min) | activity vectors on both catalogs |

`POST /v1/embed` takes `{texts: [...], kind: user|search|activity}`; the kind is for logs and metrics
only and never changes the text.

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
| ratings 1–5 for `outdoors, food, museums, live_music, nightlife, sports, shopping, big_crowds, early_mornings, long_walks` | 5 or 4 → likes (two bullets per section for a 5, one for a 4); 1 or 2 → dislikes (mirrored lexicon); 3 → nothing |
| `company`, `pace` | Social and Pace bullets ("small-group setting", "relaxed rhythm") |
| `spend`, `prefer_free`, `flexibility` | Cost bullets; `stick_to_budget` adds "expensive tickets" to the dislikes |
| `answers.perfect_afternoon`, `answers.plan_around` / `answers.never_do` | free text split into short phrases, routed to a section by keywords, positive / negative |
| Facebook `interests` | positive Interests |
| the 20 most recent rated stops | category phrases positive (≥ 4 stars) or negative (≤ 2), plus tag rules ("great people" → Social, "too crowded" → "large crowds", "too pricey" → "expensive tickets") |

Caps per section: Interests 10, Activities 8, Social 4, Environment 4, Pace 3, Cost 3, Timing 3,
Experience 3. An illustrative positive text for a user who loves the outdoors and long walks, likes live
music, prefers small groups and free things:

```text
Interests:
- outdoor recreation
- live music

Activities:
- park visit
- guided hike
- long walks
- live music performance

Social:
- small-group setting
- close friends

Pace:
- moderate pace
- extended walking

Cost:
- free admission
- under $15 admission
```

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
| `POST /v1/events/rank` | hard filters, then the classifier over `(positive', negative, event)` with `positive' = normalize(0.4·positive + 0.6·search)` (`SEARCH_WEIGHT`), optional Jev rerank of the top-k |
| Alternatives | `0.7·cosine(pool query vector, e) + 0.3·cached ML score` |
| Rating update | `normalize(0.8·old + 0.2·activity)` (`USER_EMBEDDING_ALPHA`) |

Scores from the classifier approximate an LLM judge's 0–3 rating ÷ 3; compare them within one user's
candidates, not across users ([ml/models.md](../ml/models.md)).

## Storage

| Collection | Fields | Notes |
|---|---|---|
| `users` | `positiveEmbedding`, `negativeEmbedding` (1024 floats), `positiveText`, `negativeText`, `embeddingModel`, `profileTextHash`, `profileUpdatedAt`, `facebookInterests` | never on the wire (`json:"-"`); `GET /me` carries no vector |
| `activities`, `demo_activities` | `embedding` (float64, unit norm), `embeddingModel`, `embeddingMeta {model, dimension, normalized, prompt: null, maxSeqLength, generatedAt, textHash, source, provider, backfill}`, `embeddingText`, `embeddingTextHash`, `embeddingTextMeta` | `source` is `mongo_backfill`, `embed_missing` or `go_on_demand`; the planner projects vectors only in phase B |
| `plan_pools` | `queryVector`, `hasNeg`, `scores {activityId: {ml, jev}}` | 6 h TTL; used by `/more`, `/route` and `/alternatives` |

## Environment

ML service (`/opt/ml/.env` and `ml.service`):

| Variable | Default | Purpose |
|---|---|---|
| `EMBED_PROVIDER` | `auto` | `auto`, `vertex`, `hf` or `local` |
| `EMBED_MODEL` / `EMBED_DIM` | `Qwen/Qwen3-Embedding-0.6B` / `1024` | must equal the catalog's `embeddingModel` |
| `HF_TOKEN` | – | read access to the private repos, and inference if it has the permission |
| `HF_EMBED_ROUTES` / `HF_ROUTER_BASE` | `deepinfra,hf-inference` / `https://router.huggingface.co` | |
| `HF_EMBED_TIMEOUT` / `HF_EMBED_MAX_BATCH` / `HF_EMBED_MAX_RETRIES` / `HF_AUTH_BACKOFF` | `20` / `32` / `3` / `600` | |
| `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT`, `VERTEX_LOCATION`, `VERTEX_ENDPOINT_ID`, `VERTEX_HOST`, `VERTEX_INPUT_KEY` | – | the Vertex provider (values in the design note and the probe output) |
| `EMBED_LOCAL_FALLBACK` / `EMBED_LOCAL_THREADS` / `EMBED_LOCAL_DEVICE` | `1` / `8` / `cpu` | |
| `EMBED_WARMUP` | `1` | background canary at startup |
| `EMBED_CACHE_PATH` / `EMBED_CACHE_MAX_ENTRIES` | `.cache/embeddings.sqlite` / `100000` | empty path disables the cache |
| `EMBED_MAX_CHARS` | `2000` | client-side truncation |
| `HF_HOME` | `/opt/ml/.cache` | the unit runs with `ProtectHome=true` |
| `RANKING_MODEL` / `RANKING_CHECKPOINT` / `RANKING_DEVICE` | `classifier` / bundled / `cpu` | the compatibility model ([ml/README.md](../ml/README.md)) |
| `SEARCH_WEIGHT` / `USER_EMBEDDING_ALPHA` / `RERANK_TOP_K` | `0.6` / `0.8` / `20` (12 in `ml.service`) | |
| `TYPESAFE_API_KEY` | – | enables Jev; unset means model order only |

Go API (`/opt/backend/.env`): `ML_SERVICE_URL` (`http://127.0.0.1:8000`), `ML_RERANK` (`true`; the kill
switch is `false`), `ML_RERANK_TOP_K` (12), `ML_RANK_TIMEOUT_MS` (2000), `ML_RANK_RERANK_TIMEOUT_MS`
(20000), `ML_EMBED_TIMEOUT_MS` (15000), `ML_SEARCH_TIMEOUT_MS` (5000). The Go client caches `/healthz`
for 60 s and only asks for a rerank when it reports `jev: true`.

## Jobs and checks

```sh
# fill missing or stale activity vectors through the running service (also what the timer runs)
cd /opt/ml && .venv/bin/python -m tools.embed_missing --uri 'mongodb://127.0.0.1:27017/?directConnection=true' \
  --db freetime --collections activities demo_activities [--dry-run] [--limit N]

# the service and its provider
curl -s 127.0.0.1:8000/healthz | jq '{status, jev, ranking, embedding: {model, dim, provider, providers}, profile_template_version}'
curl -s '127.0.0.1:8000/healthz?probe=1' | jq '.embedding.providers'
# a unit vector for a text, a zero vector for a blank: expect "Qwen/Qwen3-Embedding-0.6B 1024 <provider> [1.0, 0.0]"
curl -s 127.0.0.1:8000/v1/embed -H 'content-type: application/json' \
  -d '{"texts":["Interests:\n- live jazz\n\nCost:\n- free admission",""],"kind":"user"}' \
  | python3 -c 'import json,sys,math; r=json.load(sys.stdin); print(r["model"], r["dim"], r["provider"], [round(math.sqrt(sum(x*x for x in v)),6) for v in r["embeddings"]])'
# a user's stored profile (on the VPS; locally prefix with docker exec sq-mongo)
mongosh freetime --eval 'db.users.findOne({email:"demo@sidequestz.tech"},{profileTextHash:1,embeddingModel:1,positiveEmbedding:{$slice:3}})'
# nothing leaks to the app
curl -s https://api.sidequestz.tech/me -H "Authorization: Bearer $T" | grep -c positive_embedding   # expect 0
```

`ml-embed-missing.timer` runs the first command 2 minutes after boot and every 15 minutes; the catalog
backfill for many activities at once (texts and vectors on a GPU) is `ml/datagen/mongo_backfill.py` with
`ml/datagen/backfill.sbatch` on Raven, which is how the Atlanta catalog got its vectors on 2026-09-26.

## Retraining

The classifier is bound to this encoder and text format. To retrain, or to swap the encoder, follow
[ml/training.md](../ml/training.md) (`mpcdf.py submit raven ml/compatibility/classifier/train.sbatch`),
publish with `python -m compatibility.classifier.publish`, then re-embed every user and activity with
the new model and set `EMBED_MODEL` accordingly. Real labels for that retraining are on the
[roadmap](ROADMAP.md): `plan_runs.outcome` (what was saved out of what was shown) and ratings.
