# SideQuestz Events Merchant — Implementation Summary

This document summarizes the full implementation of the **Events Merchant Server** (Ticketmaster Demo & Visa Sandbox), located in the `Events/` directory.

---

## 1. Executive Summary

- **Role**: Dedicated merchant server simulating an event ticketing platform (Ticketmaster-like) for Saltlight Harbor.
- **Port & Host**: Runs on port `8085` (`http://localhost:8085` in local development, `events.sidequestz.tech` in production), operating on an independent subdomain to guarantee zero port or routing conflicts with the primary backend (`:8080`).
- **Core Capabilities**:
  - Full modern web UI for fans: Event Discovery, Event Details, Ticket Selector with live fee calculation, and Digital Ticket Passes.
  - Autonomous Agentic Checkout: RFC 9421 HTTP Message Signatures (Visa Trusted Agent Protocol) for quotes and orders.
  - Simulated Visa Network: Authorization of single-use `sbx_vtok_...` tokens and cryptographic instruction budget limits.
  - Live Operator Booth Screen: Real-time order stream (2-second polling), scenario switcher (`normal`, `sold_out`, `price_bump`, `slow`), and live audit log of rejected requests.
  - Zero-Dependency Fallback: Dual-store architecture that connects to MongoDB when available or seamlessly falls back to a thread-safe in-memory store pre-seeded with all 25 Saltlight Harbor events.

---

## 2. Directory Layout & Key Components

```
Events/
├── main.go                       # Server entrypoint with graceful shutdown & zerolog logging
├── go.mod / go.sum               # Go 1.26 module definition with gorilla/mux, mongo-driver, crypto
├── build.sh                      # Production build script (cross-compiles static binary)
├── deploy.sh                     # Automated zero-downtime deployment script to VPS
├── events.service                # Hardened systemd service unit definition
├── .gitignore                    # Ignores build artifacts, binaries, and env files
├── .env.example                  # Environment configuration template (port 8085 default)
├── README.md                     # Technical overview & endpoint documentation
├── IMPLEMENTATION_SUMMARY.md     # This document
└── pkg/
    ├── config/                   # Environment loading & PAYMENTS_MODE=sandbox validation
    │   ├── config.go
    │   └── config_test.go
    ├── models/                   # Core data models for events, quotes, orders, and dashboard
    │   ├── event.go
    │   ├── quote.go
    │   ├── order.go
    │   └── dashboard.go
    ├── store/                    # Dual-mode persistence layer (MongoDB + In-Memory fallback)
    │   ├── store.go
    │   ├── memory.go
    │   ├── mongo.go
    │   ├── seed_data.go          # Embedded 25 Saltlight Harbor ticketed events
    │   └── store_test.go
    ├── tap/                      # Visa Trusted Agent Protocol (RFC 9421 HTTP Message Signatures)
    │   ├── tap.go
    │   ├── keys.go               # Key directory & default demo agent key (sqz-agent-1)
    │   └── tap_test.go
    ├── visa/                     # Simulated Visa network authorizer & budget tracker
    │   ├── authorizer.go
    │   └── authorizer_test.go
    ├── ui/                       # Server-rendered HTML templates loaded via embed.FS
    │   ├── templates.go          # View structs, template funcs & embed.FS loader
    │   ├── ui_test.go            # Template compilation & render tests
    │   └── templates/
    │       ├── base.css          # Design system, glassmorphism, brand tokens & badges
    │       ├── home.gohtml       # Discovery catalog with search & category filters
    │       ├── event.gohtml      # Event details & Schema.org Event JSON-LD
    │       ├── tickets.gohtml    # Ticket selector, fee calculation & agent purchase widget
    │       ├── ticket_pass.gohtml# Apple Wallet / Ticketmaster ticket pass with barcode
    │       └── dashboard.gohtml  # Live booth operator feed & scenario switcher
    ├── api/                      # HTTP handlers for web views, quotes, orders & live feed
    │   ├── deps.go
    │   ├── handlers.go
    │   └── api_test.go
    └── router/                   # gorilla/mux router & CORS middleware
        └── router.go
```

