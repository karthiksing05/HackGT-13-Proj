# SideQuestz ML

The ML stack that decides which events to show a user. Given a user's likes and dislikes and a set of
candidate events, it drops the events the user can't attend, scores the rest with a trained compatibility
model, optionally lets an LLM (Jev) re-judge the top few, and returns them best first. It also keeps a
user's preference embedding up to date as they interact with events.

Everything is served over a small, stateless FastAPI service. The service never stores or fetches
anything: the caller (the backend) sends embeddings and constraints in each request.

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
user likes/dislikes ────▶ eight-section texts ─Qwen3-Embedding──▶ positive / negative embeddings (stored with the user)

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
model was trained only on that format, so it scores other text less decisively.

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
│   └── helpers/              ranking pipeline (ranking.py) and hard filters (filters.py)
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
├── tests/                    API, ranking, reranker, checkpoint and updater tests
├── dataset_example.py        ranks a dataset user's candidates through the API (see the walkthrough below)
└── example.py                standalone demo of the Jev rerank (stubbed offline, or --live)
```

Dependencies only point inward: `api/` imports `compatibility/` and `reranking/`, and nothing outside
`api/` imports FastAPI. The scoring model is injected, so swapping cosine for the classifier (or a future
model) needs no change to the pipeline.

## Running the API

```bash
cd ml
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
uvicorn api.main:app --reload            # http://127.0.0.1:8000, interactive docs at /docs
```

The bundled checkpoint at `checkpoints/compatibility_classifier.pt` is loaded by default, so no network
access or tokens are needed to start. Configuration is read from the environment or a `.env` file (found
from the working directory upward; the repo's `.env` is gitignored):

| Variable | Default | Purpose |
|---|---|---|
| `RANKING_MODEL` | `classifier` | `classifier` or `cosine` (cosine doesn't need torch and accepts any dimension) |
| `RANKING_CHECKPOINT` | bundled checkpoint | Local path or `hf://<owner>/<repo>/<file>`, e.g. `hf://karthiksing05/sidequestz-compatibility-classifier/final/best.pt` |
| `HF_TOKEN` | – | Read access for a private `hf://` checkpoint |
| `RANKING_DEVICE` | `cpu` | Torch device for the classifier |
| `RANKING_MODEL_VERSION` | `classifier-v1` / `cosine-v1` | Overrides the reported `model_version` |
| `SEARCH_WEIGHT` | `0.6` | How far a search embedding pulls the user's positive embedding |
| `USER_EMBEDDING_ALPHA` | `0.8` | How much of the old preference an update keeps |
| `TYPESAFE_API_KEY` | – | Enables the Jev rerank; unset means model order only |
| `TYPESAFE_DEFAULT_MODEL`, `TYPESAFE_BASE_URL` | SDK defaults | Read by the TypeSafe SDK |
| `RERANK_TOP_K` | `20` | How many of the model's top events Jev scores |

### Tests

```bash
cd ml
python -m unittest discover
```

## Using the API

Two endpoints, both `POST` with JSON bodies. Errors come back as `{"detail": ...}`:

| Status | Meaning |
|---|---|
| 422 | Malformed body: wrong types, missing fields, NaN/inf, empty vectors, naive datetimes |
| 400 | Well-formed but unusable: wrong embedding dimension, duplicate event ids, all-zero positive or event embedding, `min_score` out of range |
| 500 | The model failed (details are logged, not returned) |

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
didn't judge. When `reranked` is `false`, the list is ordered by `score` alone.

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

The API takes embeddings, not text, so the caller embeds with the same encoder the model was trained on.
In production, compute event embeddings once when an event is created, and user embeddings when their
preferences change.

```python
import httpx
from sentence_transformers import SentenceTransformer

encoder = SentenceTransformer("Qwen/Qwen3-Embedding-0.6B")
encoder.max_seq_length = 512


def embed(text: str) -> list[float]:
    if not text.strip():
        return [0.0] * 1024  # no dislikes -> zero vector
    return encoder.encode(text, normalize_embeddings=True).tolist()


likes = "Interests:\n- live jazz\n- pottery\n\nSocial:\n- small-group setting\n\nCost:\n- under $20 admission"
dislikes = "Social:\n- large crowds\n\nEnvironment:\n- loud nightclub setting"
events = {
    "jazz-trio": "Interests:\n- jazz\n\nActivities:\n- live performance\n\nSocial:\n- small audience\n\nCost:\n- $10 cover",
    "edm-fest": "Interests:\n- electronic dance music\n\nSocial:\n- large crowd\n\nCost:\n- $120 ticket",
    "pottery-class": "Interests:\n- ceramics\n\nActivities:\n- hands-on workshop\n\nCost:\n- $15 materials fee",
}

response = httpx.post("http://127.0.0.1:8000/v1/events/rank", json={
    "user": {
        "positive_embedding": embed(likes), "negative_embedding": embed(dislikes),
        "positive_text": likes, "negative_text": dislikes,
        "latitude": 33.7756, "longitude": -84.3963, "max_distance_miles": 10,
    },
    "events": [{"id": i, "embedding": embed(t), "description": t} for i, t in events.items()],
    "options": {"limit": 10},
}, timeout=120)
response.raise_for_status()
for e in response.json()["events"]:
    print(f'{e["score"]:.3f}  {e["event_id"]}')
```

To reflect a search ("something outdoorsy this weekend"), write it as an eight-section text and send its
embedding as `search_embedding` and the text as `search_text`.

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
