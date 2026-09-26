# SideQuests iOS ↔ backend contract

The iOS app is a GUI shell over this API. Every screen talks to one Swift protocol,
[`APIClient`](SideQuestz/Services/APIClient.swift). There are two implementations:

| | File | Used when |
|---|---|---|
| **Live** | [`Services/LiveAPIClient.swift`](SideQuestz/Services/LiveAPIClient.swift) | `SQAPIMode = live`: REST + JSON calls to your server, exactly as listed below |
| **Mock** | [`Services/Mock/`](SideQuestz/Services/Mock/) | `SQAPIMode = mock` (the default): an in-memory stand-in with the demo data, so the app runs offline |

The app contains no recommendation, AI or ranking logic. Plan generation, transit timing,
split math, age filtering, join limits and the taste profile all come from the server. The
app validates forms and previews an equal split (`SplitMath`), but it always shows the server's
result. The mock client imitates those behaviors only so the GUI works before the backend exists.
Treat it as a reference for the expected behavior, not as logic to keep.

## Pointing the app at your server

Set these keys in [`SideQuestz/Info.plist`](SideQuestz/Info.plist), or override them at launch
(Xcode ▸ Product ▸ Scheme ▸ Edit Scheme ▸ Run ▸ Arguments):

| Key | Default | Launch-argument override |
|---|---|---|
| `SQAPIMode` | `mock` | `-SQAPIMode live` |
| `SQAPIBaseURL` | `http://127.0.0.1:8000` | `-SQAPIBaseURL http://192.168.1.20:8000` |
| `SQWebSocketURL` | `ws://127.0.0.1:8000/ws` | `-SQWebSocketURL ws://192.168.1.20:8000/ws` |
| `SQMockDelay` | `5` (seconds every demo call waits, so loading states show; delete for per-call timings) | `-SQMockDelay 1` |

`127.0.0.1` reaches your Mac from the Simulator. On a real iPhone, use the Mac's LAN IP or a deployed
HTTPS URL. Plain `http://` is allowed only for local networks (`NSAllowsLocalNetworking`).

## Conventions

- **JSON keys are `snake_case`.** Enum values are `snake_case` too (`"not_free"`, `"just_me"`, `"free_now"`).
- **Dates are ISO 8601 strings with a time zone** (`"2026-09-25T18:10:00Z"`; fractional seconds are accepted).
- **Money is integer cents** (`amount_cents: 4000`). Unknown real prices are `null`. The app then shows
  placeholders like `$[price]` and never invents a price.
- **Auth.** `Authorization: Bearer <access_token>` on everything except `/auth/*`. On a `401` the app calls
  `POST /auth/refresh` once and retries. Tokens are stored in the Keychain.