---

## 3. Web UI & GoHTML Templates

The user interface was designed with a dark, glassmorphic aesthetic ("Ticketmaster meets Apple Wallet") without relying on external CDNs or third-party assets:

1. **Discovery Catalog (`/` and `/events`)**:
   - Filter by categories: All, Live Music, Nightlife, Comedy, Tours, Sports, Workshops.
   - Search bar by keyword, title, venue, or description.
   - 25 event cards with curated artwork, venue info, pricing ("From $12.00"), and remaining inventory chips.
2. **Event Details (`/{slug}`)**:
   - Hero banner with venue metadata, date/time, description, and category badges.
   - Embedded Schema.org `Event` JSON-LD microdata for search engines and agent scrapers.
3. **Ticket Selector (`/{slug}/tickets`)**:
   - Quantity selector (1–10) with live price calculation.
   - Fee breakdown table implementing the contract rule: **8% + $0.50 per ticket**.
   - `<link rel="agent-checkout" href="/api/events/{slug}/offer">` and Schema.org `Offer` microdata in `<head>`.
   - **Interactive "Simulate Agent Purchase" widget**: Enables immediate testing of an agent checkout run directly in the browser.
4. **Digital Ticket Pass (`/t/{ticket_id}`)**:
   - Apple Wallet / Ticketmaster style pass with perforated notch edges.
   - Scalable SVG barcode, Crockford base32 confirmation code (`SL-XXXXX`), buyer name, and Visa sandbox token indicator (`Paid with Visa agent token •••• 1881`).
   - Content negotiation: sending `Accept: application/json` returns the raw ticket confirmation JSON object.
5. **Booth Dashboard (`/dashboard`)**:
   - Live order stream polling `/api/dashboard/feed` every 2 seconds.
   - Displays agent verification status (`Signed agent ✓`, `sqz-agent-1`), Visa token last 4, instruction limit comparison, and ticket links.
   - **Live Rejected Requests Audit**: Displays the last 10 rejected calls (e.g. `401 bad_signature`) for the live "unsigned curl" demonstration.
   - **Scenario Switcher**: Toggle between `normal`, `sold_out` (409), `price_bump` (+40% surge price triggering 409 / over budget), and `slow` (latency simulation).

---

## 4. Visa Trusted Agent Protocol (TAP) Implementation

