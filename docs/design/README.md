# Design notes (2026-09-26 integration)

These four documents are the detailed designs behind the integration plan. Implementation agents work
from them; the shorter, user-facing docs (`docs/*.md`) are written from them once the code lands.

| Doc | Owns |
|---|---|
| [backend-contract.md](backend-contract.md) | The rebuilt app-facing Go layer: contract structs, stores, every endpoint, realtime, tests, seed tooling |
| [planner.md](planner.md) | `pkg/planner`: guaranteed pre-filters, cosine shortlist, ML scoring, the iterative DAG loop, pools, alternatives, route, save |
| [embeddings.md](embeddings.md) | The ML service: embedding providers (Vertex / HF router / local), profile and search texts, new endpoints, the Go client, the embed-missing job |
| [app-docs-deploy.md](app-docs-deploy.md) | iOS changes (live defaults, loading policy, planner extras, home base, demo link), the docs set, the deployment runbook |

## Decisions that reconcile the four documents

- **Embedding kinds.** `POST /v1/embed` takes `kind: user | search | activity`; the kind is for logging and
  metrics only. **No instruction prefix is ever added** (the classifier and the stored activity vectors were
  produced without one). The planner gets the per-request search vector from `POST /v1/search-profile`, not
  from a hand-built template.
- **Providers.** `EMBED_PROVIDER=auto|vertex|hf|local`, tried in that order in `auto`. The Vertex provider is
  a real REST client (google-auth, `gcp-sa.json`), enabled only after `ml/tools/vertex_probe.py` confirms a
  1024-d vector; the HF provider needs a token with "Make calls to Inference Providers"; local Qwen on the
  VPS is the safety net and works today. A remote provider is switched on only after
  `ml/tools/parity_check.py` reports cosine ≥ 0.995 against local goldens.
- **Field names shared by the workstreams.** `users.catalog`, `users.city`, `users.homeBase` (`home_base` on
  the wire), `users.positiveEmbedding` / `negativeEmbedding` / `positiveText` / `negativeText` /
  `embeddingModel` / `profileTextHash`; `ItineraryItem.activityId`; `plan_pools` / `plan_runs`.
- **Backend env names.** `HTTP_ADDR` (default `127.0.0.1:8080`, `PORT` accepted), `PUBLIC_BASE_URL`,
  `MONGO_URI`, `MONGO_DB`, `JWT_SECRET` (required), `ML_SERVICE_URL`, `ML_*` timeouts, `PLANNER`,
  `PLANNER_*`, `FB_*`, `DEMO_*`, `DEV_RESET_CODES`, `TRUST_PROXY`. Embedding provider settings live on the
  ML service only (`EMBED_*`, `HF_TOKEN`, `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_*`).
- **Priorities.** P0: merge, backend foundation, auth/me/preferences with real embeddings, the planner
  (retrieval + loop + save), itineraries/calendar/past/ratings, the demo seed, the app on the live server,
  deploy. P1: forum, threads, groups, friends, search, insights, checkout, realtime. P2: Facebook backend,
  photos, invites, docs polish.
- **Simulated on purpose (documented in `docs/ROADMAP.md`):** the checkout agent and card page, calendar
  connect, password-reset delivery, push.
