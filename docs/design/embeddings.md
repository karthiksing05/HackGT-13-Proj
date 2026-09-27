# Embeddings and ML-service design

Owner: agent **ml** (Python side + the Go `pkg/ml` client). Paths relative to `ml/` unless noted.

## 0. Ground truth

- Activities in Mongo carry `embedding` (1024-d float64, unit norm), `embeddingModel: "Qwen/Qwen3-Embedding-0.6B"`, `embeddingMeta{dimension, normalized, prompt: null, maxSeqLength: 512, textHash, backfill}` written by `datagen/mongo_backfill.py` with **no instruction prefix**. The classifier (`checkpoints/compatibility_classifier.pt`, first layer 7×1024 → 256) is hard-wired to that space; the docs say changing the encoder means retraining. `Backend/EMBEDDINGS.md`'s `Instruct: …\nQuery:` prefixes and Matryoshka truncation must **not** be used.
- Rating keys: `outdoors, food, museums, live_music, nightlife, sports, shopping, big_crowds, early_mornings, long_walks` (1–5; 3 neutral). Pace: app sends `relaxed|balanced|packed`; accept `chill` too.
- Today Go fabricates random user vectors (`GenerateDeterministicEmbedding`) and returns 1024 floats on `GET /me`; both go away.
- Secrets: `HF_TOKEN`, `TYPESAFE_API_KEY` in the gitignored repo-root `.env` and `/opt/ml/.env`; `gcp-sa.json` at the repo root (gitignored) and `/opt/ml/gcp-sa.json`. `ml/deploy.sh` currently rsyncs everything and `chmod -R 755`s → fix (§10).
- Probes (2026-09-26): HF token lacks "Make calls to Inference Providers" (403); model mapping shows `deepinfra: live`, `hf-inference: error`. Vertex service account lacks `roles/aiplatform.user` (403). The VPS venv already has torch 2.14 + sentence-transformers 6.1 (Python 3.11); `ProtectHome=true` → `HF_HOME=/opt/ml/.cache`.
- Qwen3-Embedding ST config: `pooling_mode_lasttoken: true`, prompts `{"query": "Instruct: …", "document": ""}`, `default_prompt_name: null` → `encode(texts)` with no `prompt_name` = no prefix, matching training.

## 1. Architecture

```
 Go backend ─ PutPreferences / Signup / Facebook import ─► ml.UserProfile ─► POST /v1/user-profile
            ─ PutRating ─► ensureUserProfile + activity embed-on-demand (/v1/embed) ─► /v1/compatibility/user-embedding/update
            ─ GeneratePlans ─► POST /v1/search-profile ─► POST /v1/events/rank (real vectors + hard filters)
            ─ pkg/ml/client.go: Embed · UserProfile · SearchProfile · RankEvents · UpdateUserEmbedding · Health (cached 60 s)
 ML service (127.0.0.1:8000, uvicorn --workers 2)
   api/routes: /v1/events/rank  /v1/compatibility/user-embedding/update  /v1/embed  /v1/user-profile  /v1/search-profile  /healthz
   profiles/   eight-section text builders (deterministic templates)
   embedding/  CachingEmbedder(sqlite) → FallbackEmbedder(chain by EMBED_PROVIDER: vertex → hf(deepinfra → hf-inference) → local)
   compatibility/ classifier (unchanged)   reranking/ Jev (unchanged)
 Mongo users{positiveEmbedding, negativeEmbedding, positiveText, negativeText, embeddingModel, profileTextHash, profileUpdatedAt, facebookInterests}
       activities / demo_activities {embedding, embeddingModel, embeddingMeta{textHash…}}
 tools/embed_missing.py (systemd timer, 15 min) ─► /v1/embed ─► guarded $set on both catalogs
```

**Python owns the text format.** `POST /v1/user-profile` and `POST /v1/search-profile` take the app's
structured inputs and return text + embedding together, so the template version, the validator and the
encoder settings live in one place. `POST /v1/embed` remains for raw texts.

## 2. When embeddings are (re)computed