- **Specification**: RFC 9421 HTTP Message Signatures using Ed25519, matching [visa/trusted-agent-protocol](https://github.com/visa/trusted-agent-protocol).
- **Headers Parsed & Validated**:
  - `Signature-Input: sig2=("@authority" "@path");created=...;expires=...;keyid="sqz-agent-1";alg="ed25519";nonce="...";tag="..."`
  - `Signature: sig2=:<base64>:`
- **Guarantees Enforced**:
  - Expiration: signature valid for at most 8 minutes from `created` (with 60s clock skew tolerance).
  - Replay prevention: nonces are recorded and checked for single-use.
  - Tag verification: routes enforce specific tags (`agent-browser-auth` for quote and order lookup, `agent-payer-auth` for order placement).
  - Authority check: matches `MERCHANT_HOST` (or request host in dev).
  - Signature verification: Ed25519 signature base verified against public keys registered in the Key Directory (`sqz-agent-1`).

---

## 5. Simulated Visa Network Authorizer

- **Endpoint**: `POST /sandbox/visa/authorize` (header `X-Sandbox-Network-Key: $SANDBOX_NETWORK_KEY`).
- **Validations**:
  - Token starts with `sbx_vtok_`.
  - Cryptogram starts with `sbx_cgm_`.
  - Merchant ID equals `sidequestz-events`.
  - Enforces single-use tokens (declines with `used` if re-presented).
  - Tracks cumulative instruction spend and declines with `over_limit` if total authorized amount exceeds the instruction's limit.
  - Declines return structured codes: `over_limit`, `wrong_merchant`, `expired`, `used`, `unknown_token`.
  - On approval, returns `200 {"result": "approved", "auth_id": "sbx_auth_..."}`.

---

## 6. API Reference (Port 8085)

| Method | Route | Signature Tag | Description |
|---|---|---|---|
| `GET` | `/` | none | Ticketmaster discovery home page |
| `GET` | `/events` | none | Alias for discovery page |
| `GET` | `/{slug}` | none | Event details page with JSON-LD `Event` |
| `GET` | `/{slug}/tickets` | optional | Ticket selector page with `Offer` and `<link rel="agent-checkout">` |
| `GET` | `/api/events/{slug}/offer?quantity=N` | `agent-browser-auth` | Price quote with fee breakdown and availability |
| `POST` | `/api/orders` | `agent-payer-auth` | Order placement with `Idempotency-Key` and Visa agent token |
| `GET` | `/api/orders/{order_id}` | `agent-browser-auth` | Order confirmation lookup |
| `GET` | `/t/{ticket_id}` | none | Ticket pass (HTML) or raw ticket JSON (`Accept: application/json`) |
| `GET` | `/dashboard` | `X-Demo-Key` | Live operator booth screen |
| `GET` | `/api/dashboard/feed` | `X-Demo-Key` | Polling feed endpoint for live orders and rejected requests |
| `POST` | `/_demo/scenario` | `X-Demo-Key` | Toggle booth scenario (`normal`, `sold_out`, `price_bump`, `slow`) |
| `POST` | `/_demo/sim-purchase` | none | Browser helper to simulate end-to-end agentic checkout |
| `GET` | `/sandbox/tap/keys/{keyid}` | none | Public key directory (`sqz-agent-1`) |
| `POST` | `/sandbox/visa/authorize` | `X-Sandbox-Network-Key` | Simulated Visa network authorization |
| `GET` | `/healthz` | none | Health check (`{"status": "ok", "sandbox": true}`) |

---

## 7. Build & Deployment Automation

- **[Events/build.sh](file:///c:/Users/Max/Documents/HackGT-13-Proj/Events/build.sh)**:
  - Compiles static stripped binary `bin/events-server`.
  - Defaults to `linux/amd64` (for remote VPS deployment) with `--native`, `--amd64`, `--arm64`, `--clean`, `--skip-tests` flags.
- **[Events/deploy.sh](file:///c:/Users/Max/Documents/HackGT-13-Proj/Events/deploy.sh)**:
  - Automates build and upload to `/opt/events` on the server.
  - Performs pre-flight `.env` verification and atomic swap (`events-server.new` → `events-server`) with rollback preservation (`events-server.prev`).
  - Reloads and restarts `events.service`, verifying status via `/healthz`.
- **[Events/events.service](file:///c:/Users/Max/Documents/HackGT-13-Proj/Events/events.service)**:
  - Systemd service unit configured for `/opt/events` under the unprivileged `sidequestz` user with security isolation.

---

## 8. Verification & Test Results

All test suites pass across all packages:
- `events/pkg/api`: PASS (7/7 tests) — contract responses, signature rejection, idempotent replay, concurrent orders on last seat, inventory release on decline, JSON-LD validity, and reserved slug checks.
- `events/pkg/config`: PASS (3/3 tests) — environment loading, sandbox enforcement, non-sandbox URL rejection.
- `events/pkg/store`: PASS (3/3 tests) — 25-event seeding, inventory decrement/increment, quote/order lookups.
- `events/pkg/tap`: PASS (5/5 tests) — sign/verify roundtrip, replay rejection, expiration, wrong tag, wrong authority.
- `events/pkg/ui`: PASS (1/1 tests) — all `.gohtml` templates compile and render properly.
- `events/pkg/visa`: PASS (1/1 tests) — approval, reuse decline, budget decline (`over_limit`), wrong merchant decline.
- **End-to-End Browser Session**: Automated browser testing confirmed event selection, simulated Visa checkout, ticket pass generation with SVG barcode, and real-time dashboard feed update.
