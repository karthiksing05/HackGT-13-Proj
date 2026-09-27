# SideQuestz Events (sandbox ticket merchant)

The ticket seller for the fictional city of Saltlight Harbor, served at `events.sidequestz.tech`.
People can browse it; tickets are bought by the SideQuestz checkout agent. Every agent request is
signed with Visa's Trusted Agent Protocol (TAP), and every order is paid with a Stripe test-mode
Shared Payment Token (SPT) the agent issued for that one purchase. Nothing real is sold or charged.

## Features

- **Storefront**: discovery at `/`, event pages at `/{slug}` (JSON-LD `Event`), ticket pages at
  `/{slug}/tickets` (JSON-LD `Offer` and `<link rel="agent-checkout">`), and the ticket pass at
  `/t/{ticket_id}` (JSON-LD `EventReservation`; `Accept: application/json` returns the ticket).
- **Agent API**: signed quotes (`agent-browser-auth`) and signed orders (`agent-payer-auth`) with a
  required `Idempotency-Key`, atomic seat reservation, and seats released when a charge fails.
- **Payments**: the order's `payment` is `{"scheme":"stripe_spt","token":"spt_…"}`. The merchant
  confirms a Stripe PaymentIntent with the token; Stripe enforces the token's `max_amount`, expiry
  and single use. The token is never stored or echoed back.
- **Booth dashboard** (`/dashboard?key=<DEMO_KEY>`): live orders with the signing key, card brand
  and last 4, the token's limit against the amount charged, recent rejected requests, and the
  scenario switch.
- **Scenarios**: `normal`; `sold_out` (409); `price_bump` (+40% between quote and order, once: 409
  `price_changed` with a new quote); `overcharge` (the merchant asks for 25% more than it quoted,
  and Stripe declines: 402 `declined/over_limit`); `slow` (2.5 s delay).
- **Store**: MongoDB when reachable, otherwise an in-memory store with the 25 ticketed events.

## Running

```bash
cd Events
go run .
```

It listens on `:8085`. See `.env.example`: `STRIPE_SECRET_KEY` must be a test key, and outside
`APP_ENV=dev` both `TAP_AGENT_PUBLIC_KEY` and a non-default `DEMO_KEY` are required.

## Endpoints

| Route | Auth | Purpose |
|---|---|---|
| `GET /`, `/events` | none | Discovery |
| `GET /{slug}` | none | Event page |
| `GET /{slug}/tickets` | none | Ticket page |
| `GET /api/events/{slug}/offer?quantity=N` | TAP `agent-browser-auth` | Quote |
| `POST /api/orders` | TAP `agent-payer-auth` | Order, paid with an SPT |
| `GET /api/orders/{order_id}` | TAP `agent-browser-auth` | Order confirmation |
| `GET /t/{ticket_id}` | none (unguessable id) | Ticket pass |
| `GET /dashboard`, `GET /api/dashboard/feed`, `POST /_demo/scenario` | demo key | Booth screen |
| `GET /sandbox/tap/keys/{keyid}` | none | The agent public keys this merchant trusts |
| `GET /healthz` | none | Health |

Errors are `{"code": "...", "message": "...", "decline_reason"?, "total_cents"?, "quote_id"?}` with
codes `bad_signature`, `missing_idempotency`, `idempotency_conflict`, `bad_request`,
`unsupported_payment`, `quantity_mismatch`, `not_found`, `sold_out`, `quote_expired`,
`price_changed`, `declined` and `payments_unavailable`.

## Tests, build, deploy

```bash
go test ./...
./build.sh            # linux/amd64 binary in bin/
./deploy.sh [host]    # to /opt/events, restarts events.service
```
