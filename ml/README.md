# SideQuestz ML

The ML stack that decides which events to show a user. Given a user's likes and dislikes and a set of
candidate events, it drops the events the user can't attend, scores the rest with a trained compatibility
model, optionally lets an LLM (Jev) re-judge the top few, and returns them best first. It also turns the
app's preferences and plan requests into the texts and embeddings ranking needs, and keeps a user's
preference embedding up to date as they rate what they did.

Everything is served over a small FastAPI service that stores nothing (apart from an on-disk embedding
cache): the backend sends vectors, texts and constraints in each request and stores what comes back.
Python owns the text format and the encoder, so the backend never builds an embedding text or a vector.

| Doc | What it covers |
|---|---|
| [`models.md`](models.md) | The final compatibility model: architecture, how it was chosen, results, Python usage |
| [`training.md`](training.md) | The classifier training recipe |
| [`dataset.md`](dataset.md) | The synthetic events and users datasets (card for the HF dataset) |
| [`description_generation.md`](description_generation.md) | The eight-section text format that users and events are written in |

## How it works

```
                        ┌──────────────── offline ────────────────┐
raw event listing ──LLM─▶ eight-section text ──Qwen3-Embedding──▶ 1024-d event embedding  (stored with the event)

                        ┌──────── texts and vectors (profiles/ → embedding/) ────────┐
Preferences + Facebook interests + ratings ─▶ POST /v1/user-profile   ─▶ likes/dislikes texts + vectors (stored with the user)
PlanRequest: mood, quick picks, who, pace, time ─▶ POST /v1/search-profile ─▶ search text + vector (this request only)
an activity text without a vector ─▶ POST /v1/embed (tools/embed_missing, every 15 min) ─▶ its embedding

                        ┌──────────── POST /v1/events/rank ────────────┐
user + candidate events
  │ 1. validate          dimensions, duplicate ids, all-zero vectors          (400 on failure)
  │ 2. hard filters      max price · max distance · already finished ·
  │                      availability window · excluded categories
  │ 3. search blend      positive = normalize(0.4·positive + 0.6·search)      (this request only)
  │ 4. score             CompatibilityClassifier (or cosine), one batched pass
  │ 5. min_score         drop low scores
  │ 6. Jev rerank        LLM scores the model's top k (0–4) — optional
  │ 7. sort + limit      Jev-scored first by Jev score, then by model score
  ▼
ranked event ids with scores
```

**Everything is written in one format.** Users' likes, users' dislikes, searches and events are all
eight-section texts (Interests, Activities, Social, Environment, Pace, Cost, Timing, Experience) embedded
with the frozen `Qwen/Qwen3-Embedding-0.6B` (1024-d, L2-normalized, no prompt, `max_seq_length` 512). The
model was trained only on that format, so it scores other text less decisively. **No instruction prefix
is ever added** (`Instruct: …\nQuery:`), for any kind of text: the classifier's training embeddings and
the stored activity vectors have none, and a prefix would move users and searches out of their space.

**The compatibility model** is a 1.85M-parameter late-fusion network over (positive, negative, event)
embeddings. On held-out users and events it gets NDCG@10 0.897 vs. 0.849 for a tuned cosine baseline.
Scores approximate an LLM judge's 0–3 rating ÷ 3 (≥ 0.5 ≈ a good match), but compare them within one
user's candidates rather than across users. Details in [`models.md`](models.md).

**Jev** (via the TypeSafe SDK) is an LLM judge that scores each of the model's top-k events on a 0–4 scale
from the user's texts and the event descriptions. It is optional and can never fail a request: if it's not
configured, the request has no texts, or the call errors, the response comes back in model order with
`reranked: false`.

## Layout

```
ml/
├── api/                      FastAPI service (the only part that knows about HTTP)
│   ├── main.py               ASGI entry point, configured from env vars
│   ├── app.py                app factory, error → status-code mapping
│   ├── checkpoints.py        resolves local / hf:// checkpoint references
│   ├── routes/               HTTP → call translation only
│   ├── schemas/              request/response models (pydantic)
│   └── helpers/              ranking pipeline, hard filters, embedding / profile / health services
├── embedding/                embedding providers (Vertex AI, HF router, local Qwen), fallback, cache
├── profiles/                 eight-section profile and search texts from the app's inputs
├── compatibility/            scoring library
│   ├── models/               CompatibilityModel interface + cosine baseline
│   ├── classifier/           the trained model: architecture, training, eval, API adapter (scorer.py)
│   ├── encoders/             text → embedding encoders
│   ├── user_embedding/       moving-average user-embedding updater
│   └── service.py            CompatibilityService: encode + score
├── reranking/                Jev LLM rerank stage (request building, parsing, fallbacks)
├── checkpoints/              bundled CPU copy of the final classifier (loaded by default)
├── data/                     embedding the datasets, HF dataset loader
├── datagen/                  synthetic event/user generation and LLM judging (Slurm jobs on MPCDF Raven)
├── tools/                    probes, parity gate, golden fixtures, embed-missing job (python -m tools.<name>)
├── tests/                    unit tests; fixtures/ holds the byte-exact text goldens and the Qwen golden
├── ml.service                systemd unit of the service; ml-embed-missing.{service,timer}: the 15-min job
├── deploy.sh                 VPS deploy (see "Deploying")
├── requirements-serve.txt    what the service needs; requirements.txt adds training and data generation
├── dataset_example.py        ranks a dataset user's candidates through the API (see the walkthrough below)
└── example.py                standalone demo of the Jev rerank (stubbed offline, or --live)
```