| Trigger | Go call | Stored |
|---|---|---|
| Signup | async `refreshUserProfile` (15 s ctx) | user profile fields |
| `PUT /me/preferences` | save prefs, then `refreshUserProfile` synchronously (15 s); on failure keep old vectors, clear `profileTextHash` | same |
| Facebook import | set `facebookInterests`, `refreshUserProfile` | same |
| Any rank call | `ensureUserProfile` when the vector is missing, `embeddingModel` ≠ healthz model, or the hash prefix ≠ template version | same |
| Plan request | `SearchProfile(req)` → `search_text` + `search_embedding` for that request only | nothing |
| `PUT /ratings/{itemId}` | activity embed-on-demand if missing (`/v1/embed`), then the existing update endpoint | activity vector; user vector |
| Activities missing vectors | `tools/embed_missing.py` timer | activity vector |

## 3. ML service: new and changed files

```
ml/
├── embedding/                      NEW package (pure; no FastAPI)
│   ├── __init__.py                 MODEL, DIM, Embedder, EmbedderError, build_embedder
│   ├── base.py                     Embedder ABC, EmbedderError, ProviderStatus, unit_rows(), embed_with_blanks()
│   ├── hf_router.py                HFRouterEmbedder (httpx; deepinfra + hf-inference routes; mapping lookup)
│   ├── vertex.py                   VertexEmbedder (google-auth REST predict; google libs imported lazily)
│   ├── local.py                    LocalEmbedder (sentence-transformers, lazy load, set_num_threads)
│   ├── cache.py                    SqliteEmbeddingCache, NullCache
│   ├── fallback.py                 FallbackEmbedder (circuit breaker), CachingEmbedder
│   ├── config.py                   EmbeddingSettings.from_env()
│   └── stats.py                    EmbedStats counters (thread-safe)
├── profiles/                       NEW package (pure)
│   ├── __init__.py                 TEMPLATE_VERSION = "profile-v1", build_profile_texts, build_search_text, profile_hash
│   ├── lexicon.py                  RATING_LIKES/DISLIKES, COMPANY, PACE, SPEND, TAGS, WHO, BUDGET, CATEGORY_PHRASES, ROUTES
│   ├── text.py                     Sections builder, phrase cleaning/routing, renderers
│   └── validate.py                 SECTIONS, check(text) (same rules as datagen/activity_text.check), text_hash
├── api/
│   ├── main.py                     CHANGED: load_embedder(), EmbeddingService, ProfileService, HealthState wiring
│   ├── app.py                      CHANGED: create_app(ranking_service, updater=None, *, embedding_service=None, profile_service=None, health=None); EmbeddingUnavailableError → 503
│   ├── deps.py / errors.py         CHANGED
│   ├── helpers/embedding.py        NEW: EmbeddingService (batch, blank→zero, dim check, stats)
│   ├── helpers/profile.py          NEW: ProfileService (build texts → embed → hash)
│   ├── helpers/health.py           NEW: HealthState (started_at, provider status, probe)
│   ├── schemas/{embedding,profile,health}.py   NEW
│   └── routes/{embedding,profile,health}.py    NEW
├── tools/                          NEW: embed_missing.py, hf_probe.py, vertex_probe.py, parity_check.py, make_golden.py, rank_smoke.py
├── tests/                          + fixtures/{golden_texts,qwen3_golden,profile_jordan,profile_seed_user,search_jordan}.json
│                                   + test_embedder.py, test_embed_api.py, test_profile_text.py, test_search_text.py,
│                                     test_profile_api.py, test_health.py, test_vertex.py, test_local_embedder.py (env-guarded), test_hf_parity.py (env-guarded)
├── requirements-serve.txt          NEW (slim)
├── ml.service                      CHANGED
├── ml-embed-missing.service/.timer NEW
├── deploy.sh                       CHANGED
└── README.md                       CHANGED
```

### 3.1 Embedder abstraction (`embedding/base.py`)

```python
MODEL = "Qwen/Qwen3-Embedding-0.6B"
DIM = 1024
EmbedKind = Literal["user", "search", "activity"]   # logging/metrics only; NEVER changes the text (no prefixes)

class EmbedderError(RuntimeError):
    def __init__(self, kind: Literal["auth", "rate_limit", "unavailable", "bad_request", "bad_response", "timeout", "not_configured"], message: str, *, retryable: bool, provider: str): ...

@dataclass
class ProviderStatus:
    name: str; status: Literal["ok", "error", "unknown", "not_loaded", "loading", "loaded", "disabled"]
    detail: str | None = None; last_ok_at: float | None = None; last_error_at: float | None = None; latency_ms: float | None = None

class Embedder(ABC):
    name: str; model: str; dim: int
    @abstractmethod
    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        """(N, dim) float32; every row unit-norm; blank texts -> zero rows. Raises EmbedderError."""
    def status(self) -> dict[str, ProviderStatus]: ...
    def warmup(self) -> None: ...

def unit_rows(m: np.ndarray) -> np.ndarray   # float64 normalize, zero rows stay zero, returns float32 (same as mongo_backfill.unit)
def embed_with_blanks(fn, texts) -> np.ndarray
```

