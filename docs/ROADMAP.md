# Roadmap

What is simulated today, what a real integration needs, and the larger pieces that were scoped out of
the hackathon build. Nothing here pretends to work: every simulated feature is labelled `// Simulated:`
in the code, behaves honestly (no fake success), and is listed below.

## Simulated pieces

| Feature | Today | A real version needs |
|---|---|---|
| Agent checkout (`POST /checkout/intents`, approve, cancel) | a state machine persisted in Mongo (so a restart picks up where it left off): preparing → awaiting approval → processing → booked, one step every `CHECKOUT_STEP_DELAY` (1.5 s), checked every 500 ms. Booked writes a demo ticket with a confirmation code and a `GET /tickets/{id}` page onto the buyer's stop, and everyone on a shared sidequest sees it. The instant path uses the user's limit. Nothing is found, filled in or paid | a merchant integration per ticket source (or a browser agent with a purchase-intent protocol), a payment service provider for the charge, refund and dispute handling, real ticket delivery and a receipt page |
| Agentic checkout (`/itineraries/{id}/checkout-runs`) | real end to end, but in a sandbox: Muse drives our own tools against our own merchant (`Events/`, `events.sidequestz.tech`, 25 seeded Saltlight events), requests are TAP-signed, and each purchase is a real Stripe **test mode** Shared Payment Token capped at its quote, charged by the merchant as a test PaymentIntent. No real money moves; live keys are refused on both sides | third-party merchants that accept agent payment tokens (Stripe ACP / Visa Intelligent Commerce), live Stripe keys and the SPT preview going GA, a card saved through Stripe instead of the test PaymentMethod mapping, refunds, and a real ticket delivery channel |
| Hosted card page (`/pay/setup`) and payment methods | a server-rendered page: "Use a demo card" adds Visa 4242, then Mastercard 4444, then Visa debit 5556; or a Stripe test number (4242 4242 4242 4242, 5555 5555 5555 4444, 4000 0566 5566 5556, and 4000 0000 0000 0002, which always declines). Each maps to a Stripe test PaymentMethod (`pm_card_visa`, …) for agentic checkout. Only the brand and last four are saved; a typed number is never stored, logged or echoed. At most 10 cards per account | a PSP's hosted elements or Apple Pay, tokenization on the PSP, PCI scope kept off our servers, webhook-driven card status |
| Calendar connect (`/integrations/{google\|outlook}/connect`) | the page marks the calendar connected without signing in to it, says so, and reads nothing. Busy blocks come only from `calendar_events`, which the showcase seed fills for a few accounts; `GET /calendar/days`, the planner and saved plans work around them | Google OAuth with the Calendar API free/busy scope and Microsoft Graph, token refresh, a free/busy sync into `calendar_events` |
| Password reset delivery | the 6-digit code is written to the server log (and returned by `/auth/password/forgot` only with `DEV_RESET_CODES=1`) | a transactional email provider (with a verified sending domain) and rate limits per address |
| Push notifications | `POST /me/devices` stores APNs tokens; nothing is sent | the Push capability on a paid Apple team, an APNs provider in Go, and choosing which realtime events also go to push when the app is closed |
| `transit.delay` realtime event | a typed emitter exists; nothing produces it | live transit data (MARTA GTFS-realtime for Atlanta) and a job that watches saved itineraries |
| Facebook connector | real Graph API v26.0 (login, likes, friends, deauthorize and data-deletion callbacks), but the Meta app is in Development mode | see Facebook below |

## Facebook: App Review and a security follow-up

