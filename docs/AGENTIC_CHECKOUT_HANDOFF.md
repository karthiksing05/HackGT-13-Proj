# Agentic checkout: status and handoff

The design is in [AGENTIC_CHECKOUT.md](AGENTIC_CHECKOUT.md). This page is what's done, what's left
and who does it. Branch: `wp/agentic-checkout`.

## Done

| Part | Where | Verified |
|---|---|---|
| Merchant on Stripe SPTs; fake agent, Visa sim and Ticketmaster wording removed; fee hack, double price bump, unchecked quantity, dashboard auth and XSS fixed; `overcharge` scenario | `Events/` | `cd Events && go test ./...` |
| Stripe smoke test for the SPT preview | `Backend/scripts/stripe-spt-smoke.sh` | not yet run against real test accounts |
| Config, ticket URL on saved items, TAP signer + `tap-keygen`, SPT issuer, test-card mapping, checkout runs (store, endpoints, realtime), Muse client, agent runner with fallback | `Backend/` | `go test ./...`, and the Mongo-backed tests with `-race` (`MONGO_TEST_URI`) |
| Catalog's 25 ticketed events on `events.sidequestz.tech` (generator, JSON, planner fixture and goldens) | `dataingestion/demo`, `Backend/pkg/planner/testdata` | slugs match `Events/pkg/store/seed_data.go` |
| Production catalog migration script | `Backend/scripts/migrate-ticket-host.sh` | tested on a local copy: 25 updated, idempotent; **not run on production** |
| iOS: models, API client (live + mock), `checkout.run`, "Let Muse get your tickets" sheet with Face ID, progress and results; Account toggle is "Agentic checkout" + default budget | `frontend/SideQuestz` | build + `SideQuestzTests` (7 new tests, incl. a mock run within budget); not yet checked by eye in the simulator |
| Docs | this page, AGENTIC_CHECKOUT.md, API_CONTRACT.md, ARCHITECTURE.md, DEPLOY.md, ROADMAP.md | |

## Left

1. **Stripe test accounts** (owner: payments). Create the agent account and the merchant ("SideQuestz
   Events") account, or one account that is both if Stripe allows issuing to its own profile. Put
   the keys in the root `.env` (`STRIPE_SECRET_KEY`, `STRIPE_MERCHANT_SECRET_KEY`,
   `STRIPE_SELLER_PROFILE`) and run `Backend/scripts/stripe-spt-smoke.sh`. Record the error codes it
   prints in AGENTIC_CHECKOUT.md › Merchant and tighten the mapping in
   `Events/pkg/payments/stripe.go`.
2. **Hosting** (owner: infra). DNS, nginx and the certificate for `events.sidequestz.tech`; deploy
   `Events/` (`Events/deploy.sh`, `events.service`) with the env in DEPLOY.md › Events merchant; add
   the agentic checkout env to `/opt/backend/.env`. Generate keys with
   `sidequestz-admin tap-keygen` (seed on the API, public key on Events).
3. **Production catalog.** `Backend/scripts/migrate-ticket-host.sh` (dry run), then `--apply`, once
   the merchant is live.
4. **End to end.** Locally first (AGENTIC_CHECKOUT.md › Local end to end), then against
   `https://events.sidequestz.tech`: a $60 run books; `price_bump` → `over_budget` or a rebuy;
   `overcharge` → `declined`; `sold_out` → `sold_out`; an unsigned `curl` to `/api/orders` → 401.
5. **iOS by eye.** Plan a Saltlight evening, get the prompt after saving, approve, watch progress,
   open a ticket. Offline: `-SQAPIMode mock -SQRoute home/muse/itin-fri`.
6. **Contract examples.** Regenerate `docs/api/examples/` so `CheckoutPlan` and `CheckoutRun`
   examples exist ([api/README.md](api/README.md)).

## Not in scope

"Find a swap" for a stop Muse couldn't buy (the results sheet links to the event page instead), refunds,
and third-party merchants. See ROADMAP.md.
