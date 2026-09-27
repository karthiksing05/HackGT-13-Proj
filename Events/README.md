# SideQuestz Events (Ticketmaster Sandbox & Merchant Server)

A high-performance Go web server and API simulating a Ticketmaster-like event ticketing platform for Saltlight Harbor. Built for hackathon demonstrations with autonomous AI agents (Meta Muse Spark) and the Visa Agentic Checkout sandbox.

---

## Features

- **Ticketmaster-style Discovery UI**:
  - Event discovery at `/` and `/events` with category filtering, search, dynamic availability, and pricing.
  - Rich event details pages at `/{slug}` with Schema.org `Event` JSON-LD microdata.
  - Interactive ticket selection at `/{slug}/tickets` with real-time fee breakdown (8% + $0.50/ea) and `<link rel="agent-checkout">` tags.
  - Apple Wallet style digital ticket pass at `/t/{ticket_id}` with barcode graphics, confirmation code (`SL-XXXXX`), and Schema.org `EventReservation` JSON-LD.
  - Content negotiation: `Accept: application/json` on `/t/{ticket_id}` returns the raw ticket JSON object.
- **Agentic Checkout & Visa Trusted Agent Protocol (TAP)**:
  - RFC 9421 HTTP message signatures using Ed25519 (`sqz-agent-1`).
  - Signed quote offers at `GET /api/events/{slug}/offer` (`agent-browser-auth`).
  - Signed orders at `POST /api/orders` (`agent-payer-auth`) with mandatory `Idempotency-Key` and atomic inventory reservation.
  - Simulated Visa Network authorizer enforcing single-use agent tokens (`sbx_vtok_...`) and instruction budget limits.
- **Operator Booth Dashboard**:
  - Live order feed at `/dashboard` polling every 2 seconds via `/api/dashboard/feed`.
  - Displays agent key ID, token last 4, instruction limit comparison, and direct links to tickets.
  - Live log of the last 10 rejected requests (e.g. `401 bad_signature`) for the "unsigned curl" demonstration.
  - Interactive scenario controller: `normal`, `sold_out`, `price_bump` (+40% price surge at checkout), and `slow` (latency simulation).
- **Zero-Dependency Quickstart**:
  - Pre-seeded with all 25 ticketed events from Saltlight Harbor.
  - Seamless dual-store: connects to MongoDB if available, or automatically falls back to a thread-safe in-memory store.

---

## Quickstart

### 1. Run the Server

```bash
cd Events
go run main.go
```

The server starts on port `8085` (leaving port `8080` open for the main Backend API).

### 2. Endpoints

| URL | Type | Description |
|---|---|---|
| `http://localhost:8085/` | Web UI | Event discovery catalog & search |
| `http://localhost:8085/{slug}` | Web UI | Event details & JSON-LD |
| `http://localhost:8085/{slug}/tickets` | Web UI | Ticket tier selector & agent purchase demo |
| `http://localhost:8085/t/{ticket_id}` | Web / JSON | Digital ticket pass |
| `http://localhost:8085/dashboard` | Web UI | Live operator booth screen & scenario switcher |
| `GET /api/events/{slug}/offer` | API | Quote (requires TAP `agent-browser-auth`) |
| `POST /api/orders` | API | Order placement (requires TAP `agent-payer-auth`) |
| `GET /api/orders/{order_id}` | API | Order confirmation |
| `POST /_demo/scenario` | API | Scenario switcher (`normal`, `sold_out`, `price_bump`, `slow`) |
| `GET /sandbox/tap/keys/{keyid}` | API | Public key directory |
| `POST /sandbox/visa/authorize` | API | Simulated Visa sandbox authorization |
| `GET /healthz` | API | Health check |

## Running Tests

```bash
cd Events
go test -v ./...
```

---

## Build & Deployment

### Build Binary
```bash
./build.sh          # Defaults to linux/amd64 (matches deploy target)
./build.sh --native # Builds for host operating system
./build.sh --clean  # Cleans bin/ first
```

### Deploy to Production VPS
Deploys `events-server` to `/opt/events` and updates the `events.service` systemd service unit:
```bash
./deploy.sh [host] [user]
# Or configure DEPLOY_HOST, DEPLOY_USER, DEPLOY_PASSWORD in .env
```