### 3.2 `HFRouterEmbedder` (`embedding/hf_router.py`)

Routes (headers `Authorization: Bearer <HF_TOKEN>`):

| Route | URL | Body | Response parse |
|---|---|---|---|
| `deepinfra` | `{router_base}/deepinfra/v1/openai/embeddings` | `{"model": <providerId, default "Qwen/Qwen3-Embedding-0.6B">, "input": [texts], "encoding_format": "float"}` | `data` sorted by `index` → `[d["embedding"]]`; `usage.prompt_tokens` → stats |
| `hf-inference` | `{router_base}/hf-inference/models/{model}/pipeline/feature-extraction` | `{"inputs": [texts], "normalize": true, "truncate": true}` | list of lists (wrap a flat list when one text) |

Mapping lookup once per process (cached 10 min, never fatal): `HfApi(token).model_info(model,
expand=["inferenceProviderMapping"])` → `provider_id`, `status` per provider; order = configured priority,
`live` first, `error` last. Batches of `max_batch` (32); texts over `max_chars` (2000) truncated
client-side. Output `unit_rows()`-normalized and dim-checked. Errors: 401/403 → `auth` (route skipped for
`HF_AUTH_BACKOFF` 600 s); 429 → retry with `Retry-After`/backoff; 5xx/timeouts → retry; other 4xx →
`bad_request` (next route); bad shape/dim → `bad_response`. After all routes fail, raise.

### 3.3 `VertexEmbedder` (`embedding/vertex.py`)

Config: `VERTEX_PROJECT` (586468035526), `VERTEX_LOCATION` (us-central1), `VERTEX_ENDPOINT_ID`
(`mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852`), `VERTEX_HOST` (the dedicated endpoint DNS, else the
regional host), `VERTEX_INPUT_KEY` (`inputs` | `prompt` | `text`; from the probe), credentials via
`GOOGLE_APPLICATION_CREDENTIALS` (google-auth, scope cloud-platform). `google` imports happen inside the
provider's constructor/`embed()`; without config or libs it raises `not_configured`. REST
`POST https://{host}/v1/projects/{p}/locations/{l}/endpoints/{e}:predict` with `{"instances":[{key: text}]}`;
`_unwrap` handles `[floats]`, `[[floats]]` and `{"embedding": …}`. Enabled in `auto` only when a startup
probe returns a 1024-d vector. `tools/vertex_probe.py` (from `Backend/EMBEDDINGS.md` §3–4.3, already drafted
in this session) records DNS, key, dim and latency. **No instruction prefix.**

### 3.4 `LocalEmbedder` (`embedding/local.py`)

Lazy, lock-guarded load of `SentenceTransformer(MODEL)` with `max_seq_length = 512`, fp32 on CPU,
`torch.set_num_threads(threads or cpu_count()//2)`; `encode(..., normalize_embeddings=True)` → `unit_rows()`;
`warmup()` loads and embeds one text; status `not_loaded | loading | loaded | error`.

### 3.5 Cache, fallback, settings

`SqliteEmbeddingCache(path, max_entries=100_000)`: WAL, one connection per thread, key
`sha1(f"{model}\x00{text}")`, `get_many`/`put_many`; shared by both uvicorn workers; blanks never cached.

`FallbackEmbedder(chain: list[Embedder], open_after=3, reset_after=60, auth_reset_after=600)`: circuit
breaker per provider; on `EmbedderError` log WARNING with kind/provider, `stats.fallbacks += 1`, try the next.
`CachingEmbedder(inner, cache)`: blanks → zeros; cache hits; one inner call for the misses.