- **Errors.** Any non-2xx status. Put a user-facing sentence in `{"message": "…"}` (or FastAPI's `{"detail": "…"}`).
  `400`/`422` messages are shown to the user as-is.
- **Lists.** Either a bare JSON array or `{"items": […], "next_cursor": "…"}`.
- **Leniency.** Optional fields may be omitted. Defaults are in
  [`Models/Decoding.swift`](SideQuestz/Models/Decoding.swift). The Swift models in
  [`Models/`](SideQuestz/Models/) are the source of truth for field names.
- `ContractTests` checks that every model round-trips through these settings.

## Endpoints

`APIClient` method → HTTP call. Types are the Swift models; JSON examples are at the end.

### Auth
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `signup` | `POST /auth/signup` | `SignupRequest` | `AuthResponse` |
| `login` | `POST /auth/login` | `{email, password}` | `AuthResponse` (401 → "That email and password don't match.") |
| `refresh` | `POST /auth/refresh` | `{refresh_token}` | `AuthTokens` |
| `logout` | `POST /auth/logout` | | 2xx |
| `forgotPassword` | `POST /auth/password/forgot` | `{email}` | always 2xx (never reveal whether the email exists) |
| `verifyResetCode` | `POST /auth/password/verify` | `{email, code}` | `{reset_token}` (400/401 → invalid code) |
| `resetPassword` | `POST /auth/password/reset` | `{reset_token, new_password}` | 2xx; sign out other sessions |
| `resendResetCode` | `POST /auth/password/resend` | `{email}` | 2xx |

### Me, integrations, payments
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `me` / `updateMe` | `GET` / `PATCH /me` | `UserPatch` | `User` |
| `uploadPhoto` | `POST /me/photo` | multipart, field `photo`, `image/jpeg` | `{url}` |
| `deletePhoto` | `DELETE /me/photo` | | 2xx |
| `setAvatarColor` | `PATCH /me/avatar` | `{color}` (`ink`/`sage`/`clay`/`forest`/`sand`) | 2xx |
| `preferences` / `savePreferences` | `GET` / `PUT /me/preferences` | `Preferences` | `Preferences` / 2xx |
| `tasteProfile` | `GET /me/taste-profile` | | `TasteProfile` (bars 0…1) |
| `registerDevice` | `POST /me/devices` | `{push_token, platform: "ios"}` | 2xx |
| `integrations` | `GET /integrations` | | `[Integration]` |
| `connectIntegration` | `POST /integrations/{google\|outlook}/connect` | | `{url}`. The OAuth URL opens in `ASWebAuthenticationSession` and should redirect to `sidequestz://…` when done |
| `disconnectIntegration` | `DELETE /integrations/{provider}` | | 2xx |
| `paymentMethods` / `addPaymentMethod` / `deletePaymentMethod` | `GET` / `POST` / `DELETE /me/payment-methods[/{id}]` | `{token}` (tokenized Visa) | `[PaymentMethod]` / `PaymentMethod` |

### Calendar, places, events
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `calendarDays` | `GET /calendar/days` | `from`, `to` (`YYYY-MM-DD`) | `[CalendarDay]`: free/busy blocks, sidequests, group events |
| `searchPlaces` | `GET /places/search` | `q`, `near=lat,lng` | `[Place]` (the app also uses MapKit search) |
| `reverseGeocode` | `GET /places/reverse` | `lat`, `lng` | `Place` |
| `eventDetail` | `GET /events/{id}` | | `ItineraryItem`: full details for any timeline block (the Event sheet) |

### Planning: the AI / recommendation stack plugs in here
| Method | HTTP | Body | Response |
|---|---|---|---|
| `generatePlans` | `POST /plans/generate` | `PlanRequest` (where, when, mood text, quick picks, budget, who, ride, pace, modes) | `PlanBatch`: the first 3 ranked options + `cursor` |
| `moreOptions` | `POST /plans/generate/more` | `{cursor}` | `PlanBatch`: 2 more; `done: true` when out ("No more right now") |
| `route` | `POST /plans/route` | `RouteRequest` (option id + stop order + ride + modes) | `RouteResult`: one leg per hop (start → stop 1 … last stop → end), each stop's `{start, end}`, `arrival`, `minutes_late` |
| `createItinerary` | `POST /itineraries` | `CreateItineraryRequest` | `Itinerary`: shown selected on Home |

Leg rule used by the demo: ≤ 0.8 mi walks; otherwise Drive / Uber / MARTA from the ride answer
(`drive` / `cover` / `none`). The server owns the real transit times.

### Itineraries, past events, ratings
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `activeItineraries` | `GET /itineraries?status=active` | | `[Itinerary]` |
| `itinerary` / `deleteItinerary` | `GET` / `DELETE /itineraries/{id}` | | `Itinerary` / 2xx |
| `updateItemNotes` | `PATCH /itineraries/{id}/items/{itemId}` (calendar-only entries: `PATCH /events/{id}`) | `{notes}` | 2xx |
| `transitOptions` | `GET /itineraries/{id}/items/{itemId}/transit` (or `/events/{id}/transit`) | | `[TransitOption]` ("Getting there": walk / MARTA / rideshare; `cost_cents` null → "[fare]") |
| `pastEvents` | `GET /me/past-events?unrated=true\|false` | | `[PastEvent]` |
| `rate` | `PUT /ratings/{itemId}` | `Rating` | 2xx; updates the taste profile server-side |

### Agent checkout (Visa)
| Method | HTTP | Body | Response |
|---|---|---|---|
| `createCheckoutIntent` | `POST /checkout/intents` | `{item_id, quantity}` | `CheckoutIntent` (agent steps, subtotal/fees/total cents or null, card) |
| `checkoutIntent` | `GET /checkout/intents/{id}` | | `CheckoutIntent` (status is also pushed over the WebSocket) |
| `approveCheckout` | `POST /checkout/intents/{id}/approve` | | `CheckoutIntent` (`state: "booked"`). This is the only call that spends money |
| `cancelCheckout` | `POST /checkout/intents/{id}/cancel` | | 2xx |

### Forum
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `forumPosts` | `GET /forum/posts` | `lat, lng, radius, scope (everyone\|friends), type (all\|plans\|free_now), when (any\|now\|today\|weekend), max_dist, cost=0,1, tags=Food,Art, open_only, sort (soonest\|closest\|spots\|newest)` | `[ForumPost]` |
| `myFreePost` | `GET /forum/posts?mine=true` | | `[MyFreePost]` (0 or 1: your live "I'm free" post) |
| `postFreeNow` | `POST /forum/posts` | `{type: "free_now", visibility: "friends"\|"everyone"}` | `MyFreePost` |
| `deleteForumPost` | `DELETE /forum/posts/{id}` | | 2xx |
| `requestToJoin` / `cancelJoinRequest` | `POST` / `DELETE /forum/posts/{id}/join-requests` | | 2xx |
| `planTogether` | `POST /forum/posts/{id}/plan-together` | | `ChatThread` (the DM with the poster) |

### Threads, album, splits
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `threads` / `thread` | `GET /threads[/{id}]` | | `[ChatThread]` / `ChatThread` (groups carry `faces`, `chips`, album title/subtitle) |
| `messages` / `sendMessage` | `GET` / `POST /threads/{id}/messages` | `before` / `{text}` | `[Message]` / `Message` |
| `startDM` | `POST /threads/dm` | `{user_id}` | `ChatThread` |
| `groupPhotos` / `uploadGroupPhoto` / `deleteGroupPhoto` | `GET` / `POST` (multipart `photo`) / `DELETE /groups/{id}/photos[/{photoId}]` | | `[GroupPhoto]` / `GroupPhoto` |
| `ledger` | `GET /threads/{id}` + `GET /groups/{id}/expenses` + `GET /groups/{id}/balances` | | `GroupLedger` (members, expenses with server-computed `shares`, balances: + means they owe you) |
| `addExpense` | `POST /groups/{id}/expenses` | `NewExpense` | `Expense` with `shares`: `floor(total / n)` each, the first `total mod n` people pay 1¢ more. Notify members |
| `deleteExpense` | `DELETE /groups/{id}/expenses/{expenseId}` | | 2xx |
| `settleUp` | `POST /groups/{id}/settle` | | 2xx (pays what you owe with the default card) |

### Friends
| Method | HTTP | Body / query | Response |
|---|---|---|---|
| `friends` | `GET /friends` | | `[Friend]` (live status line + activity) |
| `searchUsers` | `GET /users/search` | `q` | `[PersonRef]` |
| `friendRequests` / `sendFriendRequest` | `GET` / `POST /friends/requests` | `{user_id}` | `[FriendRequest]` / 2xx |
| `acceptFriendRequest` / `declineFriendRequest` | `POST /friends/requests/{id}/accept\|decline` | | 2xx |
| `removeFriend` | `DELETE /friends/{id}` | | 2xx |
| `createInvite` | `POST /invites` | | `{url}` (shareable invite link) |

### Realtime: `WS /ws`
The app connects with `Authorization: Bearer <access_token>` and expects messages shaped like
`{"type": "<event>", "data": {…}}`. [`WebSocketService`](SideQuestz/Services/WebSocketService.swift)
decodes them and publishes to screens through `RealtimeHub`.

| `type` | `data` |
|---|---|
| `message.new` | `{thread_id, message: Message}` |
| `join.request` | `{post_id, from: PersonRef}` |
| `friend.status` | `{user_id, status_line}` |
| `forum.update` | `{}` (refetch the feed) |
| `checkout.status` | `{intent_id, state}` |
| `transit.delay` | `{itinerary_id, item_id, minutes}` |

### Voice
[`VoiceInputService.swift`](SideQuestz/Services/VoiceInputService.swift) uses Apple's Speech framework
(`SFSpeechRecognizer`), on device when the phone supports it, with automatic punctuation. No audio
reaches your backend: the transcript only fills a text field, which is sent like any typed text.

## Example payloads

These are generated by `ContractTests` from the demo data, using the live client's JSON settings.

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
    "status": "open",
    "username": "jordanlee"
  }
}
```
</details>

<details><summary><code>UserPatch</code></summary>

```json
{
  "status": "not_free"
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
  "status": "open",
  "username": "jordanlee"
}
```
</details>

<details><summary><code>Preferences</code></summary>

```json
{
  "answers": {},
  "company": "small_group",
  "flexibility": "bit_over_ok",
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
        "kind": "busy",
        "people": [],
        "start": "2026-09-25T17:00:00Z",
        "title": "CS 3510 lecture"
      },
      {
        "end": "2026-09-25T20:00:00Z",
        "id": "a3",
        "interested": [],
        "kind": "sidequest",
        "people": [],
        "start": "2026-09-25T18:30:00Z",
        "title": "Skyline Park rooftop"
      },
      {
        "end": "2026-09-25T21:30:00Z",
        "id": "a5",
        "interested": [],
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

### Planning

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
    "walk",
    "marta"
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

<details><summary><code>PlanBatch</code></summary>

```json
{
  "cursor": "batch-0",
  "done": false,
  "options": [
    {
      "id": "opt-a",
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
    "walk",
    "marta"
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

<details><summary><code>CreateItineraryRequest</code></summary>

```json
{
  "lock_at": "2026-09-25T17:30:00Z",
  "max_group_size": 6,
  "option": {
    "id": "opt-a",
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
      "walk",
      "marta"
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

### Checkout

<details><summary><code>CheckoutIntent</code></summary>

```json
{
  "card_brand": "Visa",
  "card_last4": "4242",
  "id": "ci-47",
  "item_id": "a3",
  "item_title": "Skyline Park rooftop",
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

### Forum

<details><summary><code>ForumPosts</code></summary>

```json
[
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
    "join_requested": false,
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
  },
  {
    "author": {
      "color_hex": "#1D4ED8",
      "id": "u-mr",
      "initials": "MR",
      "name": "Maya R."
    },
    "capacity": 6,
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
    "join_requested": false,
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
  }
]
```
</details>

<details><summary><code>MyFreePost</code></summary>

```json
{
  "id": "mine",
  "text": "Free until 6:30 PM near Tech Square",
  "visibility": "friends"
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
  "unread": 0
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

<details><summary><code>GroupPhotos</code></summary>

```json
[
  {
    "by_name": "Maya",
    "id": "ph-5",
    "placeholder_hex": "#DDD3F3"
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
