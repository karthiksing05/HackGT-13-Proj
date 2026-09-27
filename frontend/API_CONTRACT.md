# SideQuests iOS ↔ backend contract

The iOS app is a GUI shell over this API. Every screen talks to one Swift protocol,
[`APIClient`](SideQuestz/Services/APIClient.swift). There are two implementations:

| | File | Used when |
|---|---|---|
| **Live** | [`Services/LiveAPIClient.swift`](SideQuestz/Services/LiveAPIClient.swift) | `SQAPIMode = live` (the default): REST + JSON calls to the server, exactly as listed below |
| **Mock** | [`Services/Mock/`](SideQuestz/Services/Mock/) | `SQAPIMode = mock` (`-SQAPIMode mock` at launch): an in-memory stand-in with the demo data, so the app runs offline |

The app contains no recommendation, AI or ranking logic. Plan generation, transit timing,
split math, age filtering, join limits and the taste profile all come from the server. The
app validates forms and previews an equal split (`SplitMath`), but it always shows the server's
result. The mock client imitates those behaviors only so the GUI works before the backend exists.
Treat it as a reference for the expected behavior, not as logic to keep.

## Pointing the app at your server

The app ships pointed at the live API. Set these keys in [`SideQuestz/Info.plist`](SideQuestz/Info.plist),
or override them at launch (Xcode ▸ Product ▸ Scheme ▸ Edit Scheme ▸ Run ▸ Arguments):

| Key | Default | Launch-argument override |
|---|---|---|
| `SQAPIMode` | `live` | `-SQAPIMode mock` (the offline demo data; no server needed) |
| `SQAPIBaseURL` | `https://api.sidequestz.tech` | `-SQAPIBaseURL http://127.0.0.1:8080` |
| `SQWebSocketURL` | `wss://api.sidequestz.tech/ws` | `-SQWebSocketURL ws://127.0.0.1:8080/ws` |
| `SQDemoPassword` | `$(SQ_DEMO_PASSWORD)`: the demo account's password, supplied per build (`xcodebuild … SQ_DEMO_PASSWORD='…'`); empty = no "Use the demo account" link | `-SQDemoPassword …` |

Other launch arguments: `-SQSlowLoadingAfter 2` (seconds a first load shows its skeleton before the
logo takes over; `0` = at once, a large number = never), `-SQMockLatency 0` (no artificial delay in
mock mode), `-SQMockFail forum,plans` (those demo endpoint groups fail, to see error states), and
`-SQRoute create/4/swap` (opens a screen at launch; these deep links need `-SQAPIMode mock`, since
they sign in to the demo account).

`127.0.0.1` reaches your Mac from the Simulator. On a real iPhone, use the Mac's LAN IP or a deployed
HTTPS URL. Plain `http://` is allowed only for local networks (`NSAllowsLocalNetworking`).

## Conventions

- **JSON keys are `snake_case`.** Enum values are `snake_case` too (`"not_free"`, `"just_me"`, `"free_now"`).
  Unknown enum values from the server fall back safely (e.g. an unknown checkout state reads as `processing`).
- **Dates are ISO 8601 with a time zone** (`"2026-09-25T18:10:00Z"`; fractional seconds are fine). Day-only
  fields may also be sent as `"2026-09-25"`.
- **Time zone.** Every request carries `X-Time-Zone: <IANA id>` (e.g. `America/New_York`). Use it for anything
  day-based: calendar days, "today", plan dates, "Free until 6:30 PM".
- **Money is integer cents** (`amount_cents: 4000`). Unknown real prices are `null`. The app then shows
  placeholders like `$[price]` and never invents a price.
- **Auth.** `Authorization: Bearer <access_token>` on everything except `/auth/*`. On a `401` the app calls
  `POST /auth/refresh` once (concurrent 401s share that one refresh) and retries. The refresh response is
  `{access_token, refresh_token?, expires_at?}`; if it has no new `refresh_token` the app keeps the old one.
  If the refresh fails, the app signs out and tells the user their session expired. Tokens live in the Keychain.