```python
@dataclass
class EmbeddingSettings:  # from_env()
    provider: Literal["auto", "vertex", "hf", "local"]; model; dim; hf_token; hf_routes; router_base; hf_timeout; hf_max_batch;
    hf_max_retries; local_fallback; local_threads; local_device; warmup; cache_path; cache_max_entries; max_chars; vertex_*
def build_embedder(s) -> Embedder:
    # auto   → chain [vertex (if configured), hf (if token), local]
    # vertex → Caching(Fallback([Vertex, Local if local_fallback]))
    # hf     → Caching(Fallback([HFRouter, Local if local_fallback]))
    # local  → Caching(Local)
```
Warmup (`EMBED_WARMUP=1`): a daemon thread embeds one canary through the chain at startup; startup never
blocks. Logging: one INFO line per embed call (`embed kind=user n=2 cached=1 provider=hf:deepinfra ms=312`),
WARNING on fallback, ERROR when everything fails. `EmbedStats` on `/healthz`.

## 4. API additions

```python
# schemas/embedding.py
class EmbedRequest(BaseModel):
    texts: Annotated[list[Annotated[str, Field(max_length=8000)]], Field(min_length=1, max_length=64)]
    kind: Literal["user", "search", "activity"] = "activity"
class EmbedResponse(BaseModel):
    embeddings: list[list[float]]; model: str; dim: int; provider: str; cached: int

# schemas/profile.py
RatingKey = Literal["outdoors","food","museums","live_music","nightlife","sports","shopping","big_crowds","early_mornings","long_walks"]
class ProfileAnswers(BaseModel): perfect_afternoon: str = ""; never_do: str = ""; plan_around: str = ""
class RatedEvent(BaseModel): stars: int (1..5); tags: list[str] = []; category: str | None = None; activity_tags: list[str] = []; embedding: list[float] | None = None
class UserProfileRequest(BaseModel):
    ratings: dict[str, int] = {}      # keys normalized + aliased; unknown keys ignored; values clamped 1..5
    company: Company | None; pace: Pace | None; spend: Spend | None; flexibility: Flexibility | None
    prefer_free: bool = False; answers: ProfileAnswers = ProfileAnswers()
    facebook_interests: list[str] = []; rated_events: list[RatedEvent] = []   # newest first, ≤ 20
    embed: bool = True
class UserProfileResponse(BaseModel):
    positive_text: str; negative_text: str
    positive_embedding: list[float] | None; negative_embedding: list[float] | None   # negative = zeros when negative_text == ""
    profile_text_hash: str            # "profile-v1:" + sha1(positive + "\n---\n" + negative)
    template_version: str; model: str; dim: int; provider: str
class SearchProfileRequest(BaseModel):
    mood_text: str = ""; tags: list[str] = []; who: Literal["just_me","friends","open"] | None = None
    pace: Pace | None = None; budget: int (0..3) | None = None
    start_time: AwareDatetime | None = None; back_by: AwareDatetime | None = None; timezone: str | None = None
    embed: bool = True
class SearchProfileResponse(BaseModel): search_text: str; search_embedding: list[float] | None; model: str; dim: int; provider: str

# schemas/health.py
class HealthResponse(BaseModel):
    status: Literal["ok","degraded"]; uptime_seconds: float
    ranking: dict        # {"model_version", "embedding_dim"}
    embedding: dict      # {"model","dim","provider","providers":{name:{status,detail,last_ok_at,latency_ms}},"cache":{...},"stats":{...}}
    profile_template_version: str; jev: bool
```
Routes: `POST /v1/embed` (503 `"Embedding provider unavailable."` on failure), `POST /v1/user-profile`
(an all-empty profile returns `positive_text == ""` and a zero positive vector; Go treats that as
"unranked"), `POST /v1/search-profile` (empty text → `search_embedding: None`), `GET /healthz`
(`?probe=1` embeds a canary bypassing the cache; 503 if it fails). `create_app(...)` keeps the existing
positional signature.

## 5. Profile and search text templates (`profiles/`)

Format (identical to training): sections in the fixed order `Interests, Activities, Social, Environment,
Pace, Cost, Timing, Experience`; each present section is `Name:` followed by `- bullet` lines; blank line
between sections; empty sections omitted; bullets lowercase, ≤ 8 words, deduplicated; no trailing newline.
Every output passes `profiles.validate.check` (the rules of `datagen/activity_text.check`). Caps: Interests
10, Activities 8, Social 4, Environment 4, Pace 3, Cost 3, Timing 3, Experience 3.

Rating rules: rating 5 (or 1) takes the first **two** bullets of each list; 4 (or 2) the **first** only; 3
contributes nothing. Lexicon (`lexicon.py`), abbreviated:

```python
RATING_LIKES = {
  "outdoors":       {"Interests": ["outdoor recreation"], "Activities": ["park visit", "guided hike"], "Environment": ["outdoor setting"]},
  "food":           {"Interests": ["local food"], "Activities": ["food tasting", "eating out"]},
  "museums":        {"Interests": ["arts and culture"], "Activities": ["museum visit", "gallery viewing"], "Environment": ["cultural venue"]},
  "live_music":     {"Interests": ["live music"], "Activities": ["live music performance"]},
  "nightlife":      {"Interests": ["nightlife"], "Activities": ["bar hopping"], "Environment": ["bar setting"], "Timing": ["late night"]},
  "sports":         {"Interests": ["sports"], "Activities": ["sports watch party", "pickup games"], "Pace": ["physically active"]},
  "shopping":       {"Interests": ["shopping"], "Activities": ["market browsing", "vintage shopping"]},
  "big_crowds":     {"Social": ["big lively crowds", "large-group setting"]},
  "early_mornings": {"Timing": ["early morning"], "Pace": ["early start"]},
  "long_walks":     {"Activities": ["long walks"], "Pace": ["extended walking"]},
}
RATING_DISLIKES = { … mirrored: e.g. "nightlife": {"Interests": ["nightlife"], "Environment": ["loud nightclub setting", "crowded bars"], "Timing": ["late night"]},
                    "big_crowds": {"Social": ["large crowds"], "Environment": ["crowded venue"]}, … }
COMPANY = {"solo": {"Social": ["solo attendance", "meeting new people"]}, "small_group": {"Social": ["small-group setting", "close friends"]}, "big_group": {"Social": ["large-group outing", "big lively crowds"]}}
PACE = {"relaxed"/"chill": {"Pace": ["relaxed rhythm", "low-key"]}, "balanced": {"Pace": ["moderate pace"]}, "packed": {"Pace": ["fast-paced", "back-to-back activities"]}}
SPEND = {"free_only": ["free admission"], "under_15": ["under $15 admission"], "15_to_40": ["moderate price point", "$15-40 admission"], "over_40": ["premium tickets"]}
PREFER_FREE = ["free admission"]; FLEXIBILITY_NEG = {"stick_to_budget": {"Cost": ["expensive tickets"]}}
RATING_TAGS_POS = {"great people": {"Social": ["meeting new people"]}}; RATING_TAGS_NEG = {"too crowded": {"Social": ["large crowds"]}, "too pricey": {"Cost": ["expensive tickets"]}}
CATEGORY_PHRASES = {"museum": "arts and culture", "art_gallery": "arts and culture", "park": "outdoor recreation", "trail": "hiking", "restaurant": "local food", "cafe": "coffee shops", "bar": "nightlife", "nightclub": "nightlife", "music_venue": "live music", "theater": "performing arts", "market": "markets and fairs", "landmark": "sightseeing"}
TAGS (search) = {"outdoors": {"Interests": ["outdoor recreation"], "Environment": ["outdoor setting"]}, "food": {"Interests": ["local food"], "Activities": ["eating out"]}, "art": {"Interests": ["arts and culture"], "Activities": ["gallery viewing"]}, "music": {"Interests": ["live music"]}, "chill": {"Pace": ["relaxed rhythm"], "Environment": ["laid-back atmosphere"]}, "active": {"Pace": ["physically active"], "Activities": ["active recreation"]}, "meet people": {"Social": ["meeting new people"], "Activities": ["social mixer"]}, "nerdy": {"Interests": ["science and technology", "games and puzzles"]}, "nightlife": {"Interests": ["nightlife"], "Timing": ["late night"]}}
WHO = {"just_me": {"Social": ["solo outing"]}, "friends": {"Social": ["small group of friends"]}, "open": {"Social": ["open group", "meeting new people"]}}
BUDGET = {0: ["free admission"], 1: ["under $15 admission"], 2: ["$15-40 admission"], 3: []}
```
Free text (`text.py`): split on `[.;!?\n]+|,|and|then|or`, strip leading fillers ("i'd", "something", "maybe",
…) and trailing ones ("after", "too", …), collapse spaces, drop < 3 chars / placeholders / URLs, truncate to 8
words, cap 8 phrases; route each phrase to a section by keyword regexes (Timing, Social, Environment, Pace,
Cost, Activities; default Interests).