The Meta app is in Development mode, so only people added as testers can connect (the dashboard
settings are in [DEPLOY.md](DEPLOY.md#facebook-meta-dashboard)). Going live needs App Review for
`user_likes`, `user_location` and `user_friends`, business verification, a privacy policy URL, and the
deauthorize and data-deletion callbacks (built: `POST /integrations/facebook/deauthorize`,
`POST /integrations/facebook/data-deletion`, `GET /integrations/facebook/deletion-status`).

**Bind the OAuth `state` to the browser.** Today the `state` in the Login URL is a single-use, 10-minute
token tied to the account that asked for the URL, and the callback trusts it alone. Someone could start a
Login from their own account and get another person to finish it, linking that person's Facebook to the
wrong SideQuests account. The fix: send the app to a first hop on the API that sets a short-lived cookie
bound to the `state` and then redirects to Facebook, and have the callback require the cookie.

## Crawler deployment

The catalog is a snapshot: the Atlanta `activities` were ingested and backfilled on 2026-09-26 and the
Saltlight `demo_activities` are generated. `python -m ingest crawl` (hourly events, weekly places, with
research and write-ups for new activities) runs on a laptop today. Deploying it means a host with the
keys (Ticketmaster, Google Places with billing, Muse and Gemini), a systemd timer or `crawl` as a
service, the quota counter's caps for the paid APIs, and the `ml-embed-missing` timer picking up the new
texts. Food places are left out of the Google Places type groups today, so the Atlanta catalog has no
restaurants or cafés even though the planner can schedule them; adding a food group is part of this
step. Until then the demo catalog needs its events regenerated when they pass ([DEMO.md](DEMO.md)).

## Routing provider

Travel times come from straight-line distance and fixed speeds (`Backend/pkg/travel`). The interface
(`travel.Provider.Legs`) is the seam: plug in the Google Routes API matrix (the ingestion spec's
`travel_matrix` with its cache and quota) or a self-hosted OSRM/Valhalla, keep the heuristic as the
fallback, and add real transit itineraries (MARTA GTFS) so "MARTA · 14 min" means a train.

## Planner

- **A stop's value should grow with the visit.** Transit and anywhere days still cover 8–12 miles
  because a short stop far away scores like a long one nearby ([PLANNER.md](PLANNER.md#measured-on-the-live-server)).
- **Meal times** are not modelled: a restaurant can land at 3 PM.
- **Mood parsing** is rule-based; an LLM pass could extract richer hard constraints (times, "near the
  water") but must keep the deterministic, logged behaviour.
- **The Jev rerank** is asynchronous and only feeds alternatives and logs; using it for the first page
  needs a faster judge or a cached one.
- **Recording the outcome.** `plan_runs` has an `outcome` field for what was saved, but the save path
  (`POST /itineraries`) does not write it yet; saved itineraries carry `runId` and `optionId` instead.
- **Retrieval at scale.** Candidates come from a Mongo filter plus in-process cosine; at a larger
  catalog, a vector index (Atlas Vector Search or a local ANN) replaces phase B.

## Retraining on real data

The classifier was trained on synthetic users and LLM-judged labels ([ml/models.md](../ml/models.md)).
The app now keeps what real labels need: `plan_runs` stores every shortlist with its scores, saved
itineraries point back to their run and option, and ratings carry stars and tags per stop. Next steps:
export (user profile, shown candidate, chosen or rated) triples, retrain with the recipe in
[ml/training.md](../ml/training.md), tune `SEARCH_WEIGHT` and the planner's blend on real searches, and
cover the under-represented `Social` section. The encoder and text format stay fixed unless everything
is re-embedded.

## Host approval for joins

Joining an open plan auto-accepts. The contract already has a `requested` status, the host-side
`join.request` event and the `join_requests` collection; adding approval means a host setting on the
itinerary, `POST /itineraries/{id}/join-requests/{reqId}/approve|decline`, the pending state in the
Forum card ("Requested · waiting on host") and the `join.update` event on decision.

## Embedding providers: Vertex and Hugging Face

Both remote providers are implemented and gated, and both were refused on 2026-09-26 (the HF token as
invalid, 401; the Vertex service account without permission, 403), so the local model serves. To turn
one on:

1. Hugging Face: issue a new fine-grained token with "Make calls to Inference Providers" and run
   `python -m tools.hf_probe` with it.
2. Vertex: grant the service account Vertex AI User (`roles/aiplatform.user`), then run
   `python -m tools.vertex_probe`, which reports the dedicated endpoint DNS and the instance key to set
   as `VERTEX_HOST` and `VERTEX_INPUT_KEY`.
3. Run `python -m tools.parity_check --provider hf` (or `vertex`): every cosine must be ≥ 0.995 against the
   local goldens. Only then put the credentials into `/opt/ml/.env` and restart `ml` (`auto` uses a
   remote provider as soon as its credentials work). Undeploy the Vertex model when idle: it bills per
   node-hour.

Details in [EMBEDDINGS.md](EMBEDDINGS.md).

## Smaller items

- `lock_at` and `max_group_size` on `PATCH /itineraries/{id}` (the app does not send them yet).
- Group photos and invite links are built (P2 in the plan) but lightly exercised; the invite URL
  `https://sidequests.app/invite/<code>` needs universal links to open the app.
- There is no website: `sidequestz.tech` has no DNS record. A landing page and the privacy policy
  URL that Facebook's App Review asks for would live there.
- More cities are a `dataingestion/cities/<slug>.yaml` file plus a run; the planner's city table
  (`atlanta, seattle, sf, nyc, berlin, saltlight`) grows with them.
