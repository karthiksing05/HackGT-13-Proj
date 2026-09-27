# Agentic checkout: findings and proposal

How real ticket purchasing should grow out of the simulated agent checkout we already have. This is
an exploration document: it records what Meta, Stripe and Ticketmaster actually offer as of
2026-09-26, where each piece fits in this repository, and the smallest change worth building now.
Nothing here is implemented yet.

**Summary.**

- We can't call Muse's checkout from our backend. There is no public API for it.
- Stripe's Link Agent Wallet is the one real, documented way for our own agent to get a card that
  the user approves per purchase and that is locked to one merchant and one amount.
- Ticketmaster doesn't allow automated purchasing unless you're a distribution partner, so a
  Ticketmaster event should hand the user off to Ticketmaster rather than use a browser agent.
- The existing `checkout` package already has the right shape: one intent per item, an approval
  step and a persisted state machine. We should extend it rather than build a parallel
  `CheckoutPlan`/`PurchaseTask` system.

## 1. What the codebase has today

| Concern | Where | Notes |
|---|---|---|
| Event catalog | `Backend/pkg/models/activity.go` (`Activity`, alias `Event`) | Has both `URL` and `TicketURL`. Ingestion sets `ticketUrl` from Ticketmaster's event URL (`dataingestion/ingest/adapters/ticketmaster.py:169`) and from Resident Advisor when the event is ticketed (`resident_advisor.py:192`). |
| Planner stop → saved item | `pkg/planner/route.go:313`, `pkg/planner/service.go:124`, `pkg/api/deps.go:23` (`StopDetail`), `pkg/api/itineraries/create.go:227` | **The ticket URL is dropped here.** The planner uses `TicketURL` to set `Bookable`, but `StopDetail` has no field for it, so the saved `ItineraryItem` only gets `WebsiteURL` and `Bookable`. |
| Itinerary item | `pkg/models/itinerary.go` (`ItineraryItem`), `pkg/contract/itinerary.go` | Has `PriceCents` (an estimate: the minimum of Ticketmaster's `priceRanges`), `Bookable` and `WebsiteURL`. Tickets are per viewer in `item_states.ticket` (`ItemTicket`). |
| "Finalization" | `POST /itineraries` (`pkg/api/itineraries/create.go`) | There is no separate finalize step: saving a plan option creates the itinerary. Checkout starts per item from the app ("Get tickets"). |
| Checkout orchestration | `pkg/api/checkout/` | `CheckoutIntent` in `checkout_intents`, one per (user, item). States: `preparing → awaiting_approval → processing → booked`, plus `failed` and `cancelled`. A background `Agent` (`agent.go:88`, started at `main.go:63`) moves due intents forward using guarded atomic updates. `approve` is documented as "the only call that spends". Each change is pushed to the app as a `checkout.status` realtime event. Everything is `// Simulated:`. |
| Instant checkout | `pkg/api/checkout/handlers.go:110`, `models.User.Prefs.InstantCheckout*` | Skips approval when the subtotal is within the user's limit. |
| Payment methods | `pkg/api/integrations/payments.go`, `models.PaymentMethod` | Simulated cards stored as brand, last 4 digits and a demo token. No card number is stored. The hosted `/pay/setup` page uses one-time `web_sessions` links. |
| Secrets at rest | `pkg/api/facebook/crypto.go` (`tokenBox`) | Encrypts third-party OAuth tokens per user. We can reuse it for Link tokens. |
| Existing Meta integration | `dataingestion/ingest/agent/muse.py` | Uses the Meta Model API (`api.meta.ai/v1/responses`, `muse-spark-1.3`, `web_search` tool) for research. This is a model API. It isn't the Muse consumer agent and exposes no shopping or checkout. |
| iOS client | `frontend/SideQuestz/Features/Home/CheckoutSession.swift`, `CheckoutSheet.swift`, `frontend/API_CONTRACT.md` § Agent checkout | The contract says **an unknown checkout state reads as `processing`**, so adding new states needs care (see §5). |

The existing `CheckoutIntent` already is the `PurchaseTask` from the brief. What it lacks:
merchant/provider identity, the checkout URL, a user-approved **maximum** that is separate from the
estimate, the final charged amount, external references (provider transaction and merchant order),
and a structured failure reason.

## 2. External findings

### Meta Muse

- Muse is a consumer agent (iOS, Android, muse.ai web; US only). It browses, fills forms and checks
  with the user before sensitive actions such as purchases. Meta says this check is enforced outside
  the model: a separate "Sentinel" agent approves internet-bound actions from a "Muse Secure VM". Muse
  keeps an audit trail of what it did and what it plans to do.
  ([Meta announcement](https://about.fb.com/news/2026/09/introducing-muse-personal-ai-agent/))
- Payment goes through Link. At merchants that accept Link it uses the saved method directly.
  Everywhere else Link issues a single-use virtual card scoped to the approved purchase. The user
  approves the total in chat, and Muse never sees the underlying card.
  ([Stripe newsroom](https://stripe.com/newsroom/news/stripe-helps-meta-muse-shop-with-link))
- **No developer surface for Muse's shopping or checkout could be found.** The announcement, the
  shopping page and the developer pages document no API, SDK or intent protocol for a third party to
  start a Muse purchase. The Meta Model API we already use is a separate product.

### Meta AI Connectors

- Connectors turn an existing API into tools that Meta AI can call. You register a REST API (an
  OpenAPI spec) or GraphQL through a guided UI, or you use MCP. Users link their account with OAuth.
  ([dev.meta.ai/products/connectors](https://dev.meta.ai/products/connectors),
  [wearables FAQ](https://developers.meta.com/wearables/faq/))
- Status: **developer preview with selective access**. It requires a live REST API and a defined use
  case, and "publishing and discovery" come in a later phase. The documented surfaces are AI glasses
  and Meta AI app/web. **Muse is not named as a surface for Connectors**, and nothing about
  payments or commerce is documented.

### Stripe

| Mechanism | What it is | Fits us? |
|---|---|---|
| **Link Agent Wallet** ([docs](https://docs.stripe.com/agentic-commerce/link-agent-wallet/use-link-wallet-pay-online)) | Our agent gets an OAuth token for **one consumer's** Link account (`login.link.com`, scope `payment_methods.agentic`). It creates a *spend request* with the amount, merchant name and URL, line items and at least 100 characters of context. The user approves in the Link app or web within 10 minutes. Link then returns a one-time card, a Shared Payment Token or a Link Pay Token. US and Canadian consumers. Agent payments need no Stripe account. There's a Go SDK (`stripe/link-cli/packages/sdk-go`). | **Yes, the best fit.** Approval happens in Link, outside our app and outside any LLM. Charges above the approved amount fail with `re_authorize`. Raising the amount needs the user to approve again (`spend-request update` + `request-approval`). |
| One-time card (a Link credential type) | The default for ordinary card forms. The card is single-use and has a `valid_until`. | Works for web checkouts. **Retrieving it returns the full card number, CVC, expiry and billing address to whoever calls the API.** Stripe warns hosted agents to write it to a `0600` file and never to stdout or logs. |
| **Shared Payment Token** ([docs](https://docs.stripe.com/agentic-commerce/concepts/shared-payment-tokens)) | Scoped by amount, currency, expiry and seller profile. The seller creates a PaymentIntent with it, and the agent never handles the card. The API is `...preview`-versioned under the Agentic Commerce preview terms. | Only if the **merchant** accepts SPTs (on Stripe, with ACP/UCP/MPP). None of our ticket sources do today. |
| Link Pay Token | Used on Stripe-hosted checkout pages that have Link enabled. The agent ticks a hidden "I am an AI agent" control and injects the token, which is valid for up to 30 minutes. | Works for small venues that sell through Stripe Checkout. It still needs a browser. |
| Issuing for agents ([docs](https://docs.stripe.com/issuing/agents)) | We would issue virtual cards ourselves, with MCC and amount controls and real-time authorization webhooks. Selling to consumers means "Cards for your platform", which requires contacting Stripe. | **No for now.** It makes us a card program (KYC and funding) and goes against "don't build our own vault". |

PayPal has also announced a Muse checkout partnership. It isn't investigated here because the brief
is Stripe-centred.

### Ticketmaster

- The **Discovery API** is public, and we already use it for search and event URLs.
- The **Partner API** (reserve, pay, commit, with tickets delivered in the Ticketmaster app) is
  restricted to companies with official distribution relationships. Access is arranged through
  Ticketmaster's Distributed Commerce team.
  ([Partner API](https://developer.ticketmaster.com/products-and-docs/apis/partner/),
  [FAQ](https://developer.ticketmaster.com/support/partner-api-faq/))
- The **consumer Terms of Use** (effective 2025-08-12,
  [legal.ticketmaster.com](https://legal.ticketmaster.com/terms-of-use/)) prohibit using automated
  software to search for, reserve or buy tickets. The BOTS Act (15 U.S.C. §45c) separately prohibits
  circumventing purchase-limit controls such as queues and CAPTCHAs. Ticketmaster also runs
  aggressive anti-bot measures.
- **Conclusion: a browser agent completing Ticketmaster checkout is not a reasonable option for us**,
  even with a scoped Link card and user approval. What's realistic today is a handoff to the
  Ticketmaster URL (optionally through their affiliate programme). A real integration needs Partner
  API approval. Whether Muse may do this when the user asks it directly is Meta's question, not ours.

## 3. Answers to the brief's questions

1. **Can we invoke Muse checkout directly?** No. There's no public API, and the Model API we use has no
   shopping tools.
2. **Role of Meta AI Connectors?** They reverse the direction. Meta AI calls *our* tools: search,
   plan, get itinerary, checkout status. This is distribution, not a way to pay, and it's in preview.
3. **Expose our app to Muse via MCP/API?** To Meta AI, yes once we have preview access. It needs an
   OpenAPI spec or MCP server **and an OAuth 2.0 authorization server**, which we don't have today
   (auth is our own JWT bearer tokens, `pkg/api/auth`). For Muse specifically, it's not documented.
4. **Access to Link's wallet for agents?** Each user connects their Link account through OAuth. There's
   no Stripe account requirement, but registering an OAuth client is recommended. US and Canada only.
   The docs show no waitlist, but we should confirm the terms for a hosted multi-user agent (§7).
5. **Can our agent request one-time credentials?** Yes, through spend requests. The user approves each
   one in Link.
6. **What our backend holds:** the Link OAuth access and refresh tokens, sealed with `tokenBox`. The
   spend request ID and status, provider, merchant name/host, checkout URL, event/item IDs, estimated
   amount, approved maximum, final amount, merchant order ID, confirmation code/URL, failure code and
   timestamps. Card brand and last 4 digits for display. We already store those two.
7. **What must never reach the backend:** card number, CVC, full expiry, billing address from the card
   response, Link Pay Tokens and SPT IDs after use, and the user's Link password. The one-time card
   should only exist inside the isolated checkout worker, in memory. It must never be written to
   Mongo or logs, and never enter an LLM prompt or transcript.
8. **Realistic with Ticketmaster:** discovery plus handoff now. Programmatic purchase only through
   the Partner API.
9. **Is Partner API access needed?** Yes, for any programmatic purchase.
10. **Could a browser agent do it instead?** Technically sometimes, but it breaches the Terms of Use
    and hits anti-bot controls. Not recommended.
11. **Where orchestration fits:** in `pkg/api/checkout`, which is already the orchestrator. We'd add
    a provider seam that `Agent.step` calls instead of a timer (§4).
12. **Now vs. later:** see §6.

## 4. Recommended architecture

```mermaid
sequenceDiagram
    participant App as iOS app
    participant API as Go API (pkg/api/checkout)
    participant Exec as Executor (per provider)
    participant Link as Link Agent Wallet
    participant M as Merchant

    App->>API: GET /itineraries/{id}/checkout (checkout plan)
    API-->>App: paid items, merchant, estimate, route (handoff / agent)
    App->>API: POST /checkout/intents {item_id, quantity}
    API->>Exec: Quote(intent)  (state: preparing)
    Exec-->>API: price + fees, or sold_out / needs_handoff
    API-->>App: awaiting_approval {estimate, max_authorized_cents}
    App->>API: POST /checkout/intents/{id}/approve
    API->>Link: create spend request (amount = max, merchant, context)
    Link-->>App: user approves in Link app (outside our app and the LLM)
    API->>Link: poll until approved
    API->>Exec: Execute(intent, spend request ID)  (state: processing)
    Exec->>Link: retrieve one-time card (worker memory only)
    Exec->>M: complete checkout
    Exec-->>API: order ID, final amount, confirmation (no card data)
    API-->>App: booked (checkout.status), ticket on item_states
```

**The provider seam.** It exists because two real providers do (handoff and Link), not for
abstraction's sake. The simulated timer becomes a third implementation:

```go
// pkg/api/checkout/provider.go (proposal)
type Route string // "simulated" | "handoff" | "link_card" | later: "partner_api", "spt"

type Executor interface {
    // Quote prices the purchase on the merchant (preparing → awaiting_approval).
    Quote(ctx context.Context, in *models.CheckoutIntent) (Quote, error)
    // Execute buys it with an approved credential (processing → booked | failed).
    Execute(ctx context.Context, in *models.CheckoutIntent) (Result, error)
}
```

`route(item)` picks the executor from the merchant (the ticket URL's host) and configuration:
`ticketmaster.com` goes to `handoff` until Partner API access exists; allow-listed merchants go
to `link_card`; `simulated` stays the default in dev and tests so the current test suite keeps
passing.

**Where the safety comes from.** In order of strength: (1) Link enforces the single-use card, the
merchant scope and the approved amount; overspend returns `re_authorize`. (2) Our API refuses to
execute without an approved spend request, and `max_authorized_cents` is fixed at approval. (3) The
executor checks the merchant's total against `max_authorized_cents` before it submits. (4) Only
after that do the LLM or browser instructions matter. **Instant checkout must not bypass step 1.**
With Link, every spend still needs approval in the Link app. Link lists "Granular agent controls" as
coming soon.

**Estimated vs. final price.** `PriceCents` is the catalog minimum. The quote step replaces it with
the merchant's actual subtotal and fees. The user approves a maximum (the quote plus a small fee
buffer, shown explicitly), and `final_cents` records what was charged. If the merchant total goes
over the maximum, the intent goes to `requires_user_action`, and approval starts again through
Link's update flow.

## 5. Data-model changes (following existing conventions)

`models.CheckoutIntent` gains the following fields (bson camelCase; wire snake_case in
`contract.CheckoutIntent`):

| Field | Purpose |
|---|---|
| `provider` (`simulated\|handoff\|link_card\|…`) | Which executor handled the intent |
| `merchant`, `checkoutUrl` | The merchant identity (host) and the URL the purchase starts from |
| `maxAuthorizedCents`, `finalCents` | The approved ceiling and the actual charge; the existing `subtotalCents`/`feesCents`/`totalCents` remain the quote |
| `failureCode` (`sold_out\|price_changed\|declined\|expired\|merchant_blocked\|denied`) | A machine-readable code; `failureReason` stays the human sentence |
| `externalRef`, `orderRef` | The Link spend request ID and the merchant order number |

**States.** Keep the six existing states and add only **`requires_user_action`** (used for handoff,
3-D Secure, a login wall or a price increase). Put sold-out and similar outcomes in
`failed` + `failureCode` rather than new states. Old app builds read unknown states as `processing`,
which would be wrong for a handoff, so `requires_user_action` must ship together with an app update
(or only for app versions that send a capability header).

`ItineraryItem` and `contract.ItineraryItem` gain `ticketUrl` / `ticket_url`, carried through
`StopDetail`. The **checkout plan** is computed on read (`GET /itineraries/{id}/checkout`), not
stored. It lists the itinerary's bookable items with each one's merchant, estimate, route and the
viewer's current intent. Tickets are already per viewer (`item_states`), so a group plan naturally
gets one intent per member per item. Partial success (A booked, B sold out, C booked) falls out of
per-item intents, and the planner's alternatives endpoint can act on `failureCode = sold_out`.

`models.PaymentMethod` (simulated cards) becomes irrelevant for Link-routed purchases, because the
card lives in Link. It should stay for the simulated route and be replaced by a "Link connected"
integration row, not extended.

## 6. What to build, and when

**Available now. Minimal build (no external dependencies, about 1–2 days):**

1. Carry `TicketURL` from the planner into `StopDetail` → `ItineraryItem.TicketURL` → the contract.
2. Add `provider`, `merchant`, `checkoutUrl`, `maxAuthorizedCents`, `finalCents`, `failureCode`,
   `externalRef` and `orderRef` to the intent, and add the `requires_user_action` state.
3. Introduce the `Executor` seam, with `simulated` (today's timer logic moved behind it) and
   `handoff`. For `handoff`, "approve" returns the checkout URL, the intent goes to
   `requires_user_action`, and a new `POST /checkout/intents/{id}/confirm-external`
   (`{order_ref?}`) lets the user mark it bought.
4. `GET /itineraries/{id}/checkout`: the checkout plan.
5. Update `docs/ROADMAP.md` and `frontend/API_CONTRACT.md`.

This delivers useful and honest behaviour today: Ticketmaster events hand off properly, and nothing
depends on preview APIs.

**Available now but needs setup, reviews and decisions (next step):**

- A `link_card` executor: Link OAuth connect (a hosted page like `/pay/setup`, with tokens sealed
  by `tokenBox`), spend requests through the Go SDK, and a **separate, isolated checkout worker**
  (a headless browser) that is the only process that ever retrieves the card. It should only target
  an allow-list of merchants whose terms permit agent purchases, for example venues on Stripe
  Checkout, where a Link Pay Token avoids handling a card at all.

**Requires preview or partner access:**

- Meta AI Connector (developer preview). It needs an OAuth 2.0 authorization server and an OpenAPI
  spec for a small tool set: `search_events`, `get_itinerary`, `get_checkout_plan`,
  `get_purchase_status`, and possibly `create_itinerary`. Any purchase call should create an intent
  and return the approval link, not spend.
- The Ticketmaster Partner API as a `partner_api` executor.
- SPT-based checkout with any merchant that adopts ACP/UCP (`spt` executor).

**Future possibility:**

- Handing a purchase to Muse itself, if Meta publishes an intent or handoff surface.
- Link "granular agent controls", which could let instant checkout run within user-set limits
  without approving each purchase.
- The Muse wallet reported to be in development.

## 7. Open questions

**Credentials, access and legal:**

- **Link terms for a hosted multi-user agent.** The docs describe one customer's token. We need to
  confirm that a service holding many users' tokens is permitted, and whether we need a registered
  OAuth client.
- **PCI scope of the checkout worker.** When it retrieves a one-time card number, it handles
  cardholder data. We should confirm with Stripe which SAQ or obligations apply. Using a third party
  doesn't remove PCI scope on its own.
- **Meta Connectors preview access**, whether Connectors will reach Muse, and Meta's policy on
  commerce actions through Connectors.
- **Ticketmaster Partner API and affiliate programme.** These need a business development
  conversation.
- **Legal review** of automated purchasing on any merchant before adding it to the `link_card`
  allow-list.

**Product decisions:**

- **Merchant mix.** Which share of bookable items are Ticketmaster, Resident Advisor or other?
  Suggested query: `db.activities.aggregate([{$match:{ticketUrl:{$ne:null}}},{$group:{_id:{$regexFind:{input:"$ticketUrl",regex:"^https?://[^/]+"}},n:{$sum:1}}}])`.
  If almost everything is Ticketmaster, handoff is the whole near-term story, and Link work should
  wait for merchants that allow it.
- **Double approval.** A Link purchase has an in-app review and then Link approval. Should our
  in-app "Approve" become "Review", with Link as the only approval?
- **Fee buffer.** How much headroom over the quote the user approves by default, and how it's shown.
- **Group purchases.** Does each member buy their own tickets (as the data model does today), or can
  the host buy for the group?
- **Instant checkout.** Should it stay simulated only until Link's granular controls exist?