`build_profile_texts(req)`: ratings ≥ 4 → likes (sorted by rating desc, key order), ≤ 2 → dislikes;
company/pace → positive; Cost = prefer_free + SPEND; `stick_to_budget` → negative Cost; answers
`perfect_afternoon` + `plan_around` → positive, `never_do` → negative; Facebook interests → positive
Interests; rated events (≤ 20) → category phrases positive (≥ 4) / negative (≤ 2) plus tag rules; caps;
render; `check()`; hash `"profile-v1:" + sha1(...)`.

`build_search_text(req)`: TAGS in request order (unknown → `Interests: [tag]`), WHO, PACE, BUDGET, Timing
from `start_time` in `timezone` (`"{weekday} {period}"`, `"weekend|weekday {period}"`, period = morning
<12 / afternoon 12–16 / evening 17–20 / night ≥ 21; with `back_by`: < 2 h "short outing", 2–5 h "multi-hour
outing", > 5 h "full-day outing"), then mood phrases. Empty inputs → `""`.

Golden fixtures (byte-exact tests): **A** `profile_jordan.json` = the contract's `Preferences` example +
`FacebookImport.interests` + two rated events; **B** `profile_seed_user.json`; **C** `search_jordan.json` =
the contract's `PlanRequest` (Friday 14:10 local, 4 h 20 min → "friday afternoon", "weekday afternoon",
"multi-hour outing"); **D** no-dislikes → `negative_text == ""`, zero negative vector. The exact expected
texts are in the ML design report of 2026-09-26 (reproduce from the lexicon; the test asserts the rendered
text, so write the fixture from the first correct run and review it by eye).

## 6. Go changes (`Backend/pkg/ml`, `pkg/models`, callers)

- `User`: `PositiveEmbedding`, `NegativeEmbedding`, `PositiveText`, `NegativeText`, `EmbeddingModel`,
  `ProfileTextHash`, `ProfileUpdatedAt`, `FacebookInterests` — all `json:"-"`. `Activity.Embedding`,
  `EmbeddingModel`, `EmbeddingMeta` `json:"-"`. `ItineraryItem.ActivityID` (set from the planner stop).
- `pkg/ml/client.go` (+ `rank.go`, `profile.go`): `NewClient(baseURL)` with per-call context deadlines;
  `Health` / `CachedHealth` (60 s); `Embed(ctx, texts, kind)`; `UserProfile(ctx, req)`; `SearchProfile(ctx,
  req)`; `RankEvents` (wire unchanged); `UpdateUserEmbedding`. `GenerateDeterministicEmbedding` and every
  caller deleted. `RankActivities(ctx, user, activities, search, constraints, limit) RankResult`: no user
  vector → DB order + `Reason="no_user_embedding"`; only activities with a 1024-d vector are sent (the rest
  are appended unscored and counted); constraints (`max_price`, lat/lng + `max_distance_miles`,
  `available_start/end`, `excluded_categories`) sent as a second check; `search_embedding`/`search_text` when
  present; `rerank` only if healthz says `jev: true` and `ML_RERANK != "false"`, `rerank_top_k` 12,
  deadlines 2 s / 20 s; dropped events are **not** re-appended; `model_version`, sent/returned/unembedded and
  latency logged.
