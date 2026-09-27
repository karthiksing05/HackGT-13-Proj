# Agentic checkout

After a plan is saved, Muse buys the tickets for its paid stops within one budget the user approves
with Face ID, and hands back confirmations and tickets. Everything runs in a sandbox we own: the
merchant is our own service (`Events/`, `events.sidequestz.tech`), and payment is **Stripe test mode**
Shared Payment Tokens (SPTs). No real money moves; live Stripe keys are refused on both sides.

API shapes are in [frontend/API_CONTRACT.md](../frontend/API_CONTRACT.md) › Agentic checkout; the
component map and sequence are in [ARCHITECTURE.md](ARCHITECTURE.md); environment variables are in
[DEPLOY.md](DEPLOY.md).

## Why this design

- **Our own merchant.** Buying from Ticketmaster or similar with a browser agent breaks their terms.
  Hosting the merchant ourselves makes the whole flow real (signed requests, quotes, orders, tickets)
  without touching anyone else's site, and lets the demo show failures on purpose (sold out, price
  change, overcharge).
- **Stripe SPTs instead of Visa Intelligent Commerce.** VIC sandbox access is gated. Stripe's agentic
  commerce preview gives us, in test mode, exactly the primitive we need: a token the agent issues
  for one seller, capped at one amount and a short expiry, which the seller charges as a normal
  PaymentIntent. Stripe enforces the cap, so even a misbehaving merchant can't overcharge.
- **Visa TAP signatures stay.** Every request from our agent to the merchant is signed (RFC 9421,
  Ed25519), so the merchant can tell an authorised agent from a scraper. The same package lives in
  `Backend/pkg/tap` and `Events/pkg/tap`, pinned identical by `vector_test.go`.
- **The model drives, code decides.** Muse (`muse-spark-1.3` on the Meta Model API) chooses the steps
  through function tools, but every tool validates its arguments against the run, and payment happens
  entirely in code. Muse never sees a token, card or budget reservation, and the user-facing summary
  is built from results, not from the model's claims (in testing it claimed a purchase it never made).

## Flow