- **Errors.** Any non-2xx status. Put a user-facing sentence in `{"message": "…"}` (or FastAPI's `{"detail": "…"}`).
  `400`/`422` messages are shown to the user as-is. `409` means "what you saw is stale" (e.g. settling a balance
  that changed); send a sentence saying so.
- **Lists are paginated with cursors.** Return `{"items": […], "next_cursor": "…"}` (or a bare array for a list
  that's complete). The app sends `?cursor=<next_cursor>` and follows the chain until `next_cursor` is null,
  so balances, counts and filters always see everything. Page size is up to the server.
- **Leniency.** Optional fields may be omitted, and keys the app doesn't know are ignored. Defaults are in
  [`Models/Decoding.swift`](SideQuestz/Models/Decoding.swift), which also still reads the planner's older
  spellings (`name` / `duration_min` / bare `lat`, `lng` on a stop, `title` / `summary` on an option,
  `recalculated_legs`, `arrive_time` / `depart_time` stop windows). The Swift models in
  [`Models/`](SideQuestz/Models/) are the source of truth for field names.
- `ContractTests` checks that every model round-trips through these settings, and dumps the examples at the end
  of this file (`TEST_RUNNER_SQ_DUMP_CONTRACT=<dir>` when running the unit tests). The same payloads are
  published in [`docs/api/examples/`](../docs/api/examples/), where the server's contract tests read them
  ([how](../docs/api/README.md)).

## Endpoints

`APIClient` method → HTTP call. Types are the Swift models; JSON examples are at the end.

### Auth
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `signup` | `POST /auth/signup` | `SignupRequest` (`name, email, password, username?, date_of_birth?`; the birth date drives age filtering) | `AuthResponse` |
| `login` | `POST /auth/login` | `{email, password}` | `AuthResponse` (401 → "That email and password don't match."). If `user.setup_complete` is false the app resumes Profile setup |
| `refresh` | `POST /auth/refresh` | `{refresh_token}` | `{access_token, refresh_token?, expires_at?}` |
| `logout` | `POST /auth/logout` | `{refresh_token}` (revoke that session) | 2xx |
| `forgotPassword` | `POST /auth/password/forgot` | `{email}` | always 2xx (never reveal whether the email exists) |
| `verifyResetCode` | `POST /auth/password/verify` | `{email, code}` | `{reset_token}` (400/401 → invalid code). Codes are 6 digits and expire after 10 minutes (the app says so) |
| `resetPassword` | `POST /auth/password/reset` | `{reset_token, new_password}` | 2xx; sign out other sessions |
| `resendResetCode` | `POST /auth/password/resend` | `{email}` | 2xx |

### Me, integrations, payments
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `me` / `updateMe` | `GET` / `PATCH /me` | `UserPatch` (`name, username, date_of_birth, status`: `open` = open to all \| `friends_only` \| `busy`; email and password can't change here) | `User` (includes `id, avatar_color, school, setup_complete`, and when the account has them `home_base: Place` and `city`). The home base is where a new plan starts and where the Forum looks first; Account shows it, and the handle line shows `school`, or `city` without one. The app can't change either yet. A demo account's `User` (here, in `PATCH /me` and in the login/sign-up `AuthResponse`) also has `demo_date: "YYYY-MM-DD"`: the server runs that account on that day at the real time of day in New York, and the app's clock does the same ("today", the calendar, Create's default date). Leave it out for everyone else |
| `uploadPhoto` | `POST /me/photo` | multipart, field `photo`, `image/jpeg` | `{url}` |
| `deletePhoto` | `DELETE /me/photo` | | 2xx |
| `setAvatarColor` | `PATCH /me/avatar` | `{color}` (`ink`/`sage`/`clay`/`forest`/`sand`) | 2xx |
| `preferences` / `savePreferences` | `GET` / `PUT /me/preferences` | `Preferences` (rating keys 1–5: `outdoors, food, museums, live_music, nightlife, sports, shopping, big_crowds, early_mornings, long_walks`; answer keys: `perfect_afternoon, never_do, plan_around`; `instant_checkout`, `instant_checkout_limit_cents`) | `Preferences` / 2xx. The first `PUT` finishes Profile setup: set `setup_complete: true` on the user |
| `tasteProfile` | `GET /me/taste-profile` | | `TasteProfile` (bars 0…1) |
| `registerDevice` / `unregisterDevice` | `POST /me/devices` / `DELETE /me/devices/{push_token}` | `{push_token, platform: "ios"}` | 2xx (push isn't wired in the app yet: it needs the Push capability on a paid team) |
| `integrations` | `GET /integrations` | | `[Integration]`: calendars only (`google`, `outlook`). Facebook has its own endpoints below; don't list it here (older app builds would fail to read the list) |
| `connectIntegration` | `POST /integrations/{google\|outlook}/connect` | | `{url}`. The OAuth page opens in `ASWebAuthenticationSession` and must redirect to `sidequestz://…` when done |
| `completeIntegration` | nothing | | The server finishes OAuth in its own callback; the app just reloads `integrations` |
| `disconnectIntegration` | `DELETE /integrations/{provider}` | | 2xx |
| `paymentMethods` | `GET /me/payment-methods` | | `[PaymentMethod]` |
| `paymentSetupURL` | `POST /me/payment-methods/setup` | | `{url}`: a hosted card page (card numbers never touch the app). It opens in `ASWebAuthenticationSession` and redirects to `sidequestz://payments/done`; the app then reloads `paymentMethods` |
| `addPaymentMethod` | `POST /me/payment-methods` | `{token}` from a payment SDK | `PaymentMethod` (only if you later add an SDK; the app uses the hosted page) |
| `deletePaymentMethod` | `DELETE /me/payment-methods/{id}` | | 2xx |

### Facebook (Graph API): taste context for preferences
The person connects Facebook from Setup (step 2, "Fill in your likes") or later from Account › Connected.
Your server does everything that touches Facebook: the app never sees the app secret or the Facebook token,
and it never posts. The flow:

1. `POST /integrations/facebook/connect` returns Facebook's Login dialog URL. The app opens it in
   `ASWebAuthenticationSession`, never an embedded web view, since Facebook blocks those.
2. Facebook redirects to your callback with a `code`. Your server exchanges it for a long-lived token, stores
   it and redirects to `sidequestz://integrations/facebook?status=connected`.
3. The app calls `POST /integrations/facebook/import`. Your server reads the Graph API, **stores what it read**
   (the ML stack can use it) and returns a summary with suggested 1–5 ratings.
4. The person sees the suggestions. The ratings they accept are saved with the usual `PUT /me/preferences`.
   Setup only fills trip types they haven't rated; Account shows each change first.

| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `facebookConnection` | `GET /integrations/facebook` | | `FacebookConnection {connected, needs_reconnect, name?, declined_scopes, last_import?}` |
| `facebookConnectURL` | `POST /integrations/facebook/connect` | `{rerequest}` (true: ask again for permissions they turned off, `auth_type=rerequest`) | `{url}`: Facebook's Login dialog, with your `state` and redirect URI |
| (your callback) | `GET /integrations/facebook/callback` | `code`, `state` (or `error=access_denied`) from Facebook | `302` to `sidequestz://integrations/facebook?status=connected`, `status=denied` (they said no or closed the dialog; the app stays quiet) or `status=error&message=<sentence>` |
| `importFacebook` | `POST /integrations/facebook/import` | | `FacebookImport {imported_at, liked_pages, suggested_ratings, interests, home_area?, friends_on_app: [UserSearchResult]}`. If Facebook rejects the token, answer **`409`** with a sentence and set `needs_reconnect`. **Never `401`**: that would sign the person out of SideQuests |
| `disconnectFacebook` | `DELETE /integrations/facebook` | | 2xx. Revoke on Facebook (`DELETE /me/permissions`), delete the token and the imported data. Preferences they saved stay |

The steps for your side are under "Backend work: Facebook connector" below.

### Calendar, places, events
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `calendarDays` | `GET /calendar/days` | `from`, `to` (`YYYY-MM-DD` in `X-Time-Zone`) | `[CalendarDay]`: free/busy blocks, sidequests, group events. Blocks that belong to a plan carry `itinerary_id` |
| `searchPlaces` | `GET /places/search` | `q`, `near=lat,lng` | `[Place]` (the app also uses MapKit search) |
| `reverseGeocode` | `GET /places/reverse` | `lat`, `lng` | `Place` |
| `search` | `GET /search` | `q` | `SearchResults {sidequests: [Itinerary], people: [UserSearchResult], places: [Place], posts: [ForumPost]}` (Home's search bar) |
| `eventDetail` | `GET /events/{id}` | | `ItineraryItem`: full details for any timeline block (the Event sheet), including your saved `notes`, `notes_scope`, `transit_mode`, `rating` and `ticket` |

### Planning: the AI / recommendation stack plugs in here
| Method | HTTP | Body | Response |
|---|---|---|---|
| `searchActivities` | `GET /activities/search` | `q` (may be empty), `near=lat,lng` (the plan's start, when set), `date=YYYY-MM-DD` (the plan's day in `X-Time-Zone`), `limit` (the app asks for 8; default 20, max 50) | `[ActivityHit]`, a bare array: Create › Vibe › **Must-see**, searched as you type. `{id, title, kind: event\|place, category?, subtitle, place?, start?, end?, price_cents?, distance_mi?}`; `id` is the catalog activity id (the planner's `activity_id`) and `subtitle` the one line the row shows ("Live music · 6:30 PM · 0.7 mi"). Searches the user's catalog: `q` matches name, venue, category and tags (name-prefix matches first, then events by start, then distance); events only on `date` and not over, places open that day. An empty `q` suggests that day's events, then the best places near `near`. Bad `near`/`date`/`limit` → 400 with a sentence |
| `generatePlans` | `POST /plans/generate` | `PlanRequest` (where, when, mood text, quick picks, budget, who, ride, pace, modes; `modes` is sent sorted; `must_include`: the must-see picks' activity ids, left out when there are none) | `PlanBatch`: the first 3 ranked options + `cursor`. **Every option (here and in `/plans/generate/more`) contains every `must_include` activity as a stop**, with its `activity_id`; Review tags those stops "Your pick". More than 10 ids → 400 "Pick up to 10 must-see spots." (shown as is; duplicates don't count). With no options, `reason` says why: `no_candidates_fit_window`, `no_feasible_itinerary`, `invalid_request: <detail>`, `must_include_unavailable: <title>` (a pick isn't in the catalog, isn't on this date, is over or isn't for this age; the first such pick's title, "a pick" for an unknown id) or `must_include_no_fit` (the picks can't all fit in the window with travel). Each has its own sentence in Review; anything else gets the generic one |
| `moreOptions` | `POST /plans/generate/more` | `{cursor}` | `PlanBatch`: more options; `done: true` when out ("No more right now") |
| `route` | `POST /plans/route` | `RouteRequest` (option id, stop order, start, end, start time, back-by, ride, modes; keep generated options by id). `stop_order` holds the option's own stop ids **or alternatives you returned for it** (a swap puts the new id in the old one's place), and a stop that's **left out was removed** | `RouteResult`: one leg per hop (start → stop 1 … last stop → end), each stop's `{start, end}`, `arrival`, `minutes_late`, and `broken_at`: the index in `stop_order` of the first fixed-start stop this order reaches too late, or `-1` (the route card marks that stop "Late for a fixed start" and the header says "Some stops would be late") |
| `stopAlternatives` | `POST /plans/alternatives` | `{option_id, stop_id, stop_order}`: the stop to replace, and the option's current order (so suggestions fit between the neighbors and never repeat a stop that's already in it) | `[PlanAlternative {stop: PlanStop, reason}]`, about 3–5, best first. `reason` is a few words on why it's similar ("Also rooftop views · 0.2 mi away"). Remember the stops you return: their ids come back in `stop_order` and `CreateItineraryRequest` |
| `activity` | `GET /activities/{id}` | `id` is a stop's `activity_id`; `date=YYYY-MM-DD` (the plan's day in `X-Time-Zone`; without it, the account's today, demo date included) | `ActivityDetail`: Review › tap a stop opens a pane with it. `{id, title, kind: event\|place, category, category_label, summary?, description?, venue_name?, address?, place, start?, end?, hours_line?, price_cents?, price_label, rating?, rating_count?, url?, ticket_url?, image_url?, tags}`, unknowns left out and `tags` `[]` when there are none. Read from the user's catalog only: another catalog's id, or an unknown or malformed one, is a 404, which the app treats like a server without this endpoint ("More details aren't available right now." + Try again). `category_label` is the one stop subtitles use ("Live music"); `hours_line` is the opening hours on `date` ("Open 10 AM–6 PM", "Open 5 PM–2 AM", "Closed that day"), left out when the hours aren't known; `price_label` is "Free", "$18", "$6–$12", "Up to $20", a tier ("$$") when only that is known, or "Price unknown"; `url` / `ticket_url` are absolute http(s) links ("Website", "Tickets"). Bad `date` → 400 "Check the date and try again." |
| `createItinerary` | `POST /itineraries` | `CreateItineraryRequest`: `option` is the option **as edited** on Review (swapped stops in their places, removed ones gone), `stop_order` its final order; `invite_user_ids` (optional, left out when empty) are friends brought along from Vibe › Bring friends, at most 12 | `Itinerary`: shown selected on Home. The invited friends are members from the start, as after an accepted join (Home, join records, the plan's group thread opening with "Jordan added Maya and Dev"; `join.update`, `itinerary.updated`, `thread.updated` to them); a `just_me` plan with friends is saved `friends`. 400 when an id isn't an accepted friend ("You can only add friends."), for more than 12, or when everyone doesn't fit in `max_group_size` |

Leg rule used by the demo: ≤ 0.8 mi walks; otherwise Drive / Uber / MARTA from the ride answer
(`drive` / `cover` / `none`). The server owns the real transit times. A leg's `mode` is one of `walk`,
`marta` (any transit; the app labels it "Transit"), `rideshare`, `drive`, `uber`; `transit`, `bus`,
`train` and `subway` are read as `marta`, and an unknown mode as `walk`.

**Planner extras.** The required keys are `PlanBatch {options, cursor?, done}`, `PlanOption {id, name,
tag, meta, stops}`, `PlanStop {id, title, subtitle, place {name, coordinate?}, duration_minutes}`,
`RouteResult {legs, stop_times, arrival, minutes_late}`. The planner may add, and the app reads:

| Shape | Optional fields the app uses |
|---|---|
| `PlanBatch` | `reason` (above; only with empty `options`) |
| `PlanOption` | `late_flag` (a fixed-start stop would be reached after it starts at this pace: the card shows "Tight timing"), `total_cost_cents` (sum of the known prices) |
| `PlanStop` | `activity_id` (the catalog activity: Review's stop pane loads its details with it, `GET /activities/{id}`, and it comes back on the saved itinerary's items), `kind` (`event` = fixed start, `place` = visit any time), `arrive_time`, `depart_time` (the planner's schedule; `POST /plans/route` re-times the order on screen), `flexible` (`false` = a fixed start: the pane says "Starts at 7:00 PM — fixed time"; `true` = "Drop in any time while it's open") |
| `RouteResult` | `broken_at` (above) |

Anything else the planner sends (`planner`, `run_id`, an option's `legs`, `score`, `metrics`, a stop's
`category`, `tags`, `price_cents`, `address`, a leg's `distance_km`, a stop time's `stop_id`, …) is ignored
by the app, but list it here before adding it: the server's contract tests reject undocumented keys in
[`docs/api/examples/`](../docs/api/examples/) (`PlanBatch.dag`, `PlanBatch.empty`, `RouteResult.dag` show
the extras above).

"Similar" is the server's call (the ML stack's embeddings fit here). The demo suggests places of the
same kind (views, art, food, park, games, books) that aren't already in the plan, nearest first.

### Itineraries (the app calls them "sidequests"), past events, ratings
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `activeItineraries` | `GET /itineraries?status=active` | | `[Itinerary]` (each with `is_host`) |
| `itinerary` | `GET /itineraries/{id}` | | `Itinerary` |
| `updateItinerary` | `PATCH /itineraries/{id}` | `ItineraryUpdate`: only the changed fields (`title`, `date`, `start`, `back_by`, `visibility`, `stop_order` = stop item ids in the new order; stops left out are removed) | `Itinerary`, re-timed by the server when the date, window or stops change. Host only (`is_host`); reject others with 403 |
| `deleteItinerary` | `DELETE /itineraries/{id}` | | 2xx. Host only; everyone who joined loses it |
| `leaveItinerary` | `POST /itineraries/{id}/leave` | | 2xx. For people who joined someone else's plan (`is_host: false`) |
| `updateItemNotes` | `PATCH /itineraries/{id}/items/{itemId}` (calendar-only entries: `PATCH /events/{id}`) | `{notes, notes_scope?}` (`private` = only you, `shared` = everyone on the plan; omitted = unchanged) | 2xx |
| `transitOptions` | `GET /itineraries/{id}/items/{itemId}/transit` (or `/events/{id}/transit`) | | `[TransitOption]` ("Getting there": walk / MARTA / rideshare; `cost_cents` null → "[fare]") |
| `selectTransit` | `PUT /itineraries/{id}/items/{itemId}/transit` (or `/events/{id}/transit`) | `{mode}` | 2xx; comes back as the item's `transit_mode` |
| `pastEvents` | `GET /me/past-events?unrated=true\|false` | | `[PastEvent]` (`id` = the item id, so "Rate it after" and Past agree) |
| `pastInsights` | `GET /me/insights` | | `PastInsights {headline, highlights: [{id, title, value, detail?, symbol?}], top_tags, based_on}`: what the sidequests you rated 4–5 stars have in common (Home › Past, top card). `based_on: 0` = not enough ratings; `headline` is then a nudge |
| `rate` | `PUT /ratings/{itemId}` | `Rating` | 2xx; updates the taste profile server-side |

### Agent checkout (single item)
The agent works in the background. States: `preparing` (finding tickets, building the quote) →
`awaiting_approval` → `processing` (paying) → `booked`, or `failed` (with `failure_reason`) / `cancelled`.
Push every change as `checkout.status`; the app also polls `GET /checkout/intents/{id}` while it waits.

| Method | HTTP | Body | Response |
|---|---|---|---|
| `createCheckoutIntent` | `POST /checkout/intents` | `{item_id, quantity, payment_method_id?, instant}` (null card = default; `instant: true` when the user has instant checkout on) | `CheckoutIntent` (usually `preparing`). With `instant` and a total within `instant_checkout_limit_cents`, skip approval: set `instant: true` and go straight to `processing` → `booked` |
| `checkoutIntent` | `GET /checkout/intents/{id}` | | `CheckoutIntent` |
| `updateCheckoutIntent` | `PATCH /checkout/intents/{id}` | `{payment_method_id}` (Checkout › "Change") | `CheckoutIntent` |
| `approveCheckout` | `POST /checkout/intents/{id}/approve` | | `CheckoutIntent` (`processing`, then `booked` via event/poll). This is the only call that spends money. Once booked, the item carries its `ticket` |
| `cancelCheckout` | `POST /checkout/intents/{id}/cancel` | | 2xx |

### Agentic checkout (Muse buys a plan's tickets)
After a plan is saved, Muse (`muse-spark-1.3` with function tools, driven by the backend) buys the tickets for
its paid stops on the SideQuestz Events sandbox merchant (`events.sidequestz.tech`), within one budget the user
approves with Face ID. Payment is **Stripe test mode** Shared Payment Tokens: one token per purchase, capped at
that purchase's quote; the backend reserves the budget atomically before issuing each one. Muse never sees a
payment credential. The app asks after saving a plan when agentic checkout is on (Account › Payments, stored as
`instant_checkout` / `instant_checkout_limit_cents` = the default budget) and from the plan's
"Get tickets with Muse". The server answers **503** on these endpoints until Stripe and the merchant are configured.

| Method | HTTP | Body | Response |
|---|---|---|---|
| `checkoutPlan` | `GET /itineraries/{id}/checkout` | | `CheckoutPlan`: paid stops whose `ticket_url` is on the merchant (with `booked` when you already have a ticket), `estimate_cents` (tickets before fees), `default_budget_cents`, `suggested_budget_cents` (estimate + 15%, rounded up to dollars, at least the default), the default card, `available`, `agentic_checkout`, and `active_run_id` when a run is going |
| `startCheckoutRun` | `POST /itineraries/{id}/checkout-runs` | `{budget_cents (100…100000), items: [{item_id, quantity}] (1…10), payment_method_id?}` | 201 `CheckoutRun` (`running`). 409 when a run is already going or everything is booked; 400 for a bad budget, no items or no card |
| `checkoutRun` | `GET /checkout/runs/{id}` | | `CheckoutRun` with every item's `CheckoutIntent` |
| `cancelCheckoutRun` | `POST /checkout/runs/{id}/cancel` | | `CheckoutRun` (`cancelled`; tickets already bought stay) |

Each item is a `CheckoutIntent` with the run fields: `run_id`, `merchant`, `checkout_url`, `max_authorized_cents`
(the token's cap), `final_cents` (what was charged), `confirmation`, `order_ref`, `ticket_url` (the pass), and
`failure_code` when it didn't go through: `sold_out`, `price_changed`, `over_budget`, `declined` (the payment
limit stopped a charge above the quote), `card_declined`, `merchant_error`, `agent_error`, `skipped`, `cancelled`.
No new intent states: outcomes are `booked` or `failed` + `failure_code`. The run's `summary` is written from
what actually happened (never from the model's own claims); `agent` is `muse`, or `fallback` when the server
finished without Muse. Booked items carry their `ticket` on the itinerary. Saved `ItineraryItem`s now carry
`ticket_url`.

### Forum
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `forumPosts` | `GET /forum/posts` | `lat, lng` (area center; "Current location" sends the device's), `area` (label), `radius, scope (everyone\|friends), type (all\|plans\|free_now), when (any\|now\|today\|weekend), max_dist, cost=0,1, tags=Food,Art, open_only, sort (for_you\|soonest\|closest\|spots\|newest)` | `[ForumPost]` (each with your `join_status`, the plan's group chat `thread_id` once you're in, and on plans `compatibility`: how well its stops fit your taste, a whole-number percent 0–100, absent when unscored). `for_you` (the app's default) puts the best match first, then unscored posts soonest first |
| `myFreePost` | `GET /forum/posts/mine` | | `MyFreePost` or `null`/204 (your live "I'm free" post) |
| `postFreeNow` | `POST /forum/posts` | `{type: "free_now", visibility: "friends"\|"everyone", until?, lat?, lng?, area_label?, radius_mi?}` | `MyFreePost` (with `until`, `area_label`, `radius_mi` for its audience line) |
| `deleteForumPost` | `DELETE /forum/posts/{id}` | | 2xx |
| `requestToJoin` | `POST /forum/posts/{id}/join-requests` | | `JoinResult` `{status: requested\|joined\|full\|closed, itinerary_id?, thread_id?}`. Auto-accept returns `joined` (the plan is then on Home and its group chat exists); empty 2xx = `requested` |
| `cancelJoinRequest` | `DELETE /forum/posts/{id}/join-requests` | | 2xx |
| `planTogether` | `POST /forum/posts/{id}/plan-together` | | `ChatThread` (the DM with the poster) |

### Threads, album, splits
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `threads` / `thread` | `GET /threads[/{id}]` | | `[ChatThread]` / `ChatThread`: groups and DMs, most recent first, with `unread`. Groups carry `faces`, `chips`, album title/subtitle |
| `messages` | `GET /threads/{id}/messages` | `before=<message id>` (older page) | `[Message]`, oldest first |
| `sendMessage` | `POST /threads/{id}/messages` | `{text, client_id?}`: the same `client_id` twice must not post twice | `Message` (echoes `client_id`, also on its `message.new` event) |
| `markThreadRead` | `POST /threads/{id}/read` | | 2xx (unread → 0; push `thread.read` to your other devices) |
| `startDM` | `POST /threads/dm` | `{user_id}` | `ChatThread` |
| `groupPhotos` / `uploadGroupPhoto` / `deleteGroupPhoto` | `GET` / `POST` (multipart `photo`) / `DELETE /groups/{id}/photos[/{photoId}]` | | `[GroupPhoto]` / `GroupPhoto` (`uploader_id`, `created_at`; people delete only their own) |
| `ledger` | `GET /threads/{id}` + `GET /groups/{id}/expenses` + `GET /groups/{id}/balances` | | `GroupLedger` (members, expenses with server-computed `shares`, balances: + means they owe you) |
| `addExpense` | `POST /groups/{id}/expenses` | `NewExpense` | `Expense` with `shares`: `floor(total / n)` each, the first `total mod n` people pay 1¢ more, and `created_by` (who added it). Notify members (`expense.added`) |
| `deleteExpense` | `DELETE /groups/{id}/expenses/{expenseId}` | | 2xx. Only the person who added it (`created_by`) |
| `settleUp` | `POST /groups/{id}/settle` | `{amount_cents, payment_method_id?}` (what the screen showed) | 2xx; `409` if the balance changed |

### Friends
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `friends` | `GET /friends` | | `[Friend]` (live status line + activity) |
| `searchUsers` | `GET /users/search` | `q` (name or @handle) | `[UserSearchResult]` `{person, relation: none\|friend\|outgoing\|incoming, request_id?}` |
| `suggestedPeople` | `GET /people/suggested` | | `[PersonSuggestion]` `{person, relation, request_id?, compatibility?}`: up to 20 people, first those whose taste matches yours (likes minus clashes), best first, with `compatibility` a whole-number percent 0–100; then, to fill the list, people active lately, without `compatibility` (everyone, when you have no taste profile). Never you, your friends or bot accounts; `[]` for the demo cast; 503 while the ML service is down (only with a taste profile). The app keeps the list for the session and reloads it on pull-to-refresh |
| `friendRequests` | `GET /friends/requests` | | `[FriendRequest]`: incoming, plus the ones you sent with `outgoing: true` (so "Requested" survives a relaunch) |
| `sendFriendRequest` | `POST /friends/requests` | `{user_id}` | `FriendRequest` (`outgoing: true`) |
| `cancelFriendRequest` | `DELETE /friends/requests/{id}` | | 2xx (your own outgoing request) |
| `acceptFriendRequest` / `declineFriendRequest` | `POST /friends/requests/{id}/accept\|decline` | | 2xx |
| `removeFriend` | `DELETE /friends/{user_id}` | | 2xx |
| `createInvite` | `POST /invites` | | `{url}` (shareable; `https://sidequests.app/invite/<code>` should also open `sidequestz://invite/<code>`) |
| `acceptInvite` | `POST /invites/{code}/accept` | | `Friend` (the inviter) |

`PersonRef` everywhere: `{id, name, initials?, color_hex?, photo_url?, username?}` (initials and color are
derived when missing; avatars show `photo_url` when present).

### Realtime: `WS /ws`
The app connects with `Authorization: Bearer <access_token>` and expects messages shaped like
`{"type": "<event>", "data": {…}}`. [`WebSocketService`](SideQuestz/Services/WebSocketService.swift)
decodes them and publishes to screens through `RealtimeHub`.

| `type` | `data` | Screens that react |
|---|---|---|
| `message.new` | `{thread_id, message: Message}` | Chat, DMs, Groups list |
| `thread.updated` | `ChatThread` (new thread, members, last message, unread) | Groups list, DMs |
| `thread.read` | `{thread_id}` | unread badges |
| `join.request` | `{post_id, from: PersonRef}` | host: Home counts |
| `join.update` | `{post_id, result: JoinResult}` | Forum card, Home |
| `friend.status` | `{user_id, status_line}` | Friends |
| `friend.request` | `FriendRequest` | Friends › Requests |
| `forum.update` | `{}` (refetch the feed) | Forum |
| `checkout.status` | `{intent_id, state}` | Checkout, Muse sheet |
| `checkout.run` | `{run_id, state: running\|done\|cancelled, spent_cents}` | Muse sheet, Home (reload tickets when done) |
| `transit.delay` | `{itinerary_id, item_id, minutes}` | Home timeline, Event sheet |
| `itinerary.updated` | `Itinerary` | Home |
| `itinerary.removed` | `{itinerary_id}` | Home |
| `expense.added` | `{group_id, expense: Expense}` | Splits |
| `photo.added` | `{group_id, photo: GroupPhoto}` | Album |

### Voice
[`VoiceInputService.swift`](SideQuestz/Services/VoiceInputService.swift) uses Apple's Speech framework
(`SFSpeechRecognizer`), on device when the phone supports it, with automatic punctuation. No audio
reaches your backend: the transcript only fills a text field, which is sent like any typed text.

## For the backend team: differences from `Backend/API_ENDPOINTS.md`

The app is built against this file. Where `Backend/API_ENDPOINTS.md` says something else, this file wins:

- **Missing there, needed by the app:** `GET /search`, `GET /me/insights`, `GET /threads/{id}`, `POST /threads/{id}/read`, `GET /forum/posts/mine`,
  `PATCH /events/{id}` and `GET`/`PUT /events/{id}/transit` (calendar-only blocks), `PUT …/items/{itemId}/transit`,
  `POST /itineraries/{id}/leave`, `PATCH /checkout/intents/{id}`, the agentic checkout endpoints (`GET /itineraries/{id}/checkout`, `POST /itineraries/{id}/checkout-runs`, `GET /checkout/runs/{id}`, `POST /checkout/runs/{id}/cancel`), `POST /me/payment-methods/setup`,
  `DELETE /friends/requests/{id}`, `POST /invites/{code}/accept`, `DELETE /me/devices/{push_token}`, and the
  realtime events above beyond `message.new`, `POST /plans/alternatives` (Review's swap), `GET /activities/search` (Vibe's
  must-see search), and the Facebook endpoints (`GET`/`DELETE /integrations/facebook`,
  `POST /integrations/facebook/connect`, `POST /integrations/facebook/import`, plus the callbacks below).
- **Different shapes:** `POST /auth/refresh` returns `{access_token, refresh_token?, expires_at?}`; sign-up also
  takes `username` and `date_of_birth`; `GET /me` also returns `id`, `avatar_color`, `school`, `setup_complete`,
  `home_base`, `city`; plan options, stops, batches and routes carry the planner extras above;
  plan requests take `must_include`; free posts take `until` and a location; `POST /plans/route` also takes start, end, start time and back-by;
  notes have a scope; settle-up takes the amount; lists paginate with `next_cursor`; statuses are `open` / `friends_only` / `busy`; checkout intents take `instant` (preferences hold the on/off and the limit).
- **Only there (not used by the app yet):** the `GET /events` catalog, the host's join-request approval
  endpoints (joins auto-accept for now), and `lock_at` / `max_group_size` in `PATCH /itineraries/{id}`.

## Backend work: Facebook connector

Everything below is server-side; the app side is done, including the demo backend. Graph API **v26.0** was
current when this was written.

**1. Meta app (developers.facebook.com)**
- Create an app with the "Authenticate and request data from users with Facebook Login" use case, and add
  Facebook Login.
- Under Facebook Login › Settings › Valid OAuth Redirect URIs, add `https://<your-api>/integrations/facebook/callback`.
  Facebook requires HTTPS.
  - `localhost` redirects work only while the app is in Development mode, so only the Simulator on the same
    Mac can use them.
  - From a phone, use a tunnel (ngrok, Cloudflare Tunnel) or the deployed server.
- **For the demo, keep the app in Development mode** and add every demo account under App roles › Roles
  (Testers). Those people can grant every permission below without App Review.
- Going Live later needs:
  - App Review of `user_likes`, `user_location` and `user_friends`;
  - business verification;
  - a privacy policy URL;
  - the data-deletion callback in step 5.
- Server settings: `FB_APP_ID`, `FB_APP_SECRET`, `FB_GRAPH_VERSION=v26.0`. Turn on "Require App Secret" and send
  `appsecret_proof` (HMAC-SHA256 of the token, keyed with the app secret) on every Graph call.

**2. Connect (`POST /integrations/facebook/connect` → `{url}`)**
- Make a random single-use `state`, store it with the user id for 10 minutes, and return:
  `https://www.facebook.com/v26.0/dialog/oauth?client_id=<FB_APP_ID>&redirect_uri=<callback>&state=<state>&response_type=code&scope=public_profile,user_likes,user_location,user_friends`
- Add `&auth_type=rerequest` when the body says `rerequest: true`.

**3. Callback (`GET /integrations/facebook/callback`)**
- No bearer token here, since this is a browser redirect. Find the user by `state` and reject unknown or used ones.
- If Facebook sent `error=access_denied`, redirect to `sidequestz://integrations/facebook?status=denied`.
- Otherwise:
  1. `GET https://graph.facebook.com/v26.0/oauth/access_token?client_id&redirect_uri&client_secret&code` to get a short-lived token.
  2. `GET …/oauth/access_token?grant_type=fb_exchange_token&client_id&client_secret&fb_exchange_token=<token>` to get a long-lived token (about 60 days).
  3. `GET /me?fields=id,name` and `GET /me/permissions` to record the Facebook user id and the granted and declined scopes.
  4. Store the token encrypted at rest. Redirect to `…?status=connected`, or on failure to `…?status=error&message=<url-encoded sentence>`.

**4. Import (`POST /integrations/facebook/import` → `FacebookImport`)**
- **Graph calls:**
  - `GET /me?fields=id,name,location` (the city is `location.name`);
  - `GET /me/likes?fields=id,name,category,category_list,created_time&limit=100`, following `paging.next`
    (cap it at about 1,000 Pages);
  - `GET /me/friends?fields=id,name`. This only returns friends who also connected SideQuests; match their Facebook
    ids to your users and return them as `UserSearchResult` with the current relation.
- **Not available any more:** Graph API v26 has no user events edge and no tagged places, so don't plan on them.
  Posts (`user_posts`) aren't requested: the privacy and App Review cost is high for little extra signal.
- **Store** the raw rows (Pages with categories, city, friend ids) replacing the previous import. The
  recommendation / ML stack reads them from there.
- **Suggested ratings:** count liked Pages per trip type and scale to 1–5, suggesting only types with at least
  about 3 Pages. The ML team can replace this mapping. A baseline mapping from Page categories:

  | Trip type | Page categories (examples) |
  |---|---|
  | `outdoors` | Park, Outdoor Recreation, Hiking Trail, Campground, Nature Preserve, Beach |
  | `food` | Restaurant (any cuisine), Café, Coffee Shop, Bakery, Food & Beverage, Food Truck |
  | `museums` | Museum, Art Gallery, Art Museum, Artist, History Museum, Science Museum |
  | `live_music` | Musician/Band, Concert Venue, Music Festival, Live Music Venue |
  | `nightlife` | Bar, Night Club, Lounge, Pub, Brewery, Comedy Club |
  | `sports` | Sports Team, Sports League, Stadium, Gym/Physical Fitness Center, Climbing Gym, Bowling Alley |
  | `shopping` | Shopping Mall, Clothing Store, Farmers Market, Flea Market, Bookstore, Vintage Store |
  | `big_crowds` | Festival, Stadium, Amusement Park, Theme Park |
  | `long_walks` | Walking Tour, Botanical Garden, Hiking Trail, Neighborhood |
  | `early_mornings` | Yoga Studio, Running Club, Farmers Market |

- `interests` is 3–8 short plain words for what the Pages have in common ("Hiking", "Indie rock").
- **Errors:** if Graph returns error code 190 (token expired or revoked), set `needs_reconnect` and answer
  `409 {"message": "Facebook needs you to sign in again."}`.

**5. Deauthorize and data deletion**
Configure both callbacks in the app dashboard. Both receive a `signed_request`: base64url signature and payload
joined by a dot, where the signature is HMAC-SHA256 over the payload with the app secret.
- **Deauthorize** (`POST /integrations/facebook/deauthorize`): the person removed SideQuests on Facebook. Delete
  the token and mark the connection disconnected.
- **Data deletion** (`POST /integrations/facebook/data-deletion`): delete everything imported for that Facebook
  user. Respond `{"url": "<status page>", "confirmation_code": "<code>"}`.

**6. Suggested tables**
- `facebook_accounts`: `user_id` (primary key), `fb_user_id` (unique), `access_token_encrypted`,
  `token_expires_at`, `granted_scopes`, `declined_scopes`, `needs_reconnect`, `connected_at`.
- `facebook_imports`: `user_id`, `imported_at`, `pages` (id, name, category, liked_at), `city`, `friend_fb_ids`,
  `suggested_ratings`, `interests`.

## Example payloads

Generated by `ContractTests` from the demo data (`TEST_RUNNER_SQ_DUMP_CONTRACT=<dir>`, then
`python3 scripts/gen_contract_examples.py <dir> API_CONTRACT.md`). The same payloads live in
[`docs/api/examples/`](../docs/api/examples/) for the backend's contract tests.

### Auth

<details><summary><code>SignupRequest</code></summary>

```json
{
  "date_of_birth": "2004-05-02T04:00:00Z",
  "email": "jordan@gatech.edu",
  "name": "Jordan Lee",
  "password": "wander2026",
  "username": "jordanlee"
}
```
</details>

<details><summary><code>AuthResponse</code></summary>

```json
{
  "tokens": {
    "access_token": "eyJ…",
    "expires_at": "2026-09-25T18:10:00Z",
    "refresh_token": "r1"
  },
  "user": {
    "age_bracket": "adult",
    "avatar_color": "ink",
    "email": "jordan@gatech.edu",
    "id": "u-jl",
    "name": "Jordan Lee",
    "school": "Georgia Tech",
    "setup_complete": true,
    "status": "open",
    "username": "jordanlee"
  }
}
```
</details>

<details><summary><code>UserPatch</code></summary>

```json
{
  "status": "busy"
}
```
</details>

### Me

<details><summary><code>User</code></summary>

```json
{
  "age_bracket": "adult",
  "avatar_color": "ink",
  "email": "jordan@gatech.edu",
  "id": "u-jl",
  "name": "Jordan Lee",
  "school": "Georgia Tech",
  "setup_complete": true,
  "status": "open",
  "username": "jordanlee"
}
```
</details>

<details><summary><code>UserHomeBase</code> (GET /me for an account with a home base (the demo account))</summary>

```json
{
  "age_bracket": "adult",
  "avatar_color": "sage",
  "city": "saltlight",
  "email": "demo@sidequestz.tech",
  "home_base": {
    "coordinate": {
      "lat": 31.368,
      "lng": -81.425
    },
    "name": "Seaside Market Square"
  },
  "id": "seed-sandy",
  "name": "Sandy Byte",
  "school": "Saltlight Harbor College",
  "setup_complete": true,
  "status": "open",
  "username": "sandybyte"
}
```
</details>

<details><summary><code>Preferences</code></summary>

```json
{
  "answers": {},
  "company": "small_group",
  "flexibility": "bit_over_ok",
  "instant_checkout": false,
  "instant_checkout_limit_cents": 5000,
  "pace": "balanced",
  "prefer_free": true,
  "ratings": {
    "food": 4,
    "museums": 3,
    "nightlife": 2,
    "outdoors": 5
  },
  "spend": "under_15",
  "split_style": "equally"
}
```
</details>

<details><summary><code>TasteProfile</code></summary>

```json
{
  "bars": [
    {
      "label": "Outdoors",
      "value": 0.82
    },
    {
      "label": "Food",
      "value": 0.7
    },
    {
      "label": "Art",
      "value": 0.55
    },
    {
      "label": "Social",
      "value": 0.64
    },
    {
      "label": "Nightlife",
      "value": 0.28
    }
  ]
}
```
</details>

<details><summary><code>Integrations</code></summary>

```json
[
  {
    "connected": true,
    "provider": "google"
  },
  {
    "connected": false,
    "provider": "outlook"
  }
]
```
</details>

<details><summary><code>PaymentMethods</code></summary>

```json
[
  {
    "brand": "Visa",
    "id": "pm-4242",
    "is_default": true,
    "last4": "4242"
  }
]
```
</details>

### Facebook (Graph API)

<details><summary><code>FacebookConnection</code> (GET /integrations/facebook)</summary>

```json
{
  "connected": true,
  "declined_scopes": [],
  "last_import": {
    "friends_on_app": [
      {
        "person": {
          "color_hex": "#9D174D",
          "id": "u-pk",
          "initials": "PK",
          "name": "Priya K."
        },
        "relation": "none"
      },
      {
        "person": {
          "color_hex": "#4338CA",
          "id": "u-cn",
          "initials": "CN",
          "name": "Chris N."
        },
        "relation": "incoming",
        "request_id": "fr-chris"
      }
    ],
    "home_area": "Atlanta, Georgia",
    "imported_at": "2026-09-25T18:10:00Z",
    "interests": [
      "Hiking",
      "Indie rock",
      "Coffee",
      "Street food",
      "Board games"
    ],
    "liked_pages": 48,
    "suggested_ratings": {
      "food": 5,
      "live_music": 5,
      "long_walks": 4,
      "museums": 3,
      "nightlife": 2,
      "outdoors": 5,
      "sports": 3
    }
  },
  "name": "Jordan Lee",
  "needs_reconnect": false
}
```
</details>

<details><summary><code>FacebookImport</code> (POST /integrations/facebook/import)</summary>

```json
{
  "friends_on_app": [
    {
      "person": {
        "color_hex": "#9D174D",
        "id": "u-pk",
        "initials": "PK",
        "name": "Priya K."
      },
      "relation": "none"
    },
    {
      "person": {
        "color_hex": "#4338CA",
        "id": "u-cn",
        "initials": "CN",
        "name": "Chris N."
      },
      "relation": "incoming",
      "request_id": "fr-chris"
    }
  ],
  "home_area": "Atlanta, Georgia",
  "imported_at": "2026-09-25T18:10:00Z",
  "interests": [
    "Hiking",
    "Indie rock",
    "Coffee",
    "Street food",
    "Board games"
  ],
  "liked_pages": 48,
  "suggested_ratings": {
    "food": 5,
    "live_music": 5,
    "long_walks": 4,
    "museums": 3,
    "nightlife": 2,
    "outdoors": 5,
    "sports": 3
  }
}
```
</details>

### Calendar + events

<details><summary><code>CalendarDays</code></summary>

```json
[
  {
    "date": "2026-09-25T04:00:00Z",
    "id": "2026-09-25",
    "items": [
      {
        "end": "2026-09-25T14:45:00Z",
        "id": "cal-2026-09-25-0",
        "interested": [],
        "kind": "busy",
        "people": [],
        "start": "2026-09-25T13:30:00Z",
        "title": "MATH 3012"
      },
      {
        "end": "2026-09-25T17:50:00Z",
        "id": "a1",
        "interested": [],
        "itinerary_id": "itin-fri",
        "kind": "busy",
        "people": [],
        "start": "2026-09-25T17:00:00Z",
        "title": "CS 3510 lecture"
      },
      {
        "end": "2026-09-25T20:00:00Z",
        "id": "a3",
        "interested": [],
        "itinerary_id": "itin-fri",
        "kind": "sidequest",
        "people": [],
        "start": "2026-09-25T18:30:00Z",
        "title": "Skyline Park rooftop"
      },
      {
        "end": "2026-09-25T21:30:00Z",
        "id": "a5",
        "interested": [],
        "itinerary_id": "itin-fri",
        "kind": "sidequest",
        "people": [],
        "start": "2026-09-25T20:20:00Z",
        "title": "Krog Street Tunnel murals"
      },
      {
        "end": "2026-09-25T23:30:00Z",
        "id": "a6",
        "interested": [
          {
            "color_hex": "#B45309",
            "id": "u-ak",
            "initials": "AK",
            "name": "Ava K."
          }
        ],
        "itinerary_id": "itin-fri",
        "kind": "group",
        "people": [
          {
            "color_hex": "#18211C",
            "id": "u-jl",
            "initials": "JL",
            "name": "Jordan Lee"
          },
          {
            "color_hex": "#1D4ED8",
            "id": "u-mr",
            "initials": "MR",
            "name": "Maya R."
          },
          {
            "color_hex": "#0F766E",
            "id": "u-dp",
            "initials": "DP",
            "name": "Dev P."
          }
        ],
        "start": "2026-09-25T22:00:00Z",
        "title": "Group dinner, Krog St"
      }
    ]
  }
]
```
</details>

<details><summary><code>ItineraryItem</code></summary>

```json
{
  "bookable": false,
  "description": "3 people joined your open plan. Joining locked at 5 PM.",
  "end": "2026-09-25T23:30:00Z",
  "extra_going": 0,
  "id": "a6",
  "interested": [
    {
      "color_hex": "#B45309",
      "id": "u-ak",
      "initials": "AK",
      "name": "Ava K."
    }
  ],
  "kind": "group",
  "people": [
    {
      "color_hex": "#18211C",
      "id": "u-jl",
      "initials": "JL",
      "name": "Jordan Lee"
    },
    {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    }
  ],
  "place": {
    "coordinate": {
      "lat": 33.7571,
      "lng": -84.364
    },
    "name": "Krog Street Market"
  },
  "start": "2026-09-25T22:00:00Z",
  "title": "Group dinner, Krog Street Market",
  "website_url": "https://krogstreetmarket.com"
}
```
</details>

<details><summary><code>TransitOptions</code></summary>

```json
[
  {
    "cost_cents": 0,
    "minutes": 18,
    "mode": "walk"
  },
  {
    "cost_cents": 250,
    "minutes": 12,
    "mode": "marta"
  },
  {
    "minutes": 8,
    "mode": "rideshare"
  }
]
```
</details>

<details><summary><code>SearchResults</code> (GET /search?q=krog)</summary>

```json
{
  "people": [],
  "places": [
    {
      "coordinate": {
        "lat": 33.7571,
        "lng": -84.364
      },
      "name": "Krog Street Market"
    }
  ],
  "posts": [],
  "sidequests": [
    {
      "back_by": "2026-09-26T00:00:00Z",
      "date": "2026-09-25T04:00:00Z",
      "end_place": {
        "coordinate": {
          "lat": 33.771,
          "lng": -84.3918
        },
        "name": "Home · North Ave Apts"
      },
      "going_count": 3,
      "id": "itin-fri",
      "is_host": true,
      "items": [
        {
          "bookable": false,
          "description": "From your Google Calendar. SideQuests plans around it.",
          "end": "2026-09-25T17:50:00Z",
          "extra_going": 0,
          "id": "a1",
          "interested": [],
          "kind": "busy",
          "people": [],
          "place": {
            "name": "Klaus Advanced Computing Building"
          },
          "start": "2026-09-25T17:00:00Z",
          "title": "CS 3510 lecture"
        },
        {
          "bookable": false,
          "description": "Route options below. Times update live if you run late.",
          "end": "2026-09-25T18:25:00Z",
          "extra_going": 0,
          "id": "a2",
          "interested": [],
          "kind": "transit",
          "people": [],
          "place": {
            "name": "Tech Square → Ponce City Market"
          },
          "start": "2026-09-25T18:00:00Z",
          "title": "Transit to Ponce City Market"
        },
        {
          "bookable": true,
          "description": "Mini golf, carnival games and skyline views on the roof.",
          "end": "2026-09-25T20:00:00Z",
          "extra_going": 0,
          "id": "a3",
          "interested": [],
          "kind": "sidequest",
          "people": [],
          "place": {
            "coordinate": {
              "lat": 33.7727,
              "lng": -84.3653
            },
            "name": "Ponce City Market, rooftop"
          },
          "start": "2026-09-25T18:30:00Z",
          "ticket_url": "https://events.sidequestz.tech/skyline-park-rooftop/tickets",
          "title": "Skyline Park rooftop",
          "website_url": "https://poncecitymarket.com"
        },
        {
          "bookable": false,
          "description": "Route options below. Times update live if you run late.",
          "end": "2026-09-25T20:20:00Z",
          "extra_going": 0,
          "id": "a4",
          "interested": [],
          "kind": "transit",
          "people": [],
          "place": {
            "name": "BeltLine Eastside Trail"
          },
          "start": "2026-09-25T20:00:00Z",
          "title": "Walk the Eastside Trail"
        },
        {
          "bookable": false,
          "description": "Self-guided street art walk. Free.",
          "end": "2026-09-25T21:30:00Z",
          "extra_going": 0,
          "id": "a5",
          "interested": [],
          "kind": "sidequest",
          "people": [],
          "place": {
            "coordinate": {
              "lat": 33.7535,
              "lng": -84.363
            },
            "name": "Krog Street Tunnel, Cabbagetown"
          },
          "start": "2026-09-25T20:20:00Z",
          "title": "Krog Street Tunnel murals",
          "website_url": "https://atlantabeltline.org"
        },
        {
          "bookable": false,
          "description": "3 people joined your open plan. Joining locked at 5 PM.",
          "end": "2026-09-25T23:30:00Z",
          "extra_going": 0,
          "id": "a6",
          "interested": [
            {
              "color_hex": "#B45309",
              "id": "u-ak",
              "initials": "AK",
              "name": "Ava K."
            }
          ],
          "kind": "group",
          "people": [
            {
              "color_hex": "#18211C",
              "id": "u-jl",
              "initials": "JL",
              "name": "Jordan Lee"
            },
            {
              "color_hex": "#1D4ED8",
              "id": "u-mr",
              "initials": "MR",
              "name": "Maya R."
            },
            {
              "color_hex": "#0F766E",
              "id": "u-dp",
              "initials": "DP",
              "name": "Dev P."
            }
          ],
          "place": {
            "coordinate": {
              "lat": 33.7571,
              "lng": -84.364
            },
            "name": "Krog Street Market"
          },
          "start": "2026-09-25T22:00:00Z",
          "title": "Group dinner, Krog Street Market",
          "website_url": "https://krogstreetmarket.com"
        }
      ],
      "lock_at": "2026-09-25T21:00:00Z",
      "max_group_size": 6,
      "start": "2026-09-25T17:00:00Z",
      "start_place": {
        "coordinate": {
          "lat": 33.7766,
          "lng": -84.389
        },
        "name": "Tech Square (current location)"
      },
      "title": "Free Friday afternoon",
      "visibility": "open"
    }
  ]
}
```
</details>

### Planning

<details><summary><code>ActivityHits</code> (GET /activities/search: an event, a free place, a place with no known price or distance (no near))</summary>

```json
[
  {
    "category": "trivia",
    "distance_mi": 1.5,
    "end": "2026-09-25T20:00:00Z",
    "id": "act-rooftop-trivia",
    "kind": "event",
    "place": {
      "coordinate": {
        "lat": 33.7727,
        "lng": -84.3653
      },
      "name": "Skyline Park rooftop"
    },
    "price_cents": 500,
    "start": "2026-09-25T19:00:00Z",
    "subtitle": "Trivia · 3:00 PM · 1.5 mi",
    "title": "Rooftop trivia"
  },
  {
    "category": "views",
    "distance_mi": 1.5,
    "id": "act-jackson-street-bridge",
    "kind": "place",
    "place": {
      "coordinate": {
        "lat": 33.7596,
        "lng": -84.3721
      },
      "name": "Jackson Street Bridge"
    },
    "price_cents": 0,
    "subtitle": "Skyline views · Free · 1.5 mi",
    "title": "Jackson Street Bridge"
  },
  {
    "category": "food",
    "id": "act-politan-row",
    "kind": "place",
    "place": {
      "coordinate": {
        "lat": 33.787,
        "lng": -84.3839
      },
      "name": "Politan Row"
    },
    "subtitle": "Food hall · $",
    "title": "Politan Row"
  }
]
```
</details>

<details><summary><code>PlanRequest</code></summary>

```json
{
  "back_by": "2026-09-25T22:30:00Z",
  "budget": 1,
  "date": "2026-09-25T18:10:00Z",
  "end": {
    "coordinate": {
      "lat": 33.771,
      "lng": -84.3918
    },
    "name": "Home · North Ave Apts"
  },
  "modes": [
    "marta",
    "walk"
  ],
  "mood_text": "Something chill and outside, then cheap food after.",
  "pace": "balanced",
  "range": "transit",
  "ride": "none",
  "start": {
    "coordinate": {
      "lat": 33.7766,
      "lng": -84.389
    },
    "name": "Tech Square (current location)"
  },
  "start_time": "2026-09-25T18:10:00Z",
  "tags": [
    "Outdoors",
    "Food",
    "Meet people"
  ],
  "who": "friends"
}
```
</details>

<details><summary><code>PlanRequest.mustInclude</code> (with two must-see picks: every option includes them)</summary>

```json
{
  "back_by": "2026-09-25T22:30:00Z",
  "budget": 1,
  "date": "2026-09-25T18:10:00Z",
  "end": {
    "coordinate": {
      "lat": 33.771,
      "lng": -84.3918
    },
    "name": "Home · North Ave Apts"
  },
  "modes": [
    "marta",
    "walk"
  ],
  "mood_text": "Something chill and outside, then cheap food after.",
  "must_include": [
    "act-rooftop-trivia",
    "act-jackson-street-bridge"
  ],
  "pace": "balanced",
  "range": "transit",
  "ride": "none",
  "start": {
    "coordinate": {
      "lat": 33.7766,
      "lng": -84.389
    },
    "name": "Tech Square (current location)"
  },
  "start_time": "2026-09-25T18:10:00Z",
  "tags": [
    "Outdoors",
    "Food",
    "Meet people"
  ],
  "who": "friends"
}
```
</details>

<details><summary><code>PlanBatch</code> (the demo planner: required keys only)</summary>

```json
{
  "cursor": "batch-0",
  "done": false,
  "options": [
    {
      "id": "opt-a",
      "late_flag": false,
      "meta": "~$ · 1.8 mi walking · 2 transit legs",
      "name": "Rooftop + murals",
      "stops": [
        {
          "duration_minutes": 80,
          "id": "opt-a-0",
          "place": {
            "coordinate": {
              "lat": 33.7727,
              "lng": -84.3653
            },
            "name": "Skyline Park rooftop"
          },
          "subtitle": "Games + views · $",
          "title": "Skyline Park rooftop"
        },
        {
          "duration_minutes": 60,
          "id": "opt-a-1",
          "place": {
            "coordinate": {
              "lat": 33.7535,
              "lng": -84.363
            },
            "name": "Krog Street Tunnel murals"
          },
          "subtitle": "Street art · Free",
          "title": "Krog Street Tunnel murals"
        },
        {
          "duration_minutes": 35,
          "id": "opt-a-2",
          "place": {
            "coordinate": {
              "lat": 33.7571,
              "lng": -84.364
            },
            "name": "Krog Street Market"
          },
          "subtitle": "Food hall · $",
          "title": "Krog Street Market"
        }
      ],
      "tag": "Best match"
    },
    {
      "id": "opt-b",
      "late_flag": false,
      "meta": "~$ · 2.3 mi walking · 1 transit leg",
      "name": "Park + food hall",
      "stops": [
        {
          "duration_minutes": 75,
          "id": "opt-b-0",
          "place": {
            "coordinate": {
              "lat": 33.787,
              "lng": -84.374
            },
            "name": "Piedmont Park loop"
          },
          "subtitle": "Walk · Free",
          "title": "Piedmont Park loop"
        },
        {
          "duration_minutes": 60,
          "id": "opt-b-1",
          "place": {
            "coordinate": {
              "lat": 33.777,
              "lng": -84.388
            },
            "name": "Board game café"
          },
          "subtitle": "Games · $",
          "title": "Board game café"
        },
        {
          "duration_minutes": 50,
          "id": "opt-b-2",
          "place": {
            "coordinate": {
              "lat": 33.7725,
              "lng": -84.3657
            },
            "name": "Ponce City Market food hall"
          },
          "subtitle": "Food hall · $$",
          "title": "Ponce City Market food hall"
        }
      ],
      "tag": "Chill"
    },
    {
      "id": "opt-c",
      "late_flag": false,
      "meta": "~$$ · 1.1 mi walking · 2 transit legs",
      "name": "Downtown loop",
      "stops": [
        {
          "duration_minutes": 60,
          "id": "opt-c-0",
          "place": {
            "coordinate": {
              "lat": 33.7603,
              "lng": -84.3932
            },
            "name": "Centennial Olympic Park"
          },
          "subtitle": "Park · Free",
          "title": "Centennial Olympic Park"
        },
        {
          "duration_minutes": 50,
          "id": "opt-c-1",
          "place": {
            "coordinate": {
              "lat": 33.759,
              "lng": -84.3925
            },
            "name": "SkyView Ferris wheel"
          },
          "subtitle": "Views · $$",
          "title": "SkyView Ferris wheel"
        },
        {
          "duration_minutes": 75,
          "id": "opt-c-2",
          "place": {
            "coordinate": {
              "lat": 33.7575,
              "lng": -84.3645
            },
            "name": "Open group dinner (Forum)"
          },
          "subtitle": "3 going · $",
          "title": "Open group dinner (Forum)"
        }
      ],
      "tag": "Meet people"
    }
  ]
}
```
</details>

<details><summary><code>PlanBatch.dag</code> (the DAG planner: options with late_flag / total_cost_cents, stops with their extras)</summary>

```json
{
  "cursor": "dag_run_7f3k_2",
  "done": false,
  "options": [
    {
      "id": "run_7f3k-0",
      "late_flag": false,
      "meta": "~$ · 1.8 mi walking · 2 marta legs",
      "name": "Rooftop + murals",
      "stops": [
        {
          "activity_id": "act_rooftop",
          "arrive_time": "2026-09-25T18:24:00Z",
          "depart_time": "2026-09-25T19:44:00Z",
          "duration_minutes": 80,
          "flexible": true,
          "id": "stop_act_rooftop_0",
          "kind": "place",
          "place": {
            "coordinate": {
              "lat": 33.7727,
              "lng": -84.3653
            },
            "name": "Skyline Park rooftop"
          },
          "subtitle": "Games + views · $",
          "title": "Skyline Park rooftop"
        },
        {
          "activity_id": "act_murals",
          "arrive_time": "2026-09-25T19:58:00Z",
          "depart_time": "2026-09-25T20:58:00Z",
          "duration_minutes": 60,
          "flexible": true,
          "id": "stop_act_murals_1",
          "kind": "place",
          "place": {
            "coordinate": {
              "lat": 33.7535,
              "lng": -84.363
            },
            "name": "Krog Street Tunnel murals"
          },
          "subtitle": "Street art · Free",
          "title": "Krog Street Tunnel murals"
        },
        {
          "activity_id": "act_krog",
          "arrive_time": "2026-09-25T21:01:00Z",
          "depart_time": "2026-09-25T21:36:00Z",
          "duration_minutes": 35,
          "flexible": true,
          "id": "stop_act_krog_2",
          "kind": "place",
          "place": {
            "coordinate": {
              "lat": 33.7571,
              "lng": -84.364
            },
            "name": "Krog Street Market"
          },
          "subtitle": "Food hall · $",
          "title": "Krog Street Market"
        }
      ],
      "tag": "Best match",
      "total_cost_cents": 2400
    },
    {
      "id": "run_7f3k-1",
      "late_flag": true,
      "meta": "~$$ · 1.1 mi walking · 2 marta legs",
      "name": "Downtown loop",
      "stops": [
        {
          "activity_id": "act_centennial",
          "arrive_time": "2026-09-25T18:32:00Z",
          "depart_time": "2026-09-25T19:32:00Z",
          "duration_minutes": 60,
          "flexible": true,
          "id": "stop_act_centennial_0",
          "kind": "place",
          "place": {
            "coordinate": {
              "lat": 33.7603,
              "lng": -84.3932
            },
            "name": "Centennial Olympic Park"
          },
          "subtitle": "Park · Free",
          "title": "Centennial Olympic Park"
        },
        {
          "activity_id": "act_skyview",
          "arrive_time": "2026-09-25T19:35:00Z",
          "depart_time": "2026-09-25T20:25:00Z",
          "duration_minutes": 50,
          "flexible": true,
          "id": "stop_act_skyview_1",
          "kind": "place",
          "place": {
            "coordinate": {
              "lat": 33.759,
              "lng": -84.3925
            },
            "name": "SkyView Ferris wheel"
          },
          "subtitle": "Views · $$",
          "title": "SkyView Ferris wheel"
        },
        {
          "activity_id": "act_dinner",
          "arrive_time": "2026-09-25T21:42:00Z",
          "depart_time": "2026-09-25T22:57:00Z",
          "duration_minutes": 75,
          "flexible": false,
          "id": "stop_act_dinner_2",
          "kind": "event",
          "place": {
            "coordinate": {
              "lat": 33.7575,
              "lng": -84.3645
            },
            "name": "Open group dinner (Forum)"
          },
          "subtitle": "Community event · $ · 5:30 PM",
          "title": "Open group dinner (Forum)"
        }
      ],
      "tag": "Meet people",
      "total_cost_cents": 3400
    }
  ]
}
```
</details>

<details><summary><code>PlanBatch.empty</code> (no options, with the reason)</summary>

```json
{
  "done": true,
  "options": [],
  "reason": "no_candidates_fit_window"
}
```
</details>

<details><summary><code>RouteRequest</code></summary>

```json
{
  "back_by": "2026-09-25T22:30:00Z",
  "end": {
    "coordinate": {
      "lat": 33.771,
      "lng": -84.3918
    },
    "name": "Home · North Ave Apts"
  },
  "modes": [
    "marta",
    "walk"
  ],
  "option_id": "opt-a",
  "ride": "none",
  "start": {
    "coordinate": {
      "lat": 33.7766,
      "lng": -84.389
    },
    "name": "Tech Square (current location)"
  },
  "start_time": "2026-09-25T18:10:00Z",
  "stop_order": [
    "opt-a-0",
    "opt-a-1",
    "opt-a-2"
  ]
}
```
</details>

<details><summary><code>RouteResult</code></summary>

```json
{
  "arrival": "2026-09-25T21:57:00Z",
  "broken_at": -1,
  "legs": [
    {
      "minutes": 14,
      "mode": "marta"
    },
    {
      "minutes": 14,
      "mode": "marta"
    },
    {
      "minutes": 3,
      "mode": "walk"
    },
    {
      "minutes": 21,
      "mode": "marta"
    }
  ],
  "minutes_late": 0,
  "stop_times": [
    {
      "end": "2026-09-25T19:44:00Z",
      "start": "2026-09-25T18:24:00Z"
    },
    {
      "end": "2026-09-25T20:58:00Z",
      "start": "2026-09-25T19:58:00Z"
    },
    {
      "end": "2026-09-25T21:36:00Z",
      "start": "2026-09-25T21:01:00Z"
    }
  ]
}
```
</details>

<details><summary><code>RouteResult.dag</code> (an order that reaches a fixed start too late: broken_at, minutes_late)</summary>

```json
{
  "arrival": "2026-09-25T21:57:00Z",
  "broken_at": 1,
  "legs": [
    {
      "minutes": 14,
      "mode": "marta"
    },
    {
      "minutes": 14,
      "mode": "marta"
    },
    {
      "minutes": 3,
      "mode": "walk"
    },
    {
      "minutes": 21,
      "mode": "marta"
    }
  ],
  "minutes_late": 25,
  "stop_times": [
    {
      "end": "2026-09-25T19:44:00Z",
      "start": "2026-09-25T18:24:00Z"
    },
    {
      "end": "2026-09-25T20:58:00Z",
      "start": "2026-09-25T19:58:00Z"
    },
    {
      "end": "2026-09-25T21:36:00Z",
      "start": "2026-09-25T21:01:00Z"
    }
  ]
}
```
</details>

<details><summary><code>PlanAlternatives</code> (POST /plans/alternatives)</summary>

```json
[
  {
    "reason": "Also art to see · 0.9 mi away",
    "stop": {
      "duration_minutes": 45,
      "id": "alt-cabbagetown-murals",
      "place": {
        "coordinate": {
          "lat": 33.7507,
          "lng": -84.361
        },
        "name": "Cabbagetown murals"
      },
      "subtitle": "Street art · Free",
      "title": "Cabbagetown murals"
    }
  },
  {
    "reason": "Also art to see · 2.4 mi away",
    "stop": {
      "duration_minutes": 90,
      "id": "alt-high-museum-of-art",
      "place": {
        "coordinate": {
          "lat": 33.7901,
          "lng": -84.3856
        },
        "name": "High Museum of Art"
      },
      "subtitle": "Museum · $$",
      "title": "High Museum of Art"
    }
  },
  {
    "reason": "Also art to see · 2.7 mi away",
    "stop": {
      "duration_minutes": 50,
      "id": "alt-castleberry-hill-galleries",
      "place": {
        "coordinate": {
          "lat": 33.7487,
          "lng": -84.4007
        },
        "name": "Castleberry Hill galleries"
      },
      "subtitle": "Galleries · Free",
      "title": "Castleberry Hill galleries"
    }
  },
  {
    "reason": "Also art to see · 3.6 mi away",
    "stop": {
      "duration_minutes": 60,
      "id": "alt-atlanta-contemporary",
      "place": {
        "coordinate": {
          "lat": 33.7839,
          "lng": -84.4153
        },
        "name": "Atlanta Contemporary"
      },
      "subtitle": "Art gallery · Free",
      "title": "Atlanta Contemporary"
    }
  }
]
```
</details>

<details><summary><code>ActivityDetail</code> (GET /activities/{id} for a place: its hours on the plan's day, rating and links)</summary>

```json
{
  "address": "675 Ponce De Leon Ave NE, Atlanta, GA 30308",
  "category": "games",
  "category_label": "Games + views",
  "description": "Take the freight elevator to the top of Ponce City Market for nine holes of mini golf, skee-ball and boardwalk-style games, with the Midtown and downtown skylines on every side. Games are paid by the round; walking around the roof is free. It gets busy after 5 on Fridays, so an afternoon visit means shorter lines.",
  "hours_line": "Open 11 AM–10 PM",
  "id": "act-skyline-park-rooftop",
  "kind": "place",
  "place": {
    "coordinate": {
      "lat": 33.7727,
      "lng": -84.3653
    },
    "name": "Skyline Park rooftop"
  },
  "price_label": "$",
  "rating": 4.5,
  "rating_count": 2318,
  "summary": "Mini golf, carnival games and skyline views on the roof of Ponce City Market.",
  "tags": [
    "games",
    "arcade",
    "mini golf"
  ],
  "ticket_url": "https://events.sidequestz.tech/skyline-park-rooftop/tickets",
  "title": "Skyline Park rooftop",
  "url": "https://poncecitymarket.com"
}
```
</details>

<details><summary><code>ActivityDetail.event</code> (an event: its start, end, venue and price)</summary>

```json
{
  "address": "1280 Peachtree St NE, Atlanta, GA 30309",
  "category": "art",
  "category_label": "Gallery talk",
  "end": "2026-09-25T20:15:00Z",
  "id": "act-gallery-talk-at-the-high",
  "kind": "event",
  "place": {
    "coordinate": {
      "lat": 33.7901,
      "lng": -84.3856
    },
    "name": "High Museum of Art"
  },
  "price_cents": 1800,
  "price_label": "$18",
  "start": "2026-09-25T19:30:00Z",
  "summary": "A curator walks through the new exhibition in 45 minutes; museum admission is included.",
  "tags": [
    "art",
    "museum",
    "talk"
  ],
  "ticket_url": "https://events.sidequestz.tech/gallery-talk-at-the-high/tickets",
  "title": "Gallery talk at the High",
  "url": "https://high.org",
  "venue_name": "High Museum of Art"
}
```
</details>

<details><summary><code>CreateItineraryRequest</code></summary>

```json
{
  "lock_at": "2026-09-25T17:30:00Z",
  "max_group_size": 6,
  "option": {
    "id": "opt-a",
    "late_flag": false,
    "meta": "~$ · 1.8 mi walking · 2 transit legs",
    "name": "Rooftop + murals",
    "stops": [
      {
        "duration_minutes": 80,
        "id": "opt-a-0",
        "place": {
          "coordinate": {
            "lat": 33.7727,
            "lng": -84.3653
          },
          "name": "Skyline Park rooftop"
        },
        "subtitle": "Games + views · $",
        "title": "Skyline Park rooftop"
      },
      {
        "duration_minutes": 60,
        "id": "opt-a-1",
        "place": {
          "coordinate": {
            "lat": 33.7535,
            "lng": -84.363
          },
          "name": "Krog Street Tunnel murals"
        },
        "subtitle": "Street art · Free",
        "title": "Krog Street Tunnel murals"
      },
      {
        "duration_minutes": 35,
        "id": "opt-a-2",
        "place": {
          "coordinate": {
            "lat": 33.7571,
            "lng": -84.364
          },
          "name": "Krog Street Market"
        },
        "subtitle": "Food hall · $",
        "title": "Krog Street Market"
      }
    ],
    "tag": "Best match"
  },
  "plan": {
    "back_by": "2026-09-25T22:30:00Z",
    "budget": 1,
    "date": "2026-09-25T18:10:00Z",
    "end": {
      "coordinate": {
        "lat": 33.771,
        "lng": -84.3918
      },
      "name": "Home · North Ave Apts"
    },
    "modes": [
      "marta",
      "walk"
    ],
    "mood_text": "Something chill and outside, then cheap food after.",
    "pace": "balanced",
    "range": "transit",
    "ride": "none",
    "start": {
      "coordinate": {
        "lat": 33.7766,
        "lng": -84.389
      },
      "name": "Tech Square (current location)"
    },
    "start_time": "2026-09-25T18:10:00Z",
    "tags": [
      "Outdoors",
      "Food",
      "Meet people"
    ],
    "who": "friends"
  },
  "route": {
    "arrival": "2026-09-25T21:57:00Z",
    "broken_at": -1,
    "legs": [
      {
        "minutes": 14,
        "mode": "marta"
      },
      {
        "minutes": 14,
        "mode": "marta"
      },
      {
        "minutes": 3,
        "mode": "walk"
      },
      {
        "minutes": 21,
        "mode": "marta"
      }
    ],
    "minutes_late": 0,
    "stop_times": [
      {
        "end": "2026-09-25T19:44:00Z",
        "start": "2026-09-25T18:24:00Z"
      },
      {
        "end": "2026-09-25T20:58:00Z",
        "start": "2026-09-25T19:58:00Z"
      },
      {
        "end": "2026-09-25T21:36:00Z",
        "start": "2026-09-25T21:01:00Z"
      }
    ]
  },
  "stop_order": [
    "opt-a-0",
    "opt-a-1",
    "opt-a-2"
  ],
  "visibility": "friends"
}
```
</details>

### Itineraries + ratings

<details><summary><code>Itinerary</code></summary>

```json
{
  "back_by": "2026-09-26T00:00:00Z",
  "date": "2026-09-25T04:00:00Z",
  "end_place": {
    "coordinate": {
      "lat": 33.771,
      "lng": -84.3918
    },
    "name": "Home · North Ave Apts"
  },
  "going_count": 3,
  "id": "itin-fri",
  "is_host": true,
  "items": [
    {
      "bookable": false,
      "description": "From your Google Calendar. SideQuests plans around it.",
      "end": "2026-09-25T17:50:00Z",
      "extra_going": 0,
      "id": "a1",
      "interested": [],
      "kind": "busy",
      "people": [],
      "place": {
        "name": "Klaus Advanced Computing Building"
      },
      "start": "2026-09-25T17:00:00Z",
      "title": "CS 3510 lecture"
    },
    {
      "bookable": false,
      "description": "Route options below. Times update live if you run late.",
      "end": "2026-09-25T18:25:00Z",
      "extra_going": 0,
      "id": "a2",
      "interested": [],
      "kind": "transit",
      "people": [],
      "place": {
        "name": "Tech Square → Ponce City Market"
      },
      "start": "2026-09-25T18:00:00Z",
      "title": "Transit to Ponce City Market"
    },
    {
      "bookable": true,
      "description": "Mini golf, carnival games and skyline views on the roof.",
      "end": "2026-09-25T20:00:00Z",
      "extra_going": 0,
      "id": "a3",
      "interested": [],
      "kind": "sidequest",
      "people": [],
      "place": {
        "coordinate": {
          "lat": 33.7727,
          "lng": -84.3653
        },
        "name": "Ponce City Market, rooftop"
      },
      "start": "2026-09-25T18:30:00Z",
      "ticket_url": "https://events.sidequestz.tech/skyline-park-rooftop/tickets",
      "title": "Skyline Park rooftop",
      "website_url": "https://poncecitymarket.com"
    },
    {
      "bookable": false,
      "description": "Route options below. Times update live if you run late.",
      "end": "2026-09-25T20:20:00Z",
      "extra_going": 0,
      "id": "a4",
      "interested": [],
      "kind": "transit",
      "people": [],
      "place": {
        "name": "BeltLine Eastside Trail"
      },
      "start": "2026-09-25T20:00:00Z",
      "title": "Walk the Eastside Trail"
    },
    {
      "bookable": false,
      "description": "Self-guided street art walk. Free.",
      "end": "2026-09-25T21:30:00Z",
      "extra_going": 0,
      "id": "a5",
      "interested": [],
      "kind": "sidequest",
      "people": [],
      "place": {
        "coordinate": {
          "lat": 33.7535,
          "lng": -84.363
        },
        "name": "Krog Street Tunnel, Cabbagetown"
      },
      "start": "2026-09-25T20:20:00Z",
      "title": "Krog Street Tunnel murals",
      "website_url": "https://atlantabeltline.org"
    },
    {
      "bookable": false,
      "description": "3 people joined your open plan. Joining locked at 5 PM.",
      "end": "2026-09-25T23:30:00Z",
      "extra_going": 0,
      "id": "a6",
      "interested": [
        {
          "color_hex": "#B45309",
          "id": "u-ak",
          "initials": "AK",
          "name": "Ava K."
        }
      ],
      "kind": "group",
      "people": [
        {
          "color_hex": "#18211C",
          "id": "u-jl",
          "initials": "JL",
          "name": "Jordan Lee"
        },
        {
          "color_hex": "#1D4ED8",
          "id": "u-mr",
          "initials": "MR",
          "name": "Maya R."
        },
        {
          "color_hex": "#0F766E",
          "id": "u-dp",
          "initials": "DP",
          "name": "Dev P."
        }
      ],
      "place": {
        "coordinate": {
          "lat": 33.7571,
          "lng": -84.364
        },
        "name": "Krog Street Market"
      },
      "start": "2026-09-25T22:00:00Z",
      "title": "Group dinner, Krog Street Market",
      "website_url": "https://krogstreetmarket.com"
    }
  ],
  "lock_at": "2026-09-25T21:00:00Z",
  "max_group_size": 6,
  "start": "2026-09-25T17:00:00Z",
  "start_place": {
    "coordinate": {
      "lat": 33.7766,
      "lng": -84.389
    },
    "name": "Tech Square (current location)"
  },
  "title": "Free Friday afternoon",
  "visibility": "open"
}
```
</details>

<details><summary><code>ItineraryUpdate</code> (PATCH body)</summary>

```json
{
  "back_by": "2026-09-25T23:00:00Z",
  "start": "2026-09-25T19:00:00Z",
  "stop_order": [
    "a5",
    "a3",
    "a6"
  ],
  "title": "Rooftop evening",
  "visibility": "open"
}
```
</details>

<details><summary><code>PastEvents</code></summary>

```json
[
  {
    "company": "with 3 others",
    "date": "2026-09-24T23:00:00Z",
    "id": "x1",
    "kind": "group",
    "place": "Tech Square",
    "title": "Board game café"
  },
  {
    "company": "solo",
    "date": "2026-09-24T20:00:00Z",
    "id": "x2",
    "kind": "sidequest",
    "place": "BeltLine",
    "title": "Eastside Trail walk"
  }
]
```
</details>

<details><summary><code>PastInsights</code> (GET /me/insights)</summary>

```json
{
  "based_on": 3,
  "headline": "You like afternoon sidequests on your own.",
  "highlights": [
    {
      "detail": "1 of your 2 favorites",
      "id": "time",
      "symbol": "moon.stars",
      "title": "Favorite time",
      "value": "Afternoons"
    },
    {
      "detail": "1 of your 2 favorites",
      "id": "company",
      "symbol": "person.2",
      "title": "Company",
      "value": "On your own"
    },
    {
      "detail": "1 of your 2 favorites",
      "id": "area",
      "symbol": "mappin.and.ellipse",
      "title": "Favorite area",
      "value": "Decatur Square"
    },
    {
      "detail": "5 stars",
      "id": "best",
      "symbol": "star",
      "title": "Top rated",
      "value": "Jazz night in Decatur"
    }
  ],
  "top_tags": [
    "Good value",
    "Great people",
    "Would go again"
  ]
}
```
</details>

<details><summary><code>Rating</code></summary>

```json
{
  "note": "great music, but the line was long",
  "stars": 4,
  "tags": [
    "Great people"
  ]
}
```
</details>

<details><summary><code>Ticket</code> (on a booked item)</summary>

```json
{
  "confirmation": "SQ-4F7K2",
  "id": "tk-1",
  "mine": true,
  "quantity": 2,
  "total_cents": 2400,
  "url": "https://tickets.example/SQ-4F7K2"
}
```
</details>

### Checkout

<details><summary><code>CheckoutIntent</code></summary>

```json
{
  "card_brand": "Visa",
  "card_last4": "4242",
  "id": "ci-47",
  "instant": false,
  "item_id": "a3",
  "item_title": "Skyline Park rooftop",
  "payment_method_id": "pm-4242",
  "quantity": 1,
  "state": "awaiting_approval",
  "steps": [
    {
      "done": true,
      "text": "Found tickets on the official site"
    },
    {
      "done": true,
      "text": "Filled in your name and email"
    },
    {
      "done": false,
      "text": "Waiting for your approval"
    }
  ]
}
```
</details>

### Agentic checkout

<details><summary><code>CheckoutPlan</code> (GET /itineraries/{id}/checkout)</summary>

```json
{
  "agentic_checkout": true,
  "available": true,
  "card_brand": "Visa",
  "card_last4": "4242",
  "default_budget_cents": 5000,
  "estimate_cents": 2700,
  "items": [
    {
      "booked": false,
      "item_id": "stop-2",
      "merchant": "events.sidequestz.tech",
      "price_cents": 1200,
      "quantity": 1,
      "start": "2026-09-26T22:30:00Z",
      "ticket_url": "https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets",
      "title": "Sunset Jazz on Pier Nine"
    },
    {
      "booked": false,
      "item_id": "stop-4",
      "merchant": "events.sidequestz.tech",
      "price_cents": 1500,
      "quantity": 1,
      "start": "2026-09-27T02:00:00Z",
      "ticket_url": "https://events.sidequestz.tech/barnacle-bash-silent-disco/tickets",
      "title": "Barnacle Bash Silent Disco"
    }
  ],
  "itinerary_id": "665f1c2e9b1e8a0012345678",
  "payment_method_id": "pm-4242",
  "suggested_budget_cents": 5000
}
```
</details>

<details><summary><code>CreateCheckoutRun</code> (POST /itineraries/{id}/checkout-runs)</summary>

```json
{
  "budget_cents": 5000,
  "items": [
    {
      "item_id": "stop-2",
      "quantity": 2
    },
    {
      "item_id": "stop-4",
      "quantity": 2
    }
  ],
  "payment_method_id": "pm-4242"
}
```
</details>

<details><summary><code>CheckoutRun</code> (GET /checkout/runs/{id}: one stop booked, the other over what was left of the budget)</summary>

```json
{
  "agent": "muse",
  "budget_cents": 5000,
  "card_brand": "Visa",
  "card_last4": "4242",
  "created_at": "2026-09-26T21:00:00Z",
  "currency": "usd",
  "finished_at": "2026-09-26T21:01:00Z",
  "id": "run-9",
  "intents": [
    {
      "card_brand": "Visa",
      "card_last4": "4242",
      "checkout_url": "https://events.sidequestz.tech/sunset-jazz-on-pier-nine/tickets",
      "confirmation": "SL-7KD4Q",
      "fees_cents": 292,
      "final_cents": 2692,
      "id": "ci-51",
      "instant": false,
      "item_id": "stop-2",
      "item_title": "Sunset Jazz on Pier Nine",
      "max_authorized_cents": 2692,
      "merchant": "events.sidequestz.tech",
      "order_ref": "SL-7KD4Q",
      "payment_method_id": "pm-4242",
      "quantity": 2,
      "run_id": "run-9",
      "state": "booked",
      "steps": [
        {
          "done": false,
          "text": "Waiting for Muse to open the ticket page"
        },
        {
          "done": true,
          "text": "Opened the ticket page"
        },
        {
          "done": true,
          "text": "Found 2 tickets · $26.92 with fees"
        },
        {
          "done": true,
          "text": "Paying with a Stripe token limited to $26.92"
        },
        {
          "done": true,
          "text": "Booked · SL-7KD4Q"
        }
      ],
      "subtotal_cents": 2400,
      "ticket_url": "https://events.sidequestz.tech/t/9f3c2a7e5b1d4c8e0a6f2b9d4e1c7a3f",
      "total_cents": 2692
    },
    {
      "card_brand": "Visa",
      "card_last4": "4242",
      "checkout_url": "https://events.sidequestz.tech/barnacle-bash-silent-disco/tickets",
      "failure_code": "over_budget",
      "failure_reason": "$33.40 is more than the $23.08 left in your budget.",
      "fees_cents": 340,
      "id": "ci-52",
      "instant": false,
      "item_id": "stop-4",
      "item_title": "Barnacle Bash Silent Disco",
      "merchant": "events.sidequestz.tech",
      "payment_method_id": "pm-4242",
      "quantity": 2,
      "run_id": "run-9",
      "state": "failed",
      "steps": [
        {
          "done": false,
          "text": "Waiting for Muse to open the ticket page"
        },
        {
          "done": true,
          "text": "Opened the ticket page"
        },
        {
          "done": true,
          "text": "Found 2 tickets · $33.40 with fees"
        },
        {
          "done": true,
          "text": "$33.40 is more than the $23.08 left in your budget."
        }
      ],
      "subtotal_cents": 3000,
      "total_cents": 3340
    }
  ],
  "itinerary_id": "665f1c2e9b1e8a0012345678",
  "spent_cents": 2692,
  "state": "done",
  "summary": "Muse got 1 of 2: Sunset Jazz on Pier Nine (2 tickets, $26.92, SL-7KD4Q). Barnacle Bash Silent Disco: over budget. Spent $26.92 of your $50.00 budget (sandbox: nothing was charged)."
}
```
</details>

### Forum

<details><summary><code>ForumQuery</code> (app side of GET /forum/posts; sent as query parameters)</summary>

```json
{
  "area": {
    "coordinate": {
      "lat": 33.7838,
      "lng": -84.3833
    },
    "is_current_location": false,
    "name": "Midtown Atlanta"
  },
  "cost": [],
  "open_only": false,
  "radius_mi": 2,
  "scope": "everyone",
  "sort": "for_you",
  "tags": [],
  "type": "all",
  "when": "any"
}
```
</details>

<details><summary><code>ForumPosts</code></summary>

```json
[
  {
    "author": {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    "capacity": 6,
    "compatibility": 92,
    "day": "today",
    "distance_mi": 0.4,
    "friends_only": false,
    "going": [
      {
        "color_hex": "#0F766E",
        "id": "u-dp",
        "initials": "DP",
        "name": "Dev P."
      },
      {
        "color_hex": "#B45309",
        "id": "u-ak",
        "initials": "AK",
        "name": "Ava K."
      },
      {
        "color_hex": "#4338CA",
        "id": "u-cn",
        "initials": "CN",
        "name": "Chris N."
      }
    ],
    "going_count": 4,
    "id": "p1",
    "interested_count": 2,
    "is_friend": true,
    "join_status": "none",
    "lock_label": "Locks 5:00 PM",
    "meta": "Hosting · 0.4 mi away",
    "plan_together_sent": false,
    "posted_minutes_ago": 25,
    "price_tier": 1,
    "route": "3 stops along the Eastside Trail",
    "spots_left": 2,
    "starts_in_minutes": 210,
    "tags": [
      "Outdoors",
      "Food"
    ],
    "title": "Sunset + tacos on the BeltLine",
    "type": "plan",
    "when": "Today · 5:30–8 PM"
  },
  {
    "author": {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    },
    "day": "today",
    "distance_mi": 0.3,
    "friends_only": true,
    "going": [],
    "going_count": 0,
    "id": "p2",
    "interested_count": 0,
    "is_friend": true,
    "join_status": "none",
    "meta": "Free now · 0.3 mi away",
    "plan_together_sent": false,
    "posted_minutes_ago": 8,
    "price_tier": 0,
    "starts_in_minutes": 0,
    "tags": [
      "Outdoors",
      "Games"
    ],
    "text": "Free 2–6 near Tech Square. Down for anything outside or a board game café.",
    "type": "free_now"
  }
]
```
</details>

<details><summary><code>MyFreePost</code></summary>

```json
{
  "area_label": "Midtown Atlanta",
  "id": "mine",
  "radius_mi": 2,
  "text": "Free until 6:30 PM near Tech Square",
  "until": "2026-09-25T22:30:00Z",
  "visibility": "friends"
}
```
</details>

<details><summary><code>NewFreePost</code> (app side of POST /forum/posts)</summary>

```json
{
  "area": {
    "coordinate": {
      "lat": 33.7838,
      "lng": -84.3833
    },
    "is_current_location": false,
    "name": "Midtown Atlanta"
  },
  "radius_mi": 2,
  "until": "2026-09-25T22:30:00Z",
  "visibility": "everyone"
}
```
</details>

<details><summary><code>JoinResult</code></summary>

```json
{
  "itinerary_id": "itin-42",
  "status": "joined",
  "thread_id": "g42"
}
```
</details>

### Threads, album, splits

<details><summary><code>ChatThread</code></summary>

```json
{
  "album_subtitle": "9 photos · 3 people",
  "album_title": "Krog St dinner · Sep 25",
  "chips": [
    "3 people",
    "You owe $9",
    "9 photos"
  ],
  "faces": [
    {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    }
  ],
  "id": "g1",
  "is_group": true,
  "last_message": "Maya: photo dump in the album after pls",
  "last_time": "5:12 PM",
  "members": [
    {
      "color_hex": "#18211C",
      "id": "u-jl",
      "initials": "JL",
      "name": "Jordan Lee"
    },
    {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    }
  ],
  "subtitle": "3 people · Today 6 PM",
  "title": "Krog St dinner crew",
  "unread": 2
}
```
</details>

<details><summary><code>Messages</code></summary>

```json
[
  {
    "id": "m-1",
    "sender_id": "u-mr",
    "sender_name": "Maya",
    "sent_at": "2026-09-25T17:13:00Z",
    "text": "grabbing a table by the window"
  },
  {
    "id": "m-2",
    "sender_id": "u-jl",
    "sender_name": "You",
    "sent_at": "2026-09-25T17:16:00Z",
    "text": "omw, 5 min out"
  }
]
```
</details>

<details><summary><code>Message</code> (with client_id)</summary>

```json
{
  "client_id": "c-5A1B",
  "id": "m-9",
  "sender_id": "u-jl",
  "sender_name": "You",
  "sent_at": "2026-09-25T18:10:00Z",
  "text": "omw"
}
```
</details>

<details><summary><code>GroupPhotos</code></summary>

```json
[
  {
    "by_name": "Maya",
    "id": "ph-5",
    "placeholder_hex": "#DDD3F3",
    "uploader_id": "u-mr"
  }
]
```
</details>

<details><summary><code>NewExpense</code></summary>

```json
{
  "amount_cents": 4000,
  "payer_id": "u-jl",
  "split_among": [
    "u-jl",
    "u-mr",
    "u-dp"
  ],
  "what": "Pizza"
}
```
</details>

<details><summary><code>GroupLedger</code></summary>

```json
{
  "balances": [
    {
      "net_cents": 250,
      "user_id": "u-mr"
    },
    {
      "net_cents": -1150,
      "user_id": "u-dp"
    }
  ],
  "expenses": [
    {
      "amount_cents": 4200,
      "created_by": "u-dp",
      "id": "ex-14",
      "payer_id": "u-dp",
      "shares": [
        1400,
        1400,
        1400
      ],
      "split_among": [
        "u-jl",
        "u-mr",
        "u-dp"
      ],
      "what": "Dumplings"
    },
    {
      "amount_cents": 750,
      "created_by": "u-jl",
      "id": "ex-15",
      "payer_id": "u-jl",
      "shares": [
        250,
        250,
        250
      ],
      "split_among": [
        "u-jl",
        "u-mr",
        "u-dp"
      ],
      "what": "MARTA fares"
    }
  ],
  "members": [
    {
      "color_hex": "#18211C",
      "id": "u-jl",
      "initials": "JL",
      "name": "Jordan Lee"
    },
    {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    }
  ]
}
```
</details>

### Friends

<details><summary><code>Friends</code></summary>

```json
[
  {
    "activity": "free",
    "person": {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    "status_line": "Free until 8 PM"
  },
  {
    "activity": "free",
    "person": {
      "color_hex": "#0F766E",
      "id": "u-dp",
      "initials": "DP",
      "name": "Dev P."
    },
    "status_line": "Free now · 0.3 mi away"
  },
  {
    "activity": "on_sidequest",
    "person": {
      "color_hex": "#BE185D",
      "id": "u-st",
      "initials": "ST",
      "name": "Sam T."
    },
    "status_line": "On a sidequest · Thrift crawl"
  },
  {
    "activity": "busy",
    "person": {
      "color_hex": "#B45309",
      "id": "u-ak",
      "initials": "AK",
      "name": "Ava K."
    },
    "status_line": "Busy until 5 PM"
  }
]
```
</details>

<details><summary><code>FriendRequests</code></summary>

```json
[
  {
    "id": "fr-chris",
    "note": "Met on Stone Mountain sunrise",
    "outgoing": false,
    "person": {
      "color_hex": "#4338CA",
      "id": "u-cn",
      "initials": "CN",
      "name": "Chris N."
    }
  }
]
```
</details>

<details><summary><code>OutgoingFriendRequest</code> (response of POST /friends/requests)</summary>

```json
{
  "id": "fr-48",
  "note": "Requested just now",
  "outgoing": true,
  "person": {
    "color_hex": "#BE185D",
    "id": "u-st",
    "initials": "ST",
    "name": "Sam T."
  }
}
```
</details>

<details><summary><code>UserSearchResults</code></summary>

```json
[
  {
    "person": {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    "relation": "friend"
  },
  {
    "person": {
      "color_hex": "#B45309",
      "id": "u-ak",
      "initials": "AK",
      "name": "Ava K."
    },
    "relation": "friend"
  },
  {
    "person": {
      "color_hex": "#BE185D",
      "id": "u-st",
      "initials": "ST",
      "name": "Sam T."
    },
    "relation": "friend"
  },
  {
    "person": {
      "color_hex": "#9D174D",
      "id": "u-pk",
      "initials": "PK",
      "name": "Priya K."
    },
    "relation": "none"
  }
]
```
</details>

<details><summary><code>PersonSuggestions</code> (GET /people/suggested)</summary>

```json
[
  {
    "compatibility": 88,
    "person": {
      "color_hex": "#9D174D",
      "id": "u-pk",
      "initials": "PK",
      "name": "Priya K."
    },
    "relation": "none"
  },
  {
    "compatibility": 74,
    "person": {
      "color_hex": "#4338CA",
      "id": "u-cn",
      "initials": "CN",
      "name": "Chris N."
    },
    "relation": "incoming",
    "request_id": "fr-chris"
  },
  {
    "compatibility": 63,
    "person": {
      "color_hex": "#4338CA",
      "id": "u-go",
      "initials": "GO",
      "name": "GT Outdoors Club"
    },
    "relation": "none"
  }
]
```
</details>