- `BuildUserProfileRequest(u, rated)` maps `UserPrefs` (spend tier → enum, flexible → enum, pace passthrough,
  answers) + `FacebookInterests` + rated events (category from the item's `ActivityID`).
- Handlers: `PutPreferences` → save → `refreshUserProfile` (sync 15 s; on error clear the hash);
  `Signup` → async refresh; `PutRating` → resolve the activity → embed on demand if it has text but no vector
  (`SetActivityEmbedding` guarded on `embeddingTextHash`, same `embeddingMeta` shape, `source:
  "go_on_demand"`) → `ensureUserProfile` → update (kind = stars ≥ 3 ? positive : negative); `GeneratePlans` →
  `SearchProfile({MoodText, Tags, Who, Pace, Budget, StartTime, BackBy, Timezone})` (5 s, nil on error) →
  `ensureUserProfile` → rank with constraints from the window.
- Store: `ListRatingsByUser`, `ResolveActivityForItem`, `SetUserProfile` (`$set`), `SetUserVector`,
  `ClearProfileHash`, `SetActivityEmbedding`.
- Go env: `ML_SERVICE_URL`, `ML_RERANK` (`true`), `ML_RERANK_TOP_K` (12), `ML_RANK_TIMEOUT_MS` (2000),
  `ML_RANK_RERANK_TIMEOUT_MS` (20000), `ML_EMBED_TIMEOUT_MS` (15000), `ML_SEARCH_TIMEOUT_MS` (5000).

## 7. `tools/embed_missing.py`

`python -m tools.embed_missing --uri mongodb://127.0.0.1:27017/?directConnection=true --db freetime
--collections activities demo_activities [--ml-url http://127.0.0.1:8000] [--batch 32] [--limit N]
[--dry-run] [--no-stale]`. Selection copied from `mongo_backfill.needs_embedding` (missing vector, or
`embeddingMeta.textHash ≠ embeddingTextHash`); skips docs whose `sha1(embeddingText) != embeddingTextHash`;
dedupes by hash across collections; POST `/v1/embed` in batches (`kind: "activity"`); guarded `UpdateOne`
with `embedding`, `embeddingModel`, `embeddingMeta{model, dimension, normalized, prompt: null,
maxSeqLength: 512, generatedAt, textHash, source: "embed_missing", provider}`; `bulk_write` in batches of
250; per-collection counts; exit 1 if the service is unreachable. Systemd: `ml-embed-missing.service`
(oneshot) + `ml-embed-missing.timer` (`OnBootSec=2min`, `OnUnitActiveSec=15min`).

## 8. Env

| Variable | Default | Purpose |
|---|---|---|
| `EMBED_PROVIDER` | `auto` | `auto` / `vertex` / `hf` / `local` |
| `EMBED_MODEL` / `EMBED_DIM` | `Qwen/Qwen3-Embedding-0.6B` / `1024` | must equal the activities' `embeddingModel` |
| `HF_TOKEN` | – | fine-grained token with "Make calls to Inference Providers" |
| `HF_EMBED_ROUTES` / `HF_ROUTER_BASE` | `deepinfra,hf-inference` / `https://router.huggingface.co` | |
| `HF_EMBED_TIMEOUT` / `HF_EMBED_MAX_BATCH` / `HF_EMBED_MAX_RETRIES` / `HF_AUTH_BACKOFF` | `20` / `32` / `3` / `600` | |
| `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT`, `VERTEX_LOCATION`, `VERTEX_ENDPOINT_ID`, `VERTEX_HOST`, `VERTEX_INPUT_KEY` | – | Vertex provider |
| `EMBED_LOCAL_FALLBACK` / `EMBED_LOCAL_THREADS` / `EMBED_LOCAL_DEVICE` | `1` / `8` / `cpu` | |
| `EMBED_WARMUP` | `1` | background canary at startup |
| `EMBED_CACHE_PATH` / `EMBED_CACHE_MAX_ENTRIES` | `.cache/embeddings.sqlite` / `100000` | empty disables |
| `EMBED_MAX_CHARS` | `2000` | |
| `HF_HOME` | `/opt/ml/.cache` (service) | required with `ProtectHome=true` |
| `RERANK_TOP_K` | 20 (code) → 12 in `ml.service` | |
| existing `RANKING_*`, `SEARCH_WEIGHT`, `USER_EMBEDDING_ALPHA`, `TYPESAFE_API_KEY` | unchanged | |

## 9. Tests

Python (`python -m unittest discover`): `test_embedder.py` (httpx `MockTransport` fake router: deepinfra
shape reordered by index; hf-inference shape; normalization; batching 70 → 3 requests; 429 then 200 with
`Retry-After`; 403 → auth error → next route; all fail → error; fallback chain and circuit breaker; cache
hit → no HTTP; blanks → zero rows; wrong dim → `bad_response`; mapping failure → defaults),
`test_embed_api.py` (FakeEmbedder dim 8 injected; happy path; 422; 503; `cached`), `test_profile_text.py`
(goldens A/B/D; thresholds; alias/camelCase keys; caps; dedupe; cleaning table; `check()`; hash prefix),
`test_search_text.py` (golden C; empty; unknown tag; timezone/period/duration; no timing without
`start_time`), `test_profile_api.py`, `test_health.py`, `test_vertex.py` (importable without google;
`not_configured` without config; a fake HTTP transport happy path), `test_local_embedder.py`
(`ML_TEST_LOCAL_EMBEDDER=1`: dim 1024, unit norm, cosine ≥ 0.999 vs `qwen3_golden.json`),
`test_hf_parity.py` (`ML_TEST_HF=1` + token: cosine ≥ 0.995).

Go: `pkg/ml/client_test.go` (`Embed`, `UserProfile`, `SearchProfile`, `HealthCached`,
`RankActivitiesSendsStoredVectors`, `DoesNotReappendDropped`, `NoUserEmbedding`, `RerankFlagFromHealth`),
`pkg/ml/profile_test.go`, handler tests for preferences → profile refresh and ratings → embed on demand.

## 10. Deployment steps (VPS)

1. `requirements-serve.txt`: `fastapi uvicorn pydantic>=2.5 python-dotenv numpy httpx huggingface_hub>=0.30 torch sentence-transformers transformers typesafe-sdk pymongo google-auth requests` (no wandb/datasets); install with `--extra-index-url https://download.pytorch.org/whl/cpu`.
2. `deploy.sh`: upload serving packages and checkpoints with tar over SSH, preserving server secrets, caches and the venv; install `requirements-serve.txt`; prefetch weights in `/opt/ml/.cache`; install and restart the service and timer. Tests and health checks run separately. See [deployment](../DEPLOY.md).
3. `ml.service`: `Environment=HF_HOME=/opt/ml/.cache EMBED_PROVIDER=auto EMBED_CACHE_PATH=/opt/ml/.cache/embeddings.sqlite EMBED_LOCAL_THREADS=8 OMP_NUM_THREADS=8 HF_HUB_DISABLE_TELEMETRY=1 RERANK_TOP_K=12`, `TimeoutStartSec=300`, `ReadWritePaths=/opt/ml/.cache`, keep `--workers 2` and `EnvironmentFile=-/opt/ml/.env`.
4. Secrets: `/opt/ml/.env` (600) with `HF_TOKEN`, `TYPESAFE_API_KEY`, `GOOGLE_APPLICATION_CREDENTIALS=/opt/ml/gcp-sa.json`; copy `gcp-sa.json` (600).
5. `python -m tools.hf_probe`, `python -m tools.vertex_probe`, `python -m tools.parity_check` on the VPS; enable a remote provider only when parity passes.
6. Deploy Go; the first `ml-embed-missing` run fills the 5 missing vectors.

## 11. Verification

```bash
curl -s 127.0.0.1:8000/healthz | jq '{status, jev, ranking, embedding: {model, dim, provider, providers}, profile_template_version}'
curl -s '127.0.0.1:8000/healthz?probe=1' | jq '.embedding.providers'
curl -s 127.0.0.1:8000/v1/embed -H 'content-type: application/json' -d '{"texts":["Interests:\n- live jazz\n\nCost:\n- free admission",""],"kind":"user"}' \
  | python3 -c 'import json,sys,math; r=json.load(sys.stdin); print(r["model"], r["dim"], r["provider"], [round(math.sqrt(sum(x*x for x in v)),6) for v in r["embeddings"]])'
# expect: Qwen/Qwen3-Embedding-0.6B 1024 <provider> [1.0, 0.0]
curl -s 127.0.0.1:8000/v1/user-profile -H 'content-type: application/json' -d @tests/fixtures/profile_jordan.json | jq '{positive_text, negative_text, profile_text_hash, provider}'
python -m tools.parity_check --ml-url http://127.0.0.1:8000     # min cosine >= 0.995
mongosh freetime --eval 'db.users.findOne({email:"…"},{profileTextHash:1,embeddingModel:1,positiveEmbedding:{$slice:3}})'
curl -s https://api.sidequestz.tech/me -H "Authorization: Bearer $T" | grep -c positive_embedding   # expect 0
```

## 12. Risks

Token/IAM permissions until regenerated/granted (local fallback covers); provider parity (hard gate);
rate limits/cold starts (retries, cache); local fallback resources (2 workers × ~2.5 GB, first load 20–60 s;
warmup thread; `TimeoutStartSec=300`); two uvicorn workers → SQLite cache on disk; contract drift
(tolerant decoder); ratings on items without `ActivityID` (skipped with a log); Jev latency/cost (only when
healthz says so, top-k 12, kill switch `ML_RERANK=false`).

## 13. Task split

T1 `embedding/*` + tests · T2 `profiles/*` + goldens · T3 API wiring + README · T4 Go `pkg/ml` + models
· T5 Go handlers/store · T6 ops (`requirements-serve.txt`, `deploy.sh`, units, `tools/*`) · T7 VPS rollout.
T1/T2/T4/T6 in parallel → T3 → T5 → T7.