1. **Plan.** `GET /itineraries/{id}/checkout` lists the saved stops whose `ticket_url` is on
   `MERCHANT_HOST`, with an estimate and a suggested budget (estimate + 15%, rounded up to dollars, at
   least the user's default budget).
2. **Approve.** The app shows "Let Muse get your tickets": stops, quantities, budget chips, the card.
   Face ID (or the passcode), then `POST /itineraries/{id}/checkout-runs`. That creates a
   `checkout_runs` document and one `CheckoutIntent` per item. At most one run per plan is `running`
   (unique partial index).
3. **Run.** The runner claims the run under a 90 s lease (renewed while working; a restart resumes
   `running` runs). Muse gets the items and budget as data and works through the tools:
   - `open_page(url)`: signed `agent-browser-auth` GET, only on `MERCHANT_HOST`; returns the title,
     the page text (capped at 8 KB and labelled `untrusted_page_text`) and JSON-LD.
   - `get_offer(item_id)`: the quote at the quantity the user approved.
   - `buy_tickets(item_id, quote_id)`:
     1. Reserve the quote total in Mongo with one conditional update
        (`reserved + spent + total ≤ budget`), or fail the item `over_budget`.
     2. Issue an SPT: `max_amount` = the quote total, 10-minute expiry, scoped to
        `STRIPE_SELLER_PROFILE`, idempotency key `spt-{intent}-{quote}`.
     3. `POST /api/orders`, signed `agent-payer-auth`, `Idempotency-Key: {intent}:{quote}`, with the
        buyer's real name and email.
     4. 201: commit the spend, save the ticket on the item, mark it `booked`. 409/402: release the
        reservation, revoke the SPT, set `failure_code`. A `price_changed` answer carries the new
        quote, and Muse may buy once more if it still fits.
   - `skip_item(item_id, reason)` and `finish(summary)`.
4. **Fallback.** If Muse is unavailable, errors or runs out of turns (`AGENT_MAX_TURNS`), the runner
   finishes the same tools in itinerary order (`agent: "fallback"`). Items still pending at the end
   fail `agent_error`.
5. **Results.** `checkout.status` per step and `checkout.run` per item and at the end; the app also
   polls `GET /checkout/runs/{id}` every 2 s. The results sheet shows the summary, each confirmation
   with **Open ticket**, and why anything wasn't bought.

## Merchant (`Events/`)

- The website ("Saltlight Tickets") is an ordinary ticket site with no mention of agents: listings
  at `/`, event pages at `/{slug}` and checkout at `/{slug}/tickets` (schema.org JSON-LD on each).
  People pay on Stripe Checkout (test mode); `/orders/complete` issues their ticket once the
  session is paid. Muse reads the same pages but buys through the signed API below.
- `GET /api/events/{slug}/offer?quantity=n`: a quote (`unit × qty`, fees `8% + $0.50` per ticket),
  valid for a few minutes.
- `POST /api/orders`: verifies the TAP signature (the nonce is recorded only after it verifies),
  replays by `Idempotency-Key`, re-prices the quote, reserves inventory, charges the SPT as a
  PaymentIntent (`payment_method_data[shared_payment_granted_token]`, `confirm=true`), and releases
  the inventory if the charge is declined. Errors are `{code, message, decline_reason, total_cents,
  quote_id}`.
- Demo scenarios per event: `sold_out`, `price_bump` (409 with a new quote), and `overcharge` (the
  merchant tries to charge 125% of the quote; Stripe refuses it and the order is `402 declined /
  over_limit`: the payment layer protects the user even from the merchant).
- `POST /_demo/scenario` (`X-Demo-Key`) switches the scenario.

### Stripe in test mode (verified with `Backend/scripts/stripe-spt-smoke.sh`)

- **Two Stripe accounts.** Stripe refuses a token issued to your own profile ("the network_id … is
  the same as the counterparty network_id"), so the agent (Backend, `STRIPE_SECRET_KEY`) and the
  merchant (Events, `STRIPE_MERCHANT_SECRET_KEY` or its own `STRIPE_SECRET_KEY`) are separate
  sandboxes. Two sandboxes under one login work.
- **The seller profile is the merchant's test-mode one** (`profile_test_…`); a live `profile_…` fails
  with "The livemode of the API request and the network_id … do not match". The smoke script prints it
  when `STRIPE_SELLER_PROFILE` is empty; the Backend refuses a live one at startup.
- **No `return_url`** on issued tokens: Stripe answers `parameter_unknown`, although its docs example
  has one.
- `expires_at` must be in the future (up to 90 days). Brand and last four come from the
  PaymentIntent (`expand[]=payment_method`); the granted token doesn't carry them.

Stripe's errors here have **no `code`**, only a message, so `Events/pkg/payments/stripe.go` maps:

| Case | Stripe's answer | `decline_reason` |
|---|---|---|
| Charge above the token's limit | `invalid_request_error`: "The requested amount is greater than the remaining amount capturable with this shared payment granted token." | `over_limit` |
| Token already spent or revoked | "…cannot be used because it is already in a deactivated state." (granted token `deactivated_reason: consumed`) | `used` |
| Token expired | the same message as spent; the granted token (read before charging) says `deactivated_reason: expired` | `expired` |
| No such token | 404 / `resource_missing` | `unknown_token` |
| Card declined | `card_error` or 402 | `card_declined` |
| Needs 3-D Secure | PaymentIntent `requires_action` | `requires_action` |

## Test cards

The hosted card page's demo cards map to Stripe test PaymentMethods by last four
(`Backend/pkg/payments`): 4242 → `pm_card_visa`, 4444 → `pm_card_mastercard`, 5556 →
`pm_card_visa_debit`, 0002 → `pm_card_chargeDeclined` (always declines, for the failure demo),
0005 → `pm_card_amex`, 1117 → `pm_card_discover`; anything else falls back by brand, then Visa.

## Catalog

The Saltlight demo catalog's 25 ticketed events use `https://events.sidequestz.tech/{slug}` and
`…/{slug}/tickets` (`MERCHANT_BASE` in `dataingestion/demo/generate.py`); the merchant's seed
(`Events/pkg/store/seed_data.go`) has the same 25 slugs. Places without tickets stay on
`saltlight.example`. Production's `freetime.demo_activities` is migrated with
`Backend/scripts/migrate-ticket-host.sh` (dry run by default; `--apply` writes; idempotent).

## Safety properties

- Budget: enforced in code across the run (atomic reservation) and by Stripe per purchase (token cap).
- Scope: Muse can only open pages on `MERCHANT_HOST`, only buy listed items at the approved quantity,
  and only with a quote the merchant issued.
- Secrets: SPT ids, card data and keys never enter model input, transcripts or logs. Transcripts keep
  the last 200 events per run.
- Prompt injection: page text is passed as labelled untrusted data, and no tool does anything the run
  didn't already authorise, so an injected instruction can at worst waste turns.
- Sandbox: `PAYMENTS_MODE=sandbox` is required and only `sk_test_` / `rk_test_` keys are accepted.

## Local end to end

```bash
# Events (terminal 1)
cd Events && APP_ENV=dev MERCHANT_BASE_URL=http://localhost:8085 STRIPE_MERCHANT_SECRET_KEY=sk_test_…(merchant sandbox) go run .
# Backend (terminal 2): same TAP demo key in dev, requests go to the local merchant
cd Backend && APP_ENV=dev MERCHANT_HOST=events.sidequestz.tech MERCHANT_BASE_URL=http://localhost:8085 \
  PAYMENTS_MODE=sandbox STRIPE_SECRET_KEY=sk_test_…(agent sandbox) STRIPE_SELLER_PROFILE=profile_test_…(merchant) go run .
```

Then as the demo user: save a Saltlight plan with paid stops, approve a run, and watch the orders arrive in the
Stripe test dashboard. The iOS mock shows the whole flow offline:
`-SQAPIMode mock -SQRoute home/muse/itin-fri`.
