# SideQuestz Events: implementation summary

The sandbox ticket merchant for Saltlight Harbor, in `Events/` (its own Go module, port 8085,
`events.sidequestz.tech` in production). [README.md](README.md) has the endpoints, scenarios and
configuration; this page records how it is built and what changed from the first version.

## Layout

```
Events/
├── main.go                 entrypoint, graceful shutdown
├── build.sh, deploy.sh     linux/amd64 build; upload to /opt/events and restart events.service
├── events.service          systemd unit
└── pkg/
    ├── config/             env loading; sandbox-only, test Stripe keys only, real keys outside dev
    ├── models/             events, quotes, orders, dashboard feed, scenarios
    ├── store/              MongoDB store with an in-memory fallback, 25 seeded events
    ├── tap/                Trusted Agent Protocol (RFC 9421, Ed25519) sign/verify, key directory
    ├── payments/           Stripe SPT charger (raw HTTP, preview API), Fake for tests
    ├── ui/                 server-rendered templates (embed.FS)
    ├── api/                handlers: pages, offers, orders (orders.go), dashboard
    └── router/             gorilla/mux routes
```

## Order path

1. Verify the TAP signature (`agent-payer-auth`); the nonce is recorded only after the signature
   checks out.
2. Require `Idempotency-Key`; a retry of a recorded order returns it (200).
3. Validate the body: `payment.scheme` must be `stripe_spt` with an `spt_` token, and the
   quantity must equal the quote's.
4. Scenarios: `slow`, `sold_out`, `price_bump` (a repriced quote, applied once).
5. Check the expected total against the quote (409 `price_changed` with the current quote).
6. Reserve seats atomically, then charge: a PaymentIntent confirmed with the token, sent with the
   idempotency key `order-<Idempotency-Key>`. `overcharge` asks for 125% of the quote here.
7. On a decline, release the seats and answer 402 `declined` with the reason (`over_limit`,
   `expired`, `used`, `unknown_token`, `card_declined`, `requires_action`).
8. Record the order (payment summary: brand, last 4, PaymentIntent id, the token's limit; never
   the token) and answer 201.

## Revisions after the first version

- Payments moved from a simulated Visa network to Stripe test-mode Shared Payment Tokens. The Visa
  simulator never enforced a budget (no instruction was ever registered outside tests), so it was
  removed with `/sandbox/visa/authorize`.
- Removed `/_demo/sim-purchase` and the "Simulate Agent Purchase" widget: they created paid orders
  without a signature or a payment. Orders now only come from the SideQuestz agent.
- Removed the "Ticketmaster Partner" wording: this merchant has no relationship with Ticketmaster.
- The TAP key directory takes the agent's public key (`TAP_AGENT_PUBLIC_KEY`). The demo key, whose
  seed is in the repo, is trusted only in dev.
- The dashboard and its feed require the demo key, and the feed escapes everything it renders
  (rejected request paths come from anyone).
- `price_bump` no longer raises the quote itself, so a repriced order can go through.
- Fees are exactly 8% + $0.50 per ticket (the one-off adjustment for $12 × 2 is gone).
- Error bodies follow the shared contract: `code` is the machine code, `message` a sentence.
- Fixed leftover Go string concatenation in `dashboard.gohtml` that broke its live refresh.

## Tests

`go test ./...` covers contract responses and errors, signature rejection, idempotent replay,
concurrent orders on the last seat, seats released on decline, the overcharge decline, the one-time
price bump, payment and quantity validation, dashboard auth, JSON-LD on every page type, reserved
slugs, config guards, the Stripe client against a fake Stripe, and the Fake charger's limits.