Dependencies only point inward: `api/` imports `compatibility/`, `reranking/`, `embedding/` and
`profiles/`, and nothing outside `api/` imports FastAPI. The scoring model and the embedder are injected,
so swapping cosine for the classifier, or one provider for another, needs no change to the pipeline.

## Running the API

```bash
cd ml
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt           # requirements-serve.txt is enough to serve
uvicorn api.main:app --reload             # http://127.0.0.1:8000, interactive docs at /docs
```

The bundled checkpoint at `checkpoints/compatibility_classifier.pt` is loaded by default, and the local
embedder downloads `Qwen/Qwen3-Embedding-0.6B` once into the Hugging Face cache, so no tokens are needed to
start. Configuration is read from the environment or a `.env` file (found from the working directory
upward; the repo's `.env` is gitignored):

| Variable | Default | Purpose |
|---|---|---|
| `RANKING_MODEL` | `classifier` | `classifier` or `cosine` (cosine doesn't need torch and accepts any dimension) |
| `RANKING_CHECKPOINT` | bundled checkpoint | Local path or `hf://<owner>/<repo>/<file>`, e.g. `hf://karthiksing05/sidequestz-compatibility-classifier/final/best.pt` |
| `HF_TOKEN` | – | Read access for a private `hf://` checkpoint; also the HF router provider (see below) |
| `RANKING_DEVICE` | `cpu` | Torch device for the classifier |
| `RANKING_MODEL_VERSION` | `classifier-v1` / `cosine-v1` | Overrides the reported `model_version` |
| `SEARCH_WEIGHT` | `0.6` | How far a search embedding pulls the user's positive embedding |
| `USER_EMBEDDING_ALPHA` | `0.8` | How much of the old preference an update keeps |
| `TYPESAFE_API_KEY` | – | Enables the Jev rerank; unset means model order only |
| `TYPESAFE_DEFAULT_MODEL`, `TYPESAFE_BASE_URL` | SDK defaults | Read by the TypeSafe SDK |
| `RERANK_TOP_K` | `20` (`12` in `ml.service`) | How many of the model's top events Jev scores |
| `RERANK_TIMEOUT_SECONDS` | `15` | After this the model order is returned (`reranked: false`); below the backend's 20 s rerank deadline |
| `LOG_LEVEL` | `INFO` | The app's logs: one line per embed call, fallbacks, warmup |

Embeddings (see [Embedding providers](#embedding-providers)):

| Variable | Default | Purpose |
|---|---|---|
| `EMBED_PROVIDER` | `auto` | `auto` / `vertex` / `hf` / `local` |
| `EMBED_MODEL` / `EMBED_DIM` | `Qwen/Qwen3-Embedding-0.6B` / `1024` | Must equal the activities' `embeddingModel` |
| `HF_EMBED_ROUTES` / `HF_ROUTER_BASE` | `deepinfra,hf-inference` / `https://router.huggingface.co` | HF router routes, tried `live` ones first |
| `HF_EMBED_TIMEOUT` / `HF_EMBED_MAX_BATCH` / `HF_EMBED_MAX_RETRIES` / `HF_AUTH_BACKOFF` | `20` / `32` / `3` / `600` | Seconds; texts per request; retries on 429/5xx; seconds a route is skipped after a 401/403 |
| `GOOGLE_APPLICATION_CREDENTIALS` | – | Service-account key for Vertex AI (needs `roles/aiplatform.user`) |
| `VERTEX_PROJECT` / `VERTEX_LOCATION` / `VERTEX_ENDPOINT_ID` | our Model Garden endpoint | |
| `VERTEX_HOST` / `VERTEX_INPUT_KEY` | regional host / `inputs` | The dedicated endpoint DNS and instance key that `tools.vertex_probe` finds |
| `EMBED_LOCAL_FALLBACK` / `EMBED_LOCAL_THREADS` / `EMBED_LOCAL_DEVICE` | `1` / `8` / `cpu` | Local Qwen behind a pinned remote provider; torch threads; device |
| `EMBED_WARMUP` | `1` | Load and probe the providers on a background thread at startup (startup never waits) |
| `EMBED_CACHE_PATH` / `EMBED_CACHE_MAX_ENTRIES` | `.cache/embeddings.sqlite` / `100000` | SQLite cache shared by the workers; an empty path disables it |
| `EMBED_MAX_CHARS` | `2000` | Longer texts are cut before a remote call |
| `HF_HOME` | HF default | Where the local model's weights live (`/opt/ml/.cache` on the VPS, which hides `/root`) |

### Tests

```bash
cd ml
python -m unittest discover        # no network or model needed; the Mongo tests skip without a local mongod
ML_TEST_LOCAL_EMBEDDER=1 python -m unittest tests.test_local_embedder   # loads Qwen, checks the goldens
ML_TEST_HF=1 HF_TOKEN=… python -m unittest tests.test_hf_parity         # the HF router against the goldens

cd ../Backend
go test ./pkg/ml/                                                       # the Go client, against httptest fakes
ML_LIVE_URL=http://127.0.0.1:8000 go test ./pkg/ml/ -run Live -v         # the Go client against a running service
```

`tests/test_embed_missing.py` uses the MongoDB at `ML_TEST_MONGO_URI` (default `127.0.0.1:27017`) in a
throwaway `sq_test_*` database. The text goldens in `tests/fixtures/` are compared byte for byte; after an
intentional change to `profiles/`, regenerate them with `python -m tools.make_golden --profiles`, review the
diff and bump `profiles.TEMPLATE_VERSION`.

## Using the API

Every endpoint takes and returns JSON. Errors come back as `{"detail": ...}`:

| Status | Meaning |
|---|---|
| 422 | Malformed body: wrong types, missing fields, unknown enum values, NaN/inf, empty vectors, naive datetimes |
| 400 | Well-formed but unusable: wrong embedding dimension, duplicate event ids, all-zero positive or event embedding, `min_score` out of range |
| 500 | The model failed (details are logged, not returned) |
| 503 | No embedding provider could serve (`"Embedding provider unavailable."`); `/healthz?probe=1` when its canary fails |

The backend calls all of them through `Backend/pkg/ml` (`UserProfile`, `SearchProfile`, `Embed`,
`RankEvents`/`Rank`/`RankActivities`, `UpdateUserEmbedding`, `Health`), which adds deadlines and never
invents a vector when a call fails.

### `POST /v1/events/rank`

Filters, scores and ranks candidate events for one user.

**Request**

| Field | Type | Notes |
|---|---|---|
| `user.positive_embedding` | `float[1024]` | Embedding of the user's likes. Required, not all zeros |
| `user.negative_embedding` | `float[1024]` | Embedding of the user's dislikes; **all zeros** when they have none |
| `user.positive_text`, `user.negative_text` | `string?` | The eight-section texts behind the embeddings. Needed for the Jev rerank |
| `user.max_price` | `number?` | Drops events with `price` above it |
| `user.latitude`, `user.longitude`, `user.max_distance_miles` | `number?` | Drops events farther away (haversine) |
| `user.available_start`, `user.available_end` | `datetime?` | Timezone-aware ISO 8601. Drops events outside the window |
| `user.excluded_categories` | `string[]` | Case-insensitive match on `event.category` |
| `events[].id` | `string` | Opaque, must be unique in the request |
| `events[].embedding` | `float[1024]` | Embedding of the event's eight-section text |
| `events[].description` | `string?` | The eight-section text. Events without one aren't sent to Jev |
| `events[].price`, `start_time`, `end_time`, `latitude`, `longitude`, `category` | optional | Used by the filters. Events that have already ended are always dropped |
| `search_embedding` | `float[1024]?` | What the user searched for this time; blended into the positive embedding, never stored |
| `search_text` | `string?` | The same search, as text, for Jev |
| `options.min_score` | `number?` | Drops events scoring below it |
| `options.limit` | `int?` | Max results, applied after the rerank |
| `options.rerank` | `bool` | `false` skips Jev (default `true`) |
| `options.rerank_top_k` | `int?` | Overrides `RERANK_TOP_K` for this request |

A filter only applies when both the user and the event have the data it needs: missing data never drops
an event.

**Response**

```json
{
  "events": [
    {"event_id": "jazz-trio", "score": 0.692, "rerank_score": 4.0},
    {"event_id": "pottery-class", "score": 0.770, "rerank_score": 3.99},
    {"event_id": "edm-fest", "score": 0.156, "rerank_score": 0.0}
  ],
  "model_version": "classifier-v1",
  "reranked": true
}
```

`score` is the compatibility model's score; `rerank_score` is Jev's 0–4 score, or `null` for events Jev
didn't judge. When `reranked` is `false` (Jev skipped, failed or slower than `RERANK_TIMEOUT_SECONDS`),
the list is ordered by `score` alone. Events removed by the filters, `min_score` or `limit` are absent, and
callers must not add them back; the backend sends its own constraints again so the filters act as a second
check.

### `POST /v1/compatibility/user-embedding/update`

Folds an event the user interacted with into one of their preference embeddings. Call it with the
positive embedding when they liked or attended an event, and with the negative one when they disliked it,
then store the result.

```
new = normalize(α · embedding + (1 − α) · event_embedding)      α = USER_EMBEDDING_ALPHA (0.8)
```

```json
// request
{"embedding": [/* 1024 floats */], "kind": "positive", "event_embedding": [/* 1024 floats */]}
// response
{"embedding": [/* 1024 floats, unit norm */], "kind": "positive"}
```

An all-zero `embedding` (no signal yet) becomes the event's direction, so this also builds a user's
first negative embedding. `kind` is echoed back for the caller's bookkeeping; the math is the same for both.

### `POST /v1/compatibility/users`

How well other users' taste matches one user's, for People for you. Symmetric cosine math over the
stored likes (`p`) and dislikes (`n`) vectors, "likes minus clashes":

```
score = cos(pA, pB) − 0.5 · (cos(pA, nB) + cos(nA, pB)) / 2        percent = round(100 · clamp(score, 0, 1))
```

An all-zero `negative_embedding` means no dislikes and adds nothing.

```json
// request
{"user": {"positive_embedding": [/* 1024 */], "negative_embedding": [/* 1024 */]},
 "candidates": [{"id": "u2", "positive_embedding": [/* 1024 */], "negative_embedding": [/* 1024 */]}]}
// response: best first
{"results": [{"id": "u2", "score": 0.81, "percent": 81}]}
```

400 for mismatched dimensions, repeated ids or an all-zero user `positive_embedding`.

### `POST /v1/compatibility/itineraries`

How well each itinerary fits a user: the mean compatibility-model score (the ranking route's model, no
filters, no Jev) over its stops, all scored in one batch. `percent = round(100 · clamp(mean, 0, 1))`.

```json
// request
{"user": {"positive_embedding": [/* 1024 */], "negative_embedding": [/* 1024 */]},
 "itineraries": [{"id": "plan-1", "events": [{"id": "act-1", "embedding": [/* 1024 */]}]}]}
// response: best first; itineraries without events are absent
{"results": [{"id": "plan-1", "score": 0.74, "percent": 74, "scored_events": 1}], "model_version": "classifier-v1"}
```

Both routes are stateless: the Go API sends the vectors and nothing is stored.

### `POST /v1/user-profile`

Turns the app's preferences into the user's likes and dislikes texts, embeds both, and hashes them. The
backend stores all of it on the user.

| Field | Type | Notes |
|---|---|---|
| `ratings` | `{key: 1–5}` | `outdoors, food, museums, live_music, nightlife, sports, shopping, big_crowds, early_mornings, long_walks` (camelCase and aliases accepted, unknown keys ignored). 5 or 1 adds two bullets per section, 4 or 2 one, 3 nothing |
| `company`, `pace`, `spend`, `flexibility`, `prefer_free` | contract enums | `pace` also accepts `chill`; `stick_to_budget` adds "expensive tickets" to the dislikes |
| `answers` | `{perfect_afternoon, never_do, plan_around}` | Free text, cut into short phrases and routed to sections by keyword; `never_do` goes to the dislikes |
| `facebook_interests` | `string[]` | From the Facebook import |
| `rated_events` | `[{stars, tags, category, activity_tags}]` | Newest first, the first 20 count: 4–5 stars → likes, 1–2 → dislikes, by category and the activity's tags; rating tags ("Great people", "Too crowded", …) apply either way |
| `embed` | `bool` | `false`: texts and hash only |

The response has `positive_text`, `negative_text`, `positive_embedding`, `negative_embedding` (all zeros
when there are no dislikes), `profile_text_hash` (`"profile-v1:" + sha1(positive + "\n---\n" + negative)`),
`template_version`, `model`, `dim` and `provider`. An empty profile has `positive_text: ""` and a zero
positive vector: rank such a user by the search vector or a prior. The backend rebuilds a stored profile
when the hash doesn't start with `/healthz`'s `profile_template_version` or its `embeddingModel` differs.

For the contract's `Preferences` example, Jordan's Facebook interests and two rated events
([`tests/fixtures/profile_jordan.json`](tests/fixtures/profile_jordan.json)):

<table>
<tr><th><code>positive_text</code></th><th><code>negative_text</code></th></tr>
<tr><td>

```text
Interests:
- outdoor recreation
- local food
- hiking
- indie rock
- coffee
- street food
- board games

Activities:
- park visit
- guided hike
- food tasting

Social:
- small-group setting
- close friends
- meeting new people

Environment:
- outdoor setting

Pace:
- moderate pace

Cost:
- free admission
- under $15 admission
```

</td><td valign="top">

```text
Interests:
- nightlife

Social:
- large crowds

Environment:
- loud nightclub setting

Timing:
- late night
```

</td></tr>
</table>

### `POST /v1/search-profile`

Turns one plan request into the search text and vector, used for that request only (`search_text` and
`search_embedding` of `/v1/events/rank`). Fields: `mood_text`, `tags` (quick picks; unknown ones become
Interests), `who` (`just_me | friends | open`), `pace`, `budget` (0–3), `start_time` and `back_by`
(timezone-aware), `timezone` (the viewer's IANA zone, for "friday afternoon") and `embed`. The response
has `search_text`, `search_embedding` (`null` when the text is empty), `model`, `dim` and `provider`. The
contract's `PlanRequest` ([`tests/fixtures/search_jordan.json`](tests/fixtures/search_jordan.json), Friday
14:10 in Atlanta, back by 18:30) gives:

```text
Interests:
- outdoor recreation
- local food

Activities:
- eating out
- social mixer

Social:
- meeting new people
- small group of friends

Environment:
- outdoor setting
- outside

Pace:
- moderate pace
- chill

Cost:
- under $15 admission
- cheap food

Timing:
- friday afternoon
- weekday afternoon
- multi-hour outing
```

### `POST /v1/embed`

Raw texts to vectors: for activity texts (the backend's embed-on-demand and `tools.embed_missing`). User
and search texts come from the two endpoints above, which also build the text.

```json
// request: 1-64 texts of at most 8000 characters; kind (user | search | activity) is for logging only
{"texts": ["Interests:\n- live jazz\n\nCost:\n- free admission", ""], "kind": "activity"}
// response: a unit vector per text, the zero vector for a blank one
{"embeddings": [[/* 1024 floats */], [/* 1024 zeros */]], "model": "Qwen/Qwen3-Embedding-0.6B", "dim": 1024,
 "provider": "local", "cached": 0}
```

`provider` is who embedded the uncached texts: `vertex`, `hf:deepinfra`, `hf:hf-inference`, `local`, or
`cache` when every text was cached.

### `GET /healthz`

```json
{"status": "ok", "uptime_seconds": 812.4, "jev": true, "profile_template_version": "profile-v1",
 "ranking": {"model_version": "classifier-v1", "embedding_dim": 1024},
 "embedding": {"model": "Qwen/Qwen3-Embedding-0.6B", "dim": 1024, "mode": "auto", "provider": "local",
               "providers": {"vertex": {"status": "disabled", "detail": "disabled until a probe returns a 1024-d vector; …"},
                             "hf:deepinfra": {"status": "error", "detail": "auth: HTTP 401 …; mapping=live; auth backoff 568s"},
                             "hf:hf-inference": {"status": "error", "detail": "…"},
                             "local": {"status": "loaded", "detail": "device=cpu threads=8 load_s=5.1"}},
               "cache": {"enabled": true, "entries": 4, "hits": 2, "misses": 4},
               "stats": {"calls": 5, "texts": 8, "cached": 2, "fallbacks": 1, "errors": 0}}}
```

`status` is `degraded` when no provider can serve (ranking still works). `?probe=1` first embeds a canary
through the providers, bypassing the cache, and answers 503 with the same report when that fails; the
deploy script gates on it.

### Quick smoke test

With the cosine model, any dimension works, so the pipeline can be tried with tiny vectors:

```bash
RANKING_MODEL=cosine uvicorn api.main:app
```

```bash
curl -s localhost:8000/v1/events/rank -H 'content-type: application/json' -d '{
  "user": {"positive_embedding": [1, 0, 0], "negative_embedding": [0, 0, 1], "max_price": 50},
  "events": [
    {"id": "match",    "embedding": [0.9, 0.1, 0]},
    {"id": "disliked", "embedding": [0, 0.2, 0.9]},
    {"id": "too-pricey", "embedding": [1, 0, 0], "price": 120}
  ]
}'
```

`match` ranks above `disliked`, and `too-pricey` is filtered out.

### Calling it with real embeddings

The ranking endpoint takes embeddings; the service makes them too, with the encoder the model was trained
on. In production, activity vectors are computed once per text (the backfill and `tools.embed_missing`)
and user vectors when preferences change (`/v1/user-profile`, stored by the backend).

```python
import httpx

ML = "http://127.0.0.1:8000"
profile = httpx.post(f"{ML}/v1/user-profile", json={
    "ratings": {"live_music": 5, "big_crowds": 1}, "company": "small_group", "spend": "under_15",
    "answers": {"perfect_afternoon": "Live jazz somewhere small, then a pottery class."},
}, timeout=120).json()
events = {
    "jazz-trio": "Interests:\n- jazz\n\nActivities:\n- live performance\n\nSocial:\n- small audience\n\nCost:\n- $10 cover",
    "edm-fest": "Interests:\n- electronic dance music\n\nSocial:\n- large crowd\n\nCost:\n- $120 ticket",
    "pottery-class": "Interests:\n- ceramics\n\nActivities:\n- hands-on workshop\n\nCost:\n- $15 materials fee",
}
vectors = httpx.post(f"{ML}/v1/embed", json={"texts": list(events.values()), "kind": "activity"}, timeout=120).json()["embeddings"]

response = httpx.post(f"{ML}/v1/events/rank", json={
    "user": {
        "positive_embedding": profile["positive_embedding"], "negative_embedding": profile["negative_embedding"],
        "positive_text": profile["positive_text"], "negative_text": profile["negative_text"],
        "latitude": 33.7756, "longitude": -84.3963, "max_distance_miles": 10,
    },
    "events": [{"id": i, "embedding": v, "description": t} for (i, t), v in zip(events.items(), vectors)],
    "options": {"limit": 10, "rerank": False},
}, timeout=120)
response.raise_for_status()
for e in response.json()["events"]:
    print(f'{e["score"]:.3f}  {e["event_id"]}')
# 0.729  jazz-trio
# 0.578  pottery-class
# 0.188  edm-fest
```

For a search ("something outdoorsy this weekend"), send the `search_text` and `search_embedding` from
`/v1/search-profile` along with the user.

### Walkthrough: a user from the dataset

[`dataset_example.py`](dataset_example.py) takes one user and their 20 candidate events from the `users`
dataset. It embeds their texts, posts them to `/v1/events/rank` (the app from `api.main`, called
in-process), and compares the result with the dataset's LLM-judge ratings (0–3). It needs `HF_TOKEN`, and
runs the Jev rerank if `TYPESAFE_API_KEY` is set.

```bash
cd ml
python dataset_example.py                          # train user #4, shown below
python dataset_example.py --split test --index 0   # a held-out user
```

This user is from the **train** split, so the classifier saw their judge ratings during training; pick a
`test` user to see how it does on unseen users and events. The dataset carries no prices, times or
locations as structured fields, so the hard filters don't drop anything here.

**The user (`u00004`).** The persona the data was generated from: interested in study groups (campus
life); dislikes arts and culture and live music; goes with a partner or close friends; fine with $20–50;
flexible schedule (a shift worker with odd hours); prefers indoor venues; high-energy and intense. The API
receives these two texts and their embeddings:

<table>
<tr><th>Likes (<code>positive_text</code>)</th><th>Dislikes (<code>negative_text</code>)</th></tr>
<tr><td>

```text
Interests:
- academic discussion
- career development
- skill-building

Activities:
- guided study session
- problem-solving workshop
- interactive presentation

Social:
- partner or close friend
- small-group collaboration

Environment:
- indoor venue
- focused atmosphere

Pace:
- high-energy rhythm
- intense engagement

Cost:
- moderate price point
- under $50 admission

Timing:
- flexible schedule
- odd hour availability

Experience:
- active participation
- collaborative learning
```

</td><td valign="top">

```text
Interests:
- arts and culture
- visual arts
- performing arts

Activities:
- live music performance
- theater production
- art gallery viewing

Environment:
- outdoor setting
- cultural venue
```

</td></tr>
</table>

**The candidates.** 20 events: 8 retrieved by cosine similarity to the likes, 4 hard negatives (close to
the likes but with something the user wouldn't want, e.g. an art-gallery venue) and 8 random ones (a
reptile expo, a running club, a jazz night, ...).

**The ranking.** The model scores all 20, Jev scores the model's top 20 (here, all of them), and the
response is sorted by Jev's score, with ties broken by the model's score. `judge` is the dataset's 0–3
rating, which the API never sees:

| # | Model score | Jev score (0–4) | Judge rating (0–3) | Candidate type | Event |
|---|---|---|---|---|---|
| **1** | 0.616 | 3.73 | 3 | retrieved | study group |
| **2** | 0.762 | 3.68 | 2 | retrieved | late-night study break |
| **3** | 0.684 | 3.68 | 3 | retrieved | guest lecture |
| **4** | 0.829 | 3.50 | 3 | retrieved | study group |
| **5** | 0.869 | 3.31 | 3 | retrieved | study group |
| 6 | 0.746 | 3.26 | 2 | retrieved | study group |
| 7 | 0.827 | 3.18 | 3 | hard negative | study group |
| 8 | 0.803 | 3.12 | 2 | retrieved | study group |
| 9 | 0.529 | 3.08 | 3 | hard negative | guest lecture |
| 10 | 0.666 | 3.07 | 2 | random | robotics demo day |
| 11 | 0.686 | 3.00 | 2 | hard negative | public speaking |
| 12 | 0.663 | 2.85 | 2 | random | founder office hours |
| 13 | 0.493 | 1.98 | 1 | random | puzzle hunt |
| 14 | 0.500 | 1.81 | 1 | retrieved | study group |
| 15 | 0.277 | 1.39 | 1 | random | aquarium hobbyist swap |
| 16 | 0.516 | 1.24 | 1 | hard negative | guest lecture (in an art gallery) |
| 17 | 0.098 | 1.14 | 0 | random | reptile expo |
| 18 | 0.348 | 0.82 | 1 | random | running club |
| 19 | 0.248 | 0.60 | 0 | random | holiday market |
| 20 | 0.203 | 0.08 | 1 | random | jazz night |

Both stages put the academic events on top and the disliked or off-topic ones (the jazz night, the holiday
market, the reptile expo) at the bottom. By the model alone, the top 5 are rated 3, 3, 3, 2, 2 by the
judge (NDCG@10 **0.931**). After Jev they're rated 3, 2, 3, 3, 3 (NDCG@10 **0.972**). Jev's scores vary a
little between runs, so the exact order of close events can change.

<details>
<summary><b>The top 5 events' texts</b> (what the model and Jev saw)</summary>

| # | Event text |
|---|---|
| 1 | **study group**: Interests: academic challenges, campus life · Activities: team-based collaboration, competitive study session · Environment: convention center, indoor setting · Pace: fast-paced, quick problem solving · Cost: $30 admission · Timing: weekend early morning, 07:00 start · Experience: all skill levels |
| 2 | **late-night study break**: Interests: academic study, campus life · Activities: team-based collaboration, intensive study session · Environment: community center · Pace: focused work, structured break · Cost: free for students, paid general admission · Timing: friday evening, one-hour duration · Experience: intermediate-level |
| 3 | **guest lecture**: Interests: academic disciplines, campus life, scholarly dialogues · Activities: guest lecture, team-based problem-solving, collaborative exercises · Environment: hybrid event, main lecture hall, digital stream · Pace: intensive, interactive · Cost: paid admission · Timing: sunday afternoon, full-day event · Experience: beginner-friendly, formal introduction, small group work |
| 4 | **study group**: Interests: academic study, peer networking, exam preparation · Activities: group learning, student networking · Environment: convention center, indoor venue · Pace: collaborative learning · Cost: paid admission · Timing: monthly recurring, first friday, morning session · Experience: beginner-friendly, social learning |
| 5 | **study group**: Interests: academic study, syllabus discussion · Activities: drop-in format, collaborative learning, homework assistance · Environment: coworking space, small group setting, casual atmosphere · Pace: flexible timing, low-pressure social · Cost: free for students, nominal fee for general admission · Timing: weekday evening, monthly recurring · Experience: deadline support, quiet focus option |

Each is sent as eight-section text (one `Section:` header and `- item` lines per section), condensed here
to one line.

</details>

For reference, the judge's reasons for two of the extremes: the #1 event had "Strong alignment on academic
focus, collaborative activities, and indoor setting; cost and pace match well" (rating 3). The art-gallery
guest lecture (#16) was marked down because "the event's art gallery setting conflicts with the user's
dislikes" (rating 1).

## Embedding providers

```
EMBED_PROVIDER=auto   cache ─▶ Vertex AI (only after its startup probe returned a 1024-d vector)
                            ─▶ HF router: deepinfra, hf-inference (routes the Hub reports live go first)
                            ─▶ local Qwen3-Embedding-0.6B (sentence-transformers, fp32, CPU)
```

Every provider returns the same thing: `Qwen/Qwen3-Embedding-0.6B` vectors, 1024-d, L2-normalized, with
no instruction prefix, and the zero vector for a blank text. A provider that fails three times in a row
is skipped for 60 s (600 s after an auth error), each fallback is logged, and when all fail the endpoints
answer 503. `vertex`, `hf` and `local` pin one provider, with the local model behind a remote one unless
`EMBED_LOCAL_FALLBACK=0`. Vectors are cached on disk by (model, text) for both workers.

**A remote provider is switched on only after parity:** `python -m tools.parity_check --provider hf`
(or `vertex`) must report every cosine ≥ 0.995 against the local goldens. The classifier was trained on
the local model's vectors and the stored activity vectors come from the same model (local fp32 against the
stored bf16 backfill: cosine 0.9997).

Status on 2026-09-26 (`tools.hf_probe`, `tools.vertex_probe`):

| Provider | Status |
|---|---|
| HF router | The `HF_TOKEN` in the repo `.env` is rejected as invalid (401 on `whoami` and on both routes); the model's mapping says deepinfra `live`, hf-inference `error`. Needs a valid fine-grained token with "Make calls to Inference Providers", then parity |
| Vertex AI | The key's service account (the project's default compute account) gets a token but is denied `aiplatform.endpoints.get` and `aiplatform.endpoints.predict` (403). Grant `roles/aiplatform.user`, rerun the probe for the dedicated DNS and input key, then parity. A deployed endpoint bills per node-hour, idle or not |
| Local | Works: about 5 s to load on a laptop (20–60 s expected on the VPS). This is what serves today |

## Tools

Run from `ml/` as `python -m tools.<name>`; each has `--help`.

| Tool | What it does |
|---|---|
| `hf_probe` | The token's permission, the model's provider mapping, one embed per HF route. Never prints the token |
| `vertex_probe` | The key's account and a token, the endpoint's description, host DNS, then the input key that works; prints the `VERTEX_HOST` / `VERTEX_INPUT_KEY` to set |
| `parity_check --ml-url URL` or `--provider NAME` | The parity gate against `tests/fixtures/qwen3_golden.json` (≥ 0.995) |
| `make_golden [--profiles] [--embeddings]` | Regenerates the fixtures: the profile and search texts, and the five-text local Qwen golden |
| `rank_smoke [--catalog demo_activities] [--profile FILE] [--mood TEXT]` | Ranks stored catalog vectors (read-only) for a fixture profile through a running service and prints the best and worst, to judge the order by eye |
| `embed_missing [--dry-run] [--no-stale] [--limit N]` | Embeds activities without a current vector through the service, with guarded writes (never over a changed text or a vector from elsewhere); every 15 min on the VPS (`ml-embed-missing.timer`) |

## Deploying

`./deploy.sh [--skip-tests] [host] [user] [password]` runs the unit tests, snapshots `/opt/ml` to
`/opt/ml.prev`, uploads the code (never `.env`, `gcp-sa.json`, keys, caches, venvs, `datagen/` or
`data/`), sets directories 755, files 644 and secrets 600, installs `requirements-serve.txt` (CPU torch),
fetches the model weights into `/opt/ml/.cache`, installs `ml.service` and the embed-missing timer, and
fails unless `/healthz` and `/healthz?probe=1` answer. Secrets are placed by hand: `/opt/ml/.env` (600)
with `HF_TOKEN`, `TYPESAFE_API_KEY` and `GOOGLE_APPLICATION_CREDENTIALS=/opt/ml/gcp-sa.json`, plus
`/opt/ml/gcp-sa.json` (600). Rollback: `rm -rf /opt/ml && mv /opt/ml.prev /opt/ml && systemctl restart ml`.

## Data and training

The model was trained on synthetic data generated and judged by `Qwen/Qwen3.5-9B` on the MPCDF Raven
cluster:

1. `datagen/generate.py`: 100k synthetic raw event listings, each converted to eight-section text.
2. `datagen/generate_users.py`: 10k synthetic users with eight-section likes and dislikes.
3. `datagen/select_candidates.py`: 20 candidate events per user (retrieved, hard negatives, random), split
   80/10/10 by user with disjoint event pools.
4. `datagen/judge.py`: the LLM rates each (user, event) pair 0–3; the label is rating ÷ 3.
5. `data/embed.py`: embeds every text with `Qwen3-Embedding-0.6B`.
6. `compatibility/classifier/train.sbatch`: runs the training recipe from [`training.md`](training.md)
   and logs to W&B; `python -m compatibility.classifier.publish` uploads the result.

The datasets and full checkpoints live in private Hugging Face repos (see [`models.md`](models.md)); set
`HF_TOKEN` or run `hf auth login` to access them. `python -m data.hf_dataset` prints a summary.

## Limitations

- Labels come from an LLM judge on synthetic users and events, not real behaviour. Retrain on logged
  impressions and outcomes once the app has them.
- The model is tied to `Qwen3-Embedding-0.6B` and the eight-section format; changing either means
  retraining.
- It's a re-ranker: shortlist candidates (e.g. by cosine) before sending them, rather than sending the
  whole catalogue.
- The search blend (`SEARCH_WEIGHT`) wasn't part of training and should be tuned against real searches.
- The profile and search texts come from hand-written tables (`profiles/lexicon.py`), not a model. They
  follow the training format and the activity texts' vocabulary, but their wording hasn't been tuned
  against real users.
