# Roadmap

What is simulated today, what a real integration needs, and the larger pieces that were scoped out of
the hackathon build. Nothing here pretends to work: every simulated feature is labelled `// Simulated:`
in the code, behaves honestly (no fake success), and is listed below.

## Simulated pieces

| Feature | Today | A real version needs |
|---|---|---|
| Agent checkout (`POST /checkout/intents`, approve, cancel) | a persisted state machine ticking every 1.5 s: preparing → awaiting approval → processing → booked, with a fake ticket and confirmation code; the instant path uses the user's limit; nothing is bought | a merchant integration per ticket source (or a browser agent with a purchase-intent protocol), a payment service provider for the charge, refund and dispute handling, real ticket delivery and a receipt page |
| Hosted card page (`/pay/setup`) and payment methods | a server-rendered page with a "Demo card" button; test numbers `4242…`, `5454…`, `1881…` add a card record; no PAN is ever stored | a PSP's hosted elements or Apple Pay, tokenization on the PSP, PCI scope kept off our servers, webhook-driven card status |
| Calendar connect (`/integrations/{google\|outlook}/connect`) | the "OAuth" page marks the provider connected and reads nothing; `GET /calendar/days` returns only the user's own items, never busy blocks | Google OAuth with the Calendar API free/busy scope and Microsoft Graph, token refresh, a free/busy sync into the planner's window, the "plans around it" busy blocks the design shows |
| Password reset delivery | the 6-digit code is logged at INFO and returned in the response when `DEV_RESET_CODES=1` | a transactional email provider (with a verified sending domain) and rate limits per address |
| Push notifications | `POST /me/devices` stores APNs tokens; nothing is sent | the Push capability on a paid Apple team, an APNs provider in Go, and choosing which realtime events also go to push when the app is closed |
| `transit.delay` realtime event | a typed emitter exists; nothing produces it | live transit data (MARTA GTFS-realtime for Atlanta) and a job that watches saved itineraries |
| Facebook connector | real Graph API v26.0 (login, likes, friends, deauthorize and data-deletion callbacks), but the Meta app is in development mode | see Facebook App Review below |

## Crawler deployment

The catalog is a snapshot: the Atlanta `activities` were ingested and backfilled on 2026-09-26 and the
Saltlight `demo_activities` are generated. `python -m ingest crawl` (hourly events, weekly places, with
research and write-ups for new activities) runs on a laptop today. Deploying it means a host with the
keys (Ticketmaster, Google Places with billing, Muse and Gemini), a systemd timer or `crawl` as a
service, the quota counter's caps for the paid APIs, and the `ml-embed-missing` timer picking up the new
texts. Until then the demo catalog needs its events regenerated when they pass ([DEMO.md](DEMO.md)).

## Routing provider

Travel times come from straight-line distance and fixed speeds (`Backend/pkg/travel`). The interface
(`travel.Provider.Legs`) is the seam: plug in the Google Routes API matrix (the ingestion spec's
`travel_matrix` with its cache and quota) or a self-hosted OSRM/Valhalla, keep the heuristic as the
fallback, and add real transit itineraries (MARTA GTFS) so "MARTA · 14 min" means a train.

## Retraining on real data

The classifier was trained on synthetic users and LLM-judged labels ([ml/models.md](../ml/models.md)).
The app now logs what it needs for real labels: `plan_runs` keeps every shortlist with its scores and
`plan_runs.outcome` records which option and stops were saved, and ratings carry stars and tags per
stop. Next steps: export (user profile, shown candidate, chosen or rated) triples, retrain with the
recipe in [ml/training.md](../ml/training.md), tune `SEARCH_WEIGHT` and the planner's blend on real
searches, and cover the under-represented `Social` section. The encoder and text format stay fixed
unless everything is re-embedded.

## Facebook App Review

The Meta app is in development mode, so only people added as testers can connect. Going live needs App
Review for `user_likes`, `user_location` and `user_friends`, business verification, a privacy policy
URL, and the deauthorize and data-deletion callbacks (built: `POST /integrations/facebook/deauthorize`,
`POST /integrations/facebook/data-deletion`, `GET /integrations/facebook/deletion-status`). The redirect
URI `https://api.sidequestz.tech/integrations/facebook/callback` must be whitelisted.

## Host approval for joins

Joining an open plan auto-accepts. The contract already has a `requested` status, the host-side
`join.request` event and the `join_requests` collection; adding approval means a host setting on the
itinerary, `POST /itineraries/{id}/join-requests/{reqId}/approve|decline`, the pending state in the
Forum card ("Requested · waiting on host") and the `join.update` event on decision.

## Embedding providers: Vertex and Hugging Face

Both remote providers are implemented and gated; both were blocked by permissions on 2026-09-26 and the
local model serves. To turn one on:

1. Hugging Face: issue a fine-grained token with "Make calls to Inference Providers", put it in
   `/opt/ml/.env`, run `python -m tools.hf_probe`.
2. Vertex: grant the service account `roles/aiplatform.user`, confirm the dedicated endpoint DNS and the
   instance key with `python -m tools.vertex_probe`, set `VERTEX_HOST` and `VERTEX_INPUT_KEY`.
3. Run `python -m tools.parity_check` (cosine ≥ 0.995 against the local goldens), then set
   `EMBED_PROVIDER` or leave `auto` and restart `ml`. Undeploy the Vertex model when idle: it bills per
   node-hour.

Details in [EMBEDDINGS.md](EMBEDDINGS.md).

## Smaller items

- `lock_at` and `max_group_size` on `PATCH /itineraries/{id}` (the app does not send them yet).
- Group photos and invite links are built (P2 in the plan) but lightly exercised; the invite URL
  `https://sidequests.app/invite/<code>` needs universal links to open the app.
- More cities are a `dataingestion/cities/<slug>.yaml` file plus a run; the planner's city table
  (`atlanta, seattle, sf, nyc, berlin, saltlight`) grows with them.
- Candidate retrieval is a Mongo filter plus in-process cosine; at a larger catalog, a vector index
  (Atlas Vector Search or a local ANN) replaces phase B.
- The Jev rerank is asynchronous and only feeds alternatives and logs; using it for the first page needs
  a faster judge or a cached one.
- The mood parser is rule-based; an LLM pass could extract richer hard constraints (times, "near the
  water") but must keep the deterministic, logged behaviour.
